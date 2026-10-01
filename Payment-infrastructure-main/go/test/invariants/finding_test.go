package invariants

import (
	"testing"

	"nazar/internal/contract"
)

// test_finding_without_explanation_raises (D4, docs/06 §4): "a signal that cannot explain
// itself cannot cross a boundary" — enforced at construction, not by convention.
func TestFindingWithoutExplanationRaises(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewFinding with an empty explanation did not panic — D4 is not enforced")
		}
	}()
	contract.NewFinding("some_signal", contract.StatusFired, "")
}

func TestFindingWithExplanationDoesNotRaise(t *testing.T) {
	f := contract.NewFinding("some_signal", contract.StatusFired, "a real explanation")
	if f.Explanation == "" {
		t.Fatal("explanation was lost")
	}
}

// test_signal_that_did_not_run_is_not_clean (D5, docs/06 §4): four-state status. A check
// that never ran must never render as StatusClear.
func TestSignalThatDidNotRunIsNotClean(t *testing.T) {
	fv := contract.NewFeatureVector()

	fv.NotEvaluated("payee_fwd_ratio_1h", "STALE")
	if fv.Status["payee_fwd_ratio_1h"] == contract.StatusClear {
		t.Error("NotEvaluated feature reads as CLEAR — D5 violated")
	}
	if fv.Status["payee_fwd_ratio_1h"] != contract.StatusNotEvaluated {
		t.Errorf("got status %s, want NOT_EVALUATED", fv.Status["payee_fwd_ratio_1h"])
	}
	if fv.Reason["payee_fwd_ratio_1h"] == "" {
		t.Error("NotEvaluated feature has no reason recorded")
	}

	fv.NotApplicable("device_is_new_to_payer", "NO_DEVICE_ID")
	if fv.Status["device_is_new_to_payer"] == contract.StatusClear {
		t.Error("NotApplicable feature reads as CLEAR — D5 violated")
	}

	// The four states really are distinct, and NOT_APPLICABLE != NOT_EVALUATED != CLEAR != FIRED.
	states := map[contract.Status]bool{
		contract.StatusClear: true, contract.StatusFired: true,
		contract.StatusNotApplicable: true, contract.StatusNotEvaluated: true,
	}
	if len(states) != 4 {
		t.Fatalf("expected 4 distinct status values, got %d", len(states))
	}
}
