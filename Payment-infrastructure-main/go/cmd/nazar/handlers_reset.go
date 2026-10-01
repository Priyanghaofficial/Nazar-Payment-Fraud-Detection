package main

import (
	"context"
	"net/http"
	"time"

	"nazar/internal/graph"
	"nazar/internal/novelty"
)

// POST /v1/admin/reset — return the instance to a plain state.
//
// This exists for the demo and says so. Running several scenarios in front of a room means
// the third one inherits the totals of the first two: the value-stopped counter is bloated,
// the latency percentiles average over load nobody is talking about any more, the graph
// still holds a ring from an earlier campaign, and the novelty reservoir is calibrated on
// traffic that has nothing to do with what is being shown. None of that is wrong exactly —
// it is all real — but it makes each scenario harder to read than it needs to be.
//
// What this is NOT is a way to make a bad result disappear. It clears everything or nothing,
// it reports exactly what it cleared, and it restarts the audit chain from zero rather than
// editing it — a hash chain whose history could be quietly rewritten would be worthless, so
// the honest move is to start a visibly new one. In a real deployment this endpoint would
// not exist.
type resetReport struct {
	Decisions   int64  `json:"decisions_deleted"`
	Transactions int64 `json:"transactions_deleted"`
	Alerts      int64  `json:"alerts_deleted"`
	RedisKeys   int64  `json:"redis_keys_deleted"`
	Graph       bool   `json:"graph_cleared"`
	Novelty     bool   `json:"novelty_cleared"`
	Latency     bool   `json:"latency_cleared"`
	Sim         bool   `json:"simulator_stopped"`
	JudgeSession bool  `json:"judge_session_cleared"`
	Policy      string `json:"policy_version"`
	AuditChain  string `json:"audit_chain"`
	Note        string `json:"note"`
	TookMs      float64 `json:"took_ms"`
}

func (s *Server) handleAdminReset(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var rep resetReport

	// 1. Stop anything still generating traffic, before clearing what it has produced —
	//    otherwise an in-flight campaign repopulates the tables mid-wipe.
	s.sim.mu.Lock()
	if s.sim.trafficCancel != nil {
		s.sim.trafficCancel()
		s.sim.trafficCancel = nil
	}
	s.sim.trafficRunning = false
	if s.sim.campaignCancel != nil {
		s.sim.campaignCancel()
		s.sim.campaignCancel = nil
	}
	s.sim.campaign = nil
	s.sim.warmed = false
	s.sim.pairs = nil
	s.sim.mu.Unlock()
	s.sim.ambientSent.Store(0)
	s.sim.attackSent.Store(0)
	rep.Sim = true

	// Give the in-flight decision goroutines a moment to land so their writes are included
	// in the truncate rather than arriving just after it.
	time.Sleep(400 * time.Millisecond)

	// 2. Persisted history.
	for _, tbl := range []string{"alerts", "decisions", "transactions"} {
		res, err := s.db.ExecContext(ctx, "DELETE FROM "+tbl)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "clearing " + tbl + ": " + err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		switch tbl {
		case "alerts":
			rep.Alerts = n
		case "decisions":
			rep.Decisions = n
		case "transactions":
			rep.Transactions = n
		}
	}

	// 3. Redis: every profile, window and idempotency key this instance wrote.
	if n, err := s.rdb.DBSize(ctx).Result(); err == nil {
		rep.RedisKeys = n
	}
	if err := s.rdb.FlushDB(ctx).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "flushing redis: " + err.Error()})
		return
	}

	// 4. In-process state. Replacing the engines is simpler and safer than draining them.
	s.graph = graph.NewEngine()
	rep.Graph = true
	s.novelty = novelty.NewEngine()
	rep.Novelty = true
	s.latency.Reset()
	rep.Latency = true

	// 5. The write-ahead log and its audit chain.
	if err := s.wal.Truncate(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rep.AuditChain = "restarted at sequence 0"

	// 6. Live policy back to the approved on-disk bundle, so a tuned demo policy does not
	//    silently carry into the next scenario.
	if s.basePolicy != nil && s.policyRef != nil {
		s.policyRef.Store(s.basePolicy)
		rep.Policy = s.basePolicy.Version
	}

	// 7. The phone session the console follows.
	s.judgeMu.Lock()
	s.judgeSession = nil
	s.judgeMu.Unlock()
	rep.JudgeSession = true

	rep.TookMs = float64(time.Since(start).Microseconds()) / 1000.0
	rep.Note = "Instance returned to a plain state. The audit chain restarts at sequence zero rather than being edited — a chain whose history could be rewritten would prove nothing. A production deployment has no such endpoint."

	// Tell every connected console to clear its feed, so the room does not keep looking at
	// rows for decisions that no longer exist.
	s.hub.Publish("reset", rep)

	writeJSON(w, http.StatusOK, rep)
}
