// Package decide implements the decision engine: the ordered stages in docs/04 §1
// (local filters + regulatory rails -> trusted-pair fast path -> score -> expected-cost
// minimisation -> policy rails -> advisory attachment) and the versioned policy bundle
// that parameterises them (docs/00 §10).
package decide

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"nazar/internal/contract"
)

type Economics struct {
	LossGivenFraud     map[string]float64 `yaml:"loss_given_fraud"`
	FrictionCostMinor  map[string]int64   `yaml:"friction_cost_minor"`
	AbandonProb        map[string]float64 `yaml:"abandon_prob"`
	StopProb           map[string]float64 `yaml:"stop_prob"`
	MarginMinor        int64              `yaml:"margin_minor"`
}

type Ladder struct {
	Rungs               []string `yaml:"rungs"`
	AdvisoryMaxRung      string   `yaml:"advisory_max_rung"`
	AdvisoryMaxSteps     int      `yaml:"advisory_max_steps"`
	MinReporterReputation float64 `yaml:"min_reporter_reputation"`
	MaxAdvisoryAgeHours  float64  `yaml:"max_advisory_age_hours"`
	MinAdvisoryConfidence float64 `yaml:"min_advisory_confidence"`
}

type TrustedPair struct {
	MinTxnCount90d      int64   `yaml:"min_txn_count_90d"`
	AmountHeadroomRatio float64 `yaml:"amount_headroom_ratio"`
	SampleRate          float64 `yaml:"sample_rate"`
}

type Degradation struct {
	ValueCapMinor int64 `yaml:"value_cap_minor"`
}

type ControlGroup struct {
	Fraction float64  `yaml:"fraction"`
	Exempt   []string `yaml:"exempt"`
}

type Policy struct {
	Version       string       `yaml:"version"`
	EffectiveFrom string       `yaml:"effective_from"`
	ApprovedBy    []string     `yaml:"approved_by"`
	Economics     Economics    `yaml:"economics"`
	Ladder        Ladder       `yaml:"ladder"`
	TrustedPair   TrustedPair  `yaml:"trusted_pair"`
	Degradation   Degradation  `yaml:"degradation"`
	ControlGroup  ControlGroup `yaml:"control_group"`
}

func LoadPolicy(repoRoot, filename string) (*Policy, error) {
	path := filepath.Join(repoRoot, "policy", filename)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("decide: reading policy %s: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("decide: parsing policy: %w", err)
	}
	return &p, nil
}

func (p *Policy) advisoryMaxRungAction() contract.Action {
	return contract.Action(p.Ladder.AdvisoryMaxRung)
}
