package watch

import (
	"sync"
	"testing"
	"time"
)

// newNopWatcher builds a watcher that never fires (sink never called) and
// operates on a temp dir so fsnotify can actually subscribe.
func newNopWatcher(t *testing.T) *Watcher {
	t.Helper()
	w, err := NewWatcherConfig(Config{
		Debounce:          time.Hour, // effectively disabled
		HeartbeatInterval: time.Hour,
	}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	t.Cleanup(w.Stop)
	return w
}

// TestManagerPauseResume verifies the basic Pause/Resume lifecycle.
// Uses a real tmp directory so AddRepo can walk real paths.
//
// M2 semantics: Register declares a slot as paused (not subscribed). A
// subsequent Resume or SubscribeGroup call activates the fsnotify subscription.
func TestManagerPauseResume(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	dir := t.TempDir()

	// M2: Register declares the slot as paused (not subscribed at boot).
	m.Register(dir, "main")
	if !m.IsPaused(dir, "main") {
		t.Fatal("M2: want paused after Register (no eager subscribe at boot)")
	}
	if m.ActiveCount() != 0 {
		t.Fatalf("M2: want ActiveCount=0 after Register, got %d", m.ActiveCount())
	}
	if m.PausedCount() != 1 {
		t.Fatalf("M2: want PausedCount=1 after Register, got %d", m.PausedCount())
	}

	// Activate via Resume (simulates first MCP query / cold-wake).
	// Resume lazily calls AddRepo since refCount was 0.
	elapsed := m.Resume(dir, "main")
	if m.IsPaused(dir, "main") {
		t.Fatal("want not paused after Resume")
	}
	if m.ActiveCount() != 1 {
		t.Fatalf("want ActiveCount=1 after Resume, got %d", m.ActiveCount())
	}
	if m.PausedCount() != 0 {
		t.Fatalf("want PausedCount=0 after Resume, got %d", m.PausedCount())
	}
	if elapsed > 2*time.Second {
		t.Errorf("resume took unexpectedly long: %s", elapsed)
	}

	// Pause — removes fsnotify subscription (idle eviction).
	m.Pause(dir, "main")
	if !m.IsPaused(dir, "main") {
		t.Fatal("want paused after Pause")
	}
	if m.ActiveCount() != 0 {
		t.Fatalf("want ActiveCount=0 after pause, got %d", m.ActiveCount())
	}
	if m.PausedCount() != 1 {
		t.Fatalf("want PausedCount=1 after pause, got %d", m.PausedCount())
	}

	// Pause again — idempotent.
	m.Pause(dir, "main")
	if m.PausedCount() != 1 {
		t.Fatal("double-pause must be idempotent")
	}

	// Resume again — re-subscribes.
	m.Resume(dir, "main")
	if m.ActiveCount() != 1 {
		t.Fatalf("want ActiveCount=1 after second Resume, got %d", m.ActiveCount())
	}

	// Resume again — idempotent.
	m.Resume(dir, "main")
	if m.ActiveCount() != 1 {
		t.Fatal("double-resume must be idempotent")
	}
}

// TestManagerMultiRefRefcount verifies that the fsnotify subscription stays
// alive while at least one ref is active.
//
// M2 semantics: Register declares slots as paused. Resume activates them and
// creates the fsnotify subscription. RefCount tracks active refs; the
// subscription is removed only when all refs are paused.
func TestManagerMultiRefRefcount(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	dir := t.TempDir()

	// M2: Register two refs (declares slots, no subscribe).
	m.Register(dir, "main")
	m.Register(dir, "feat/x")

	// Activate both refs via Resume (simulates first query / cold-wake).
	// The first Resume triggers AddRepo; the second finds refCount > 0 and skips.
	m.Resume(dir, "main")
	m.Resume(dir, "feat/x")

	// Pause one — repo should still be subscribed (feat/x still active).
	m.Pause(dir, "main")
	repos := w.Repos()
	found := false
	for _, r := range repos {
		if r == dir {
			found = true
			break
		}
	}
	if !found {
		t.Error("repo should still be in watcher after pausing only one of two refs")
	}

	// Pause the second ref — now the subscription should be removed.
	m.Pause(dir, "feat/x")
	repos = w.Repos()
	for _, r := range repos {
		if r == dir {
			t.Error("repo should be removed from watcher after all refs paused")
		}
	}

	// Resume feat/x — repo subscription should come back.
	m.Resume(dir, "feat/x")
	repos = w.Repos()
	found = false
	for _, r := range repos {
		if r == dir {
			found = true
			break
		}
	}
	if !found {
		t.Error("repo should be back in watcher after resuming")
	}
}

// TestManagerConcurrentWakes verifies that 10 simultaneous cold-wakes of
// different (dir, ref) slots complete without data races or deadlocks.
func TestManagerConcurrentWakes(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	const N = 10
	dirs := make([]string, N)
	for i := 0; i < N; i++ {
		dirs[i] = t.TempDir()
		// Seed each dir in the watcher first, then register and pause.
		if _, err := w.AddRepo(dirs[i]); err != nil {
			t.Fatalf("AddRepo[%d]: %v", i, err)
		}
		m.Register(dirs[i], "main")
		m.Pause(dirs[i], "main")
	}

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(dir string) {
			defer wg.Done()
			m.Resume(dir, "main")
		}(dirs[i])
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent wakes deadlocked")
	}

	// All should be active now.
	if got := m.ActiveCount(); got != N {
		t.Errorf("want ActiveCount=%d, got %d", N, got)
	}
	if got := m.PausedCount(); got != 0 {
		t.Errorf("want PausedCount=0, got %d", got)
	}
}

// TestManagerUnknownSlotPause verifies that pausing an unregistered slot
// does not panic and marks it paused.
func TestManagerUnknownSlotPause(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	// Pause without prior Register — should not panic.
	m.Pause("/nonexistent", "main")
	if !m.IsPaused("/nonexistent", "main") {
		t.Fatal("want paused for unknown slot after Pause")
	}
}

// TestManagerResumeWithin500ms verifies that a Resume call completes within
// the 500ms budget specified in #2096.
func TestManagerResumeWithin500ms(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	dir := t.TempDir()
	if _, err := w.AddRepo(dir); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	m.Register(dir, "main")
	m.Pause(dir, "main")

	elapsed := m.Resume(dir, "main")
	if elapsed > 500*time.Millisecond {
		t.Errorf("resume latency %s exceeds 500ms budget", elapsed)
	}
}
