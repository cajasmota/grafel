// group_algo_overlay_apply_5354_test.go — A2 (#5354): MCP applies the
// <group>-algo.json overlay onto in-memory entities at group load, and is
// absence/staleness tolerant.
package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	"github.com/cajasmota/grafel/internal/registry"
)

// applyFixtureDoc builds a tiny per-repo graph with slug-qualified IDs and NO
// algo fields set (so any group values that show up came from the overlay).
func applyFixtureDoc(slug string, names ...string) *graph.Document {
	doc := &graph.Document{Version: 1, Repo: slug}
	for _, n := range names {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         slug + ":" + n,
			Name:       n,
			Kind:       "function",
			SourceFile: slug + "/" + n + ".go",
			Language:   "go",
		})
	}
	return doc
}

// setupApplyGroup writes two on-disk graph.fb repos, registers the group config
// (so CurrentSourceMtimes resolves), and returns a *State pointing at them plus
// the resolved overlay path and current source mtimes.
func setupApplyGroup(t *testing.T) (st *State, overlayPath string, curMtimes map[string]int64, serviceID, leafID string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	pathA := filepath.Join(root, "svc")
	pathB := filepath.Join(root, "web")

	docA := applyFixtureDoc("svc", "Service", "a1")
	docB := applyFixtureDoc("web", "b1")

	for _, rp := range []struct {
		path string
		doc  *graph.Document
	}{{pathA, docA}, {pathB, docB}} {
		stateDir := daemon.StateDirForRepo(rp.path)
		fbPath := filepath.Join(stateDir, "graph.fb")
		if err := fbwriter.WriteAtomic(fbPath, rp.doc); err != nil {
			t.Fatalf("write graph.fb: %v", err)
		}
	}

	// Register the group config so groupalgo.CurrentSourceMtimes can resolve it.
	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{
		{Slug: "svc", Path: pathA},
		{Slug: "web", Path: pathB},
	}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	// Build the in-memory mcp Registry pointing at the two repo paths. Repo
	// names match the group-config slugs.
	regPath := filepath.Join(home, "registry.json")
	inMem := &Registry{
		Path: regPath,
		Groups: map[string]RegistryGroup{
			"acme": {Repos: map[string]RegistryRepo{
				"svc": {Path: pathA},
				"web": {Path: pathB},
			}},
		},
	}
	st = NewState(inMem)

	overlayPath, err = groupalgo.OverlayPath("acme")
	if err != nil {
		t.Fatalf("overlay path: %v", err)
	}
	curMtimes, err = groupalgo.CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("current mtimes: %v", err)
	}
	return st, overlayPath, curMtimes, "svc:Service", "web:b1"
}

func entityByID(grp *LoadedGroup, id string) *graph.Entity {
	for _, lr := range grp.Repos {
		if lr == nil || lr.Doc == nil {
			continue
		}
		for i := range lr.Doc.Entities {
			if lr.Doc.Entities[i].ID == id {
				return &lr.Doc.Entities[i]
			}
		}
	}
	return nil
}

// TestApplyOverlay_GroupValuesAppliedAtLoad: with a fresh overlay present, a
// cross-repo entity reads the GROUP community_id + pagerank after Reload.
func TestApplyOverlay_GroupValuesAppliedAtLoad(t *testing.T) {
	st, overlayPath, cur, serviceID, leafID := setupApplyGroup(t)

	ov := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			serviceID: {CommunityID: 42, PageRank: 0.77, Centrality: 0.5, IsGodNode: true},
			leafID:    {CommunityID: 42, PageRank: 0.02},
		},
		Communities: []graph.CommunityResult{{ID: 42, Size: 2, AutoName: "cross-repo-core"}},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	grp := st.Group("acme")
	if grp == nil {
		t.Fatal("group not loaded")
	}

	svc := entityByID(grp, serviceID)
	if svc == nil {
		t.Fatalf("%s not found", serviceID)
	}
	if svc.CommunityID == nil || *svc.CommunityID != 42 {
		t.Errorf("Service community_id = %v; want 42 (group overlay not applied)", svc.CommunityID)
	}
	if svc.PageRank == nil || *svc.PageRank != 0.77 {
		t.Errorf("Service pagerank = %v; want 0.77", svc.PageRank)
	}
	if !svc.IsGodNode {
		t.Error("Service IsGodNode not applied from overlay")
	}
	// Cross-repo leaf in the SAME group community.
	leaf := entityByID(grp, leafID)
	if leaf == nil || leaf.CommunityID == nil || *leaf.CommunityID != 42 {
		t.Errorf("cross-repo leaf community_id = %v; want 42", leaf)
	}
	// Group community summary surfaced for grafel_clusters.
	if len(grp.Communities) != 1 || grp.Communities[0].AutoName != "cross-repo-core" {
		t.Errorf("group Communities summary not applied: %v", grp.Communities)
	}
}

// TestApplyOverlay_AbsentIsNoop: with no overlay, entities keep their graph.fb
// values (here: nil/unset). No panic, no group values.
func TestApplyOverlay_AbsentIsNoop(t *testing.T) {
	st, overlayPath, _, serviceID, _ := setupApplyGroup(t)
	// Ensure no overlay exists.
	_ = os.Remove(overlayPath)

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	grp := st.Group("acme")
	if grp == nil {
		t.Fatal("group not loaded")
	}
	svc := entityByID(grp, serviceID)
	if svc == nil {
		t.Fatalf("%s not found", serviceID)
	}
	if svc.CommunityID != nil {
		t.Errorf("absent overlay leaked a community_id: %v (must stay graph.fb value nil)", *svc.CommunityID)
	}
	if svc.PageRank != nil {
		t.Errorf("absent overlay leaked a pagerank: %v", *svc.PageRank)
	}
	if grp.Communities != nil {
		t.Errorf("absent overlay leaked a community summary: %v", grp.Communities)
	}
}

// TestApplyOverlay_StaleNotApplied: an overlay whose recorded source mtime no
// longer matches the current graph.fb mtime is treated as stale → not applied.
func TestApplyOverlay_StaleNotApplied(t *testing.T) {
	st, overlayPath, cur, serviceID, _ := setupApplyGroup(t)

	// Record DELIBERATELY-WRONG source mtimes so the overlay is stale vs the
	// real on-disk graph.fb mtimes.
	stale := map[string]int64{}
	for slug, mt := range cur {
		stale[slug] = mt + 1 // off by one nano → mismatch
	}
	ov := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: stale,
		Results: map[string]groupalgo.EntityOverlay{
			serviceID: {CommunityID: 99, PageRank: 0.9},
		},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	grp := st.Group("acme")
	svc := entityByID(grp, serviceID)
	if svc == nil {
		t.Fatalf("%s not found", serviceID)
	}
	if svc.CommunityID != nil {
		t.Errorf("stale overlay was applied (community_id=%v); must fall back to graph.fb", *svc.CommunityID)
	}
}

// TestApplyOverlay_MidSessionSwap: after applying overlay v1, swapping in a new
// overlay (different values, fresh mtimes) and reloading picks up v2 without a
// graph.fb change.
func TestApplyOverlay_MidSessionSwap(t *testing.T) {
	st, overlayPath, cur, serviceID, _ := setupApplyGroup(t)

	write := func(pr float64) {
		ov := &groupalgo.Overlay{
			Group:        "acme",
			SourceMtimes: cur,
			Results:      map[string]groupalgo.EntityOverlay{serviceID: {CommunityID: 1, PageRank: pr}},
		}
		if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
			t.Fatalf("write overlay: %v", err)
		}
	}

	write(0.10)
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	if svc := entityByID(st.Group("acme"), serviceID); svc == nil || svc.PageRank == nil || *svc.PageRank != 0.10 {
		t.Fatalf("v1 not applied: %v", svc)
	}

	// Ensure the overlay mtime advances (filesystem granularity safety).
	time.Sleep(10 * time.Millisecond)
	future := time.Now().Add(time.Second)
	_ = os.Chtimes(overlayPath, future, future)
	write(0.20)
	// Bump again to guarantee a later mtime than v1.
	future2 := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(overlayPath, future2, future2)

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload v2: %v", err)
	}
	if svc := entityByID(st.Group("acme"), serviceID); svc == nil || svc.PageRank == nil || *svc.PageRank != 0.20 {
		t.Fatalf("v2 swap not applied: %v", svc)
	}
}
