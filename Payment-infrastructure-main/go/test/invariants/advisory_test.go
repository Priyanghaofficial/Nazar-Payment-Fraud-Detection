package invariants

import (
	"context"
	"math/rand"
	"testing"

	"nazar/internal/contract"
	"nazar/internal/decide"
	"nazar/internal/features"
)

// prop_advisory_monotone_and_capped (F-20, docs/06 §4, docs/04 §5): an advisory may never
// lower the rung the engine would otherwise have reached on its own data, and may never
// push the final action past policy.Ladder.AdvisoryMaxRung (STEP_UP_INTERSTITIAL) — in
// particular it can never reach HOLD.
func TestPropAdvisoryMonotoneAndCapped(t *testing.T) {
	engine := buildTestEngine(t)
	rng := rand.New(rand.NewSource(7))

	capIdx := contract.LadderIndex(contract.ActionStepUpInterstitial)

	const iterations = 1000
	for i := 0; i < iterations; i++ {
		ev := randomEvent(rng, i)
		pb := randomProfileBundle(rng, nil)
		fv := features.Compute(ev, pb, ev.AcceptedAtMs)

		advisories := randomAdvisories(rng)

		d, _ := engine.Decide(context.Background(), &decide.Input{
			Event: ev, Profile: pb, Features: fv,
			Graph: &contract.GraphResult{Evaluated: true}, Advisories: advisories,
		})

		if d.Action == contract.ActionCap || d.Action == contract.ActionBlock {
			continue // off-ladder, advisories don't apply — not what this property covers
		}

		finalIdx := contract.LadderIndex(d.Action)
		preIdx := contract.LadderIndex(d.PreAdvisoryAction)

		if finalIdx < preIdx {
			t.Errorf("iteration %d: advisory LOWERED the action: pre=%s final=%s", i, d.PreAdvisoryAction, d.Action)
		}
		// The cap governs what ADVISORIES may add on top of the engine's own conclusion —
		// docs/04 §5's attachAdvisory takes pre-advisory action as its floor. If the model
		// or policy rails alone already reached HOLD (preIdx == HOLD's index), that is not
		// an advisory escalation at all, and asserting the cap there would be testing the
		// wrong thing. The real property: advisories may never be the REASON the final
		// index exceeds the cap.
		if finalIdx > preIdx && finalIdx > capIdx {
			t.Errorf("iteration %d: an advisory escalated the action past the cap: pre=%s(%d) final=%s(%d) cap=%d — F-20 violated",
				i, d.PreAdvisoryAction, preIdx, d.Action, finalIdx, capIdx)
		}
		if finalIdx > preIdx && d.Action == contract.ActionHold {
			t.Errorf("iteration %d: an advisory escalated pre-advisory %s all the way to HOLD — advisories may never reach HOLD", i, d.PreAdvisoryAction)
		}
	}
}

func randomAdvisories(rng *rand.Rand) []decide.Advisory {
	n := rng.Intn(4)
	out := make([]decide.Advisory, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, decide.Advisory{
			SignalID:           "consortium-advisory",
			SignatureValid:     rng.Intn(5) != 0, // mostly valid, sometimes forged
			ReporterReputation: rng.Float64(),
			AgeHours:           rng.Float64() * 200,
			Confidence:         rng.Float64(),
			Steps:              rng.Intn(6), // deliberately allow "too many steps" to prove the cap holds
			Explanation:        "randomised test advisory",
		})
	}
	return out
}
