package explain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"nazar/internal/audit"
	"nazar/internal/contract"
	"nazar/internal/decide"
)

// This file answers two questions the problem statement puts under "fraud explanations", and
// that a sceptical reviewer asks in this order:
//
//   1. Show me the trace — what actually executed, in what order, with what inputs?
//   2. If the same transaction arrived again, would you decide the same thing?
//
// Neither is answered by prose. Reproduce() re-executes the real scorer, the real
// calibrator, the real prevalence correction and the real cost minimisation against the
// feature snapshot persisted at decision time, and compares every intermediate value with
// what was stored. It then recomputes the SHA-256 audit link from the stored predecessor
// hash. A green result means: deterministic, and the stored record has not been edited.
//
// What this deliberately does NOT claim: it does not re-derive the features from Redis. The
// features are the persisted input to this replay, exactly as docs/06's test_replay_is_a_read
// requires — a replay is a read, never a recomputation of state that has since moved on.

// TraceStep is one stage of the decision sequence, as executed.
type TraceStep struct {
	Stage       int               `json:"stage"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Inputs      map[string]string `json:"inputs,omitempty"`
	Output      string            `json:"output"`
	Outcome     string            `json:"outcome"` // executed | short_circuit | skipped | not_evaluated
}

// ReproCheck is one recomputed value compared against what was persisted.
type ReproCheck struct {
	Name       string `json:"name"`
	Stored     string `json:"stored"`
	Recomputed string `json:"recomputed"`
	Match      bool   `json:"match"`
	Note       string `json:"note,omitempty"`
}

// Determinism is the verdict of a full re-execution.
type Determinism struct {
	Reproduced      bool         `json:"reproduced"`
	ChainIntact     bool         `json:"chain_intact"`
	Checks          []ReproCheck `json:"checks"`
	Trace           []TraceStep  `json:"trace"`
	Note            string       `json:"note"`
	ScorerAvailable bool         `json:"scorer_available"`
}

// Reproduce re-executes the deterministic part of the decision from the persisted snapshot.
func Reproduce(eng *decide.Engine, d *contract.Decision, ev *contract.Event) *Determinism {
	det := &Determinism{Reproduced: true}
	if d.Features == nil {
		det.Reproduced = false
		det.Note = "no persisted feature snapshot for this decision — nothing to replay against"
		return det
	}

	fv := cloneFeatures(d.Features)

	// ── Stage 1: local filters + regulatory rails (recovered from the record) ──────
	det.Trace = append(det.Trace, TraceStep{
		Stage: 1, Name: "Local filters & regulatory rails",
		Description: "Confirmed-fraud blocklist, then the regulatory rule class. A hit here short-circuits everything below it — regulation is not negotiable against economics.",
		Output:      railStage1Output(d),
		Outcome:     outcomeFromReasons(d.ReasonCodes, "LOCAL_BLOCKLIST_HIT", "REGULATORY_CAP"),
		Inputs:      map[string]string{"creditor_account": accountOf(ev), "fired_rules": strings.Join(firedRules(d), ", ")},
	})

	// ── Stage 2: trusted-pair fast path ────────────────────────────────────────────
	fast := hasReason(d.ReasonCodes, "TRUSTED_PAIR_FAST_PATH")
	det.Trace = append(det.Trace, TraceStep{
		Stage: 2, Name: "Trusted-pair fast path",
		Description: "An established payer/payee pair paying a usual amount skips full scoring. Never taken when the profile is degraded, when the device is unrecognised, or when the beneficiary is ring-flagged.",
		Output:      map[bool]string{true: "taken — decision short-circuited to allow", false: "not taken — continued to full scoring"}[fast],
		Outcome:     map[bool]string{true: "short_circuit", false: "executed"}[fast],
	})

	// ── Stage 3: score (the actual re-execution) ───────────────────────────────────
	var recomputedAdj *float64
	if eng.Scorer != nil {
		det.ScorerAvailable = true
		raw, contribs, err := eng.Scorer.Score(fv)
		if err != nil {
			det.Checks = append(det.Checks, ReproCheck{Name: "Model score", Stored: fmtP(d.PModel), Recomputed: "error: " + err.Error(), Match: false})
			det.Reproduced = false
		} else {
			cal := eng.Calibrator.Calibrate(raw)
			adj := eng.Prevalence.Adjust(cal)
			recomputedAdj = &adj

			det.Checks = append(det.Checks, ReproCheck{
				Name: "Calibrated fraud probability (p_model)", Stored: fmtP(d.PModel), Recomputed: fmt.Sprintf("%.10f", cal),
				Match: floatMatch(d.PModel, cal),
				Note:  "LightGBM forward pass over the persisted feature vector, then beta calibration",
			})
			det.Checks = append(det.Checks, ReproCheck{
				Name: "Prevalence-corrected probability", Stored: fmtP(d.PPrevalenceAdj), Recomputed: fmt.Sprintf("%.10f", adj),
				Match: floatMatch(d.PPrevalenceAdj, adj),
				Note:  "explicit, versioned prior correction — " + eng.Prevalence.Version,
			})
			det.Checks = append(det.Checks, contributionCheck(d.Contributions, contribs))

			det.Trace = append(det.Trace, TraceStep{
				Stage: 3, Name: "Score",
				Description: "Gradient-boosted model over the feature vector, then beta calibration, then prevalence correction. Pure function of the features — no I/O, which is why it replays exactly.",
				Inputs: map[string]string{
					"features_evaluated": fmt.Sprintf("%d of %d", countClear(d.Features), len(d.Features.Status)),
					"model_bundle":       d.ModelBundleVersion,
				},
				Output:  fmt.Sprintf("raw %.6f → calibrated %.6f → prevalence-adjusted %.6f", raw, cal, adj),
				Outcome: "executed",
			})
		}
	} else {
		det.Trace = append(det.Trace, TraceStep{
			Stage: 3, Name: "Score", Description: "No scorer configured on this instance.",
			Output: "skipped", Outcome: "not_evaluated",
		})
	}

	// ── Stage 4: expected-cost minimisation ────────────────────────────────────────
	if recomputedAdj != nil && ev != nil {
		action, loss, cost := eng.EvaluateCost(*recomputedAdj, ev.Rail, ev.InstructedAmountMinor)
		det.Checks = append(det.Checks, ReproCheck{
			Name: "Expected loss", Stored: fmtI(d.ExpectedLossMinor), Recomputed: fmt.Sprintf("%d", loss),
			Match: intMatch(d.ExpectedLossMinor, loss), Note: "minor units (paise)",
		})
		det.Checks = append(det.Checks, ReproCheck{
			Name: "Expected total cost", Stored: fmtI(d.ExpectedCostMinor), Recomputed: fmt.Sprintf("%d", cost),
			Match: intMatch(d.ExpectedCostMinor, cost), Note: "includes friction and lost-good-business terms",
		})
		det.Trace = append(det.Trace, TraceStep{
			Stage: 4, Name: "Expected-cost minimisation",
			Description: "Each candidate action is priced: expected fraud loss if we let it through, plus the friction we impose, plus the business we lose by annoying a genuine customer. The cheapest wins. There is no risk threshold anywhere in this system.",
			Inputs:      map[string]string{"p_fraud": fmt.Sprintf("%.6f", *recomputedAdj), "amount": rupees(float64(ev.InstructedAmountMinor)), "rail": string(ev.Rail)},
			Output:      fmt.Sprintf("cheapest action = %s at %s expected cost", action, rupees(float64(cost))),
			Outcome:     "executed",
		})

		// ── Stage 5: policy rails may only raise friction ──────────────────────────
		floor := railFloor(d)
		withRails := decide.RaiseTo(action, floor)
		det.Trace = append(det.Trace, TraceStep{
			Stage: 5, Name: "Policy rails",
			Description: "Written policy rules can raise the response but never lower it, and never block — a rule may add friction, it may not deny someone their money.",
			Inputs:      map[string]string{"rail_floor": string(floor), "fired": strings.Join(firedRules(d), ", ")},
			Output:      fmt.Sprintf("%s → %s", action, withRails),
			Outcome:     map[bool]string{true: "executed", false: "skipped"}[floor != ""],
		})

		if !fast && !hasReason(d.ReasonCodes, "LOCAL_BLOCKLIST_HIT") && !hasReason(d.ReasonCodes, "CONTROL_GROUP") &&
			!strings.HasPrefix(strings.Join(d.ReasonCodes, ","), "REGULATORY_CAP") {
			det.Checks = append(det.Checks, ReproCheck{
				Name: "Action before advisories", Stored: string(d.PreAdvisoryAction), Recomputed: string(withRails),
				Match: string(d.PreAdvisoryAction) == string(withRails),
				Note:  "cost-minimal action raised to the policy-rail floor",
			})
		}

		// ── Stage 6: advisory attachment ───────────────────────────────────────────
		det.Trace = append(det.Trace, TraceStep{
			Stage: 6, Name: "Cross-bank advisory attachment",
			Description: "Admissible advisories from other institutions can add at most two rungs and are hard-capped below Hold, so a report we cannot audit can never hold someone's payment.",
			Inputs:      map[string]string{"pre_advisory": string(d.PreAdvisoryAction)},
			Output:      string(d.Action),
			Outcome:     map[bool]string{true: "executed", false: "skipped"}[d.PreAdvisoryAction != d.Action],
		})
	}

	// ── Tamper evidence: recompute the audit link ──────────────────────────────────
	det.Checks = append(det.Checks, chainCheck(d))
	for _, c := range det.Checks {
		if c.Name == "Audit chain link" {
			det.ChainIntact = c.Match
		}
		if !c.Match {
			det.Reproduced = false
		}
	}

	if det.Reproduced {
		det.Note = "Re-executed against the persisted feature snapshot: every intermediate value matched, and the audit link recomputes to the stored hash. Same inputs, same decision."
	} else if det.Note == "" {
		det.Note = "One or more values did not reproduce. Every mismatch is listed above with both values — this is what a real divergence looks like, not a failure to render."
	}
	return det
}

// chainCheck recomputes h_i = SHA256(h_{i-1} || canonical(record)) exactly as wal.Append did
// when the decision was made. Editing any field in the canonical set — the action, the
// probability, the versions, the timestamps — changes this hash and is therefore detectable.
func chainCheck(d *contract.Decision) ReproCheck {
	pModelStr := "nil"
	if d.PModel != nil {
		pModelStr = fmt.Sprintf("%.6f", *d.PModel)
	}
	rec := audit.CanonicalRecord{
		EndToEndID: d.EndToEndID, DecisionSeq: d.DecisionSeq, AcceptedAtMs: d.AcceptedAtMs,
		DecidedAtMs: d.DecidedAtMs, Action: string(d.Action), PModel: pModelStr,
		PolicyVersion: d.PolicyVersion, ModelBundleVersion: d.ModelBundleVersion, RulesVersion: d.RulesVersion,
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		return ReproCheck{Name: "Audit chain link", Stored: shortHash(d.Hash), Recomputed: "error", Match: false}
	}
	h := sha256.New()
	h.Write(d.PrevHash)
	h.Write(payload)
	got := h.Sum(nil)
	return ReproCheck{
		Name:       "Audit chain link",
		Stored:     shortHash(d.Hash),
		Recomputed: shortHash(got),
		Match:      hex.EncodeToString(got) == hex.EncodeToString(d.Hash),
		Note:       fmt.Sprintf("SHA-256 over (previous hash ‖ canonical record) at chain sequence %d", d.ChainSeq),
	}
}

// contributionCheck compares the per-feature attributions. These are recomputed by the same
// single-order ablation the live path uses, so an exact match here means the model, the
// feature order and the missing-value handling are all identical to decision time.
func contributionCheck(stored, got map[string]float64) ReproCheck {
	if len(stored) == 0 {
		return ReproCheck{Name: "Feature attributions", Stored: "none persisted", Recomputed: fmt.Sprintf("%d features", len(got)), Match: true, Note: "nothing stored to compare against"}
	}
	var worst float64
	var worstID string
	matched := 0
	for id, sv := range stored {
		gv, ok := got[id]
		if !ok {
			continue
		}
		if diff := math.Abs(sv - gv); diff > worst {
			worst, worstID = diff, id
		}
		matched++
	}
	ok := worst < 1e-9
	note := fmt.Sprintf("%d attributions compared; largest divergence %.2e", matched, worst)
	if !ok {
		note += " on " + worstID
	}
	return ReproCheck{
		Name: "Feature attributions", Stored: fmt.Sprintf("%d features", len(stored)),
		Recomputed: fmt.Sprintf("%d features", len(got)), Match: ok, Note: note,
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func cloneFeatures(fv *contract.FeatureVector) *contract.FeatureVector {
	out := contract.NewFeatureVector()
	for k, v := range fv.Values {
		out.Values[k] = v
	}
	for k, v := range fv.Status {
		out.Status[k] = v
	}
	for k, v := range fv.Staleness {
		out.Staleness[k] = v
	}
	for k, v := range fv.Reason {
		out.Reason[k] = v
	}
	return out
}

// railFloor recovers the highest rung any fired policy rail demanded, from the persisted
// findings — the rule bundle's own mapping, not a guess.
func railFloor(d *contract.Decision) contract.Action {
	floor := contract.Action("")
	for _, f := range d.Findings {
		if f.Status != contract.StatusFired || !strings.HasPrefix(f.SignalID, "rule:") {
			continue
		}
		var rung contract.Action
		switch strings.TrimPrefix(f.SignalID, "rule:") {
		case "RAIL-101", "RAIL-102":
			rung = contract.ActionStepUpInterstitial
		default:
			continue
		}
		if contract.LadderIndex(rung) > contract.LadderIndex(floor) {
			floor = rung
		}
	}
	return floor
}

func firedRules(d *contract.Decision) []string {
	var out []string
	for _, f := range d.Findings {
		if f.Status == contract.StatusFired && strings.HasPrefix(f.SignalID, "rule:") {
			out = append(out, strings.TrimPrefix(f.SignalID, "rule:"))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

func railStage1Output(d *contract.Decision) string {
	if hasReason(d.ReasonCodes, "LOCAL_BLOCKLIST_HIT") {
		return "blocklist hit — decision short-circuited to block"
	}
	for _, rc := range d.ReasonCodes {
		if strings.HasPrefix(rc, "REGULATORY_CAP") {
			return "regulatory cap applied (" + rc + ") — short-circuited, off-ladder"
		}
	}
	return "no blocklist hit, no regulatory rail fired — continued"
}

func outcomeFromReasons(codes []string, shortCircuit ...string) string {
	for _, sc := range shortCircuit {
		for _, c := range codes {
			if strings.HasPrefix(c, sc) {
				return "short_circuit"
			}
		}
	}
	return "executed"
}

func hasReason(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func accountOf(ev *contract.Event) string {
	if ev == nil {
		return "(event not persisted)"
	}
	return ev.CreditorAccount
}

func countClear(fv *contract.FeatureVector) int {
	n := 0
	for _, st := range fv.Status {
		if st == contract.StatusClear {
			n++
		}
	}
	return n
}

func floatMatch(stored *float64, got float64) bool {
	if stored == nil {
		return false
	}
	return math.Abs(*stored-got) < 1e-9
}

func intMatch(stored *int64, got int64) bool {
	if stored == nil {
		return false
	}
	return *stored == got
}

func fmtP(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%.10f", *p)
}

func fmtI(v *int64) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *v)
}

func shortHash(b []byte) string {
	if len(b) == 0 {
		return "(none)"
	}
	s := hex.EncodeToString(b)
	if len(s) <= 16 {
		return s
	}
	return s[:16] + "…"
}
