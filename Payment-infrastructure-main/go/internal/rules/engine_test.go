package rules

import (
	"testing"

	"nazar/internal/contract"
	"nazar/internal/features"
)

func TestEngine_APPScamPattern_FiresStepUpInterstitial(t *testing.T) {
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	eng, err := LoadEngine(root, "2026-08-14.001.yaml")
	if err != nil {
		t.Fatalf("LoadEngine: %v", err)
	}

	ev := &contract.Event{
		EndToEndID:            "e2e-1",
		AcceptedAtMs:          1_700_000_000_000,
		Rail:                  contract.RailUPI,
		DebtorAccount:         "BANK_A-000001",
		CreditorAccount:       "BANK_A-000999",
		InstructedAmountMinor: 300_000, // Rs 3,000 — above RAIL-102's floor, below RAIL-001's regulatory cap
	}
	pb := &contract.ProfileBundle{
		Payer: contract.PayerBundle{
			Present:          true,
			TxnCountLifetime: 50,
			KnownPayees:      map[string]bool{}, // creditor NOT in this set -> new payee
		},
		Payee: contract.PayeeBundle{Present: true},
	}
	fv := contract.NewFeatureVector()
	fv.Set("payee_is_new_to_payer", 1)
	fv.Set("device_is_new_to_payer", 0)

	results := eng.Evaluate(ev, pb, fv)
	found := false
	for _, r := range results {
		if r.RuleID == "RAIL-102" {
			found = true
			if !r.Fired {
				t.Errorf("RAIL-102 (new payee + high value) expected to fire, did not")
			}
			if r.Action != ActionStepUpInterstitial {
				t.Errorf("RAIL-102 action = %s, want STEP_UP_INTERSTITIAL", r.Action)
			}
		}
	}
	if !found {
		t.Fatalf("RAIL-102 not present in bundle results")
	}
}

func TestEngine_RegulatoryCoolingRail_CapsNewBeneficiary(t *testing.T) {
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	eng, err := LoadEngine(root, "2026-08-14.001.yaml")
	if err != nil {
		t.Fatalf("LoadEngine: %v", err)
	}
	ev := &contract.Event{
		AcceptedAtMs:          1_700_000_000_000,
		Rail:                  contract.RailUPI,
		InstructedAmountMinor: 600_000, // above the 500000 cap
	}
	pb := &contract.ProfileBundle{
		Pair: contract.PairBundle{Present: false}, // never seen -> brand new -> cooling applies
	}
	fv := contract.NewFeatureVector()

	results := eng.Evaluate(ev, pb, fv)
	for _, r := range results {
		if r.RuleID == "RAIL-001" {
			if !r.Fired {
				t.Errorf("RAIL-001 (regulatory cooling) expected to fire for a brand-new pair above the cap")
			}
			if r.Action != ActionCap || r.CapMinor != 500000 {
				t.Errorf("RAIL-001 action=%s cap=%d, want CAP 500000", r.Action, r.CapMinor)
			}
		}
	}
}
