// Tests for the commit-coupling soft-edge pass (#21).
//
// Two layers of coverage:
//
//  1. Pure helpers (parseGitLog / buildSupport / filterPairs) drive the
//     algorithm without touching the filesystem — fast, deterministic.
//
//  2. End-to-end smoke against a temp git repo created via the real `git`
//     binary. Skips when git is unavailable or the test is run on a host
//     without a working PATH.
package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ──────────────────────────────────────────────────────────────────────────
// Unit tests — pure helpers
// ──────────────────────────────────────────────────────────────────────────

func TestParseGitLog_ParsesMultipleCommits(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"__archigraph_commit__:abc",
		"a.go",
		"b.go",
		"",
		"__archigraph_commit__:def",
		"a.go",
		"c.go",
		"",
	}, "\n"))

	stats := &CommitCouplingStats{}
	commits, err := parseGitLog(input, 100, stats)
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("want 2 commits, got %d", len(commits))
	}
	// files must be sorted within each commit
	if commits[0].files[0] != "a.go" || commits[0].files[1] != "b.go" {
		t.Errorf("commit 0 files unsorted: %v", commits[0].files)
	}
	if commits[1].files[0] != "a.go" || commits[1].files[1] != "c.go" {
		t.Errorf("commit 1 files: %v", commits[1].files)
	}
}

func TestParseGitLog_DedupsFilesWithinCommit(t *testing.T) {
	// A rename within a commit can produce the same path twice; we dedup
	// so pair enumeration doesn't double-count.
	input := strings.NewReader(strings.Join([]string{
		"__archigraph_commit__:abc",
		"a.go",
		"a.go",
		"b.go",
		"",
	}, "\n"))

	stats := &CommitCouplingStats{}
	commits, err := parseGitLog(input, 100, stats)
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(commits) != 1 || len(commits[0].files) != 2 {
		t.Fatalf("want 1 commit with 2 deduped files, got %+v", commits)
	}
}

func TestParseGitLog_SkipsOversizeCommits(t *testing.T) {
	// 3-file commit, then a 5-file commit, max=3 → second is dropped.
	input := strings.NewReader(strings.Join([]string{
		"__archigraph_commit__:abc",
		"a", "b", "c",
		"",
		"__archigraph_commit__:def",
		"a", "b", "c", "d", "e",
		"",
	}, "\n"))

	stats := &CommitCouplingStats{}
	commits, err := parseGitLog(input, 3, stats)
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(commits) != 1 {
		t.Errorf("want 1 surviving commit, got %d", len(commits))
	}
	if stats.SkippedOversizeCommits != 1 {
		t.Errorf("want 1 oversize-skipped, got %d", stats.SkippedOversizeCommits)
	}
}

func TestParseGitLog_SkipsEmptyCommit(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"__archigraph_commit__:abc",
		"",
		"__archigraph_commit__:def",
		"a.go",
		"",
	}, "\n"))
	stats := &CommitCouplingStats{}
	commits, err := parseGitLog(input, 100, stats)
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	// Empty commit contributes nothing; single-file commit alone produces
	// no pairs but is still recorded so total_commits is honest.
	if len(commits) != 1 {
		t.Fatalf("want 1 commit, got %d", len(commits))
	}
}

func TestBuildSupport_CountsUnorderedPairsOnce(t *testing.T) {
	commits := []commitRecord{
		{files: []string{"a", "b", "c"}}, // pairs: (a,b), (a,c), (b,c)
		{files: []string{"a", "b"}},      // pairs: (a,b)
		{files: []string{"a", "c"}},      // pairs: (a,c)
	}
	support := buildSupport(commits)
	if got, want := support[pairKey{"a", "b"}], 2; got != want {
		t.Errorf("support(a,b) = %d, want %d", got, want)
	}
	if got, want := support[pairKey{"a", "c"}], 2; got != want {
		t.Errorf("support(a,c) = %d, want %d", got, want)
	}
	if got, want := support[pairKey{"b", "c"}], 1; got != want {
		t.Errorf("support(b,c) = %d, want %d", got, want)
	}
	if len(support) != 3 {
		t.Errorf("want 3 distinct pairs, got %d", len(support))
	}
}

func TestFilterPairs_AppliesThreshold(t *testing.T) {
	support := map[pairKey]int{
		{"a", "b"}: 5,
		{"a", "c"}: 4,
		{"a", "d"}: 10,
	}
	kept := filterPairs(support, 5)
	if len(kept) != 2 {
		t.Fatalf("want 2 kept pairs, got %d", len(kept))
	}
	// Order is map-iteration dependent — assert by set membership.
	keys := map[string]int{}
	for _, p := range kept {
		keys[p.a+":"+p.b] = p.support
	}
	if keys["a:b"] != 5 || keys["a:d"] != 10 {
		t.Errorf("unexpected kept set: %v", keys)
	}
	if _, present := keys["a:c"]; present {
		t.Errorf("filtered pair leaked through")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Apply — synthetic doc, no git involvement
// ──────────────────────────────────────────────────────────────────────────

func TestApplyCommitCoupling_SkipsNonGitRepo(t *testing.T) {
	dir := t.TempDir()
	doc := &graph.Document{Repo: "test"}
	stats := ApplyCommitCoupling(doc, dir, DefaultCommitCouplingConfig())
	if !stats.Skipped {
		t.Fatalf("want Skipped=true, got %+v", stats)
	}
	if stats.SkipReason == "" {
		t.Errorf("want SkipReason populated")
	}
	if len(doc.Entities) != 0 || len(doc.Relationships) != 0 {
		t.Errorf("non-git repo must not mutate document")
	}
}

func TestApplyCommitCoupling_NilDoc(t *testing.T) {
	stats := ApplyCommitCoupling(nil, t.TempDir(), DefaultCommitCouplingConfig())
	if !stats.Skipped {
		t.Errorf("nil doc must be skipped")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// End-to-end smoke against a real git fixture
// ──────────────────────────────────────────────────────────────────────────

// gitAvailable reports whether `git` is on PATH. Tests that need git skip
// when this returns false.
func gitAvailable(t *testing.T) bool {
	t.Helper()
	_, err := exec.LookPath("git")
	return err == nil
}

// makeFixtureRepo creates a git repo at dir and runs the supplied commits.
// Each commit is described as (files-to-touch, message). Files are created
// with deterministic content (the commit index) so each commit produces a
// real diff.
func makeFixtureRepo(t *testing.T, dir string, commits [][]string) {
	t.Helper()
	mustRun := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		// Quiet env so the user's global gitconfig (commit-template, signing)
		// doesn't break the test.
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	mustRun("init", "-q", "-b", "main")
	mustRun("config", "commit.gpgsign", "false")

	for i, files := range commits {
		for _, f := range files {
			path := filepath.Join(dir, f)
			if err := writeFile(path, []byte{byte('a' + (i % 26)), '\n'}); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			mustRun("add", f)
		}
		mustRun("commit", "-q", "-m", "c"+string(rune('0'+i)))
	}
}

// writeFile creates path (and parent dirs) and writes content. The test
// caller varies content per commit so each commit produces a real diff.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func TestApplyCommitCoupling_EndToEnd(t *testing.T) {
	if !gitAvailable(t) {
		t.Skip("git not available")
	}
	// 3 files always changed together in 5 commits → 3 pairs (3 choose 2)
	// at support=5. One extra unrelated file appears in 2 commits only —
	// must not produce any edges at MinSupport=5.
	trio := []string{"a.go", "b.go", "c.go"}
	commits := [][]string{
		trio, trio, trio, trio, trio,
		{"unrelated1.go"},
		{"unrelated2.go"},
	}

	dir := t.TempDir()
	makeFixtureRepo(t, dir, commits)

	doc := &graph.Document{Repo: "fixture"}
	stats := ApplyCommitCoupling(doc, dir, DefaultCommitCouplingConfig())
	if stats.Skipped {
		t.Fatalf("e2e skipped: %s", stats.SkipReason)
	}
	if stats.TotalCommits != 7 {
		t.Errorf("TotalCommits = %d, want 7", stats.TotalCommits)
	}
	if stats.CoupledEdges != 3 {
		t.Errorf("CoupledEdges = %d, want 3 (3 choose 2 from the trio)", stats.CoupledEdges)
	}
	if stats.FileEntities != 3 {
		t.Errorf("FileEntities = %d, want 3", stats.FileEntities)
	}

	// Verify edge properties: support should be 5 for each trio pair, and
	// confidence = 5/7 (~0.7143).
	fileCount := 0
	edgeCount := 0
	for _, e := range doc.Entities {
		if e.Kind == KindFile {
			fileCount++
		}
	}
	for _, r := range doc.Relationships {
		if r.Kind != KindCommitCoupled {
			continue
		}
		edgeCount++
		if r.Properties["support"] != "5" {
			t.Errorf("edge %s support=%q, want 5", r.ID, r.Properties["support"])
		}
		conf := r.Properties["confidence"]
		if !strings.HasPrefix(conf, "0.71") {
			t.Errorf("edge %s confidence=%q, want ~0.7143", r.ID, conf)
		}
	}
	if fileCount != 3 {
		t.Errorf("File entities in doc = %d, want 3", fileCount)
	}
	if edgeCount != 3 {
		t.Errorf("COMMIT_COUPLED edges in doc = %d, want 3", edgeCount)
	}

	// Verify the edges are between the right files.
	want := map[string]bool{
		"a.go|b.go": false,
		"a.go|c.go": false,
		"b.go|c.go": false,
	}
	idToName := map[string]string{}
	for _, e := range doc.Entities {
		if e.Kind == KindFile {
			idToName[e.ID] = e.Name
		}
	}
	for _, r := range doc.Relationships {
		if r.Kind != KindCommitCoupled {
			continue
		}
		a, b := idToName[r.FromID], idToName[r.ToID]
		if a > b {
			a, b = b, a
		}
		want[a+"|"+b] = true
	}
	for k, hit := range want {
		if !hit {
			t.Errorf("expected COMMIT_COUPLED edge for %s not emitted", k)
		}
	}
}

func TestApplyCommitCoupling_Idempotent(t *testing.T) {
	if !gitAvailable(t) {
		t.Skip("git not available")
	}
	trio := []string{"x", "y", "z"}
	dir := t.TempDir()
	makeFixtureRepo(t, dir, [][]string{trio, trio, trio, trio, trio})

	doc := &graph.Document{Repo: "fixture"}
	first := ApplyCommitCoupling(doc, dir, DefaultCommitCouplingConfig())
	entitiesAfterFirst := len(doc.Entities)
	relsAfterFirst := len(doc.Relationships)

	second := ApplyCommitCoupling(doc, dir, DefaultCommitCouplingConfig())
	if len(doc.Entities) != entitiesAfterFirst {
		t.Errorf("entities changed on second apply: %d → %d", entitiesAfterFirst, len(doc.Entities))
	}
	if len(doc.Relationships) != relsAfterFirst {
		t.Errorf("relationships changed on second apply: %d → %d", relsAfterFirst, len(doc.Relationships))
	}
	if second.CoupledEdges != 0 || second.FileEntities != 0 {
		t.Errorf("second apply emitted new artifacts: %+v", second)
	}
	if first.CoupledEdges != 3 {
		t.Errorf("first apply edges = %d, want 3", first.CoupledEdges)
	}
}

func TestApplyCommitCoupling_RespectsMinSupport(t *testing.T) {
	if !gitAvailable(t) {
		t.Skip("git not available")
	}
	// Trio in 3 commits — below default MinSupport=5, above MinSupport=2.
	trio := []string{"p", "q", "r"}
	dir := t.TempDir()
	makeFixtureRepo(t, dir, [][]string{trio, trio, trio})

	doc := &graph.Document{Repo: "fixture"}
	stats := ApplyCommitCoupling(doc, dir, DefaultCommitCouplingConfig())
	if stats.CoupledEdges != 0 {
		t.Errorf("MinSupport=5 with 3 co-changes must emit 0 edges, got %d", stats.CoupledEdges)
	}

	doc2 := &graph.Document{Repo: "fixture"}
	cfg := DefaultCommitCouplingConfig()
	cfg.MinSupport = 2
	stats2 := ApplyCommitCoupling(doc2, dir, cfg)
	if stats2.CoupledEdges != 3 {
		t.Errorf("MinSupport=2 with 3 co-changes must emit 3 edges, got %d", stats2.CoupledEdges)
	}
}
