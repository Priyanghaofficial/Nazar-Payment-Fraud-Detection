package scoring

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/dmitryikh/leaves"

	"nazar/internal/contract"
)

// LeavesScorer is the real Scorer: a LightGBM model loaded via the pure-Go `leaves` library
// (docs/00 §6's "leaves (pure Go)" alternative to Treelite/cgo — chosen for the demo build
// so there is no C toolchain dependency). This is the second of Scorer's >=2 implementations
// required by docs/00 §3.2, and the one actually used once Milestone 2 has trained a model.
type LeavesScorer struct {
	ensemble     *leaves.Ensemble
	featureOrder []string
	bundleVersion string
	trainedAt     string
}

type featureManifest struct {
	BundleVersion string   `json:"bundle_version"`
	TrainedAt     string   `json:"trained_at"`
	FeatureOrder  []string `json:"feature_order"`
}

// LoadLeavesScorer reads a LightGBM text model plus the JSON manifest that records the
// exact column order used at training time (docs/02 §4: feature identity is immutable and
// the model binds to it by position within this manifest, not by name at inference time).
func LoadLeavesScorer(modelPath, manifestPath string) (*LeavesScorer, error) {
	// loadTransformation=true: apply the model's sigmoid objective so PredictSingle returns
	// an actual probability in (0,1), matching Python's model.predict_proba() at training
	// time. Loading without it returns the raw logit — BetaCalibrator.Calibrate assumes a
	// probability input (it clamps to [1e-6, 1-1e-6] and takes its log), so feeding it a raw
	// logit silently produces nonsense calibrated scores. Caught by
	// TestLoadLeavesScorer_TrainedModel's sanity check before this shipped.
	ensemble, err := leaves.LGEnsembleFromFile(modelPath, true)
	if err != nil {
		return nil, fmt.Errorf("scoring: loading LightGBM model %s: %w", modelPath, err)
	}
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("scoring: loading feature manifest %s: %w", manifestPath, err)
	}
	var fm featureManifest
	if err := json.Unmarshal(mb, &fm); err != nil {
		return nil, fmt.Errorf("scoring: parsing feature manifest: %w", err)
	}
	if ensemble.NFeatures() != len(fm.FeatureOrder) {
		return nil, fmt.Errorf("scoring: model expects %d features, manifest lists %d", ensemble.NFeatures(), len(fm.FeatureOrder))
	}
	return &LeavesScorer{
		ensemble:      ensemble,
		featureOrder:  fm.FeatureOrder,
		bundleVersion: fm.BundleVersion,
		trainedAt:     fm.TrainedAt,
	}, nil
}

func (s *LeavesScorer) Meta() contract.ModelMeta {
	return contract.ModelMeta{BundleVersion: s.bundleVersion, TrainedAt: s.trainedAt, FeatureOrder: s.featureOrder}
}

func (s *LeavesScorer) Score(fv *contract.FeatureVector) (float64, map[string]float64, error) {
	vals := s.toVector(fv)
	raw := s.ensemble.PredictSingle(vals, 0)

	// P0 attribution: single-order ablation, NOT TreeSHAP (docs/00 §6 specifies exact
	// TreeSHAP via Treelite's pred_contrib; `leaves` does not expose that, and building a
	// from-scratch TreeSHAP walker was judged not worth the time against the "thinnest
	// thing that demonstrates the claim" rule in docs/06). For each feature, zero it out
	// (0 = "nothing unusual" for every feature in this registry — ratios, counts, and
	// booleans are all centred near zero) and attribute the resulting score delta to that
	// feature. Real, computed, per-transaction, and monotone-consistent with the model —
	// just not exact Shapley decomposition. Labelled honestly wherever it's shown.
	contribs := make(map[string]float64, len(s.featureOrder))
	for i, id := range s.featureOrder {
		if fv.Status[id] != contract.StatusClear {
			continue
		}
		original := vals[i]
		vals[i] = 0
		ablated := s.ensemble.PredictSingle(vals, 0)
		vals[i] = original
		contribs[id] = raw - ablated
	}
	return raw, contribs, nil
}

func (s *LeavesScorer) toVector(fv *contract.FeatureVector) []float64 {
	out := make([]float64, len(s.featureOrder))
	for i, id := range s.featureOrder {
		if fv.Status[id] == contract.StatusClear {
			out[i] = fv.Values[id]
		} else {
			out[i] = math.NaN() // LightGBM's native missing-value handling (docs/02 §5.6)
		}
	}
	return out
}
