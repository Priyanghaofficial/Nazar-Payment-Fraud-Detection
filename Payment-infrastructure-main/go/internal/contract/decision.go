package contract

// Action is a rung on the decision ladder plus the two off-ladder actions (docs/04 §5).
type Action string

const (
	ActionAllow               Action = "ALLOW"
	ActionAllowMonitor        Action = "ALLOW_MONITOR"
	ActionStepUp              Action = "STEP_UP"
	ActionStepUpInterstitial  Action = "STEP_UP_INTERSTITIAL"
	ActionHold                Action = "HOLD"
	ActionBlock               Action = "BLOCK" // local blocklist only, or CAP-class regulatory rail
	ActionCap                 Action = "CAP"   // off-ladder — docs/04 §5
)

// Ladder is the ordered rung sequence used by the advisory attach step. Advisories may
// never push past AdvisoryMaxRung (STEP_UP_INTERSTITIAL) — F-20.
var Ladder = []Action{ActionAllow, ActionAllowMonitor, ActionStepUp, ActionStepUpInterstitial, ActionHold}

func LadderIndex(a Action) int {
	for i, x := range Ladder {
		if x == a {
			return i
		}
	}
	return -1
}

// DecisionKind mirrors the Postgres enum (docs/02 §7).
type DecisionKind string

const (
	KindLive       DecisionKind = "LIVE"
	KindShadow     DecisionKind = "SHADOW"
	KindReplay     DecisionKind = "REPLAY"
	KindResolution DecisionKind = "RESOLUTION"
	KindControl    DecisionKind = "CONTROL"
)

// Decision is the full, persisted decision record — the audit chain's payload. JSON tags
// are snake_case throughout: this struct is serialised directly to the console over
// /v1/decide and /v1/decisions/{id}, so its wire shape IS the API contract.
type Decision struct {
	EndToEndID   string       `json:"end_to_end_id"`
	DecisionSeq  int          `json:"decision_seq"`
	Kind         DecisionKind `json:"kind"`
	DecidedAtMs  int64        `json:"decided_at_ms"`
	AcceptedAtMs int64        `json:"accepted_at_ms"`

	Action            Action   `json:"action"`
	PreAdvisoryAction Action   `json:"pre_advisory_action"` // what our own data alone concluded, before any advisory (docs/04 §5)
	RailFired         string   `json:"rail_fired"`
	ReasonCodes       []string `json:"reason_codes"`

	PModel            *float64 `json:"p_model"`             // calibrated probability
	PPrevalenceAdj    *float64 `json:"p_prevalence_adj"`    // after prior correction
	ExpectedLossMinor *int64   `json:"expected_loss_minor"`
	ExpectedCostMinor *int64   `json:"expected_cost_minor"` // includes friction — the real objective (docs/04 §2)

	Features      *FeatureVector     `json:"features"`
	Findings      []Finding          `json:"findings"`
	Contributions map[string]float64 `json:"contributions"` // exact TreeSHAP / linear signed contributions

	ModelBundleVersion    string `json:"model_bundle_version"`
	PolicyVersion         string `json:"policy_version"`
	RulesVersion          string `json:"rules_version"`
	SignalRegistryVersion string `json:"signal_registry_version"`

	IsControl        bool    `json:"is_control"`
	ActionPropensity float64 `json:"action_propensity"` // P(this action | policy), for off-policy eval

	Degraded []string `json:"degraded"`

	TotalMs      float64 `json:"total_ms"`
	QueueDelayMs float64 `json:"queue_delay_ms"`
	ServiceMs    float64 `json:"service_ms"`

	// audit chain
	DecisionShard int16  `json:"decision_shard"`
	ChainSeq      int64  `json:"chain_seq"`
	PrevHash      []byte `json:"prev_hash"` // base64 on the wire (Go's default []byte JSON encoding)
	Hash          []byte `json:"hash"`
	CheckpointID  *int64 `json:"checkpoint_id"`
}
