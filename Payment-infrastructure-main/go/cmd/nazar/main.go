// cmd/nazar is the P0 monolith: one binary, goroutine pools instead of five services
// (docs/00 §5's explicit P0 note). The seam interfaces are the split ones from day one, so
// separating into decision/profile-apply/graph/casework later is a deployment change, not a
// rewrite.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"nazar/internal/consortium"
	"nazar/internal/decide"
	"nazar/internal/fanout"
	"nazar/internal/features"
	"nazar/internal/graph"
	"nazar/internal/narrate"
	"nazar/internal/novelty"
	"nazar/internal/obs"
	"nazar/internal/persist"
	"nazar/internal/profile"
	"nazar/internal/rules"
	"nazar/internal/wal"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	repoRoot, err := features.FindRepoRoot(".")
	if err != nil {
		log.Fatalf("nazar: %v", err)
	}
	log.Printf("nazar: repo root = %s", repoRoot)

	redisAddr := env("REDIS_ADDR", "localhost:6379")
	pgDSN := env("POSTGRES_DSN", "postgres://nazar:nazar@localhost:5432/nazar?sslmode=disable")
	port := env("PORT", "8080")
	dataDir := env("NAZAR_DATA_DIR", filepath.Join(repoRoot, "data"))
	_ = os.MkdirAll(dataDir, 0755)

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		log.Fatalf("nazar: opening postgres: %v", err)
	}
	db.SetMaxOpenConns(20)

	profileStore := profile.NewRedisProfileStore(rdb)

	rulesEngine, err := rules.LoadEngine(repoRoot, "2026-08-14.001.yaml")
	if err != nil {
		log.Fatalf("nazar: loading rules: %v", err)
	}
	policy, err := decide.LoadPolicy(repoRoot, "2026-08-14.001.yaml")
	if err != nil {
		log.Fatalf("nazar: loading policy: %v", err)
	}

	blocklist := decide.NewBlocklist()
	if err := blocklist.Refresh(context.Background(), db); err != nil {
		log.Printf("nazar: blocklist refresh (non-fatal, likely first boot): %v", err)
	}

	scorer, scorerLabel := loadScorer(repoRoot)
	calibrator, calibratorLabel := loadCalibrator(repoRoot)
	prevalence := loadPrevalence(repoRoot)
	log.Printf("nazar: scorer=%s calibrator=%s prevalence(train=%.4f natural=%.4f v=%s)",
		scorerLabel, calibratorLabel, prevalence.TrainPrevalence, prevalence.NaturalPrevalence, prevalence.Version)

	graphEngine := graph.NewEngine()
	noveltyEngine := novelty.NewEngine()
	tokeniser := consortium.NewTokeniser([]byte(env("NAZAR_CONSORTIUM_PEPPER", "demo-pepper-epoch-1-not-for-production")))
	consortiumRegistry := consortium.NewRegistry(db, tokeniser)

	// The hot-swap slot the Policy Studio drives. Starts holding the approved on-disk
	// bundle, so behaviour is identical until someone deliberately tunes it.
	policyRef := decide.NewPolicyRef(policy)

	engine := &decide.Engine{
		Policy: policy, Rules: rulesEngine, Scorer: scorer, Calibrator: calibrator,
		Prevalence: prevalence, Blocklist: blocklist, Live: policyRef,
		ModelBundleVersion: scorer.Meta().BundleVersion, PolicyVersion: policy.Version,
		RulesVersion: rulesEngine.Version, SignalRegistryVersion: "2026-08-14.001",
	}

	w, err := wal.Open(filepath.Join(dataDir, "wal.ndjson"))
	if err != nil {
		log.Fatalf("nazar: opening WAL: %v", err)
	}
	defer w.Close()

	if tipSeq, tipHash, err := persist.GetChainTip(context.Background(), db, 0); err != nil {
		log.Printf("nazar: could not read audit chain tip (starting a fresh chain): %v", err)
	} else if tipSeq > 0 {
		w.ResumeChain(tipSeq, tipHash)
		log.Printf("nazar: resumed audit chain at seq=%d", tipSeq)
	}

	sink := persist.NewPostgresSink(db)
	shipper := persist.NewShipper(sink, 4096)

	// Reconciliation (docs/01 §6.1): anything durably WAL'd but never shipped (e.g. the
	// process was killed between WAL.Append and the shipper draining it) gets a second
	// chance here. Safe to repeat: the decisions table's ON CONFLICT DO NOTHING makes this
	// idempotent even if some of these already made it to Postgres.
	if walDecisions, err := wal.Replay(filepath.Join(dataDir, "wal.ndjson")); err != nil {
		log.Printf("nazar: WAL replay on boot failed (continuing without reconciliation): %v", err)
	} else if len(walDecisions) > 0 {
		log.Printf("nazar: reconciling %d WAL'd decisions into Postgres", len(walDecisions))
		for _, d := range walDecisions {
			shipper.Enqueue(d)
		}
	}

	hub := fanout.NewHub()
	latency := obs.NewLatencyTracker(10000)

	depHealth := obs.NewDependencyHealth(rdb, db)

	srv := &Server{
		repoRoot: repoRoot, rdb: rdb, db: db, profileStore: profileStore,
		engine: engine, wal: w, shipper: shipper, hub: hub, latency: latency,
		blocklist: blocklist, health: depHealth, graph: graphEngine, novelty: noveltyEngine,
		consortium: consortiumRegistry,
		policy: policy, redisContainer: env("NAZAR_REDIS_CONTAINER", "nazar-redis"),
		containerEngine: env("NAZAR_CONTAINER_ENGINE", "podman"),
		sim: newSimulator(), narrator: narrate.FromEnv(),
		policyRef: policyRef, basePolicy: policy,
	}
	log.Printf("nazar: analyst narrator = %+v", srv.narrator.Meta())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go shipper.Run(ctx)

	mux := http.NewServeMux()
	srv.Routes(mux)

	httpSrv := &http.Server{Addr: ":" + port, Handler: withCORS(mux)}
	go func() {
		log.Printf("nazar: listening on :%s", port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("nazar: http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("nazar: shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	cancel()
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

