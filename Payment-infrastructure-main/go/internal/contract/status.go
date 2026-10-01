package contract

// Status is the four-state signal/feature status (D5 — "not-applicable and not-evaluated
// are distinct from clean. A check that never ran must never render as a check that passed.")
type Status string

const (
	StatusClear         Status = "CLEAR"          // evaluated, no finding
	StatusFired         Status = "FIRED"          // evaluated, finding present
	StatusNotApplicable Status = "NOT_APPLICABLE"  // evaluated the guard; guard says this doesn't apply
	StatusNotEvaluated  Status = "NOT_EVALUATED"   // did not run (dependency down, deadline, cold start)
)

// Provenance classifies who controls a feature's value (docs/02 §2).
type Provenance string

const (
	ProvenanceA Provenance = "A" // attacker-controlled: set directly by the party being scored
	ProvenanceB Provenance = "B" // attacker-shapeable: costly to move, but movable
	ProvenanceC Provenance = "C" // bank-observed: derived from our own history, not forgeable
)
