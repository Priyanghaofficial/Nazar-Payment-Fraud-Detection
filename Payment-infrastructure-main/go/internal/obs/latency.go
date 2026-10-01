// Package obs implements the three-number latency discipline (docs/01 §1) and a lightweight
// percentile tracker. P0 simplification: a fixed-size ring buffer of recent samples, sorted
// on read, in place of a true HDR histogram — real percentiles over real measured samples,
// just O(n log n) on read instead of O(1). Fine at demo QPS; the interface is what an HDR
// swap would sit behind.
package obs

import (
	"sort"
	"sync"
)

type Sample struct {
	TotalMs      float64
	QueueDelayMs float64
	ServiceMs    float64
}

type LatencyTracker struct {
	mu      sync.Mutex
	samples []Sample
	cap     int
	next    int
	filled  bool
}

func NewLatencyTracker(capacity int) *LatencyTracker {
	return &LatencyTracker{samples: make([]Sample, capacity), cap: capacity}
}

func (t *LatencyTracker) Record(s Sample) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.samples[t.next] = s
	t.next = (t.next + 1) % t.cap
	if t.next == 0 {
		t.filled = true
	}
}

type Percentiles struct {
	N      int     `json:"n"`
	P50    float64 `json:"p50"`
	P90    float64 `json:"p90"`
	P99    float64 `json:"p99"`
	P999   float64 `json:"p999"`
	Max    float64 `json:"max"`
}

func (t *LatencyTracker) Snapshot() Percentiles {
	t.mu.Lock()
	n := t.next
	if t.filled {
		n = t.cap
	}
	vals := make([]float64, n)
	for i := 0; i < n; i++ {
		vals[i] = t.samples[i].TotalMs
	}
	t.mu.Unlock()

	sort.Float64s(vals)
	if n == 0 {
		return Percentiles{}
	}
	return Percentiles{
		N:    n,
		P50:  pct(vals, 0.50),
		P90:  pct(vals, 0.90),
		P99:  pct(vals, 0.99),
		P999: pct(vals, 0.999),
		Max:  vals[n-1],
	}
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

// Reset discards every recorded sample. Used by the demo reset so the p50/p99 shown belong
// to the run being demonstrated rather than to whatever load happened before it — a latency
// figure averaged over a previous scenario is a wrong number, not a stale one.
func (t *LatencyTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.samples = make([]Sample, t.cap)
	t.next = 0
	t.filled = false
}
