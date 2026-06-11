// issue4705_replace_test.go — end-to-end validation that go.mod local-path
// `replace` directives make a replaced import resolve to the in-repo package
// (#4705c). Mirrors the #4332 cross-package harness but writes a go.mod that
// carries a `replace example.com/x => ./internal/x` directive.
package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractGoModuleWithGoMod(t *testing.T, goMod string, files map[string]string) []types.EntityRecord {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// Caches are keyed by repoRoot (a fresh TempDir each run) so no isolation
	// reset is needed.
	ex := &GoExtractor{}
	var merged []types.EntityRecord
	for rel, src := range files {
		ents, err := ex.Extract(context.Background(), extractor.FileInput{
			Path: rel, Language: "go", Content: []byte(src), RepoRoot: dir,
		})
		if err != nil {
			t.Fatalf("extract %s: %v", rel, err)
		}
		merged = append(merged, ents...)
	}
	for k := range merged {
		if merged[k].Name == "" {
			continue
		}
		merged[k].ID = graph.EntityID("acme/widgets", merged[k].Kind, merged[k].Name, merged[k].SourceFile)
	}
	return merged
}

// TestIssue4705_ReplaceImportResolvesInternal: `import "example.com/x"` backed by
// `replace example.com/x => ./internal/x` binds to the in-repo file entity
// rather than falling through to external_package.
func TestIssue4705_ReplaceImportResolvesInternal(t *testing.T) {
	const module = "github.com/acme/widgets"
	goMod := "module " + module + "\n\ngo 1.22\n\nreplace example.com/x => ./internal/x\n"
	files := map[string]string{
		"internal/x/x.go":           "package x\nfunc Helper() int { return 0 }\n",
		"internal/engine/engine.go": "package engine\nimport \"example.com/x\"\nfunc Run() int { return x.Helper() }\n",
	}
	merged := extractGoModuleWithGoMod(t, goMod, files)

	// The extractor must stamp go_pkg_dir = "internal/x" on the replaced import.
	stamped := false
	for k := range merged {
		for j := range merged[k].Relationships {
			r := merged[k].Relationships[j]
			if r.Kind == "IMPORTS" && r.ToID == "example.com/x" {
				if r.Properties["go_pkg_dir"] == "internal/x" {
					stamped = true
				}
			}
		}
	}
	if !stamped {
		t.Fatal("replaced import example.com/x was not stamped go_pkg_dir=internal/x")
	}

	// Identify the in-repo internal/x file entity IDs.
	xFileIDs := map[string]bool{}
	for k := range merged {
		e := merged[k]
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" &&
			filepath.Dir(e.SourceFile) == "internal/x" {
			xFileIDs[e.ID] = true
		}
	}
	if len(xFileIDs) == 0 {
		t.Fatal("no internal/x file entity found")
	}

	// Count IMPORTS edges whose ToID now points at an internal/x file entity.
	connected := func() int {
		n := 0
		for k := range merged {
			for j := range merged[k].Relationships {
				r := merged[k].Relationships[j]
				if r.Kind == "IMPORTS" && xFileIDs[r.ToID] {
					n++
				}
			}
		}
		return n
	}

	if c := connected(); c != 0 {
		t.Fatalf("precondition: expected 0 resolved importers pre-pass, got %d", c)
	}

	n := resolve.ResolveGoInTreeImports(merged)
	t.Logf("ResolveGoInTreeImports rewrote %d edges; connected after=%d", n, connected())
	if connected() < 1 {
		t.Errorf("replaced import did not resolve to internal/x (#4705c)")
	}
}
