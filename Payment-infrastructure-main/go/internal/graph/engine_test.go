package graph

import "testing"

// test_merchant_is_not_a_ring (docs/06 §4): 500 payers -> one merchant -> zero ring signal.
func TestMerchantIsNotARing(t *testing.T) {
	g := NewEngine()
	now := int64(1_700_000_000_000)
	for i := 0; i < 500; i++ {
		payer := accountID("PAYER", i)
		g.OnEvent(payer, "MERCHANT-001", deviceID(i), now-int64(i)*1000)
	}
	res := g.Evaluate("MERCHANT-001", now)
	if res.RingScore != 0 {
		t.Fatalf("merchant with 500 distinct payers got ring_score=%.3f, want 0", res.RingScore)
	}
}

// The complementary case the same milestone gate names: a small, device-linked,
// forwarding beneficiary should score nonzero.
func TestSmallDeviceLinkedForwardingClusterScoresNonzero(t *testing.T) {
	g := NewEngine()
	now := int64(1_700_000_000_000)
	sharedDevice := "device-shared-1"

	payers := []string{"BANK_A-p1", "BANK_A-p2", "BANK_A-p3", "BANK_B-p4", "BANK_B-p5",
		"BANK_A-p6", "BANK_A-p7", "BANK_B-p8", "BANK_A-p9", "BANK_A-p10", "BANK_B-p11"}
	for i, p := range payers {
		dev := sharedDevice
		if i%3 == 0 {
			dev = "device-unique-" + p
		}
		g.OnEvent(p, "MULE-001", dev, now-int64(i)*60_000)
	}
	// the mule forwards on quickly to a further account
	g.OnEvent("MULE-001", "CASHOUT-001", "", now-30_000)

	res := g.Evaluate("MULE-001", now)
	if res.RingScore <= 0 {
		t.Fatalf("small device-linked forwarding cluster got ring_score=%.3f, want >0", res.RingScore)
	}
	if res.HopsToCashout < 1 {
		t.Errorf("expected hops_to_cashout >= 1, got %d", res.HopsToCashout)
	}
}

func accountID(prefix string, i int) string {
	return prefix + "-000" + itoa(i)
}

func deviceID(i int) string {
	return "dev-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
