package dashboard

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

func TestApplyGroupAlgorithmOverlay(t *testing.T) {
	grp := &DashGroup{Repos: map[string]*DashRepo{
		"api": {Doc: &graph.Document{Entities: []graph.Entity{
			{ID: "api:a", Name: "A"},
			{ID: "api:b", Name: "B"},
		}}},
		"web": {Doc: &graph.Document{Entities: []graph.Entity{
			{ID: "web:c", Name: "C"},
		}}},
	}}
	ov := &groupalgo.Overlay{
		Results: map[string]groupalgo.EntityOverlay{
			"api:a": {CommunityID: 7, PageRank: 0.8, Centrality: 0.4, IsGodNode: true},
			"api:b": {CommunityID: 7, PageRank: 0.2},
			"web:c": {CommunityID: 7, PageRank: 0.6, IsArticulationPoint: true},
		},
		Communities: []graph.CommunityResult{{ID: 7, Size: 3, AutoName: "shared-core"}},
		Stats:       graph.AlgorithmStats{NumCommunities: 1, NumGodNodes: 1},
	}

	applyGroupAlgorithmOverlay(grp, ov)

	a := grp.Repos["api"].Doc.Entities[0]
	if a.CommunityID == nil || *a.CommunityID != 7 || a.PageRank == nil || *a.PageRank != 0.8 || !a.IsGodNode {
		t.Fatalf("api:a overlay not restored: %+v", a)
	}
	c := grp.Repos["web"].Doc.Entities[0]
	if c.Centrality == nil || !c.IsArticulationPt {
		t.Fatalf("web:c overlay not restored: %+v", c)
	}
	if got := grp.Repos["api"].Doc.Communities; len(got) != 1 || got[0].Size != 2 || got[0].AutoName != "shared-core" {
		t.Fatalf("api community projection wrong: %+v", got)
	}
	if got := grp.Repos["web"].Doc.Communities; len(got) != 1 || got[0].Size != 1 || got[0].TopEntities[0] != "web:c" {
		t.Fatalf("web community projection wrong: %+v", got)
	}
	if got := grp.Repos["api"].Doc.AlgorithmStats; got == nil || got.NumCommunities != 1 {
		t.Fatalf("algorithm stats not restored: %+v", got)
	}
}

func TestRestorePersistedGroupAlgorithmOverlay(t *testing.T) {
	mtimes := map[string]int64{"repo": 42}
	ov := &groupalgo.Overlay{
		Group:        "cold-overlay",
		SourceMtimes: mtimes,
		Results: map[string]groupalgo.EntityOverlay{
			"service:a": {CommunityID: 4, PageRank: 0.91, Centrality: 0.72, IsGodNode: true},
		},
		Communities: []graph.CommunityResult{{ID: 4, Size: 1, AutoName: "service-core"}},
		Stats:       graph.AlgorithmStats{NumCommunities: 1},
	}
	path := filepath.Join(t.TempDir(), "overlay.json")
	if err := groupalgo.WriteOverlayTo(path, ov); err != nil {
		t.Fatal(err)
	}

	grp := &DashGroup{
		Repos: map[string]*DashRepo{
			"repo": {Doc: &graph.Document{Entities: []graph.Entity{{ID: "service:a", Name: "ServiceA"}}}},
		},
		pendingAlgo: []pendingAlgoRepo{{}},
	}
	if !restoreGroupAlgorithmOverlay(grp, path, mtimes) {
		t.Fatal("fresh persisted overlay was not restored")
	}
	entity := grp.Repos["repo"].Doc.Entities[0]
	if entity.PageRank == nil || *entity.PageRank != 0.91 || entity.CommunityID == nil || *entity.CommunityID != 4 {
		t.Fatalf("persisted overlay not restored: %+v", entity)
	}
	if got := grp.Repos["repo"].Doc.Communities; len(got) != 1 || got[0].AutoName != "service-core" {
		t.Fatalf("persisted communities not restored: %+v", got)
	}
	if len(grp.pendingAlgo) != 0 {
		t.Fatalf("authoritative group overlay left redundant pending sweeps: %+v", grp.pendingAlgo)
	}
	if restoreGroupAlgorithmOverlay(grp, path, map[string]int64{"repo": 43}) {
		t.Fatal("stale persisted overlay was accepted")
	}
}
