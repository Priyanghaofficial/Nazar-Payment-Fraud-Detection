// Alert management: problem_statement.txt's "Alert management" capability. Any LIVE decision
// that lands somewhere other than the two free-pass rungs (ALLOW / ALLOW_MONITOR) raises a
// real, persisted alert — not a UI-only badge — that a real queue (GET /v1/alerts) can list
// and a real action (POST /v1/alerts/{id}/resolve) can close out.
package persist

import (
	"context"
	"database/sql"
	"fmt"

	"nazar/internal/contract"
)

// Severity is a direct, honest function of the ladder rung — no separate scoring pass.
func Severity(action contract.Action) string {
	switch action {
	case contract.ActionBlock:
		return "critical"
	case contract.ActionHold:
		return "high"
	case contract.ActionCap:
		return "medium"
	case contract.ActionStepUpInterstitial, contract.ActionStepUp:
		return "low"
	default:
		return "low"
	}
}

// ShouldAlert is true for every action except the two that mean "nothing to look at" —
// the ALLOW rungs. This is the entire alerting rule at P0: real, not a fabricated ML layer
// bolted on top of the decision the engine already made.
func ShouldAlert(action contract.Action) bool {
	return action != contract.ActionAllow && action != contract.ActionAllowMonitor
}

// EmitAlert writes one alert row keyed to the exact persisted decision (same three-column
// key the alerts table's FK constraint enforces), so an alert can never dangle without the
// decision it's about.
func EmitAlert(ctx context.Context, db *sql.DB, d *contract.Decision) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO alerts (end_to_end_id, decision_seq, decided_at, raised_at, severity, status)
		VALUES ($1, $2, to_timestamp($3/1000.0), to_timestamp($3/1000.0), $4, 'open')
		ON CONFLICT DO NOTHING`,
		d.EndToEndID, d.DecisionSeq, d.DecidedAtMs, Severity(d.Action),
	)
	if err != nil {
		return fmt.Errorf("persist: insert alert: %w", err)
	}
	return nil
}

// AlertRow is one queue entry: the alert plus enough decision/transaction context to triage
// without a second round trip.
type AlertRow struct {
	ID              int64   `json:"id"`
	EndToEndID      string  `json:"end_to_end_id"`
	DecidedAtMs     int64   `json:"decided_at_ms"`
	RaisedAtMs      int64   `json:"raised_at_ms"`
	Severity        string  `json:"severity"`
	Status          string  `json:"status"`
	ResolvedAtMs    *int64  `json:"resolved_at_ms"`
	Action          string  `json:"action"`
	DebtorAccount   string  `json:"debtor_account"`
	CreditorAccount string  `json:"creditor_account"`
	AmountMinor     int64   `json:"amount_minor"`
	Rail            string  `json:"rail"`
}

// GetAlerts lists real alert rows, newest first. status is "open", "resolved", or "" (all).
func GetAlerts(ctx context.Context, db *sql.DB, status string, limit int) ([]AlertRow, error) {
	query := `
		SELECT a.id, a.end_to_end_id, extract(epoch from a.decided_at)*1000,
		       extract(epoch from a.raised_at)*1000, a.severity, a.status,
		       extract(epoch from a.resolved_at)*1000,
		       d.action, t.debtor_account, t.creditor_account, t.amount_minor, t.rail
		FROM alerts a
		JOIN decisions d ON d.end_to_end_id = a.end_to_end_id AND d.decision_seq = a.decision_seq AND d.decided_at = a.decided_at
		JOIN transactions t ON t.end_to_end_id = a.end_to_end_id`
	args := []any{}
	if status != "" {
		query += ` WHERE a.status = $1`
		args = append(args, status)
	}
	query += fmt.Sprintf(` ORDER BY a.raised_at DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("persist: query alerts: %w", err)
	}
	defer rows.Close()

	var out []AlertRow
	for rows.Next() {
		var r AlertRow
		var decidedAtMs, raisedAtMs float64
		var resolvedAtMs sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.EndToEndID, &decidedAtMs, &raisedAtMs, &r.Severity, &r.Status,
			&resolvedAtMs, &r.Action, &r.DebtorAccount, &r.CreditorAccount, &r.AmountMinor, &r.Rail); err != nil {
			return nil, fmt.Errorf("persist: scan alert: %w", err)
		}
		r.DecidedAtMs = int64(decidedAtMs)
		r.RaisedAtMs = int64(raisedAtMs)
		if resolvedAtMs.Valid {
			ms := int64(resolvedAtMs.Float64)
			r.ResolvedAtMs = &ms
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountOpenAlerts backs the sidebar's live open-alert badge.
func CountOpenAlerts(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM alerts WHERE status = 'open'`).Scan(&n)
	return n, err
}

// ResolveAlert closes an alert for real — a persisted state change, not a client-side hide.
func ResolveAlert(ctx context.Context, db *sql.DB, id int64, resolvedBy string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE alerts SET status = 'resolved', resolved_at = now(), resolved_by = $2
		WHERE id = $1 AND status = 'open'`, id, resolvedBy)
	if err != nil {
		return fmt.Errorf("persist: resolve alert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
