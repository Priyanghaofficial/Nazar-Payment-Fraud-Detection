package main

import "testing"

// Dealing must exhaust every story before any repeats: three judges in a row seeing the same
// scam is the exact impression the rotation exists to prevent.
func TestScenarioDeckExhaustsBeforeRepeating(t *testing.T) {
	scenarioDeck = nil
	lastScenarioKey = ""
	n := len(judgeScenarios)

	for round := 0; round < 4; round++ {
		seen := map[string]int{}
		for i := 0; i < n; i++ {
			seen[pickScenario().Key]++
		}
		if len(seen) != n {
			t.Fatalf("round %d dealt %d distinct of %d: %v", round, len(seen), n, seen)
		}
	}
}

// And never the same story twice across the seam between decks.
func TestScenarioDeckNeverRepeatsBackToBack(t *testing.T) {
	scenarioDeck = nil
	lastScenarioKey = ""
	prev := ""
	for i := 0; i < 200; i++ {
		k := pickScenario().Key
		if k == prev {
			t.Fatalf("draw %d repeated %q back to back", i, k)
		}
		prev = k
	}
}
