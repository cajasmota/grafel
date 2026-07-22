// links_processflow_fb_test.go — the #5904 PR-b side-table write-back contract
// (supersedes the #1893/#1702 fb-write lock).
//
// runPhantomEdgePass re-runs RunProcessFlowWithCompanions + RunEventFlow after
// promoting cross-repo phantom CALLS edges. Under #5904 PR-b the SINK is no
// longer a whole-graph rewrite (fbwriter.WriteGraphGen + graph.WriteAtomic) —
// which for a #5901 segment-set collapsed the whole graph in memory (the #5915
// P1 OOM). Instead the cross-repo-aware flow DELTA is written to the per-repo
// <stateDir>/flows.json side-table and REPLACE-merged at read time.
//
// THE CONTRACT (this file): a two-repo fixture group with a cross-repo HTTP call
// (client "fe" → route in "be"), run the phantom-edge pass, then assert:
//   - the flow side-table holds the re-synthesised SCOPE.Process flows + the
//     phantom cross_repo CALLS edge (the DELTA is durable);
//   - graph.fb / graph.json are NOT rewritten (byte-identical mtime — the
//     no-graph-rewrite guard mirroring PR-a's TestWriteback_success).
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/flows"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/registry"
)

// writeFixtureFB writes doc as graph.fb (and graph.json) into the state dir
// the phantom-edge pass and daemon resolve for repoPath. Mirrors how
// `grafel index` seeds per-repo state.
func writeFixtureFB(t *testing.T, repoPath string, doc *graph.Document) {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir %s: %v", stateDir, err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write fixture graph.fb: %v", err)
	}
	// Also seed graph.json so the pass's load path + dual-write behave like
	// production; the assertion below deletes it before loading to force a
	// pure-fb read.
	if err := graph.WriteAtomic(filepath.Join(stateDir, "graph.json"), doc, false); err != nil {
		t.Fatalf("write fixture graph.json: %v", err)
	}
}

// countProcessEntities returns how many SCOPE.Process flow entities a doc holds.
func countProcessEntities(doc *graph.Document) int {
	n := 0
	for i := range doc.Entities {
		if doc.Entities[i].Kind == engine.EntityKindProcess {
			n++
		}
	}
	return n
}

// TestPhantomEdgePass_WritesFlowsToSideTable asserts the #5904 PR-b contract:
// the re-synthesised cross-repo process flows are written to the flow SIDE-TABLE
// (flows.json), and graph.fb / graph.json are NOT rewritten.
func TestPhantomEdgePass_WritesFlowsToSideTable(t *testing.T) {
	// Isolate all daemon/state paths into a tempdir.
	daemonRoot := t.TempDir()
	t.Setenv(daemon.EnvRoot, daemonRoot)

	// Two fixture repo working trees (non-git is fine; gitmeta.Capture falls
	// back to a default ref and StateDirForRepo resolves consistently).
	fePath := filepath.Join(t.TempDir(), "fe")
	bePath := filepath.Join(t.TempDir(), "be")
	for _, p := range []string{fePath, bePath} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir repo %s: %v", p, err)
		}
	}

	// Frontend doc: a multi-hop chain whose tail makes a cross-repo HTTP
	// call. The phantom CALLS edge is injected by the pass (from links.json),
	// NOT pre-seeded here — so this fixture has no Process flow yet.
	fe := &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "fe_entry", Name: "loadDashboard", Kind: "SCOPE.Function", Language: "ts", SourceFile: "dashboard.ts"},
			{ID: "fe_loadData", Name: "fetchSummary", Kind: "SCOPE.Function", Language: "ts", SourceFile: "dashboard.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "fe_r1", FromID: "fe_entry", ToID: "fe_loadData", Kind: "CALLS"},
		},
	}
	// Backend doc: a handler with its own downstream CALLS chain.
	be := &graph.Document{
		Repo: "be",
		Entities: []graph.Entity{
			{ID: "be_handler", Name: "OrdersController.getSummary", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrdersController.java"},
			{ID: "be_service", Name: "OrderService.summarize", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderService.java"},
			{ID: "be_repo", Name: "OrderRepository.fetchAll", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderRepository.java"},
		},
		Relationships: []graph.Relationship{
			{ID: "be_r1", FromID: "be_handler", ToID: "be_service", Kind: "CALLS"},
			{ID: "be_r2", FromID: "be_service", ToID: "be_repo", Kind: "CALLS"},
		},
	}
	writeFixtureFB(t, fePath, fe)
	writeFixtureFB(t, bePath, be)

	// Group config keyed by slug (docs map + link Source/Target use slugs).
	cfg := &registry.GroupConfig{
		Name: "fixtgrp",
		Repos: []registry.Repo{
			{Slug: "fe", Path: fePath},
			{Slug: "be", Path: bePath},
		},
	}

	// Links file: one cross-repo HTTP CALLS link fe_loadData → be_handler.
	// method=http makes it a phantom-edge candidate; only "fe" is an
	// affectedRepo (source of the cross-repo CALLS), so only fe gets its
	// flow recomputed + written.
	linksDoc := links.Document{
		Version: 1,
		Links: []links.Link{
			{
				ID:       "lnk1",
				Source:   "fe::fe_loadData",
				Target:   "be::be_handler",
				Relation: links.RelationCalls,
				Method:   links.MethodHTTP,
			},
		},
	}
	linksPath := filepath.Join(t.TempDir(), "fixtgrp-links.json")
	b, err := json.Marshal(linksDoc)
	if err != nil {
		t.Fatalf("marshal links: %v", err)
	}
	if err := os.WriteFile(linksPath, b, 0o644); err != nil {
		t.Fatalf("write links file: %v", err)
	}

	// Capture graph.fb / graph.json mtimes BEFORE the pass so we can prove the
	// pass does NOT rewrite the graph (the #5915 P1 no-collapse guarantee).
	feState := daemon.StateDirForRepo(fePath)
	fbPath := filepath.Join(feState, "graph.fb")
	jsonPath := filepath.Join(feState, "graph.json")
	fbBefore := mustModTime(t, fbPath)
	jsonBefore := mustModTime(t, jsonPath)

	// Run the production phantom-edge pass.
	added, err := runPhantomEdgePass("fixtgrp", cfg, linksPath)
	if err != nil {
		t.Fatalf("runPhantomEdgePass: %v", err)
	}
	if added == 0 {
		t.Fatalf("expected ≥1 phantom edge promoted, got 0")
	}

	// NO-GRAPH-REWRITE GUARD: graph.fb + graph.json must be byte-for-byte
	// untouched (same mtime). If the write-back ever regresses to rewriting the
	// graph, these mtimes advance and the segment-set collapse hazard returns.
	if got := mustModTime(t, fbPath); !got.Equal(fbBefore) {
		t.Errorf("graph.fb was rewritten by the phantom pass (mtime %v -> %v) — must use the flow side-table", fbBefore, got)
	}
	if got := mustModTime(t, jsonPath); !got.Equal(jsonBefore) {
		t.Errorf("graph.json was rewritten by the phantom pass (mtime %v -> %v) — must use the flow side-table", jsonBefore, got)
	}

	// PRIMARY ASSERTION: the recomputed cross-repo process flows land in the flow
	// side-table (flows.json), fresh + non-stale.
	sc, ok := flows.Read(feState)
	if !ok {
		t.Fatalf("flow side-table not written / not fresh for affected repo fe")
	}
	got := 0
	for i := range sc.Entities {
		if sc.Entities[i].Kind == engine.EntityKindProcess {
			got++
		}
	}
	if got == 0 {
		t.Fatalf("flow side-table has 0 SCOPE.Process flow entities after phantom-edge pass "+
			"(entities=%d relationships=%d)", len(sc.Entities), len(sc.Relationships))
	}
	// The phantom cross_repo CALLS edge must be part of the delta.
	var phantom int
	for i := range sc.Relationships {
		if sc.Relationships[i].Kind == "CALLS" && sc.Relationships[i].PropGet("cross_repo") == "true" {
			phantom++
		}
	}
	if phantom == 0 {
		t.Errorf("flow side-table missing the phantom cross_repo CALLS edge: %+v", sc.Relationships)
	}
	t.Logf("flow side-table: %d SCOPE.Process flows, %d phantom CALLS (added=%d)", got, phantom, added)
}

func mustModTime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.ModTime()
}

// TestPhantomEdgePass_EmptyLinksClearsStaleSidecar is the #5904 PR-b total-link-
// removal guard. A repo "fe" carries a valid cross-repo flows.json (source_key
// still matches its un-reindexed graph). The last cross-repo link is then removed
// — the links file is now EMPTY. The pass MUST still clear fe's obsolete sidecar;
// otherwise flows.Read serves the stale cross-repo flows forever (until fe is
// next reindexed). Regression: an early return on empty links skips the cleanup
// and this test fails (the sidecar survives).
func TestPhantomEdgePass_EmptyLinksClearsStaleSidecar(t *testing.T) {
	daemonRoot := t.TempDir()
	t.Setenv(daemon.EnvRoot, daemonRoot)

	fePath := filepath.Join(t.TempDir(), "fe")
	if err := os.MkdirAll(fePath, 0o755); err != nil {
		t.Fatal(err)
	}
	fe := &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "fe_entry", Name: "loadDashboard", Kind: "SCOPE.Function", Language: "ts", SourceFile: "dashboard.ts"},
			{ID: "fe_loadData", Name: "fetchSummary", Kind: "SCOPE.Function", Language: "ts", SourceFile: "dashboard.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "fe_r1", FromID: "fe_entry", ToID: "fe_loadData", Kind: "CALLS"},
		},
	}
	writeFixtureFB(t, fePath, fe)
	feState := daemon.StateDirForRepo(fePath)

	// Pre-seed a valid cross-repo flow sidecar for fe (as a prior link run would).
	seedEnts := []graph.Entity{
		graph.Entity{ID: "fe_xrepo_proc", Name: "CrossRepoFlow", Kind: engine.EntityKindProcess}.
			WithProperties(map[string]string{"cross_stack": "true", "step_count": "2"}),
	}
	seedRels := []graph.Relationship{
		graph.Relationship{ID: "fe_xs0", FromID: "fe_xrepo_proc", ToID: "fe_loadData", Kind: engine.RelationshipKindStepInProcess}.WithProperties(map[string]string{"step_index": "0"}),
		graph.Relationship{ID: "fe_ph", FromID: "fe_loadData", ToID: "be::x", Kind: "CALLS"}.WithProperties(map[string]string{"cross_repo": "true"}),
	}
	if err := flows.Upsert(feState, seedEnts, seedRels); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}
	if _, ok := flows.Read(feState); !ok {
		t.Fatal("precondition: seeded sidecar must read fresh")
	}

	cfg := &registry.GroupConfig{Name: "fixtgrp", Repos: []registry.Repo{{Slug: "fe", Path: fePath}}}

	// EMPTY links file — the last cross-repo link was removed.
	linksDoc := links.Document{Version: 1, Links: []links.Link{}}
	linksPath := filepath.Join(t.TempDir(), "fixtgrp-links.json")
	b, _ := json.Marshal(linksDoc)
	if err := os.WriteFile(linksPath, b, 0o644); err != nil {
		t.Fatalf("write links file: %v", err)
	}

	if _, err := runPhantomEdgePass("fixtgrp", cfg, linksPath); err != nil {
		t.Fatalf("runPhantomEdgePass: %v", err)
	}

	// The obsolete sidecar must be gone → flows.Read falls back to baked intra.
	if sc, ok := flows.Read(feState); ok {
		t.Fatalf("stale flows.json NOT cleared after total link removal: %+v", sc)
	}
	if _, statErr := os.Stat(flows.Path(feState)); !os.IsNotExist(statErr) {
		t.Fatalf("flows.json still present after total link removal (stat err=%v)", statErr)
	}
}
