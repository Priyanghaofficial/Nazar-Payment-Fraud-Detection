// Package novelty implements the unsupervised detector (docs/00 §6, docs/06 Milestone 6):
// k-NN distance in feature space turned into a conformal p-value. It ships in `shadow` — it
// may never BLOCK — and D5's four-state status applies to it like every other signal.
//
// Why it matters disproportionately: this is the only detector in the system that needs no
// fraud labels at all. It learns the shape of ordinary traffic and reports how unusual a
// payment is against that, so an attack pattern nobody has labelled yet is still unusual on
// the day it first appears. The supervised model cannot do that by construction.
//
// P0 substitution, stated honestly: docs/00 §6 specifies leaf-space kNN (embedding a
// transaction by which leaf of the trained GBM it lands in per tree). The `leaves` inference
// library used for scoring does not expose per-tree leaf indices, so this uses the robustly
// scaled numeric feature vector as the embedding space instead. The conformal mechanism —
// the part that makes "how unusual is this" a calibrated number rather than a hunch — is real.
package novelty

import (
	"math"
	"sort"
	"sync"
)

const (
	maxCalibrationSize = 1500
	kNeighbours        = 10
	// A conformal p-value is only as trustworthy as the traffic it was calibrated on. Thirty
	// points is enough to compute a number and nowhere near enough for that number to mean
	// anything: early in a run, per-account velocity counters are still ramping from zero, so
	// a reservoir of the first thirty payments describes a regime that no longer exists by
	// the time anyone looks. Saying COLD_START for longer is the honest answer.
	minCalibration     = 200
	// Recomputing the calibration set's own nonconformity scores is O(m^2). It runs in the
	// async lane, never on the scoring path, and only every refreshEvery observations.
	// The reference set must track the traffic it is meant to describe. Refreshing rarely
	// means comparing live payments against a stale snapshot, which under any upward drift
	// makes ordinary traffic look progressively more novel until the detector flags most of
	// it. O(m^2) in the async lane is affordable; a detector nobody can trust is not.
	refreshEvery = 50
	// alphaSampleCap bounds that O(m^2): the reference distribution is estimated from a
	// bounded sample of the reservoir rather than all of it.
	alphaSampleCap = 500
)

type point struct {
	values map[string]float64
}

// Engine holds a bounded reservoir of recent feature vectors as the conformal calibration
// set, plus the cached reference distribution derived from it.
type Engine struct {
	mu   sync.Mutex
	pts  []point
	next int
	dims []string

	// scale is a per-dimension robust spread (IQR-based) used to normalise distances.
	// Without it, Euclidean distance is dominated entirely by whichever feature happens to
	// be denominated in paise, and "unusual" degenerates into "large amount".
	scale map[string]float64

	// alphas is the sorted nonconformity distribution of the calibration set itself: for
	// each sampled calibration point, its own distance to its k-th nearest neighbour among
	// the others. A new point's p-value is its rank against this distribution.
	//
	// This is the part that has to be right. Comparing a new point's k-NN distance against
	// the raw distances to every calibration point (rather than against their own k-NN
	// distances) yields a p-value of roughly (n-k)/n for every input — near-constant, and
	// therefore a detector that can never fire.
	alphas       []float64

	// ref is the calibration set the alphas were measured against, kept verbatim.
	//
	// A conformal p-value is only meaningful if the new point's nonconformity is measured
	// the same way the calibration scores were. alphas are k-NN distances within a bounded
	// SAMPLE of the reservoir; measuring a new point against the whole reservoir instead
	// compares it to a denser cloud, so its k-th neighbour is systematically nearer and it
	// looks more normal than it is. Holding the reference set makes both sides identical.
	ref          []point
	sinceRefresh int
}

func NewEngine() *Engine { return &Engine{} }

// Observe adds a scored transaction's feature vector to the calibration reservoir. Called
// from the async lane for every LIVE decision — not just flagged ones — so the reservoir
// reflects the traffic distribution rather than the alerts it produced.
func (e *Engine) Observe(values map[string]float64, dims []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dims == nil {
		e.dims = dims
	}
	p := point{values: copyMap(values)}
	if len(e.pts) < maxCalibrationSize {
		e.pts = append(e.pts, p)
	} else {
		e.pts[e.next] = p
		e.next = (e.next + 1) % maxCalibrationSize
	}
	e.sinceRefresh++
	if len(e.pts) >= minCalibration && (e.alphas == nil || e.sinceRefresh >= refreshEvery) {
		e.refreshLocked()
		e.sinceRefresh = 0
	}
}

// refreshLocked recomputes the per-dimension scale and the reference nonconformity
// distribution. Caller must hold e.mu.
func (e *Engine) refreshLocked() {
	// 1. Robust per-dimension spread, so every feature contributes comparably.
	e.scale = make(map[string]float64, len(e.dims))
	col := make([]float64, 0, len(e.pts))
	for _, d := range e.dims {
		col = col[:0]
		for _, p := range e.pts {
			if v, ok := p.values[d]; ok && !math.IsNaN(v) {
				col = append(col, v)
			}
		}
		e.scale[d] = robustSpread(col)
	}

	// 2. Reference distribution: each sampled point's own k-NN distance to the others.
	sample := e.pts
	if len(sample) > alphaSampleCap {
		// Deterministic stride sample — no RNG, so behaviour stays reproducible.
		stride := len(sample) / alphaSampleCap
		if stride < 1 {
			stride = 1
		}
		s := make([]point, 0, alphaSampleCap)
		for i := 0; i < len(sample) && len(s) < alphaSampleCap; i += stride {
			s = append(s, sample[i])
		}
		sample = s
	}
	alphas := make([]float64, 0, len(sample))
	for i := range sample {
		alphas = append(alphas, kthDistance(sample[i].values, e.dims, e.scale, sample, kNeighbours, i))
	}
	sort.Float64s(alphas)
	e.alphas = alphas
	e.ref = sample
}

// Result is the novelty finding: a conformal p-value (small = unusual) plus the scaled k-NN
// distance it came from, for the explanation.
type Result struct {
	Evaluated   bool
	PValue      float64
	KNNDistance float64
	Reason      string
}

// Evaluate computes the split-conformal p-value for one feature vector:
//
//	p = (#{calibration points at least as nonconforming as this one} + 1) / (n + 1)
//
// where nonconformity is distance to the k-th nearest neighbour, measured in robustly scaled
// feature space. p is approximately uniform on ordinary traffic and small for genuinely
// unusual traffic — which is what makes "only 2% of recent payments look this unusual" a
// statement with a defined meaning.
func (e *Engine) Evaluate(values map[string]float64) Result {
	e.mu.Lock()
	dims := e.dims
	scale := e.scale
	alphas := e.alphas
	nPts := len(e.pts)
	ref := make([]point, len(e.ref))
	copy(ref, e.ref)
	e.mu.Unlock()

	if nPts < minCalibration || dims == nil || len(alphas) == 0 || len(ref) == 0 {
		return Result{Evaluated: false, Reason: "COLD_START — not enough recent traffic yet to say what normal looks like"}
	}

	// Measured against the same reference set the alphas came from — see Engine.ref.
	alpha := kthDistance(values, dims, scale, ref, kNeighbours, -1)

	idx := sort.SearchFloat64s(alphas, alpha)
	ge := len(alphas) - idx
	pValue := float64(ge+1) / float64(len(alphas)+1)

	return Result{Evaluated: true, PValue: pValue, KNNDistance: alpha}
}

// kthDistance returns the distance from target to its k-th nearest neighbour among pts,
// skipping index `skip` (use -1 to skip nothing) so a calibration point is never counted as
// its own nearest neighbour.
func kthDistance(target map[string]float64, dims []string, scale map[string]float64,
	pts []point, k int, skip int) float64 {
	dists := make([]float64, 0, len(pts))
	for i, p := range pts {
		if i == skip {
			continue
		}
		dists = append(dists, scaledEuclidean(target, p.values, dims, scale))
	}
	if len(dists) == 0 {
		return 0
	}
	sort.Float64s(dists)
	if k > len(dists) {
		k = len(dists)
	}
	if k < 1 {
		return 0
	}
	return dists[k-1]
}

func scaledEuclidean(a, b map[string]float64, dims []string, scale map[string]float64) float64 {
	var sum float64
	for _, d := range dims {
		av, aok := a[d]
		bv, bok := b[d]
		if !aok || !bok || math.IsNaN(av) || math.IsNaN(bv) {
			continue // a dimension missing on either side contributes nothing, never a zero
		}
		s := 1.0
		if scale != nil {
			if v, ok := scale[d]; ok && v > 0 {
				s = v
			}
		}
		diff := (av - bv) / s
		sum += diff * diff
	}
	return math.Sqrt(sum)
}

// robustSpread returns an IQR-based scale, falling back to standard deviation and then to 1.
// Robust rather than standard deviation because the reservoir legitimately contains
// outliers, and one large payment should not flatten a whole dimension.
func robustSpread(col []float64) float64 {
	if len(col) < 4 {
		return 1
	}
	s := append([]float64(nil), col...)
	sort.Float64s(s)
	q1 := s[len(s)/4]
	q3 := s[(3*len(s))/4]
	if iqr := q3 - q1; iqr > 1e-9 {
		return iqr
	}
	var mean float64
	for _, v := range s {
		mean += v
	}
	mean /= float64(len(s))
	var varsum float64
	for _, v := range s {
		varsum += (v - mean) * (v - mean)
	}
	if sd := math.Sqrt(varsum / float64(len(s))); sd > 1e-9 {
		return sd
	}
	return 1
}

func copyMap(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Reset clears the reservoir and the reference distribution, returning the detector to
// cold start. It will say COLD_START until it has seen enough traffic again, which is the
// honest state for a detector that has just been told to forget what normal looks like.
func (e *Engine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pts = nil
	e.next = 0
	e.dims = nil
	e.scale = nil
	e.alphas = nil
	e.ref = nil
	e.sinceRefresh = 0
}
