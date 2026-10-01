package decide

import (
	"context"
	"time"

	"nazar/internal/contract"
)

// LoadProfileWithDeadline enforces Nazar's self-deadline on the profile load (docs/01 §2:
// "the most important section... there is no reason for Nazar to ever time out"). It never
// returns an error — a slow or failed store degrades to an empty bundle, which the caller
// treats as fully degraded, rather than the request hanging or erroring. Extracted as its
// own function (rather than inlined in the HTTP handler) specifically so
// prop_deadline_always_answered can exercise it against an injected slow ProfileStore
// without needing a live HTTP server.
func LoadProfileWithDeadline(ctx context.Context, store contract.ProfileStore, ev *contract.Event, deadline time.Duration) *contract.ProfileBundle {
	loadCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	pb, err := store.Load(loadCtx, ev)
	if err != nil || pb == nil {
		return &contract.ProfileBundle{}
	}
	return pb
}
