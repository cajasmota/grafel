package cli

// status_dupslug_5822_test.go — #5822, the relationship over-count.
//
// The contributor measured `grafel status` reporting 3,487,888 relationships
// against the indexer's own committed 1,963,736 on the same graph — very close
// to exactly 2x. It is NOT a write-path defect: the graph stores no duplicates.
//
// ComputeStatusSummaryForRef walks the group's REGISTRY ENTRIES and accumulates
// every group-level counter once per entry, while the per-repo rows it renders
// are keyed by SLUG (s.RepoStats[r.Slug] = rs) and printed by slug. Two
// registry entries that resolve to the same slug are therefore SUMMED TWICE and
// PRINTED ONCE — which is exactly the reported shape: "TOTAL exceeds the sum of
// the visible rows", with a ~2x factor when one repo is double-registered.
//
// Nothing in the tree could observe this before these tests: no test anywhere
// built two registry entries sharing a slug.
//
// The invariant pinned here is deliberately expressed as an IDENTITY rather
// than as a magic number — every group-level counter must equal the sum over
// the rows that are actually rendered. That is the property the user can check
// on screen, and it is the property the defect broke.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// dup5822Repo materialises one repo with a fully-populated state directory:
// a graph-stats.json sidecar (entity/relationship/unsupported-extension
// counts), a graph.fb carrying http-endpoint and process entities, and an
// enrichment-candidates.json carrying both enrichment and repair candidates.
//
// Every group-level accumulator in ComputeStatusSummaryForRef is fed by exactly
// one of these files, so a single fixture exercises all of them at once.
func dup5822Repo(t *testing.T, root, dirName string, ents, rels int) string {
	t.Helper()
	repoPath := filepath.Join(root, dirName)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	side := graph.GraphStatsSidecar{
		Version:               1,
		ComputedAt:            time.Now().Add(-10 * time.Minute),
		TotalFiles:            5,
		TotalEntities:         ents,
		TotalRelationships:    rels,
		UnsupportedExtensions: map[string]int{".bin": 4},
	}
	data, err := json.Marshal(side)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), data, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	doc := &graph.Document{
		Repo:        dirName,
		GeneratedAt: time.Now().Add(-10 * time.Minute),
		IndexedRef:  "main",
		IndexedSHA:  "abcdef012345",
		Entities: []graph.Entity{
			{ID: "h1", Name: "GET /a", Kind: "http_endpoint_definition", SourceFile: "a.go", Language: "go"},
			{ID: "h2", Name: "GET /b", Kind: "http_endpoint_definition", SourceFile: "b.go", Language: "go"},
			{ID: "p1", Name: "flow", Kind: "process", SourceFile: "c.go", Language: "go"},
		},
		Relationships: []graph.Relationship{},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	cands := []map[string]string{
		{"kind": "repair_edge", "subject_id": "r1"},
		{"kind": "repair_edge", "subject_id": "r2"},
		{"kind": "describe", "subject_id": "e1"},
		{"kind": "describe", "subject_id": "e2"},
		{"kind": "describe", "subject_id": "e3"},
	}
	cdata, err := json.Marshal(cands)
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "enrichment-candidates.json"), cdata, 0o644); err != nil {
		t.Fatalf("write candidates: %v", err)
	}
	return repoPath
}

// assertTotalsMatchRows is the whole invariant, applied to every group-level
// counter that ComputeStatusSummaryForRef accumulates per registry entry.
//
// It sums the RENDERED rows (summary.RepoStats, which is keyed by slug) and
// requires each TOTAL to equal that sum. Rows are the ground truth here: they
// are what the user sees, and they are already deduped by construction because
// the map is keyed by slug.
//
// HTTPEndpoints / ProcessFlows / EnrichmentCandidates / RepairCandidates have
// no per-row field to sum against, so they are checked against the expected
// per-repo constants times the number of DISTINCT slugs.
func assertTotalsMatchRows(t *testing.T, s *StatusSummary) {
	t.Helper()
	var wantEnts, wantRels int
	for _, rs := range s.RepoStats {
		wantEnts += rs.Entities
		wantRels += rs.Relationships
	}
	if s.TotalRelationships != wantRels {
		t.Errorf("TOTAL relationships = %d, but the printed rows sum to %d — "+
			"a registry entry was counted into TOTAL more than once while its row "+
			"was written once (#5822)", s.TotalRelationships, wantRels)
	}
	if s.TotalEntities != wantEnts {
		t.Errorf("TOTAL entities = %d, but the printed rows sum to %d (#5822)",
			s.TotalEntities, wantEnts)
	}
	distinct := len(s.RepoStats)
	if got, want := s.HTTPEndpoints, 2*distinct; got != want {
		t.Errorf("HTTPEndpoints = %d, want %d (2 per distinct slug) (#5822)", got, want)
	}
	if got, want := s.ProcessFlows, 1*distinct; got != want {
		t.Errorf("ProcessFlows = %d, want %d (1 per distinct slug) (#5822)", got, want)
	}
	if got, want := s.EnrichmentCandidates, 3*distinct; got != want {
		t.Errorf("EnrichmentCandidates = %d, want %d (3 per distinct slug) (#5822)", got, want)
	}
	if got, want := s.RepairCandidates, 2*distinct; got != want {
		t.Errorf("RepairCandidates = %d, want %d (2 per distinct slug) (#5822)", got, want)
	}
	if got, want := s.UnsupportedExt[".bin"], 4*distinct; got != want {
		t.Errorf("UnsupportedExt[.bin] = %d, want %d (4 per distinct slug) (#5822)", got, want)
	}
}

// TestStatusTotalsCountADoubleRegisteredRepoOnce is the reported defect,
// reproduced at its smallest: ONE repo, registered TWICE under the same slug —
// the shape a hand-edited or merged fleet config produces.
//
// Before the fix every TOTAL was exactly 2x the single row that got printed.
func TestStatusTotalsCountADoubleRegisteredRepoOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	repoPath := dup5822Repo(t, root, "mono", 448_000, 1_963_736)

	repos := []registry.Repo{
		{Slug: "mono", Path: repoPath},
		{Slug: "mono", Path: repoPath},
	}
	s := ComputeStatusSummaryForRef("grp", repos, "")

	// Anti-vacuity: the fixture must really produce ONE row for TWO entries,
	// otherwise "TOTAL equals the sum of the rows" holds for free.
	if len(s.RepoStats) != 1 {
		t.Fatalf("fixture broken: %d rows for 2 same-slug entries, want 1 — "+
			"this test can only observe the defect when the rows collapse", len(s.RepoStats))
	}
	if s.RepoStats["mono"].Relationships != 1_963_736 {
		t.Fatalf("fixture broken: row reports %d rels, want 1963736",
			s.RepoStats["mono"].Relationships)
	}

	assertTotalsMatchRows(t, s)
}

// TestStatusTotalsDedupeSameSlugAcrossDifferentPaths is the same defect with
// the two entries pointing at DIFFERENT directories.
//
// This is the case a path-keyed dedupe would miss: the paths differ, so
// "already seen this path" never fires, yet the second entry still overwrites
// the first entry's row and TOTAL still double-counts. The dedupe key has to be
// the SLUG, because the slug is what the row map is keyed by — anything else
// leaves the arithmetic broken for this shape.
func TestStatusTotalsDedupeSameSlugAcrossDifferentPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	a := dup5822Repo(t, root, "copy-a", 10, 100)
	b := dup5822Repo(t, root, "copy-b", 20, 200)

	repos := []registry.Repo{
		{Slug: "svc", Path: a},
		{Slug: "svc", Path: b},
	}
	s := ComputeStatusSummaryForRef("grp", repos, "")

	if len(s.RepoStats) != 1 {
		t.Fatalf("fixture broken: %d rows for 2 same-slug entries, want 1", len(s.RepoStats))
	}
	assertTotalsMatchRows(t, s)
}

// TestStatusTotalsStillSumDistinctSlugs is the PERMISSIVE control, and the more
// important half of this pair.
//
// A dedupe that keys on anything coarser than the slug — the group, the parent
// directory, "has a graph already" — would collapse these two genuinely
// different repos into one and UNDER-report the group, turning an over-count
// into a silent under-count. Every counter here must be the honest sum of two
// real repos.
func TestStatusTotalsStillSumDistinctSlugs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	a := dup5822Repo(t, root, "alpha", 11, 101)
	b := dup5822Repo(t, root, "beta", 22, 202)

	repos := []registry.Repo{
		{Slug: "alpha", Path: a},
		{Slug: "beta", Path: b},
	}
	s := ComputeStatusSummaryForRef("grp", repos, "")

	if len(s.RepoStats) != 2 {
		t.Fatalf("fixture broken: %d rows for 2 distinct slugs, want 2", len(s.RepoStats))
	}
	if s.TotalEntities != 33 || s.TotalRelationships != 303 {
		t.Fatalf("two distinct repos reported %d entities / %d rels, want 33/303 — "+
			"the dedupe collapsed repos that are not the same repo (#5822)",
			s.TotalEntities, s.TotalRelationships)
	}
	assertTotalsMatchRows(t, s)
}

// TestStatusTotalsDedupeIsSlugExactNotPrefix pins the other coarse-key
// direction: slugs that merely SHARE A PREFIX (or differ only in case) are
// different repos and must both count. A dedupe normalising the key — lower-
// casing, trimming, matching on a prefix — would silently drop one of these.
func TestStatusTotalsDedupeIsSlugExactNotPrefix(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	a := dup5822Repo(t, root, "api", 5, 50)
	b := dup5822Repo(t, root, "api-gateway", 7, 70)
	// NOTE the directory name differs from the slug on purpose: macOS ships a
	// case-INSENSITIVE filesystem, so a directory literally named "API" would
	// be the same directory as "api" and the two repos would share one state
	// dir — a fixture artefact that has nothing to do with the code under test.
	c := dup5822Repo(t, root, "api-upper-dir", 9, 90)

	repos := []registry.Repo{
		{Slug: "api", Path: a},
		{Slug: "api-gateway", Path: b},
		{Slug: "API", Path: c},
	}
	s := ComputeStatusSummaryForRef("grp", repos, "")

	if len(s.RepoStats) != 3 {
		t.Fatalf("fixture broken: %d rows, want 3 distinct slugs", len(s.RepoStats))
	}
	if s.TotalEntities != 21 || s.TotalRelationships != 210 {
		t.Fatalf("three distinct slugs reported %d entities / %d rels, want 21/210 — "+
			"the dedupe key is coarser than the slug (#5822)",
			s.TotalEntities, s.TotalRelationships)
	}
	assertTotalsMatchRows(t, s)
}
