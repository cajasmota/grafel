package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/watch"
)

// bootWatcher boots a daemon with the given poll setting and returns the live
// Watcher the engine plane constructed.
func bootWatcher(t *testing.T, poll bool) *watch.Watcher {
	t.Helper()
	isolateDaemonEnv(t)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ready := make(chan *watch.Watcher, 1)
	cfg := daemon.Config{
		Layout:              layout,
		GroupsForRepo:       func(string) []string { return nil },
		SchedulerIndex:      func(context.Context, string, string) error { return nil },
		SchedulerLinks:      func(context.Context, string) error { return nil },
		SchedulerGroupAlgo:  func(context.Context, string) error { return nil },
		ChangeDetectionPoll: poll,
		// An hour: this test drives AddRepo directly, never the ticker.
		ChangePollInterval: time.Hour,
		OnWatcherReady: func(w *watch.Watcher) {
			select {
			case ready <- w:
			default:
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = daemon.Run(ctx, cfg) }()

	select {
	case w := <-ready:
		return w
	case <-time.After(30 * time.Second):
		t.Fatal("watcher never became ready")
		return nil
	}
}

func pollModeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a", "b", "a/c", "a/c/d"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "x.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// #6932 arm A, end to end through the engine plane: with
// Config.ChangeDetectionPoll set, every existing subscribe path (they all go
// through Watcher.AddRepo) takes ZERO fs watch descriptors. That is the entire
// point of the mode — on Linux each of those descriptors is one inotify watch
// out of a per-UID, host-level, non-namespaced pool a container cannot raise.
func TestEnginePlane_PollModeTakesNoWatchDescriptors(t *testing.T) {
	w := bootWatcher(t, true)
	repo := pollModeRepo(t)
	n, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n != 0 {
		t.Fatalf("poll mode subscribed %d directories; must be 0", n)
	}
	if _, dirs, _, _, _ := w.Stats(); dirs != 0 {
		t.Fatalf("poll mode holds %d directory subscriptions; must be 0", dirs)
	}
	// The configured cadence must reach the poller. bootWatcher asks for an
	// hour, which is nothing like watch.DefaultChangePollInterval, so a
	// dropped Config.ChangePollInterval cannot pass for the default.
	cp, ok := w.ChangeDelegate().(*watch.ChangePoller)
	if !ok {
		t.Fatalf("delegate is %T, want *watch.ChangePoller", w.ChangeDelegate())
	}
	if got := cp.Interval(); got != time.Hour {
		t.Fatalf("poll interval = %v, want the configured 1h", got)
	}
}

// The control. Without the flag the engine plane is byte-for-byte the
// pre-#6932 daemon and still subscribes — otherwise the assertion above is
// measuring a broken watcher rather than a working mode.
func TestEnginePlane_DefaultModeStillSubscribes(t *testing.T) {
	w := bootWatcher(t, false)
	repo := pollModeRepo(t)
	n, err := w.AddRepo(repo)
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if n == 0 {
		t.Fatal("default (fsnotify) mode subscribed 0 directories — the control is vacuous")
	}
	if d := w.ChangeDelegate(); d != nil {
		t.Fatalf("default mode installed a change delegate: %T", d)
	}
}
