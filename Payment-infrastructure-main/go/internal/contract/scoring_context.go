package contract

// ScoringContext is what every Signal.Evaluate receives: the event, the profile bundle
// already loaded (read strictly before write, docs/02 §3.4), the feature vector assembled
// so far, and the deadline budget remaining. Signals must not perform their own I/O against
// the profile store — internal/features may not import internal/profile (docs/00 §9), and
// this struct is how that boundary is enforced structurally rather than by convention.
type ScoringContext struct {
	Event    *Event
	Profile  *ProfileBundle
	Features *FeatureVector

	DeadlineRemainingMs float64
	Degraded            []string // dependency names already known-down for this request

	// Graph and consortium results, if evaluated, are attached here rather than fetched by
	// the signal itself — keeps every Signal a pure function of its inputs.
	Graph      *GraphResult
	Consortium *ConsortiumResult
}

type GraphResult struct {
	Evaluated         bool
	StalenessSec      float64
	RingScore         float64
	RingSize          int
	ComponentBankCount int
	HopsToCashout     int
	DeviceSharedDegree int
}

type ConsortiumResult struct {
	Evaluated  bool
	Findings   []Finding
}
