// Package audit implements the per-shard hash chain (docs/01 §6.2): h_i = SHA256(h_{i-1}
// || canonical(record)). P0 simplification, stated explicitly in CLAUDE.md's "simplest
// thing that works" table: ONE chain, ONE writer (this single decision process), shard 0 —
// not the sharded-chain-plus-Merkle-anchor design docs/01 describes for multi-replica [P1].
// The property that matters for the demo is real: any edit or deletion of a persisted
// decision breaks the chain from that point forward, and Verify actually recomputes it.
package audit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
)

// CanonicalRecord is the minimal, order-independent payload that gets hashed. It excludes
// the hash fields themselves (obviously) and anything not yet known at chain time.
type CanonicalRecord struct {
	EndToEndID   string `json:"end_to_end_id"`
	DecisionSeq  int    `json:"decision_seq"`
	AcceptedAtMs int64  `json:"accepted_at_ms"`
	DecidedAtMs  int64  `json:"decided_at_ms"`
	Action       string `json:"action"`
	PModel       string `json:"p_model"` // formatted, so float rounding can't shift the hash between runs
	PolicyVersion string `json:"policy_version"`
	ModelBundleVersion string `json:"model_bundle_version"`
	RulesVersion string `json:"rules_version"`
}

// Chain is a single append-only hash chain. Shard is always 0 at P0 (one writer).
type Chain struct {
	mu       sync.Mutex
	Shard    int16
	seq      int64
	prevHash []byte
}

func NewChain(shard int16) *Chain {
	return &Chain{Shard: shard}
}

// Resume restores in-memory chain state after a restart. Without this, a fresh process
// starts a new chain at seq=1/prevHash=nil while Postgres still holds the prior run's tail
// — two "genesis" records end up on the same shard, and verification breaks at the seam.
// Call once at boot with the tip already persisted (or zero values on a genuinely empty chain).
func (c *Chain) Resume(seq int64, lastHash []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq = seq
	c.prevHash = lastHash
}

// Next canonicalises rec, computes the next link, and advances the chain. It returns the
// sequence number, the previous hash (nil for the genesis record), and the new hash — all
// of which get stamped onto the Decision row.
func (c *Chain) Next(rec CanonicalRecord) (seq int64, prevHash, hash []byte, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	payload, err := json.Marshal(rec)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("audit: canonicalising record: %w", err)
	}
	h := sha256.New()
	h.Write(c.prevHash)
	h.Write(payload)
	next := h.Sum(nil)

	prev := c.prevHash
	c.seq++
	c.prevHash = next
	return c.seq, prev, next, nil
}

// Verify recomputes the chain over an ordered slice of records and their claimed hashes,
// returning the index of the first break (-1 if the chain is intact). This is what the
// console's "Verify Chain" button actually calls (docs/07 — no fake verification).
func Verify(records []CanonicalRecord, claimedPrevHash, claimedHash [][]byte) (breakAt int, ok bool) {
	var prev []byte
	for i, rec := range records {
		payload, err := json.Marshal(rec)
		if err != nil {
			return i, false
		}
		h := sha256.New()
		h.Write(prev)
		h.Write(payload)
		computed := h.Sum(nil)

		if !bytesEqual(prev, claimedPrevHash[i]) || !bytesEqual(computed, claimedHash[i]) {
			return i, false
		}
		prev = computed
	}
	return -1, true
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
