package invariants

import (
	"context"
	"math/rand"
	"testing"

	"nazar/internal/contract"
	"nazar/internal/decide"
	"nazar/internal/features"
)

// prop_no_block_under_degradation (D7, docs/06 §4, docs/01 §10): over the cross product of
// every injected failure and a wide random sample of events, no BLOCK occurs that would not
// have occurred healthy. Concretely: with an empty blocklist (BLOCK's only source in this
// engine), Decide must never return ActionBlock, regardless of degradation, amount, or
// profile state.
func TestPropNoBlockUnderDegradation(t *testing.T) {
	engine := buildTestEngine(t)
	rng := rand.New(rand.NewSource(42))

	degradationPatterns := [][]string{
		nil,
		{"profile:payer"},
		{"profile:payee"},
		{"profile:pair"},
		{"profile:payer", "profile:payee", "profile:device", "profile:pair", "profile:asn"}, // total outage
	}

	const iterations = 2000
	blockCount := 0
	for i := 0; i < iterations; i++ {
		ev := randomEvent(rng, i)
		pb := randomProfileBundle(rng, degradationPatterns[i%len(degradationPatterns)])
		degraded := degradationPatterns[i%len(degradationPatterns)]
		fv := features.Compute(ev, pb, ev.AcceptedAtMs)

		d, _ := engine.Decide(context.Background(), &decide.Input{
			Event: ev, Profile: pb, Features: fv, Degraded: degraded,
			Graph: &contract.GraphResult{Evaluated: len(degraded) == 0},
		})

		if d.Action == contract.ActionBlock {
			blockCount++
			t.Errorf("iteration %d: got BLOCK with empty blocklist and degraded=%v amount=%d — D7 violated",
				i, degraded, ev.InstructedAmountMinor)
		}
	}
	if blockCount > 0 {
		t.Fatalf("%d/%d iterations produced an unexplained BLOCK", blockCount, iterations)
	}
}

func randomEvent(rng *rand.Rand, i int) *contract.Event {
	rails := []contract.Rail{contract.RailUPI, contract.RailIMPS, contract.RailNEFT, contract.RailCardCNP}
	amounts := []int64{5000, 50000, 300000, 600000, 2000000, 10000000}
	return &contract.Event{
		EndToEndID:            randID(rng, i),
		AcceptedAtMs:          1_700_000_000_000 + int64(i)*1000,
		Rail:                  rails[rng.Intn(len(rails))],
		DebtorAccount:         randAccount(rng, "PAYER"),
		CreditorAccount:       randAccount(rng, "PAYEE"),
		InstructedAmountMinor: amounts[rng.Intn(len(amounts))],
		DeviceID:              maybeString(rng, "dev-"+randID(rng, i)),
		GeoCell:                maybeString(rng, "12.9:77.5"),
	}
}

func randomProfileBundle(rng *rand.Rand, degraded []string) *contract.ProfileBundle {
	isDegraded := func(tag string) bool {
		for _, d := range degraded {
			if d == tag {
				return true
			}
		}
		return false
	}
	pb := &contract.ProfileBundle{}

	pb.Payer.Degraded = isDegraded("profile:payer")
	pb.Payer.Present = !pb.Payer.Degraded
	if pb.Payer.Present && rng.Intn(2) == 0 {
		pb.Payer.TxnCountLifetime = int64(rng.Intn(500))
		pb.Payer.AmtMedianMinor = int64(rng.Intn(100000) + 1000)
		pb.Payer.AmtMADMinor = int64(rng.Intn(20000) + 100)
		pb.Payer.AmtP95Minor = int64(rng.Intn(500000) + 10000)
		pb.Payer.KnownPayees = map[string]bool{}
		pb.Payer.KnownDevices = map[string]bool{}
		pb.Payer.KnownASNs = map[int32]bool{}
	}

	pb.Payee.Degraded = isDegraded("profile:payee")
	pb.Payee.Present = !pb.Payee.Degraded

	pb.Device.Degraded = isDegraded("profile:device")
	pb.Device.Present = !pb.Device.Degraded

	pb.Pair.Degraded = isDegraded("profile:pair")
	if !pb.Pair.Degraded && rng.Intn(3) == 0 {
		pb.Pair.Present = true
		pb.Pair.TxnCount90d = int64(rng.Intn(20) + 2)
		pb.Pair.AmtP95Minor = int64(rng.Intn(500000) + 1000)
	}

	pb.ASN.Degraded = isDegraded("profile:asn")
	pb.ASN.Present = !pb.ASN.Degraded

	return pb
}

func randID(rng *rand.Rand, i int) string {
	return "rand-" + itoa(i) + "-" + itoa(rng.Intn(1_000_000))
}

func randAccount(rng *rand.Rand, prefix string) string {
	return "BANK_A-" + prefix + "-" + itoa(rng.Intn(1000))
}

func maybeString(rng *rand.Rand, s string) string {
	if rng.Intn(4) == 0 {
		return ""
	}
	return s
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
