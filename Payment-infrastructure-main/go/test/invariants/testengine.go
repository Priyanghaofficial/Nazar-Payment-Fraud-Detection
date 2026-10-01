package invariants

import (
	"testing"

	"nazar/internal/decide"
	"nazar/internal/features"
	"nazar/internal/rules"
	"nazar/internal/scoring"
)

// buildTestEngine constructs a real decide.Engine from the checked-in policy/rules bundles
// — the same files the production binary loads — with the heuristic scorer and an identity
// calibrator/prevalence so tests don't depend on a trained model artefact existing.
func buildTestEngine(t *testing.T) *decide.Engine {
	t.Helper()
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	rulesEngine, err := rules.LoadEngine(root, "2026-08-14.001.yaml")
	if err != nil {
		t.Fatalf("LoadEngine: %v", err)
	}
	policy, err := decide.LoadPolicy(root, "2026-08-14.001.yaml")
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	return &decide.Engine{
		Policy: policy, Rules: rulesEngine,
		Scorer:     scoring.NewHeuristicScorer(),
		Calibrator: scoring.NewIdentityBetaCalibrator(),
		Prevalence: &decide.PrevalenceCorrector{Version: "test", TrainPrevalence: 0.5, NaturalPrevalence: 0.5},
		Blocklist:  decide.NewBlocklist(), // empty: BLOCK can only come from here, and it's empty
		ModelBundleVersion: "test", PolicyVersion: policy.Version,
		RulesVersion: rulesEngine.Version, SignalRegistryVersion: "test",
	}
}
