package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// smallDoc builds a tiny graph used by the perf/sidecar tests:
//
//	entities:      e1, e2, e3
//	relationships: e1 -> e2 (CALLS, in-repo), e1 -> ext-9 (CALLS, cross-repo)
//
// Derived truth: 1 cross-repo edge (ext-9 is not an entity here) and 2 orphan
// entities (e1 and e3 have no incoming edge; e2 does).
func smallDoc(t *testing.T, computedAt time.Time) *graph.Document {
	t.Helper()
	return &graph.Document{
		Version:     1,
		GeneratedAt: computedAt,
		Stats:       graph.Stats{Entities: 3, Relationships: 2, Files: 3},
		Entities: []graph.Entity{
			{ID: "e1", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "e2", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
			{ID: "e3", Name: "C", Kind: "function", SourceFile: "c.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{FromID: "e1", ToID: "e2", Kind: "CALLS"},
			{FromID: "e1", ToID: "ext-9", Kind: "CALLS"},
		},
	}
}

// writeRepoWithGraph creates a repo dir (with .git), writes graph.fb, and writes
// a graph-stats.json sidecar with the given entity/relationship counts. It
// returns the repo path.
func writeRepoWithGraph(t *testing.T, root, slug string, doc *graph.Document, sideEntities, sideRels int) string {
	t.Helper()
	repoPath := filepath.Join(root, slug)
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	side := &graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         doc.GeneratedAt,
		TotalEntities:      sideEntities,
		TotalRelationships: sideRels,
	}
	if err := graph.WriteSidecar(filepath.Join(stateDir, "graph.fb"), side, true); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return repoPath
}

// TestComputeCrossRepoAndOrphans verifies the single O(E) adjacency pass that
// replaced the old O(relationships×entities) nested scan (#5689).
func TestComputeCrossRepoAndOrphans(t *testing.T) {
	doc := smallDoc(t, time.Now())
	cross, orphans := computeCrossRepoAndOrphans(doc)
	if cross != 1 {
		t.Errorf("cross-repo edges = %d, want 1", cross)
	}
	if orphans != 2 {
		t.Errorf("orphan entities = %d, want 2", orphans)
	}
}

// TestComputeRepoHealth_LoadsGraphAtMostOnce is the anti-regression guard for
// #5689: the doctor health path must load the (potentially 291k-entity) graph at
// most once per repo — never the old three-loads-per-repo pattern — and must not
// run the O(E×N) scan.
func TestComputeRepoHealth_LoadsGraphAtMostOnce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := writeRepoWithGraph(t, tmp, "repo", smallDoc(t, time.Now()), 3, 2)

	var loads int32
	orig := loadGraphFromDir
	loadGraphFromDir = func(dir string) (*graph.Document, error) {
		atomic.AddInt32(&loads, 1)
		return orig(dir)
	}
	t.Cleanup(func() { loadGraphFromDir = orig })

	rh := computeRepoHealth(registry.Repo{Slug: "repo", Path: repoPath}, false)

	if got := atomic.LoadInt32(&loads); got > 1 {
		t.Fatalf("graph loaded %d times, want at most 1 (#5689 regression)", got)
	}
	if rh.CrossRepoEdges != 1 {
		t.Errorf("CrossRepoEdges = %d, want 1", rh.CrossRepoEdges)
	}
	if rh.orphanEntities != 2 {
		t.Errorf("orphanEntities = %d, want 2", rh.orphanEntities)
	}
}

// TestComputeRepoHealth_LiveCountsWhenLoadSucceeds verifies that when the graph
// loads, entity/relationship counts come from the LIVE graph (doc.Stats) — the
// same snapshot as the orphan/cross-repo metrics — even when the sidecar is
// stale, and that deep does not change the counts. This is the fix for the
// stale-count / >100% OrphanRate inconsistency.
func TestComputeRepoHealth_LiveCountsWhenLoadSucceeds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	// Sidecar deliberately DISAGREES with the live graph (stale sidecar: 999/888)
	// so we can prove the live doc.Stats (3/2) wins when the load succeeds.
	repoPath := writeRepoWithGraph(t, tmp, "repo", smallDoc(t, time.Now()), 999, 888)
	repo := registry.Repo{Slug: "repo", Path: repoPath}

	def := computeRepoHealth(repo, false)
	if def.Entities != 3 || def.Relationships != 2 {
		t.Errorf("default entities/rels = %d/%d, want 3/2 (live graph, not stale sidecar)", def.Entities, def.Relationships)
	}

	deep := computeRepoHealth(repo, true)
	if deep.Entities != 3 || deep.Relationships != 2 {
		t.Errorf("deep entities/rels = %d/%d, want 3/2 (live graph)", deep.Entities, deep.Relationships)
	}

	// deep no longer changes counts, and the graph-derived metrics match.
	if def.Entities != deep.Entities || def.Relationships != deep.Relationships ||
		def.CrossRepoEdges != deep.CrossRepoEdges || def.orphanEntities != deep.orphanEntities {
		t.Errorf("default and deep differ when load succeeds: def=%+v deep=%+v", def, deep)
	}
}

// TestComputeRepoHealth_SidecarFallbackOnLoadFailure verifies the degraded path:
// when the graph fails to load, counts fall back to the graph-stats.json sidecar
// and the graph-derived metrics (cross-repo/orphans) are zero.
func TestComputeRepoHealth_SidecarFallbackOnLoadFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := writeRepoWithGraph(t, tmp, "repo", smallDoc(t, time.Now()), 999, 888)
	repo := registry.Repo{Slug: "repo", Path: repoPath}

	// Force the graph load to fail (simulates a missing/corrupt graph.fb).
	orig := loadGraphFromDir
	loadGraphFromDir = func(string) (*graph.Document, error) {
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { loadGraphFromDir = orig })

	rh := computeRepoHealth(repo, false)
	if rh.Entities != 999 || rh.Relationships != 888 {
		t.Errorf("entities/rels = %d/%d, want 999/888 (sidecar fallback on load failure)", rh.Entities, rh.Relationships)
	}
	if rh.CrossRepoEdges != 0 || rh.orphanEntities != 0 {
		t.Errorf("derived metrics = (%d,%d), want (0,0) when the graph can't be loaded", rh.CrossRepoEdges, rh.orphanEntities)
	}
}

// TestComputeRepoHealth_MatchingSidecarEquivalent asserts the HARD CONSTRAINT:
// for a normal repo whose sidecar matches the graph, default and --deep yield
// identical values (output equivalence).
func TestComputeRepoHealth_MatchingSidecarEquivalent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)
	repoPath := writeRepoWithGraph(t, tmp, "repo", smallDoc(t, time.Now()), 3, 2)
	repo := registry.Repo{Slug: "repo", Path: repoPath}

	def := computeRepoHealth(repo, false)
	deep := computeRepoHealth(repo, true)

	if def.Entities != deep.Entities || def.Relationships != deep.Relationships ||
		def.CrossRepoEdges != deep.CrossRepoEdges || def.orphanEntities != deep.orphanEntities {
		t.Errorf("default and deep differ for matching sidecar: def=%+v deep=%+v", def, deep)
	}
	if def.Entities != 3 || def.Relationships != 2 {
		t.Errorf("entities/rels = %d/%d, want 3/2", def.Entities, def.Relationships)
	}
}

// TestComputeDoctorHealth_EndToEnd wires a real group config + repo through
// ComputeDoctorHealth and confirms the enriched report aggregates the
// sidecar-sourced counts and the single-pass orphan/cross-repo metrics, and that
// PrintDoctorHealth still renders.
func TestComputeDoctorHealth_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := writeRepoWithGraph(t, tmp, "svc", smallDoc(t, time.Now()), 3, 2)

	// Minimal group config referencing the repo.
	cfgPath := filepath.Join(tmp, "group.json")
	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Slug: "svc", Path: repoPath, Stack: registry.StackList{"go"}}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}

	reports := ComputeDoctorHealth([]registry.GroupRef{{Name: "g1", ConfigPath: cfgPath}}, false)
	if len(reports) != 1 {
		t.Fatalf("got %d group reports, want 1", len(reports))
	}
	g := reports[0]
	if g.TotalEntities != 3 {
		t.Errorf("TotalEntities = %d, want 3 (sidecar)", g.TotalEntities)
	}
	if g.TotalRelationships != 2 {
		t.Errorf("TotalRelationships = %d, want 2 (sidecar)", g.TotalRelationships)
	}
	if g.TotalCrossRepoEdges != 1 {
		t.Errorf("TotalCrossRepoEdges = %d, want 1", g.TotalCrossRepoEdges)
	}
	if g.OrphanEntities != 2 {
		t.Errorf("OrphanEntities = %d, want 2", g.OrphanEntities)
	}

	var buf bytes.Buffer
	PrintDoctorHealth(&buf, reports)
	if !bytes.Contains(buf.Bytes(), []byte("svc")) {
		t.Errorf("enriched report did not render repo slug:\n%s", buf.String())
	}
}
