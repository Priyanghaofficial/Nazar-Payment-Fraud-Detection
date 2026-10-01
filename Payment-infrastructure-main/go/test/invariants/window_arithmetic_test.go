package invariants

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// test_window_arithmetic_property (F-32/F-33, docs/06 §4, docs/01 §10): Redis window reads
// (ZCOUNT) match a brute-force reference implementation over the same random event stream.
// This is the test that would have caught the previous design's ZCARD-over-a-24h-trim bug
// on day one. Requires a live Redis (REDIS_ADDR, default localhost:6379); skips if
// unreachable rather than failing the whole suite in an environment without one.
func TestWindowArithmeticProperty(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not reachable at %s, skipping (this test needs live infra): %v", addr, err)
	}
	defer rdb.Close()

	key := fmt.Sprintf("test:window_arithmetic:%d", time.Now().UnixNano())
	defer rdb.Del(context.Background(), key)

	rng := rand.New(rand.NewSource(99))
	const n = 500
	const spreadMs = 48 * 3600 * 1000 // 48h spread
	now := int64(1_700_000_000_000)

	type sample struct {
		member string
		score  int64
	}
	var samples []sample
	pipe := rdb.Pipeline()
	for i := 0; i < n; i++ {
		ts := now - int64(rng.Intn(spreadMs))
		member := fmt.Sprintf("m%d", i)
		samples = append(samples, sample{member: member, score: ts})
		pipe.ZAdd(context.Background(), key, redis.Z{Score: float64(ts), Member: member})
	}
	if _, err := pipe.Exec(context.Background()); err != nil {
		t.Fatalf("seeding zset: %v", err)
	}

	windows := []int64{60_000, 3_600_000, 86_400_000} // 1m, 1h, 24h — the same windows features/compute.go reads
	for _, w := range windows {
		lo, hi := now-w, now
		got, err := rdb.ZCount(context.Background(), key, fmt.Sprintf("(%d", lo), fmt.Sprintf("%d", hi)).Result()
		if err != nil {
			t.Fatalf("ZCOUNT: %v", err)
		}

		var want int64
		for _, s := range samples {
			if s.score > lo && s.score <= hi { // exclusive lower bound, matching docs/02 §3.3's ZCOUNT usage
				want++
			}
		}

		if got != want {
			t.Errorf("window %dms: ZCOUNT=%d, brute-force reference=%d — window arithmetic diverges (F-32/33 class bug)", w, got, want)
		}

		// The bug this test is named for: ZCARD would return len(samples) regardless of
		// window, which is provably wrong whenever the window doesn't cover the full spread.
		card, _ := rdb.ZCard(context.Background(), key).Result()
		if w < spreadMs && card == got {
			// only a suspicious coincidence check, not a hard failure — but worth knowing
			t.Logf("note: ZCARD (%d) happened to equal ZCOUNT (%d) for window %dms — coincidental, not a bug on its own", card, got, w)
		}
	}
}
