package cli

// status_ref_5822_test.go — #5822 defects C and D.
//
// Both defects have ONE root cause: `grafel status` re-derived every repo's
// per-ref state directory from LIVE GIT at display time, instead of from the
// ref the caller asked for (C) or from what is actually on disk (D).
//
//   C — `--ref main` was consumed only to print "Note: showing state for ref
//       …"; ComputeStatusSummary took no ref at all and every repo resolved
//       through daemon.StateDirForRepo → gitmeta.CaptureCached → current HEAD.
//       The command therefore printed a note claiming one thing and rendered
//       another.
//   D — when git could not be RUN (2s deadline, EAGAIN on fork, an OOM-killed
//       child), the capture is untrusted and its Ref is "", which
//       RefSafeEncode turns into the "_unknown" sentinel directory. No graph
//       ever lives there, so the repo rendered as "0 entities · 0 rels ·
//       indexed (never)" — indistinguishable from a repo that genuinely has no
//       graph — and contributed nothing to TOTAL. Next run git succeeded and
//       the numbers came back.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// initGitRepoCLI creates a REAL git checkout on branch `main`.
//
// It is a real repo deliberately. A non-git tmpdir would make git answer "not a
// git repository" — a durable, trusted answer that legitimately resolves to the
// `_unknown` slot — so every assertion below would hold regardless of the
// production code, which is exactly the fixture-vacuity trap this repo has been
// bitten by before.
func initGitRepoCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runIn("init")
	runIn("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn("add", ".")
	runIn("commit", "-m", "init")
	return dir
}

// seedSidecar writes a graph-stats.json with the given entity count into the
// state directory for (repo, ref). It deliberately builds the path with
// StateDirForRepoRef rather than StateDirForRepo: the latter would run a git
// capture and MEMOIZE the resulting ref, which would then be served from cache
// even while git is unavailable and would hide the defect under test.
func seedSidecar(t *testing.T, repoPath, ref string, entities, rels int) string {
	t.Helper()
	dir := daemon.StateDirForRepoRef(repoPath, ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-5 * time.Minute),
		TotalEntities:      entities,
		TotalRelationships: rels,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph-stats.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stubGitUnrunnable puts a `git` on PATH that kills itself with SIGKILL. A
// signalled child is classified gitUnavailable by internal/gitmeta, which is
// the same state a fork EAGAIN or a fired 2s deadline produces — the #5822-D
// trigger, injected rather than provoked. Returns the real PATH.
func stubGitUnrunnable(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub git script is POSIX-shell only")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte("#!/bin/sh\nkill -9 $$\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := os.Getenv("PATH")
	t.Setenv("PATH", binDir)
	return real
}

// TestComputeStatusSummaryForRef_ReadsTheRequestedRef is defect C.
//
// The fixture has TWO graphs on disk for one repo — one under refs/main (the
// live HEAD) and a differently-sized one under refs/feature-x. Asking for
// feature-x must report feature-x's numbers. Before the fix the ref never
// reached the resolver and HEAD's numbers came back with a note claiming
// otherwise.
func TestComputeStatusSummaryForRef_ReadsTheRequestedRef(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	repoPath := initGitRepoCLI(t)

	seedSidecar(t, repoPath, "main", 111, 222)
	seedSidecar(t, repoPath, "feature-x", 7777, 8888)

	// Anti-vacuity: the live HEAD must really be `main`, otherwise "asked for
	// feature-x, got feature-x" could be true by accident.
	if got, want := daemon.StateDirForRepo(repoPath), daemon.StateDirForRepoRef(repoPath, "main"); got != want {
		t.Fatalf("fixture broken: live HEAD resolves to %q, want %q — this test can only "+
			"distinguish 'used the requested ref' from 'used HEAD' when they differ", got, want)
	}

	repos := []registry.Repo{{Slug: "r", Path: repoPath}}
	summary := ComputeStatusSummaryForRef("grp", repos, "feature-x")

	rs, ok := summary.RepoStats["r"]
	if !ok {
		t.Fatal("repo missing from RepoStats")
	}
	if rs.Entities != 7777 || rs.Relationships != 8888 {
		t.Fatalf("--ref feature-x reported %d entities / %d rels, want 7777/8888 — "+
			"the ref was ignored and the current HEAD's graph was shown instead (#5822 C)",
			rs.Entities, rs.Relationships)
	}
	if summary.TotalEntities != 7777 {
		t.Fatalf("TOTAL entities = %d, want 7777", summary.TotalEntities)
	}
}

// TestComputeStatusSummary_DefaultsToHEAD guards the other side: with no ref
// requested, status must keep showing the current HEAD's graph exactly as
// before.
func TestComputeStatusSummary_DefaultsToHEAD(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	repoPath := initGitRepoCLI(t)

	seedSidecar(t, repoPath, "main", 111, 222)
	seedSidecar(t, repoPath, "feature-x", 7777, 8888)

	summary := ComputeStatusSummary("grp", []registry.Repo{{Slug: "r", Path: repoPath}})
	if rs := summary.RepoStats["r"]; rs.Entities != 111 {
		t.Fatalf("default view reported %d entities, want 111 (the HEAD ref's graph)", rs.Entities)
	}
}

// TestRunStatus_ThreadsRefIntoTheSummary is defect C end-to-end, through the
// command function rather than through ComputeStatusSummaryForRef directly.
//
// The unit test above proves the summary CAN honour a ref; this proves `grafel
// status --ref` actually asks it to. Without it, reverting one argument at the
// call site — the literal defect, where the flag was resolved, validated,
// announced in a note, and then dropped — would leave every other test green.
func TestRunStatus_ThreadsRefIntoTheSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv(daemon.EnvRoot, t.TempDir())

	repoPath := initGitRepoCLI(t)
	seedSidecar(t, repoPath, "main", 111, 222)
	seedSidecar(t, repoPath, "feature-x", 7777, 8888)

	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "testgroup.fleet.json")
	cfg := registry.GroupConfig{Name: "testgroup"}
	cfg.Repos = []registry.Repo{{Slug: "testrepo", Path: repoPath}}
	if err := registry.SaveGroupConfig(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("testgroup", cfgPath); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runStatus(&buf, "", "feature-x", false); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `ref "feature-x"`) {
		t.Fatalf("fixture broken: the ref note is missing, so the run under test is not "+
			"the --ref run:\n%s", out)
	}
	if !strings.Contains(out, fmtInt(7777)) {
		t.Fatalf("status --ref feature-x did not report feature-x's graph — the note "+
			"claims one ref and the numbers come from HEAD (#5822 C):\n%s", out)
	}
	if strings.Contains(out, fmtInt(111)+" entities") {
		t.Fatalf("status --ref feature-x reported the HEAD ref's entity count:\n%s", out)
	}
}

// The other half of defect C — `--ref @all` must refuse rather than print a
// note advertising a per-ref breakdown it does not produce — lives with the
// rest of the flag's coverage in ref_flag_test.go (TestStatus_Ref_All), which
// previously asserted the misleading behaviour.

// TestComputeStatusSummary_UnresolvableRefIsNotAHealthyZero is defect D.
//
// git cannot be run; the repo's ref is therefore UNKNOWN, not empty. The old
// code turned that into refs/_unknown and rendered a perfectly healthy-looking
// "0 entities · 0 rels · indexed (never)".
func TestComputeStatusSummary_UnresolvableRefIsNotAHealthyZero(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	repoPath := initGitRepoCLI(t)
	seedSidecar(t, repoPath, "main", 4242, 5252)

	realPath := stubGitUnrunnable(t)

	repos := []registry.Repo{{Slug: "r", Path: repoPath}}
	summary := ComputeStatusSummaryForRef("grp", repos, "")
	rs, ok := summary.RepoStats["r"]
	if !ok {
		t.Fatal("repo missing from RepoStats — an unknown ref must not make the repo vanish")
	}
	if !rs.RefUnknown {
		t.Fatalf("RefUnknown is false: an un-runnable git fell through to the _unknown "+
			"sentinel and the repo reads as %d entities / indexed %q (#5822 D)",
			rs.Entities, rs.LastIndexedAge)
	}

	var buf bytes.Buffer
	PrintStatusSummary(&buf, summary)
	out := buf.String()
	if strings.Contains(out, "indexed (never)") {
		t.Fatalf("a repo whose ref could not be determined rendered as never-indexed — "+
			"indistinguishable from a repo that genuinely has no graph:\n%s", out)
	}
	if !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("the unknown state is not surfaced to the user:\n%s", out)
	}
	if !strings.Contains(out, "INCOMPLETE") {
		t.Fatalf("TOTAL does not disclose that a repo was skipped:\n%s", out)
	}

	// Anti-vacuity, and the "next run the numbers come back" half of the report:
	// with git schedulable again the SAME fixture must report the real graph.
	// If this fails, the seeded graph was never where production looks and the
	// assertions above proved nothing.
	t.Setenv("PATH", realPath)
	back := ComputeStatusSummaryForRef("grp", repos, "")
	if rs := back.RepoStats["r"]; rs.RefUnknown || rs.Entities != 4242 {
		t.Fatalf("fixture broken: with git healthy the repo reports RefUnknown=%v / %d entities, "+
			"want false / 4242", rs.RefUnknown, rs.Entities)
	}
}

// TestComputeStatusSummary_ExplicitRefSurvivesAnUnrunnableGit ties the two
// defects together: once the requested ref is threaded through, `--ref` needs
// no git at all, so it keeps working in exactly the conditions that break the
// default view.
func TestComputeStatusSummary_ExplicitRefSurvivesAnUnrunnableGit(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	repoPath := initGitRepoCLI(t)
	seedSidecar(t, repoPath, "main", 4242, 5252)

	stubGitUnrunnable(t)

	summary := ComputeStatusSummaryForRef("grp", []registry.Repo{{Slug: "r", Path: repoPath}}, "main")
	rs := summary.RepoStats["r"]
	if rs.RefUnknown || rs.Entities != 4242 {
		t.Fatalf("--ref main with git unavailable: RefUnknown=%v entities=%d, want false/4242 — "+
			"an explicitly requested ref needs no git capture", rs.RefUnknown, rs.Entities)
	}
}
