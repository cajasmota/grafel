// issue4332_crosspkg_test.go — end-to-end live-pipeline validation for #4332.
//
// Bug: Go cross-package import/call resolution drops edges. On archigraph's own
// self-graph the internal/resolve package reported 0 importers though grep finds
// 5+ packages importing it, and cross-package CALLS (`resolve.BuildIndex()`) were
// collapsed to an ambiguity-prone bare name (`BuildIndex`) that dropped whenever
// two packages defined a symbol of the same name.
//
// These tests drive the REAL extraction + resolver passes — exactly the
// sequence cmd/archigraph/index.go runs — on faithful multi-package Go modules
// that use a realistic `module github.com/...` path (the bug is module-path
// keyed, so a toy bare module name would not reproduce it). Per-half unit tests
// already pass; the drop only surfaces when real extractor output flows through
// the real resolver, which is what these exercise.
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

// extractGoModuleForTest extracts every (relPath, src) file with the real Go
// extractor under a realistic RepoRoot (so go.mod is read and in-tree keying
// fires), stamps deterministic entity IDs like the indexer, and returns the
// merged record slice.
func extractGoModuleForTest(t *testing.T, module string, files map[string]string) []types.EntityRecord {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+module+"\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
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

func is16Hex(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// TestIssue4332_ImportersConnectAcrossPackages confirms a package imported by
// several in-tree packages reports those importers after resolution — the
// "internal/resolve has 0 importers though grep finds 5+" symptom.
func TestIssue4332_ImportersConnectAcrossPackages(t *testing.T) {
	const module = "github.com/acme/widgets"
	files := map[string]string{
		"internal/resolve/build.go":     "package resolve\nfunc BuildIndex() int { return 0 }\n",
		"internal/resolve/imports.go":   "package resolve\nfunc ResolveImports() int { return 1 }\n",
		"internal/resolve/refs.go":      "package resolve\nfunc References() int { return 2 }\n",
		"internal/engine/engine.go":     "package engine\nimport \"" + module + "/internal/resolve\"\nfunc Run() int { return resolve.BuildIndex() }\n",
		"internal/enrichment/repair.go": "package enrichment\nimport \"" + module + "/internal/resolve\"\nfunc Repair() int { return resolve.ResolveImports() }\n",
		"internal/external/synth.go":    "package external\nimport \"" + module + "/internal/resolve\"\nfunc Synth() int { return resolve.References() }\n",
		"cmd/app/index.go":              "package main\nimport \"" + module + "/internal/resolve\"\nfunc idx() int { return resolve.BuildIndex() }\n",
		"cmd/app/daemon.go":             "package main\nimport \"" + module + "/internal/resolve\"\nfunc dm() int { return resolve.BuildIndex() }\n",
	}
	merged := extractGoModuleForTest(t, module, files)

	resolveFileIDs := map[string]bool{}
	for k := range merged {
		e := merged[k]
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" &&
			filepath.Dir(e.SourceFile) == "internal/resolve" {
			resolveFileIDs[e.ID] = true
		}
	}

	countConnected := func() int {
		conn := map[string]bool{}
		for k := range merged {
			for j := range merged[k].Relationships {
				r := merged[k].Relationships[j]
				if r.Kind == "IMPORTS" && resolveFileIDs[r.ToID] && is16Hex(r.FromID) {
					conn[r.FromID] = true
				}
			}
		}
		return len(conn)
	}

	if c := countConnected(); c != 0 {
		t.Fatalf("precondition: expected 0 resolved importers pre-pass, got %d", c)
	}

	resolve.ResolveGoInTreeImports(merged)
	idx := resolve.BuildIndex(merged)
	resolve.ReferencesEmbedded(merged, idx)

	after := countConnected()
	t.Logf("internal/resolve connected importers: before=0 after=%d", after)
	if after < 5 {
		t.Errorf("internal/resolve has %d graph-connected importers, want 5 (#4332)", after)
	}
}

// TestIssue4332_CrossPackageCall_NameCollision is the core regression. engine
// calls resolve.BuildIndex(), and a second package (symbols) ALSO defines
// BuildIndex. Before the fix the qualifier was dropped and the bare name went
// ambiguous → the CALLS edge bound nowhere. After the fix it binds to exactly
// resolve.BuildIndex via the stamped package directory.
func TestIssue4332_CrossPackageCall_NameCollision(t *testing.T) {
	const module = "github.com/acme/widgets"
	files := map[string]string{
		"internal/resolve/build.go": "package resolve\nfunc BuildIndex() int { return 0 }\n",
		"internal/symbols/build.go": "package symbols\nfunc BuildIndex() int { return 99 }\n",
		"internal/engine/engine.go": "package engine\nimport \"" + module + "/internal/resolve\"\nfunc Run() int { return resolve.BuildIndex() }\n",
	}
	merged := extractGoModuleForTest(t, module, files)

	var resolveBuildID, symbolsBuildID string
	for k := range merged {
		e := merged[k]
		if e.Name == "BuildIndex" {
			if e.SourceFile == "internal/resolve/build.go" {
				resolveBuildID = e.ID
			}
			if e.SourceFile == "internal/symbols/build.go" {
				symbolsBuildID = e.ID
			}
		}
	}
	if resolveBuildID == "" || symbolsBuildID == "" {
		t.Fatal("missing BuildIndex entities")
	}

	// Confirm the extractor stamped the package qualifier on the cross-package
	// CALLS edge (the structural carrier the resolver consumes).
	stamped := false
	for k := range merged {
		if merged[k].SourceFile != "internal/engine/engine.go" {
			continue
		}
		for _, r := range merged[k].Relationships {
			if r.Kind == "CALLS" && r.Properties["go_call_pkg_dir"] == "internal/resolve" &&
				r.Properties["call_leaf"] == "BuildIndex" {
				stamped = true
			}
		}
	}
	if !stamped {
		t.Fatal("extractor did not stamp go_call_pkg_dir/call_leaf on resolve.BuildIndex() CALL")
	}

	resolve.ResolveGoInTreeImports(merged)
	importTbl := resolve.BuildImportTable(merged)
	resolve.ResolveImports(merged, importTbl)
	idx := resolve.BuildIndex(merged)
	idx.ResolveGoCrossPackageCalls(merged)
	resolve.ReferencesEmbedded(merged, idx)

	for k := range merged {
		if merged[k].SourceFile != "internal/engine/engine.go" {
			continue
		}
		for _, r := range merged[k].Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			switch r.ToID {
			case resolveBuildID:
				// correct
			case symbolsBuildID:
				t.Errorf("CALL bound to symbols.BuildIndex (wrong package — qualifier ignored)")
			default:
				t.Errorf("cross-package CALL resolve.BuildIndex() unresolved/ambiguous, ToID=%q", r.ToID)
			}
		}
	}
}

// TestIssue4332_NoFalseStampOnLocalCall guards against over-eager stamping: a
// call on a local variable / receiver (`s.Helper()`, `buf.Write()`) whose
// operand merely shares a name with no in-tree import must NOT be stamped with
// go_call_pkg_dir.
func TestIssue4332_NoFalseStampOnLocalCall(t *testing.T) {
	const module = "github.com/acme/widgets"
	files := map[string]string{
		// `resolve` here is a LOCAL VARIABLE, not the imported package.
		"internal/engine/local.go": "package engine\n" +
			"type R struct{}\n" +
			"func (r R) Build() int { return 1 }\n" +
			"func Use() int {\n\tresolve := R{}\n\treturn resolve.Build()\n}\n",
	}
	merged := extractGoModuleForTest(t, module, files)
	for k := range merged {
		for _, r := range merged[k].Relationships {
			if r.Kind == "CALLS" && r.Properties["go_call_pkg_dir"] != "" {
				t.Errorf("local-var call wrongly stamped go_call_pkg_dir=%q (no import named that)",
					r.Properties["go_call_pkg_dir"])
			}
		}
	}
}
