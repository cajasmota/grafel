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
	return seedRepoWithStatusfileAtInner(t, repoPath, slug, reindexRequired, time.Now())
}

// seedRepoWithStatusfileAt is seedRepoWithStatusfile with an explicit heartbeat
// time, for exercising the liveness gate.
func seedRepoWithStatusfileAt(t *testing.T, root, slug string, reindexRequired bool, heartbeatAt time.Time) registry.Repo {
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
	return seedRepoWithStatusfileAtInner(t, repoPath, slug, reindexRequired, heartbeatAt)
}

func seedRepoWithStatusfileAtInner(t *testing.T, repoPath, slug string, reindexRequired bool, heartbeatAt time.Time) registry.Repo {
	t.Helper()
	f := &statusfile.File{RepoPath: repoPath, ReindexRequired: reindexRequired, HeartbeatAt: heartbeatAt}
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

// TestPrintStatusSummary_DoesNotClaimProgressWhenEngineIsGone is review
// finding 3. statusfile.Read is a plain unmarshal with no freshness gate, so a
// stopped daemon leaves the last heartbeat's ReindexRequired=true on disk
// forever. The old line then told the user "grafel is migrating them
// automatically ... No action needed" while nothing whatsoever was running —
// the exact opposite of the correct advice, which is to start the daemon.
func TestPrintStatusSummary_DoesNotClaimProgressWhenEngineIsGone(t *testing.T) {
	s := &StatusSummary{
		GroupName:       "grp",
		ReindexRequired: 3,
		MigrationLive:   false, // engine not heartbeating
		RepoStats: map[string]*RepoStatus{
			"a": {Slug: "a", Path: "/repo/a", LastIndexedAge: "1m ago", ReindexRequired: true},
		},
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()

	if strings.Contains(strings.ToLower(out), "no action needed") {
		t.Errorf("claims no action needed while the engine is not running:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "not running") && !strings.Contains(strings.ToLower(out), "grafel start") {
		t.Errorf("must tell the user to start the daemon — that IS the action:\n%s", out)
	}
}

// TestPrintStatusSummary_ReportsGivenUpRepos proves the migration's give-up is
// visible. A repo the engine has abandoned still has ReindexRequired=true, so
// without this it would be counted as "in progress" forever.
func TestPrintStatusSummary_ReportsGivenUpRepos(t *testing.T) {
	s := &StatusSummary{
		GroupName:       "grp",
		ReindexRequired: 2,
		MigrationFailed: 2,
		MigrationLive:   true,
		RepoStats: map[string]*RepoStatus{
			"a": {Slug: "a", Path: "/repo/a", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationFailed: true},
			"b": {Slug: "b", Path: "/repo/b", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationFailed: true},
		},
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := strings.ToLower(buf.String())

	if !strings.Contains(out, "could not be migrated") {
		t.Errorf("abandoned repos must be reported, not counted as in-progress:\n%s", out)
	}
	if !strings.Contains(out, "grafel index") {
		t.Errorf("must name the manual fix:\n%s", out)
	}
}

// TestComputeStatusSummary_MigrationLivenessFromHeartbeat proves the liveness
// signal is derived from the statusfile's own HeartbeatAt, so it needs no
// daemon dial and works exactly where the rest of this registry-based summary
// works.
func TestComputeStatusSummary_MigrationLivenessFromHeartbeat(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	fresh := seedRepoWithStatusfileAt(t, root, "fresh", true, time.Now())
	summary := ComputeStatusSummary("grp", []registry.Repo{fresh})
	if !summary.MigrationLive {
		t.Error("a repo heartbeating right now must read as a live migration")
	}

	root2 := t.TempDir()
	t.Setenv(daemon.EnvRoot, root2)
	stale := seedRepoWithStatusfileAt(t, root2, "stale", true, time.Now().Add(-30*time.Minute))
	summary2 := ComputeStatusSummary("grp", []registry.Repo{stale})
	if summary2.MigrationLive {
		t.Error("a 30-minute-old heartbeat must NOT read as a live migration")
	}
}
