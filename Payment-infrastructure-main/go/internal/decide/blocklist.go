package decide

import (
	"context"
	"database/sql"
	"sync"
)

// Blocklist is the local, zero-I/O confirmation store (docs/02 §3.5, docs/04 §3.3): the
// only non-regulatory path to BLOCK, and only after an exact match plus an analyst
// disposition with four-eyes approval already recorded in Postgres.
//
// P0 simplification: an exact in-memory map, refreshed from Postgres on an interval, in
// place of the in-process cuckoo filter + confirm-on-hit two-step described at [P1] scale
// (docs/02 §3.5). At ~2k demo accounts the exact map already IS zero I/O and never
// produces a false positive, so the two-step's only purpose — bounding memory at large
// keyspace — doesn't apply yet. The interface (Hit) is what the cuckoo-filter swap would
// sit behind.
type Blocklist struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

type Entry struct {
	Account       string
	List          string // local | consortium | watchlist
	Reason        string
	ReporterCount int
}

func NewBlocklist() *Blocklist {
	return &Blocklist{entries: map[string]Entry{}}
}

// Hit is never authoritative on its own for a probabilistic structure (docs/02 §3.5); here
// it already is exact, so a hit is a hit.
func (b *Blocklist) Hit(account string) (Entry, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	e, ok := b.entries[account]
	return e, ok
}

func (b *Blocklist) Refresh(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT account, list, reason, reporter_count FROM blocklist_entries`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := map[string]Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Account, &e.List, &e.Reason, &e.ReporterCount); err != nil {
			return err
		}
		next[e.Account] = e
	}
	b.mu.Lock()
	b.entries = next
	b.mu.Unlock()
	return rows.Err()
}
