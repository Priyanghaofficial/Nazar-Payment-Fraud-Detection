package decide

import (
	"context"
	"fmt"
	"hash/fnv"

	"nazar/internal/contract"
	"nazar/internal/rules"
)

// Engine implements the six-stage decision sequence (docs/04 §1). Everything it needs is
// injected — it performs no I/O itself, which is what test_no_io_after_profile_load checks
// against the whole call graph from Decide() down.
type Engine struct {
	Policy      *Policy
	Rules       *rules.Engine
	Scorer      contract.Scorer
	Calibrator  contract.Calibrator
	Prevalence  *PrevalenceCorrector
	Blocklist   *Blocklist

	// Live is an optional hot-swap slot (live_policy.go). When set it overrides Policy for
	// the duration of one decision. Nil in every test — the struct literal in
	// test/invariants/testengine.go keeps working unchanged.
	Live *PolicyRef

	ModelBundleVersion    string
	PolicyVersion         string
	RulesVersion          string
	SignalRegistryVersion string
}

// snapshot returns an Engine whose Policy is pinned for the lifetime of one decision. The
// live policy is read exactly once here, so a swap landing mid-decision can never split a
// single decision across two policies — and the policy_version stamped on the result is
// always the one that actually produced it.
func (e *Engine) snapshot() *Engine {
	live := e.Live.Load()
	if live == nil || live == e.Policy {
		return e
	}
	cp := *e
	cp.Policy = live
	cp.PolicyVersion = live.Version
	return &cp
}

// EvaluateCost exposes the expected-cost minimisation (stage 4) for counterfactual replay:
// given a fraud probability, rail and amount, which action is cost-minimal under this
// engine's current policy, and what are the expected loss and total cost. Replay calls the
// exact function the live path uses — it is not a reimplementation that can drift.
func (e *Engine) EvaluateCost(pFraud float64, rail contract.Rail, amountMinor int64) (contract.Action, int64, int64) {
	action, loss, cost := e.snapshot().minimiseCost(pFraud, rail, amountMinor)
	return action, *loss, *cost
}

// RaiseTo returns whichever of the two actions sits higher on the ladder. Policy rails may
// only ever raise friction (D7); replay needs the same floor logic the live path uses.
func RaiseTo(action, floor contract.Action) contract.Action {
	if contract.LadderIndex(floor) > contract.LadderIndex(action) {
		return floor
	}
	return action
}

// LivePolicy returns the policy currently in force — the hot-swapped one if present,
// otherwise the statically configured bundle.
func (e *Engine) LivePolicy() *Policy {
	if live := e.Live.Load(); live != nil {
		return live
	}
	return e.Policy
}

// Input bundles everything Decide needs beyond the engine's own configuration.
type Input struct {
	Event    *contract.Event
	Profile  *contract.ProfileBundle
	Features *contract.FeatureVector
	Degraded []string // dependency names already known-down (docs/00 §8)
	Graph    *contract.GraphResult
	Advisories []Advisory // from consortium, already fetched — the engine does not fetch
}

// Advisory is an admissibility-checked candidate from the consortium lane (docs/04 §5b).
type Advisory struct {
	SignalID           string
	SignatureValid     bool
	ReporterReputation float64
	AgeHours           float64
	Confidence         float64
	Steps              int // how many ladder rungs this advisory would add if admissible
	Explanation        string
}

// Decide runs the full six-stage sequence and returns the decision plus every finding
// produced along the way (rule findings, the model finding, and NOT_EVALUATED placeholders
// for any signal that did not run — D5).
func (e *Engine) Decide(ctx context.Context, in *Input) (*contract.Decision, []contract.Finding) {
	e = e.snapshot() // pin the policy for this decision (live_policy.go)
	ev, pb, fv := in.Event, in.Profile, in.Features
	var findings []contract.Finding
	var reasonCodes []string

	isControl, propensity := e.controlAssignment(ev.EndToEndID)

	// ── Stage 1: local filters + regulatory rails ──────────────────────────────
	if entry, hit := e.Blocklist.Hit(ev.CreditorAccount); hit {
		findings = append(findings, contract.NewFinding("local_blocklist", contract.StatusFired,
			fmt.Sprintf("creditor account is on the %s blocklist: %s", entry.List, entry.Reason)))
		reasonCodes = append(reasonCodes, "LOCAL_BLOCKLIST_HIT")
		d := e.buildDecision(ev, pb, fv, contract.ActionBlock, contract.ActionBlock, "local_blocklist",
			reasonCodes, nil, nil, in.Degraded, isControl, propensity, findings)
		return d, findings
	}

	ruleResults := e.Rules.Evaluate(ev, pb, fv)
	for _, r := range ruleResults {
		status := contract.StatusClear
		if r.Fired {
			status = contract.StatusFired
		}
		findings = append(findings, contract.NewFinding("rule:"+r.RuleID, status, ruleExplanation(r)))
		fv.Set("rf_"+r.RuleID, boolToF(r.Fired)) // docs/02 §4.5: every predicate is ALSO a feature
	}

	// Regulatory CAP short-circuits everything below it (docs/04 §5: "CAP is off-ladder").
	for _, r := range ruleResults {
		if r.Class == rules.ClassRegulatory && r.Fired && r.Action == rules.ActionCap {
			reasonCodes = append(reasonCodes, "REGULATORY_CAP:"+r.RuleID)
			d := e.buildDecision(ev, pb, fv, contract.ActionCap, contract.ActionCap, r.RuleID,
				reasonCodes, nil, nil, in.Degraded, isControl, propensity, findings)
			return d, findings
		}
	}

	if isControl {
		reasonCodes = append(reasonCodes, "CONTROL_GROUP")
		d := e.buildDecision(ev, pb, fv, contract.ActionAllow, contract.ActionAllow, "",
			reasonCodes, nil, nil, in.Degraded, isControl, propensity, findings)
		return d, findings
	}

	ringFlagged := in.Graph != nil && in.Graph.Evaluated && in.Graph.RingScore >= 0.5

	// ── Stage 2: trusted-pair fast path (docs/04 §4) ────────────────────────────
	if e.trustedPair(pb, ev, ringFlagged, in.Degraded) {
		reasonCodes = append(reasonCodes, "TRUSTED_PAIR_FAST_PATH")
		findings = append(findings, contract.NewFinding("trusted_pair", contract.StatusFired,
			"payer/payee pair has sufficient shared history and this transaction is within its usual amount range; fast-pathed to ALLOW"))
		action := e.applyPolicyRails(contract.ActionAllow, ruleResults, &reasonCodes)
		d := e.buildDecision(ev, pb, fv, action, action, "", reasonCodes, nil, nil, in.Degraded, isControl, propensity, findings)
		return d, findings
	}

	// ── Stage 3: score ───────────────────────────────────────────────────────
	raw, contribs, err := e.Scorer.Score(fv)
	var pModel, pAdjusted *float64
	if err != nil {
		findings = append(findings, contract.NewFinding("model", contract.StatusNotEvaluated,
			fmt.Sprintf("scorer error, proceeding on rules only: %v", err)))
	} else {
		calibrated := e.Calibrator.Calibrate(raw)
		adjusted := e.Prevalence.Adjust(calibrated)
		pModel = &calibrated
		pAdjusted = &adjusted
		findings = append(findings, contract.NewFinding("model", contract.StatusFired,
			fmt.Sprintf("calibrated probability %.4f (prevalence-adjusted %.4f) from %s", calibrated, adjusted, e.ModelBundleVersion)))
	}

	// ── Stage 4: expected-cost minimisation (docs/04 §2) ────────────────────────
	baseAction := contract.ActionAllow
	var expLoss, expCost *int64
	if pAdjusted != nil {
		baseAction, expLoss, expCost = e.minimiseCost(*pAdjusted, ev.Rail, ev.InstructedAmountMinor)
	}

	// ── Stage 5: policy rails (may only raise friction — D7) ────────────────────
	preAdvisory := e.applyPolicyRails(baseAction, ruleResults, &reasonCodes)
	if ringFlagged && in.Graph != nil {
		findings = append(findings, contract.NewFinding("graph", contract.StatusFired,
			fmt.Sprintf("ring score %.2f across %d entities — advisory only, never a block (docs/00 §D7)", in.Graph.RingScore, in.Graph.RingSize)))
	} else if in.Graph != nil && in.Graph.Evaluated {
		findings = append(findings, contract.NewFinding("graph", contract.StatusClear, "no ring pattern detected for this payee"))
	} else {
		findings = append(findings, contract.NewFinding("graph", contract.StatusNotEvaluated, "graph signal did not run for this decision (down, stale, or deadline)"))
	}

	// degradation: cap value, never deny (D7)
	action := preAdvisory
	if len(in.Degraded) > 0 && ev.InstructedAmountMinor > e.Policy.Degradation.ValueCapMinor {
		reasonCodes = append(reasonCodes, "DEGRADED_VALUE_CAP")
		if contract.LadderIndex(action) < contract.LadderIndex(contract.ActionStepUpInterstitial) {
			action = contract.ActionStepUpInterstitial // more friction, never a block (D7)
		}
	}

	// ── Stage 6: advisory attachment (docs/04 §5, capped below HOLD — F-20) ─────
	final := e.attachAdvisories(action, in.Advisories, &reasonCodes, &findings)

	d := e.buildDecision(ev, pb, fv, final, preAdvisory, "", reasonCodes, expLoss, expCost, in.Degraded, isControl, propensity, findings)
	d.PModel = pModel
	d.PPrevalenceAdj = pAdjusted
	d.Contributions = contribs
	return d, findings
}

func (e *Engine) trustedPair(pb *contract.ProfileBundle, ev *contract.Event, ringFlagged bool, degraded []string) bool {
	p := pb.Pair
	if len(degraded) > 0 {
		return false // never fast-path degraded
	}
	if !p.Present || p.TxnCount90d < e.Policy.TrustedPair.MinTxnCount90d {
		return false
	}
	if p.LastDisposition == "FRAUD" {
		return false
	}
	if p.AmtP95Minor <= 0 || float64(ev.InstructedAmountMinor) > float64(p.AmtP95Minor)*e.Policy.TrustedPair.AmountHeadroomRatio {
		return false
	}
	if pb.Device.Present && ev.DeviceID != "" && !pb.Payer.KnownDevices[ev.DeviceID] {
		return false // ATO cannot ride the fast path
	}
	if ringFlagged {
		return false
	}
	if p.LastCreditorAccount != "" && p.LastCreditorAccount != ev.CreditorAccount {
		return false // VPA/account repoint guard
	}
	return true
}

func economicsKey(a contract.Action) string {
	switch a {
	case contract.ActionAllow:
		return "allow"
	case contract.ActionAllowMonitor:
		return "allow_monitor"
	case contract.ActionStepUp:
		return "step_up"
	case contract.ActionStepUpInterstitial:
		return "step_up_interstitial"
	case contract.ActionHold:
		return "hold"
	}
	return "allow"
}

// applyPolicyRails raises the action to at least the highest rung fired by any policy rail
// (docs/04 §3.2). Policy rails may never lower an action and never produce BLOCK — the rule
// bundle enforces the latter structurally (no policy rule specifies action: BLOCK).
func (e *Engine) applyPolicyRails(base contract.Action, ruleResults []rules.Result, reasonCodes *[]string) contract.Action {
	action := base
	for _, r := range ruleResults {
		if r.Class != rules.ClassPolicy || !r.Fired {
			continue
		}
		var rung contract.Action
		switch r.Action {
		case rules.ActionStepUp:
			rung = contract.ActionStepUp
		case rules.ActionStepUpInterstitial:
			rung = contract.ActionStepUpInterstitial
		default:
			continue // ActionNone: rule-feature only, no rail effect
		}
		if contract.LadderIndex(rung) > contract.LadderIndex(action) {
			action = rung
			*reasonCodes = append(*reasonCodes, "POLICY_RAIL:"+r.RuleID)
		}
	}
	return action
}

// attachAdvisories implements docs/04 §5's attachAdvisory, including the two fixes: a real
// admissibility check (F-21) and the advisory_max_rung cap (F-20).
func (e *Engine) attachAdvisories(action contract.Action, advisories []Advisory, reasonCodes *[]string, findings *[]contract.Finding) contract.Action {
	if action == contract.ActionBlock || action == contract.ActionCap {
		return action // off-ladder — fixes F-44
	}
	var admissible []Advisory
	for _, a := range advisories {
		ok := a.SignatureValid &&
			a.ReporterReputation >= e.Policy.Ladder.MinReporterReputation &&
			a.AgeHours <= e.Policy.Ladder.MaxAdvisoryAgeHours &&
			a.Confidence >= e.Policy.Ladder.MinAdvisoryConfidence
		*findings = append(*findings, contract.NewFinding("consortium:"+a.SignalID,
			statusOf(ok), a.Explanation))
		if ok {
			admissible = append(admissible, a)
		}
	}
	if len(admissible) == 0 {
		return action // fail-open, byte-identical (docs/00 §8)
	}
	maxSteps := 0
	for _, a := range admissible {
		if a.Steps > maxSteps {
			maxSteps = a.Steps
		}
	}
	if maxSteps > e.Policy.Ladder.AdvisoryMaxSteps {
		maxSteps = e.Policy.Ladder.AdvisoryMaxSteps
	}
	idx := contract.LadderIndex(action) + maxSteps
	capIdx := contract.LadderIndex(e.Policy.advisoryMaxRungAction())
	if idx > capIdx {
		idx = capIdx // the cap that makes "advisories can never reach HOLD" true — F-20
	}
	if idx <= contract.LadderIndex(action) {
		return action
	}
	*reasonCodes = append(*reasonCodes, "ADVISORY_ESCALATED")
	return contract.Ladder[idx]
}

func statusOf(admissible bool) contract.Status {
	if admissible {
		return contract.StatusFired
	}
	return contract.StatusNotApplicable
}

// controlAssignment implements the unbiased-training-data control group (docs/00 §10,
// D6): a stable fraction of traffic, keyed by end_to_end_id so it's deterministic and
// reproducible, bypasses intervention regardless of score. action_propensity records
// P(this action | policy) for later off-policy evaluation (docs/03 §7.2).
func (e *Engine) controlAssignment(e2eID string) (isControl bool, propensity float64) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(e2eID))
	bucket := float64(h.Sum32()%10000) / 10000.0
	if bucket < e.Policy.ControlGroup.Fraction {
		return true, e.Policy.ControlGroup.Fraction
	}
	return false, 1.0 - e.Policy.ControlGroup.Fraction
}

func boolToF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func ruleExplanation(r rules.Result) string {
	if r.Fired {
		return r.Explanation
	}
	return "did not fire: " + r.Explanation
}

func (e *Engine) buildDecision(ev *contract.Event, pb *contract.ProfileBundle, fv *contract.FeatureVector,
	action, preAdvisory contract.Action, railFired string, reasonCodes []string,
	expLoss, expCost *int64, degraded []string, isControl bool, propensity float64, findings []contract.Finding) *contract.Decision {
	return &contract.Decision{
		Findings:              findings,
		EndToEndID:            ev.EndToEndID,
		Kind:                  contract.KindLive,
		AcceptedAtMs:          ev.AcceptedAtMs,
		Action:                action,
		PreAdvisoryAction:     preAdvisory,
		RailFired:             railFired,
		ReasonCodes:           reasonCodes,
		ExpectedLossMinor:     expLoss,
		ExpectedCostMinor:     expCost,
		Features:              fv,
		ModelBundleVersion:    e.ModelBundleVersion,
		PolicyVersion:         e.PolicyVersion,
		RulesVersion:          e.RulesVersion,
		SignalRegistryVersion: e.SignalRegistryVersion,
		IsControl:             isControl,
		ActionPropensity:      propensity,
		Degraded:              degraded,
	}
}
