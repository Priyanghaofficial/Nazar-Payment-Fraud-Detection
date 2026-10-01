package invariants

import (
	"context"
	"testing"
	"time"

	"nazar/internal/contract"
	"nazar/internal/decide"
)

// slowProfileStore simulates a dependency that is much slower than the deadline, but
// respects context cancellation (as a real network client does) rather than blocking
// obliviously — this is the realistic failure mode a timeout mechanism must handle.
type slowProfileStore struct {
	delay time.Duration
}

func (s *slowProfileStore) Load(ctx context.Context, ev *contract.Event) (*contract.ProfileBundle, error) {
	select {
	case <-time.After(s.delay):
		return &contract.ProfileBundle{Payer: contract.PayerBundle{Present: true}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *slowProfileStore) Apply(ctx context.Context, ev *contract.Event) error { return nil }

// prop_deadline_always_answered (docs/06 §4, docs/01 §2): under any injected dependency
// latency, a decision is always returned within Nazar's self-enforced deadline — never
// hangs waiting on a slow store.
func TestPropDeadlineAlwaysAnswered(t *testing.T) {
	store := &slowProfileStore{delay: 5 * time.Second} // deliberately far beyond any sane deadline
	deadline := 20 * time.Millisecond

	ev := &contract.Event{EndToEndID: "deadline-test", AcceptedAtMs: time.Now().UnixMilli()}

	start := time.Now()
	pb := decide.LoadProfileWithDeadline(context.Background(), store, ev, deadline)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("LoadProfileWithDeadline took %v against a %v deadline and a 5s-slow store — it did not enforce the deadline", elapsed, deadline)
	}
	if pb == nil {
		t.Fatal("LoadProfileWithDeadline returned nil — callers must always get a usable (possibly empty) bundle")
	}
	if pb.Payer.Present {
		t.Fatal("got the slow store's real bundle instead of the degraded empty one — the deadline did not actually cut the call off")
	}
}

// The complementary case: a store that responds well within the deadline is used as-is.
func TestPropDeadline_FastStoreIsUsed(t *testing.T) {
	store := &slowProfileStore{delay: 1 * time.Millisecond}
	ev := &contract.Event{EndToEndID: "fast-test", AcceptedAtMs: time.Now().UnixMilli()}

	pb := decide.LoadProfileWithDeadline(context.Background(), store, ev, 20*time.Millisecond)
	if !pb.Payer.Present {
		t.Fatal("a fast store's real result was discarded — the deadline should only degrade on an actual timeout")
	}
}
