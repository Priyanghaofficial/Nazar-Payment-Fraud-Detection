package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"nazar/internal/contract"
	"nazar/internal/decide"
	"nazar/internal/scoring"
)

// loadScorer tries the trained LightGBM bundle first (py/training output, Milestone 2) and
// falls back to the hand-weighted heuristic (docs/00 §3.2: every seam has >=2
// implementations, and this is the one that lets the system run honestly before training
// has happened).
func loadScorer(repoRoot string) (contract.Scorer, string) {
	modelPath := filepath.Join(repoRoot, "py", "training", "output", "model.txt")
	manifestPath := filepath.Join(repoRoot, "py", "training", "output", "model_manifest.json")
	if fileExists(modelPath) && fileExists(manifestPath) {
		s, err := scoring.LoadLeavesScorer(modelPath, manifestPath)
		if err == nil {
			return s, "leaves:" + s.Meta().BundleVersion
		}
		log.Printf("nazar: found model files but failed to load (%v) — falling back to heuristic", err)
	}
	return scoring.NewHeuristicScorer(), "heuristic-fallback"
}

func loadCalibrator(repoRoot string) (contract.Calibrator, string) {
	path := filepath.Join(repoRoot, "py", "training", "output", "calibrator.json")
	if fileExists(path) {
		c, err := scoring.LoadBetaCalibrator(path)
		if err == nil {
			return c, "beta:" + c.Version
		}
		log.Printf("nazar: found calibrator file but failed to load (%v) — falling back to identity", err)
	}
	return scoring.NewIdentityBetaCalibrator(), "identity-fallback"
}

type prevalenceFile struct {
	Version           string  `json:"version"`
	TrainPrevalence   float64 `json:"train_prevalence"`
	NaturalPrevalence float64 `json:"natural_prevalence"`
}

func loadPrevalence(repoRoot string) *decide.PrevalenceCorrector {
	path := filepath.Join(repoRoot, "py", "training", "output", "prevalence.json")
	if fileExists(path) {
		b, err := os.ReadFile(path)
		if err == nil {
			var pf prevalenceFile
			if json.Unmarshal(b, &pf) == nil {
				return &decide.PrevalenceCorrector{Version: pf.Version, TrainPrevalence: pf.TrainPrevalence, NaturalPrevalence: pf.NaturalPrevalence}
			}
		}
	}
	// identity default: no correction until Milestone 2 trains on a known prevalence.
	return &decide.PrevalenceCorrector{Version: "identity-fallback", TrainPrevalence: 0.5, NaturalPrevalence: 0.5}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
