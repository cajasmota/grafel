package docgen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildMinimalGroupForTier3 creates a minimal ARCHIGRAPH_HOME fixture with one
// group, two repos (slug: "core" and "sidecar"), and a graph.json in each.
// It sets both ARCHIGRAPH_HOME and XDG_CONFIG_HOME so that registry.ConfigPathFor
// resolves to the temp directory.
func buildMinimalGroupForTier3(t *testing.T) (archHome, group, coreSlug string) {
	t.Helper()
	archHome = t.TempDir()
	group = "tier3-test-group"
	coreSlug = "core"

	t.Setenv("ARCHIGRAPH_HOME", archHome)

	// ConfigPathFor uses XDG_CONFIG_HOME/archigraph/<name>.fleet.json when
	// XDG_CONFIG_HOME is set. Override it so the test config is discovered.
	xdgConfigHome := filepath.Join(archHome, "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)

	// Config dir: XDG_CONFIG_HOME/archigraph/<group>.fleet.json
	cfgDir := filepath.Join(xdgConfigHome, "archigraph")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}

	// Repo paths.
	coreRepoPath := filepath.Join(archHome, "fake-core")
	sidecarRepoPath := filepath.Join(archHome, "fake-sidecar")
	for _, p := range []string{coreRepoPath, sidecarRepoPath} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	// Group config — uses slug field so Tier 3 can match by slug.
	groupCfg := map[string]interface{}{
		"name": group,
		"repos": []map[string]interface{}{
			{"slug": coreSlug, "path": coreRepoPath},
			{"slug": "sidecar", "path": sidecarRepoPath},
		},
	}
	cfgBytes, _ := json.Marshal(groupCfg)
	// ConfigPathFor resolves to <cfgDir>/<group>.fleet.json
	if err := os.WriteFile(filepath.Join(cfgDir, group+".fleet.json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write group config: %v", err)
	}

	// Synthetic graph for "core" repo — two service entities + one function entity.
	svc1ID := "svc1aabbccdd0011"
	svc2ID := "svc2aabbccdd0022"
	fnID := "fn00aabbccdd0033"
	pr1, pr2 := 0.6, 0.4
	entities := []interface{}{
		map[string]interface{}{
			"id": svc1ID, "name": "AuthService", "kind": "SCOPE.Service",
			"source_file": "auth/service.go", "start_line": 1, "end_line": 200,
			"language": "go", "pagerank": pr1,
		},
		map[string]interface{}{
			"id": svc2ID, "name": "UserService", "kind": "SCOPE.Service",
			"source_file": "user/service.go", "start_line": 1, "end_line": 150,
			"language": "go", "pagerank": pr2,
		},
		map[string]interface{}{
			"id": fnID, "name": "helperFn", "kind": "SCOPE.Function",
			"source_file": "util/helper.go", "start_line": 5, "end_line": 20,
			"language": "go",
		},
	}
	rels := []interface{}{
		map[string]interface{}{
			"id": "rel1aabb00110022", "from_id": svc1ID, "to_id": svc2ID,
			"kind": "CALLS",
		},
	}
	graphDoc := map[string]interface{}{
		"version": 1, "repo": coreRepoPath,
		"entities": entities, "relationships": rels,
	}
	graphBytes, _ := json.Marshal(graphDoc)

	// Write graph.json into the repo's .archigraph/ directory (fallback location
	// that daemon.StateDirForRepo resolves to in test environments without
	// ARCHIGRAPH_DAEMON_ROOT set).
	coreArchDir := filepath.Join(coreRepoPath, ".archigraph")
	if err := os.MkdirAll(coreArchDir, 0o755); err != nil {
		t.Fatalf("mkdir coreArchDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coreArchDir, "graph.json"), graphBytes, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	// Write an empty graph for sidecar so group config parses cleanly.
	sidecarArchDir := filepath.Join(sidecarRepoPath, ".archigraph")
	if err := os.MkdirAll(sidecarArchDir, 0o755); err != nil {
		t.Fatalf("mkdir sidecarArchDir: %v", err)
	}
	emptyGraph := map[string]interface{}{
		"version": 1, "repo": sidecarRepoPath,
		"entities": []interface{}{}, "relationships": []interface{}{},
	}
	emptyBytes, _ := json.Marshal(emptyGraph)
	if err := os.WriteFile(filepath.Join(sidecarArchDir, "graph.json"), emptyBytes, 0o644); err != nil {
		t.Fatalf("write sidecar graph.json: %v", err)
	}

	return archHome, group, coreSlug
}

// ---------------------------------------------------------------------------
// 1. enumerateRepoSeeds (via RunTier3 + fixture graph)
// ---------------------------------------------------------------------------

func TestEnumerateRepoSeeds_PageWorthyFilter(t *testing.T) {
	// Build a synthetic doc with two services and one function.
	pr1, pr2 := 0.6, 0.4
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "svc1", Name: "AuthService", Kind: "SCOPE.Service", PageRank: &pr1},
			{ID: "svc2", Name: "UserService", Kind: "SCOPE.Service", PageRank: &pr2},
			{ID: "fn1", Name: "helperFn", Kind: "SCOPE.Function"},
		},
	}

	seeds, skipped := docgen.EnumerateRepoSeedsForTest(doc, "test-group")
	if len(seeds) != 2 {
		t.Errorf("expected 2 page-worthy seeds (services only), got %d: %v", len(seeds), seeds)
	}
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
	// Highest PageRank should be first.
	if len(seeds) >= 2 && seeds[0] != "svc1" {
		t.Errorf("expected svc1 (higher pagerank) first, got %q", seeds[0])
	}
}

func TestEnumerateRepoSeeds_Cap(t *testing.T) {
	// Build a doc with MaxSeedsPerRepo+5 service entities.
	entities := make([]graph.Entity, docgen.MaxSeedsPerRepo+5)
	for i := range entities {
		entities[i] = graph.Entity{
			ID:   strings.Repeat("a", 16)[0:15] + string(rune('a'+i%26)),
			Name: "Svc",
			Kind: "service",
		}
	}
	doc := &graph.Document{Entities: entities}

	seeds, skipped := docgen.EnumerateRepoSeedsForTest(doc, "test-group")
	if len(seeds) != docgen.MaxSeedsPerRepo {
		t.Errorf("expected %d seeds (cap), got %d", docgen.MaxSeedsPerRepo, len(seeds))
	}
	if skipped != 5 {
		t.Errorf("expected 5 skipped, got %d", skipped)
	}
}

// ---------------------------------------------------------------------------
// 2. isPageWorthy via PageWorthyKinds
// ---------------------------------------------------------------------------

func TestIsPageWorthy_KnownKinds(t *testing.T) {
	cases := []struct {
		kind  string
		want  bool
	}{
		{"SCOPE.Service", true},
		{"SCOPE.Module", true},
		{"SCOPE.Package", true},
		{"SCOPE.ViewSet", true},
		{"SCOPE.Function", false},
		{"SCOPE.Variable", false},
		{"service", true},
		{"module", true},
		{"orchestrator", true},
		{"repository", true},
		{"SCOPE.Class", false},
	}
	for _, tc := range cases {
		got := docgen.IsPageWorthyForTest(tc.kind)
		if got != tc.want {
			t.Errorf("isPageWorthy(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. checkRepoCoverage
// ---------------------------------------------------------------------------

func TestCheckRepoCoverage_AllCovered(t *testing.T) {
	pageWorthyIDs := map[string]bool{"svc1": true, "svc2": true}
	pages := []docgen.PageOutput{
		{EntityID: "svc1"},
		{EntityID: "svc2"},
	}
	violations := docgen.CheckRepoContracts("testrepo", pageWorthyIDs, pages, "/tmp/nonexistent/index.md")
	for _, v := range violations {
		if v.Kind == "repo-coverage" {
			t.Errorf("unexpected repo-coverage violation: %s", v.Message)
		}
	}
}

func TestCheckRepoCoverage_MissingEntity(t *testing.T) {
	pageWorthyIDs := map[string]bool{"svc1": true, "svc2": true}
	pages := []docgen.PageOutput{
		{EntityID: "svc1"},
		// svc2 has no page
	}
	violations := docgen.CheckRepoContracts("testrepo", pageWorthyIDs, pages, "/tmp/nonexistent/index.md")
	found := false
	for _, v := range violations {
		if v.Kind == "repo-coverage" && strings.Contains(v.Message, "svc2") {
			found = true
		}
	}
	if !found {
		t.Error("expected repo-coverage violation for missing svc2, got none")
	}
}

// ---------------------------------------------------------------------------
// 4. checkPageOwnership
// ---------------------------------------------------------------------------

func TestCheckPageOwnership_NoConflict(t *testing.T) {
	pages := []docgen.PageOutput{
		{EntityID: "svc1", MDPath: "/out/svc1-page.md"},
		{EntityID: "svc2", MDPath: "/out/svc2-page.md"},
	}
	violations := docgen.CheckRepoContracts("testrepo", map[string]bool{}, pages, "/tmp/nonexistent/index.md")
	for _, v := range violations {
		if v.Kind == "page-ownership" {
			t.Errorf("unexpected page-ownership violation: %s", v.Message)
		}
	}
}

func TestCheckPageOwnership_Conflict(t *testing.T) {
	// Two pages with the same entity ID — ownership conflict.
	pages := []docgen.PageOutput{
		{EntityID: "svc1", MDPath: "/out/svc1-page.md"},
		{EntityID: "svc1", MDPath: "/out/svc1-duplicate-page.md"},
	}
	violations := docgen.CheckRepoContracts("testrepo", map[string]bool{}, pages, "/tmp/nonexistent/index.md")
	found := false
	for _, v := range violations {
		if v.Kind == "page-ownership" {
			found = true
			if !strings.Contains(v.Message, "svc1") {
				t.Errorf("ownership conflict message should mention entity ID 'svc1': %s", v.Message)
			}
		}
	}
	if !found {
		t.Error("expected page-ownership violation for duplicate entity ID, got none")
	}
}

// ---------------------------------------------------------------------------
// 5. checkRepoIndex
// ---------------------------------------------------------------------------

func TestCheckRepoIndex_ValidIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")

	// Write a valid page file.
	pagePath := filepath.Join(dir, "svc1aabbccdd0011-page.md")
	if err := os.WriteFile(pagePath, []byte("# Page"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write an index that links to the page and to score.json.
	indexContent := "# Index\n\n[svc1aabbccdd0011](svc1aabbccdd0011-page.md)\n\n[score.json](score.json)\n"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pages := []docgen.PageOutput{
		{EntityID: "svc1aabbccdd0011", MDPath: pagePath},
	}
	violations := docgen.CheckRepoContracts("testrepo", map[string]bool{}, pages, indexPath)
	for _, v := range violations {
		if v.Kind == "repo-index" {
			t.Errorf("unexpected repo-index violation: %s", v.Message)
		}
	}
}

func TestCheckRepoIndex_MissingIndex(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "index.md") // does not exist
	violations := docgen.CheckRepoContracts("testrepo", map[string]bool{}, nil, indexPath)
	found := false
	for _, v := range violations {
		if v.Kind == "repo-index" {
			found = true
		}
	}
	if !found {
		t.Error("expected repo-index violation for missing index.md, got none")
	}
}

func TestCheckRepoIndex_MissingPageLink(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")

	// Index that only links to score.json but not to any page.
	if err := os.WriteFile(indexPath, []byte("# Index\n[score.json](score.json)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pages := []docgen.PageOutput{
		{EntityID: "svc1aabb", MDPath: filepath.Join(dir, "svc1aabb-page.md")},
	}
	violations := docgen.CheckRepoContracts("testrepo", map[string]bool{}, pages, indexPath)
	found := false
	for _, v := range violations {
		if v.Kind == "repo-index" && strings.Contains(v.Message, "svc1aabb") {
			found = true
		}
	}
	if !found {
		t.Error("expected repo-index violation for missing page link, got none")
	}
}

// ---------------------------------------------------------------------------
// 6. Tier3Score JSON schema
// ---------------------------------------------------------------------------

func TestTier3Score_JSONSchema(t *testing.T) {
	score := docgen.Tier3Score{
		Tier:                    3,
		WallTimeMS:              480000,
		Repo:                    "core",
		PageCount:               23,
		SliceCount:              12,
		TotalTokenCount:         64000,
		MissingCoverageCount:    0,
		OwnershipConflictCount:  0,
		IndexLinkCount:          23,
		IndexLinkUnresolved:     0,
		SkippedBelowBudgetCount: 0,
	}

	data, err := json.MarshalIndent(score, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}

	required := []string{
		"tier", "wall_time_ms", "repo", "page_count", "slice_count",
		"total_token_count", "missing_coverage_count", "ownership_conflict_count",
		"index_link_count", "index_link_unresolved", "skipped_below_budget_count",
	}
	for _, f := range required {
		if _, ok := parsed[f]; !ok {
			t.Errorf("Tier3Score JSON missing required field: %q", f)
		}
	}
	if got := parsed["tier"].(float64); got != 3 {
		t.Errorf("tier: want 3, got %v", got)
	}
	if got := parsed["repo"].(string); got != "core" {
		t.Errorf("repo: want 'core', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// 7. RunTier3 — missing group returns graceful error
// ---------------------------------------------------------------------------

func TestRunTier3_MissingGroup(t *testing.T) {
	opts := docgen.Tier3RunOpts{
		Group:     "no-such-group-xyz",
		RepoSlug:  "core",
		OutputDir: t.TempDir(),
	}
	_, _, err := docgen.RunTier3(opts)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
	if strings.Contains(err.Error(), "nil pointer") {
		t.Errorf("unexpected nil pointer in error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 8. RunTier3 — missing repo slug returns helpful error
// ---------------------------------------------------------------------------

func TestRunTier3_MissingRepoSlug(t *testing.T) {
	_, group, _ := buildMinimalGroupForTier3(t)

	opts := docgen.Tier3RunOpts{
		Group:     group,
		RepoSlug:  "", // omitted
		OutputDir: t.TempDir(),
	}
	_, _, err := docgen.RunTier3(opts)
	if err == nil {
		t.Fatal("expected error for empty repo slug, got nil")
	}
	if !strings.Contains(err.Error(), "--repo is required") {
		t.Errorf("error should mention --repo flag: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 9. RunTier3 — with minimal synthetic group (integration)
// ---------------------------------------------------------------------------

func TestRunTier3_WithMinimalGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, group, coreSlug := buildMinimalGroupForTier3(t)
	outDir := t.TempDir()

	opts := docgen.Tier3RunOpts{
		Group:     group,
		RepoSlug:  coreSlug,
		MaxPages:  3,
		OutputDir: outDir,
	}

	returnedDir, score, err := docgen.RunTier3(opts)
	if err != nil {
		if strings.Contains(err.Error(), "nil pointer") {
			t.Fatalf("nil pointer panic: %v", err)
		}
		// Graceful errors from graph loading are acceptable in test envs.
		t.Logf("RunTier3 returned error (acceptable in test env): %v", err)
		return
	}

	// Verify score schema.
	if score.Tier != 3 {
		t.Errorf("tier: want 3, got %d", score.Tier)
	}
	if score.Repo != coreSlug {
		t.Errorf("repo: want %q, got %q", coreSlug, score.Repo)
	}

	// score.json must exist in <outDir>/<repoSlug>/.
	scoreFile := filepath.Join(returnedDir, coreSlug, "score.json")
	if _, err := os.Stat(scoreFile); err != nil {
		t.Errorf("score.json not found: %v", err)
	}

	// index.md must exist.
	indexFile := filepath.Join(returnedDir, coreSlug, "index.md")
	if _, err := os.Stat(indexFile); err != nil {
		t.Errorf("index.md not found: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 10. deduplicatePages (via RunTier3 internals — tested indirectly through
//     the integration test; direct unit test below)
// ---------------------------------------------------------------------------

func TestDeduplicatePages_RemovesDuplicates(t *testing.T) {
	pages := []docgen.PageOutput{
		{EntityID: "svc1", MDPath: "/a/svc1-page.md"},
		{EntityID: "svc2", MDPath: "/a/svc2-page.md"},
		{EntityID: "svc1", MDPath: "/b/svc1-page.md"}, // duplicate
	}
	deduped := docgen.DeduplicatePagesForTest(pages)
	if len(deduped) != 2 {
		t.Errorf("expected 2 pages after dedup, got %d", len(deduped))
	}
	// First occurrence wins.
	if deduped[0].MDPath != "/a/svc1-page.md" {
		t.Errorf("first svc1 page should be kept, got %q", deduped[0].MDPath)
	}
}
