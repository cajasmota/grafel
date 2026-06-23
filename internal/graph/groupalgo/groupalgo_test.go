package groupalgo

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// fixtureGraphs builds two per-repo graphs that, when unioned, form ONE
// cross-repo subsystem: repo-A defines Service plus a cluster of internal
// callers; repo-B is a cluster of modules, each of which CALLS Service in
// repo-A (the cross-repo phantom CALLS edges that the link pass would have
// written into repo-B's graph.fb).
//
// IDs are slug-qualified so they are group-unique, exactly as the real
// extractor produces them (which is what lets phantom edges resolve across
// repos — decision Q4).
func fixtureGraphs() (repoA, repoB *graph.Document, serviceID string) {
	const aSlug = "svc"
	const bSlug = "web"

	mkEnt := func(slug, name string) graph.Entity {
		return graph.Entity{
			ID:         slug + ":" + name,
			Name:       name,
			Kind:       "function",
			SourceFile: slug + "/" + name + ".go",
			Language:   "go",
		}
	}
	mkRel := func(from, to string) graph.Relationship {
		return graph.Relationship{
			ID:     from + "->" + to,
			FromID: from,
			ToID:   to,
			Kind:   "CALLS",
		}
	}

	serviceID = aSlug + ":Service"

	// repo-A: Service + an internal callers cluster (a1..a6 all call Service,
	// and chain a bit among themselves so Louvain sees a community).
	repoA = &graph.Document{Version: 1, Repo: aSlug}
	repoA.Entities = append(repoA.Entities, mkEnt(aSlug, "Service"))
	for _, n := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		repoA.Entities = append(repoA.Entities, mkEnt(aSlug, n))
		repoA.Relationships = append(repoA.Relationships, mkRel(aSlug+":"+n, serviceID))
	}
	// internal chaining within A
	repoA.Relationships = append(repoA.Relationships,
		mkRel(aSlug+":a1", aSlug+":a2"),
		mkRel(aSlug+":a2", aSlug+":a3"),
		mkRel(aSlug+":a3", aSlug+":a4"),
	)

	// repo-B: a module cluster (b1..b6) chained among themselves, each calling
	// into repo-A's Service via a cross-repo phantom CALLS edge.
	repoB = &graph.Document{Version: 1, Repo: bSlug}
	for _, n := range []string{"b1", "b2", "b3", "b4", "b5", "b6"} {
		repoB.Entities = append(repoB.Entities, mkEnt(bSlug, n))
		// CROSS-REPO edge: repo-B's bN -> repo-A's Service.
		repoB.Relationships = append(repoB.Relationships, mkRel(bSlug+":"+n, serviceID))
	}
	repoB.Relationships = append(repoB.Relationships,
		mkRel(bSlug+":b1", bSlug+":b2"),
		mkRel(bSlug+":b2", bSlug+":b3"),
		mkRel(bSlug+":b3", bSlug+":b4"),
		mkRel(bSlug+":b4", bSlug+":b5"),
	)

	return repoA, repoB, serviceID
}

// TestUnion_CrossRepoCommunityAndPageRank is the A1 acceptance thesis, exercised
// in-memory: running RunAlgorithms over the UNION must (a) place entities from
// both repos in a single community, and (b) give Service a strictly higher
// PageRank than it gets from repo-A alone (proving the cross-repo inbound edges
// from repo-B are now seen).
func TestUnion_CrossRepoCommunityAndPageRank(t *testing.T) {
	repoA, repoB, serviceID := fixtureGraphs()

	// Per-repo (repo-A only) PageRank — the OLD per-repo scope. Compare Service
	// against a repo-A leaf peer (a1) that has the same kind of internal edges.
	// PageRank is L1-normalised (the whole vector sums to ~1), so the absolute
	// score of a node MECHANICALLY shrinks as the union grows — the meaningful,
	// scale-invariant signal is Service's IMPORTANCE relative to its peers. The
	// thesis of #5349 is precisely that cross-repo inbound edges raise Service's
	// relative importance; we measure it as the Service/peer PageRank ratio.
	perRepo := graph.RunAlgorithms(repoA.Entities, repoA.Relationships)
	perRepoRatio := perRepo.PageRank[serviceID] / perRepo.PageRank["svc:a1"]

	// Group union.
	var ents []graph.Entity
	var rels []graph.Relationship
	ents = append(ents, repoA.Entities...)
	ents = append(ents, repoB.Entities...)
	rels = append(rels, repoA.Relationships...)
	rels = append(rels, repoB.Relationships...)

	entityRepo := map[string]string{}
	for _, e := range repoA.Entities {
		entityRepo[e.ID] = "svc"
	}
	for _, e := range repoB.Entities {
		entityRepo[e.ID] = "web"
	}

	group := graph.RunAlgorithms(ents, rels)
	groupRatio := group.PageRank[serviceID] / group.PageRank["svc:a1"]

	// (b) Service's relative PageRank RISES at group scope: the six extra
	// cross-repo inbound CALLS from repo-B make Service a bigger hub relative to
	// a repo-A leaf than per-repo computation could ever see.
	if !(groupRatio > perRepoRatio) {
		t.Errorf("expected group Service/peer PageRank ratio > per-repo: group=%.4f per-repo=%.4f", groupRatio, perRepoRatio)
	}
	t.Logf("Service/peer PageRank ratio: per-repo=%.4f group=%.4f (rise=%.4f)", perRepoRatio, groupRatio, groupRatio-perRepoRatio)

	// (b') And in the group, Service must out-rank a repo-B module that has NO
	// cross-repo inbound (b1 only receives one internal edge), confirming the
	// cross-repo inbound edges are what lift it.
	if !(group.PageRank[serviceID] > group.PageRank["web:b1"]) {
		t.Errorf("expected group PageRank(Service) > PageRank(web:b1): %.6f vs %.6f", group.PageRank[serviceID], group.PageRank["web:b1"])
	}

	// (a) A single community spans BOTH repos. Find the community Service is in,
	// confirm it contains at least one entity from each repo.
	svcComm, ok := group.CommunityID[serviceID]
	if !ok {
		t.Fatalf("Service has no community id in group result")
	}
	sawSvcRepo, sawWebRepo := false, false
	for id, cid := range group.CommunityID {
		if cid != svcComm {
			continue
		}
		switch entityRepo[id] {
		case "svc":
			sawSvcRepo = true
		case "web":
			sawWebRepo = true
		}
	}
	if !(sawSvcRepo && sawWebRepo) {
		// Report community spread for debugging.
		spread := map[int]map[string]int{}
		for id, cid := range group.CommunityID {
			if spread[cid] == nil {
				spread[cid] = map[string]int{}
			}
			spread[cid][entityRepo[id]]++
		}
		t.Fatalf("Service's community %d does not span both repos (svc=%v web=%v). spread=%v",
			svcComm, sawSvcRepo, sawWebRepo, spread)
	}
	t.Logf("Service community %d spans both repos (cross-repo subsystem formed)", svcComm)
}

// TestRunAlgorithms_EmptyGroup_NoPanic confirms the zero-entity guard.
func TestRunAlgorithms_EmptyGroup_NoPanic(t *testing.T) {
	res := graph.RunAlgorithms(nil, nil)
	if res == nil {
		t.Fatal("expected non-nil empty result")
	}
	if len(res.CommunityID) != 0 || len(res.PageRank) != 0 {
		t.Errorf("expected empty maps, got community=%d pagerank=%d", len(res.CommunityID), len(res.PageRank))
	}
}

// writeFixtureRepo writes a tiny graph.fb into the daemon state dir for repoPath
// and returns a registry.Repo entry pointing at it.
func writeFixtureRepo(t *testing.T, slug, repoPath string, doc *graph.Document) registry.Repo {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	fbPath := filepath.Join(stateDir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write fixture graph.fb for %s: %v", slug, err)
	}
	return registry.Repo{Slug: slug, Path: repoPath}
}

// TestAssembleGroupGraph_OnDisk is the integration test: register a real
// 2-repo group with on-disk graph.fb files and assert AssembleGroupGraph +
// RunGroupAlgorithms concatenate them and form a cross-repo community.
func TestAssembleGroupGraph_OnDisk(t *testing.T) {
	// Isolate all grafel state into the test tempdir.
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoA, repoB, serviceID := fixtureGraphs()
	// Use non-git working-tree dirs; StateDirForRepo tolerates non-git (default ref).
	pathA := filepath.Join(root, "repoA")
	pathB := filepath.Join(root, "repoB")
	rA := writeFixtureRepo(t, "svc", pathA, repoA)
	rB := writeFixtureRepo(t, "web", pathB, repoB)

	// Register the group config + registry entry.
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

	// Assemble.
	ents, rels, entityRepo, srcMtimes, err := AssembleGroupGraph("acme")
	if err != nil {
		t.Fatalf("AssembleGroupGraph: %v", err)
	}
	wantEnts := len(repoA.Entities) + len(repoB.Entities)
	wantRels := len(repoA.Relationships) + len(repoB.Relationships)
	if len(ents) != wantEnts {
		t.Errorf("union entities=%d want=%d", len(ents), wantEnts)
	}
	if len(rels) != wantRels {
		t.Errorf("union rels=%d want=%d", len(rels), wantRels)
	}
	if entityRepo[serviceID] != "svc" {
		t.Errorf("Service attributed to repo %q want svc", entityRepo[serviceID])
	}
	if entityRepo["web:b1"] != "web" {
		t.Errorf("web:b1 attributed to repo %q want web", entityRepo["web:b1"])
	}
	if _, ok := srcMtimes["svc"]; !ok {
		t.Errorf("srcMtimes missing svc graph.fb mtime")
	}
	if _, ok := srcMtimes["web"]; !ok {
		t.Errorf("srcMtimes missing web graph.fb mtime")
	}

	// RunGroupAlgorithms end-to-end.
	res, err := RunGroupAlgorithms("acme")
	if err != nil {
		t.Fatalf("RunGroupAlgorithms: %v", err)
	}
	if res.NumRepos != 2 {
		t.Errorf("NumRepos=%d want 2", res.NumRepos)
	}
	if res.Results == nil || len(res.Results.CommunityID) == 0 {
		t.Fatalf("expected non-empty algorithm results")
	}
	// Cross-repo community formed.
	svcComm := res.Results.CommunityID[serviceID]
	sawSvc, sawWeb := false, false
	for id, cid := range res.Results.CommunityID {
		if cid != svcComm {
			continue
		}
		switch res.EntityRepo[id] {
		case "svc":
			sawSvc = true
		case "web":
			sawWeb = true
		}
	}
	if !(sawSvc && sawWeb) {
		t.Errorf("on-disk: Service community %d does not span both repos (svc=%v web=%v)", svcComm, sawSvc, sawWeb)
	}
}

// TestRunGroupAlgorithms_EmptyGroup confirms a registered group with no indexed
// repos yields an empty (non-nil) result and does not panic.
func TestRunGroupAlgorithms_EmptyGroup(t *testing.T) {
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	// A repo path that was never indexed (no graph.fb) — skipped during assembly.
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

	res, err := RunGroupAlgorithms("empty")
	if err != nil {
		t.Fatalf("RunGroupAlgorithms (empty): %v", err)
	}
	if res.NumEntities != 0 {
		t.Errorf("NumEntities=%d want 0", res.NumEntities)
	}
	if res.Results == nil {
		t.Fatal("expected non-nil Results even for empty group")
	}
	if len(res.Results.CommunityID) != 0 {
		t.Errorf("expected empty CommunityID, got %d", len(res.Results.CommunityID))
	}
}

// TestUnknownGroup confirms an unregistered group name is an error, not a panic.
func TestUnknownGroup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))
	if _, err := RunGroupAlgorithms("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown group")
	}
}
