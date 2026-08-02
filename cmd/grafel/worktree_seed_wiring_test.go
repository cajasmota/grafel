package main

// Wiring guards for #5964 on the cmd/grafel side.
//
// The seeding primitives in internal/daemon are well covered on their own, but
// coverage of a helper proves nothing about whether it is CALLED. Two mutants
// removed the consumption side entirely and left every suite green:
//
//   - deleting the verifyOrDiscardSeed guard from the incremental callback, so
//     seeds were consumed with no provenance verification at all;
//   - forcing repoTag to "" in daemonSchedulerIndex, so the repo-tag pin was
//     ignored on the full-index path and a worktree's entity ids silently
//     diverged from its parent's.
//
// Both are now driven behaviourally. None of these tests inspects source text.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// seedFakeStateDir writes a state dir that LOOKS seeded: a generation file, a
// `current` pointer, a diff manifest, and a valid provenance stamp. No indexing
// involved, so these run in microseconds.
func seedFakeStateDir(t *testing.T, parentSD, childSD, repoTag string) {
	t.Helper()
	if err := os.MkdirAll(parentSD, 0o755); err != nil {
		t.Fatal(err)
	}
	name := graph.GenFileName(1)
	if err := os.WriteFile(filepath.Join(parentSD, name), []byte("PARENT-GRAPH-BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(parentSD, name); err != nil {
		t.Fatal(err)
	}
	m := diff.LoadManifest(parentSD)
	m.Files["a.go"] = diff.FileEntry{SHA256: "aa", Size: 1}
	if err := diff.SaveManifest(parentSD, "/x", m); err != nil {
		t.Fatal(err)
	}
	out := daemon.SeedWorktreeGraph(daemon.SeedRequest{
		ParentPath:     filepath.Join(t.TempDir(), "p"),
		ParentRef:      "release/x",
		ParentStateDir: parentSD,
		ChildPath:      filepath.Join(t.TempDir(), "c"),
		ChildRef:       "feat/x",
		ChildStateDir:  childSD,
		RepoTag:        repoTag,
	})
	if !out.Seeded {
		t.Fatalf("fixture seed failed: %s — %s", out.Reason, out.Detail)
	}
}

func TestVerifyOrDiscardSeed_RejectsAndDiscardsATamperedSeed(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	seedFakeStateDir(t, parentSD, childSD, "svc")

	// Tamper: the graph on disk is no longer the graph the stamp covers.
	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(1)), []byte("TAMPERED-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	reason, ok := verifyOrDiscardSeed(childSD)
	if ok {
		t.Fatal("verifyOrDiscardSeed accepted a seed whose content does not match its stamp")
	}
	if reason != string(daemon.SeedFallbackStampMismatch) {
		t.Errorf("reason=%q want %q", reason, daemon.SeedFallbackStampMismatch)
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind != graph.GraphAbsent {
		t.Error("the untrusted seed is still resolvable — a full index would merge into it")
	}
}

func TestVerifyOrDiscardSeed_AcceptsAnIntactSeedAndConsumesTheStamp(t *testing.T) {
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	seedFakeStateDir(t, parentSD, childSD, "svc")

	reason, ok := verifyOrDiscardSeed(childSD)
	if !ok || reason != "" {
		t.Fatalf("intact seed rejected: reason=%q ok=%v", reason, ok)
	}
	if _, err := daemon.ReadSeedStamp(childSD); err == nil {
		t.Error("stamp not consumed — the next pass would verify it against this pass's own newer generation")
	}
	if desc, _ := graph.CurrentGraphDescriptor(childSD); desc.Kind == graph.GraphAbsent {
		t.Error("the verified seed was removed")
	}
}

func TestVerifyOrDiscardSeed_KeepsTheChildsOwnGraphWhenTheStampIsMerelyStale(t *testing.T) {
	// The GRAFEL_INCREMENTAL_REINDEX=0 sequence: a full index ran over the
	// seeded dir and wrote a newer generation. The stale stamp must not cause
	// that graph to be discarded.
	root := t.TempDir()
	parentSD := filepath.Join(root, "parent-state")
	childSD := filepath.Join(root, "child-state")
	seedFakeStateDir(t, parentSD, childSD, "svc")

	if err := os.WriteFile(filepath.Join(childSD, graph.GenFileName(2)), []byte("CHILD-OWN-GRAPH"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(childSD, graph.GenFileName(2)); err != nil {
		t.Fatal(err)
	}

	reason, ok := verifyOrDiscardSeed(childSD)
	if !ok {
		t.Fatalf("a stale stamp over the child's own newer graph was treated as untrusted: reason=%q", reason)
	}
	desc, _ := graph.CurrentGraphDescriptor(childSD)
	if desc.Kind == graph.GraphAbsent {
		t.Fatal("DATA LOSS: the child's own graph was made unresolvable by a stale stamp")
	}
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "CHILD-OWN-GRAPH" {
		t.Errorf("resolved body=%q want CHILD-OWN-GRAPH", body)
	}
	if _, err := daemon.ReadSeedStamp(childSD); err == nil {
		t.Error("the stale stamp was not cleared — it would re-trigger on every subsequent pass")
	}
}

// Drives the scheduler's incremental callback itself, so deleting the
// verification guard from it fails here rather than silently.
func TestDaemonSchedulerIncremental_RefusesToConsumeATamperedSeed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package p\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.ResolveIncrementalStateDir(repo, "")
	seedFakeStateDir(t, filepath.Join(root, "parent-state"), stateDir, "svc")
	// The seeded graph bytes are not a real graph, but that is irrelevant: the
	// verification guard must reject this before anything tries to load it.
	if err := os.WriteFile(filepath.Join(stateDir, graph.GenFileName(1)), []byte("TAMPERED-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := daemonSchedulerIncremental(context.Background(), repo, "", nil)

	if res.Done {
		t.Fatal("the incremental callback consumed a seed that failed provenance verification")
	}
	if !strings.HasPrefix(res.FallbackReason, "seed_") {
		t.Fatalf("FallbackReason=%q want a seed_* reason naming which check failed", res.FallbackReason)
	}
	if !strings.Contains(res.FallbackReason, string(daemon.SeedFallbackStampMismatch)) {
		t.Errorf("FallbackReason=%q does not name the failing check", res.FallbackReason)
	}
}

// resolveIndexRepoTag is the seam daemonSchedulerIndex reads. Forcing it to ""
// is the mutant that silently un-pins a worktree's entity ids.
func TestResolveIndexRepoTag_ReturnsThePinAndNothingWhenUnpinned(t *testing.T) {
	dir := t.TempDir()
	if got := resolveIndexRepoTag(dir); got != "" {
		t.Errorf("unpinned state dir returned %q — every ordinary repo would be re-tagged", got)
	}
	if err := daemon.WriteRepoTagPin(dir, "myservice"); err != nil {
		t.Fatal(err)
	}
	if got := resolveIndexRepoTag(dir); got != "myservice" {
		t.Fatalf("resolveIndexRepoTag=%q want myservice", got)
	}
}

// End-to-end on the FULL-index path: a pinned worktree state dir must produce
// entity ids computed from the PIN, not from the worktree directory's name.
// This runs a real (small) in-process index, because that is the only way to
// prove the tag reached graph.EntityID.
func TestDaemonSchedulerIndex_FullIndexHonoursTheRepoTagPin(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))
	// Force the in-process index path so this stays one fast process. The
	// toggle is an atomic, not an env read at call time.
	prev := sched.SetSubprocessIndexEnabled(false)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prev) })

	repo := filepath.Join(root, "wt-agent-pinned")
	if err := os.MkdirAll(filepath.Join(repo, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module pinned\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "package svc\n\ntype Widget struct{ Name string }\n\nfunc (w *Widget) Render() string { return w.Name }\n"
	if err := os.WriteFile(filepath.Join(repo, "svc", "widget.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	seedGitRun(t, repo, "init", "-q", "-b", "feat/pinned")
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "fixture")

	const pinnedTag = "parent-slug-not-the-dir-name"
	stateDir := daemon.ResolveIncrementalStateDir(repo, "")
	if err := daemon.WriteRepoTagPin(stateDir, pinnedTag); err != nil {
		t.Fatal(err)
	}
	defaultTag := filepath.Base(repo)
	if defaultTag == pinnedTag {
		t.Fatal("fixture is inert: the pin must differ from the directory basename")
	}

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil || doc == nil {
		t.Fatalf("load graph: %v", err)
	}
	// Assert over EVERY source-bearing entity rather than one hand-picked
	// name, so the test cannot rot on a naming change in the extractor.
	checked := 0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.SourceFile == "" || e.ID == "" {
			continue // synthetic (ext:*) entities have no repo-tagged id
		}
		wantPinned := graph.EntityID(pinnedTag, e.Kind, e.Name, e.SourceFile)
		gotDefault := graph.EntityID(defaultTag, e.Kind, e.Name, e.SourceFile)
		if wantPinned == gotDefault {
			t.Fatalf("fixture is inert: pinned and default ids collide for %q", e.Name)
		}
		if e.ID == gotDefault {
			t.Fatalf("entity %q (%s) id=%s was computed from the DIRECTORY BASENAME %q — the repo-tag pin was ignored on the full-index path (want %s from pin %q)",
				e.Name, e.Kind, e.ID, defaultTag, wantPinned, pinnedTag)
		}
		if e.ID == wantPinned {
			checked++
		}
	}
	if checked == 0 {
		t.Fatalf("no source-bearing entity carried a pin-derived id (graph had %d entities) — the fixture proves nothing", len(doc.Entities))
	}
	t.Logf("%d source-bearing entities carry ids derived from the pinned tag %q", checked, pinnedTag)
}
