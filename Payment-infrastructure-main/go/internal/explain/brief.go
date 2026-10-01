package explain

import (
	"fmt"
	"strings"

	"nazar/internal/narrate"
)

// Brief projects an Explanation down to the whitelist a language model is allowed to see.
//
// This is a *construction* whitelist, not a filter: nothing is copied out of the Event, so
// there is no path by which payer-controlled free text (remittance_info above all) reaches a
// model. Every string below is either a label this package owns, a rule explanation from our
// own versioned bundle, or a number Nazar computed. CLAUDE.md non-negotiable #14.
func (ex *Explanation) Brief(det *Determinism) narrate.Brief {
	b := narrate.Brief{
		Action:      ex.Action,
		ActionLabel: ex.ActionLabel,
		Outcome:     ex.Outcome,
		AmountText:  rupees(float64(ex.AmountMinor)),
		Rail:        ex.Rail,
		RiskBand:    ex.RiskBand,
		Probability: "not scored",
		Degraded:    ex.Degraded,
	}
	if ex.PAdjusted != nil {
		b.Probability = fmt.Sprintf("%.3f%% (calibrated, prevalence-corrected)", *ex.PAdjusted*100)
	}

	for _, e := range ex.Evidence {
		b.Evidence = append(b.Evidence, fmt.Sprintf("[%s] %s — %s", e.Severity, e.Title, e.Detail))
	}
	for _, e := range ex.Cleared {
		b.Cleared = append(b.Cleared, fmt.Sprintf("%s — %s", e.Title, e.Detail))
	}
	for _, e := range ex.NotEvaluated {
		b.NotEvaluated = append(b.NotEvaluated, fmt.Sprintf("%s — %s", e.Title, e.Reason))
	}
	for _, d := range ex.Detectors {
		indep := ""
		if d.Independent {
			indep = " (independent of the supervised model)"
		}
		b.Detectors = append(b.Detectors, fmt.Sprintf("%s: %s — %s%s", d.Name, d.Verdict, d.Summary, indep))
	}
	for _, c := range ex.CostTable {
		mark := ""
		if c.Chosen {
			mark = "  <- chosen, lowest expected cost"
		}
		b.Economics = append(b.Economics, fmt.Sprintf("%s: expected fraud loss %s + friction %s + lost good business %s = %s%s",
			c.Action, rupees(float64(c.ExpectedFraudLossMinor)), rupees(float64(c.FrictionMinor)),
			rupees(float64(c.LostBusinessMinor)), rupees(float64(c.TotalCostMinor)), mark))
	}
	for _, c := range ex.Counterfactuals {
		b.Counterfactuals = append(b.Counterfactuals, c.Question+" "+c.Answer)
	}
	if det != nil {
		var failed []string
		for _, c := range det.Checks {
			if !c.Match {
				failed = append(failed, c.Name)
			}
		}
		if det.Reproduced {
			b.Determinism = fmt.Sprintf("Re-executed from the persisted feature snapshot: all %d checks reproduced exactly, and the audit hash recomputes to the stored value.", len(det.Checks))
		} else {
			b.Determinism = "Re-execution did NOT reproduce: " + strings.Join(failed, ", ")
		}
	}
	return b
}
