// Package rules implements CEL rule evaluation (docs/00 §6: "CEL only. Never eval. F-72").
// Predicates are compiled once at bundle load, not per request.
package rules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/cel-go/cel"
	"gopkg.in/yaml.v3"

	"nazar/internal/contract"
)

type Class string

const (
	ClassRegulatory Class = "regulatory"
	ClassPolicy     Class = "policy"
)

type RuleAction string

const (
	ActionCap                RuleAction = "CAP"
	ActionStepUpInterstitial RuleAction = "STEP_UP_INTERSTITIAL"
	ActionStepUp             RuleAction = "STEP_UP"
	ActionNone               RuleAction = "NONE" // rule-feature only — docs/02 §4.5
)

type ruleDef struct {
	ID          string   `yaml:"id"`
	Class       Class    `yaml:"class"`
	Authority   string   `yaml:"authority"`
	VerifiedOn  string   `yaml:"verified_on"`
	Predicate   string   `yaml:"predicate"`
	Action      RuleAction `yaml:"action"`
	CapMinor    int64    `yaml:"cap_minor"`
	Rails       []string `yaml:"rails"`
	Explanation string   `yaml:"explanation"`
}

type bundleFile struct {
	Version       string    `yaml:"version"`
	EffectiveFrom string    `yaml:"effective_from"`
	Rules         []ruleDef `yaml:"rules"`
}

type compiledRule struct {
	def     ruleDef
	program cel.Program
}

// Result is what firing (or not) one rule produces.
type Result struct {
	RuleID      string
	Class       Class
	Fired       bool
	Action      RuleAction
	CapMinor    int64
	Explanation string
	Authority   string
	VerifiedOn  string
}

// Engine holds the compiled rule bundle. Immutable after NewEngine — swapping bundles means
// constructing a new Engine and atomically repointing the holder (docs/00 §3.3).
type Engine struct {
	Version string
	rules   []compiledRule
}

var celEnv *cel.Env

func init() {
	env, err := cel.NewEnv(
		cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("payer", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("payee", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("pair", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("device", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("geo_jump_kmh", cel.DoubleType),
	)
	if err != nil {
		panic(fmt.Sprintf("rules: building CEL environment: %v", err))
	}
	celEnv = env
}

func LoadEngine(repoRoot, filename string) (*Engine, error) {
	path := filepath.Join(repoRoot, "rules", filename)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rules: reading bundle %s: %w", path, err)
	}
	var bf bundleFile
	if err := yaml.Unmarshal(b, &bf); err != nil {
		return nil, fmt.Errorf("rules: parsing bundle: %w", err)
	}
	eng := &Engine{Version: bf.Version}
	for _, rd := range bf.Rules {
		ast, iss := celEnv.Compile(rd.Predicate)
		if iss != nil && iss.Err() != nil {
			return nil, fmt.Errorf("rules: compiling %s: %w", rd.ID, iss.Err())
		}
		prg, err := celEnv.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("rules: programming %s: %w", rd.ID, err)
		}
		eng.rules = append(eng.rules, compiledRule{def: rd, program: prg})
	}
	return eng, nil
}

// Evaluate runs every rule in the bundle against the activation built from
// (event, profile, features). It returns one Result per rule (so rule-features can be
// emitted even for rules with Action=NONE) plus the fired subset for rail purposes.
func (e *Engine) Evaluate(ev *contract.Event, pb *contract.ProfileBundle, fv *contract.FeatureVector) []Result {
	activation := buildActivation(ev, pb, fv)
	results := make([]Result, 0, len(e.rules))
	for _, cr := range e.rules {
		fired := false
		if railApplies(cr.def.Rails, ev.Rail) {
			out, _, err := cr.program.Eval(activation)
			if err == nil {
				if b, ok := out.Value().(bool); ok {
					fired = b
				}
			}
		}
		results = append(results, Result{
			RuleID:      cr.def.ID,
			Class:       cr.def.Class,
			Fired:       fired,
			Action:      cr.def.Action,
			CapMinor:    cr.def.CapMinor,
			Explanation: cr.def.Explanation,
			Authority:   cr.def.Authority,
			VerifiedOn:  cr.def.VerifiedOn,
		})
	}
	return results
}

// railApplies implements the per-rule rail scoping every rule bundle entry declares
// (docs/04 §3.1: RAIL-001 is UPI/IMPS only). An empty Rails list means "applies everywhere".
func railApplies(rails []string, rail contract.Rail) bool {
	if len(rails) == 0 {
		return true
	}
	for _, r := range rails {
		if contract.Rail(r) == rail {
			return true
		}
	}
	return false
}

func buildActivation(ev *contract.Event, pb *contract.ProfileBundle, fv *contract.FeatureVector) map[string]any {
	pairHours := 0.0 // no prior relationship reads as "brand new" — the cooling rail's job
	if pb.Pair.Present && pb.Pair.FirstAddedAtMs > 0 {
		pairHours = float64(ev.AcceptedAtMs-pb.Pair.FirstAddedAtMs) / 3_600_000.0
	}
	geoJump := 0.0
	if v, ok := fv.Values["geo_jump_kmh"]; ok && fv.Status["geo_jump_kmh"] == contract.StatusClear {
		geoJump = v
	}
	burstiness := 0.0
	if v, ok := fv.Values["payee_fanin_burstiness"]; ok && fv.Status["payee_fanin_burstiness"] == contract.StatusClear {
		burstiness = v
	}
	return map[string]any{
		"event": map[string]any{
			"amount_minor": ev.InstructedAmountMinor,
			"rail":         string(ev.Rail),
			"initiation":   ev.Initiation,
		},
		"payer": map[string]any{
			"txn_count_1h":          pb.Payer.TxnVelocity1h,
			"baseline_txn_1h_p999":  pb.Payer.Txn1hP999,
		},
		"payee": map[string]any{
			"is_new_to_payer":  featureBool(fv, "payee_is_new_to_payer"),
			"fanin_24h":        pb.Payee.Fanin24h,
			"fanin_burstiness": burstiness,
		},
		"pair": map[string]any{
			"first_added_within_hours": pairHours,
			"txn_count_90d":            pb.Pair.TxnCount90d,
		},
		"device": map[string]any{
			"is_new_to_payer": featureBool(fv, "device_is_new_to_payer"),
		},
		"geo_jump_kmh": geoJump,
	}
}

func featureBool(fv *contract.FeatureVector, id string) bool {
	if fv.Status[id] != contract.StatusClear {
		return false
	}
	return fv.Values[id] == 1
}
