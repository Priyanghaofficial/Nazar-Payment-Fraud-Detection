package persist

import (
	"context"
	"sync"

	"nazar/internal/contract"
)

// FakeSink is the deterministic in-memory DecisionSink used in tests (docs/00 §3.2).
type FakeSink struct {
	mu       sync.Mutex
	Emitted  []*contract.Decision
}

func NewFakeSink() *FakeSink { return &FakeSink{} }

func (f *FakeSink) Emit(ctx context.Context, d *contract.Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Emitted = append(f.Emitted, d)
	return nil
}
