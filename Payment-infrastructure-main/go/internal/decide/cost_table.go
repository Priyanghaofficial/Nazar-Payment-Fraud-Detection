package decide

import (
	"math"
	"sort"

	"nazar/internal/contract"
)

// This file makes stage 4 (expected-cost minimisation, docs/04 §2) inspectable rather than
// just executable. The judge-facing question "how did you decide this was fraud?" has a
// precise answer for this system — it did not "decide it was fraud", it chose the action
// with the lowest expected cost — and that is only convincing if the whole cost table and
// the probability at which each action takes over can be shown, per transaction.
//
// minimiseCost is defined in terms of CostTable so the number the console displays and the
// number the live path acts on cannot drift apart.

// ActionCost is one row of the expected-cost table: every term, not just the total.
type ActionCost struct {
	Action contract.Action `json:"action"`
	// ExpectedFraudLossMinor = p * amount * loss_given_fraud * (1 - stop_prob)
	ExpectedFraudLossMinor int64 `json:"expected_fraud_loss_minor"`
	// FrictionMinor is the operational cost of applying this action at all.
	FrictionMinor int64 `json:"friction_minor"`
	// LostBusinessMinor = (1 - p) * abandon_prob * margin — the cost of annoying a good customer.
	LostBusinessMinor int64 `json:"lost_business_minor"`
	TotalCostMinor    int64 `json:"total_cost_minor"`
	Chosen            bool  `json:"chosen"`
}

// ActionThreshold is the fraud probability at or above which an action becomes the
// cost-minimising choice, for one specific (rail, amount). These are not configured
// thresholds — they fall out of the economics, which is the point.
type ActionThreshold struct {
	Action contract.Action `json:"action"`
	MinP   float64         `json:"min_p"`
}

var costCandidates = []contract.Action{
	contract.ActionAllow,
	contract.ActionStepUp,
	contract.ActionStepUpInterstitial,
	contract.ActionHold,
}

// CostTable computes the full expected-cost breakdown for every candidate action under this
// engine's live policy.
func (e *Engine) CostTable(pFraud float64, rail contract.Rail, amountMinor int64) []ActionCost {
	e = e.snapshot()
	lgf := e.Policy.Economics.LossGivenFraud[string(rail)]
	amount := float64(amountMinor)

	rows := make([]ActionCost, 0, len(costCandidates))
	bestCost := math.MaxFloat64
	bestIdx := 0
	for i, a := range costCandidates {
		key := economicsKey(a)
		stop := e.Policy.Economics.StopProb[key]
		friction := float64(e.Policy.Economics.FrictionCostMinor[key])
		abandon := e.Policy.Economics.AbandonProb[key]

		fraudLoss := pFraud * amount * lgf * (1 - stop)
		lostBusiness := (1 - pFraud) * abandon * float64(e.Policy.Economics.MarginMinor)
		total := fraudLoss + friction + lostBusiness

		rows = append(rows, ActionCost{
			Action:                 a,
			ExpectedFraudLossMinor: int64(fraudLoss),
			FrictionMinor:          int64(friction),
			LostBusinessMinor:      int64(lostBusiness),
			TotalCostMinor:         int64(total),
		})
		if total < bestCost {
			bestCost = total
			bestIdx = i
		}
	}
	rows[bestIdx].Chosen = true
	return rows
}

// minimiseCost picks the cost-minimising action. It reads the same table the console shows,
// so an explanation can never describe a computation the engine did not perform.
func (e *Engine) minimiseCost(pFraud float64, rail contract.Rail, amountMinor int64) (contract.Action, *int64, *int64) {
	rows := e.CostTable(pFraud, rail, amountMinor)
	bestAction := contract.ActionAllow
	var bestLoss, bestCost int64
	for _, r := range rows {
		if r.Chosen {
			bestAction, bestLoss, bestCost = r.Action, r.ExpectedFraudLossMinor, r.TotalCostMinor
			break
		}
	}

	// ALLOW_MONITOR: operationally distinct from ALLOW (flagged for async review) but
	// cost-identical, so it's a tie-break rather than a cost-argmin outcome.
	const monitorThreshold = 0.01
	if bestAction == contract.ActionAllow && pFraud >= monitorThreshold {
		bestAction = contract.ActionAllowMonitor
	}
	return bestAction, &bestLoss, &bestCost
}

// ActionThresholds finds, for one (rail, amount), the probability at which each action first
// becomes cost-minimal. Computed by bisection on the real cost function rather than read
// from a config value — if the economics change, these move, which is exactly what the
// Policy Studio's live tuning demonstrates.
func (e *Engine) ActionThresholds(rail contract.Rail, amountMinor int64) []ActionThreshold {
	e = e.snapshot()
	at := func(p float64) contract.Action {
		for _, r := range e.CostTable(p, rail, amountMinor) {
			if r.Chosen {
				return r.Action
			}
		}
		return contract.ActionAllow
	}

	seen := map[contract.Action]float64{at(0): 0}
	prev := at(0)
	// Coarse scan to locate each transition, then bisect it to ~1e-6 precision.
	const steps = 400
	for i := 1; i <= steps; i++ {
		p := float64(i) / steps
		cur := at(p)
		if cur == prev {
			continue
		}
		lo, hi := float64(i-1)/steps, p
		for j := 0; j < 40; j++ {
			mid := (lo + hi) / 2
			if at(mid) == prev {
				lo = mid
			} else {
				hi = mid
			}
		}
		if _, ok := seen[cur]; !ok {
			seen[cur] = hi
		}
		prev = cur
	}

	out := make([]ActionThreshold, 0, len(seen))
	for a, p := range seen {
		out = append(out, ActionThreshold{Action: a, MinP: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MinP < out[j].MinP })
	return out
}

// BreakEvenAmount answers "at this same probability, how large would the payment have to be
// before we'd escalate to `target`?" — the counterfactual a customer actually asks. Returns
// (amount, true) when a crossover exists inside the search range.
func (e *Engine) BreakEvenAmount(pFraud float64, rail contract.Rail, target contract.Action) (int64, bool) {
	e = e.snapshot()
	at := func(amt int64) contract.Action {
		for _, r := range e.CostTable(pFraud, rail, amt) {
			if r.Chosen {
				return r.Action
			}
		}
		return contract.ActionAllow
	}
	const maxAmount = int64(100_000_000) // Rs 10 lakh in paise — beyond any per-txn rail limit
	if contract.LadderIndex(at(maxAmount)) < contract.LadderIndex(target) {
		return 0, false // never reaches the target action at this probability
	}
	lo, hi := int64(0), maxAmount
	for lo < hi {
		mid := lo + (hi-lo)/2
		if contract.LadderIndex(at(mid)) >= contract.LadderIndex(target) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, true
}
