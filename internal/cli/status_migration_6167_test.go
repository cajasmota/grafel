package cli

// status_migration_6167_test.go — #6167 half 2: a graph-format migration must
// be VISIBLE.
//
// A user with 140 registered repos upgraded across an fbversion bump and their
// daemon spent hours reindexing. `grafel status` said nothing about it, so the
// only available reading was "the product is hung" — which led to restarts that
// re-entered the queue. The daemon already recomputes ReindexRequired into every
// repo's statusfile on each heartbeat (internal/daemon/statuswriter.go:135) and
// ComputeStatusSummary already reads that statusfile (status_stats.go:117), so
// surfacing this costs no extra I/O and no graph load (the #5995 contract).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// seedRepoWithStatusfile creates a repo with a valid graph.fb and a statusfile
// carrying the given reindex-required state, mirroring what the engine's status
// writer would have persisted.
func seedRepoWithStatusfile(t *testing.T, root, slug string, reindexRequired bool) registry.Repo {
	t.Helper()
	repoPath := filepath.Join(root, slug)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Repo:        repoPath,
		Stats:       graph.Stats{Entities: 1, Relationships: 0, Files: 1},
		Entities:    []graph.Entity{{ID: "e1", Name: "E", Kind: "function", SourceFile: "a.go", Language: "go"}},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	f := &statusfile.File{RepoPath: repoPath, ReindexRequired: reindexRequired}
	if reindexRequired {
		f.ReindexReason = "graph.fb format version 4 is older than required version 5 — please reindex"
	}
	if err := statusfile.Write(repoPath, f); err != nil {
		t.Fatalf("statusfile.Write: %v", err)
	}
	return registry.Repo{Slug: slug, Path: repoPath}
}

// TestStatusSummary_CountsReposAwaitingFormatMigration proves the count is
// derived from the statusfile the engine already maintains.
func TestStatusSummary_CountsReposAwaitingFormatMigration(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repos := []registry.Repo{
		seedRepoWithStatusfile(t, root, "stale-a", true),
		seedRepoWithStatusfile(t, root, "stale-b", true),
		seedRepoWithStatusfile(t, root, "current-c", false),
	}

	summary := ComputeStatusSummary("grp", repos)

	if summary.ReindexRequired != 2 {
		t.Errorf("summary.ReindexRequired = %d, want 2", summary.ReindexRequired)
	}
	if rs := summary.RepoStats["stale-a"]; rs == nil || !rs.ReindexRequired {
		t.Errorf("stale-a: ReindexRequired = %v, want true", rs)
	}
	if rs := summary.RepoStats["current-c"]; rs == nil || rs.ReindexRequired {
		t.Errorf("current-c: ReindexRequired = %v, want false", rs)
	}
}

// TestPrintStatusSummary_ShowsMigrationProgress is the user-facing half: with a
// migration underway the output must say so, name it as a format upgrade, and
// carry both the remaining count and the denominator, so "24 of 140" reads as
// progress rather than a hang.
func TestPrintStatusSummary_ShowsMigrationProgress(t *testing.T) {
	s := &StatusSummary{
		GroupName: "grp",
		RepoStats: map[string]*RepoStatus{},
	}
	for i := 0; i < 140; i++ {
		slug := "r" + itoaPad(i)
		s.RepoStats[slug] = &RepoStatus{Slug: slug, Path: "/repo/" + slug, LastIndexedAge: "1m ago"}
	}
	i := 0
	for _, rs := range s.RepoStats {
		if i < 24 {
			rs.ReindexRequired = true
		}
		i++
	}
	s.ReindexRequired = 24

	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()

	if !strings.Contains(strings.ToLower(out), "format upgrade") {
		t.Errorf("output does not name the cause (%q missing):\n%s", "format upgrade", out)
	}
	if !strings.Contains(out, "24") || !strings.Contains(out, "140") {
		t.Errorf("output must carry both the remaining count (24) and the total (140):\n%s", out)
	}
	if !strings.Contains(out, "reindex") {
		t.Errorf("output must say what is happening (reindex):\n%s", out)
	}
}

// TestPrintStatusSummary_SilentWhenNoMigration is the zero-noise guard: the
// overwhelming majority of runs have no migration underway, and a status line
// that is always present is a line users stop reading. It also keeps the
// byte-for-byte golden in status_output_parity_5995_test.go valid.
func TestPrintStatusSummary_SilentWhenNoMigration(t *testing.T) {
	s := &StatusSummary{
		GroupName: "grp",
		RepoStats: map[string]*RepoStatus{
			"a": {Slug: "a", Path: "/repo/a", LastIndexedAge: "1m ago"},
		},
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()

	for _, forbidden := range []string{"format upgrade", "migrat", "awaiting reindex"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("output mentions %q with no migration underway — must be silent:\n%s", forbidden, out)
		}
	}
}

func itoaPad(i int) string {
	s := ""
	for _, d := range []int{100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}
