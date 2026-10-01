package main

import (
	"database/sql"
	"sync"

	"github.com/redis/go-redis/v9"

	"nazar/internal/consortium"
	"nazar/internal/decide"
	"nazar/internal/fanout"
	"nazar/internal/graph"
	"nazar/internal/narrate"
	"nazar/internal/novelty"
	"nazar/internal/obs"
	"nazar/internal/persist"
	"nazar/internal/profile"
	"nazar/internal/wal"
)

// Server holds every dependency the HTTP handlers need. Nothing here is scoring-path state
// beyond what decide.Engine already owns — this struct exists to give handlers a receiver.
type Server struct {
	repoRoot     string
	rdb          *redis.Client
	db           *sql.DB
	profileStore *profile.RedisProfileStore
	engine       *decide.Engine
	wal          *wal.WAL
	shipper      *persist.Shipper
	hub          *fanout.Hub
	latency      *obs.LatencyTracker
	blocklist    *decide.Blocklist
	health       *obs.DependencyHealth
	graph        *graph.Engine
	novelty      *novelty.Engine
	consortium   *consortium.Registry
	policy       *decide.Policy

	// Live-demo surfaces (all off the scoring path).
	sim        *simulator      // ambient traffic + attack campaigns
	narrator   narrate.Narrator // analyst write-ups, on demand only
	policyRef  *decide.PolicyRef // hot-swap slot the Policy Studio drives
	basePolicy *decide.Policy    // the approved on-disk bundle, for reset

	judgeMu      sync.Mutex
	judgeSession *judgeSessionResponse // most recent QR session, so the console can follow it

	redisContainer  string
	containerEngine string // "podman" or "docker", read from NAZAR_CONTAINER_ENGINE
}
