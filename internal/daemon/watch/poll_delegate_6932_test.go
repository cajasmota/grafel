package watch

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type recordingDelegate struct {
	mu      sync.Mutex
	added   []string
	removed []string
	err     error
}

func (d *recordingDelegate) AddRepo(p string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	d.added = append(d.added, p)
	return nil
}

func (d *recordingDelegate) RemoveRepo(p string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.removed = append(d.removed, p)
}

func (d *recordingDelegate) snapshot() ([]string, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.added...), append([]string(nil), d.removed...)
}

func delegateRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a", "b", "a/c"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "x.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The whole point of poll mode is that ZERO fs watch descriptors are taken.
// With a delegate installed, AddRepo must route the repo to the delegate and
// subscribe nothing — while still reporting the repo as watched, because it IS
// watched, by the poller.
func TestWatcher_PollDelegate_TakesNoSubscriptions(t *testing.T) {
	repo := delegateRepo(t)
	d := &recordingDelegate{}
	w, err := NewWatcherConfig(Config{Debounce: time.Hour, Delegate: d}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()

	n, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n != 0 {
		t.Fatalf("delegate mode subscribed %d directories; must be 0", n)
	}
	_, dirs, _, _, _ := w.Stats()
	if dirs != 0 {
		t.Fatalf("delegate mode holds %d fsnotify directory subscriptions; must be 0", dirs)
	}
	added, _ := d.snapshot()
	abs, _ := filepath.Abs(repo)
	if len(added) != 1 || added[0] != abs {
		t.Fatalf("delegate did not receive the repo: %v", added)
	}
	if repos := w.Repos(); len(repos) != 1 || repos[0] != abs {
		t.Fatalf("watcher does not report the delegated repo as watched: %v", repos)
	}

	// Idempotent: a second AddRepo must not re-register with the delegate.
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("second AddRepo: %v", err)
	}
	added, _ = d.snapshot()
	if len(added) != 1 {
		t.Fatalf("delegate AddRepo not idempotent: %v", added)
	}

	w.RemoveRepo(repo)
	_, removed := d.snapshot()
	if len(removed) != 1 || removed[0] != abs {
		t.Fatalf("delegate did not receive the removal: %v", removed)
	}
	if repos := w.Repos(); len(repos) != 0 {
		t.Fatalf("repo still reported after RemoveRepo: %v", repos)
	}
}

// A delegate that refuses a repo must leave the watcher reporting NOTHING for
// it. Reporting it as watched while nothing observes it is exactly the silent
// half-failure #6932 is about.
func TestWatcher_PollDelegate_FailureIsNotReportedAsWatched(t *testing.T) {
	repo := delegateRepo(t)
	d := &recordingDelegate{err: errors.New("nope")}
	w, err := NewWatcherConfig(Config{Debounce: time.Hour, Delegate: d}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()

	if _, err := w.AddRepo(repo); err == nil {
		t.Fatal("AddRepo must surface the delegate's error")
	}
	if repos := w.Repos(); len(repos) != 0 {
		t.Fatalf("a repo the delegate refused is reported as watched: %v", repos)
	}
}

// Without a delegate the watcher behaves exactly as before: it subscribes.
// This is the control that stops the delegate branch from passing vacuously.
func TestWatcher_NoDelegate_StillSubscribes(t *testing.T) {
	repo := delegateRepo(t)
	w, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()

	n, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n == 0 {
		t.Fatal("fsnotify mode subscribed 0 directories — the control is vacuous")
	}
}

// backendWatchList asks FSNOTIFY what it subscribed, not grafel's own counter.
// A counter a component keeps about itself is a diagnostic signature; the
// library's record of what it handed the kernel is the artefact.
func backendWatchList(w *Watcher) int {
	w.mu.Lock()
	fs := w.fs
	w.mu.Unlock()
	if fs == nil {
		return 0
	}
	return len(fs.WatchList())
}

// #6932 review, blocker 2: restartBackend is a SECOND subscribe path. It walks
// w.repos and calls subscribeRepo directly, never AddRepo — and poll mode
// deliberately populates w.repos so Repos()/Stats() stay truthful. Without the
// guard, one backend restart converts a poll-mode daemon into a full fsnotify
// subscriber: ~976 descriptors per worktree, in exactly the container whose
// inotify pool cannot be raised, with the poller never told.
func TestWatcher_PollDelegate_BackendRestartResubscribesNothing(t *testing.T) {
	repo := delegateRepo(t)
	d := &recordingDelegate{}
	w, err := NewWatcherConfig(Config{Debounce: time.Hour, Delegate: d}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()

	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n := backendWatchList(w); n != 0 {
		t.Fatalf("fsnotify backend already holds %d paths before the restart", n)
	}
	// Premise: the repo IS in w.repos, so the re-subscribe loop has something
	// to iterate. Otherwise this test passes for the wrong reason.
	if repos := w.Repos(); len(repos) != 1 {
		t.Fatalf("fixture premise broken: w.repos holds %v, so restartBackend would iterate nothing", repos)
	}

	if !w.restartBackend() {
		t.Fatal("restartBackend reported a stop was in flight")
	}

	if n := backendWatchList(w); n != 0 {
		t.Fatalf("a backend restart subscribed %d paths in poll mode; must be 0", n)
	}
	if _, dirs, _, _, _ := w.Stats(); dirs != 0 {
		t.Fatalf("a backend restart left %d directory subscriptions in poll mode", dirs)
	}
}

// The control. Without a delegate, restartBackend DOES re-subscribe — so the
// assertion above measures the guard, not a watcher that cannot subscribe.
func TestWatcher_NoDelegate_BackendRestartResubscribes(t *testing.T) {
	repo := delegateRepo(t)
	w, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()

	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	before := backendWatchList(w)
	if before == 0 {
		t.Fatal("fsnotify mode subscribed nothing — the control is vacuous")
	}
	if !w.restartBackend() {
		t.Fatal("restartBackend reported a stop was in flight")
	}
	if after := backendWatchList(w); after != before {
		t.Fatalf("backend re-subscribed %d paths after a restart, want %d", after, before)
	}
}

// The seam's own claim, at the backend rather than at grafel's counter:
// AddRepo in delegate mode hands fsnotify nothing at all.
func TestWatcher_PollDelegate_BackendWatchListIsZero(t *testing.T) {
	repo := delegateRepo(t)
	d := &recordingDelegate{}
	w, err := NewWatcherConfig(Config{Debounce: time.Hour, Delegate: d}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n := backendWatchList(w); n != 0 {
		t.Fatalf("fsnotify backend WatchList = %d in poll mode; must be 0", n)
	}

	ctrl, err := NewWatcherConfig(Config{Debounce: time.Hour}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer ctrl.Stop()
	if _, err := ctrl.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n := backendWatchList(ctrl); n == 0 {
		t.Fatal("fsnotify backend WatchList = 0 in fsnotify mode — the comparison is vacuous")
	}
}
