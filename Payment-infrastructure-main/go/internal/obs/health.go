package obs

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"
)

// DependencyHealth answers the "up vs up-and-non-degraded" question docs/01 §2 requires of
// /healthz, and backs the console's resilience screen (docs/07 screen 6).
type DependencyHealth struct {
	rdb *redis.Client
	db  *sql.DB
}

func NewDependencyHealth(rdb *redis.Client, db *sql.DB) *DependencyHealth {
	return &DependencyHealth{rdb: rdb, db: db}
}

type Status struct {
	Redis    DepStatus `json:"redis"`
	Postgres DepStatus `json:"postgres"`
}

type DepStatus struct {
	Up        bool    `json:"up"`
	LatencyMs float64 `json:"latency_ms"`
	Error     string  `json:"error,omitempty"`
}

func (h *DependencyHealth) Check(ctx context.Context) Status {
	// Each dependency gets its OWN deadline — sharing one budget across sequential checks
	// means a slow/hung first check silently starves the second's context before it even
	// starts, which would misreport Postgres as down while only Redis is unhealthy.
	var s Status

	redisCtx, redisCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	t0 := time.Now()
	if err := h.rdb.Ping(redisCtx).Err(); err != nil {
		s.Redis = DepStatus{Up: false, Error: err.Error()}
	} else {
		s.Redis = DepStatus{Up: true, LatencyMs: msSince(t0)}
	}
	redisCancel()

	pgCtx, pgCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	t1 := time.Now()
	if err := h.db.PingContext(pgCtx); err != nil {
		s.Postgres = DepStatus{Up: false, Error: err.Error()}
	} else {
		s.Postgres = DepStatus{Up: true, LatencyMs: msSince(t1)}
	}
	pgCancel()

	return s
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Microseconds()) / 1000.0
}
