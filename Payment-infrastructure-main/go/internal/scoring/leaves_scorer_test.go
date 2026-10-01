package scoring

import (
	"path/filepath"
	"testing"

	"nazar/internal/contract"
	"nazar/internal/features"
)

// TestLoadLeavesScorer_TrainedModel loads whatever model py/training last produced (if any)
// and confirms it scores without error, with monotone-sane behaviour on the clearest
// feature: a brand-new payee should never score lower than an established one, all else
// equal. Skips if no trained model exists yet (this repo can run entirely on the heuristic
// fallback — training is optional for the demo to function, per docs/06's "stub
// deliberately" table not applying here, but a genuinely absent artefact is not a test failure).
func TestLoadLeavesScorer_TrainedModel(t *testing.T) {
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	modelPath := filepath.Join(root, "py", "training", "output", "model.txt")
	manifestPath := filepath.Join(root, "py", "training", "output", "model_manifest.json")

	s, err := LoadLeavesScorer(modelPath, manifestPath)
	if err != nil {
		t.Skipf("no trained model at %s yet (train via py/training/train_nazar_model.py): %v", modelPath, err)
	}

	base := contract.NewFeatureVector()
	for _, id := range s.featureOrder {
		base.Set(id, 0)
	}
	base.Set("payee_is_new_to_payer", 0)
	base.Set("amt_over_p95", 1.0)

	novel := contract.NewFeatureVector()
	for _, id := range s.featureOrder {
		novel.Set(id, 0)
	}
	novel.Set("payee_is_new_to_payer", 1)
	novel.Set("amt_over_p95", 1.0)

	pBase, contribsBase, err := s.Score(base)
	if err != nil {
		t.Fatalf("Score(base): %v", err)
	}
	pNovel, _, err := s.Score(novel)
	if err != nil {
		t.Fatalf("Score(novel): %v", err)
	}
	t.Logf("p(established payee)=%.6f p(new payee)=%.6f", pBase, pNovel)
	if pNovel < pBase {
		t.Errorf("new-payee score (%.6f) is LOWER than established-payee score (%.6f) — monotone constraint violated or model learned the wrong direction", pNovel, pBase)
	}
	if len(contribsBase) == 0 {
		t.Error("Score returned zero contributions — attribution is broken")
	}
}
