package features

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"nazar/internal/contract"
)

type registryEntry struct {
	ID           string   `yaml:"id"`
	Version      int      `yaml:"version"`
	Description  string   `yaml:"description"`
	RequiresKeys []string `yaml:"requires_keys"`
	Provenance   string   `yaml:"provenance"`
	CostToForge  string   `yaml:"cost_to_forge"`
	Monotone     string   `yaml:"monotone"`
	Guard        string   `yaml:"guard"`
	NAWhen       []string `yaml:"na_when"`
	Rails        []string `yaml:"rails"`
	Catches      []string `yaml:"catches"`
}

// LoadRegistry reads features/registry.yaml — the single source of truth shared with the
// Python trainer (docs/00 §9, docs/02 §4). It is data, not code: adding a feature here does
// not require a Go code change to be visible to test_feature_catalogue_key_coverage.
func LoadRegistry(repoRoot string) ([]contract.FeatureDef, error) {
	path := filepath.Join(repoRoot, "features", "registry.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("features: reading registry at %s: %w", path, err)
	}
	var entries []registryEntry
	if err := yaml.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("features: parsing registry: %w", err)
	}
	defs := make([]contract.FeatureDef, 0, len(entries))
	for _, e := range entries {
		rails := make([]contract.Rail, 0, len(e.Rails))
		for _, r := range e.Rails {
			rails = append(rails, contract.Rail(r))
		}
		defs = append(defs, contract.FeatureDef{
			ID:           e.ID,
			Version:      e.Version,
			Description:  e.Description,
			RequiresKeys: e.RequiresKeys,
			Provenance:   contract.Provenance(e.Provenance),
			CostToForge:  e.CostToForge,
			Monotone:     e.Monotone,
			Rails:        rails,
			Catches:      e.Catches,
		})
	}
	return defs, nil
}

// FindRepoRoot walks upward from the given directory looking for features/registry.yaml.
// Used so the binary works whether it's launched from go/, from the repo root, or as a
// built artefact with NAZAR_REPO_ROOT set explicitly.
func FindRepoRoot(start string) (string, error) {
	if env := os.Getenv("NAZAR_REPO_ROOT"); env != "" {
		return env, nil
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("features: resolving absolute path for %s: %w", start, err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "features", "registry.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("features: could not locate repo root (features/registry.yaml) above %s", start)
}
