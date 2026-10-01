// Package graph implements the P0 in-process graph (docs/00 §6, docs/06 Milestone 3):
// payer/payee/device adjacency with decay, a bounded component walk, and a ring score
// derived from frequency/structure rather than a fixed "shared beneficiary" weight — the
// mechanism docs/02 §4.2 requires so a 500-payer merchant scores zero (test_merchant_is_not_a_ring)
// while a small, device-linked, forwarding cluster scores high.
//
// P0 simplification, labelled per CLAUDE.md: single in-process adjacency map with a mutex,
// not a sharded store. Correct algorithm at P0 scale (~2k accounts); sharding is a [P1]
// concern (docs/06 "Stub deliberately" table).
package graph

import (
	"sort"
	"sync"
)

const (
	// RingSizeCap: a payee with more distinct payers than this is merchant-shaped by
	// construction — the single most important line in this file (test_merchant_is_not_a_ring).
	RingSizeCap = 25
	// componentWalkCap bounds BFS cost — never walk an unbounded graph on a scoring path.
	componentWalkCap = 200
	edgeDecayHalfLifeMs = 72 * 3600 * 1000 // 72h: a shared device/payer link fades over days
)

type edge struct {
	lastSeenMs int64
	count      int
}

type Engine struct {
	mu sync.RWMutex

	// payerToPayees / payeeToPayers: the payment graph, undirected in storage, directed in meaning.
	payeeToPayers map[string]map[string]*edge
	payerToPayees map[string]map[string]*edge

	// deviceToAccounts: device-sharing edges — the strongest single ring/mule signal at P0.
	deviceToAccounts map[string]map[string]*edge
	accountToDevices map[string]map[string]bool

	bankOf func(account string) string
}

func NewEngine() *Engine {
	return &Engine{
		payeeToPayers:    map[string]map[string]*edge{},
		payerToPayees:    map[string]map[string]*edge{},
		deviceToAccounts: map[string]map[string]*edge{},
		accountToDevices: map[string]map[string]bool{},
		bankOf:           defaultBankOf,
	}
}

// OnEvent applies one transaction to the graph. Called from the async lane (docs/00 §5:
// "Long, bursty, unbounded work... must never share a scheduler with a 25ms deadline"),
// never from the scoring path.
func (g *Engine) OnEvent(payer, payee, deviceID string, atMs int64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	addEdge(g.payeeToPayers, payee, payer, atMs)
	addEdge(g.payerToPayees, payer, payee, atMs)

	if deviceID != "" {
		addEdge(g.deviceToAccounts, deviceID, payer, atMs)
		if g.accountToDevices[payer] == nil {
			g.accountToDevices[payer] = map[string]bool{}
		}
		g.accountToDevices[payer][deviceID] = true
	}
}

func addEdge(m map[string]map[string]*edge, from, to string, atMs int64) {
	if m[from] == nil {
		m[from] = map[string]*edge{}
	}
	e, ok := m[from][to]
	if !ok {
		e = &edge{}
		m[from][to] = e
	}
	e.lastSeenMs = atMs
	e.count++
}

// Result mirrors contract.GraphResult's fields without importing contract, keeping this
// package dependency-free (it is itself a seam's implementation detail, called from cmd/nazar).
type Result struct {
	RingScore          float64
	RingSize           int
	ComponentBankCount int
	HopsToCashout      int
	DeviceSharedDegree int
}

// Evaluate computes the ring signal for a payee as of nowMs (docs/05 §2: frequency-derived
// weight, not a constant). It never mutates state and performs no I/O — safe to call from
// the scoring path with a bounded walk.
func (g *Engine) Evaluate(payee string, nowMs int64) Result {
	g.mu.RLock()
	defer g.mu.RUnlock()

	payers := g.payeeToPayers[payee]
	numPayers := len(payers)

	if numPayers == 0 {
		return Result{}
	}
	if numPayers > RingSizeCap {
		// Merchant-shaped: many distinct payers. This is the line
		// test_merchant_is_not_a_ring depends on — no amount of device sharing below
		// overrides it, because at this fan-in device overlap is population noise, not signal.
		return Result{RingSize: numPayers}
	}

	// Device-sharing bonus: do >=2 of this payee's payers share a device? That is very
	// hard to explain as independent customers and is the strongest small-cluster signal.
	deviceCounts := map[string]int{}
	for payerAcct := range payers {
		for d := range g.accountToDevices[payerAcct] {
			deviceCounts[d]++
		}
	}
	sharedDeviceDegree := 0
	for _, c := range deviceCounts {
		if c >= 2 {
			sharedDeviceDegree += c
		}
	}

	// Bank diversity across payers in the component (docs/02 §4.2: cross-bank fan-in is
	// harder to fake than same-bank — used here as a corroborating structural signal).
	banks := map[string]bool{}
	for payerAcct := range payers {
		banks[g.bankOf(payerAcct)] = true
	}

	// hops_to_cashout: does this payee itself forward on quickly, chaining through further
	// accounts? Bounded BFS on the payer->payee edges starting FROM the payee acting as a payer.
	hops := g.cashoutDepth(payee, nowMs, 5)

	base := 0.0
	if numPayers >= 3 {
		base += 0.25
	}
	if sharedDeviceDegree > 0 {
		base += 0.45 * clamp01(float64(sharedDeviceDegree)/float64(numPayers))
	}
	if hops >= 1 {
		base += 0.3 * clamp01(float64(hops)/3.0)
	}

	return Result{
		RingScore:          clamp01(base),
		RingSize:           numPayers,
		ComponentBankCount: len(banks),
		HopsToCashout:      hops,
		DeviceSharedDegree: sharedDeviceDegree,
	}
}

// cashoutDepth walks the payer->payee chain starting at `start` acting as a payer, following
// the most recent edge at each hop, decaying relevance by recency, capped at maxHops.
func (g *Engine) cashoutDepth(start string, nowMs int64, maxHops int) int {
	cur := start
	visited := map[string]bool{start: true}
	depth := 0
	for i := 0; i < maxHops; i++ {
		outs := g.payerToPayees[cur]
		if len(outs) == 0 {
			break
		}
		var next string
		var newest int64 = -1
		for to, e := range outs {
			if visited[to] {
				continue
			}
			if e.lastSeenMs > newest && withinDecayWindow(e.lastSeenMs, nowMs) {
				newest = e.lastSeenMs
				next = to
			}
		}
		if next == "" {
			break
		}
		visited[next] = true
		cur = next
		depth++
		if len(visited) > componentWalkCap {
			break
		}
	}
	return depth
}

func withinDecayWindow(lastSeenMs, nowMs int64) bool {
	return nowMs-lastSeenMs < 6*edgeDecayHalfLifeMs
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func defaultBankOf(acct string) string {
	for i, r := range acct {
		if r == '-' {
			return acct[:i]
		}
	}
	return "UNKNOWN"
}

// TopPayees returns the beneficiaries this engine currently knows about, ranked by how many
// distinct payers have paid them within the decay window — which is exactly the axis the ring
// score is built on, so the account at the top of this list is the one most likely to show
// structure when it is opened.
//
// This exists because the console cannot honestly offer accounts to inspect otherwise. The
// graph is in-process by design (CLAUDE.md: in-process Go adjacency at P0), so it holds only
// what this process has seen since it started, while persisted history reaches much further
// back. Offering an account from history that the graph has since forgotten renders as a
// screen of zeros — technically correct and completely misleading. Listing what the engine
// actually holds means every account offered has something to show.
type PayeeSummary struct {
	Account       string `json:"account"`
	DistinctPayers int   `json:"distinct_payers"`
	SharedDevices  int   `json:"shared_devices"`
}

func (g *Engine) TopPayees(nowMs int64, limit int) []PayeeSummary {
	g.mu.RLock()
	defer g.mu.RUnlock()

	out := make([]PayeeSummary, 0, len(g.payeeToPayers))
	for payee, payers := range g.payeeToPayers {
		n := 0
		for _, e := range payers {
			if withinDecayWindow(e.lastSeenMs, nowMs) {
				n++
			}
		}
		if n == 0 {
			continue
		}
		out = append(out, PayeeSummary{
			Account: payee, DistinctPayers: n, SharedDevices: len(g.accountToDevices[payee]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DistinctPayers != out[j].DistinctPayers {
			return out[i].DistinctPayers > out[j].DistinctPayers
		}
		return out[i].Account < out[j].Account
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
