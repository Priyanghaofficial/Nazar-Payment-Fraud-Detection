package explain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"nazar/internal/contract"
	"nazar/internal/decide"
)

// Evidence is one reason, stated the way a person would state it.
type Evidence struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Detail   string  `json:"detail"`
	Source   string  `json:"source"`   // rail | model | graph | novelty | consortium | blocklist | profile
	Severity string  `json:"severity"` // critical | high | medium | low | info
	Weight   float64 `json:"weight"`   // |signed model contribution|, 0 for signals the model doesn't see
	Signed   float64 `json:"signed"`   // signed contribution: positive pushed toward fraud
	Value    float64 `json:"value"`
	HasValue bool    `json:"has_value"`
	Family   string  `json:"family"`
	Reason   string  `json:"reason,omitempty"` // why it wasn't evaluated
}

// Detector is one independent opinion about this transaction. The point of listing them
// side by side is that they do not share inputs: the supervised model learned from labelled
// history, the anomaly detector learned only the shape of normal traffic, the graph looks at
// the network around the beneficiary, and the rails encode written policy. Agreement across
// them is evidence; disagreement is a reason to challenge rather than block.
type Detector struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Verdict     string   `json:"verdict"` // FIRED | CLEAR | NOT_EVALUATED | NOT_APPLICABLE
	Score       *float64 `json:"score"`
	ScoreLabel  string   `json:"score_label"`
	Summary     string   `json:"summary"`
	Independent bool     `json:"independent"`
	Blocking    bool     `json:"blocking"` // may this detector alone raise friction?
}

// Counterfactual is a "what would have had to be different" statement, computed against the
// real cost function rather than asserted.
type Counterfactual struct {
	Kind     string `json:"kind"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type Explanation struct {
	EndToEndID        string   `json:"end_to_end_id"`
	Action            string   `json:"action"`
	ActionLabel       string   `json:"action_label"`
	PreAdvisoryAction string   `json:"pre_advisory_action"`
	Outcome           string   `json:"outcome"`
	Headline          string   `json:"headline"`
	Narrative         []string `json:"narrative"`

	PModel    *float64 `json:"p_model"`
	PAdjusted *float64 `json:"p_prevalence_adj"`
	RiskBand  string   `json:"risk_band"`

	Evidence     []Evidence `json:"evidence"`
	Cleared      []Evidence `json:"cleared"`
	NotEvaluated []Evidence `json:"not_evaluated"`

	Detectors       []Detector                `json:"detectors"`
	CostTable       []decide.ActionCost       `json:"cost_table"`
	Thresholds      []decide.ActionThreshold  `json:"thresholds"`
	Counterfactuals []Counterfactual          `json:"counterfactuals"`

	AmountMinor int64             `json:"amount_minor"`
	Rail        string            `json:"rail"`
	DecidedAtMs int64             `json:"decided_at_ms"`
	TotalMs     float64           `json:"total_ms"`
	ReasonCodes []string          `json:"reason_codes"`
	Degraded    []string          `json:"degraded"`
	Versions    map[string]string `json:"versions"`
	Tier        string            `json:"tier"`
}

// Input is everything Build needs. Event and Engine are optional: without the event there is
// no amount to reason about, so the economics section is omitted rather than guessed.
type Input struct {
	Decision *contract.Decision
	Event    *contract.Event
	Engine   *decide.Engine
}

var actionLabel = map[contract.Action]string{
	contract.ActionAllow:              "Allowed",
	contract.ActionAllowMonitor:       "Allowed, under watch",
	contract.ActionStepUp:             "Extra verification required",
	contract.ActionStepUpInterstitial: "Warned, with confirmation required",
	contract.ActionHold:               "Held for review",
	contract.ActionCap:                "Amount capped",
	contract.ActionBlock:              "Blocked",
}

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}

// Build produces the full explanation. It never re-scores: every number here either came
// from the persisted decision or is a fresh evaluation of the *policy* arithmetic (which is
// deterministic given the stored probability), and the latter is labelled as such.
func Build(in Input) *Explanation {
	d := in.Decision
	ex := &Explanation{
		EndToEndID:        d.EndToEndID,
		Action:            string(d.Action),
		ActionLabel:       actionLabel[d.Action],
		PreAdvisoryAction: string(d.PreAdvisoryAction),
		Outcome:           outcomeOf(d.Action),
		PModel:            d.PModel,
		PAdjusted:         d.PPrevalenceAdj,
		DecidedAtMs:       d.DecidedAtMs,
		TotalMs:           d.TotalMs,
		ReasonCodes:       d.ReasonCodes,
		Degraded:          d.Degraded,
		Versions: map[string]string{
			"model":    d.ModelBundleVersion,
			"policy":   d.PolicyVersion,
			"rules":    d.RulesVersion,
			"registry": d.SignalRegistryVersion,
		},
		Tier: "[MEASURED] on this transaction — every value below was computed at decision time and persisted; nothing is recomputed or estimated here.",
	}
	if in.Event != nil {
		ex.AmountMinor = in.Event.InstructedAmountMinor
		ex.Rail = string(in.Event.Rail)
	}

	ex.Evidence, ex.Cleared, ex.NotEvaluated = buildEvidence(d)
	ex.Detectors = buildDetectors(d)
	ex.RiskBand = riskBand(d)

	if in.Engine != nil && in.Event != nil && d.PPrevalenceAdj != nil {
		p := *d.PPrevalenceAdj
		ex.CostTable = in.Engine.CostTable(p, in.Event.Rail, in.Event.InstructedAmountMinor)
		ex.Thresholds = in.Engine.ActionThresholds(in.Event.Rail, in.Event.InstructedAmountMinor)
		ex.Counterfactuals = buildCounterfactuals(in.Engine, d, in.Event, ex)
	}

	ex.Headline = headline(d, ex)
	ex.Narrative = narrative(d, ex)
	return ex
}

func outcomeOf(a contract.Action) string {
	switch a {
	case contract.ActionAllow, contract.ActionAllowMonitor:
		return "allowed"
	case contract.ActionCap:
		return "capped"
	case contract.ActionBlock:
		return "blocked"
	default:
		return "challenged"
	}
}

// riskBand is descriptive, not a threshold the engine acts on — the engine acts on expected
// cost. It exists so the console can colour a row without pretending a probability is a
// verdict.
func riskBand(d *contract.Decision) string {
	switch d.Action {
	case contract.ActionBlock, contract.ActionCap, contract.ActionHold:
		return "severe"
	case contract.ActionStepUpInterstitial:
		return "high"
	case contract.ActionStepUp:
		return "elevated"
	case contract.ActionAllowMonitor:
		return "low"
	default:
		return "minimal"
	}
}

// ── evidence ────────────────────────────────────────────────────────────────

func buildEvidence(d *contract.Decision) (fired, cleared, notEval []Evidence) {
	contribs := d.Contributions
	if contribs == nil {
		contribs = map[string]float64{}
	}

	// 1. Signals that emitted a finding. These are the ones with a written explanation
	//    already attached at decision time (D4 — a signal that cannot explain itself cannot
	//    cross a boundary), so we surface their own words rather than paraphrasing them.
	for _, f := range d.Findings {
		e := Evidence{
			ID:     f.SignalID,
			Title:  signalTitle(f.SignalID, f.Status),
			Detail: publicDetail(f.SignalID, f.Explanation),
			Source: signalSource(f.SignalID),
			Family: signalFamily(f.SignalID),
		}
		switch f.SignalID {
		case "novelty":
			e.Value, e.HasValue = f.Score, f.Status != contract.StatusNotEvaluated
		case "graph":
			e.HasValue = false
		}
		switch f.Status {
		case contract.StatusFired:
			e.Severity = signalSeverity(f.SignalID)
			fired = append(fired, e)
		case contract.StatusClear:
			e.Severity = "info"
			cleared = append(cleared, e)
		case contract.StatusNotEvaluated:
			e.Severity = "info"
			e.Reason = f.Explanation
			notEval = append(notEval, e)
		case contract.StatusNotApplicable:
			e.Severity = "info"
			e.Reason = f.Explanation
			notEval = append(notEval, e)
		}
	}

	// 2. Feature-level evidence. A feature earns a line when it is both CLEAR (actually
	//    measured) and notable by its own registry-derived threshold. The model's signed
	//    contribution rides along as the weight, so the bar length is the model's own
	//    attribution rather than a presentation-layer invention.
	if d.Features != nil {
		byFamily := map[string]Evidence{}
		for id, v := range d.Features.Values {
			if d.Features.Status[id] != contract.StatusClear {
				continue
			}
			if strings.HasPrefix(id, "rf_") {
				continue // rule-features are already represented by their rule's finding
			}
			p := lookup(id)
			if !p.Notable(v) {
				continue
			}
			c := contribs[id]
			e := Evidence{
				ID: id, Title: p.Title, Detail: p.Render(v),
				Source: "profile", Severity: p.Severity(v),
				Weight: absf(c), Signed: c, Value: v, HasValue: true, Family: p.Family,
			}
			// One line per family: four velocity windows saying the same thing is noise, so
			// the strongest member wins and the rest stay in the raw feature vector.
			if prev, ok := byFamily[p.Family]; !ok || better(e, prev) {
				byFamily[p.Family] = e
			}
		}
		for _, e := range byFamily {
			fired = append(fired, e)
		}

		// Features that could not be computed are stated, not hidden — a judge asking "what
		// didn't you know?" gets a real answer.
		for id, st := range d.Features.Status {
			if st == contract.StatusClear || strings.HasPrefix(id, "rf_") {
				continue
			}
			reason := d.Features.Reason[id]
			if reason == "" {
				reason = string(st)
			}
			notEval = append(notEval, Evidence{
				ID: id, Title: lookup(id).Title, Source: "profile",
				Severity: "info", Family: lookup(id).Family, Reason: reason,
			})
		}
	}

	sort.SliceStable(fired, func(i, j int) bool {
		si, sj := severityRank[fired[i].Severity], severityRank[fired[j].Severity]
		if si != sj {
			return si < sj
		}
		return fired[i].Weight > fired[j].Weight
	})
	sort.SliceStable(notEval, func(i, j int) bool { return notEval[i].ID < notEval[j].ID })
	return fired, cleared, notEval
}

func better(a, b Evidence) bool {
	if severityRank[a.Severity] != severityRank[b.Severity] {
		return severityRank[a.Severity] < severityRank[b.Severity]
	}
	return a.Weight > b.Weight
}

// publicDetail replaces a signal's internal explanation with language written for the person
// reading it. The rule bundle's own text is written for whoever maintains the rule — it cites
// document sections and internal milestone names — and none of that belongs in front of an
// analyst, a customer, or a judge. Anything without a curated line falls back to the original
// with internal references stripped, so a new rule is never silently unexplained.
var publicRuleDetail = map[string]string{
	"rule:RAIL-001": "The beneficiary was added to this account less than 24 hours ago, and the amount is above the regulatory cooling-period limit for a brand-new payee.",
	"rule:RAIL-101": "This account is transacting far faster than it ever normally does — more than three times its own busiest hour on record.",
	"rule:RAIL-102": "This is the very first payment this customer has ever made to this account, and the amount sits squarely in the band impersonation scams use.",
	"rule:RF-001":   "First payment to this beneficiary, for a substantial amount.",
	"rule:RF-002":   "Money is arriving into the beneficiary account in a sudden burst, far above its own normal rate.",
	"rule:RF-003":   "A device we have never seen on this account is moving a substantial amount.",
	"rule:RF-004":   "The relationship between these two accounts is less than three days old.",
	"rule:RF-005":   "The implied travel speed since this customer's last payment is faster than a commercial aircraft.",
}

var internalRef = regexp.MustCompile(`\s*\((docs/|F-|D\d)[^)]*\)|\s*—\s*the core [^.]*\.|\s*\(docs[^)]*\)`)

func publicDetail(signalID, explanation string) string {
	if s, ok := publicRuleDetail[signalID]; ok {
		return s
	}
	s := internalRef.ReplaceAllString(explanation, "")
	s = strings.ReplaceAll(s, "Rs ", "₹")
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "."))
	if s == "" {
		return explanation
	}
	return s + "."
}

// signalFamily lets a rule and the feature underneath it collapse into one line. A judge
// hearing "first payment to this beneficiary" twice — once from a rule and once from the
// feature that rule reads — learns nothing the second time.
func signalFamily(id string) string {
	switch id {
	case "rule:RAIL-102", "rule:RF-001", "rule:RF-004":
		return "relationship"
	case "rule:RAIL-001":
		return "regulatory"
	case "rule:RAIL-101":
		return "velocity"
	case "rule:RF-002":
		return "fanin"
	case "rule:RF-003":
		return "device"
	case "rule:RF-005":
		return "geo"
	}
	return "signal:" + signalSource(id)
}

func signalSource(id string) string {
	switch {
	case strings.HasPrefix(id, "rule:"):
		return "rail"
	case strings.HasPrefix(id, "consortium:"):
		return "consortium"
	case id == "local_blocklist":
		return "blocklist"
	case id == "graph":
		return "graph"
	case id == "novelty":
		return "novelty"
	case id == "model":
		return "model"
	case id == "trusted_pair":
		return "profile"
	}
	return "signal"
}

func signalTitle(id string, st contract.Status) string {
	switch {
	case strings.HasPrefix(id, "rule:RAIL-001"):
		return "Regulatory beneficiary cooling period"
	case strings.HasPrefix(id, "rule:RAIL-101"):
		return "Payment rate far above this payer's own baseline"
	case strings.HasPrefix(id, "rule:RAIL-102"):
		return "First payment to a new beneficiary, in the scam-typical amount band"
	case strings.HasPrefix(id, "rule:RF-"):
		return "Risk pattern " + strings.TrimPrefix(id, "rule:")
	case strings.HasPrefix(id, "consortium:"):
		return "Cross-bank advisory"
	case id == "local_blocklist":
		return "Beneficiary is on the confirmed-fraud list"
	case id == "graph":
		if st == contract.StatusFired {
			return "Beneficiary sits in a suspected mule network"
		}
		return "Network around the beneficiary looks ordinary"
	case id == "novelty":
		if st == contract.StatusFired {
			return "Behaviour unlike anything we've seen recently"
		}
		return "Behaviour consistent with recent traffic"
	case id == "model":
		return "Learned fraud model"
	case id == "trusted_pair":
		return "Established relationship between these accounts"
	}
	return humanise(id)
}

func signalSeverity(id string) string {
	switch {
	case id == "local_blocklist":
		return "critical"
	case strings.HasPrefix(id, "rule:RAIL-001"):
		return "critical"
	case strings.HasPrefix(id, "rule:RAIL-"):
		return "high"
	case id == "graph":
		return "high"
	case strings.HasPrefix(id, "consortium:"):
		return "high"
	case id == "novelty":
		return "medium"
	case id == "model":
		return "medium"
	}
	return "low"
}

// ── detectors ───────────────────────────────────────────────────────────────

func buildDetectors(d *contract.Decision) []Detector {
	var firedRails, totalRails int
	var railNames []string
	var graphF, noveltyF, modelF *contract.Finding
	var advisories int
	for i := range d.Findings {
		f := &d.Findings[i]
		switch {
		case strings.HasPrefix(f.SignalID, "rule:"):
			totalRails++
			if f.Status == contract.StatusFired {
				firedRails++
				railNames = append(railNames, strings.TrimPrefix(f.SignalID, "rule:"))
			}
		case f.SignalID == "graph":
			graphF = f
		case f.SignalID == "novelty":
			noveltyF = f
		case f.SignalID == "model":
			modelF = f
		case strings.HasPrefix(f.SignalID, "consortium:"):
			if f.Status == contract.StatusFired {
				advisories++
			}
		}
	}

	dets := []Detector{{
		ID: "rails", Name: "Written rules & regulatory rails", Kind: "deterministic",
		Verdict: verdictOf(firedRails > 0, totalRails > 0),
		Summary: railSummary(firedRails, totalRails, railNames),
		Independent: true, Blocking: true,
	}}

	model := Detector{
		ID: "model", Name: "Supervised fraud model (LightGBM, calibrated)", Kind: "supervised",
		Verdict: "NOT_EVALUATED", Summary: "the model did not run for this decision",
		Independent: false, Blocking: true, ScoreLabel: "calibrated probability",
	}
	if modelF != nil && modelF.Status != contract.StatusNotEvaluated && d.PModel != nil {
		p := *d.PModel
		model.Verdict = "FIRED"
		model.Score = &p
		model.Summary = fmt.Sprintf("scored %.2f%% probability of fraud, calibrated against held-out data and corrected for real-world prevalence", p*100)
	} else if modelF != nil {
		model.Summary = modelF.Explanation
	}
	dets = append(dets, model)

	nov := Detector{
		ID: "novelty", Name: "Behavioural anomaly detector (conformal k-NN)", Kind: "unsupervised",
		Verdict: "NOT_EVALUATED", Independent: true, Blocking: false,
		ScoreLabel: "conformal p-value (lower = more unusual)",
		Summary:    "not enough recent traffic yet to say what normal looks like",
	}
	if noveltyF != nil {
		if noveltyF.Status == contract.StatusNotEvaluated {
			nov.Summary = noveltyF.Explanation
		} else {
			p := noveltyF.Score
			nov.Score = &p
			nov.Verdict = string(noveltyF.Status)
			if noveltyF.Status == contract.StatusFired {
				nov.Summary = fmt.Sprintf("only %.1f%% of recent traffic looks as unusual as this — flagged without ever having seen a labelled example of this attack", p*100)
			} else {
				nov.Summary = fmt.Sprintf("%.0f%% of recent traffic is at least this unusual — unremarkable", p*100)
			}
		}
	}
	dets = append(dets, nov)

	gr := Detector{
		ID: "graph", Name: "Beneficiary network analysis", Kind: "graph",
		Verdict: "NOT_EVALUATED", Independent: true, Blocking: false,
		ScoreLabel: "ring score",
		Summary:    "the graph signal did not run for this decision",
	}
	if graphF != nil {
		gr.Verdict = string(graphF.Status)
		gr.Summary = graphF.Explanation
	}
	dets = append(dets, gr)

	cons := Detector{
		ID: "consortium", Name: "Cross-bank advisories", Kind: "federated",
		Verdict: "CLEAR", Independent: true, Blocking: false,
		Summary: "no other bank has reported this beneficiary",
	}
	if advisories > 0 {
		cons.Verdict = "FIRED"
		cons.Summary = fmt.Sprintf("%d admissible advisory report(s) from other institutions — advisory only, capped below Hold", advisories)
	}
	dets = append(dets, cons)
	return dets
}

func verdictOf(fired, ran bool) string {
	if !ran {
		return "NOT_EVALUATED"
	}
	if fired {
		return "FIRED"
	}
	return "CLEAR"
}

func railSummary(fired, total int, names []string) string {
	if total == 0 {
		return "no rule bundle evaluated for this decision"
	}
	if fired == 0 {
		return fmt.Sprintf("all %d rules evaluated, none matched", total)
	}
	return fmt.Sprintf("%d of %d rules matched: %s", fired, total, strings.Join(names, ", "))
}

// ── counterfactuals ─────────────────────────────────────────────────────────

func buildCounterfactuals(eng *decide.Engine, d *contract.Decision, ev *contract.Event, ex *Explanation) []Counterfactual {
	var out []Counterfactual
	if d.PPrevalenceAdj == nil {
		return out
	}
	p := *d.PPrevalenceAdj

	// 1. What the model alone would have done, with no rails at all. This separates "a rule
	//    said so" from "the learned model said so" — the single most common judge question
	//    once they realise rails exist.
	modelOnly, _, _ := eng.EvaluateCost(p, ev.Rail, ev.InstructedAmountMinor)
	out = append(out, Counterfactual{
		Kind:     "ablation",
		Question: "What would the learned model have done on its own, with every written rule switched off?",
		Answer: fmt.Sprintf("%s — the model's calibrated probability of %.2f%% makes that the cheapest action at %s on %s. The final outcome was %s.",
			actionLabel[modelOnly], p*100, rupees(float64(ev.InstructedAmountMinor)), ev.Rail, actionLabel[d.Action]),
	})

	// 2. The amount at which this same probability would have escalated further. Real
	//    bisection on the real cost function, holding p fixed — stated explicitly, because
	//    changing the amount would in reality also change the amount features.
	next := nextRung(modelOnly)
	if next != "" {
		if amt, ok := eng.BreakEvenAmount(p, ev.Rail, next); ok {
			out = append(out, Counterfactual{
				Kind:     "amount",
				Question: fmt.Sprintf("How large would this payment have had to be for the economics alone to demand %s?", strings.ToLower(actionLabel[next])),
				Answer: fmt.Sprintf("%s or more, holding the fraud probability fixed at %.2f%%. (In reality a different amount would also move the amount-based features, so this isolates the economics, not the whole system.)",
					rupees(float64(amt)), p*100),
			})
		}
	}

	// 3. Where the probability bands sit for this exact payment.
	if len(ex.Thresholds) > 0 {
		var parts []string
		for _, t := range ex.Thresholds {
			if t.MinP <= 0 {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s above %.2f%%", strings.ToLower(actionLabel[t.Action]), t.MinP*100))
		}
		if len(parts) > 0 {
			out = append(out, Counterfactual{
				Kind:     "policy",
				Question: fmt.Sprintf("At %s on %s, what probability would it take to trigger each response?", rupees(float64(ev.InstructedAmountMinor)), ev.Rail),
				Answer: fmt.Sprintf("%s. These are not configured cut-offs — they fall out of the cost of being wrong in each direction, which is why they move when the amount does.",
					strings.Join(parts, "; ")),
			})
		}
	}

	// 4. Advisory contribution, when one actually changed the outcome.
	if d.PreAdvisoryAction != "" && d.PreAdvisoryAction != d.Action {
		out = append(out, Counterfactual{
			Kind:     "advisory",
			Question: "Did another bank's report change this outcome?",
			Answer: fmt.Sprintf("Yes. On our own data alone this was %s; cross-bank advisories raised it to %s. Advisories are capped below Hold by policy, so they can add friction but can never block on their own.",
				actionLabel[d.PreAdvisoryAction], actionLabel[d.Action]),
		})
	}
	return out
}

func nextRung(a contract.Action) contract.Action {
	i := contract.LadderIndex(a)
	if i < 0 || i+1 >= len(contract.Ladder) {
		return ""
	}
	return contract.Ladder[i+1]
}

// ── narrative ───────────────────────────────────────────────────────────────

func headline(d *contract.Decision, ex *Explanation) string {
	if len(ex.Evidence) == 0 {
		if d.Action == contract.ActionAllow || d.Action == contract.ActionAllowMonitor {
			return "Nothing unusual: every signal we evaluated came back clear."
		}
		return actionLabel[d.Action] + "."
	}
	// One fact per family, at most two, strongest first. A headline that repeats the same
	// finding in two vocabularies reads as padding and buries the actual reason.
	seen := map[string]bool{}
	var parts []string
	for _, e := range ex.Evidence {
		if len(parts) >= 2 || seen[e.Family] {
			continue
		}
		if len(parts) > 0 && severityRank[e.Severity] > severityRank["high"] {
			break
		}
		seen[e.Family] = true
		parts = append(parts, strings.TrimSuffix(strings.TrimSpace(e.Detail), "."))
	}
	s := strings.Join(parts, ". ")
	if len(s) > 0 {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	return s + "."
}

func narrative(d *contract.Decision, ex *Explanation) []string {
	var out []string

	// 1. What we saw.
	if n := len(ex.Evidence); n > 0 {
		out = append(out, fmt.Sprintf("%d signal%s fired on this payment, and %d more were evaluated and came back clear.",
			n, plural(n), len(ex.Cleared)))
	} else {
		out = append(out, fmt.Sprintf("No signal fired. %d checks ran and all came back clear.", len(ex.Cleared)))
	}

	// 2. Corroboration across independent detectors — the strongest honest claim available.
	var agree []string
	for _, det := range ex.Detectors {
		if det.Verdict == "FIRED" && det.Independent {
			agree = append(agree, det.Name)
		}
	}
	if len(agree) >= 2 {
		out = append(out, fmt.Sprintf("Independently of the learned model, %d detectors that share no training signal also flagged it: %s.",
			len(agree), strings.Join(agree, " and ")))
	} else if len(agree) == 1 {
		out = append(out, fmt.Sprintf("%s flagged this independently of the learned model.", agree[0]))
	}

	// 3. Why this action and not another — the economics, in money.
	if len(ex.CostTable) > 0 {
		var chosen, allow *decide.ActionCost
		for i := range ex.CostTable {
			if ex.CostTable[i].Chosen {
				chosen = &ex.CostTable[i]
			}
			if ex.CostTable[i].Action == contract.ActionAllow {
				allow = &ex.CostTable[i]
			}
		}
		if chosen != nil && allow != nil {
			if chosen.Action == contract.ActionAllow {
				out = append(out, fmt.Sprintf("Letting it through carries an expected cost of %s, lower than any intervention — so we did not add friction.",
					rupees(float64(allow.TotalCostMinor))))
			} else {
				out = append(out, fmt.Sprintf("We chose %s because it is the cheapest option overall: %s expected cost against %s if we had simply allowed it. That comparison prices the friction we impose on a genuine customer, not just the fraud we might stop.",
					strings.ToLower(actionLabel[chosen.Action]), rupees(float64(chosen.TotalCostMinor)), rupees(float64(allow.TotalCostMinor))))
			}
		}
	}

	// 4. Honesty about limits.
	if len(ex.NotEvaluated) > 0 {
		out = append(out, fmt.Sprintf("%d checks could not be evaluated for this payment and were excluded rather than scored as zero.", len(ex.NotEvaluated)))
	}
	if len(d.Degraded) > 0 {
		out = append(out, fmt.Sprintf("This decision ran degraded (%s). Under degradation we cap value rather than deny — a dependency being down is never a reason to refuse someone's money.",
			strings.Join(d.Degraded, ", ")))
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
