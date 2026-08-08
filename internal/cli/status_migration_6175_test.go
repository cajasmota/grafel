package cli

// status_migration_6175_test.go — #6175: `grafel status` needs a THIRD
// migration category.
//
// #6167 gave the format migration two: "in progress" and "could not be
// migrated". A repo the indexer declines to accept has neither, so it stayed in
// the in-progress count forever and the count never reached zero — reinstating,
// for that class of repo, the silence #6167 was filed about.

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

// TestPrintStatusSummary_ReportsNotAcceptedRepos is the defect proof: the
// in-progress count must EXCLUDE a repo the indexer will never accept, and that
// repo must get its own, non-blaming line.
func TestPrintStatusSummary_ReportsNotAcceptedRepos(t *testing.T) {
	s := &StatusSummary{
		GroupName:            "grp",
		ReindexRequired:      3,
		MigrationNotAccepted: 2,
		MigrationLive:        true,
		RepoStats: map[string]*RepoStatus{
			"a": {Slug: "a", Path: "/repo/a", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationNotAccepted: true},
			"b": {Slug: "b", Path: "/repo/b", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationNotAccepted: true},
			"c": {Slug: "c", Path: "/repo/c", LastIndexedAge: "1m ago", ReindexRequired: true},
		},
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := strings.ToLower(buf.String())

	if !strings.Contains(out, "not accepted by the indexer") {
		t.Errorf("a repo the indexer declines must be reported under its own state:\n%s", out)
	}
	// 3 stale - 2 not accepted = 1 genuinely migrating.
	if !strings.Contains(out, "1 of 3 repo(s)") {
		t.Errorf("in-progress line must count only the 1 repo actually being migrated, not all 3:\n%s", out)
	}
}

// TestPrintStatusSummary_NotAcceptedLineIsNotBlaming pins the wording contract
// from the issue: nothing is wrong with these repos, so the line must not read
// as an error and must not hand the user a command to run.
//
// Specifically NOT because `grafel index` would be dropped — it would not. That
// command is synchronous by default (internal/cli/index.go:73) and the
// synchronous branch of Service.Index bypasses the scheduler's SkipEnqueue gate
// entirely, so it would succeed at cold-indexing the worktree as an independent
// root repo: the exact outcome #3680's gate exists to prevent. The command is
// omitted because following it would be harmful, not because it would fail.
func TestPrintStatusSummary_NotAcceptedLineIsNotBlaming(t *testing.T) {
	s := &StatusSummary{
		GroupName:            "grp",
		ReindexRequired:      1,
		MigrationNotAccepted: 1,
		MigrationLive:        true,
		RepoStats: map[string]*RepoStatus{
			"a": {Slug: "a", Path: "/repo/a", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationNotAccepted: true},
		},
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := buf.String()

	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(l), "not accepted by the indexer") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no not-accepted line in:\n%s", out)
	}
	for _, forbidden := range []string{"⚠", "could not", "failed", "grafel index"} {
		if strings.Contains(strings.ToLower(line), strings.ToLower(forbidden)) {
			t.Errorf("the not-accepted line reads as an error / demands action (%q): %q", forbidden, line)
		}
	}
	if !strings.Contains(strings.ToLower(line), "worktree") {
		t.Errorf("the line must name the likely cause so the user can recognise it: %q", line)
	}
}

// TestPrintStatusSummary_AllNotAcceptedIsNotAlsoInProgress is the counterpart
// of #6167's round-3 LOW 5 for the new state: when every remaining stale repo
// is one the indexer declines, there is nothing in progress, and printing
// "0 of N ... migrating them automatically" above the explanation would be the
// same self-contradicting pair that finding removed.
func TestPrintStatusSummary_AllNotAcceptedIsNotAlsoInProgress(t *testing.T) {
	s := &StatusSummary{
		GroupName:            "grp",
		ReindexRequired:      2,
		MigrationNotAccepted: 2,
		MigrationLive:        true,
		RepoStats: map[string]*RepoStatus{
			"a": {Slug: "a", Path: "/repo/a", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationNotAccepted: true},
			"b": {Slug: "b", Path: "/repo/b", LastIndexedAge: "1m ago", ReindexRequired: true, MigrationNotAccepted: true},
		},
	}
	var buf bytes.Buffer
	PrintStatusSummary(&buf, s)
	out := strings.ToLower(buf.String())

	if strings.Contains(out, "in progress") {
		t.Errorf("nothing is in progress — every remaining stale repo is one the indexer declines:\n%s", out)
	}
	if !strings.Contains(out, "not accepted by the indexer") {
		t.Errorf("must still explain the remaining repos:\n%s", out)
	}
}

// TestComputeStatusSummary_CountsNotAcceptedFromStatusfile proves the counter
// is read from the sidecar the engine already writes — same free read as the
// other two categories, no daemon dial and no graph load.
func TestComputeStatusSummary_CountsNotAcceptedFromStatusfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repos := []registry.Repo{
		seedRepoWithMigrationState(t, root, "wt", statusfile.File{ReindexRequired: true, ReindexNotAccepted: true}),
		seedRepoWithMigrationState(t, root, "stale", statusfile.File{ReindexRequired: true}),
	}

	got := ComputeStatusSummary("grp", repos)

	if got.ReindexRequired != 2 {
		t.Errorf("ReindexRequired = %d, want 2", got.ReindexRequired)
	}
	if got.MigrationNotAccepted != 1 {
		t.Errorf("MigrationNotAccepted = %d, want 1", got.MigrationNotAccepted)
	}
}

// TestComputeStatusSummary_SurfacesNotAcceptedPerRepo covers the per-repo half
// of the same read: the group total says how many, RepoStatus says WHICH, and a
// per-repo renderer needs the latter to explain a specific repo's line.
func TestComputeStatusSummary_SurfacesNotAcceptedPerRepo(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repos := []registry.Repo{
		seedRepoWithMigrationState(t, root, "wt", statusfile.File{ReindexRequired: true, ReindexNotAccepted: true}),
		seedRepoWithMigrationState(t, root, "stale", statusfile.File{ReindexRequired: true}),
	}

	got := ComputeStatusSummary("grp", repos)

	if rs := got.RepoStats["wt"]; rs == nil || !rs.MigrationNotAccepted {
		t.Errorf("wt: per-repo MigrationNotAccepted not surfaced: %+v", rs)
	}
	if rs := got.RepoStats["stale"]; rs == nil || rs.MigrationNotAccepted {
		t.Errorf("stale: an ordinary stale repo must not be marked not-accepted: %+v", rs)
	}
}

// TestComputeStatusSummary_FailedRepoCountedOnce guards the arithmetic when a
// sidecar carries BOTH flags (the engine never writes that pair — see
// staleReindexGuard.notAcceptedByIndexer — but a summary that double-subtracts
// would silently under-report the in-progress count).
func TestComputeStatusSummary_FailedRepoCountedOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repos := []registry.Repo{
		seedRepoWithMigrationState(t, root, "both", statusfile.File{
			ReindexRequired: true, ReindexMigrationFailed: true, ReindexNotAccepted: true,
		}),
	}
	got := ComputeStatusSummary("grp", repos)

	if got.MigrationFailed != 1 {
		t.Errorf("MigrationFailed = %d, want 1", got.MigrationFailed)
	}
	if got.MigrationNotAccepted != 0 {
		t.Errorf("MigrationNotAccepted = %d, want 0 — the repo is already counted as failed", got.MigrationNotAccepted)
	}
}

// seedRepoWithMigrationState creates a repo with a valid graph.fb and a
// statusfile carrying the given migration flags, mirroring what the engine's
// status writer would have persisted.
func seedRepoWithMigrationState(t *testing.T, root, slug string, f statusfile.File) registry.Repo {
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
	f.RepoPath = repoPath
	f.HeartbeatAt = time.Now()
	if err := statusfile.Write(repoPath, &f); err != nil {
		t.Fatalf("statusfile.Write: %v", err)
	}
	return registry.Repo{Slug: slug, Path: repoPath}
}
