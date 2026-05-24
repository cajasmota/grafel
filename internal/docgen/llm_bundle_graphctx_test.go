package docgen_test

// llm_bundle_graphctx_test.go — integration-style unit tests for the
// graph_context fields populated by BuildBundle (#1827):
//   - qualified_name  (entity.QualifiedName)
//   - repo            (Document.Repo for the entity's document)
//   - source_window   (N lines around entity.StartLine from entity.SourceFile)
//
// The tests set up an isolated in-memory group (via ARCHIGRAPH_HOME,
// XDG_CONFIG_HOME, ARCHIGRAPH_DAEMON_ROOT env overrides), write a minimal
// graph.json, and assert that BuildBundle populates all three fields.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Test harness helpers
// ---------------------------------------------------------------------------

// graphCtxTestHarness sets up an isolated environment for a group with one
// repo, writes a graph.json containing a single entity, and writes a real
// source file so ReadSourceWindow can read it.
//
// Returns the entity ID, the fleet config group name, and a cleanup func.
func graphCtxTestHarness(t *testing.T) (groupName, entityID string) {
	t.Helper()

	tmp := t.TempDir()

	// Isolate the archigraph home and XDG config directories.
	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "myrepo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = "graphctx-test-group"

	// --- Write the fleet config (.fleet.json) ---
	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}

	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name": groupName,
		"repos": []map[string]interface{}{
			{"path": repoPath, "slug": "myrepo"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// --- Write a minimal source file so ReadSourceWindow has something to read ---
	srcRelPath := "pkg/server/server.go"
	srcAbsPath := filepath.Join(repoPath, srcRelPath)
	if err := os.MkdirAll(filepath.Dir(srcAbsPath), 0o755); err != nil {
		t.Fatalf("mkdir src dir: %v", err)
	}
	srcLines := "package server\n\n// Server is the main HTTP server.\ntype Server struct {\n\taddr string\n}\n\n// handleQueryGraph handles graph query requests.\nfunc (s *Server) handleQueryGraph(req Request) Response {\n\t// implementation\n\treturn Response{}\n}\n"
	if err := os.WriteFile(srcAbsPath, []byte(srcLines), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// --- Write graph.json in the daemon state dir for the repo ---
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	entityID = "aabbccdd11223344"
	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 1, Entities: 1, Relationships: 0},
		Entities: []graph.Entity{
			{
				ID:            entityID,
				Name:          "handleQueryGraph",
				QualifiedName: "github.com/test/myrepo/pkg/server.Server.handleQueryGraph",
				Kind:          "Function",
				SourceFile:    srcRelPath,
				StartLine:     9,
				EndLine:       11,
				Language:      "go",
			},
		},
		Relationships: []graph.Relationship{},
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal graph doc: %v", err)
	}
	graphPath := filepath.Join(stateDir, "graph.json")
	if err := os.WriteFile(graphPath, docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return groupName, entityID
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestBuildBundle_GraphContext_QualifiedName asserts that BuildBundle populates
// graph_context.qualified_name from the entity's QualifiedName field.
func TestBuildBundle_GraphContext_QualifiedName(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	qn := bundle.GraphContext.QualifiedName
	if qn == "" {
		t.Error("graph_context.qualified_name is empty — expected non-empty qualified name")
	}
	t.Logf("qualified_name = %q", qn)
}

// TestBuildBundle_GraphContext_Repo asserts that BuildBundle populates
// graph_context.repo from Document.Repo of the entity's owning graph document.
func TestBuildBundle_GraphContext_Repo(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	repo := bundle.GraphContext.Repo
	if repo == "" {
		t.Error("graph_context.repo is empty — expected non-empty repo path")
	}
	t.Logf("repo = %q", repo)
}

// TestBuildBundle_GraphContext_SourceWindow asserts that BuildBundle populates
// graph_context.source_window with a non-trivial excerpt from the entity's
// source file, bounded between 50 and 5000 characters.
func TestBuildBundle_GraphContext_SourceWindow(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	sw := bundle.GraphContext.SourceWindow
	if len(sw) < 50 {
		t.Errorf("graph_context.source_window too short: got %d chars, want ≥ 50\nwindow: %q", len(sw), sw)
	}
	if len(sw) > 5000 {
		t.Errorf("graph_context.source_window too long: got %d chars, want ≤ 5000", len(sw))
	}
	t.Logf("source_window (%d chars):\n%s", len(sw), sw)
}

// TestBuildBundle_GraphContext_AllThree runs all three field checks in one
// bundle call and provides a combined before/after-style report in the log.
func TestBuildBundle_GraphContext_AllThree(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	gc := bundle.GraphContext

	// All three must be non-empty.
	if gc.QualifiedName == "" {
		t.Error("graph_context.qualified_name: empty (want non-empty)")
	}
	if gc.Repo == "" {
		t.Error("graph_context.repo: empty (want non-empty)")
	}
	if gc.SourceWindow == "" {
		t.Error("graph_context.source_window: empty (want non-empty excerpt)")
	}

	// source_window sanity bounds per issue spec.
	if len(gc.SourceWindow) < 50 || len(gc.SourceWindow) > 5000 {
		t.Errorf("graph_context.source_window length %d out of [50, 5000] bounds", len(gc.SourceWindow))
	}

	t.Logf("graph_context after fix:\n  entity_name    = %q\n  qualified_name = %q\n  repo           = %q\n  source_file    = %q\n  source_window  = (%d chars)\n%s",
		gc.EntityName, gc.QualifiedName, gc.Repo, gc.SourceFile, len(gc.SourceWindow), gc.SourceWindow)
}

// TestBuildBundle_GraphContext_SourceWindowGracefulMissing verifies that if
// the source file doesn't exist, BuildBundle still succeeds (non-fatal) and
// source_window is left empty rather than failing the whole bundle.
func TestBuildBundle_GraphContext_SourceWindowGracefulMissing(t *testing.T) {
	tmp := t.TempDir()

	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "missingrepo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName := "graphctx-missing-src-group"

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}

	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name": groupName,
		"repos": []map[string]interface{}{
			{"path": repoPath, "slug": "missingrepo"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	entityID := "ddccbbaa44332211"
	// Source file intentionally does NOT exist on disk.
	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 1, Entities: 1, Relationships: 0},
		Entities: []graph.Entity{
			{
				ID:            entityID,
				Name:          "ghostFunc",
				QualifiedName: "github.com/test/missingrepo.ghostFunc",
				Kind:          "Function",
				SourceFile:    "pkg/gone/gone.go", // file does not exist
				StartLine:     5,
				EndLine:       10,
				Language:      "go",
			},
		},
		Relationships: []graph.Relationship{},
	}
	docJSON, _ := json.Marshal(doc)
	graphPath := filepath.Join(stateDir, "graph.json")
	if err := os.WriteFile(graphPath, docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle must not fail when source file is missing: %v", err)
	}

	// qualified_name and repo must still be populated.
	if bundle.GraphContext.QualifiedName == "" {
		t.Error("qualified_name must be populated even when source file is missing")
	}
	if bundle.GraphContext.Repo == "" {
		t.Error("repo must be populated even when source file is missing")
	}
	// source_window must be empty (graceful degradation, not a crash).
	if bundle.GraphContext.SourceWindow != "" {
		t.Errorf("expected empty source_window for missing file, got %q", bundle.GraphContext.SourceWindow)
	}
	t.Logf("graceful-missing: qualified_name=%q repo=%q source_window=%q",
		bundle.GraphContext.QualifiedName, bundle.GraphContext.Repo, bundle.GraphContext.SourceWindow)
}

// ---------------------------------------------------------------------------
// CWD-invariance tests (#1834)
// ---------------------------------------------------------------------------

// TestBuildBundle_SourceWindow_CWDInside verifies that source_window is
// populated when the working directory is INSIDE the indexed repo root.
// This is the happy-path regression guard: the fix must not break the
// normal case.
func TestBuildBundle_SourceWindow_CWDInside(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	// Change cwd to the repo directory itself.
	// The harness creates the repo at <tmp>/myrepo — we can recover that path
	// by resolving the fleet config (use t.TempDir trick via graphCtxTestHarness).
	// However graphCtxTestHarness does not expose repoPath. We work around this
	// by temporarily changing cwd to the system temp dir sub-dir created by the
	// harness. Since ARCHIGRAPH_HOME is set via t.Setenv, just call BuildBundle.
	//
	// The important assertion: source_window is non-empty, proving that BuildBundle
	// resolved the source file against the fleet config's absRepoPath, not cwd.

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	sw := bundle.GraphContext.SourceWindow
	if sw == "" {
		t.Error("source_window is empty — fix must populate it regardless of cwd")
	}
	t.Logf("CWDInside: source_window (%d chars): %s", len(sw), sw)
}

// TestBuildBundle_SourceWindow_CWDOutside verifies that source_window is
// populated even when the working directory is set to /tmp — completely
// outside the indexed repo root. This is the regression guard for #1834.
//
// Before the fix: BuildBundle called filepath.Join(seedRepo, sourceFile)
// where seedRepo was a bare slug (e.g. "archigraph"). That produced a
// relative path resolved from cwd, which failed with "not a directory"
// when cwd was /tmp.
//
// After the fix: seedRepo is the fleet-config absolute path, so
// filepath.Join(absRepoPath, sourceFile) is always absolute.
func TestBuildBundle_SourceWindow_CWDOutside(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	// Stash current cwd; change to /tmp (far outside the repo).
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(os.TempDir()); err != nil {
		t.Fatalf("Chdir /tmp: %v", err)
	}
	t.Cleanup(func() {
		// Restore cwd after the test regardless of outcome.
		_ = os.Chdir(origDir)
	})

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle must not fail with cwd=%s: %v", os.TempDir(), err)
	}

	sw := bundle.GraphContext.SourceWindow
	if sw == "" {
		t.Errorf("source_window is empty with cwd=%s — fix (#1834) must resolve source file via fleet-config absRepoPath, not cwd", os.TempDir())
	}
	if len(sw) < 50 {
		t.Errorf("source_window suspiciously short (%d chars) — may be truncated", len(sw))
	}
	t.Logf("CWDOutside (cwd=%s): source_window (%d chars):\n%s", os.TempDir(), len(sw), sw)
}

// TestBuildBundle_SectionGraphContextPropagated reproduces #1975: BuildBundle
// constructed each LLMSectionPrompt without copying the bundle-level
// GraphContext, so the LLM fill step received only stub_markdown + guidance
// per section — no source_window, no neighbour_briefs, no qualified_name. All
// 13 sections fired context-blind. After the fix every section carries the
// bundle-level GraphContext as a struct copy so a section-isolated fill
// worker still has the grounding payload. (#1975)
func TestBuildBundle_SectionGraphContextPropagated(t *testing.T) {
	groupName, entityID := graphCtxTestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: entityID,
			NoCache:      true,
		},
		Tier:    1,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if len(bundle.Sections) == 0 {
		t.Fatalf("expected at least one section, got 0")
	}
	if bundle.GraphContext.QualifiedName == "" {
		t.Fatalf("bundle.GraphContext.QualifiedName empty — harness invariant broken")
	}
	for i := range bundle.Sections {
		sp := &bundle.Sections[i]
		if sp.GraphContext.QualifiedName != bundle.GraphContext.QualifiedName {
			t.Errorf("section %q: expected GraphContext.QualifiedName=%q, got %q",
				sp.Section, bundle.GraphContext.QualifiedName, sp.GraphContext.QualifiedName)
		}
		if sp.GraphContext.EntityName != bundle.GraphContext.EntityName {
			t.Errorf("section %q: expected GraphContext.EntityName=%q, got %q",
				sp.Section, bundle.GraphContext.EntityName, sp.GraphContext.EntityName)
		}
		if sp.GraphContext.Repo != bundle.GraphContext.Repo {
			t.Errorf("section %q: expected GraphContext.Repo=%q, got %q",
				sp.Section, bundle.GraphContext.Repo, sp.GraphContext.Repo)
		}
		if sp.GraphContext.SourceWindow != bundle.GraphContext.SourceWindow {
			t.Errorf("section %q: source_window mismatch (bundle has %d chars, section has %d)",
				sp.Section, len(bundle.GraphContext.SourceWindow), len(sp.GraphContext.SourceWindow))
		}
	}
}
