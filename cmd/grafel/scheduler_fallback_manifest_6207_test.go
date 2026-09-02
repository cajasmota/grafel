package main

// #6207 — the scheduler's fallback full index must leave a COMPLETE, EXACT and
// CORRECTLY LOCATED diff manifest behind, must write it only once the graph it
// describes is durably on disk, and must remain a FULL index.
//
// `daemonSchedulerIncremental` declines (any reason), the scheduler runs
// `daemonSchedulerIndex`, and that full index has just established ground truth
// for every file in the repo. That is exactly what a manifest records, so the
// manifest must reflect it. Before this change neither branch of
// daemonSchedulerIndex asked for a manifest, so `i.incremental` stayed false
// and index.go's write was skipped entirely — the manifest on the scheduler
// path was advanced ONLY by TryIncremental, including on the passes where it
// did no work.
//
// Every test here drives the REAL path: it makes an incremental pass decline
// and then runs the fallback the scheduler runs. Assertions are on observable
// state — the manifest's contents hashed against the bytes on disk, the
// directory it landed in, and the indexer's own per-run file counts — never on
// timing.
//
// The cold fixture reaches the decline via the #5710 `absent-graph-nonempty-tree`
// guard: there is no graph.fb yet, which is the ordinary state of a repo the
// daemon has never indexed. That branch writes no manifest of its own, so
// anything found in the manifest afterwards came from the fallback and nowhere
// else — and each test asserts that emptiness before proceeding rather than
// assuming it.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// fallbackFixtureFiles is the repo-relative file set the manifest must account
// for, EXACTLY. Hand-derived from the fixture rather than from the indexer's
// own walk, so neither an empty walk nor an over-broad one can satisfy it.
var fallbackFixtureFiles = []string{"go.mod", "svc/gadget.go", "svc/thing.go", "svc/widget.go"}

// fallbackManifestRepo builds a tiny committed git repo and isolates every
// state dir the daemon or the indexer might touch. branch selects the branch
// the fixture is committed on, which matters to the ref-divergence test.
func fallbackManifestRepo(t *testing.T, name, branch string) (repo string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
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

func writeFixtureFile(t *testing.T, repo, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha256OfFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// assertManifestExact is the property under test. It is deliberately an
// EXACTNESS check, not a presence check: the manifest's key set must equal the
// repo's file set, and every stamp must be the hash of the bytes on disk.
//
// Cardinality matters as much as presence. UpdateManifest's prune is the same
// mechanism as #5667's loop guard — a manifest entry for a file no longer in
// the gitignore-aware walk is otherwise immortal, is reported "deleted" on
// every pass, and re-trips the too-many-changed fallback forever. "No stale
// entry survives" is therefore the most valuable half of this assertion, and a
// want-list walk alone would never see it.
func assertManifestExact(t *testing.T, repo, stateDir string) {
	t.Helper()
	m := diff.LoadManifest(stateDir)
	if m == nil || len(m.Files) == 0 {
		t.Fatalf("the fallback full index wrote NO manifest in %s — the next incremental pass "+
			"sees the same changed set again, forever (#6207)", stateDir)
	}
	got := make([]string, 0, len(m.Files))
	for k := range m.Files {
		got = append(got, k)
	}
	sort.Strings(got)
	want := append([]string(nil), fallbackFixtureFiles...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("manifest key set is not the repo's file set\n got: %v\nwant: %v\n"+
			"an extra entry is a stale record that re-presents as \"deleted\" on every pass (#5667); "+
			"a missing one is a file with no baseline (#6207)", got, want)
	}
	for _, rel := range want {
		wantHash := sha256OfFile(t, filepath.Join(repo, filepath.FromSlash(rel)))
		if m.Files[rel].SHA256 != wantHash {
			t.Fatalf("manifest stamp for %q is %q, want the indexed content hash %q — "+
				"the manifest does not reflect what was indexed (#6207)",
				rel, m.Files[rel].SHA256, wantHash)
		}
	}
}

// declineIncrementalPass drives the scheduler's incremental callback and
// requires that it DECLINES for the expected reason. Asserting the reason (not
// just the decline) keeps the fixture honest: if the pass ever starts
// succeeding, or starts declining somewhere else that happens to write a
// manifest of its own, this fails rather than passing for the wrong reason.
func declineIncrementalPass(t *testing.T, repo, wantReason string) {
	t.Helper()
	res := daemonSchedulerIncremental(context.Background(), repo, "", nil)
	if res.Done {
		t.Fatal("fixture is inert: the incremental pass SUCCEEDED, so the scheduler would never " +
			"reach the fallback full index this test is about")
	}
	if !strings.Contains(res.FallbackReason, wantReason) {
		t.Fatalf("incremental declined with reason %q, want one containing %q — the fixture is "+
			"exercising a different decline path than this test assumes", res.FallbackReason, wantReason)
	}
}

// assertNoManifestYet proves the decline path itself wrote nothing, so whatever
// a later assertion finds is attributable to the fallback index alone.
func assertNoManifestYet(t *testing.T, stateDir string) {
	t.Helper()
	if m := diff.LoadManifest(stateDir); len(m.Files) != 0 {
		t.Fatalf("fixture is inert: a manifest with %d entries already exists in %s, so the "+
			"assertions below would not be about the fallback index at all", len(m.Files), stateDir)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns what was
// written. The indexer logs its per-run file counts there, which is the only
// observable that distinguishes a full walk from a delta-filtered one.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = prev }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	os.Stderr = prev
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// useInProcessIndex forces daemonSchedulerIndex down its in-process branch.
func useInProcessIndex(t *testing.T) {
	t.Helper()
	prev := sched.SetSubprocessIndexEnabled(false)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prev) })
}

// useInlineSubprocessIndex forces the fork branch and runs the child IN-PROCESS
// through its real entrypoint.
//
// The child is the side that writes the manifest — it owns the walk, and it is
// the only side that can order the write after its own graph write — so the
// property can only be pinned by driving the parent's argv construction INTO a
// child. Rather than fork a binary this test does not have, it captures the
// exact opts the scheduler hands to sched.RunSubprocessIndex, renders them
// through the same argv builder the real fork uses (sched.SubprocessIndexArgs),
// and feeds that argv to runIndexInternal. Every link in the chain (opts field
// → flag → IndexOption → manifest write) is therefore exercised; only the fork
// itself is elided.
func useInlineSubprocessIndex(t *testing.T, sawFork *bool) {
	t.Helper()
	prevEnabled := sched.SetSubprocessIndexEnabled(true)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prevEnabled) })
	prevRunner := subprocessIndexRunner
	subprocessIndexRunner = func(ctx context.Context, repoPath, ref string, skipPasses []string, opts *sched.SubprocessIndexOptions, logger *slog.Logger) error {
		args := sched.SubprocessIndexArgs(repoPath, ref, skipPasses, opts)
		if len(args) == 0 || args[0] != "index-internal" {
			return fmt.Errorf("unexpected child argv %v", args)
		}
		*sawFork = true
		if code := runIndexInternal(args[1:]); code != 0 {
			return fmt.Errorf("index-internal child exited %d (argv %v)", code, args)
		}
		return nil
	}
	t.Cleanup(func() { subprocessIndexRunner = prevRunner })
}

// --- the core property, on both branches ------------------------------------

// TestSchedulerFallbackIndex_WritesExactManifest_InProcess covers the
// in-process branch of daemonSchedulerIndex (GRAFEL_SUBPROCESS_INDEXER=0).
func TestSchedulerFallbackIndex_WritesExactManifest_InProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "fallback-inprocess", "main")
	useInProcessIndex(t)

	stateDir := daemon.StateDirForRepo(repo)
	assertNoManifestYet(t, stateDir)
	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")
	assertNoManifestYet(t, stateDir)

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}
	assertManifestExact(t, repo, stateDir)
}

// TestSchedulerFallbackIndex_WritesExactManifest_Subprocess covers the fork
// branch. See useInlineSubprocessIndex for how the child is driven.
func TestSchedulerFallbackIndex_WritesExactManifest_Subprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "fallback-subprocess", "main")
	var sawFork bool
	useInlineSubprocessIndex(t, &sawFork)

	stateDir := daemon.StateDirForRepo(repo)
	assertNoManifestYet(t, stateDir)
	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")
	assertNoManifestYet(t, stateDir)

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}
	if !sawFork {
		t.Fatal("the subprocess branch was never taken — this test asserted nothing about it")
	}
	assertManifestExact(t, repo, stateDir)
}

// --- the manifest must land beside the graph, not beside the enqueue ref -----

// TestSchedulerFallbackIndex_ManifestLandsBesideTheGraph pins that the manifest
// is written to the directory the GRAPH went to, not to one derived from the
// ref captured at enqueue time.
//
// daemonSchedulerIndex resolves its stateDir from the `ref` argument — the ref
// as of ENQUEUE. The indexer resolves its output directory from gitmeta at
// INDEX time. A checkout landing between the two makes those different
// directories. This test reproduces that by enqueuing with a ref the repo is
// NOT on: the graph goes under the current HEAD's ref dir, and the manifest
// must follow it there and must NOT appear under the enqueue ref.
//
// Getting this wrong is not silently fatal — the #5710 head-advance guard
// usually catches the mismatch — but it leaves one branch with no baseline and
// the other with a manifest describing a graph built from a different branch,
// which costs reindex cycles that get attributed elsewhere.
func TestSchedulerFallbackIndex_ManifestLandsBesideTheGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "fallback-refdiverge", "main")
	useInProcessIndex(t)

	const enqueueRef = "refs/heads/feature-a"
	graphDir := daemon.StateDirForRepo(repo) // resolved from HEAD, at index time
	enqueueDir := daemon.StateDirForRepoRef(repo, enqueueRef)
	if graphDir == enqueueDir {
		t.Fatal("fixture is inert: the enqueue-ref dir and the HEAD-resolved dir are the same, " +
			"so this test could not observe a divergence")
	}
	assertNoManifestYet(t, graphDir)
	assertNoManifestYet(t, enqueueDir)

	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")

	// Enqueue-time ref is feature-a; HEAD is main.
	if err := daemonSchedulerIndex(context.Background(), repo, enqueueRef); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}

	// The graph must be where gitmeta put it, and the manifest must be with it.
	if _, err := graph.LoadGraphFromDir(graphDir); err != nil {
		t.Fatalf("fixture is inert: no graph under the HEAD-resolved dir %s: %v", graphDir, err)
	}
	assertManifestExact(t, repo, graphDir)
	if m := diff.LoadManifest(enqueueDir); len(m.Files) != 0 {
		t.Fatalf("a manifest with %d entries was written under the ENQUEUE ref %s while the graph "+
			"went to %s — the manifest describes a graph that is not next to it (#6207)",
			len(m.Files), enqueueDir, graphDir)
	}
}

// --- no manifest unless the graph it describes is durably on disk ------------

// TestSchedulerFallbackIndex_GraphWriteFailureLeavesManifestUntouched pins the
// ordering constraint.
//
// The manifest must be written only AFTER graph.fb is durably on disk. The
// graph write is explicitly non-fatal and the index-internal child can be
// SIGKILLed at any point, so "the index returned" is not the same as "the graph
// landed". A manifest published for a graph that never landed is a PERMANENT
// stuck state on the daemon's automatic healing path: the next pass computes
// totalChanged == 0, the #5710 absent-graph guard fires on ABSENCE only and the
// previous generation is still present, so the pass reports Done over a stale
// graph forever. Leaving the manifest untouched instead means the same changed
// set re-presents next pass and the repo heals itself.
func TestSchedulerFallbackIndex_GraphWriteFailureLeavesManifestUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "fallback-graphfail", "main")
	useInProcessIndex(t)
	stateDir := daemon.StateDirForRepo(repo)

	// Pass 1: an ordinary successful fallback. Establishes both a graph and a
	// manifest, so the repo is in the state where the stuck-forever failure is
	// reachable (a PRESENT previous generation).
	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")
	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	assertManifestExact(t, repo, stateDir)
	before := diff.LoadManifest(stateDir)
	staleStamp := before.Files["svc/thing.go"].SHA256
	if staleStamp == "" {
		t.Fatal("fixture is inert: no stamp recorded for svc/thing.go after pass 1")
	}

	// Mutate a file so a manifest written by pass 2 would be observably
	// DIFFERENT from pass 1's, then make the graph write fail.
	writeFixtureFile(t, repo, "svc/thing.go", "package svc\n\nfunc Thing() int { return 99 }\n\nfunc Extra() {}\n")
	if sha256OfFile(t, filepath.Join(repo, "svc", "thing.go")) == staleStamp {
		t.Fatal("fixture is inert: the mutated file hashes the same as before")
	}

	prevWriter := writeGraphGen
	writeGraphGen = func(dir string, doc *graph.Document) (string, fbwriter.UndeclaredKindReport, error) {
		return "", fbwriter.UndeclaredKindReport{}, errors.New("injected: graph.fb write failed (disk full / EPERM / gen-flip)")
	}
	t.Cleanup(func() { writeGraphGen = prevWriter })

	// Pass 2: the index runs, the graph write fails. Index() treats that as
	// non-fatal, so a returned error is neither guaranteed nor what is asserted.
	_ = daemonSchedulerIndex(context.Background(), repo, "")

	after := diff.LoadManifest(stateDir)
	if got := after.Files["svc/thing.go"].SHA256; got != staleStamp {
		t.Fatalf("the manifest advanced svc/thing.go to %q even though the graph write FAILED "+
			"(was %q). The graph on disk is still the previous generation, so the next pass sees "+
			"totalChanged==0 over a stale graph and reports Done forever — a permanent stuck state "+
			"on the automatic healing path (#6207)", got, staleStamp)
	}
	if len(after.Files) != len(before.Files) {
		t.Fatalf("manifest membership changed (%d → %d) on a failed graph write",
			len(before.Files), len(after.Files))
	}
}

// --- the fallback must remain a FULL index -----------------------------------

// TestSchedulerFallbackIndex_StaysAFullIndex pins the BEHAVIOUR half of the
// flag split, which the manifest-content assertions above cannot see.
//
// On a cold repo, making the fallback diff-aware changes nothing observable:
// the manifest is empty, the prior graph will not open, and the incremental
// path falls back to processing everything anyway. So a mutant that re-fuses
// the two flags — or a call site that simply hands WithIncremental to the
// fallback, which is the wrong shape #6207 exists to reject — passes every
// other test in this file.
//
// This test warms the repo first (manifest + graph both present), then forces a
// reject with a limit of 1, and asserts the fallback index processed the FULL
// walk. A diff-aware fallback would filter to the delta — the very delta the
// reject just declared untrustworthy.
func TestSchedulerFallbackIndex_StaysAFullIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "fallback-stayfull", "main")
	useInProcessIndex(t)
	stateDir := daemon.StateDirForRepo(repo)

	// Warm: a successful fallback leaves a graph AND a complete manifest.
	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")
	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("warm-up index: %v", err)
	}
	assertManifestExact(t, repo, stateDir)

	// Change two files and cap the incremental limit at one, so the next pass
	// rejects with too-many-changed rather than succeeding.
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "1")
	writeFixtureFile(t, repo, "svc/thing.go", "package svc\n\nfunc Thing() int { return 99 }\n")
	writeFixtureFile(t, repo, "svc/gadget.go", "package svc\n\nfunc Gadget() string { return \"gg\" }\n")
	declineIncrementalPass(t, repo, "too-many-changed")

	out := captureStderr(t, func() {
		if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
			t.Errorf("fallback index: %v", err)
		}
	})
	t.Log(out)

	if strings.Contains(out, "incremental — processing") {
		t.Fatalf("the FALLBACK index was delta-filtered — it logged an incremental cache-hit line. "+
			"The run taken BECAUSE the delta could not be trusted must not filter its walk to that "+
			"same delta (#6207).\nstderr:\n%s", out)
	}
	// "processed=N extracted=M" is the indexer's per-run summary: M is the count
	// of files the pipeline actually parsed. The fixture's four walked files
	// are three .go files plus go.mod; go.mod is filtered out before the
	// processed/extracted counters, so both read 3 on a FULL walk. A
	// delta-filtered run would report the size of the delta instead.
	//
	// The `processed=` prefix is load-bearing, not decoration. Pass 2.5 emits
	// `shape_extracted=%d` on the same stream, which CONTAINS "extracted=3" —
	// it is gated on shapeStats.Endpoints > 0 so it cannot fire on this
	// endpoint-free fixture today, but the day someone adds an HTTP handler to
	// it this assertion would become satisfiable without a full walk.
	const wantExtracted = "processed=3 extracted=3"
	if !strings.Contains(out, wantExtracted) {
		t.Fatalf("the fallback index did not extract the full walk (wanted %q in its per-run "+
			"summary) — it processed a delta, not the repo (#6207).\nstderr:\n%s", wantExtracted, out)
	}
	// The manifest must still be exact afterwards.
	assertManifestExact(t, repo, stateDir)
}
