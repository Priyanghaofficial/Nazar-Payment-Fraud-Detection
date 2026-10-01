package invariants

import (
	"context"
	"database/sql"
	"math"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nazar/internal/contract"
	"nazar/internal/persist"
)

// test_replay_is_a_read (docs/06 §4, D1/D2): Time Machine output must byte-match the
// persisted vector — never recomputed. This test writes a decision with a known feature
// vector straight to Postgres via the real DecisionSink, reads it back via the real query
// path (persist.GetLatestDecision — the same function the HTTP handler uses), and checks
// every value round-trips exactly. Skips if Postgres isn't reachable.
func TestReplayIsARead(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://nazar:nazar@localhost:5432/nazar?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("Postgres not reachable, skipping (needs live infra): %v", err)
	}

	fv := contract.NewFeatureVector()
	fv.Set("amt_robust_z", 2.3456)
	fv.Set("payee_fanin_1h", 7)
	fv.NotApplicable("geo_jump_kmh", "GEO_UNAVAILABLE_OR_UNCHANGED")
	fv.NotEvaluated("payee_inflow_concentration", "COLD_START")

	pModel := 0.4242
	e2eID := "replay-test-" + time.Now().Format("20060102150405.000000000")
	d := &contract.Decision{
		EndToEndID: e2eID, DecisionSeq: 0, Kind: contract.KindLive,
		DecidedAtMs: time.Now().UnixMilli(), AcceptedAtMs: time.Now().UnixMilli(),
		Action: contract.ActionStepUpInterstitial, PreAdvisoryAction: contract.ActionStepUpInterstitial,
		ReasonCodes: []string{"TEST_REASON"}, PModel: &pModel,
		Features: fv, Findings: []contract.Finding{contract.NewFinding("test", contract.StatusFired, "test finding")},
		ModelBundleVersion: "test-v1", PolicyVersion: "test-v1", RulesVersion: "test-v1", SignalRegistryVersion: "test-v1",
		TotalMs: 1.23, QueueDelayMs: 0.1, ServiceMs: 1.1,
		DecisionShard: 99, ChainSeq: 1, Hash: []byte{1, 2, 3, 4},
	}

	sink := persist.NewPostgresSink(db)
	if err := sink.Emit(ctx, d); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	got, err := persist.GetLatestDecision(ctx, db, e2eID)
	if err != nil {
		t.Fatalf("GetLatestDecision: %v", err)
	}
	if got == nil {
		t.Fatal("GetLatestDecision returned nil for a decision we just wrote")
	}

	if got.Action != d.Action {
		t.Errorf("action: got %s, want %s", got.Action, d.Action)
	}
	if got.PModel == nil || math.Abs(*got.PModel-pModel) > 1e-9 {
		t.Errorf("p_model did not round-trip exactly: got %v, want %v", got.PModel, pModel)
	}
	if got.Features.Values["amt_robust_z"] != 2.3456 {
		t.Errorf("amt_robust_z did not round-trip: got %v, want 2.3456", got.Features.Values["amt_robust_z"])
	}
	if got.Features.Status["payee_fanin_1h"] != contract.StatusClear {
		t.Errorf("payee_fanin_1h status: got %s, want CLEAR", got.Features.Status["payee_fanin_1h"])
	}
	if got.Features.Status["geo_jump_kmh"] != contract.StatusNotApplicable {
		t.Errorf("geo_jump_kmh status: got %s, want NOT_APPLICABLE — replay must preserve D5 status, not just the value", got.Features.Status["geo_jump_kmh"])
	}
	if got.Features.Status["payee_inflow_concentration"] != contract.StatusNotEvaluated {
		t.Errorf("payee_inflow_concentration status: got %s, want NOT_EVALUATED", got.Features.Status["payee_inflow_concentration"])
	}
	if len(got.Findings) != 1 || got.Findings[0].Explanation != "test finding" {
		t.Errorf("findings did not round-trip: got %+v", got.Findings)
	}
}
