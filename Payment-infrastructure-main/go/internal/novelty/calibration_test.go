package novelty

import (
	"math"
	"math/rand"
	"testing"
)

// The whole value of a conformal p-value is that it is calibrated: on data drawn from the
// same distribution as the calibration set, p is uniform on (0,1]. So about 5% of ordinary
// traffic should score below 0.05 — no more. A detector that flags 18% of normal payments
// is not a sensitive detector, it is a miscalibrated one, and every "this is unusual" it
// produces is worth less as a result.
func TestPValueIsUniformOnInDistributionTraffic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	e := NewEngine()
	dims := []string{"amount", "velocity", "age"}

	sample := func() map[string]float64 {
		return map[string]float64{
			"amount":   rng.NormFloat64()*1000 + 5000,
			"velocity": rng.NormFloat64()*2 + 6,
			"age":      rng.NormFloat64()*30 + 200,
		}
	}

	// Fill well past alphaSampleCap so the subsampling path is exercised.
	for i := 0; i < 1200; i++ {
		e.Observe(sample(), dims)
	}

	var n, below, sum int
	var ps []float64
	for i := 0; i < 600; i++ {
		r := e.Evaluate(sample())
		if !r.Evaluated {
			continue
		}
		n++
		ps = append(ps, r.PValue)
		if r.PValue < 0.05 {
			below++
		}
		_ = sum
	}
	if n == 0 {
		t.Fatal("no evaluations")
	}

	rate := float64(below) / float64(n)
	median := quantile(ps, 0.5)
	t.Logf("n=%d  p<0.05 = %.1f%%  median p = %.3f", n, rate*100, median)

	// Generous bounds — this is a statistical property, not an exact one.
	if rate > 0.12 {
		t.Errorf("false-positive rate %.1f%% at p<0.05; a calibrated detector gives ~5%%", rate*100)
	}
	if math.Abs(median-0.5) > 0.15 {
		t.Errorf("median p = %.3f; a calibrated detector centres near 0.50", median)
	}
}

// And it must still fire on something genuinely different, or calibrating it was pointless.
func TestPValueIsSmallForOutOfDistributionTraffic(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	e := NewEngine()
	dims := []string{"amount", "velocity", "age"}
	for i := 0; i < 800; i++ {
		e.Observe(map[string]float64{
			"amount":   rng.NormFloat64()*1000 + 5000,
			"velocity": rng.NormFloat64()*2 + 6,
			"age":      rng.NormFloat64()*30 + 200,
		}, dims)
	}
	r := e.Evaluate(map[string]float64{"amount": 900000, "velocity": 140, "age": 0})
	if !r.Evaluated {
		t.Fatal("not evaluated")
	}
	t.Logf("far-out point p = %.4f", r.PValue)
	if r.PValue > 0.05 {
		t.Errorf("clearly anomalous point scored p=%.3f; expected < 0.05", r.PValue)
	}
}

func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[int(q*float64(len(s)-1))]
}

// Real traffic is not i.i.d. Velocity and count features climb over the course of a run, so
// a reference set that is a stale snapshot makes every new payment look unlike anything it
// has seen — and the detector degenerates into flagging almost everything. This reproduces
// that: the calibration property has to survive gradual drift, not just a fixed distribution.
func TestPValueSurvivesGradualDrift(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	e := NewEngine()
	dims := []string{"amount", "velocity", "age"}

	step := 0
	sample := func() map[string]float64 {
		step++
		drift := float64(step) * 0.004 // slow upward march, as a velocity counter would
		return map[string]float64{
			"amount":   rng.NormFloat64()*1000 + 5000,
			"velocity": rng.NormFloat64()*2 + 6 + drift,
			"age":      rng.NormFloat64()*30 + 200,
		}
	}

	for i := 0; i < 1200; i++ {
		e.Observe(sample(), dims)
	}

	var n, below int
	var ps []float64
	for i := 0; i < 600; i++ {
		v := sample()
		r := e.Evaluate(v)
		e.Observe(v, dims) // the live path observes as it goes
		if !r.Evaluated {
			continue
		}
		n++
		ps = append(ps, r.PValue)
		if r.PValue < 0.05 {
			below++
		}
	}
	rate := float64(below) / float64(n)
	t.Logf("under drift: n=%d  p<0.05 = %.1f%%  median p = %.3f", n, rate*100, quantile(ps, 0.5))
	if rate > 0.20 {
		t.Errorf("drift makes %.1f%% of ordinary traffic look novel; the reference set is not keeping up", rate*100)
	}
}
