package groupalgo

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// setupIncrGroup registers a 2-repo group ("acme") with the cross-repo fixture
// graphs on disk and returns the group name, the two repo paths, and the
// Service entity id. Caller mutates a repo's graph.fb + re-runs to exercise the
// incremental path.
func setupIncrGroup(t *testing.T) (group, pathA, pathB, serviceID string) {
	t.Helper()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoA, repoB, svc := fixtureGraphs()
	pathA = filepath.Join(root, "repoA")
	pathB = filepath.Join(root, "repoB")
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
	return "acme", pathA, pathB, svc
}

// resultsEqual compares the algo-result fields the overlay carries / applies for
// STRICT parity: the community assignment, PageRank, centrality, and node flags.
// (SurpriseEndpoints is not persisted in the overlay, so it is excluded — the
// skip path legitimately reconstitutes it empty; see overlayToResults.)
func resultsEqual(t *testing.T, want, got *graph.AlgorithmResults) {
	t.Helper()
	if !reflect.DeepEqual(want.CommunityID, got.CommunityID) {
		t.Errorf("CommunityID mismatch\n want=%v\n  got=%v", want.CommunityID, got.CommunityID)
	}
	if !reflect.DeepEqual(want.PageRank, got.PageRank) {
		t.Errorf("PageRank mismatch")
	}
	if !reflect.DeepEqual(want.Centrality, got.Centrality) {
		t.Errorf("Centrality mismatch")
	}
	if !reflect.DeepEqual(want.GodNodes, got.GodNodes) {
		t.Errorf("GodNodes mismatch")
	}
	if !reflect.DeepEqual(want.ArticulationPoints, got.ArticulationPoints) {
		t.Errorf("ArticulationPoints mismatch")
	}
	// Community summary + stats (what grafel_clusters / handleListCommunities read).
	if want.Stats.NumCommunities != got.Stats.NumCommunities {
		t.Errorf("NumCommunities want=%d got=%d", want.Stats.NumCommunities, got.Stats.NumCommunities)
	}
	if want.Stats.LouvainModularity != got.Stats.LouvainModularity {
		t.Errorf("LouvainModularity want=%v got=%v", want.Stats.LouvainModularity, got.Stats.LouvainModularity)
	}
}

// TestCommunityInputHash_StableAndContentSensitive locks the gate's contract:
// the hash is stable across runs and re-orderings of the same content, and
// changes iff a node or community-graph edge changes.
func TestCommunityInputHash_StableAndContentSensitive(t *testing.T) {
	repoA, repoB, _ := fixtureGraphs()
	ents := append(append([]graph.Entity{}, repoA.Entities...), repoB.Entities...)
	rels := append(append([]graph.Relationship{}, repoA.Relationships...), repoB.Relationships...)

	h1 := graph.CommunityInputHash(ents, rels)
	if h1 == "" {
		t.Fatal("empty hash")
	}
	// Re-order: hash is content-only, must be identical.
	rev := func(rs []graph.Relationship) []graph.Relationship {
		out := make([]graph.Relationship, len(rs))
		for i := range rs {
			out[len(rs)-1-i] = rs[i]
		}
		return out
	}
	if got := graph.CommunityInputHash(ents, rev(rels)); got != h1 {
		t.Errorf("hash not order-independent: %s != %s", got, h1)
	}
	// Same hash twice (determinism).
	if got := graph.CommunityInputHash(ents, rels); got != h1 {
		t.Errorf("hash not stable: %s != %s", got, h1)
	}
	// A non-community-graph change (a property unrelated to edge weight, an
	// entity Name/Signature) does NOT change the hash — the partition is unaffected.
	ents2 := append([]graph.Entity{}, ents...)
	ents2[0].Name = "Renamed"
	ents2[0].Signature = "func Renamed()"
	if ents2[0].Properties == nil {
		ents2[0].Properties = map[string]string{}
	}
	ents2[0].Properties["description"] = "docs change"
	if got := graph.CommunityInputHash(ents2, rels); got != h1 {
		t.Errorf("hash changed on a non-community-graph edit (should not): %s != %s", got, h1)
	}
	// Adding a node + an edge DOES change the hash.
	ents3 := append(append([]graph.Entity{}, ents...), graph.Entity{ID: "svc:NewNode", Name: "NewNode", Kind: "function", SourceFile: "svc/new.go"})
	rels3 := append(append([]graph.Relationship{}, rels...), graph.Relationship{FromID: "svc:NewNode", ToID: "svc:Service", Kind: "CALLS"})
	if got := graph.CommunityInputHash(ents3, rels3); got == h1 {
		t.Error("hash unchanged after adding a node + edge (should change)")
	}
}

// TestIncremental_SkipsWhenUnaffected proves the layer-4 win: after a first
// compute writes the overlay, a reindex that leaves the community input graph
// unchanged (only a non-structural field / mtime moved) is SKIPPED, and the
// reconstituted result is STRICTLY equal to a full recompute.
func TestIncremental_SkipsWhenUnaffected(t *testing.T) {
	group, pathA, _, _ := setupIncrGroup(t)

	// First compute = full pass, writes the overlay (with input_hash).
	first, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("first incremental run: %v", err)
	}
	if first.Skipped {
		t.Fatal("first run must NOT skip (no prior overlay)")
	}
	if err := WriteOverlayFromResult(first); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	// Re-write repo-A's graph.fb with the SAME community-graph content but a
	// changed non-structural field (a docs/description property) — this is the
	// docs-only-push shape: graph.fb mtime bumps, community input graph is
	// identical. Re-running must SKIP.
	repoA, _, _ := fixtureGraphs()
	for i := range repoA.Entities {
		if repoA.Entities[i].Properties == nil {
			repoA.Entities[i].Properties = map[string]string{}
		}
		repoA.Entities[i].Properties["description"] = "edited docs"
	}
	writeFixtureRepo(t, "svc", pathA, repoA)

	skip, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("second incremental run: %v", err)
	}
	if !skip.Skipped {
		t.Fatal("unaffected change must SKIP the recompute")
	}
	if skip.InputHash != first.InputHash {
		t.Errorf("input hash drifted across an unaffected change: %s != %s", skip.InputHash, first.InputHash)
	}
	// STRICT parity: skip result == a full recompute over the same end-state union.
	full, err := RunGroupAlgorithms(group)
	if err != nil {
		t.Fatalf("full reference: %v", err)
	}
	resultsEqual(t, full.Results, skip.Results)
}

// TestIncremental_RecomputesWhenStructureChanges proves the other branch: a
// structural change (new node + cross-cluster edge) is NOT skipped and the
// recomputed result equals a from-scratch full pass.
func TestIncremental_RecomputesWhenStructureChanges(t *testing.T) {
	group, pathA, _, serviceID := setupIncrGroup(t)

	first, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("first incremental run: %v", err)
	}
	if err := WriteOverlayFromResult(first); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	// Structural change in repo-A: add a new tightly-connected cluster.
	repoA, _, _ := fixtureGraphs()
	mkEnt := func(name string) graph.Entity {
		return graph.Entity{ID: "svc:" + name, Name: name, Kind: "function", SourceFile: "svc/" + name + ".go", Language: "go"}
	}
	mkRel := func(from, to string) graph.Relationship {
		return graph.Relationship{ID: from + "->" + to, FromID: from, ToID: to, Kind: "CALLS"}
	}
	cluster := []string{"c1", "c2", "c3", "c4", "c5", "c6"}
	for _, n := range cluster {
		repoA.Entities = append(repoA.Entities, mkEnt(n))
	}
	for i := 0; i < len(cluster); i++ {
		for j := i + 1; j < len(cluster); j++ {
			repoA.Relationships = append(repoA.Relationships, mkRel("svc:"+cluster[i], "svc:"+cluster[j]))
		}
	}
	repoA.Relationships = append(repoA.Relationships, mkRel("svc:c1", serviceID))
	writeFixtureRepo(t, "svc", pathA, repoA)

	got, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("structural incremental run: %v", err)
	}
	if got.Skipped {
		t.Fatal("structural change must NOT skip")
	}
	if got.InputHash == first.InputHash {
		t.Fatal("input hash must change on a structural change")
	}
	// STRICT parity: recompute == from-scratch full pass over the same end-state.
	full, err := RunGroupAlgorithms(group)
	if err != nil {
		t.Fatalf("full reference: %v", err)
	}
	resultsEqual(t, full.Results, got.Results)
	// Sanity: the new cluster actually formed a community.
	if _, ok := got.Results.CommunityID["svc:c1"]; !ok {
		t.Error("new node c1 missing from community result")
	}
}
