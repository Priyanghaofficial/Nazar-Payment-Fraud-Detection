package main

import (
	"fmt"
	"net/http"
)

// handleCalibration exposes what the console's calibration screen can honestly show right
// now: the active calibrator's method/version, the prevalence-correction parameters
// (docs non-negotiable #9: explicit and versioned), and a REAL histogram of calibrated
// scores computed from actually-persisted decisions. It does NOT fabricate a reliability
// diagram / ECE — that needs matured labels (transaction_outcomes / labels tables), which
// this demo session has none of yet. The screen must say that plainly rather than draw a
// chart against invented ground truth.
func (s *Server) handleCalibration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `
		SELECT p_model FROM decisions
		WHERE p_model IS NOT NULL
		ORDER BY decided_at DESC LIMIT 2000`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	const buckets = 10
	hist := make([]int, buckets)
	n := 0
	for rows.Next() {
		var p float64
		if err := rows.Scan(&p); err != nil {
			continue
		}
		idx := int(p * buckets)
		if idx >= buckets {
			idx = buckets - 1
		}
		if idx < 0 {
			idx = 0
		}
		hist[idx]++
		n++
	}

	bucketLabels := make([]string, buckets)
	for i := 0; i < buckets; i++ {
		bucketLabels[i] = fmtRange(float64(i)/buckets, float64(i+1)/buckets)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"calibrator_method":  s.engine.Calibrator.Meta().Method,
		"calibrator_version": s.engine.Calibrator.Meta().Version,
		"model_bundle":       s.engine.Scorer.Meta().BundleVersion,
		"prevalence": map[string]any{
			"version":            s.engine.Prevalence.Version,
			"train_prevalence":   s.engine.Prevalence.TrainPrevalence,
			"natural_prevalence": s.engine.Prevalence.NaturalPrevalence,
		},
		"score_distribution": map[string]any{
			"n": n, "buckets": bucketLabels, "counts": hist,
		},
		"reliability_diagram_available": false,
		"reliability_diagram_note":      "ECE / reliability diagram requires matured labels (transaction_outcomes + labels tables) — none are available in this session yet. This is the honest state, not a placeholder bug.",
	})
}

func fmtRange(lo, hi float64) string {
	return fmt.Sprintf("%.2f-%.2f", lo, hi)
}
