package worktree

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeLedger is a descriptor ledger with a hard capacity, so the accounting
// assertions below never depend on the host's real RLIMIT_NOFILE.
type fakeLedger struct {
	mu       sync.Mutex
	capacity int // -1 == unlimited
	used     int
	reserves int
}

func (l *fakeLedger) Reserve(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reserves++
	if l.capacity >= 0 && l.used+n > l.capacity {
		return false
	}
	l.used += n
	return true
}

func (l *fakeLedger) Release(n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.used -= n
	if l.used < 0 {
		l.used = 0
	}
}

// Cost is the macOS kqueue arithmetic (one descriptor for the directory plus
// one for every entry in it), asserted on every platform so the test pins the
// charge shape rather than the host's platform.
func (l *fakeLedger) Cost(entries int) int { return 1 + entries }

func (l *fakeLedger) snapshot() (used, reserves int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.used, l.reserves
}

// gitDirsParent creates a directory that looks enough like a main checkout for
// gitWorktreesDir: a real .git directory, plus n existing entries under
// .git/worktrees (what `git worktree add` leaves behind).
func gitDirsParent(t *testing.T, worktrees int) ParentRepo {
	t.Helper()
	root := t.TempDir()
	wt := filepath.Join(root, ".git", "worktrees")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < worktrees; i++ {
		if err := os.MkdirAll(filepath.Join(wt, "wt"+string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return ParentRepo{GroupName: "g", Slug: "s", Path: root}
}

func testWatcher(t *testing.T, l *fakeLedger, parents ...ParentRepo) *Watcher {
	t.Helper()
	w := NewWatcher(NewStore(filepath.Join(t.TempDir(), "worktrees.json")),
		func() []ParentRepo { return parents },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.fdb = fdLedger{reserve: l.Reserve, release: l.Release, cost: l.Cost}
	return w
}

// TestGitDirsWatch_ChargesDescriptorLedger is #6233: the .git/worktrees watch
// draws from the same per-process descriptor pool as the subscription watcher,
// so it must charge the ledger for every directory it watches, priced by the
// platform cost model rather than counted as one.
func TestGitDirsWatch_ChargesDescriptorLedger(t *testing.T) {
	l := &fakeLedger{capacity: -1}
	w := testWatcher(t, l, gitDirsParent(t, 3), gitDirsParent(t, 0))

	stop := w.startGitDirsWatch(context.Background())
	used, _ := l.snapshot()
	stop()

	// (1 dir + 3 entries) + (1 dir + 0 entries)
	if want := 5; used != want {
		t.Fatalf("ledger charged %d descriptors for the .git/worktrees watches, want %d", used, want)
	}
}

// TestGitDirsWatch_ReleasesDescriptorsOnStop proves the charge is not a leak:
// closing the watcher returns every descriptor, so a restarted watcher can
// re-reserve them.
func TestGitDirsWatch_ReleasesDescriptorsOnStop(t *testing.T) {
	l := &fakeLedger{capacity: -1}
	w := testWatcher(t, l, gitDirsParent(t, 2))

	stop := w.startGitDirsWatch(context.Background())
	if used, _ := l.snapshot(); used == 0 {
		t.Fatalf("nothing was charged, so this test cannot prove a release")
	}
	stop()

	if used, _ := l.snapshot(); used != 0 {
		t.Fatalf("ledger still holds %d descriptors after stop, want 0", used)
	}
}

// TestGitDirsWatch_ReleasesReservationWhenAddFails covers the error path: a
// reservation made for a watch that fsnotify then refuses must be given back,
// or a daemon on a repo it cannot open bleeds budget away from subscriptions.
func TestGitDirsWatch_ReleasesReservationWhenAddFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0000 directory is still openable")
	}
	p := gitDirsParent(t, 0)
	dir := filepath.Join(p.Path, ".git", "worktrees")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	l := &fakeLedger{capacity: -1}
	w := testWatcher(t, l, p)

	stop := w.startGitDirsWatch(context.Background())
	used, reserves := l.snapshot()
	stop()

	if reserves == 0 {
		t.Fatalf("no reservation was attempted, so the release path is untested")
	}
	if used != 0 {
		t.Fatalf("ledger still holds %d descriptors after a failed fsnotify Add, want 0", used)
	}
}

// TestGitDirsWatch_RefusedDirIsNotWatched is the behavioural half: when the
// budget refuses the reservation the directory must NOT be watched at all.
// Charging and then watching anyway would be accounting theatre — the process
// would hold a descriptor the ledger believes it refused.
func TestGitDirsWatch_RefusedDirIsNotWatched(t *testing.T) {
	l := &fakeLedger{capacity: 0} // every reservation refused
	polls := gitDirsWatchPollProbe(t, l, 3*time.Second)
	if polls != 0 {
		t.Fatalf("a refused .git/worktrees watch still delivered %d event-driven polls, want 0", polls)
	}
	if used, _ := l.snapshot(); used != 0 {
		t.Fatalf("refused reservation charged %d descriptors, want 0", used)
	}
}

// TestGitDirsWatch_AdmittedDirIsWatched is the control for the test above: with
// budget available the same stimulus DOES drive a poll, so the negative result
// there is the refusal and not a broken harness.
func TestGitDirsWatch_AdmittedDirIsWatched(t *testing.T) {
	l := &fakeLedger{capacity: -1}
	// Generous ceiling, not a fixed wait: the probe returns as soon as the
	// poll lands (~0.8s), so this only buys tolerance on a loaded machine.
	polls := gitDirsWatchPollProbe(t, l, 30*time.Second)
	if polls == 0 {
		t.Fatal("an admitted .git/worktrees watch delivered no event-driven poll")
	}
}

// gitDirsWatchPollProbe starts the .git/worktrees watch against ledger l,
// creates a worktree directory under it, and reports how many times poll() ran
// as a result. The reconciliation ticker is not running (startGitDirsWatch is
// called directly), so every count is event-driven.
func gitDirsWatchPollProbe(t *testing.T, l *fakeLedger, window time.Duration) int {
	t.Helper()
	p := gitDirsParent(t, 0)

	var mu sync.Mutex
	calls := 0
	w := NewWatcher(NewStore(filepath.Join(t.TempDir(), "worktrees.json")),
		func() []ParentRepo {
			mu.Lock()
			calls++
			mu.Unlock()
			return []ParentRepo{p}
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.fdb = fdLedger{reserve: l.Reserve, release: l.Release, cost: l.Cost}

	ctx, cancel := context.WithCancel(context.Background())
	stop := w.startGitDirsWatch(ctx)
	defer func() { cancel(); stop() }()

	mu.Lock()
	base := calls
	mu.Unlock()

	if err := os.MkdirAll(filepath.Join(p.Path, ".git", "worktrees", "new"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Debounce is 750ms; poll() then calls parents(). Sample until the
	// deadline so the admitted case is not timing-flaky, and let the refused
	// case burn the full window before reporting zero.
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := calls - base
		mu.Unlock()
		if n > 0 {
			return n
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	return calls - base
}
