// Package invariants holds the property/invariant test suite named in docs/06 §4 — "the
// architecture in executable form". Each test file's name and top-level test function name
// matches the doc's own naming so the mapping from spec to test is a grep away.
package invariants

import (
	"testing"

	"nazar/internal/features"
)

// test_feature_catalogue_key_coverage (docs/06 §4, docs/02 §3.1): every feature in the
// registry has a backing Redis key pattern and a producer, OR is explicitly declared as
// computed-not-read. This is the ~30-line test that would have caught the six orphaned
// features named in REVIEW.md's F-34.
func TestFeatureCatalogueKeyCoverage(t *testing.T) {
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	defs, err := features.LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("registry loaded zero features — something is broken in the loader or the YAML")
	}

	// The only feature that is legitimately keyless: it's an aggregate computed over the
	// OTHER features' statuses at assembly time (internal/features/compute.go), not read
	// from Redis. Every other feature must declare at least one backing key.
	computedNotRead := map[string]bool{"cold_start_features_n": true}

	for _, d := range defs {
		if computedNotRead[d.ID] {
			continue
		}
		if len(d.RequiresKeys) == 0 {
			t.Errorf("feature %q has no requires_keys and is not in the computed-not-read allowlist — orphaned feature (F-34 class bug)", d.ID)
		}
		if d.Provenance == "" {
			t.Errorf("feature %q has no provenance class (A/B/C) — docs/02 §2 requires one", d.ID)
		}
		if d.Description == "" {
			t.Errorf("feature %q has no description", d.ID)
		}
		if len(d.Rails) == 0 {
			t.Errorf("feature %q declares no applicable rails", d.ID)
		}
	}
}

// test_no_feature_derives_from_our_decisions (D6, docs/00 §2): registry provenance check.
// No feature ID may reference "decision", "action", or "score" in a way that would suggest
// it derives from Nazar's own prior output (the specific narrow exception, trusted_pair's
// use of LastDisposition, is a decision INPUT used directly in decide.Engine, never routed
// through the feature registry — see docs/04 §4's own note on this).
func TestNoFeatureDerivesFromOurDecisions(t *testing.T) {
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	defs, err := features.LoadRegistry(root)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	forbidden := []string{"our_action", "our_decision", "prior_decision", "our_score", "nazar_decision"}
	for _, d := range defs {
		for _, f := range forbidden {
			if containsFold(d.ID, f) || containsFold(d.Description, f) {
				t.Errorf("feature %q appears to derive from Nazar's own prior decisions (matched %q) — violates D6", d.ID, f)
			}
		}
	}
}

func containsFold(s, substr string) bool {
	sl, subl := toLower(s), toLower(substr)
	for i := 0; i+len(subl) <= len(sl); i++ {
		if sl[i:i+len(subl)] == subl {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
