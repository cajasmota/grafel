package mcp

// get_source_nested_module_5682_test.go — regression coverage for #5682.
//
// THE BUG: get_source fails for files in nested build units (monorepos with
// per-module go.mod / package.json / pom.xml / *.csproj). The indexer records
// an entity's source_file RELATIVE TO ITS NESTED-MODULE ROOT (e.g. the file
// services/billing/pkg/handler.go is recorded as "pkg/handler.go" because the
// go.mod that anchors it lives at services/billing/). The read side, however,
// joins the recorded source_file onto the GROUP/REPO root (LoadedRepo.Path):
//
//	abs = filepath.Join(lr.Path, e.SourceFile)   // <root>/pkg/handler.go — WRONG
//
// which points at a file that does not exist, and get_source returns
//
//	lstat <root>/pkg/handler.go: no such file or directory
//
// even though the file is really at <root>/services/billing/pkg/handler.go.
//
// THE FIX (read-side, lowest risk): resolveEntitySourcePath keeps the exact
// happy-path join for files that live at the group root (single os.Stat), and
// only on a not-exist result falls back — sibling loaded repos, then a bounded
// unique-suffix walk from the repo root — to recover the real path.
//
// Non-vacuous proof: TestGetSource_NestedModuleRelativePath_5682 FAILS on the
// pre-fix handler with the lstat error above. TestGetSource_RootLevelEntity_5682
// pins the HARD CONSTRAINT that group-root files resolve EXACTLY as today.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// newTestServerWithRepoPath builds a Server whose single "test" group has one
// repo whose LoadedRepo.Path is set to repoRoot (unlike newTestServer, which
// leaves Path empty). This is required to exercise the join(lr.Path, ...) path
// that the #5682 bug lives on.
func newTestServerWithRepoPath(t *testing.T, repoSlug, repoRoot string, doc *graph.Document) *Server {
	t.Helper()
	doc.Repo = repoSlug
	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{repoSlug: {Path: repoRoot}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{}}
	lg.Repos[repoSlug] = &LoadedRepo{
		Repo:       repoSlug,
		Path:       repoRoot,
		Doc:        doc,
		LabelIndex: BuildLabelIndex(doc),
		BM25:       BuildBM25(doc),
	}
	st.groups["test"] = lg
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

// TestGetSource_NestedModuleRelativePath_5682 is the load-bearing regression:
// the entity's source_file is recorded RELATIVE TO ITS NESTED-MODULE ROOT
// ("pkg/handler.go"), but the real file lives deeper at
// services/billing/pkg/handler.go under the repo root. Pre-fix, get_source
// stat's <root>/pkg/handler.go and fails; post-fix the bounded suffix walk
// recovers the real path.
func TestGetSource_NestedModuleRelativePath_5682(t *testing.T) {
	root := t.TempDir()

	// Nested build unit: services/billing/ carries its own go.mod, so the
	// indexer records files under it module-relative (stripped of the module
	// root prefix).
	moduleDir := filepath.Join(root, "services", "billing")
	pkgDir := filepath.Join(moduleDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"),
		[]byte("module example.com/billing\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	handlerSrc := strings.Join([]string{
		"package pkg",
		"",
		"func Charge(amount int) int {",
		"\t// MARKER_NESTED_CHARGE_BODY",
		"\treturn amount",
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(pkgDir, "handler.go"),
		[]byte(handlerSrc), 0o644); err != nil {
		t.Fatalf("write handler: %v", err)
	}

	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:            "ent_charge",
			Name:          "Charge",
			QualifiedName: "example.com/billing/pkg.Charge",
			Kind:          "SCOPE.Operation", Subtype: "function",
			// Module-relative — NOT relative to the repo root.
			SourceFile: "pkg/handler.go",
			StartLine:  3, EndLine: 6, Language: "go",
			Properties: map[string]string{"module": "pkg"},
		}},
	}
	srv := newTestServerWithRepoPath(t, "billing-mono", root, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "ent_charge"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source returned tool error (the #5682 lstat bug):\n%s",
			extractResultText(t, res))
	}
	text := extractResultText(t, res)
	if !strings.Contains(text, "MARKER_NESTED_CHARGE_BODY") {
		t.Errorf("nested-module source not returned, got:\n%s", text)
	}
}

// TestGetSource_RootLevelEntity_5682 pins the HARD CONSTRAINT: an entity whose
// source_file lives directly at the group/repo root resolves EXACTLY as today
// (the plain join, no fallback needed). Guards against a regression where the
// fallback machinery changes happy-path behaviour.
func TestGetSource_RootLevelEntity_5682(t *testing.T) {
	root := t.TempDir()
	mainSrc := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\t// MARKER_ROOT_MAIN_BODY",
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte(mainSrc), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:            "ent_main",
			Name:          "main",
			QualifiedName: "main.main",
			Kind:          "SCOPE.Operation", Subtype: "function",
			SourceFile: "main.go",
			StartLine:  3, EndLine: 5, Language: "go",
		}},
	}
	srv := newTestServerWithRepoPath(t, "root-repo", root, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "ent_main"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("root-level get_source should succeed, got error:\n%s",
			extractResultText(t, res))
	}
	text := extractResultText(t, res)
	if !strings.Contains(text, "MARKER_ROOT_MAIN_BODY") {
		t.Errorf("root-level source not returned, got:\n%s", text)
	}
}

// TestGetSource_SiblingCollisionNotReturned_5682 pins review defect D2: a
// colliding relative path present at a SIBLING repo's root must NOT be returned
// when the entity's own repo (lr) holds the real nested file. The entity is
// resolved to lr, so its source lives in lr; returning another repo's file for
// a common path ("pkg/handler.go") would be silently-wrong source.
func TestGetSource_SiblingCollisionNotReturned_5682(t *testing.T) {
	// repoA (the entity's repo): nested build unit; real file is deep.
	rootA := t.TempDir()
	nestedDir := filepath.Join(rootA, "services", "billing", "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "handler.go"),
		[]byte("package pkg\n\n// MARKER_CORRECT_REPO_A\nfunc Charge() {}\n"), 0o644); err != nil {
		t.Fatalf("write A: %v", err)
	}

	// repoB (a sibling in the same group): a DECOY file at the same relative
	// path but at the repo ROOT — the collision that D2 warns about.
	rootB := t.TempDir()
	decoyDir := filepath.Join(rootB, "pkg")
	if err := os.MkdirAll(decoyDir, 0o755); err != nil {
		t.Fatalf("mkdir B: %v", err)
	}
	if err := os.WriteFile(filepath.Join(decoyDir, "handler.go"),
		[]byte("package pkg\n\n// MARKER_WRONG_REPO_B\nfunc Other() {}\n"), 0o644); err != nil {
		t.Fatalf("write B: %v", err)
	}

	docA := &graph.Document{
		Repo: "billing",
		Entities: []graph.Entity{{
			ID: "ent_charge", Name: "Charge",
			QualifiedName: "billing.pkg.Charge",
			Kind:          "SCOPE.Operation", Subtype: "function",
			SourceFile: "pkg/handler.go", StartLine: 3, EndLine: 4, Language: "go",
		}},
	}
	docB := &graph.Document{Repo: "gateway"} // no entities; just the decoy tree

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{
			"billing": {Path: rootA},
			"gateway": {Path: rootB},
		}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	lg := &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{
		"billing": {Repo: "billing", Path: rootA, Doc: docA, LabelIndex: BuildLabelIndex(docA), BM25: BuildBM25(docA)},
		"gateway": {Repo: "gateway", Path: rootB, Doc: docB, LabelIndex: BuildLabelIndex(docB), BM25: BuildBM25(docB)},
	}}
	st.groups["test"] = lg
	st.mu.Unlock()
	srv := &Server{State: st, Tel: NewTelemetry(0)}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "billing::ent_charge"}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source returned tool error:\n%s", extractResultText(t, res))
	}
	text := extractResultText(t, res)
	if !strings.Contains(text, "MARKER_CORRECT_REPO_A") {
		t.Errorf("expected the in-repo nested file, got:\n%s", text)
	}
	if strings.Contains(text, "MARKER_WRONG_REPO_B") {
		t.Errorf("D2 regression: returned the SIBLING repo's colliding file:\n%s", text)
	}
}

// TestGetSource_SuffixIndexBuiltOncePerRepo_5682 pins review defect D1: the
// filesystem tree is walked AT MOST ONCE per repo, no matter how many
// nested-module lookups occur. It wraps the buildSuffixIndex seam with a call
// counter and drives repeated resolveEntitySourcePath lookups that all miss the
// happy-path stat (so each would have triggered a full walk in the pre-review
// implementation).
func TestGetSource_SuffixIndexBuiltOncePerRepo_5682(t *testing.T) {
	root := t.TempDir()
	nestedDir := filepath.Join(root, "services", "billing", "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "handler.go"),
		[]byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Wrap the build seam with a counter (restored after the test). Tests in a
	// package run sequentially unless t.Parallel is called, so the swap is safe.
	orig := buildSuffixIndex
	defer func() { buildSuffixIndex = orig }()
	var mu sync.Mutex
	builds := 0
	buildSuffixIndex = func(r string) (map[string][]string, bool) {
		mu.Lock()
		builds++
		mu.Unlock()
		return orig(r)
	}

	lr := &LoadedRepo{Repo: "billing", Path: root}

	// Many lookups on nested (happy-path-missing) source files.
	for i := 0; i < 8; i++ {
		got := resolveEntitySourcePath(lr, "pkg/handler.go")
		want := filepath.Join(root, "services", "billing", "pkg", "handler.go")
		if got != want {
			t.Fatalf("lookup %d: got %q, want %q", i, got, want)
		}
	}
	// A genuine miss should also be cheap (no extra build) — pins N2.
	if got := resolveEntitySourcePath(lr, "pkg/missing.go"); got != filepath.Join(root, "pkg", "missing.go") {
		t.Errorf("genuine miss should return the primary join, got %q", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if builds != 1 {
		t.Errorf("D1: suffix index built %d times across repeated lookups, want exactly 1", builds)
	}
}
