package main

import (
	"context"
	"net/http"
	"time"

	"nazar/internal/explain"
	"nazar/internal/narrate"
	"nazar/internal/persist"
)

// GET /v1/decisions/{id}/explain — the whole "why" surface for one decision: ranked plain
// English evidence, every independent detector's verdict, the expected-cost table that
// actually chose the action, counterfactuals computed against the real cost function, the
// stage-by-stage execution trace, and a live re-execution proving the decision is
// reproducible and the audit link is intact.
//
// This is a read plus a deterministic recomputation. It never mutates anything and never
// re-scores against current state — the persisted feature snapshot is the input, per
// test_replay_is_a_read.
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()

	d, err := persist.GetLatestDecision(ctx, s.db, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no decision found for " + id})
		return
	}
	ev, _ := persist.GetEvent(ctx, s.db, id)

	ex := explain.Build(explain.Input{Decision: d, Event: ev, Engine: s.engine})
	det := explain.Reproduce(s.engine, d, ev)

	writeJSON(w, http.StatusOK, map[string]any{
		"explanation": ex,
		"determinism": det,
		"narrator":    s.narrator.Meta(),
	})
}

// POST /v1/decisions/{id}/narrate — the language-model write-up, explicitly on demand and
// explicitly off the decision path. If the model is unreachable (no key, no network, air-gapped
// box down) this falls back to the deterministic narrator and says so, rather than erroring:
// the explanation was never the model's to produce.
func (s *Server) handleNarrate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()

	d, err := persist.GetLatestDecision(ctx, s.db, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no decision found for " + id})
		return
	}
	ev, _ := persist.GetEvent(ctx, s.db, id)

	ex := explain.Build(explain.Input{Decision: d, Event: ev, Engine: s.engine})
	det := explain.Reproduce(s.engine, d, ev)
	brief := ex.Brief(det)

	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := s.narrator.Narrate(callCtx, brief)
	if err != nil {
		fallback, _ := narrate.DeterministicNarrator{}.Narrate(callCtx, brief)
		fallback.Note = "Language model unavailable (" + err.Error() + "). This write-up was assembled deterministically from the same structured findings — no external dependency."
		writeJSON(w, http.StatusOK, map[string]any{"narrative": fallback, "degraded": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"narrative": res, "degraded": false})
}

// POST /v1/decisions/{id}/chat  {"messages":[{"role":"user","content":"why not just block it?"}]}
//
// The interactive lane. An analyst asks follow-up questions about a decision that has
// already been made and persisted; the model is shown the same whitelisted Brief the
// write-up uses, plus the conversation so far. Nothing here can alter a decision — this
// endpoint has no write path — and if the model is unreachable the deterministic answerer
// replies with the record itself rather than the console going silent.
func (s *Server) handleDecisionChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()

	var req struct {
		Messages []narrate.Turn `json:"messages"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(narrate.SanitiseTurns(req.Messages)) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no question asked"})
		return
	}

	d, err := persist.GetLatestDecision(ctx, s.db, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no decision found for " + id})
		return
	}
	ev, _ := persist.GetEvent(ctx, s.db, id)

	ex := explain.Build(explain.Input{Decision: d, Event: ev, Engine: s.engine})
	det := explain.Reproduce(s.engine, d, ev)
	brief := ex.Brief(det)

	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if chatter, ok := s.narrator.(narrate.ChatNarrator); ok {
		if ans, err := chatter.Chat(callCtx, brief, req.Messages); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"answer": ans, "degraded": false})
			return
		} else {
			// Fall through to the deterministic answerer below, carrying the reason.
			fallback, _ := narrate.DeterministicNarrator{}.Chat(callCtx, brief, req.Messages)
			fallback.Note = "Language model unavailable (" + err.Error() + "). This is the decision record itself, restated — no external dependency."
			writeJSON(w, http.StatusOK, map[string]any{"answer": fallback, "degraded": true})
			return
		}
	}
	fallback, _ := narrate.DeterministicNarrator{}.Chat(callCtx, brief, req.Messages)
	writeJSON(w, http.StatusOK, map[string]any{"answer": fallback, "degraded": true})
}
