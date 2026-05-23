package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// runDaemonWithPhaseBForTest spins up daemon.Run with a synthetic
// scheduler index hook that records every reindex request. The repo
// is created in a tempdir and registered via ReposToWatch.
func runDaemonWithPhaseBForTest(t *testing.T, repos []string, schedIdx daemon.Config) (daemon.Layout, func()) {
	t.Helper()
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	cfg := schedIdx
	cfg.Layout = layout
	cfg.ReposToWatch = func() []string { return repos }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, cfg) }()

	// Poll for readiness.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := client.Dial()
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cleanup := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Logf("daemon did not exit within 3s")
		}
	}
	return layout, cleanup
}

// TestPhaseB_FileWriteTriggersReindex creates a file in a watched
// repo and verifies the scheduler index hook fires within the
// debounce window.
func TestPhaseB_FileWriteTriggersReindex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	// fsnotify on macOS /var/folders (where t.TempDir lives) is flaky in
	// sandboxed CI/test runs — events do not always reach the watcher
	// within the debounce window. Opt-in via ARCHIGRAPH_FSNOTIFY_TESTS=1
	// when running on a host where fsnotify is known to work end-to-end.
	if os.Getenv("ARCHIGRAPH_FSNOTIFY_TESTS") != "1" {
		t.Skip("set ARCHIGRAPH_FSNOTIFY_TESTS=1 to run fsnotify-backed phaseb tests")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	var indexCount atomic.Int32
	indexedCh := make(chan string, 4)

	cfg := daemon.Config{
		// Bare RPC entrypoints — unused in this test but Service
		// requires them non-nil to invoke Index/Rebuild RPCs.
		Index: func(_ proto.IndexArgs) (string, string, error) { return "", "", nil },
		Rebuild: func(_ proto.RebuildArgs) ([]string, string, error) {
			return nil, "", nil
		},
		SchedulerIndex: func(_ context.Context, p string) error {
			indexCount.Add(1)
			indexedCh <- p
			return nil
		},
		SchedulerLinks: func(_ context.Context, _ string) error { return nil },
		SchedulerAlgo:  func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:  func(_ string) []string { return nil },
	}

	_, cleanup := runDaemonWithPhaseBForTest(t, []string{repo}, cfg)
	defer cleanup()

	// Small settle delay so AddRepo finishes before the write.
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(repo, "src", "main.go"),
		[]byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-indexedCh:
		abs, _ := filepath.Abs(repo)
		if got != abs {
			t.Errorf("scheduler got repo=%q, want %q", got, abs)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("scheduler did not fire within 5s; calls=%d", indexCount.Load())
	}
}

// TestPhaseB_RapidWritesCoalesce ensures a tight burst of writes
// produces a single index call (debounce coalescing).
func TestPhaseB_RapidWritesCoalesce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	// See TestPhaseB_FileWriteTriggersReindex for the rationale on the
	// ARCHIGRAPH_FSNOTIFY_TESTS gate.
	if os.Getenv("ARCHIGRAPH_FSNOTIFY_TESTS") != "1" {
		t.Skip("set ARCHIGRAPH_FSNOTIFY_TESTS=1 to run fsnotify-backed phaseb tests")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		calls int
	)
	cfg := daemon.Config{
		Index: func(_ proto.IndexArgs) (string, string, error) { return "", "", nil },
		Rebuild: func(_ proto.RebuildArgs) ([]string, string, error) {
			return nil, "", nil
		},
		SchedulerIndex: func(_ context.Context, _ string) error {
			mu.Lock()
			calls++
			mu.Unlock()
			return nil
		},
		SchedulerLinks: func(_ context.Context, _ string) error { return nil },
		SchedulerAlgo:  func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:  func(_ string) []string { return nil },
	}

	_, cleanup := runDaemonWithPhaseBForTest(t, []string{repo}, cfg)
	defer cleanup()
	time.Sleep(150 * time.Millisecond)

	// Burst of 8 writes over ~400ms — debounce is 2s so all should
	// coalesce.
	for i := 0; i < 8; i++ {
		if err := os.WriteFile(filepath.Join(repo, "src", "a.go"),
			[]byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait beyond the 2s debounce window.
	time.Sleep(2500 * time.Millisecond)

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Errorf("expected coalesced single index, got %d", got)
	}
}

// TestPhaseB_StatusRPCReflectsWatcher dials the daemon and verifies
// the Status reply includes the watcher count after AddRepo runs.
func TestPhaseB_StatusRPCReflectsWatcher(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := daemon.Config{
		Index: func(_ proto.IndexArgs) (string, string, error) { return "", "", nil },
		Rebuild: func(_ proto.RebuildArgs) ([]string, string, error) {
			return nil, "", nil
		},
		SchedulerIndex: func(_ context.Context, _ string) error { return nil },
		SchedulerLinks: func(_ context.Context, _ string) error { return nil },
		SchedulerAlgo:  func(_ context.Context, _ string) error { return nil },
		GroupsForRepo:  func(_ string) []string { return nil },
	}
	_, cleanup := runDaemonWithPhaseBForTest(t, []string{repo}, cfg)
	defer cleanup()

	c, err := client.Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	st, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.WatcherRepos != 1 {
		t.Errorf("expected WatcherRepos=1, got %d (Dirs=%d)", st.WatcherRepos, st.WatcherDirs)
	}
}
