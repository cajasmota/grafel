package groupalgo

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestDiffGroup_CrossRepoRankNonDecreasing is the A4 acceptance test: on the
// 2-repo fixture with a cross-repo CALLS edge into repo-A's Service, the diff
// report is produced and the cross-repo hub's PageRank RANK is non-decreasing
// group-vs-repo (the core thesis — cross-repo inbound edges lift a hub's rank,
// never lower it). The report must also marshal cleanly to JSON for CI.
func TestDiffGroup_CrossRepoRankNonDecreasing(t *testing.T) {
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoA, repoB, serviceID := fixtureGraphs()
	pathA := filepath.Join(root, "repoA")
	pathB := filepath.Join(root, "repoB")
	rA := writeFixtureRepo(t, "svc", pathA, repoA)
	rB := writeFixtureRepo(t, "web", pathB, repoB)

	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{rA, rB}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	rep, err := DiffGroup("acme", 10)
	if err != nil {
		t.Fatalf("DiffGroup: %v", err)
	}

	// Report is well-formed.
	if rep.Group != "acme" {
		t.Errorf("report group=%q want acme", rep.Group)
	}
	if rep.NumRepos != 2 {
		t.Errorf("NumRepos=%d want 2", rep.NumRepos)
	}
	if rep.NumEntities != len(repoA.Entities)+len(repoB.Entities) {
		t.Errorf("NumEntities=%d want %d", rep.NumEntities, len(repoA.Entities)+len(repoB.Entities))
	}
	if len(rep.TopRankChurn) == 0 {
		t.Errorf("expected a non-empty top rank churn table")
	}

	// The six cross-repo CALLS edges into Service make it a cross-repo entity.
	if rep.CrossRepoEntities == 0 {
		t.Fatalf("expected at least one cross-repo-called entity (Service)")
	}

	// CORE THESIS: no cross-repo entity lost rank.
	if !rep.CrossRepoRankNonDecreasing {
		t.Fatalf("cross-repo rank assertion FAILED: regressions=%+v", rep.CrossRepoRankRegressions)
	}

	// Service specifically must be ranked, and not regressed.
	for _, r := range rep.CrossRepoRankRegressions {
		if r.EntityID == serviceID {
			t.Fatalf("Service regressed: per-repo rank=%d group rank=%d", r.PerRepoRank, r.GroupRank)
		}
	}

	// Service's group rank must be <= its per-repo rank (rose or held).
	var svcRow *RankChurnRow
	for i := range rep.TopRankChurn {
		if rep.TopRankChurn[i].EntityID == serviceID {
			svcRow = &rep.TopRankChurn[i]
			break
		}
	}
	if svcRow == nil {
		t.Fatalf("Service not in top rank churn table; cannot verify rank movement")
	}
	if svcRow.PerRepoRank > 0 && svcRow.GroupRank > svcRow.PerRepoRank {
		t.Errorf("Service rank regressed: per-repo=%d group=%d", svcRow.PerRepoRank, svcRow.GroupRank)
	}
	t.Logf("Service rank: per-repo=%d group=%d (delta=%.0f), cross-repo entities=%d, community changed=%d, modularity delta=%.4f",
		svcRow.PerRepoRank, svcRow.GroupRank, svcRow.RankDelta, rep.CrossRepoEntities, rep.CommunityChanged, rep.ModularityDelta)

	// JSON round-trips (CI consumability).
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var back DiffReport
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if back.CrossRepoRankNonDecreasing != rep.CrossRepoRankNonDecreasing {
		t.Errorf("JSON round-trip changed assertion result")
	}
}

// TestDiffGroup_Empty confirms an empty group produces a clean report (assertion
// trivially holds, no panic).
func TestDiffGroup_Empty(t *testing.T) {
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoPath := filepath.Join(root, "ghost")
	cfgPath, err := registry.ConfigPathFor("empty")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "empty", Repos: []registry.Repo{{Slug: "ghost", Path: repoPath}}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("empty", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	rep, err := DiffGroup("empty", 0)
	if err != nil {
		t.Fatalf("DiffGroup(empty): %v", err)
	}
	if rep.NumEntities != 0 {
		t.Errorf("NumEntities=%d want 0", rep.NumEntities)
	}
	if !rep.CrossRepoRankNonDecreasing {
		t.Errorf("empty group should trivially satisfy the rank assertion")
	}
	if len(rep.CrossRepoRankRegressions) != 0 {
		t.Errorf("empty group should have no regressions")
	}
}
