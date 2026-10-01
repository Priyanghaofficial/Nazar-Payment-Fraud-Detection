// Package narrate turns a structured Explanation into an analyst-facing write-up.
//
// Three things about this package are deliberate, and all three are the pitch:
//
//  1. It is OFF the request path. No decision ever waits on a language model. The
//     deterministic narrative in internal/explain is always produced first and is always
//     sufficient on its own; this lane only adds prose on demand, when a human opens a case.
//
//  2. The model never sees free text from the payment. CLAUDE.md non-negotiable #14 —
//     raw remittance_info never reaches an LLM, because an attacker controls that field and
//     a language model is the single most injectable component in the system. The brief
//     below is built by whitelist from values Nazar itself computed, never by serialising
//     the event. assertNoFreeText() enforces that at runtime, and TestBriefCarriesNoAttackerText
//     enforces it at build time.
//
//  3. The provider is a seam, not a dependency. Provider is chosen by base URL: Groq today
//     because it is fast and free to demo against, an on-premise OpenAI-compatible server
//     (vLLM, llama.cpp, Ollama) in production by changing one environment variable. The
//     air-gapped claim is not aspirational — it is the same code path with a different host,
//     and DeterministicNarrator is the zero-network floor beneath both.
package narrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Brief is the ONLY thing a language model is ever shown. Every field is a value this system
// computed or a label it controls. There is no field here an attacker can write into.
type Brief struct {
	Action      string   `json:"action"`
	ActionLabel string   `json:"action_label"`
	Outcome     string   `json:"outcome"`
	AmountText  string   `json:"amount"`
	Rail        string   `json:"rail"`
	RiskBand    string   `json:"risk_band"`
	Probability string   `json:"calibrated_fraud_probability"`
	Evidence    []string `json:"evidence_that_fired"`
	Cleared     []string `json:"checks_that_came_back_clear"`
	NotEvaluated []string `json:"checks_that_could_not_run"`
	Detectors   []string `json:"independent_detector_verdicts"`
	Economics   []string `json:"expected_cost_of_each_option"`
	Counterfactuals []string `json:"counterfactuals"`
	Degraded    []string `json:"degraded_dependencies"`
	Determinism string   `json:"reproducibility"`
}

// Result is what a narrator returns.
type Result struct {
	Summary    string   `json:"summary"`      // 2-3 sentences for the top of the case file
	Reasoning  []string `json:"reasoning"`    // the walk-through
	NextSteps  []string `json:"next_steps"`   // what the analyst should do now
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	Endpoint   string   `json:"endpoint"`
	OnPremise  bool     `json:"on_premise"`
	LatencyMs  float64  `json:"latency_ms"`
	Deterministic bool  `json:"deterministic"` // true when produced without any model call
	Note       string   `json:"note"`
}

// Narrator is the seam. Swapping hosted for on-premise is a constructor change, nothing else.
type Narrator interface {
	Narrate(ctx context.Context, b Brief) (*Result, error)
	Meta() Meta
}

type Meta struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	OnPremise bool   `json:"on_premise"`
	Available bool   `json:"available"`
	Note      string `json:"note"`
}

// ErrNotConfigured is returned when no model endpoint is configured. Callers fall back to
// the deterministic narrator rather than showing an error — the system is never mute.
var ErrNotConfigured = errors.New("narrate: no language model endpoint configured")

// forbiddenSubstrings are field names that must never appear in a brief. This is a
// belt-and-braces runtime check on top of the whitelist construction: if someone later adds
// a field to Brief that carries event text, this trips instead of silently shipping payer
// free-text to a third party.
var forbiddenKeys = []string{"remittance", "narration", "description", "memo", "note_to_payee"}

// assertNoFreeText is called before every outbound request.
func assertNoFreeText(b Brief) error {
	var all []string
	all = append(all, b.Evidence...)
	all = append(all, b.Cleared...)
	all = append(all, b.NotEvaluated...)
	all = append(all, b.Detectors...)
	all = append(all, b.Economics...)
	all = append(all, b.Counterfactuals...)
	for _, s := range all {
		low := strings.ToLower(s)
		for _, k := range forbiddenKeys {
			if strings.Contains(low, k+":") || strings.Contains(low, k+" =") {
				return fmt.Errorf("narrate: refusing to send brief containing %q — attacker-controlled free text must never reach a language model (CLAUDE.md #14)", k)
			}
		}
	}
	return nil
}

// DeterministicNarrator produces the write-up with no model, no network and no API key. It
// is the air-gapped floor: if every external dependency is gone, an analyst still gets a
// structured explanation, because the explanation was never the model's to produce.
type DeterministicNarrator struct{}

func (DeterministicNarrator) Meta() Meta {
	return Meta{
		Provider: "none (built-in)", Model: "deterministic template", Endpoint: "in-process",
		OnPremise: true, Available: true,
		Note:      "Rule-based write-up assembled from the same structured explanation. No network, no model, no key. This is what runs when the LLM lane is switched off or unreachable.",
	}
}

func (DeterministicNarrator) Narrate(_ context.Context, b Brief) (*Result, error) {
	r := &Result{
		Provider: "none (built-in)", Model: "deterministic template", Endpoint: "in-process",
		OnPremise: true, Deterministic: true,
		Note: "Generated without a language model.",
	}
	r.Summary = fmt.Sprintf("%s — %s on %s. %s", b.ActionLabel, b.AmountText, b.Rail, firstOr(b.Evidence, "No signal fired."))
	r.Reasoning = append(r.Reasoning, b.Evidence...)
	r.Reasoning = append(r.Reasoning, b.Economics...)
	switch b.Outcome {
	case "challenged":
		r.NextSteps = []string{
			"Confirm the customer genuinely intended this payment before releasing it.",
			"If the beneficiary is confirmed fraudulent, report it so other banks see the advisory.",
		}
	case "blocked", "capped":
		r.NextSteps = []string{"Verify the blocklist or regulatory basis, and record the four-eyes approval on the case."}
	default:
		r.NextSteps = []string{"No action required. Retained for audit and for model feedback."}
	}
	return r, nil
}

func firstOr(xs []string, def string) string {
	if len(xs) == 0 {
		return def
	}
	return xs[0]
}

// ── interactive lane ────────────────────────────────────────────────────────
//
// Same seam, same brief, same guarantees. An analyst asks follow-up questions about a
// decision that has ALREADY been made; nothing here can change it, and the model is still
// shown only the whitelisted Brief. The analyst's own questions are free text, but the
// analyst is a trusted operator sitting at the console — the injection surface this system
// actually guards against is attacker-controlled *payment* fields, which never appear here.

// Turn is one message in an analyst conversation.
type Turn struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// Answer is a single reply in that conversation.
type Answer struct {
	Reply     string  `json:"reply"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	OnPremise bool    `json:"on_premise"`
	LatencyMs float64 `json:"latency_ms"`
	Grounded  bool    `json:"grounded"` // false when answered without a model
	Note      string  `json:"note"`
}

// ChatNarrator is optional. A narrator that cannot hold a conversation simply does not
// implement it, and the handler falls back to the deterministic answerer.
type ChatNarrator interface {
	Chat(ctx context.Context, b Brief, history []Turn) (*Answer, error)
}

// maxTurns bounds how much conversation is replayed to the model. A demo conversation is a
// handful of questions; anything past this is either a mistake or an attempt to push the
// brief out of the context window with filler.
const maxTurns = 16

// SanitiseTurns drops empty and over-long messages and keeps only the most recent exchange.
// Length-capping matters: the brief is the grounding, and a caller that sends a megabyte of
// "user" text could otherwise crowd it out of the prompt entirely.
func SanitiseTurns(in []Turn) []Turn {
	out := make([]Turn, 0, len(in))
	for _, t := range in {
		role := strings.ToLower(strings.TrimSpace(t.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		c := strings.TrimSpace(t.Content)
		if c == "" {
			continue
		}
		if len(c) > 2000 {
			c = c[:2000]
		}
		out = append(out, Turn{Role: role, Content: c})
	}
	if len(out) > maxTurns {
		out = out[len(out)-maxTurns:]
	}
	return out
}

// Chat on the deterministic narrator is the zero-network floor: it cannot answer an
// arbitrary question, so it says so plainly rather than guessing. The console shows this
// verbatim, which is the honest behaviour when the model lane is unavailable.
func (DeterministicNarrator) Chat(_ context.Context, b Brief, _ []Turn) (*Answer, error) {
	var sb strings.Builder
	sb.WriteString("The language-model lane is not available, so I can only restate the record rather than answer freely.\n\n")
	sb.WriteString("Decision: " + b.ActionLabel + " on " + b.AmountText + " over " + b.Rail + ".\n")
	if len(b.Evidence) > 0 {
		sb.WriteString("\nWhat fired:\n")
		for _, e := range b.Evidence {
			sb.WriteString("  • " + e + "\n")
		}
	}
	if len(b.Cleared) > 0 {
		sb.WriteString("\nWhat came back clear:\n")
		for _, e := range b.Cleared {
			sb.WriteString("  • " + e + "\n")
		}
	}
	sb.WriteString("\n" + b.Determinism)
	return &Answer{
		Reply: sb.String(), Provider: "none (built-in)", Model: "deterministic template",
		OnPremise: true, Grounded: false,
		Note: "No model was called. This is the decision record itself, restated.",
	}, nil
}
