// Package wal implements the local durable append (docs/01 §6.1): a decision is durable
// before the customer sees it, independent of Postgres being reachable. P0 simplification:
// NDJSON file with an fsync per write rather than the batched group-commit described for
// [P1] scale — correct and durable, just not throughput-optimised, which does not matter
// at 200 TPS. Tamper-evidence comes from the audit chain (internal/audit), not from this
// file's format.
package wal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"nazar/internal/audit"
	"nazar/internal/contract"
)

type record struct {
	Seq          int64  `json:"seq"`
	EndToEndID   string `json:"end_to_end_id"`
	DecisionSeq  int    `json:"decision_seq"`
	Action       string `json:"action"`
	DecidedAtMs  int64  `json:"decided_at_ms"`
	PrevHashHex  string `json:"prev_hash_hex"`
	HashHex      string `json:"hash_hex"`
	DecisionJSON json.RawMessage `json:"decision"`
}

// WAL appends a Decision to the local file AND assigns its chain seq/hash — this is the
// point at which a decision becomes durable and tamper-evident, before the HTTP response
// is written.
type WAL struct {
	mu    sync.Mutex
	file  *os.File
	chain *audit.Chain
}

func Open(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("wal: opening %s: %w", path, err)
	}
	return &WAL{file: f, chain: audit.NewChain(0)}, nil
}

// Append assigns the audit chain fields on d (ChainSeq, PrevHash, Hash, DecisionShard),
// writes the record to the local file with an fsync, and returns. After this returns, the
// decision is durable even if the process crashes before Postgres is reached.
func (w *WAL) Append(d *contract.Decision) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	pModelStr := "nil"
	if d.PModel != nil {
		pModelStr = fmt.Sprintf("%.6f", *d.PModel)
	}
	rec := audit.CanonicalRecord{
		EndToEndID: d.EndToEndID, DecisionSeq: d.DecisionSeq, AcceptedAtMs: d.AcceptedAtMs,
		DecidedAtMs: d.DecidedAtMs, Action: string(d.Action), PModel: pModelStr,
		PolicyVersion: d.PolicyVersion, ModelBundleVersion: d.ModelBundleVersion, RulesVersion: d.RulesVersion,
	}
	seq, prevHash, hash, err := w.chain.Next(rec)
	if err != nil {
		return err
	}
	d.DecisionShard = w.chain.Shard
	d.ChainSeq = seq
	d.PrevHash = prevHash
	d.Hash = hash

	dj, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("wal: marshalling decision: %w", err)
	}
	line := record{
		Seq: seq, EndToEndID: d.EndToEndID, DecisionSeq: d.DecisionSeq, Action: string(d.Action),
		DecidedAtMs: d.DecidedAtMs, PrevHashHex: hexOrEmpty(prevHash), HashHex: hexOrEmpty(hash),
		DecisionJSON: dj,
	}
	lb, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("wal: marshalling record: %w", err)
	}
	if _, err := w.file.Write(append(lb, '\n')); err != nil {
		return fmt.Errorf("wal: writing record: %w", err)
	}
	return w.file.Sync()
}

// ResumeChain restores the audit chain's position after a restart (see audit.Chain.Resume).
func (w *WAL) ResumeChain(seq int64, lastHash []byte) {
	w.chain.Resume(seq, lastHash)
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// Replay reads every decision back out of the WAL file in append order — used on startup
// to reconcile any records that were durably WAL'd but never made it to Postgres (docs/01
// §6.1: "the shipper drains WAL -> Postgres with at-least-once delivery").
func Replay(path string) ([]*contract.Decision, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []*contract.Decision
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var line record
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue // a torn final write is possible after a crash; skip rather than fail replay
		}
		var d contract.Decision
		if err := json.Unmarshal(line.DecisionJSON, &d); err != nil {
			continue
		}
		out = append(out, &d)
	}
	return out, sc.Err()
}

func hexOrEmpty(b []byte) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("%x", b)
}

// Truncate empties the log and starts a fresh audit chain from sequence zero.
//
// This is a demo affordance and nothing else. A real deployment never truncates its audit
// log — the whole value of a hash chain is that history cannot be discarded quietly — which
// is why this lives behind an explicit admin action and why the console tells the room the
// chain has been restarted rather than presenting a fresh chain as a long-standing one.
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.file.Truncate(0); err != nil {
		return fmt.Errorf("wal: truncating: %w", err)
	}
	if _, err := w.file.Seek(0, 0); err != nil {
		return fmt.Errorf("wal: rewinding: %w", err)
	}
	w.chain = audit.NewChain(0)
	return nil
}
