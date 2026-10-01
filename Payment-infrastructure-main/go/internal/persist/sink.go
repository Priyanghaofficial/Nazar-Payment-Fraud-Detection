// Package persist implements the DecisionSink seam against Postgres (docs/02 §7) and the
// async shipper that drains the WAL into it (docs/01 §6.1). A second DecisionSink
// implementation — FakeSink — lives alongside it for tests (docs/00 §3.2's ">=2
// implementations" rule).
package persist

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"nazar/internal/contract"
)

type PostgresSink struct {
	db *sql.DB
}

func NewPostgresSink(db *sql.DB) *PostgresSink {
	return &PostgresSink{db: db}
}

func (s *PostgresSink) Emit(ctx context.Context, d *contract.Decision) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("persist: begin tx: %w", err)
	}
	defer tx.Rollback()

	featuresJSON, _ := json.Marshal(d.Features.JSONSafeValues())
	statusJSON, _ := json.Marshal(d.Features.Status)
	stalenessJSON, _ := json.Marshal(d.Features.Staleness)
	contribsJSON, _ := json.Marshal(d.Contributions)
	findingsJSON, _ := json.Marshal(d.Findings)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO decisions (
			end_to_end_id, decision_seq, kind, decided_at, accepted_at,
			action, pre_advisory_action, rail_fired, reason_codes,
			p_model, p_prevalence_adj, expected_loss_minor, expected_cost_minor,
			features, feature_status, feature_staleness, contributions, findings,
			model_bundle_version, policy_version, rules_version, signal_registry_version,
			is_control, action_propensity, degraded,
			total_ms, queue_delay_ms, service_ms,
			decision_shard, chain_seq, prev_hash, hash
		) VALUES (
			$1,$2,$3, to_timestamp($4/1000.0), to_timestamp($5/1000.0),
			$6,$7,$8,$9,
			$10,$11,$12,$13,
			$14,$15,$16,$17,$18,
			$19,$20,$21,$22,
			$23,$24,$25,
			$26,$27,$28,
			$29,$30,$31,$32
		) ON CONFLICT (end_to_end_id, decision_seq, decided_at) DO NOTHING`,
		d.EndToEndID, d.DecisionSeq, string(d.Kind), d.DecidedAtMs, d.AcceptedAtMs,
		string(d.Action), string(d.PreAdvisoryAction), d.RailFired, pqStringArray(d.ReasonCodes),
		d.PModel, d.PPrevalenceAdj, d.ExpectedLossMinor, d.ExpectedCostMinor,
		featuresJSON, statusJSON, stalenessJSON, contribsJSON, findingsJSON,
		d.ModelBundleVersion, d.PolicyVersion, d.RulesVersion, d.SignalRegistryVersion,
		d.IsControl, d.ActionPropensity, pqStringArray(d.Degraded),
		d.TotalMs, d.QueueDelayMs, d.ServiceMs,
		d.DecisionShard, d.ChainSeq, d.PrevHash, d.Hash,
	)
	if err != nil {
		return fmt.Errorf("persist: insert decision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Alert emission happens strictly after the decision row is durably committed — the
	// alerts table's FK requires the decision to already exist, and this is the one place
	// that's guaranteed true without a race against the async shipper.
	if ShouldAlert(d.Action) {
		if err := EmitAlert(ctx, s.db, d); err != nil {
			log.Printf("persist: alert emission failed for %s (decision itself is safely persisted): %v", d.EndToEndID, err)
		}
	}
	return nil
}

// EmitTransaction writes the canonical event to the transactions table (docs/02 §7).
// remittance_info is hashed, never stored raw (B5, DPDP §8).
func EmitTransaction(ctx context.Context, db *sql.DB, ev *contract.Event) error {
	var remittanceHash []byte
	if ev.RemittanceInfo != "" {
		h := sha256.Sum256([]byte(ev.RemittanceInfo))
		remittanceHash = h[:]
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO transactions (
			end_to_end_id, accepted_at, event_ts, rail, channel, bank_instance,
			debtor_account, creditor_account, creditor_vpa, creditor_ifsc,
			amount_minor, currency, device_id, ip, asn, geo_cell, session_id,
			initiation, remittance_hash, schema_version
		) VALUES (
			$1, to_timestamp($2/1000.0), to_timestamp($3/1000.0), $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, NULLIF($14,'')::inet, NULLIF($15,0), $16, $17,
			$18, $19, $20
		) ON CONFLICT (end_to_end_id, accepted_at) DO NOTHING`,
		ev.EndToEndID, ev.AcceptedAtMs, ev.EventTsMs, string(ev.Rail), ev.Channel, ev.BankInstance,
		ev.DebtorAccount, ev.CreditorAccount, nullIfEmpty(ev.CreditorVPA), nullIfEmpty(ev.CreditorIFSC),
		ev.InstructedAmountMinor, ev.Currency, nullIfEmpty(ev.DeviceID), ev.IP, ev.ASN, nullIfEmpty(ev.GeoCell), nullIfEmpty(ev.SessionID),
		nullIfEmpty(ev.Initiation), remittanceHash, ev.SchemaVersion,
	)
	if err != nil {
		return fmt.Errorf("persist: insert transaction: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func pqStringArray(ss []string) string {
	if len(ss) == 0 {
		return "{}"
	}
	out := "{"
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += `"` + s + `"`
	}
	return out + "}"
}
