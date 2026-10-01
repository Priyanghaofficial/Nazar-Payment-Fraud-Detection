package contract

import "strings"

// TopReason and SourceFromID are the one-line summaries a table row needs. They live here,
// beside the types they read, because two different paths produce that row — the live SSE
// broadcast and the Postgres history query that hydrates a freshly-opened tab — and a row
// that explains itself differently depending on which path produced it is a bug waiting to
// be demoed.
//
// The full ranked explanation is a separate, heavier call (GET /v1/decisions/{id}/explain).
// This is deliberately the cheap version: it must be safe to compute on the hot path.

var reasonRank = map[string]int{
	"local_blocklist": 0,
	"rule:RAIL-001":   1,
	"rule:RAIL-101":   2,
	"rule:RAIL-102":   2,
	"graph":           3,
	"novelty":         4,
	"model":           5,
	"trusted_pair":    6,
}

var reasonLabel = map[string]string{
	"local_blocklist": "Beneficiary on confirmed-fraud list",
	"rule:RAIL-001":   "Regulatory cooling period on a new beneficiary",
	"rule:RAIL-101":   "Payment rate far above this payer's baseline",
	"rule:RAIL-102":   "First payment to a new beneficiary",
	"graph":           "Beneficiary sits in a suspected mule network",
	"novelty":         "Unlike anything seen in recent traffic",
	"model":           "Learned model scored this elevated",
	"trusted_pair":    "Established relationship, routine amount",
}

// TopReason returns the most important thing that fired, in one clause.
func TopReason(findings []Finding, action Action) string {
	best, bestRank := "", 99
	for _, f := range findings {
		if f.Status != StatusFired {
			continue
		}
		if r, ok := reasonRank[f.SignalID]; ok && r < bestRank {
			bestRank, best = r, reasonLabel[f.SignalID]
		}
	}
	if best != "" {
		return best
	}
	if action == ActionAllow || action == ActionAllowMonitor {
		return "Nothing unusual"
	}
	return "Cost of allowing exceeded cost of challenging"
}

// SourceFromID labels where a payment came from, so an audience can tell the judge's own
// handset apart from background traffic at a glance. Derived from the id prefix stamped by
// whichever path produced it — never a field a caller can set.
func SourceFromID(e2eID string) string {
	switch {
	case strings.HasPrefix(e2eID, "sim-atk-"):
		return "attack"
	case strings.HasPrefix(e2eID, "sim-"):
		return "ambient"
	case strings.HasPrefix(e2eID, "judge-"):
		return "judge"
	case strings.HasPrefix(e2eID, "demo-"):
		return "scenario"
	}
	return "api"
}
