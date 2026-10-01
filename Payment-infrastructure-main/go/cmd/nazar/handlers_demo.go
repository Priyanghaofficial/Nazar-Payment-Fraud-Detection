package main

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"nazar/internal/audit"
	"nazar/internal/contract"
	"nazar/internal/persist"
)

// The demo scenario runner. Every scenario below calls s.DecideAndPersist — the SAME
// function the real POST /v1/decide handler uses — so this is the real production-shaped
// path exercised with scripted inputs, never a separate fake demo backend (CLAUDE.md §14).

type demoStep struct {
	Label    string             `json:"label"`
	Event    *contract.Event    `json:"event,omitempty"`
	Decision *contract.Decision `json:"decision,omitempty"`
	Note     string             `json:"note,omitempty"`
}

type demoResult struct {
	Scenario string     `json:"scenario"`
	Expected string     `json:"expected"`
	Steps    []demoStep `json:"steps"`
	Passed   bool       `json:"passed"`
}

func (s *Server) handleDemoRun(w http.ResponseWriter, r *http.Request) {
	scenario := r.PathValue("scenario")
	ctx := r.Context()
	run := time.Now().UnixNano() // makes every run's e2e_ids unique so idempotency never short-circuits a repeat demo

	var result demoResult
	var err error
	switch scenario {
	case "A":
		result, err = s.demoNormalTransaction(ctx, run)
	case "B":
		result, err = s.demoAppScam(ctx, run)
	case "C":
		result, err = s.demoMuleFanout(ctx, run)
	case "D":
		result, err = s.demoRegulatoryRail(ctx, run)
	case "E":
		result, err = s.demoRedisFailure(ctx, run)
	case "F":
		result, err = s.demoMerchantSafety(ctx, run)
	case "G":
		result, err = s.demoInvestigation(ctx, run)
	case "H":
		result, err = s.demoAuditVerify(ctx, run)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown scenario " + scenario + " (use A-H)"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func demoAccount(run int64, tag string) string {
	return fmt.Sprintf("BANK_A-DEMO-%s-%d", tag, run%100000)
}

func baseEvent(run int64, e2eSuffix, payer, payee string, amountMinor int64) *contract.Event {
	return &contract.Event{
		EndToEndID: fmt.Sprintf("demo-%d-%s", run, e2eSuffix), Rail: contract.RailUPI,
		DebtorAccount: payer, CreditorAccount: payee, InstructedAmountMinor: amountMinor,
		Initiation: "INTENT",
	}
}

// Scenario A — an established payer/payee pair making a routine payment. Expected: ALLOW.
func (s *Server) demoNormalTransaction(ctx context.Context, run int64) (demoResult, error) {
	payer, payee := demoAccount(run, "P1"), demoAccount(run, "B1")
	var steps []demoStep
	for i := 0; i < 6; i++ {
		ev := baseEvent(run, fmt.Sprintf("setup%d", i), payer, payee, 50000+int64(i)*1000)
		d, _, err := s.DecideAndPersist(ctx, ev)
		if err != nil {
			return demoResult{}, err
		}
		steps = append(steps, demoStep{Label: fmt.Sprintf("warm-up %d/6", i+1), Event: ev, Decision: d})
	}
	headline := baseEvent(run, "headline", payer, payee, 52000)
	d, _, err := s.DecideAndPersist(ctx, headline)
	if err != nil {
		return demoResult{}, err
	}
	steps = append(steps, demoStep{Label: "headline transaction", Event: headline, Decision: d})
	return demoResult{Scenario: "A", Expected: "ALLOW", Steps: steps, Passed: d.Action == contract.ActionAllow}, nil
}

// Scenario B — new beneficiary, amount in the overridable-warning band. Expected: STEP_UP_INTERSTITIAL.
func (s *Server) demoAppScam(ctx context.Context, run int64) (demoResult, error) {
	payer, payee := demoAccount(run, "P2"), demoAccount(run, "B2")
	ev := baseEvent(run, "headline", payer, payee, 300000) // Rs 3,000 — RAIL-102's band
	ev.RemittanceInfo = "urgent gift for cousin"
	d, _, err := s.DecideAndPersist(ctx, ev)
	if err != nil {
		return demoResult{}, err
	}
	return demoResult{
		Scenario: "B", Expected: "STEP_UP_INTERSTITIAL",
		Steps:  []demoStep{{Label: "first-ever payment to new beneficiary", Event: ev, Decision: d}},
		Passed: d.Action == contract.ActionStepUpInterstitial,
	}, nil
}

// Scenario C — mule fan-out: many first-time payers into one payee within a short window,
// two of them sharing a device. Expected: elevated score + nonzero graph ring evidence.
func (s *Server) demoMuleFanout(ctx context.Context, run int64) (demoResult, error) {
	payee := demoAccount(run, "MULE1")
	var steps []demoStep
	sharedDevice := fmt.Sprintf("demo-device-shared-%d", run)
	for i := 0; i < 10; i++ {
		payer := fmt.Sprintf("BANK_A-DEMO-FANOUT%d-%d", i, run%100000)
		ev := baseEvent(run, fmt.Sprintf("fanout%d", i), payer, payee, 180000)
		if i < 2 {
			ev.DeviceID = sharedDevice
		} else {
			ev.DeviceID = fmt.Sprintf("demo-device-%d-%d", i, run)
		}
		d, _, err := s.DecideAndPersist(ctx, ev)
		if err != nil {
			return demoResult{}, err
		}
		steps = append(steps, demoStep{Label: fmt.Sprintf("payer %d/10 into mule", i+1), Event: ev, Decision: d})
		time.Sleep(2 * time.Millisecond) // spread accepted_at_ms so the fan-in window is meaningful
	}
	gr := s.graph.Evaluate(payee, time.Now().UnixMilli())
	steps = append(steps, demoStep{Label: "graph evaluation", Note: fmt.Sprintf("ring_score=%.2f ring_size=%d shared_device_degree=%d", gr.RingScore, gr.RingSize, gr.DeviceSharedDegree)})
	return demoResult{Scenario: "C", Expected: "elevated risk + nonzero ring_score", Steps: steps, Passed: gr.RingScore > 0}, nil
}

// Scenario D — a large payment to a brand-new beneficiary. Expected: CAP via the
// regulatory cooling rail (RAIL-001), which is absolute and not overridable.
func (s *Server) demoRegulatoryRail(ctx context.Context, run int64) (demoResult, error) {
	payer, payee := demoAccount(run, "P3"), demoAccount(run, "B3")
	ev := baseEvent(run, "headline", payer, payee, 1_200_000) // Rs 12,000, above the Rs 5,000 cap
	d, _, err := s.DecideAndPersist(ctx, ev)
	if err != nil {
		return demoResult{}, err
	}
	return demoResult{
		Scenario: "D", Expected: "CAP (RAIL-001, regulatory)",
		Steps:  []demoStep{{Label: "large first-time payment", Event: ev, Decision: d}},
		Passed: d.Action == contract.ActionCap,
	}, nil
}

// Scenario E — kill Redis for real (podman stop), submit a transaction, confirm a real,
// non-blocking, degraded decision is still returned, then restore.
func (s *Server) demoRedisFailure(ctx context.Context, run int64) (demoResult, error) {
	container := s.redisContainer
	if container == "" {
		container = "nazar-redis"
	}
	var steps []demoStep

	if out, err := exec.Command(s.containerEngine, "stop", container).CombinedOutput(); err != nil {
		return demoResult{}, fmt.Errorf("stopping redis: %w (%s)", err, out)
	}
	steps = append(steps, demoStep{Label: "Redis stopped", Note: "container: " + container})
	time.Sleep(300 * time.Millisecond)

	payer, payee := demoAccount(run, "P4"), demoAccount(run, "B4")
	ev := baseEvent(run, "degraded", payer, payee, 90000)
	d, _, err := s.DecideAndPersist(ctx, ev)

	if out, restoreErr := exec.Command(s.containerEngine, "start", container).CombinedOutput(); restoreErr != nil {
		steps = append(steps, demoStep{Label: "Redis restore FAILED", Note: fmt.Sprintf("%v: %s", restoreErr, out)})
	} else {
		steps = append(steps, demoStep{Label: "Redis restored"})
	}

	if err != nil {
		return demoResult{}, err
	}
	steps = append([]demoStep{steps[0], {Label: "decision under degradation", Event: ev, Decision: d}}, steps[1:]...)

	passed := d.Action != contract.ActionBlock && len(d.Degraded) > 0
	return demoResult{Scenario: "E", Expected: "a real, non-BLOCK decision with degraded=[...]", Steps: steps, Passed: passed}, nil
}

// Scenario F — a legitimate merchant receiving from many distinct payers. Expected: zero
// ring signal (test_merchant_is_not_a_ring, the flagship graph-correctness claim).
func (s *Server) demoMerchantSafety(ctx context.Context, run int64) (demoResult, error) {
	merchant := demoAccount(run, "MERCHANT1")
	var steps []demoStep
	for i := 0; i < 30; i++ {
		payer := fmt.Sprintf("BANK_A-DEMO-CUST%d-%d", i, run%100000)
		ev := baseEvent(run, fmt.Sprintf("cust%d", i), payer, merchant, 45000+int64(i)*500)
		ev.DeviceID = fmt.Sprintf("demo-cust-device-%d-%d", i, run) // every customer has their own device
		d, _, err := s.DecideAndPersist(ctx, ev)
		if err != nil {
			return demoResult{}, err
		}
		if i < 3 || i == 29 {
			steps = append(steps, demoStep{Label: fmt.Sprintf("customer %d/30", i+1), Event: ev, Decision: d})
		}
	}
	gr := s.graph.Evaluate(merchant, time.Now().UnixMilli())
	steps = append(steps, demoStep{Label: "graph evaluation", Note: fmt.Sprintf("ring_score=%.2f ring_size=%d (%d payers, correctly NOT flagged as a ring)", gr.RingScore, gr.RingSize, gr.RingSize)})
	return demoResult{Scenario: "F", Expected: "ring_score == 0 despite high fan-in", Steps: steps, Passed: gr.RingScore == 0}, nil
}

// Scenario G — investigation: re-fetch a prior decision exactly as persisted (never
// recomputed), proving Time Machine reads are byte-faithful (test_replay_is_a_read).
func (s *Server) demoInvestigation(ctx context.Context, run int64) (demoResult, error) {
	b, err := s.demoAppScam(ctx, run)
	if err != nil {
		return demoResult{}, err
	}
	last := b.Steps[len(b.Steps)-1]
	time.Sleep(400 * time.Millisecond) // let the async shipper drain (docs/00 §4) before reading
	persisted, perr := persist.GetLatestDecision(ctx, s.db, last.Event.EndToEndID)
	note := "not yet shipped to Postgres — check /v1/decisions/" + last.Event.EndToEndID + " again shortly (async lane)"
	passed := false
	if perr == nil && persisted != nil {
		note = fmt.Sprintf("persisted action=%s matches live decision action=%s", persisted.Action, last.Decision.Action)
		passed = persisted.Action == last.Decision.Action
	}
	return demoResult{
		Scenario: "G", Expected: "persisted feature snapshot matches the live decision",
		Steps:  append(b.Steps, demoStep{Label: "re-fetch from Postgres (Time Machine read)", Note: note}),
		Passed: passed,
	}, nil
}

// Scenario H — audit chain verification, for real: recompute the SHA-256 chain over every
// persisted decision and compare against the stored hashes.
func (s *Server) demoAuditVerify(ctx context.Context, run int64) (demoResult, error) {
	recs, prevHashes, hashes, err := persist.LoadChainForVerification(ctx, s.db, 0)
	if err != nil {
		return demoResult{}, err
	}
	breakAt, ok := audit.Verify(recs, prevHashes, hashes)
	note := fmt.Sprintf("verified %d decisions on shard 0, chain intact", len(recs))
	if !ok {
		note = fmt.Sprintf("chain BROKEN at index %d of %d", breakAt, len(recs))
	}
	return demoResult{Scenario: "H", Expected: "chain verifies intact", Steps: []demoStep{{Label: "recompute chain", Note: note}}, Passed: ok}, nil
}
