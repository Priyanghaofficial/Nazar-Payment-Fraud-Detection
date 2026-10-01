package contract

import (
	"encoding/json"
	"math"
)

// FeatureVector is the materialised, decision-time snapshot. Every feature that is not
// StatusClear/StatusFired carries NaN as its value (LightGBM handles NaN natively —
// docs/02 §5.6) so a missing feature is never confused with a real zero.
type FeatureVector struct {
	Values     map[string]float64
	Status     map[string]Status
	Reason     map[string]string  // populated when Status != CLEAR/FIRED, e.g. "COLD_START", "OFF_SCALE"
	Staleness  map[string]float64 // seconds stale, per feature's backing source, at decision time (D2)
}

func NewFeatureVector() *FeatureVector {
	return &FeatureVector{
		Values:    map[string]float64{},
		Status:    map[string]Status{},
		Reason:    map[string]string{},
		Staleness: map[string]float64{},
	}
}

// Set records a normally-computed feature.
func (fv *FeatureVector) Set(id string, value float64) {
	fv.Values[id] = value
	fv.Status[id] = StatusClear
}

// NotEvaluated records a feature that could not be computed (dependency down, deadline,
// guard tripped) — never a silent zero and never CLEAR (D5).
func (fv *FeatureVector) NotEvaluated(id, reason string) {
	fv.Values[id] = math.NaN()
	fv.Status[id] = StatusNotEvaluated
	fv.Reason[id] = reason
}

// NotApplicable records a feature whose guard says it does not apply to this transaction.
func (fv *FeatureVector) NotApplicable(id, reason string) {
	fv.Values[id] = math.NaN()
	fv.Status[id] = StatusNotApplicable
	fv.Reason[id] = reason
}

func (fv *FeatureVector) SetStaleness(id string, seconds float64) {
	fv.Staleness[id] = seconds
}

// JSONSafeValues converts Values to a map Go's encoding/json can serialise: NaN (used for
// every NOT_EVALUATED/NOT_APPLICABLE feature, docs/02 §5.6) becomes JSON null rather than
// tripping json.Marshal's "unsupported value: NaN" error. Status/Reason already carry the
// distinction (D5); this only fixes the wire encoding of the sentinel value.
func (fv *FeatureVector) JSONSafeValues() map[string]any {
	out := make(map[string]any, len(fv.Values))
	for k, v := range fv.Values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			out[k] = nil
		} else {
			out[k] = v
		}
	}
	return out
}

// MarshalJSON makes json.Marshal(fv) — and anything embedding *FeatureVector, like
// Decision — safe by construction, instead of relying on every caller to remember
// JSONSafeValues().
func (fv *FeatureVector) MarshalJSON() ([]byte, error) {
	type alias struct {
		Values    map[string]any     `json:"values"`
		Status    map[string]Status  `json:"status"`
		Reason    map[string]string  `json:"reason"`
		Staleness map[string]float64 `json:"staleness"`
	}
	return json.Marshal(alias{
		Values: fv.JSONSafeValues(), Status: fv.Status, Reason: fv.Reason, Staleness: fv.Staleness,
	})
}

// FeatureDef is one entry in the feature registry (docs/02 §4). Machine-read by the
// trainer, the scorer, and test_feature_catalogue_key_coverage.
type FeatureDef struct {
	ID            string
	Version       int
	Description   string
	RequiresKeys  []string // backing Redis key patterns — the thing the coverage test checks
	Provenance    Provenance
	CostToForge   string
	Monotone      string // "increasing" | "decreasing" | ""
	Rails         []Rail
	Catches       []string
}
