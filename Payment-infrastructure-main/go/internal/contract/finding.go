package contract

import "fmt"

// Finding is what a Signal emits. D4: "a signal that cannot explain itself cannot cross a
// boundary" — enforced here at construction, not by convention.
type Finding struct {
	SignalID    string     `json:"signal_id"`
	Status      Status     `json:"status"`
	Score       float64    `json:"score"` // signal-specific units; 0 for pure boolean rails
	Explanation string     `json:"explanation"` // MANDATORY non-empty. NewFinding panics if empty.
	Provenance  Provenance `json:"provenance"`
	LatencyMs   float64    `json:"latency_ms"`
	Version     string     `json:"version"`
	ReasonCode  string     `json:"reason_code"`
}

// NewFinding is the only constructor. It enforces D4.
func NewFinding(signalID string, status Status, explanation string) Finding {
	if explanation == "" {
		panic(fmt.Sprintf("contract: finding for signal %q constructed with empty explanation (violates D4)", signalID))
	}
	return Finding{SignalID: signalID, Status: status, Explanation: explanation}
}

// SignalMeta describes a Signal for the registry (docs/00 §3.2).
type SignalMeta struct {
	Name      string
	Kind      string // rule | model | novelty | graph | consortium
	Advisory  bool   // true if this signal's findings can only ever raise the ladder via §5 of docs/04
	Rails     []Rail
	Requires  []string // e.g. "profile:pair", "graph"
	Version   string
	State     string // off | shadow | live
}
