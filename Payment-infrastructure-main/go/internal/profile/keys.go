// Package profile implements the ProfileStore seam (contract.ProfileStore): a Redis-backed
// implementation with hash-tagged keys for co-location (docs/02 §3.1) plus a deterministic
// fake for tests. Two structural rules from docs/00 §9 are enforced by this package's
// location, not by convention: nothing in internal/features may import this package.
package profile

import "fmt"

// Key builders. Every one matches docs/02-DATA-AND-FEATURES.md §3.1 verbatim — this is
// the single place that must stay in sync with the spec table.

func payerTxnWindow(acct string) string   { return fmt.Sprintf("w:{p:%s}:txn", acct) }
func payerAmtMinute(acct string) string   { return fmt.Sprintf("c:{p:%s}:amt:m", acct) }
func payerAmtHour(acct string) string     { return fmt.Sprintf("c:{p:%s}:amt:h", acct) }
func payerBaseline(acct string) string    { return fmt.Sprintf("b:{p:%s}", acct) }
func payerPayeeSet(acct string) string    { return fmt.Sprintf("s:{p:%s}:payees", acct) }
func payerDeviceSet(acct string) string   { return fmt.Sprintf("s:{p:%s}:devices", acct) }
func payerASNSet(acct string) string      { return fmt.Sprintf("s:{p:%s}:asns", acct) }
func payerLast(acct string) string        { return fmt.Sprintf("l:{p:%s}", acct) }
func payerAmtSample(acct string) string   { return fmt.Sprintf("smp:{p:%s}:amt", acct) } // P0: capped sample list backing the baseline stats
func payerAmtDay(acct string) string      { return fmt.Sprintf("c:{p:%s}:amt:d", acct) }
func payerFirstSeen(acct string) string   { return fmt.Sprintf("f:{p:%s}:first_seen", acct) }
func hourHistKey(acct string) string      { return fmt.Sprintf("h:{p:%s}:hourhist", acct) }

func payeePayersWindow(acct string) string { return fmt.Sprintf("w:{b:%s}:payers", acct) }
func payeeTxnWindow(acct string) string    { return fmt.Sprintf("w:{b:%s}:txn", acct) }
func payeeInMinute(acct string) string     { return fmt.Sprintf("c:{b:%s}:in:m", acct) }
func payeeOutMinute(acct string) string    { return fmt.Sprintf("c:{b:%s}:out:m", acct) }
func payeeFwd(acct string) string          { return fmt.Sprintf("fwd:{b:%s}", acct) }
func payeeFirstSeen(acct string) string    { return fmt.Sprintf("f:{b:%s}:first_seen", acct) }
func payeeInflowShare(acct string) string  { return fmt.Sprintf("hh:{b:%s}:in24h", acct) } // P0: per-payer inflow-share hash for HHI concentration

func deviceAccts(id string) string     { return fmt.Sprintf("w:{d:%s}:accts", id) }
func deviceFirstSeen(id string) string { return fmt.Sprintf("f:{d:%s}:first_seen", id) }

func pairHash(payer, payee string) string { return fmt.Sprintf("pr:{r:%s:%s}", payer, payee) }

func asnAccts(asn int32) string { return fmt.Sprintf("w:asn:%d:accts", asn) }

func idempotencyKey(e2e string) string { return fmt.Sprintf("i:{e:%s}", e2e) }

// MaxWindow bounds trims and TTLs. 24h + 10% covers every feature window we read (docs/02 §3.3).
const MaxWindowMs int64 = 24 * 3600 * 1000
