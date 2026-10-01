package persist

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"nazar/internal/contract"
)

// GetEvent reads the canonical event back as the typed struct the engine works with, for
// callers that need to re-run policy arithmetic against it (explanation, counterfactuals,
// replay) rather than just render it.
func GetEvent(ctx context.Context, db *sql.DB, e2eID string) (*contract.Event, error) {
	row := db.QueryRowContext(ctx, `
		SELECT end_to_end_id, extract(epoch from accepted_at)*1000, rail, channel, bank_instance,
		       debtor_account, creditor_account, coalesce(creditor_vpa,''), amount_minor, currency,
		       coalesce(device_id,''), coalesce(geo_cell,''), coalesce(initiation,'')
		FROM transactions WHERE end_to_end_id = $1 ORDER BY accepted_at DESC LIMIT 1`, e2eID)

	var ev contract.Event
	var acceptedAtMs float64
	var rail, channel string
	if err := row.Scan(&ev.EndToEndID, &acceptedAtMs, &rail, &channel, &ev.BankInstance,
		&ev.DebtorAccount, &ev.CreditorAccount, &ev.CreditorVPA, &ev.InstructedAmountMinor,
		&ev.Currency, &ev.DeviceID, &ev.GeoCell, &ev.Initiation); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("persist: query event: %w", err)
	}
	ev.AcceptedAtMs = int64(acceptedAtMs)
	ev.Rail = contract.Rail(rail)
	ev.Channel = channel
	return &ev, nil
}

// ReplayRow is one historical decision reduced to exactly what a policy counterfactual
// needs: the probability the model actually produced, the money at stake, and which policy
// rails actually fired. Re-pricing these under a candidate policy is honest because none of
// those three inputs depend on the policy being changed.
type ReplayRow struct {
	EndToEndID  string          `json:"end_to_end_id"`
	DecidedAtMs int64           `json:"decided_at_ms"`
	Action      contract.Action `json:"action"`
	PAdjusted   float64         `json:"p_prevalence_adj"`
	AmountMinor int64           `json:"amount_minor"`
	Rail        contract.Rail   `json:"rail"`
	RailFloor   contract.Action `json:"rail_floor"`
	Debtor      string          `json:"debtor_account"`
	Creditor    string          `json:"creditor_account"`
}

// GetReplayRows returns recent scored decisions for policy counterfactuals. Decisions with
// no model probability (blocklist hits, regulatory caps, control group, trusted-pair fast
// path) are excluded: re-pricing them would be meaningless, because economics never chose
// their action in the first place.
func GetReplayRows(ctx context.Context, db *sql.DB, limit int) ([]ReplayRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT d.end_to_end_id, extract(epoch from d.decided_at)*1000, d.action,
		       d.p_prevalence_adj, t.amount_minor, t.rail, d.findings,
		       t.debtor_account, t.creditor_account
		FROM decisions d
		JOIN transactions t ON t.end_to_end_id = d.end_to_end_id
		WHERE d.kind = 'LIVE' AND d.p_prevalence_adj IS NOT NULL
		ORDER BY d.decided_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("persist: query replay rows: %w", err)
	}
	defer rows.Close()

	var out []ReplayRow
	for rows.Next() {
		var r ReplayRow
		var decidedAtMs float64
		var action, rail string
		var findingsJSON []byte
		if err := rows.Scan(&r.EndToEndID, &decidedAtMs, &action, &r.PAdjusted,
			&r.AmountMinor, &rail, &findingsJSON, &r.Debtor, &r.Creditor); err != nil {
			return nil, fmt.Errorf("persist: scan replay row: %w", err)
		}
		r.DecidedAtMs = int64(decidedAtMs)
		r.Action = contract.Action(action)
		r.Rail = contract.Rail(rail)

		// Recover the policy-rail floor that applied, from the findings actually persisted.
		var findings []contract.Finding
		if json.Unmarshal(findingsJSON, &findings) == nil {
			for _, f := range findings {
				if f.Status != contract.StatusFired {
					continue
				}
				switch f.SignalID {
				case "rule:RAIL-101", "rule:RAIL-102":
					if contract.LadderIndex(contract.ActionStepUpInterstitial) > contract.LadderIndex(r.RailFloor) {
						r.RailFloor = contract.ActionStepUpInterstitial
					}
				}
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
