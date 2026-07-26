// links_phase_5954_test.go — the phantom-edge pass's phase stamps (#5954).
//
// runPhantomEdgePass is the SECOND full materialisation of the group union in
// the same process: it reloads every repo's graph.Document into `docs`,
// promotes phantom edges, and then re-runs process flow with every other
// repo's Document held live as a companion. Whether the engine's plateau is
// owned by that reload or by the eight-pass window upstream is exactly the
// question the trace has to answer, so the phantom pass gets its own stamps
// rather than inheriting the link pass's last one.
//
// Observed from a real pass, not asserted against literals.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/registry"
)

func TestPhantomEdgePass_StampsPhasesInOrder(t *testing.T) {
	links.ResetPhaseHistory()
	t.Cleanup(links.ResetPhaseHistory)

	daemonRoot := t.TempDir()
	t.Setenv(daemon.EnvRoot, daemonRoot)

	fePath := filepath.Join(t.TempDir(), "fe")
	bePath := filepath.Join(t.TempDir(), "be")
	for _, p := range []string{fePath, bePath} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir repo %s: %v", p, err)
		}
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
	be := &graph.Document{
		Repo: "be",
		Entities: []graph.Entity{
			{ID: "be_handler", Name: "OrdersController.getSummary", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrdersController.java"},
		},
	}
	writeFixtureFB(t, fePath, fe)
	writeFixtureFB(t, bePath, be)

	cfg := &registry.GroupConfig{
		Name: "fixtgrp",
		Repos: []registry.Repo{
			{Slug: "fe", Path: fePath},
			{Slug: "be", Path: bePath},
		},
	}
	linksDoc := links.Document{
		Version: 1,
		Links: []links.Link{{
			ID:       "lnk1",
			Source:   "fe::fe_loadData",
			Target:   "be::be_handler",
			Relation: links.RelationCalls,
			Method:   links.MethodHTTP,
		}},
	}
	b, err := json.Marshal(linksDoc)
	if err != nil {
		t.Fatalf("marshal links: %v", err)
	}
	linksPath := filepath.Join(t.TempDir(), "fixtgrp-links.json")
	if err := os.WriteFile(linksPath, b, 0o644); err != nil {
		t.Fatalf("write links file: %v", err)
	}

	if _, err := runPhantomEdgePass("fixtgrp", cfg, linksPath); err != nil {
		t.Fatalf("runPhantomEdgePass: %v", err)
	}

	want := []string{
		links.PhasePhantomLoad,
		links.PhasePhantomPromote,
		links.PhasePhantomFlow,
		links.PhasePhantomCleanup,
	}
	got := links.PhaseHistory()
	if len(got) != len(want) {
		t.Fatalf("phase history = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("phase history[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestPhantomEdgePass_LoadPhaseCoversTheReload pins the attribution that
// matters: the docs-map reload happens under phantom_load, so the second
// materialisation of the union is billed to its own phase and not to whatever
// the link pass left stamped. If the stamp moved below the loop, the reload's
// heap would be attributed to the last link pass instead.
func TestPhantomEdgePass_LoadPhaseCoversTheReload(t *testing.T) {
	links.ResetPhaseHistory()
	t.Cleanup(links.ResetPhaseHistory)
	links.SetPhase("sentinel_from_a_previous_stage")

	daemonRoot := t.TempDir()
	t.Setenv(daemon.EnvRoot, daemonRoot)
	repo := filepath.Join(t.TempDir(), "solo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFB(t, repo, &graph.Document{
		Repo:     "solo",
		Entities: []graph.Entity{{ID: "s1", Name: "f", Kind: "SCOPE.Function", Language: "go", SourceFile: "a.go"}},
	})
	cfg := &registry.GroupConfig{Name: "g", Repos: []registry.Repo{{Slug: "solo", Path: repo}}}

	linksPath := filepath.Join(t.TempDir(), "g-links.json")
	if err := os.WriteFile(linksPath, []byte(`{"version":1,"links":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runPhantomEdgePass("g", cfg, linksPath); err != nil {
		t.Fatalf("runPhantomEdgePass: %v", err)
	}
	got := links.PhaseHistory()
	if len(got) < 2 || got[1] != links.PhasePhantomLoad {
		t.Fatalf("phase history = %v, want %q immediately after the sentinel", got, links.PhasePhantomLoad)
	}
}
