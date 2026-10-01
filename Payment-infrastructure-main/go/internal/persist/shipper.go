package persist

import (
	"context"
	"log"
	"sync/atomic"

	"nazar/internal/contract"
)

// Shipper is the bounded async lane (docs/00 §4, docs/01 §4.5): decisions that are already
// durable in the WAL get queued here and drained into Postgres off the request path. On
// overflow it drops to WAL-only and raises AsyncShedTotal rather than blocking the caller
// or losing durability — the WAL copy is never at risk, only freshness in Postgres/the UI.
type Shipper struct {
	sink    contract.DecisionSink
	queue   chan *contract.Decision
	AsyncShedTotal atomic.Int64
	QueueDepth     atomic.Int64
}

func NewShipper(sink contract.DecisionSink, bufferSize int) *Shipper {
	s := &Shipper{sink: sink, queue: make(chan *contract.Decision, bufferSize)}
	return s
}

// Enqueue never blocks. On a full queue it drops and counts the shed — the decision is
// still safe in the WAL (docs/00 §8: "Async lane saturated -> freshness of graph/cases/UI"
// is the only thing that degrades).
func (s *Shipper) Enqueue(d *contract.Decision) {
	select {
	case s.queue <- d:
		s.QueueDepth.Add(1)
	default:
		s.AsyncShedTotal.Add(1)
		log.Printf("persist: async shipper queue full, shedding end_to_end_id=%s (WAL copy intact)", d.EndToEndID)
	}
}

// Run drains the queue until ctx is cancelled. Call it once, in its own goroutine — this
// IS the separate goroutine pool docs/00 §4 requires so broadcast/persist serialisation
// never touches the scoring heap.
func (s *Shipper) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-s.queue:
			s.QueueDepth.Add(-1)
			if err := s.sink.Emit(ctx, d); err != nil {
				log.Printf("persist: shipper emit failed for end_to_end_id=%s: %v (WAL copy retained for reconciliation)", d.EndToEndID, err)
			}
		}
	}
}
