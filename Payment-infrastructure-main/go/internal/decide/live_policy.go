package decide

import "sync/atomic"

// PolicyRef is an optional hot-swap slot for the policy bundle.
//
// Why it exists: a risk owner tuning a threshold mid-incident is a real operation, and the
// console's Policy Studio drives it live. The scoring path reads the policy on every
// decision from many goroutines at once, so the swap has to be a single atomic pointer
// store rather than a field mutation — otherwise "move the slider" is a data race.
//
// The versioned YAML bundle on disk stays the source of truth. A tuned policy is stamped
// with a derived version (`<base>+tuned.<n>`) so no decision can ever claim it ran under the
// approved bundle when it did not (docs/00 §10; CLAUDE.md's "every decision stamps
// policy_version"). Reset restores the on-disk bundle exactly.
//
// Engines constructed without a Live ref (every test, via the struct literal in
// test/invariants/testengine.go) behave exactly as before: Load returns nil and the
// statically-configured Policy field is used.
type PolicyRef struct {
	p atomic.Pointer[Policy]
}

func NewPolicyRef(p *Policy) *PolicyRef {
	r := &PolicyRef{}
	r.p.Store(p)
	return r
}

// Load returns the current live policy, or nil if this ref is nil or unset.
func (r *PolicyRef) Load() *Policy {
	if r == nil {
		return nil
	}
	return r.p.Load()
}

func (r *PolicyRef) Store(p *Policy) {
	if r == nil {
		return
	}
	r.p.Store(p)
}

// Clone deep-copies every map a tuned policy may touch, so a candidate policy can be edited
// and evaluated without mutating the one live decisions are currently reading.
func (p *Policy) Clone() *Policy {
	cp := *p
	cp.Economics.LossGivenFraud = copyFloatMap(p.Economics.LossGivenFraud)
	cp.Economics.FrictionCostMinor = copyIntMap(p.Economics.FrictionCostMinor)
	cp.Economics.AbandonProb = copyFloatMap(p.Economics.AbandonProb)
	cp.Economics.StopProb = copyFloatMap(p.Economics.StopProb)
	cp.Ladder.Rungs = append([]string(nil), p.Ladder.Rungs...)
	cp.ControlGroup.Exempt = append([]string(nil), p.ControlGroup.Exempt...)
	cp.ApprovedBy = append([]string(nil), p.ApprovedBy...)
	return &cp
}

func copyFloatMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyIntMap(m map[string]int64) map[string]int64 {
	if m == nil {
		return nil
	}
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
