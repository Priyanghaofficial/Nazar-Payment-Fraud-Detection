package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// GET /v1/model/metrics — the numbers, in one place, each carrying its own provenance tier.
//
// Three distinct things get reported and are never conflated:
//
//   [RECOVERED]  performance against this repo's own generator ground truth. Real numbers
//                from a real time-forward held-out split, but the labels are ours, so this
//                measures the pipeline, not a real-world detection rate.
//   [MEASURED]   performance on the ULB credit-card dataset — genuine labelled fraud from a
//                real institution. Validates the training and calibration methodology on data
//                nobody here generated.
//   [MEASURED]   live decision latency on this running process, from real traffic.
//
// Serving these from files written by the training and evaluation scripts, rather than
// hardcoding them, is what makes them auditable: delete the file and the number disappears
// instead of quietly persisting as a slide.
func (s *Server) handleModelMetrics(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"live_latency": s.latency.Snapshot(),
		"model_bundle": s.engine.ModelBundleVersion,
		"policy_version": s.engine.LivePolicy().Version,
		"rules_version":  s.engine.RulesVersion,
		"tiers": map[string]string{
			"RECOVERED": "measured against our own generator's ground truth — real evaluation, synthetic labels",
			"MEASURED":  "measured against externally labelled data or on this running process",
		},
	}

	read := func(key string, parts ...string) {
		b, err := os.ReadFile(filepath.Join(append([]string{s.repoRoot}, parts...)...))
		if err != nil {
			out[key] = nil
			return
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			out[key] = v
		} else {
			out[key] = nil
		}
	}

	read("training", "py", "training", "output", "metrics.json")
	read("manifest", "py", "training", "output", "model_manifest.json")
	read("calibrator", "py", "training", "output", "calibrator.json")
	read("prevalence", "py", "training", "output", "prevalence.json")
	read("external_validation", "py", "eval", "output", "ulb_validation_result.json")

	writeJSON(w, http.StatusOK, out)
}
