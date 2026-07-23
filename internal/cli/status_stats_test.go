package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

func TestFormatGitRef(t *testing.T) {
	tests := []struct {
		ref        string
		sha        string
		isWorktree bool
		want       string
	}{
		{"main", "abc12345678", false, " @ main (abc12345678)"},
		{"feat/my-feature", "def56789012", false, " @ feat/my-feature (def56789012)"},
		{"", "abc12345678", false, " @ detached (abc12345678)"},
		{"main", "abc12345678", true, " @ main (abc12345678) [worktree]"},
		{"", "", false, ""},
		{"main", "", false, ""},
	}
	for _, tt := range tests {
		got := formatGitRef(tt.ref, tt.sha, tt.isWorktree)
		if got != tt.want {
			t.Errorf("formatGitRef(%q, %q, %v) = %q, want %q",
				tt.ref, tt.sha, tt.isWorktree, got, tt.want)
		}
	}
}

func TestComputeStatusSummary(t *testing.T) {
	// Create temporary directory structure for testing.
	tmpDir := t.TempDir()

	// When GRAFEL_DAEMON_ROOT is set, StateDirForRepo uses a hashed state directory.
	// So we need to create files in the right places.
	t.Setenv(daemon.EnvRoot, tmpDir)

	// Create a mock repo 1.
	repo1Path := filepath.Join(tmpDir, "repo1")
	os.MkdirAll(repo1Path, 0o755)

	// Get the actual state dir that daemon.StateDirForRepo will use.
	stateDir1 := daemon.StateDirForRepo(repo1Path)
	os.MkdirAll(stateDir1, 0o755)

	// Write graph-stats.json for repo1.
	side1 := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-5 * time.Minute),
		TotalEntities:      1135,
		TotalRelationships: 2400,
		Communities:        12,
		Modularity:         0.5,
		GodNodes:           3,
		ArticulationPoints: 8,
		RuntimeMS:          5000,
	}
	data1, _ := json.Marshal(side1)
	os.WriteFile(filepath.Join(stateDir1, "graph-stats.json"), data1, 0o644)

	// Create a mock repo 2.
	repo2Path := filepath.Join(tmpDir, "repo2")
	os.MkdirAll(repo2Path, 0o755)

	// Get the actual state dir that daemon.StateDirForRepo will use.
	stateDir2 := daemon.StateDirForRepo(repo2Path)
	os.MkdirAll(stateDir2, 0o755)

	// Write graph-stats.json for repo2.
	side2 := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-10 * time.Minute),
		TotalEntities:      3200,
		TotalRelationships: 6100,
		Communities:        18,
		Modularity:         0.6,
		GodNodes:           5,
		ArticulationPoints: 12,
		RuntimeMS:          8000,
	}
	data2, _ := json.Marshal(side2)
	os.WriteFile(filepath.Join(stateDir2, "graph-stats.json"), data2, 0o644)

	// Create repo list.
	repos := []registry.Repo{
		{
			Slug: "repo-1",
			Path: repo1Path,
		},
		{
			Slug: "repo-2",
			Path: repo2Path,
		},
	}

	summary := ComputeStatusSummary("test-group", repos)

	// Verify aggregation.
	if summary.GroupName != "test-group" {
		t.Errorf("expected group name 'test-group', got %q", summary.GroupName)
	}
	if summary.TotalEntities != 4335 {
		t.Errorf("expected 4335 total entities, got %d", summary.TotalEntities)
	}
	if summary.TotalRelationships != 8500 {
		t.Errorf("expected 8500 total relationships, got %d", summary.TotalRelationships)
	}

	// Verify per-repo stats.
	if rs, ok := summary.RepoStats["repo-1"]; !ok {
		t.Error("repo-1 not found in RepoStats")
	} else {
		if rs.Entities != 1135 {
			t.Errorf("repo-1: expected 1135 entities, got %d", rs.Entities)
		}
		if rs.Relationships != 2400 {
			t.Errorf("repo-1: expected 2400 relationships, got %d", rs.Relationships)
		}
		if rs.Path != repo1Path {
			t.Errorf("repo-1: expected path %q, got %q", repo1Path, rs.Path)
		}
	}

	if rs, ok := summary.RepoStats["repo-2"]; !ok {
		t.Error("repo-2 not found in RepoStats")
	} else {
		if rs.Entities != 3200 {
			t.Errorf("repo-2: expected 3200 entities, got %d", rs.Entities)
		}
		if rs.Relationships != 6100 {
			t.Errorf("repo-2: expected 6100 relationships, got %d", rs.Relationships)
		}
	}
}

func TestFormatTimeSince(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "seconds ago",
			duration: 30 * time.Second,
			expected: "30s ago",
		},
		{
			name:     "minutes ago",
			duration: 5 * time.Minute,
			expected: "5m ago",
		},
		{
			name:     "hours ago",
			duration: 3 * time.Hour,
			expected: "3h ago",
		},
		{
			name:     "hours and minutes ago",
			duration: 2*time.Hour + 30*time.Minute,
			expected: "2h30m ago",
		},
		{
			name:     "days ago",
			duration: 2 * 24 * time.Hour,
			expected: "2d ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTime := time.Now().Add(-tt.duration)
			result := formatTimeSince(testTime)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFormatTimeSinceZero(t *testing.T) {
	result := formatTimeSince(time.Time{})
	if result != "(never)" {
		t.Errorf("expected '(never)' for zero time, got %q", result)
	}
}

func TestRepoStatusWithoutGraphStats(t *testing.T) {
	// Test that RepoStatus can be created even when graph-stats.json doesn't exist.
	tmpDir := t.TempDir()

	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "test-repo")
	os.MkdirAll(repoPath, 0o755)

	repos := []registry.Repo{
		{
			Slug: "test-repo",
			Path: repoPath,
		},
	}

	summary := ComputeStatusSummary("test-group", repos)

	rs, ok := summary.RepoStats["test-repo"]
	if !ok {
		t.Fatal("test-repo not found in RepoStats")
	}

	// Should have zero values but still be present.
	if rs.Entities != 0 {
		t.Errorf("expected 0 entities, got %d", rs.Entities)
	}
	if rs.Relationships != 0 {
		t.Errorf("expected 0 relationships, got %d", rs.Relationships)
	}
	if rs.LastIndexedAge != "(never)" {
		t.Errorf("expected '(never)', got %q", rs.LastIndexedAge)
	}
}

// TestComputeStatusSummary_ColdIndexNoSidecar reproduces #5442: a repo that
// was indexed by the daemon's incremental path has a graph.fb on disk but no
// graph-stats.json sidecar. Status must report the persisted entity count and
// a real last-indexed time (read cheaply from the graph.fb header), not
// "0 entities / (never)".
func TestComputeStatusSummary_ColdIndexNoSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "coldrepo")
	os.MkdirAll(repoPath, 0o755)
	stateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(stateDir, 0o755)

	// Write a real graph.fb (3 entities) but deliberately NO graph-stats.json.
	indexedAt := time.Now().Add(-7 * time.Minute).UTC().Truncate(time.Second)
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: indexedAt,
		Repo:        repoPath,
		Stats:       graph.Stats{Entities: 3, Relationships: 1, Files: 2},
		Entities: []graph.Entity{
			{ID: "a1", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "b2", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
			{ID: "c3", Name: "C", Kind: "function", SourceFile: "c.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{FromID: "a1", ToID: "b2", Kind: "CALLS"},
		},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	// Sanity: sidecar absent.
	if _, err := os.Stat(filepath.Join(stateDir, "graph-stats.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no sidecar, stat err = %v", err)
	}

	repos := []registry.Repo{{Slug: "coldrepo", Path: repoPath}}
	summary := ComputeStatusSummary("grp", repos)

	rs, ok := summary.RepoStats["coldrepo"]
	if !ok {
		t.Fatal("coldrepo not found in RepoStats")
	}
	if rs.Entities != 3 {
		t.Errorf("Entities = %d, want 3 (persisted count from graph.fb header)", rs.Entities)
	}
	if rs.Relationships != 1 {
		t.Errorf("Relationships = %d, want 1", rs.Relationships)
	}
	if summary.TotalEntities != 3 {
		t.Errorf("TotalEntities = %d, want 3", summary.TotalEntities)
	}
	if rs.LastIndexed.IsZero() || rs.LastIndexedAge == "(never)" {
		t.Errorf("expected a real last-indexed time, got zero/never (age=%q)", rs.LastIndexedAge)
	}
	if !rs.LastIndexed.Equal(indexedAt) {
		t.Errorf("LastIndexed = %v, want %v (graph.fb header ComputedAt)", rs.LastIndexed, indexedAt)
	}
}

// TestComputeStatusSummary_ProcessFlows guards against regressing the
// process-flow counter predicate: it must count both the bare "process" kind
// and the real "SCOPE.Process" kind emitted by engine.RunProcessFlow (Pass 7).
func TestComputeStatusSummary_ProcessFlows(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "flowrepo")
	os.MkdirAll(repoPath, 0o755)
	stateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(stateDir, 0o755)

	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Repo:        repoPath,
		Stats:       graph.Stats{Entities: 3, Relationships: 0, Files: 1},
		Entities: []graph.Entity{
			{ID: "p1", Name: "P1", Kind: "SCOPE.Process", SourceFile: "a.go", Language: "go"},
			{ID: "p2", Name: "P2", Kind: "process", SourceFile: "a.go", Language: "go"},
			{ID: "f3", Name: "F3", Kind: "function", SourceFile: "a.go", Language: "go"},
		},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	repos := []registry.Repo{{Slug: "flowrepo", Path: repoPath}}
	summary := ComputeStatusSummary("grp", repos)

	if _, ok := summary.RepoStats["flowrepo"]; !ok {
		t.Fatal("flowrepo not found in RepoStats")
	}
	if summary.ProcessFlows != 2 {
		t.Errorf("ProcessFlows = %d, want 2 (one SCOPE.Process + one process)", summary.ProcessFlows)
	}
}

// TestComputeStatusSummary_TrulyNeverIndexed asserts the negative case still
// reports 0/never when there is no graph.fb at all.
func TestComputeStatusSummary_TrulyNeverIndexed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)
	repoPath := filepath.Join(tmpDir, "fresh")
	os.MkdirAll(repoPath, 0o755)

	summary := ComputeStatusSummary("grp", []registry.Repo{{Slug: "fresh", Path: repoPath}})
	rs := summary.RepoStats["fresh"]
	if rs == nil {
		t.Fatal("fresh repo missing")
	}
	if rs.Entities != 0 || rs.LastIndexedAge != "(never)" {
		t.Errorf("want 0/never, got entities=%d age=%q", rs.Entities, rs.LastIndexedAge)
	}
}

func TestFmtIntWithCommas(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{100, "100"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1000000, "1,000,000"},
		{1234567890, "1,234,567,890"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := fmtInt(tt.input)
			if result != tt.expected {
				t.Errorf("fmtInt(%d): expected %q, got %q", tt.input, tt.expected, result)
			}
		})
	}
}

func TestLoadCandidateCountsArray(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".grafel")
	os.MkdirAll(stateDir, 0o755)

	// Write enrichment-candidates.json as a bare array with distinct subject IDs.
	candidates := []map[string]interface{}{
		{"kind": "enrichment_edge", "subject_id": "e1"},
		{"kind": "enrichment_edge", "subject_id": "e2"},
		{"kind": "repair_edge", "subject_id": "r1"},
		{"kind": "enrichment_edge", "subject_id": "e3"},
		{"kind": "repair_edge", "subject_id": "r2"},
	}
	data, _ := json.Marshal(candidates)
	os.WriteFile(filepath.Join(stateDir, "enrichment-candidates.json"), data, 0o644)

	// loadCandidateCounts now returns (uniqueSubjects, totalActions, enrichByKind, repairCount).
	subjects, actions, _, repair := loadCandidateCounts(stateDir)
	if subjects != 3 {
		t.Errorf("expected 3 unique enrichment subjects, got %d", subjects)
	}
	if actions != 3 {
		t.Errorf("expected 3 total enrichment actions, got %d", actions)
	}
	if repair != 2 {
		t.Errorf("expected 2 repair candidates, got %d", repair)
	}
}

// ---------------------------------------------------------------------------
// PH1c cold-refs tests (#2087)
// ---------------------------------------------------------------------------

func TestFormatColdRefs_NoneReturnsEmpty(t *testing.T) {
	if got := formatColdRefs(nil); got != "" {
		t.Errorf("formatColdRefs(nil) = %q, want empty", got)
	}
	if got := formatColdRefs([]string{}); got != "" {
		t.Errorf("formatColdRefs([]) = %q, want empty", got)
	}
}

func TestFormatColdRefs_OneRef(t *testing.T) {
	got := formatColdRefs([]string{"feat/foo"})
	if got != " [+ feat/foo cold]" {
		t.Errorf("unexpected: %q", got)
	}
}

func TestFormatColdRefs_ThreeRefs(t *testing.T) {
	got := formatColdRefs([]string{"alpha", "beta", "gamma"})
	if got != " [+ alpha, beta, gamma cold]" {
		t.Errorf("unexpected: %q", got)
	}
}

func TestFormatColdRefs_TruncatesAfterThree(t *testing.T) {
	got := formatColdRefs([]string{"a", "b", "c", "d", "e"})
	want := " [+ a, b, c, +2 more cold]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComputeStatusSummary_ColdRefs verifies that ColdRefs is populated when
// a second (non-active) ref slot exists alongside the hot slot.
func TestComputeStatusSummary_ColdRefs(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "myrepo")
	os.MkdirAll(repoPath, 0o755)

	// The hot stateDir is what StateDirForRepo returns (uses _unknown since
	// tmpDir is not a git repo).
	hotStateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(hotStateDir, 0o755)

	// Write a graph-stats.json for the hot slot so it's counted.
	side := graph.GraphStatsSidecar{
		Version:       1,
		ComputedAt:    time.Now().Add(-2 * time.Minute),
		TotalEntities: 100,
	}
	sideData, _ := json.Marshal(side)
	os.WriteFile(filepath.Join(hotStateDir, "graph-stats.json"), sideData, 0o644)

	// Add a cold ref slot alongside the hot one: refs/develop/graph.fb.
	// The hot slot lives at hotStateDir = <base>/refs/<hotRefSafe>/
	refsDir := filepath.Dir(hotStateDir)
	coldRefDir := filepath.Join(refsDir, "develop")
	os.MkdirAll(coldRefDir, 0o755)
	os.WriteFile(filepath.Join(coldRefDir, "graph.fb"), []byte("FAKE"), 0o644)

	// Also add a slot without a graph.fb to ensure it's NOT included.
	emptySlotDir := filepath.Join(refsDir, "feat%2Fno-graph")
	os.MkdirAll(emptySlotDir, 0o755)

	repos := []registry.Repo{{Slug: "myrepo", Path: repoPath}}
	summary := ComputeStatusSummary("grp", repos)

	rs, ok := summary.RepoStats["myrepo"]
	if !ok {
		t.Fatal("myrepo not found in RepoStats")
	}
	if len(rs.ColdRefs) != 1 || rs.ColdRefs[0] != "develop" {
		t.Errorf("ColdRefs = %v, want [develop]", rs.ColdRefs)
	}
}

func TestLoadCandidateCountsMissing(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".grafel")
	os.MkdirAll(stateDir, 0o755)

	// No enrichment-candidates.json file.
	subjects, actions, _, repair := loadCandidateCounts(stateDir)
	if subjects != 0 {
		t.Errorf("expected 0 enrichment subjects when file missing, got %d", subjects)
	}
	if actions != 0 {
		t.Errorf("expected 0 enrichment actions when file missing, got %d", actions)
	}
	if repair != 0 {
		t.Errorf("expected 0 repair candidates when file missing, got %d", repair)
	}
}

// TestPrintStatusSummary_EnrichmentWording asserts the #5693 relabel: the
// optional-enrichment line must NOT read as a stuck/blocked daemon queue. The
// old "Pending: N enrichment candidates" wording is gone; the new wording marks
// the count as optional and explains nothing auto-drains it.
func TestPrintStatusSummary_EnrichmentWording(t *testing.T) {
	s := &StatusSummary{
		GroupName:            "g",
		RepoStats:            map[string]*RepoStatus{},
		EnrichmentCandidates: 42,
		RepairCandidates:     7,
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()

	if strings.Contains(out, "Pending:") {
		t.Errorf("output still uses stuck-queue wording %q:\n%s", "Pending:", out)
	}
	if !strings.Contains(out, "enrichment opportunities") {
		t.Errorf("output missing 'enrichment opportunities' relabel:\n%s", out)
	}
	if !strings.Contains(out, "optional") || !strings.Contains(out, "nothing auto-drains") {
		t.Errorf("output does not clarify the count is optional / not auto-drained:\n%s", out)
	}
	if !strings.Contains(out, "42") || !strings.Contains(out, "7") {
		t.Errorf("output missing the counts (42 enrichment, 7 repair):\n%s", out)
	}
}
