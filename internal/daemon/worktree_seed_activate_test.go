package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// These tests drive seedWorktreeOnActivate — the function the daemon's
// worktree OnActivate hook calls — and assert on its OBSERVABLE output: what
// lands on disk and what it logs. They do not inspect source text, so a
// mutation that deletes the call, renames an import, or swallows the fallback
// reason is caught by the on-disk / log assertions rather than by a grep.

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), &buf
}

// logHas reports whether any logged record carries key=want.
func logHas(t *testing.T, buf *bytes.Buffer, key, want string) bool {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if v, ok := rec[key]; ok {
			if s, ok := v.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func seedableParent(t *testing.T, stateDir string, gen uint64, body string) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := graph.GenFileName(gen)
	if err := os.WriteFile(filepath.Join(stateDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(stateDir, name); err != nil {
		t.Fatal(err)
	}
	m := diff.LoadManifest(stateDir)
	m.Files["a.go"] = diff.FileEntry{SHA256: "aa", Size: 1}
	if err := diff.SaveManifest(stateDir, "/x", m); err != nil {
		t.Fatal(err)
	}
}

// WIRING GUARD. Everything below this comment tests seedWorktreeOnActivate
// directly. This one drives the handler the engine plane actually installs as
// worktree.Watcher.OnActivate, so that deleting the seeding call from the
// activation path — the mutant that removes the entire feature and leaves
// every suite green — fails here.
//
// It asserts observable behaviour only: a seeded graph on disk, and that the
// activation's pre-existing duties (watch + enqueue) still happen. No source
// scanning: an import alias or a call moved behind a comment would defeat that,
// and two such guards were written this week and both fell to trivial mutants.
func TestNewWorktreeActivateHandler_SeedsTheWorktreeAndStillWatchesAndEnqueues(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	parentPath := filepath.Join(root, "parent-repo")
	childPath := filepath.Join(root, "wt", "agent-wired")
	for _, d := range []string{parentPath, childPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seedableParent(t, StateDirForRepo(parentPath), 4, "PARENT-GRAPH-VIA-HANDLER")

	var watched, enqueued []string
	logger, buf := captureLogger()
	handler := newWorktreeActivateHandler(worktreeActivateDeps{
		Logger:      logger,
		Parents:     nil,
		AddRepo:     func(p string) error { watched = append(watched, p); return nil },
		Enqueue:     func(p string) { enqueued = append(enqueued, p) },
		ActivateSem: make(chan struct{}, 1),
	})

	handler(&worktree.WorktreeChild{
		ParentSlug: "myservice",
		ParentPath: parentPath,
		Path:       childPath,
		Branch:     "feat/wired",
	})

	childSD := StateDirForRepoRef(childPath, "feat/wired")
	desc, _ := graph.CurrentGraphDescriptor(childSD)
	if desc.Kind == graph.GraphAbsent {
		t.Fatalf("activation did not seed the worktree — the graph-seeding payload is not wired into OnActivate.\nlog:\n%s", buf.String())
	}
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "PARENT-GRAPH-VIA-HANDLER" {
		t.Errorf("seeded body=%q want PARENT-GRAPH-VIA-HANDLER", body)
	}
	if got := ReadRepoTagPin(childSD); got != "myservice" {
		t.Errorf("repo-tag pin=%q want myservice", got)
	}
	// The activation's original duties must survive the refactor.
	if len(watched) != 1 || watched[0] != childPath {
		t.Errorf("AddRepo calls=%v want exactly [%s]", watched, childPath)
	}
	if len(enqueued) != 1 || enqueued[0] != childPath {
		t.Errorf("Enqueue calls=%v want exactly [%s]", enqueued, childPath)
	}
}

// The handler must still watch + enqueue when seeding cannot happen: a
// worktree whose parent is unindexed must not be left unwatched.
func TestNewWorktreeActivateHandler_WatchesAndEnqueuesEvenWhenSeedingIsSkipped(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	parentPath := filepath.Join(root, "parent-repo") // never indexed
	childPath := filepath.Join(root, "wt", "agent-noseed")
	for _, d := range []string{parentPath, childPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var watched, enqueued []string
	logger, buf := captureLogger()
	handler := newWorktreeActivateHandler(worktreeActivateDeps{
		Logger:  logger,
		AddRepo: func(p string) error { watched = append(watched, p); return nil },
		Enqueue: func(p string) { enqueued = append(enqueued, p) },
	})
	handler(&worktree.WorktreeChild{
		ParentSlug: "myservice", ParentPath: parentPath,
		Path: childPath, Branch: "feat/noseed",
	})

	if len(watched) != 1 || len(enqueued) != 1 {
		t.Errorf("watched=%v enqueued=%v — activation duties were skipped on the no-seed path", watched, enqueued)
	}
	if !logHas(t, buf, "reason", string(SeedFallbackParentNotIndexed)) {
		t.Errorf("fallback reason not logged; got:\n%s", buf.String())
	}
}

func TestSeedWorktreeOnActivate_SeedsFromTheParentsActualRef(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	parentPath := filepath.Join(root, "parent-repo")
	childPath := filepath.Join(root, "wt", "agent-a")
	if err := os.MkdirAll(parentPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(childPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// The parent's ACTUAL ref, as StateDirForRepo resolves it for a
	// non-git dir, is the "_unknown" sentinel — deliberately NOT "main". A
	// hardcoded-"main" implementation (the #3652 bug) reads an empty dir here
	// and cannot seed.
	parentSD := StateDirForRepo(parentPath)
	if parentSD == StateDirForRepoRef(parentPath, "main") {
		t.Fatal("fixture is inert: the parent's actual ref dir must differ from the 'main' dir")
	}
	seedableParent(t, parentSD, 5, "PARENT-GRAPH")
	// Populate the "main" dir with DIFFERENT content, so an implementation
	// that reads "main" would succeed but with the wrong bytes — the failure
	// this asserts against is wrong content, not merely absence.
	seedableParent(t, StateDirForRepoRef(parentPath, "main"), 5, "WRONG-MAIN-GRAPH")

	child := &worktree.WorktreeChild{
		ParentSlug: "myservice",
		ParentPath: parentPath,
		Path:       childPath,
		Branch:     "feat/x",
	}
	logger, buf := captureLogger()

	out := seedWorktreeOnActivate(logger, child, nil)

	if !out.Seeded {
		t.Fatalf("Seeded=false reason=%q detail=%q", out.Reason, out.Detail)
	}
	childSD := StateDirForRepoRef(childPath, "feat/x")
	desc, _ := graph.CurrentGraphDescriptor(childSD)
	body, _ := os.ReadFile(desc.Path)
	if string(body) != "PARENT-GRAPH" {
		t.Errorf("seeded body=%q want PARENT-GRAPH (got the 'main' dir's content → #3652 regression)", body)
	}
	if got := ReadRepoTagPin(childSD); got != "myservice" {
		t.Errorf("repo-tag pin=%q want myservice", got)
	}
	if !logHas(t, buf, "msg", "worktree: seeded graph from parent ref") {
		t.Errorf("no success log line; got:\n%s", buf.String())
	}
}

func TestSeedWorktreeOnActivate_LogsANamedReasonWhenItCannotSeed(t *testing.T) {
	// A silent fallback is the rejected design: a seed that systematically
	// never fires must be visible in the log.
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	parentPath := filepath.Join(root, "parent-repo")
	childPath := filepath.Join(root, "wt", "agent-b")
	for _, d := range []string{parentPath, childPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Parent never indexed.
	child := &worktree.WorktreeChild{
		ParentSlug: "myservice",
		ParentPath: parentPath,
		Path:       childPath,
		Branch:     "feat/y",
	}
	logger, buf := captureLogger()

	out := seedWorktreeOnActivate(logger, child, nil)

	if out.Seeded {
		t.Fatal("Seeded=true with an unindexed parent")
	}
	if out.Reason != SeedFallbackParentNotIndexed {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackParentNotIndexed)
	}
	if !logHas(t, buf, "reason", string(SeedFallbackParentNotIndexed)) {
		t.Errorf("fallback reason not logged; got:\n%s", buf.String())
	}
	// The repo-tag pin must still be written: a FULL index of this worktree
	// must use the parent's tag too, or a later seed can never match it.
	if got := ReadRepoTagPin(StateDirForRepoRef(childPath, "feat/y")); got != "myservice" {
		t.Errorf("repo-tag pin=%q want myservice even on the fallback path", got)
	}
}

func TestSeedWorktreeOnActivate_LogsAReasonWhenNoParentSlugIsKnown(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	parentPath := filepath.Join(root, "parent-repo")
	childPath := filepath.Join(root, "wt", "agent-c")
	for _, d := range []string{parentPath, childPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seedableParent(t, StateDirForRepo(parentPath), 1, "G")

	child := &worktree.WorktreeChild{
		ParentPath: parentPath, // ParentSlug deliberately empty
		Path:       childPath,
		Branch:     "feat/z",
	}
	logger, buf := captureLogger()

	out := seedWorktreeOnActivate(logger, child, nil)
	if out.Seeded {
		t.Fatal("Seeded=true with no parent slug to pin the repo tag to")
	}
	if out.Reason != SeedFallbackRepoTagUnresolved {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackRepoTagUnresolved)
	}
	if !logHas(t, buf, "reason", string(SeedFallbackRepoTagUnresolved)) {
		t.Errorf("reason not logged; got:\n%s", buf.String())
	}
}

func TestSeedWorktreeOnActivate_ReportsUnresolvableParent(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	childPath := filepath.Join(root, "wt", "orphan")
	if err := os.MkdirAll(childPath, 0o755); err != nil {
		t.Fatal(err)
	}
	child := &worktree.WorktreeChild{
		ParentSlug: "myservice",
		Path:       childPath,
		Branch:     "feat/q",
	} // no ParentPath, no parents provider → nothing to derive from
	logger, buf := captureLogger()

	out := seedWorktreeOnActivate(logger, child, nil)
	if out.Seeded {
		t.Fatal("Seeded=true with no resolvable parent")
	}
	if out.Reason != SeedFallbackParentPathUnresolved {
		t.Errorf("Reason=%q want %q", out.Reason, SeedFallbackParentPathUnresolved)
	}
	if !logHas(t, buf, "reason", string(SeedFallbackParentPathUnresolved)) {
		t.Errorf("reason not logged; got:\n%s", buf.String())
	}
}
