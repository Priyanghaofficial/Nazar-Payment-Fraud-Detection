package consortium

import "testing"

// test_two_bins_one_bank_is_one_reporter (F-53, docs/06 §4, docs/05 §4.3): two participant
// codes belonging to the same legal entity must collapse to ONE reporter for the
// >=2-independent-reporters rail — otherwise a single bank can trip its own multi-reporter
// threshold by reporting from two BINs.
func TestTwoBinsOneBankIsOneReporter(t *testing.T) {
	a := legalEntityOf("BANK_A")
	aCards := legalEntityOf("BANK_A_CARDS")
	b := legalEntityOf("BANK_B")

	if a != aCards {
		t.Fatalf("BANK_A and BANK_A_CARDS resolved to different legal entities (%q vs %q) — they are the same bank and must collapse", a, aCards)
	}
	if a == b {
		t.Fatalf("BANK_A and BANK_B resolved to the SAME legal entity (%q) — they are genuinely independent and must not collapse", a)
	}

	// The rail itself: two entries from BANK_A and BANK_A_CARDS must NOT satisfy >=2.
	entities := map[string]bool{}
	for _, reporter := range []string{"BANK_A", "BANK_A_CARDS"} {
		entities[legalEntityOf(reporter)] = true
	}
	if len(entities) >= 2 {
		t.Fatalf("BANK_A + BANK_A_CARDS collapsed to %d distinct legal entities, want 1 — the rail would incorrectly fire on a single bank's two BINs", len(entities))
	}

	entities2 := map[string]bool{}
	for _, reporter := range []string{"BANK_A", "BANK_B"} {
		entities2[legalEntityOf(reporter)] = true
	}
	if len(entities2) != 2 {
		t.Fatalf("BANK_A + BANK_B collapsed to %d distinct legal entities, want 2 — genuinely independent reporters must count separately", len(entities2))
	}
}
