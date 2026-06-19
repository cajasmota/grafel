// links_processflow_fb_test.go — regression lock for the flow-invisibility
// bug (Refs #1893, #1702).
//
// THE BUG (pre-fix): runPhantomEdgePass re-runs RunProcessFlowWithCompanions
// + RunEventFlow after promoting cross-repo phantom CALLS edges, but post
// ADR-0016 (flip-day #808) the daemon serves the canonical graph.fb while
// the pass historically wrote only graph.json. So recomputed Process/Event
// flow entities never reached the daemon or dashboard — live proof was
// graph.json holding 30+32 SCOPE.Process entities while `grafel status`
// reported "0 flows".
//
// THE LOCK: build a two-repo fixture group with a cross-repo HTTP call
// (client repo "fe" → server route in "be"), write each repo's fixture as
// graph.fb, run the phantom-edge pass, then DELETE the affected repo's
// graph.json and load graph.fb (LoadGraphFromDir can now only read fb).
// Assert SCOPE.Process flow entities are present (count > 0). If the write
// path ever regresses to graph.json-only, the fb load returns zero Process
// entities and this test fails.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
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

// TestPhantomEdgePass_WritesFlowsToFB locks the fb-write of recomputed
// cross-repo process flows (the primary fix). It exercises the real
// runPhantomEdgePass write path and asserts the flows land in graph.fb,
// not just graph.json.
func TestPhantomEdgePass_WritesFlowsToFB(t *testing.T) {
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

	// Run the production phantom-edge pass.
	added, err := runPhantomEdgePass("fixtgrp", cfg, linksPath)
	if err != nil {
		t.Fatalf("runPhantomEdgePass: %v", err)
	}
	if added == 0 {
		t.Fatalf("expected ≥1 phantom edge promoted, got 0")
	}

	// PRIMARY ASSERTION: the recomputed process flows must be in graph.fb.
	// Force a pure-fb read by deleting graph.json for the affected repo, then
	// load via LoadGraphFromDir (which now can only read graph.fb).
	feState := daemon.StateDirForRepo(fePath)
	fbPath := filepath.Join(feState, "graph.fb")
	jsonPath := filepath.Join(feState, "graph.json")
	if _, statErr := os.Stat(fbPath); statErr != nil {
		t.Fatalf("graph.fb missing for fe after pass: %v", statErr)
	}
	if err := os.Remove(jsonPath); err != nil {
		t.Fatalf("remove graph.json to force fb read: %v", err)
	}

	loaded, err := graph.LoadGraphFromDir(feState)
	if err != nil {
		t.Fatalf("load graph.fb after pass: %v", err)
	}
	got := countProcessEntities(loaded)
	if got == 0 {
		t.Fatalf("REGRESSION: graph.fb has 0 SCOPE.Process flow entities after phantom-edge pass — "+
			"flows were written to graph.json only and never reach the daemon (ADR-0016). "+
			"entities=%d relationships=%d", len(loaded.Entities), len(loaded.Relationships))
	}
	t.Logf("graph.fb has %d SCOPE.Process flow entities after phantom-edge pass (added=%d)", got, added)
}
