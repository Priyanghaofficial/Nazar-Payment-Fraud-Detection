// Package scoring implements the Scorer and Calibrator seams (docs/00 §3.2).
// Two Scorer implementations exist from day one, per docs/00 §3.2's "every seam has >=2
// implementations": HeuristicScorer (deterministic, no trained artefact required — used in
// tests and as the pre-Milestone-2 fallback) and LeavesScorer (the real LightGBM model,
// loaded via github.com/dmitryikh/leaves — the pure-Go alternative to Treelite/cgo named in
// docs/00 §6, chosen here to avoid a cgo toolchain dependency in the demo build).
package scoring

import (
	"math"

	"nazar/internal/contract"
)

// HeuristicScorer is a hand-weighted linear combination over a handful of high-signal
// features. It is NOT the product's model — it exists so the decision engine is testable
// and demoable before Milestone 2 trains the real one, and so Scorer has a second,
// dependency-free implementation as the seam discipline requires.
type HeuristicScorer struct {
	Weights map[string]float64
	Bias    float64
}

func NewHeuristicScorer() *HeuristicScorer {
	return &HeuristicScorer{
		Bias: -3.0,
		Weights: map[string]float64{
			"payee_is_new_to_payer":       1.8,
			"amt_over_p95":                0.6,
			"payee_fanin_1h":              0.35,
			"payee_fanin_burstiness":      0.25,
			"device_is_new_to_payer":      1.1,
			"geo_jump_kmh":                0.004,
			"amt_robust_z":                0.15,
			"payee_first_seen_by_us_days": -0.05,
			"pair_txn_count_90d":          -0.4,
		},
	}
}

func (s *HeuristicScorer) Score(fv *contract.FeatureVector) (float64, map[string]float64, error) {
	contribs := make(map[string]float64, len(s.Weights))
	logit := s.Bias
	for id, w := range s.Weights {
		if fv.Status[id] != contract.StatusClear {
			continue // NOT_EVALUATED/NOT_APPLICABLE contribute nothing rather than a fabricated value
		}
		c := w * fv.Values[id]
		contribs[id] = c
		logit += c
	}
	p := 1.0 / (1.0 + expNeg(logit))
	return p, contribs, nil
}

func (s *HeuristicScorer) Meta() contract.ModelMeta {
	return contract.ModelMeta{BundleVersion: "heuristic-v0", TrainedAt: "n/a — hand-weighted fallback"}
}

func expNeg(x float64) float64 {
	return math.Exp(-x)
}
