package watch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// sinkRecorder counts EventSink invocations. The sink runs on a goroutine the
// test does not own (see Watcher.RescanRepo), so every field is mutex-guarded
// and assertions poll rather than read once.
type sinkRecorder struct {
	mu    sync.Mutex
	calls []sinkCall
	// gate, when non-nil, is received from before a call is recorded. It lets
	// a test hold the sink open and observe whether Resume waits for it.
	gate chan struct{}
}

type sinkCall struct {
	repo string
	bulk bool
}

func (r *sinkRecorder) sink(repo string, bulk bool) {
	if r.gate != nil {
		<-r.gate
	}
	r.mu.Lock()
	r.calls = append(r.calls, sinkCall{repo: repo, bulk: bulk})
	r.mu.Unlock()
}

func (r *sinkRecorder) snapshot() []sinkCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sinkCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *sinkRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// waitForCount polls until the recorder has reached want calls, or fails.
func (r *sinkRecorder) waitForCount(t *testing.T, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s: want at least %d sink call(s), got %d after 5s: %+v",
		what, want, r.count(), r.snapshot())
}

// settleAndAssertCount waits long enough for a stray asynchronous sink call to
// land, then asserts the total is exactly want.
func (r *sinkRecorder) settleAndAssertCount(t *testing.T, want int, what string) {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	if got := r.count(); got != want {
		t.Fatalf("%s: want exactly %d sink call(s), got %d: %+v",
			what, want, got, r.snapshot())
	}
}

// newRecordingWatcher builds a Watcher whose sink is rec. Debounce and
// heartbeat are pushed out of the test's lifetime so the ONLY thing that can
// call the sink is an explicit rescan — no fsnotify event and no heartbeat
// restart can contribute a call.
func newRecordingWatcher(t *testing.T, rec *sinkRecorder) *Watcher {
	t.Helper()
	w, err := NewWatcherConfig(Config{
		Debounce:          time.Hour,
		HeartbeatInterval: time.Hour,
	}, rec.sink, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)
	return w
}

// TestResume_CatchUpRescanAfterUnsubscribedWindow drives the #6269 lifecycle
// against ONE manager/watcher pair: subscribe, unsubscribe, edit a source file
// while nothing is watching, then resume. What it pins is that the resume at
// the end of that sequence asks the sink to reconcile the repo.
//
// Scope, stated plainly: the write to main.go is scene-setting, not an
// assertion. sinkRecorder observes only (repo, bulk), and the rescan is
// unconditional, so deleting the write would leave this test green. Nothing
// here — or anywhere in this package — verifies end-to-end that an edit made
// during a cold window actually reaches the graph; that would need the real
// scheduler and indexer, not an EventSink double.
func TestResume_CatchUpRescanAfterUnsubscribedWindow(t *testing.T) {
	rec := &sinkRecorder{}
	w := newRecordingWatcher(t, rec)
	m := NewDefaultManager(w, nil)

	repo := t.TempDir()
	srcPath := filepath.Join(repo, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// Subscribed: the watcher is live on this repo.
	m.SubscribeGroup("g1", []string{repo})
	if len(w.Repos()) != 1 {
		t.Fatalf("prereq: want 1 watched repo after SubscribeGroup, got %d", len(w.Repos()))
	}
	baseline := rec.count()

	// Unsubscribed window opens (idle eviction / never-subscribed-at-boot).
	m.Pause(repo, "")
	if len(w.Repos()) != 0 {
		t.Fatalf("prereq: want 0 watched repos after Pause, got %d", len(w.Repos()))
	}

	// The edit nobody is listening for.
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("edit during unsubscribed window: %v", err)
	}

	// Re-query: the tier cold-wake path calls Resume.
	m.Resume(repo, "")
	if len(w.Repos()) != 1 {
		t.Fatalf("prereq: want 1 watched repo after Resume, got %d", len(w.Repos()))
	}

	rec.waitForCount(t, baseline+1, "Resume after unsubscribed window")
	calls := rec.snapshot()
	got := calls[len(calls)-1]

	absRepo, err := filepath.Abs(repo)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if got.repo != absRepo {
		t.Errorf("catch-up rescan repo: got %q, want %q", got.repo, absRepo)
	}
	if !got.bulk {
		t.Errorf("catch-up rescan: want bulk=true (full reconciliation), got bulk=false")
	}
}

// TestResume_FailedAddRepoDoesNotRescanAndStaysRetryable covers the branch no
// t.TempDir-based test can reach: the one where AddRepo REFUSES.
//
// Two things are pinned in one sequence, because they are one behaviour:
//
//  1. A Resume whose AddRepo failed subscribed nothing, so it must not claim a
//     catch-up. Hoisting the rescan out of the success branch breaks this.
//  2. The failure must leave the slot retryable. Resume increments refCounts
//     and clears paused BEFORE calling AddRepo; if the failure path does not
//     put both back, refCounts sits at 1 with no entry in the watcher, every
//     later Resume reads wasZero == false, and the repo is permanently
//     unwatched while ActiveCount/IsPaused report it as watched.
//
// The injected failure is walk.IsProtectedPath, which AddRepo checks at the
// top — before it registers the repo or reserves any descriptor. It fires on
// every platform here: the media-library-bundle suffix check runs ahead of the
// darwin-only home-directory checks. The repo path is a SYMLINK so the path
// string can stay constant while the thing it resolves to changes, which is
// what makes the retry observable at all (IsProtectedPath resolves symlinks
// before matching the basename).
func TestResume_FailedAddRepoDoesNotRescanAndStaysRetryable(t *testing.T) {
	rec := &sinkRecorder{}
	w := newRecordingWatcher(t, rec)
	m := NewDefaultManager(w, nil)

	parent := t.TempDir()
	bundle := filepath.Join(parent, "media.photoslibrary")
	if err := os.Mkdir(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	repo := filepath.Join(parent, "repo")
	if err := os.Symlink(bundle, repo); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	m.Register(repo, "main")

	// First Resume: AddRepo refuses the protected path.
	m.Resume(repo, "main")
	if n := len(w.Repos()); n != 0 {
		t.Fatalf("prereq: AddRepo should have refused the protected path, but the "+
			"watcher holds %d repo(s) — the failure injection is not working", n)
	}
	rec.settleAndAssertCount(t, 0, "Resume whose AddRepo failed")

	// The bookkeeping must have been rolled back, or no later Resume can retry.
	if !m.IsPaused(repo, "main") {
		t.Error("after a failed AddRepo: want the slot paused again, got active — " +
			"a later Resume will take the wasZero==false path and never retry")
	}
	if got := m.ActiveCount(); got != 0 {
		t.Errorf("after a failed AddRepo: want ActiveCount=0, got %d — the repo is "+
			"reported as watched while receiving no events", got)
	}

	// Point the same path at a directory AddRepo will accept.
	if err := os.Remove(repo); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("mkdir real repo: %v", err)
	}

	// Second Resume: must actually retry AddRepo, and now rescan.
	m.Resume(repo, "main")
	if n := len(w.Repos()); n != 1 {
		t.Fatalf("after the refusal was cleared: want 1 watched repo, got %d — "+
			"Resume never retried AddRepo", n)
	}
	rec.waitForCount(t, 1, "retry after a failed AddRepo")
}

// TestResume_CatchUpRescanUsesAbsoluteRepoPath pins that the sink is handed the
// SAME key AddRepo registered. AddRepo absolutises repoPath before storing it in
// w.repos, and the production sink forwards its argument straight to
// Scheduler.Enqueue, which dedupes and tracks in-flight work by repo-path
// string — so a relative path would be a second, unrecognised identity for a
// repo that is already known.
//
// t.TempDir is already absolute, so the repo path has to be made relative to
// the working directory for this to test anything.
func TestResume_CatchUpRescanUsesAbsoluteRepoPath(t *testing.T) {
	rec := &sinkRecorder{}
	w := newRecordingWatcher(t, rec)
	m := NewDefaultManager(w, nil)

	parent := t.TempDir()
	absRepo := filepath.Join(parent, "repo")
	if err := os.Mkdir(absRepo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(parent)

	// Resume with the RELATIVE path, as a caller holding an unnormalised
	// slot key would.
	m.Resume("repo", "main")

	rec.waitForCount(t, 1, "Resume with a relative repo path")
	got := rec.snapshot()[0]
	if got.repo == "repo" {
		t.Fatalf("catch-up rescan forwarded the relative path %q; want it absolutised", got.repo)
	}
	// filepath.Abs resolves against the working directory but does not resolve
	// symlinks, and neither does AddRepo — compare against the same form.
	wantAbs, err := filepath.Abs("repo")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if got.repo != wantAbs {
		t.Errorf("catch-up rescan repo: got %q, want %q", got.repo, wantAbs)
	}
}

// TestResume_RefcountBumpDoesNotRescan pins the other half of the rule: a
// Resume that finds the repo ALREADY subscribed has missed nothing, so it must
// not enqueue a reindex. Two refs share one fsnotify subscription (the
// subscription is per repo path, not per ref — see DefaultManager.refCounts),
// so resuming the second ref is a refcount bump with no AddRepo.
func TestResume_RefcountBumpDoesNotRescan(t *testing.T) {
	rec := &sinkRecorder{}
	w := newRecordingWatcher(t, rec)
	m := NewDefaultManager(w, nil)

	repo := t.TempDir()

	m.Register(repo, "main")
	m.Register(repo, "feature")

	// First ref: this one DOES establish the subscription.
	m.Resume(repo, "main")
	if len(w.Repos()) != 1 {
		t.Fatalf("prereq: want 1 watched repo after first Resume, got %d", len(w.Repos()))
	}
	rec.waitForCount(t, 1, "first Resume establishes the subscription")
	afterFirst := rec.count()

	// Second ref on the same repo: refcount 1 → 2, no AddRepo, nothing was
	// missed because the subscription never lapsed.
	m.Resume(repo, "feature")
	if m.ActiveCount() != 2 {
		t.Fatalf("prereq: want ActiveCount=2 after second Resume, got %d", m.ActiveCount())
	}
	rec.settleAndAssertCount(t, afterFirst, "refcount-bump Resume")

	// An idempotent Resume of an already-active ref must not rescan either.
	m.Resume(repo, "main")
	rec.settleAndAssertCount(t, afterFirst, "idempotent Resume of active ref")
}

// TestResume_CatchUpRescanDoesNotBlockCaller pins that the rescan is
// asynchronous. Resume is called synchronously from the MCP request goroutine
// (Cache.GetForRepoRef → fireAccessHook → tier.Manager.Touch → wh.Resume, all
// direct calls), so a Resume that waited on the sink would stall a user query
// behind a reindex enqueue.
//
// The sink is held open; Resume must still return promptly.
func TestResume_CatchUpRescanDoesNotBlockCaller(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(gate) }) }
	// Safety valve: if the implementation is synchronous, Resume unblocks here
	// rather than deadlocking the test, and the elapsed assertion reports it.
	timer := time.AfterFunc(3*time.Second, unblock)
	defer timer.Stop()
	defer unblock()

	rec := &sinkRecorder{gate: gate}
	w := newRecordingWatcher(t, rec)
	m := NewDefaultManager(w, nil)

	repo := t.TempDir()
	m.Register(repo, "main")

	start := time.Now()
	m.Resume(repo, "main")
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Fatalf("Resume blocked on the sink for %s — the catch-up rescan must not "+
			"run on the caller's goroutine", elapsed)
	}

	unblock()
	rec.waitForCount(t, 1, "rescan still fires after the sink is released")
}
