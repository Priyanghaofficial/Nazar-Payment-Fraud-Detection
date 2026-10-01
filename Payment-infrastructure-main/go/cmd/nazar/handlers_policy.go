package main

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"nazar/internal/contract"
	"nazar/internal/decide"
	"nazar/internal/persist"
)

// Live policy tuning and counterfactual replay.
//
// The point for an audience: risk policy is not a constant baked into a binary. A risk owner
// can move the cost of holding a payment, or how much a wrongly-blocked customer is worth,
// and the system re-prices every subsequent decision immediately — with the change stamped
// on every decision so the audit trail never claims the approved bundle produced something
// it didn't.
//
// Replay is the honest half. Before anything goes live, the same change is re-priced against
// real recent decisions to show exactly how many would flip and what it costs — because
// "move a slider and see what happens to production" is only responsible if you can see
// what would have happened first.

var tuneCounter atomic.Int64

type tuneRequest struct {
	// All optional; anything omitted keeps its current value.
	HoldFrictionMinor      *int64   `json:"hold_friction_minor"`
	StepUpFrictionMinor    *int64   `json:"step_up_friction_minor"`
	InterstitialFrictionMinor *int64 `json:"interstitial_friction_minor"`
	FalseBlockCostMinor    *int64   `json:"false_block_cost_minor"`
	MarginMinor            *int64   `json:"margin_minor"`
	HoldStopProb           *float64 `json:"hold_stop_prob"`
	HoldAbandonProb        *float64 `json:"hold_abandon_prob"`
	DegradationCapMinor    *int64   `json:"degradation_value_cap_minor"`
	LossGivenFraudUPI      *float64 `json:"loss_given_fraud_upi"`
	Apply                  bool     `json:"apply"` // false = evaluate only, do not go live
	ReplayLimit            int      `json:"replay_limit"`
}

// candidate builds a tuned copy without touching the live bundle.
func (s *Server) candidatePolicy(req tuneRequest) *decide.Policy {
	p := s.engine.LivePolicy().Clone()
	set := func(m map[string]int64, k string, v *int64) {
		if v != nil {
			m[k] = *v
		}
	}
	set(p.Economics.FrictionCostMinor, "hold", req.HoldFrictionMinor)
	set(p.Economics.FrictionCostMinor, "step_up", req.StepUpFrictionMinor)
	set(p.Economics.FrictionCostMinor, "step_up_interstitial", req.InterstitialFrictionMinor)
	set(p.Economics.FrictionCostMinor, "false_block", req.FalseBlockCostMinor)
	if req.MarginMinor != nil {
		p.Economics.MarginMinor = *req.MarginMinor
	}
	if req.HoldStopProb != nil {
		p.Economics.StopProb["hold"] = *req.HoldStopProb
	}
	if req.HoldAbandonProb != nil {
		p.Economics.AbandonProb["hold"] = *req.HoldAbandonProb
	}
	if req.DegradationCapMinor != nil {
		p.Degradation.ValueCapMinor = *req.DegradationCapMinor
	}
	if req.LossGivenFraudUPI != nil {
		p.Economics.LossGivenFraud["UPI"] = *req.LossGivenFraudUPI
	}
	return p
}

type flipRow struct {
	EndToEndID  string `json:"end_to_end_id"`
	AmountMinor int64  `json:"amount_minor"`
	Rail        string `json:"rail"`
	PAdjusted   float64 `json:"p_prevalence_adj"`
	From        string `json:"from"`
	To          string `json:"to"`
	Direction   string `json:"direction"` // stricter | looser
	Debtor      string `json:"debtor_account"`
	Creditor    string `json:"creditor_account"`
}

// POST /v1/policy/tune — evaluate a candidate policy against real recent decisions, and
// optionally make it live.
func (s *Server) handlePolicyTune(w http.ResponseWriter, r *http.Request) {
	var req tuneRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit := req.ReplayLimit
	if limit <= 0 || limit > 1000 {
		limit = 300
	}

	candidate := s.candidatePolicy(req)
	rows, err := persist.GetReplayRows(r.Context(), s.db, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Two engines over the same decisions: today's policy and the candidate. Only the policy
	// differs, so every difference in outcome is attributable to the change.
	current := *s.engine
	current.Live = nil
	current.Policy = s.engine.LivePolicy()

	proposed := *s.engine
	proposed.Live = nil
	proposed.Policy = candidate

	var flips []flipRow
	var stricter, looser int
	var valueNewlyChallenged, valueNewlyReleased int64
	var frictionDelta int64

	for _, row := range rows {
		before, _, costBefore := current.EvaluateCost(row.PAdjusted, row.Rail, row.AmountMinor)
		after, _, costAfter := proposed.EvaluateCost(row.PAdjusted, row.Rail, row.AmountMinor)
		before = decide.RaiseTo(before, row.RailFloor)
		after = decide.RaiseTo(after, row.RailFloor)
		frictionDelta += costAfter - costBefore
		if before == after {
			continue
		}
		dir := "looser"
		if contract.LadderIndex(after) > contract.LadderIndex(before) {
			dir = "stricter"
			stricter++
			if isChallenge(after) && !isChallenge(before) {
				valueNewlyChallenged += row.AmountMinor
			}
		} else {
			looser++
			if isChallenge(before) && !isChallenge(after) {
				valueNewlyReleased += row.AmountMinor
			}
		}
		if len(flips) < 100 {
			flips = append(flips, flipRow{
				EndToEndID: row.EndToEndID, AmountMinor: row.AmountMinor, Rail: string(row.Rail),
				PAdjusted: row.PAdjusted, From: string(before), To: string(after), Direction: dir,
				Debtor: row.Debtor, Creditor: row.Creditor,
			})
		}
	}

	applied := false
	var newVersion string
	if req.Apply {
		n := tuneCounter.Add(1)
		candidate.Version = fmt.Sprintf("%s+tuned.%d", s.basePolicy.Version, n)
		s.policyRef.Store(candidate)
		newVersion = candidate.Version
		applied = true
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"evaluated_against":       len(rows),
		"flips":                   flips,
		"flips_total":             stricter + looser,
		"stricter":                stricter,
		"looser":                  looser,
		"value_newly_challenged_minor": valueNewlyChallenged,
		"value_newly_released_minor":   valueNewlyReleased,
		"expected_cost_delta_minor":    frictionDelta,
		"applied":                 applied,
		"policy_version":          newVersion,
		"candidate":               candidate,
		"note": "Replayed against real persisted decisions. Only the policy differs between the two runs — the model probability, the amount and the rails that fired are the ones actually recorded at decision time, so every flip above is attributable to this change alone.",
	})
}

func isChallenge(a contract.Action) bool {
	return a != contract.ActionAllow && a != contract.ActionAllowMonitor
}

// POST /v1/policy/reset — return to the approved on-disk bundle.
func (s *Server) handlePolicyReset(w http.ResponseWriter, r *http.Request) {
	s.policyRef.Store(s.basePolicy)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "reset", "policy_version": s.basePolicy.Version,
		"note": "Back to the approved, four-eyes-signed bundle on disk.",
	})
}

// GET /v1/policy/live — what is actually in force right now, and whether it is the approved
// bundle or a live-tuned derivative.
func (s *Server) handlePolicyLive(w http.ResponseWriter, r *http.Request) {
	live := s.engine.LivePolicy()
	writeJSON(w, http.StatusOK, map[string]any{
		"policy":          live,
		"base_version":    s.basePolicy.Version,
		"is_tuned":        live.Version != s.basePolicy.Version,
		"approved_by":     s.basePolicy.ApprovedBy,
	})
}
