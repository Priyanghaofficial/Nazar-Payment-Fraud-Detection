package invariants

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nazar/internal/features"
)

// test_no_io_after_profile_load (docs/00 §9, docs/06 §4): internal/features may not import
// internal/profile or any client library. This is enforced structurally by this test
// parsing every .go file's import list — not by convention, not by code review.
func TestNoIOAfterProfileLoad(t *testing.T) {
	root, err := features.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	featuresDir := filepath.Join(root, "go", "internal", "features")

	forbidden := []string{
		"nazar/internal/profile",
		"github.com/redis/go-redis",
		"github.com/jackc/pgx",
		"database/sql",
		"net/http",
		"net",
	}

	entries, err := os.ReadDir(featuresDir)
	if err != nil {
		t.Fatalf("reading %s: %v", featuresDir, err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(featuresDir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		checked++
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, forb := range forbidden {
				if strings.HasPrefix(importPath, forb) {
					t.Errorf("%s imports %q — internal/features must be pure functions of (ProfileBundle, Event) with zero I/O (docs/00 §9)", e.Name(), importPath)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no .go files found in internal/features — path resolution is broken")
	}
}
