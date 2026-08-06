package main

// rebuild_manifest_6208_test.go — #6208: the daemon's `grafel rebuild` path
// (both plain and `--wipe`) has the same manifest gap #6207 fixed for the
// scheduler's fallback full index, deliberately left out of that change's
// scope.
//
// `daemonRebuildFuncCore`'s indexOneInner closure only set incrementalStateDir
// when args.Incremental && !args.Wipe, and asked for a manifest ONLY via
// WithIncremental(incrementalStateDir) / IncrementalStateDir on the subprocess
// side — both of which are skipped whenever incrementalStateDir == "", i.e. on
// a plain rebuild OR any --wipe rebuild. So neither of those two shapes left a
// baseline behind, and the next incremental pass had nothing to diff against.
//
// --wipe DECISION PINNED HERE: a wiped rebuild gets a FRESH manifest
// describing what --wipe just built, not no manifest at all. See the
// daemon.go:2109 comment block for the argument; TestRebuild_Wipe_* below pins
// the outcome, not the reasoning.
//
// Every test drives the REAL path (daemonRebuildFuncCore with the real Index
// function, no mocks) so what is asserted is the manifest's CONTENT, never
// timing. fallbackManifestRepo, assertManifestExact, assertNoManifestYet,
// writeFixtureFile and seedGitRun are shared with
// scheduler_fallback_manifest_6207_test.go / worktree_seed_parity_test.go
// (same package).

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/registry"
)

// rebuildManifestFixtureRoot builds the SAME git fixture as #6207's
// fallbackManifestRepo, but OWNS its root directory outside t.TempDir with a
// best-effort retrying cleanup instead.
//
// WORKAROUND FOR #6188, not a defect in this test: a rebuild that reaches the
// daemon's install/watcher-adjacent filesystem machinery creates state under
// GRAFEL_DAEMON_ROOT asynchronously — after the call that appears to have
// written it has already returned (measured in #6188 as 1-13 poll iterations
// post-return for install.Apply's <repo>/.grafel/logs; the same class of
// after-return write). A t.TempDir-owned root therefore races t.TempDir's
// RemoveAll at cleanup and intermittently fails with
// "TempDir RemoveAll cleanup: ... directory not empty" — reproduced here as a
// flake in TestRebuild_WritesExactManifest_* only under full-package
// scheduling pressure, never in isolation, exactly #6188's documented
// signature ("Any future test that calls Apply with watchers enabled against
// a t.TempDir repo will hit it" — this rebuild path is that future test).
//
// #6188 has been decided: Apply must not return before its owned side effects
// are durable, and the #6179 workaround (same shape as this one) is reverted
// when that lands. Revert this to t.TempDir() at the same time.
func rebuildManifestFixtureRoot(t *testing.T, name, branch string) (repo string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root, err := os.MkdirTemp("", "grafel-6208-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var lastErr error
		for i := 0; i < 5; i++ {
			lastErr = os.RemoveAll(root)
			if lastErr == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		// #6188 scopes this retry to the TRANSIENT race (Apply-adjacent
		// machinery still writing under root when cleanup starts) — five
		// attempts at 20ms is comfortably past the 1-13 poll iterations #6188
		// measured. A PERMANENT failure (e.g. a future change leaks a live
		// child holding an fd under root) must not go silent just because this
		// workaround exists: t.TempDir's own cleanup would have logged
		// "TempDir RemoveAll cleanup: ..." in that case, so this does too,
		// rather than discarding the last error and leaving root on disk with
		// nothing said.
		t.Logf("rebuildManifestFixtureRoot: RemoveAll(%s) still failing after 5 retries: %v", root, lastErr)
	})

	// Never touch the real ~/.grafel, ~/Library/LaunchAgents or ~/.claude.json.
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "grafelhome"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))
	t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")

	repo = filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(repo, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, repo, "go.mod", "module fallbackfixture\n\ngo 1.21\n")
	writeFixtureFile(t, repo, "svc/widget.go", "package svc\n\ntype Widget struct{ Name string }\n\nfunc (w *Widget) Render() string { return w.Name }\n")
	writeFixtureFile(t, repo, "svc/gadget.go", "package svc\n\nfunc Gadget() string { return \"g\" }\n")
	writeFixtureFile(t, repo, "svc/thing.go", "package svc\n\nfunc Thing() int { return 7 }\n")

	seedGitRun(t, repo, "init", "-q", "-b", branch)
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "fixture")
	return repo
}

// rebuildManifestGroup builds the #6208 git fixture (rebuildManifestFixtureRoot,
// above — a #6188 workaround over #6207's fallbackManifestRepo shape) and
// registers it as a one-repo fleet group so daemonRebuildFuncCore has a real
// group to rebuild. Returns the group name and the repo path.
func rebuildManifestGroup(t *testing.T, name, branch string) (group, repo string) {
	t.Helper()
	repo = rebuildManifestFixtureRoot(t, name, branch)
	group = name + "-group"
	cfgPath := daemonHomeFleetPath(t, group)
	cfg := &registry.GroupConfig{
		Name:  group,
		Repos: []registry.Repo{{Slug: "svc", Path: repo}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	return group, repo
}

// daemonHomeFleetPath resolves the fleet.json path under the GRAFEL_HOME that
// fallbackManifestRepo already pointed the environment at.
func daemonHomeFleetPath(t *testing.T, group string) string {
	t.Helper()
	home := os.Getenv("GRAFEL_HOME")
	if home == "" {
		t.Fatal("GRAFEL_HOME resolved empty — fixture setup is not isolated")
	}
	return home + "/" + group + ".fleet.json"
}

// useInlineSubprocessRebuild forces the rebuild's subprocess reroute
// (sched.SubprocessIndexEnabled) ON and drives the REAL child entrypoint
// (runIndexInternal) in-process instead of forking a binary the test process
// does not contain, by delegating to #6207's useInlineSubprocessIndex.
//
// Deliberately does NOT stub runRebuildSubprocess itself: that closure is now
// what BUILDS the *sched.SubprocessIndexOptions (including PersistManifest,
// #6208) from rebuildSubprocessParams and hands it to subprocessIndexRunner —
// exactly the mapping this file's tests are about. Stubbing runRebuildSubprocess
// wholesale would require the test to reconstruct that mapping itself, which
// silently stops exercising it (caught in review: a mutant that dropped
// PersistManifest from runRebuildSubprocess's SubprocessIndexOptions literal
// passed every subprocess test here when they stubbed runRebuildSubprocess
// directly). Intercepting one level lower, at subprocessIndexRunner — the same
// seam daemonSchedulerIndex uses — captures the real opts runRebuildSubprocess
// built; only the fork itself is elided.
func useInlineSubprocessRebuild(t *testing.T, sawFork *bool) {
	t.Helper()
	useInlineSubprocessIndex(t, sawFork)
}

// --- plain `grafel rebuild` writes a manifest --------------------------------

func TestRebuild_WritesExactManifest_InProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	forceInProcessRebuild(t)
	group, repo := rebuildManifestGroup(t, "rebuild-plain-inprocess", "main")
	stateDir := daemon.StateDirForRepo(repo)
	assertNoManifestYet(t, stateDir)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	assertManifestExact(t, repo, stateDir)
}

func TestRebuild_WritesExactManifest_Subprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	group, repo := rebuildManifestGroup(t, "rebuild-plain-subprocess", "main")
	var sawFork bool
	useInlineSubprocessRebuild(t, &sawFork)
	stateDir := daemon.StateDirForRepo(repo)
	assertNoManifestYet(t, stateDir)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if !sawFork {
		t.Fatal("the subprocess branch was never taken — this test asserted nothing about it")
	}
	assertManifestExact(t, repo, stateDir)
}

// --- `grafel rebuild --wipe` writes a FRESH manifest, not none ---------------

// TestRebuild_Wipe_WritesFreshManifestDescribingPostWipeIndex pins the --wipe
// decision: the manifest describes what --wipe just built. It warms the repo
// with an ordinary rebuild first (graph + manifest both present, so a wipe has
// something to discard), then mutates a file's content and wipes+rebuilds. If
// the wipe left the old manifest in place (never discarded) OR if a manifest
// were written before the wipe ran (wrong ordering — RemoveAll would then
// delete it, leaving none), the post-rebuild manifest would either be stale
// (still stamping the OLD content) or absent. assertManifestExact re-hashes
// the CURRENT bytes on disk, so either failure mode is caught.
func TestRebuild_Wipe_WritesFreshManifestDescribingPostWipeIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	forceInProcessRebuild(t)
	group, repo := rebuildManifestGroup(t, "rebuild-wipe-fresh", "main")
	stateDir := daemon.StateDirForRepo(repo)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("warm-up rebuild: %v", err)
	}
	assertManifestExact(t, repo, stateDir)
	before := diff.LoadManifest(stateDir)
	staleStamp := before.Files["svc/thing.go"].SHA256

	writeFixtureFile(t, repo, "svc/thing.go", "package svc\n\nfunc Thing() int { return 999 }\n\nfunc Extra() {}\n")
	if sha256OfFile(t, repo+"/svc/thing.go") == staleStamp {
		t.Fatal("fixture is inert: the mutated file hashes the same as before")
	}

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Wipe: true, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("wipe rebuild: %v", err)
	}
	assertManifestExact(t, repo, stateDir)

	after := diff.LoadManifest(stateDir)
	if got := after.Files["svc/thing.go"].SHA256; got == staleStamp {
		t.Fatalf("manifest still stamps svc/thing.go with the PRE-wipe hash %q — the manifest was "+
			"carried across the wipe instead of describing what --wipe just built (#6208)", got)
	}
}

func TestRebuild_Wipe_WritesFreshManifestDescribingPostWipeIndex_Subprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	group, repo := rebuildManifestGroup(t, "rebuild-wipe-fresh-subprocess", "main")
	var sawFork bool
	useInlineSubprocessRebuild(t, &sawFork)
	stateDir := daemon.StateDirForRepo(repo)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("warm-up rebuild: %v", err)
	}
	assertManifestExact(t, repo, stateDir)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Wipe: true, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("wipe rebuild: %v", err)
	}
	if !sawFork {
		t.Fatal("the subprocess branch was never taken on the wipe rebuild")
	}
	assertManifestExact(t, repo, stateDir)
}

// --- the manifest a rebuild leaves behind is a real baseline ----------------

// TestRebuild_IncrementalPassDiffsAgainstRebuildManifest pins the acceptance
// criterion directly: after a plain `grafel rebuild`, a following incremental
// pass must DIFF against the manifest the rebuild left behind (Done=true,
// ChangedFiles == 1) rather than falling back to a full reindex because no
// baseline exists.
func TestRebuild_IncrementalPassDiffsAgainstRebuildManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	forceInProcessRebuild(t)
	group, repo := rebuildManifestGroup(t, "rebuild-diffbase", "main")
	stateDir := daemon.StateDirForRepo(repo)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	assertManifestExact(t, repo, stateDir)

	writeFixtureFile(t, repo, "svc/thing.go", "package svc\n\nfunc Thing() int { return 123 }\n")
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "touch thing")

	res := daemonSchedulerIncremental(context.Background(), repo, "", nil)
	if !res.Done {
		t.Fatalf("incremental pass DECLINED (reason=%q) — it had no baseline to diff against "+
			"because the preceding rebuild wrote no manifest (#6208)", res.FallbackReason)
	}
	if res.ChangedFiles != 1 {
		t.Fatalf("ChangedFiles = %d, want exactly 1 — the incremental pass did not diff narrowly "+
			"against the rebuild's manifest", res.ChangedFiles)
	}
}

// --- #6207's ordering property must still hold on the rebuild path ---------

// TestRebuild_GraphWriteFailureLeavesManifestUntouched re-pins #6207's
// ordering invariant (commitManifest only runs on writeGraphGen's success
// branch) specifically through the REBUILD path this change touches, so a
// mutant that opened a new route to commitManifest bypassing that ordering
// (e.g. by committing before the graph write, or unconditionally) is caught
// here even though #6207's own test file never drives daemonRebuildFuncCore.
func TestRebuild_GraphWriteFailureLeavesManifestUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	forceInProcessRebuild(t)
	group, repo := rebuildManifestGroup(t, "rebuild-graphfail", "main")
	stateDir := daemon.StateDirForRepo(repo)

	if _, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	assertManifestExact(t, repo, stateDir)
	before := diff.LoadManifest(stateDir)
	staleStamp := before.Files["svc/thing.go"].SHA256
	if staleStamp == "" {
		t.Fatal("fixture is inert: no stamp recorded for svc/thing.go after pass 1")
	}

	writeFixtureFile(t, repo, "svc/thing.go", "package svc\n\nfunc Thing() int { return 42 }\n\nfunc Extra() {}\n")
	if sha256OfFile(t, repo+"/svc/thing.go") == staleStamp {
		t.Fatal("fixture is inert: the mutated file hashes the same as before")
	}

	prevWriter := writeGraphGen
	writeGraphGen = func(dir string, doc *graph.Document) (string, error) {
		return "", errors.New("injected: graph.fb write failed (disk full / EPERM / gen-flip)")
	}
	t.Cleanup(func() { writeGraphGen = prevWriter })

	// Rebuild RPCs surface a per-repo child/index error; the graph write itself
	// is non-fatal inside Index, so an error here is neither guaranteed nor
	// what is asserted.
	_, _, _ = daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, Index, noopLinksFn,
	)

	after := diff.LoadManifest(stateDir)
	if got := after.Files["svc/thing.go"].SHA256; got != staleStamp {
		t.Fatalf("the manifest advanced svc/thing.go to %q even though the graph write FAILED "+
			"(was %q) — commitManifest ran outside writeGraphGen's success branch (#6207/#6208)",
			got, staleStamp)
	}
	if len(after.Files) != len(before.Files) {
		t.Fatalf("manifest membership changed (%d → %d) on a failed graph write",
			len(before.Files), len(after.Files))
	}
}
