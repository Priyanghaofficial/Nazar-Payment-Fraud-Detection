package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"nazar/internal/persist"
)

// handleListAlerts backs the Alerts screen — a real open/resolved queue, not a UI-only
// filter over Live Monitor. status=open (default) | resolved | all.
func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if status == "all" {
		status = ""
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	rows, err := persist.GetAlerts(r.Context(), s.db, status, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	openCount, _ := persist.CountOpenAlerts(r.Context(), s.db)
	writeJSON(w, http.StatusOK, map[string]any{"alerts": rows, "open_count": openCount})
}

// handleResolveAlert closes a real alert — a persisted state transition an analyst would
// actually perform, not a client-side dismiss.
func (s *Server) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid alert id"})
		return
	}
	var body struct {
		ResolvedBy string `json:"resolved_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.ResolvedBy == "" {
		body.ResolvedBy = "operator"
	}
	if err := persist.ResolveAlert(r.Context(), s.db, id, body.ResolvedBy); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no open alert with that id"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}
