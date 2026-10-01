// Package explain turns a persisted Decision into the answer to the only question a judge,
// an analyst, or a customer actually asks: *why*.
//
// Everything here is derived from data the decision already carries — the feature vector it
// scored, the findings each signal emitted, the signed model contributions, and the policy
// economics that chose the action. Nothing is invented, nothing is re-scored, and no
// sentence is produced for a signal that did not run. A feature whose status is not CLEAR
// becomes a "not evaluated" line, never silence and never a zero.
package explain

import (
	"fmt"
	"math"
	"strings"
)

// phrase renders one feature id into plain English. Title is the human name of the thing
// being measured; Render turns the raw number into a sentence fragment a non-engineer can
// read; Notable decides whether the value is worth putting in front of someone as evidence
// rather than as background.
type phrase struct {
	Title    string
	Render   func(v float64) string
	Notable  func(v float64) bool
	Severity func(v float64) string
	// Family groups related features so the evidence list doesn't show four near-identical
	// velocity lines when one will do.
	Family string
}

func gt(t float64) func(float64) bool  { return func(v float64) bool { return v > t } }
func isTrue(v float64) bool            { return v == 1 }
func never(float64) bool               { return false }
func sev(s string) func(float64) string { return func(float64) string { return s } }

// sevAbove grades a value against two cut points.
func sevAbove(high, critical float64) func(float64) string {
	return func(v float64) string {
		switch {
		case v >= critical:
			return "critical"
		case v >= high:
			return "high"
		default:
			return "medium"
		}
	}
}

func rupees(minor float64) string {
	return "₹" + commas(int64(math.Round(minor/100)))
}

func commas(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	// Indian grouping: last 3 digits, then pairs (12,34,567).
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	head, tail := s[:len(s)-3], s[len(s)-3:]
	var parts []string
	for len(head) > 2 {
		parts = append([]string{head[len(head)-2:]}, parts...)
		head = head[:len(head)-2]
	}
	if head != "" {
		parts = append([]string{head}, parts...)
	}
	out := strings.Join(parts, ",") + "," + tail
	if neg {
		return "-" + out
	}
	return out
}

func num(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e9 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// phrasebook is the whole plain-English surface of the system. Every feature in
// features/registry.yaml that can appear in a decision has an entry; anything missing falls
// back to a generic renderer rather than being dropped (see lookup).
//
// Wording rule from CLAUDE.md #12: we say "first seen by us", never "created" — we do not
// know when an account was opened, only when we first observed it.
var phrasebook = map[string]phrase{
	"amt_robust_z": {
		Title:  "Amount is far outside this payer's normal range",
		Family: "amount",
		Render: func(v float64) string {
			return fmt.Sprintf("%s robust standard deviations above this payer's own median payment", num(v))
		},
		Notable: gt(3), Severity: sevAbove(3, 8),
	},
	"amt_over_p95": {
		Title:  "Amount dwarfs anything this payer normally sends",
		Family: "amount",
		Render: func(v float64) string {
			return fmt.Sprintf("%s× this payer's 95th-percentile payment", num(v))
		},
		Notable: gt(1.5), Severity: sevAbove(2, 5),
	},
	"hour_surprisal": {
		Title:  "Unusual time of day for this payer",
		Family: "time",
		Render: func(v float64) string {
			return fmt.Sprintf("this payer almost never transacts at this hour (%s bits of surprise against their own hourly histogram)", num(v))
		},
		Notable: gt(4), Severity: sevAbove(4, 7),
	},
	"txn_velocity_1m": {
		Title:  "Burst of payments in the last minute",
		Family: "velocity",
		Render: func(v float64) string { return fmt.Sprintf("%s payments in the last 60 seconds", num(v)) },
		Notable: gt(2), Severity: sevAbove(3, 6),
	},
	"txn_velocity_5m": {
		Title:  "Burst of payments in the last five minutes",
		Family: "velocity",
		Render: func(v float64) string { return fmt.Sprintf("%s payments in the last 5 minutes", num(v)) },
		Notable: gt(4), Severity: sevAbove(5, 10),
	},
	"txn_velocity_1h": {
		Title:  "Elevated payment rate this hour",
		Family: "velocity",
		Render: func(v float64) string { return fmt.Sprintf("%s payments in the last hour", num(v)) },
		Notable: gt(8), Severity: sevAbove(10, 20),
	},
	"txn_velocity_24h": {
		Title:  "Elevated payment rate today",
		Family: "velocity",
		Render: func(v float64) string { return fmt.Sprintf("%s payments in the last 24 hours", num(v)) },
		Notable: gt(25), Severity: sevAbove(30, 60),
	},
	"amt_velocity_1h": {
		Title:  "Large value moved in the last hour",
		Family: "velocity_value",
		Render: func(v float64) string { return fmt.Sprintf("%s sent in the last hour", rupees(v)) },
		Notable: gt(500000), Severity: sevAbove(500000, 2000000),
	},
	"amt_velocity_24h": {
		Title:  "Large value moved today",
		Family: "velocity_value",
		Render: func(v float64) string { return fmt.Sprintf("%s sent in the last 24 hours", rupees(v)) },
		Notable: gt(2000000), Severity: sevAbove(2000000, 10000000),
	},
	"account_age_days": {
		Title:  "Account is new to us",
		Family: "tenure",
		Render: func(v float64) string { return fmt.Sprintf("first seen by us %s days ago", num(v)) },
		Notable: func(v float64) bool { return v < 7 }, Severity: sev("medium"),
	},
	"dormancy_days": {
		Title:  "Account woke up after a long silence",
		Family: "tenure",
		Render: func(v float64) string { return fmt.Sprintf("dormant for %s days before this payment", num(v)) },
		Notable: gt(60), Severity: sevAbove(60, 180),
	},
	"baseline_staleness_h": {
		Title:  "Payer baseline is stale",
		Family: "quality",
		Render: func(v float64) string { return fmt.Sprintf("behavioural baseline last refreshed %s hours ago", num(v)) },
		Notable: gt(72), Severity: sev("low"),
	},
	"payee_is_new_to_payer": {
		Title:  "First ever payment to this beneficiary",
		Family: "relationship",
		Render: func(float64) string {
			return "this payer has never sent money to this account before"
		},
		Notable: isTrue, Severity: sev("high"),
	},
	"payee_first_seen_by_us_days": {
		Title:  "Beneficiary account is new to us",
		Family: "relationship",
		Render: func(v float64) string { return fmt.Sprintf("beneficiary first seen by us %s days ago", num(v)) },
		Notable: func(v float64) bool { return v < 3 }, Severity: sev("medium"),
	},
	"payee_fanin_1h": {
		Title:  "Many different people are paying this account right now",
		Family: "fanin",
		Render: func(v float64) string { return fmt.Sprintf("%s distinct payers sent to this beneficiary in the last hour", num(v)) },
		Notable: gt(5), Severity: sevAbove(6, 15),
	},
	"payee_fanin_24h": {
		Title:  "Many different people paid this account today",
		Family: "fanin",
		Render: func(v float64) string { return fmt.Sprintf("%s distinct payers in the last 24 hours", num(v)) },
		Notable: gt(10), Severity: sevAbove(12, 30),
	},
	"payee_fanin_burstiness": {
		Title:  "Inflow to this account spiked suddenly",
		Family: "fanin",
		Render: func(v float64) string {
			return fmt.Sprintf("money is arriving %s× faster than this account's own 24-hour average", num(v))
		},
		Notable: gt(3), Severity: sevAbove(3, 8),
	},
	"pair_txn_count_90d": {
		Title:  "No shared history between these two accounts",
		Family: "relationship",
		Render: func(v float64) string { return fmt.Sprintf("%s prior payments between these accounts in 90 days", num(v)) },
		Notable: never, Severity: sev("info"),
	},
	"pair_amt_ratio_p95": {
		Title:  "Much larger than what these two accounts usually exchange",
		Family: "relationship",
		Render: func(v float64) string { return fmt.Sprintf("%s× the usual amount for this payer/payee pair", num(v)) },
		Notable: gt(2), Severity: sevAbove(2, 5),
	},
	"payee_fwd_latency_p50_s": {
		Title:  "Beneficiary forwards money onward almost immediately",
		Family: "cashout",
		Render: func(v float64) string {
			return fmt.Sprintf("money arriving here is typically moved on within %s seconds — pass-through behaviour, not a destination", num(v))
		},
		Notable: func(v float64) bool { return v > 0 && v < 120 }, Severity: sev("high"),
	},
	"payee_fwd_ratio_1h": {
		Title:  "Nearly everything paid in is immediately paid out",
		Family: "cashout",
		Render: func(v float64) string {
			return fmt.Sprintf("%.0f%% of this hour's inflow was forwarded straight back out", v*100)
		},
		Notable: gt(0.7), Severity: sevAbove(0.7, 0.9),
	},
	"payee_inflow_concentration": {
		Title:  "Inflow is concentrated in a few sources",
		Family: "fanin",
		Render: func(v float64) string { return fmt.Sprintf("inflow concentration index %s", num(v)) },
		Notable: gt(0.6), Severity: sev("medium"),
	},
	"payee_distinct_payer_banks_1h": {
		Title:  "Money is arriving from many different banks at once",
		Family: "fanin",
		Render: func(v float64) string {
			return fmt.Sprintf("payers from %s different banks in the last hour — a coordinated pattern, not one bank's customers", num(v))
		},
		Notable: gt(3), Severity: sevAbove(4, 8),
	},
	"device_is_new_to_payer": {
		Title:  "Payment came from an unrecognised device",
		Family: "device",
		Render: func(float64) string { return "this device has never been used on this account before" },
		Notable: isTrue, Severity: sev("high"),
	},
	"device_first_seen_hours": {
		Title:  "Device is brand new to us",
		Family: "device",
		Render: func(v float64) string { return fmt.Sprintf("device first seen by us %s hours ago", num(v)) },
		Notable: func(v float64) bool { return v < 24 }, Severity: sev("medium"),
	},
	"device_acct_degree_24h": {
		Title:  "One device is operating many different accounts",
		Family: "device",
		Render: func(v float64) string {
			return fmt.Sprintf("this device has been used on %s different accounts in 24 hours", num(v))
		},
		Notable: gt(2), Severity: sevAbove(3, 6),
	},
	"asn_is_new_to_payer": {
		Title:  "Connecting from an unfamiliar network",
		Family: "network",
		Render: func(float64) string { return "this payer has not used this network provider before" },
		Notable: isTrue, Severity: sev("low"),
	},
	"asn_acct_degree_1h": {
		Title:  "One network is driving many accounts",
		Family: "network",
		Render: func(v float64) string { return fmt.Sprintf("%s accounts transacted from this network in the last hour", num(v)) },
		Notable: gt(5), Severity: sevAbove(6, 15),
	},
	"geo_jump_kmh": {
		Title:  "Impossible travel since the last payment",
		Family: "geo",
		Render: func(v float64) string {
			return fmt.Sprintf("implied travel speed of %s km/h since this payer's previous transaction", num(v))
		},
		Notable: gt(800), Severity: sevAbove(800, 2000),
	},
	"cold_start_features_n": {
		Title:  "Several signals had no history to work from",
		Family: "quality",
		Render: func(v float64) string { return fmt.Sprintf("%s features were in cold start for this transaction", num(v)) },
		Notable: gt(6), Severity: sev("low"),
	},
}

// lookup returns the phrase for a feature id, synthesising a safe generic one for anything
// not in the book (including the rf_* rule-features, which get their English from the rule
// bundle's own explanation instead).
func lookup(id string) phrase {
	if p, ok := phrasebook[id]; ok {
		return p
	}
	return phrase{
		Title:    humanise(id),
		Family:   "other",
		Render:   func(v float64) string { return fmt.Sprintf("%s = %s", id, num(v)) },
		Notable:  never,
		Severity: sev("info"),
	}
}

func humanise(id string) string {
	s := strings.ReplaceAll(id, "_", " ")
	if s == "" {
		return id
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
