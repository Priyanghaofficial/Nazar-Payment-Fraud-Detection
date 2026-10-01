package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"nazar/internal/contract"
)

// The live traffic and attack simulator.
//
// Why this exists: a fraud console with no traffic in it is indistinguishable from a
// mockup, and "we detect fraud in real time" is a claim you have to be able to *watch*.
// Everything below drives s.DecideAndPersist — the identical function the production HTTP
// handler and the payer app call. There is no simulation-only decision path, no pre-baked
// verdicts and no seeded rows: the attacks are real payment events, scored live by the real
// engine, and the outcomes are whatever the engine actually decides. If a campaign runs and
// the system fails to catch it, the console will show exactly that.
//
// Accounts created here are prefixed SIM- so they are always distinguishable from the judge
// demo (JUDGE-) and the scripted scenarios (DEMO-) in any query afterwards.

const (
	simAccountPrefix = "SIM"
	ambientPairs     = 24 // established payer/payee pairs kept warm for baseline traffic
	warmupPerPair    = 6  // enough shared history to clear the trusted-pair threshold (5)
)

type campaignProgress struct {
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Sent       int    `json:"sent"`
	Total      int    `json:"total"`
	Challenged int    `json:"challenged"`
	Allowed    int    `json:"allowed"`
	Running    bool   `json:"running"`
	StartedMs  int64  `json:"started_ms"`
	Narrative  string `json:"narrative"`
	// Counting payments treats a Rs 2 credential probe and a Rs 5,000 cash-out as the same
	// event, which is the exact mistake this system is built not to make: challenging the
	// Rs 2 payment costs more than the fraud it prevents. A campaign of tiny probes with one
	// large finale therefore shows a low payment-count rate and a high value rate, and the
	// count alone reads as failure when the economics are working. Both are reported.
	ValueAtRiskMinor     int64 `json:"value_at_risk_minor"`
	ValueStoppedMinor    int64 `json:"value_stopped_minor"`
}

type simulator struct {
	mu sync.Mutex

	trafficCancel  context.CancelFunc
	trafficRunning bool
	trafficTPS     float64
	warmed         bool
	pairs          [][2]string

	campaign       *campaignProgress
	campaignCancel context.CancelFunc

	ambientSent atomic.Int64
	attackSent  atomic.Int64
}

func newSimulator() *simulator { return &simulator{trafficTPS: 2} }

func (s *Server) handleSimStatus(w http.ResponseWriter, r *http.Request) {
	sim := s.sim
	sim.mu.Lock()
	st := map[string]any{
		"traffic_running": sim.trafficRunning,
		"traffic_tps":     sim.trafficTPS,
		"ambient_sent":    sim.ambientSent.Load(),
		"attack_sent":     sim.attackSent.Load(),
		"campaign":        sim.campaign,
		"campaigns_available": availableCampaigns(),
	}
	sim.mu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

// POST /v1/sim/traffic {"action":"start"|"stop","tps":2.0}
func (s *Server) handleSimTraffic(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string  `json:"action"`
		TPS    float64 `json:"tps"`
	}
	_ = decodeBody(r, &body)

	sim := s.sim
	switch body.Action {
	case "start":
		sim.mu.Lock()
		if sim.trafficRunning {
			sim.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"status": "already running", "tps": sim.trafficTPS})
			return
		}
		if body.TPS > 0 && body.TPS <= 25 {
			sim.trafficTPS = body.TPS
		}
		ctx, cancel := context.WithCancel(context.Background())
		sim.trafficCancel = cancel
		sim.trafficRunning = true
		tps := sim.trafficTPS
		sim.mu.Unlock()

		go s.runAmbientTraffic(ctx, tps)
		writeJSON(w, http.StatusOK, map[string]any{"status": "started", "tps": tps})

	case "stop":
		sim.mu.Lock()
		if sim.trafficCancel != nil {
			sim.trafficCancel()
		}
		sim.trafficRunning = false
		sim.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"status": "stopped"})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be start|stop"})
	}
}

// runAmbientTraffic keeps a realistic baseline of ordinary payments flowing between
// established pairs, so the console shows a working payment system rather than an empty
// table punctuated by attacks. These are genuinely ordinary: most clear the trusted-pair
// fast path and are allowed, which is the correct and useful contrast.
func (s *Server) runAmbientTraffic(ctx context.Context, tps float64) {
	s.ensureWarmPairs(ctx)

	interval := time.Duration(float64(time.Second) / tps)
	if interval < 20*time.Millisecond {
		interval = 20 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sim.mu.Lock()
			pairs := s.sim.pairs
			s.sim.mu.Unlock()
			if len(pairs) == 0 {
				continue
			}
			p := pairs[rng.Intn(len(pairs))]
			n := s.sim.ambientSent.Add(1)

			// Ordinary variation around a household-sized payment, plus the occasional
			// genuinely larger one — real traffic is not uniform, and a detector that only
			// ever sees identical amounts proves nothing.
			amount := int64(20000 + rng.Intn(60000))
			if rng.Float64() < 0.08 {
				amount = int64(150000 + rng.Intn(250000))
			}
			ev := &contract.Event{
				EndToEndID:            fmt.Sprintf("sim-amb-%d-%d", time.Now().UnixNano(), n),
				Rail:                  contract.RailUPI,
				DebtorAccount:         p[0],
				CreditorAccount:       p[1],
				InstructedAmountMinor: amount,
				Initiation:            "INTENT",
				DeviceID:              deviceFor(p[0]), GeoCell: geoCellFor(p[0]),
			}
			if _, _, err := s.DecideAndPersist(ctx, ev); err != nil {
				return // context cancelled or the engine is genuinely unhappy — stop, don't spin
			}
		}
	}
}

// ensureWarmPairs builds the established relationships the ambient traffic needs. Done once
// per process, through the real decision path, so the shared history is real accumulated
// state in Redis rather than a flag someone set.
func (s *Server) ensureWarmPairs(ctx context.Context) {
	s.sim.mu.Lock()
	if s.sim.warmed {
		s.sim.mu.Unlock()
		return
	}
	s.sim.warmed = true
	s.sim.mu.Unlock()

	run := time.Now().UnixNano()
	var pairs [][2]string
	for i := 0; i < ambientPairs; i++ {
		payer := fmt.Sprintf("BANK_A-%s-CUST%02d-%d", simAccountPrefix, i, run%100000)
		payee := fmt.Sprintf("BANK_B-%s-MERCH%02d-%d", simAccountPrefix, i%8, run%100000)
		for j := 0; j < warmupPerPair; j++ {
			ev := &contract.Event{
				EndToEndID:            fmt.Sprintf("sim-warm-%d-%d-%d", run, i, j),
				Rail:                  contract.RailUPI,
				DebtorAccount:         payer,
				CreditorAccount:       payee,
				InstructedAmountMinor: int64(30000 + j*2000),
				Initiation:            "INTENT",
				DeviceID:              deviceFor(payer), GeoCell: geoCellFor(payer),
			}
			if _, _, err := s.DecideAndPersist(ctx, ev); err != nil {
				return
			}
		}
		pairs = append(pairs, [2]string{payer, payee})
	}
	s.sim.mu.Lock()
	s.sim.pairs = pairs
	s.sim.mu.Unlock()
}

func deviceFor(account string) string { return "dev-" + account }

// ── attack campaigns ────────────────────────────────────────────────────────

type campaignSpec struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Expect      string `json:"expect"`
	Steps       int    `json:"steps"`
}

func availableCampaigns() []campaignSpec {
	return []campaignSpec{
		{Kind: "app_scam", Label: "APP scam wave", Steps: 12,
			Description: "Victims are talked into paying a new beneficiary they have never paid before, in the amount band these scams actually use.",
			Expect:      "Warned with confirmation required — the payer is not a criminal, so we interrupt rather than block."},
		{Kind: "mule_fanout", Label: "Mule fan-out ring", Steps: 18,
			Description: "Many first-time payers push money into one collection account within minutes, several sharing a device.",
			Expect:      "Rising risk on the beneficiary plus a non-zero ring score — advisory, never an automatic block."},
		{Kind: "ato_burst", Label: "Account takeover burst", Steps: 10,
			Description: "One account, suddenly on an unrecognised device from a distant location, emptying out in rapid succession.",
			Expect:      "Escalating friction as velocity and device novelty compound."},
		{Kind: "card_testing", Label: "Card / credential testing", Steps: 20,
			Description: "Rapid low-value probes to find which credentials still work before the real charge.",
			Expect:      "Velocity rails fire well before the large follow-up transaction."},
		{Kind: "smurfing", Label: "Structuring / smurfing", Steps: 14,
			Description: "One payer splits a large sum across many brand-new beneficiaries to stay under per-payment attention.",
			Expect:      "New-beneficiary signals fire repeatedly; the pattern is visible even though each payment is small."},
	}
}

// POST /v1/sim/attack/{kind} — launches a campaign that unfolds over seconds so it can be
// watched, not a batch that completes before anyone looks up.
func (s *Server) handleSimAttack(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	var spec *campaignSpec
	for _, c := range availableCampaigns() {
		if c.Kind == kind {
			cc := c
			spec = &cc
			break
		}
	}
	if spec == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown campaign " + kind})
		return
	}

	sim := s.sim
	sim.mu.Lock()
	if sim.campaign != nil && sim.campaign.Running {
		running := sim.campaign.Kind
		sim.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "campaign " + running + " is already running"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sim.campaignCancel = cancel
	sim.campaign = &campaignProgress{
		Kind: spec.Kind, Label: spec.Label, Total: spec.Steps,
		Running: true, StartedMs: time.Now().UnixMilli(), Narrative: spec.Description,
	}
	sim.mu.Unlock()

	go s.runCampaign(ctx, *spec)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "launched", "campaign": spec})
}

// POST /v1/sim/attack/stop
func (s *Server) handleSimAttackStop(w http.ResponseWriter, r *http.Request) {
	sim := s.sim
	sim.mu.Lock()
	if sim.campaignCancel != nil {
		sim.campaignCancel()
	}
	if sim.campaign != nil {
		sim.campaign.Running = false
	}
	sim.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) runCampaign(ctx context.Context, spec campaignSpec) {
	defer func() {
		s.sim.mu.Lock()
		if s.sim.campaign != nil {
			s.sim.campaign.Running = false
		}
		final := s.sim.campaign
		s.sim.mu.Unlock()
		s.hub.Publish("campaign", final)
	}()

	run := time.Now().UnixNano()
	events := buildCampaignEvents(spec.Kind, run, spec.Steps)

	// Paced so a human can follow it. This is presentation, not throttling: the engine
	// decides each one in single-digit milliseconds regardless.
	pace := 550 * time.Millisecond

	for i, ev := range events {
		select {
		case <-ctx.Done():
			return
		case <-time.After(pace):
		}
		d, _, err := s.DecideAndPersist(ctx, ev)
		if err != nil {
			return
		}
		s.sim.attackSent.Add(1)

		s.sim.mu.Lock()
		if s.sim.campaign != nil {
			s.sim.campaign.Sent = i + 1
			s.sim.campaign.ValueAtRiskMinor += ev.InstructedAmountMinor
			if d.Action == contract.ActionAllow || d.Action == contract.ActionAllowMonitor {
				s.sim.campaign.Allowed++
			} else {
				s.sim.campaign.Challenged++
				s.sim.campaign.ValueStoppedMinor += ev.InstructedAmountMinor
			}
		}
		snapshot := *s.sim.campaign
		s.sim.mu.Unlock()

		// A second SSE channel so the console can show campaign progress without having to
		// infer it from the decision stream.
		s.hub.Publish("campaign", &snapshot)
	}
}

// buildCampaignEvents scripts the *inputs* of each attack. It scripts nothing about the
// outcome — what the engine makes of these is genuinely up to the engine.
func buildCampaignEvents(kind string, run int64, steps int) []*contract.Event {
	id := func(i int) string { return fmt.Sprintf("sim-atk-%s-%d-%d", kind, run, i) }
	acct := func(f string, a ...any) string {
		return fmt.Sprintf("BANK_A-"+simAccountPrefix+"-"+f, a...)
	}
	var out []*contract.Event

	switch kind {
	case "app_scam":
		// Each victim is a different person paying a different fresh beneficiary — this is
		// what a scam wave looks like from the bank's side, not one account misbehaving.
		for i := 0; i < steps; i++ {
			victim := acct("VICTIM%02d-%d", i, run%100000)
			mule := acct("SCAM%02d-%d", i, run%100000)
			out = append(out, &contract.Event{
				EndToEndID: id(i), Rail: contract.RailUPI,
				DebtorAccount: victim, CreditorAccount: mule,
				InstructedAmountMinor: int64(210000 + i*18000), // inside the scam-typical band
				Initiation:            "INTENT", DeviceID: deviceFor(victim), GeoCell: geoCellFor(victim),
			})
		}

	case "mule_fanout":
		// A real fan-out ring has two structural tells beyond the fan-in itself, and the
		// simulation has to reproduce both or it is not the attack it claims to be:
		// a small number of operators driving many "payer" accounts (heavy device reuse),
		// and money that does not stop at the collection account but is forwarded onward
		// within seconds. Both are what the graph detector keys on.
		mule := acct("MULE-%d", run%100000)
		operators := []string{
			fmt.Sprintf("dev-op1-%d", run),
			fmt.Sprintf("dev-op2-%d", run),
			fmt.Sprintf("dev-op3-%d", run),
		}
		fanIn := steps
		if fanIn > 4 {
			fanIn = steps - 3 // leave room for the cash-out chain below
		}
		for i := 0; i < fanIn; i++ {
			payer := acct("FAN%02d-%d", i, run%100000)
			out = append(out, &contract.Event{
				EndToEndID: id(i), Rail: contract.RailUPI,
				DebtorAccount: payer, CreditorAccount: mule,
				InstructedAmountMinor: int64(140000 + (i%5)*15000),
				Initiation:            "INTENT",
				// Three handsets between all of them: the single hardest thing for a ring to
				// hide and the strongest small-cluster signal we have.
				DeviceID: operators[i%len(operators)],
			})
		}
		// Cash-out chain: collection account forwards onward, and that account forwards
		// again. This is what makes hops_to_cashout non-zero and separates a mule from a
		// merchant that simply receives from many people.
		hop1 := acct("MULEHOP1-%d", run%100000)
		hop2 := acct("MULEHOP2-%d", run%100000)
		out = append(out,
			&contract.Event{EndToEndID: id(fanIn), Rail: contract.RailUPI,
				DebtorAccount: mule, CreditorAccount: hop1, InstructedAmountMinor: 900000,
				Initiation: "INTENT", DeviceID: operators[0]},
			&contract.Event{EndToEndID: id(fanIn + 1), Rail: contract.RailUPI,
				DebtorAccount: hop1, CreditorAccount: hop2, InstructedAmountMinor: 850000,
				Initiation: "INTENT", DeviceID: operators[1]},
			&contract.Event{EndToEndID: id(fanIn + 2), Rail: contract.RailUPI,
				DebtorAccount: hop2, CreditorAccount: acct("CASHOUT-%d", run%100000),
				InstructedAmountMinor: 820000, Initiation: "INTENT", DeviceID: operators[2]},
		)

	case "ato_burst":
		victim := acct("ATO-%d", run%100000)
		newDevice := fmt.Sprintf("dev-attacker-%d", run)
		for i := 0; i < steps; i++ {
			out = append(out, &contract.Event{
				EndToEndID: id(i), Rail: contract.RailUPI,
				DebtorAccount: victim, CreditorAccount: acct("ATOOUT%02d-%d", i, run%100000),
				InstructedAmountMinor: int64(90000 + i*45000), // draining, escalating
				Initiation:            "INTENT", DeviceID: newDevice,
				IP: "203.0.113.44", GeoCell: "far-cell-9",
			})
		}

	case "card_testing":
		attacker := acct("PROBE-%d", run%100000)
		dev := fmt.Sprintf("dev-probe-%d", run)
		for i := 0; i < steps; i++ {
			amt := int64(200 + i*50) // tiny probes...
			if i == steps-1 {
				amt = 480000 // ...then the real one
			}
			out = append(out, &contract.Event{
				EndToEndID: id(i), Rail: contract.RailUPI,
				DebtorAccount: attacker, CreditorAccount: acct("PROBEDST%02d-%d", i%3, run%100000),
				InstructedAmountMinor: amt, Initiation: "INTENT", DeviceID: dev,
			})
		}

	case "smurfing":
		payer := acct("SMURF-%d", run%100000)
		for i := 0; i < steps; i++ {
			out = append(out, &contract.Event{
				EndToEndID: id(i), Rail: contract.RailUPI,
				DebtorAccount: payer, CreditorAccount: acct("SMURFDST%02d-%d", i, run%100000),
				InstructedAmountMinor: int64(185000 + (i%3)*7000),
				Initiation:            "INTENT", DeviceID: deviceFor(payer), GeoCell: geoCellFor(payer),
			})
		}
	}
	return out
}
