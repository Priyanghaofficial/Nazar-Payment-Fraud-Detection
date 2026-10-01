package consortium

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nazar/internal/audit"
)

type Op string

const (
	OpReport  Op = "report"
	OpRetract Op = "retract"
	OpDispute Op = "dispute"
)

type Status string

const (
	StatusActive    Status = "ACTIVE"
	StatusRetracted Status = "RETRACTED"
	StatusDisputed  Status = "DISPUTED"
	StatusExpired   Status = "EXPIRED"
)

// Entry is one consortium registry row (docs/05 §4.6's wire format, minus the transport
// framing — Wire() renders that separately).
type Entry struct {
	EntryID      string
	Token        string
	Epoch        int
	Kind         string
	ThreatClass  string
	Reporter     string // participant id, e.g. "BANK_A"
	LegalEntity  string
	Status       Status
	Confidence   float64
	CaseID       string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ChainSeq     int64
	PrevHash     []byte
	Hash         []byte
}

// legalEntityOf collapses participant codes that belong to the same legal entity (docs/05
// §4.3: "two BINs of the same bank... count once"). P0: a small static map demonstrating
// the mechanism; a real deployment resolves this from the participant registry.
var legalEntityAliases = map[string]string{
	"BANK_A":       "LEI-BANK-A",
	"BANK_A_CARDS": "LEI-BANK-A", // same legal entity as BANK_A — the case this rule exists for
	"BANK_B":       "LEI-BANK-B",
}

func legalEntityOf(participant string) string {
	if le, ok := legalEntityAliases[participant]; ok {
		return le
	}
	return "LEI-" + participant
}

// Registry is the P0 consortium store: Postgres-backed (consortium_entries, docs/02 §7),
// with per-reporter hash chains held in memory (docs/05 §4.4 — "per-reporter chains + a
// published Merkle root. No global order, no consensus, no trusted operator").
type Registry struct {
	db        *sql.DB
	tokeniser *Tokeniser

	mu     sync.Mutex
	chains map[string]*audit.Chain // reporter -> its own chain
}

func NewRegistry(db *sql.DB, tokeniser *Tokeniser) *Registry {
	return &Registry{db: db, tokeniser: tokeniser, chains: map[string]*audit.Chain{}}
}

func (r *Registry) chainFor(reporter string) *audit.Chain {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.chains[reporter]
	if !ok {
		c = audit.NewChain(0)
		r.chains[reporter] = c
	}
	return c
}

// Report publishes a new entry (docs/05 §4.2: "publish a token with a threat class and a
// mandatory TTL, default 180d").
func (r *Registry) Report(ctx context.Context, reporter, account, threatClass, caseID string, confidence float64, ttl time.Duration) (*Entry, error) {
	if ttl <= 0 {
		ttl = 180 * 24 * time.Hour
	}
	epoch := r.tokeniser.CurrentEpoch()
	token, err := r.tokeniser.Token(epoch, "creditor_account", account)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	entry := &Entry{
		Token: token, Epoch: epoch, Kind: "creditor_account", ThreatClass: threatClass,
		Reporter: reporter, LegalEntity: legalEntityOf(reporter), Status: StatusActive,
		Confidence: confidence, CaseID: caseID, CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
	entry.EntryID = entryID(reporter, token, caseID)

	chain := r.chainFor(reporter)
	seq, prevHash, hash, err := chain.Next(canonicalRecord(entry, OpReport))
	if err != nil {
		return nil, err
	}
	entry.ChainSeq, entry.PrevHash, entry.Hash = seq, prevHash, hash

	wireJSON, err := json.Marshal(Wire(entry, OpReport))
	if err != nil {
		return nil, fmt.Errorf("consortium: marshalling wire payload: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO consortium_entries (entry_id, token, epoch, reporter_bank, kind, threat_class, status, confidence, case_id, created_at, expires_at, wire_json)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (entry_id) DO NOTHING`,
		entry.EntryID, entry.Token, entry.Epoch, entry.Reporter, entry.Kind, entry.ThreatClass, string(entry.Status),
		entry.Confidence, entry.CaseID, entry.CreatedAt, entry.ExpiresAt, wireJSON)
	if err != nil {
		return nil, fmt.Errorf("consortium: inserting entry: %w", err)
	}
	return entry, nil
}

// Retract withdraws a report — "effective immediately at all members" (docs/05 §4.2).
func (r *Registry) Retract(ctx context.Context, entryID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE consortium_entries SET status = $1 WHERE entry_id = $2`, string(StatusRetracted), entryID)
	return err
}

// Dispute drops an entry to advisory-only pending resolution (docs/05 §4.2). In this
// architecture consortium findings are ALREADY advisory-only everywhere — dispute mainly
// exists so a disputed entry stops counting toward the >=2-independent-reporters rail.
func (r *Registry) Dispute(ctx context.Context, entryID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE consortium_entries SET status = $1 WHERE entry_id = $2`, string(StatusDisputed), entryID)
	return err
}

// LookupResult is what a member gets back for a candidate account: whether the >=2
// independent-legal-entity rail fires, plus the raw active entries for display/advisory use.
type LookupResult struct {
	Token        string
	RailFires    bool
	LegalEntities []string
	Entries      []Entry
}

// Lookup tokens `account` under the current epoch and returns every ACTIVE, non-expired
// entry plus whether the independence rail (docs/05 §4.3) fires. reputationFloor filters
// out low-reputation reporters, matching the policy's MinReporterReputation.
func (r *Registry) Lookup(ctx context.Context, account string, minReputation float64, reputationOf func(reporter string) float64) (*LookupResult, error) {
	token, err := r.tokeniser.TokenCurrentEpoch("creditor_account", account)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT entry_id, token, epoch, reporter_bank, kind, threat_class, status, confidence, case_id, created_at, expires_at
		FROM consortium_entries
		WHERE token = $1 AND status = $2 AND expires_at > now()`, token, string(StatusActive))
	if err != nil {
		return nil, fmt.Errorf("consortium: lookup query: %w", err)
	}
	defer rows.Close()

	res := &LookupResult{Token: token}
	seenEntities := map[string]bool{}
	for rows.Next() {
		var e Entry
		var status string
		if err := rows.Scan(&e.EntryID, &e.Token, &e.Epoch, &e.Reporter, &e.Kind, &e.ThreatClass, &status, &e.Confidence, &e.CaseID, &e.CreatedAt, &e.ExpiresAt); err != nil {
			return nil, err
		}
		e.Status = Status(status)
		e.LegalEntity = legalEntityOf(e.Reporter)
		if reputationOf != nil && reputationOf(e.Reporter) < minReputation {
			continue // reputation floor, docs/05 §4.3
		}
		res.Entries = append(res.Entries, e)
		if !seenEntities[e.LegalEntity] {
			seenEntities[e.LegalEntity] = true
			res.LegalEntities = append(res.LegalEntities, e.LegalEntity)
		}
	}
	res.RailFires = len(res.LegalEntities) >= 2 // the collapse is what makes this mean something (F-53)
	return res, rows.Err()
}

func entryID(reporter, token, caseID string) string {
	h := sha256.Sum256([]byte(reporter + "|" + token + "|" + caseID + "|" + time.Now().String()))
	return hex.EncodeToString(h[:16])
}

func canonicalRecord(e *Entry, op Op) audit.CanonicalRecord {
	// Reuse audit.CanonicalRecord's shape loosely — consortium entries aren't decisions,
	// but the same "hash the meaningful fields, chain by reporter" mechanism applies
	// (docs/05 §4.4). Encoding the op and token into the existing fields keeps this to one
	// hashing primitive in the codebase rather than a near-duplicate.
	return audit.CanonicalRecord{
		EndToEndID: string(op) + ":" + e.Token, DecisionSeq: 0,
		AcceptedAtMs: e.CreatedAt.UnixMilli(), DecidedAtMs: e.CreatedAt.UnixMilli(),
		Action: e.ThreatClass, PModel: e.Reporter, PolicyVersion: e.LegalEntity,
		ModelBundleVersion: e.CaseID, RulesVersion: string(e.Status),
	}
}
