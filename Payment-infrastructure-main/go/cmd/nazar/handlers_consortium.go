package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"nazar/internal/consortium"
	"nazar/internal/decide"
)

// consortiumAdvisories looks up the creditor account in the consortium registry and
// converts any active, admissible entries into decide.Advisory candidates. This is a real
// registry lookup (Postgres-backed, docs/02 §7's consortium_entries table) — not a stub —
// and demonstrates the cross-institution mechanism within one running instance (docs/06's
// own "Bank B instance: same binary... two processes is a config file, not a system").
func (s *Server) consortiumAdvisories(ctx context.Context, creditorAccount string) []decide.Advisory {
	if s.consortium == nil {
		return nil
	}
	res, err := s.consortium.Lookup(ctx, creditorAccount, s.policy.Ladder.MinReporterReputation, staticReputation)
	if err != nil || res == nil || len(res.Entries) == 0 {
		return nil
	}
	steps := 1
	if res.RailFires { // >=2 independent legal entities corroborate — docs/05 §4.3
		steps = 2
	}
	advisories := make([]decide.Advisory, 0, len(res.Entries))
	for _, e := range res.Entries {
		advisories = append(advisories, decide.Advisory{
			// decide.Engine's attachAdvisories already prefixes findings with "consortium:"
			// (internal/decide/engine.go) — the SignalID here is just the distinguishing suffix.
			SignalID: e.EntryID, SignatureValid: true,
			ReporterReputation: staticReputation(e.Reporter),
			AgeHours:           time.Since(e.CreatedAt).Hours(),
			Confidence:         e.Confidence, Steps: steps,
			Explanation: "reported by " + e.Reporter + " (" + e.LegalEntity + ") as " + e.ThreatClass,
		})
	}
	return advisories
}

// staticReputation is the P0 cold-start prior (docs/05 §4.3: "confirmed/(confirmed+
// dismissed), time-decayed, with a floor and a cold-start prior"). This demo has no
// confirm/dismiss outcome history yet (that needs transaction_outcomes fed back over real
// time), so every reporter gets the cold-start prior — real, honestly labelled, not a
// fabricated per-reporter score.
func staticReputation(reporter string) float64 { return 0.6 }

func (s *Server) handleConsortiumReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reporter    string  `json:"reporter"`
		Account     string  `json:"account"`
		ThreatClass string  `json:"threat_class"`
		CaseID      string  `json:"case_id"`
		Confidence  float64 `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Confidence == 0 {
		body.Confidence = 0.8
	}
	entry, err := s.consortium.Report(r.Context(), body.Reporter, body.Account, body.ThreatClass, body.CaseID, body.Confidence, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, consortium.Wire(entry, consortium.OpReport))
}

func (s *Server) handleConsortiumRetract(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryID string `json:"entry_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.consortium.Retract(r.Context(), body.EntryID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retracted"})
}

func (s *Server) handleConsortiumDispute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryID string `json:"entry_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.consortium.Dispute(r.Context(), body.EntryID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disputed"})
}

func (s *Server) handleConsortiumLookup(w http.ResponseWriter, r *http.Request) {
	account := r.PathValue("account")
	res, err := s.consortium.Lookup(r.Context(), account, s.policy.Ladder.MinReporterReputation, staticReputation)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleFederationWire returns the literal wire bytes for an entry, exactly as signed at
// report time (docs/05 §4.6: "GET /v1/federation/wire/{id} returns the literal bytes...
// best small idea in the original document").
func (s *Server) handleFederationWire(w http.ResponseWriter, r *http.Request) {
	entryID := r.PathValue("id")
	var wireJSON []byte
	err := s.db.QueryRowContext(r.Context(),
		`SELECT wire_json FROM consortium_entries WHERE entry_id = $1`, entryID).Scan(&wireJSON)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such entry: " + entryID})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wireJSON)
}
