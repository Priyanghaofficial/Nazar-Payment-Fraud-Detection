package scoring

import (
	"encoding/json"
	"math"
	"os"

	"nazar/internal/contract"
)

// BetaCalibrator implements beta calibration (Kull, Silva Filho & Flach 2017): three
// parameters, smooth, and — unlike isotonic — it extrapolates sensibly past the observed
// score range. Chosen over bootstrapped isotonic per CLAUDE.md's "simplest thing that
// works" table.
//
//	logit(p_cal) = a*ln(s) - b*ln(1-s) + c
type BetaCalibrator struct {
	A       float64 `json:"a"`
	B       float64 `json:"b"`
	C       float64 `json:"c"`
	Version string  `json:"version"`
}

// NewIdentityBetaCalibrator returns the untrained default (a=1, b=1, c=0), which is not
// quite the identity function but close to it in the middle of the range — used until
// Milestone 2 fits real parameters on a natural-prevalence slice (docs/03 §4).
func NewIdentityBetaCalibrator() *BetaCalibrator {
	return &BetaCalibrator{A: 1, B: 1, C: 0, Version: "identity-v0"}
}

func LoadBetaCalibrator(path string) (*BetaCalibrator, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c BetaCalibrator
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *BetaCalibrator) Calibrate(raw float64) float64 {
	s := clamp(raw, 1e-6, 1-1e-6)
	logit := c.A*math.Log(s) - c.B*math.Log(1-s) + c.C
	return 1.0 / (1.0 + math.Exp(-logit))
}

func (c *BetaCalibrator) Meta() contract.CalibratorMeta {
	return contract.CalibratorMeta{Method: "beta", Version: c.Version}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
