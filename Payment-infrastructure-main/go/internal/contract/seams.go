package contract

import "context"

// The five seams (docs/00 §3.2). Every replaceable component sits behind one of these.
// No caller may type-assert through a seam (lint-checked by go/test/invariants). Every
// seam has >=2 implementations from day one: the real one and a deterministic fake used
// in tests.

// ProfileStore: Redis -> Dragonfly -> Aerospike, without callers knowing.
type ProfileStore interface {
	Load(ctx context.Context, ev *Event) (*ProfileBundle, error)
	Apply(ctx context.Context, ev *Event) error
}

// Signal: rule | model | novelty | graph | consortium. The Signal list is data (the
// registry), not code — adding/removing/reordering a signal is a config change (docs/00 §3.2).
type Signal interface {
	Meta() SignalMeta
	Evaluate(ctx context.Context, sc *ScoringContext) ([]Finding, error)
}

// Scorer: LightGBM -> XGBoost -> NN -> a different model per rail.
type Scorer interface {
	Score(fv *FeatureVector) (raw float64, contribs map[string]float64, err error)
	Meta() ModelMeta
}

type ModelMeta struct {
	BundleVersion string
	TrainedAt     string
	FeatureOrder  []string
}

// Calibrator: beta -> isotonic -> temperature scaling.
type Calibrator interface {
	Calibrate(raw float64) float64
	Meta() CalibratorMeta
}

type CalibratorMeta struct {
	Method  string
	Version string
}

// DecisionSink: sync PG -> WAL+async -> Kafka -> whatever.
type DecisionSink interface {
	Emit(ctx context.Context, d *Decision) error
}
