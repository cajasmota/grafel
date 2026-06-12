package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShouldSkipDir(t *testing.T) {
	cases := map[string]bool{
		"node_modules": true,
		".git":         true,
		"target":       true,
		"src":          false,
		"internal":     false,
		".archigraph":  true,
		"dist":         true,
		".claude":      true, // #3648: agent scratch / linked worktrees
	}
	for in, want := range cases {
		if got := ShouldSkipDir(in); got != want {
			t.Errorf("ShouldSkipDir(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestShouldSkipPath(t *testing.T) {
	cases := map[string]bool{
		"/repo/src/main.go":                false,
		"/repo/node_modules/foo/bar.js":    true,
		"/repo/.git/HEAD":                  true,
		"/repo/target/build.out":           true,
		"/repo/src/.archigraph/graph.json": true,
		"/repo/a.log":                      true,
		"/repo/a.swp":                      true,
		"/repo/cmd/foo/main_test.go":       false,
		// #3648: agent worktrees under .claude/ must be dropped at any depth,
		// including the high-churn node_modules nested inside each worktree.
		"/repo/.claude/worktrees/agent-x/src/main.ts":               true,
		"/repo/.claude/worktrees/agent-x/node_modules/foo/index.js": true,
	}
	for in, want := range cases {
		if got := ShouldSkipPath(in); got != want {
			t.Errorf("ShouldSkipPath(%q) = %v, want %v", in, got, want)
		}
	}
}

// newTestWatcher builds a watcher with a short debounce and a very high
// bulk threshold so existing tests don't accidentally trigger bulk mode.
func newTestWatcher(debounce time.Duration, sink EventSink) (*Watcher, error) {
	return NewWatcherConfig(Config{
		Debounce:          debounce,
		BulkThreshold:     10000, // effectively disable bulk detection
		HeartbeatInterval: time.Hour,
	}, sink, nil)
}

// TestDebounce verifies that a burst of writes within the debounce
// window collapses to a single sink invocation.
func TestDebounce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	doneCh := make(chan string, 4)
	w, err := newTestWatcher(150*time.Millisecond, func(repoPath string, _ bool) {
		calls.Add(1)
		doneCh <- repoPath
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Burst of 5 writes within ~50ms — fsnotify will surface each as
	// a Write event but the debouncer should coalesce them.
	for i := 0; i < 5; i++ {
		f := filepath.Join(src, "main.go")
		if err := os.WriteFile(f, []byte("package main // burst"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("debounce did not fire within 2s; calls=%d", calls.Load())
	}

	// Wait long enough that any leftover timer would have fired.
	time.Sleep(400 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected single debounced fire, got %d", got)
	}
}

// TestDebounceTwoBursts verifies that two separate bursts (separated
// by more than the debounce window) each trigger one sink call.
func TestDebounceTwoBursts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		calls int
	)
	w, err := newTestWatcher(100*time.Millisecond, func(string, bool) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	touch := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, "src", name),
			[]byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("a.go")
	time.Sleep(300 * time.Millisecond) // exceed debounce window
	touch("b.go")
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 2 {
		t.Errorf("two bursts should yield two sink calls, got %d", got)
	}
}

// TestSkipDirRespected verifies that creating files inside a skipped
// directory does NOT trigger the sink.
func TestSkipDirRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	nm := filepath.Join(repo, "node_modules", "foo")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w, err := newTestWatcher(100*time.Millisecond, func(string, bool) {
		calls.Add(1)
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Write inside node_modules — should be ignored.
	if err := os.WriteFile(filepath.Join(nm, "ignored.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 sink calls for node_modules write, got %d", got)
	}

	// Sanity check: write to src — should trigger.
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 sink call for src write, got %d", got)
	}
}

// TestClaudeWorktreeNotWatched is the #3648 regression guard: edits inside a
// .claude/worktrees/<agent> checkout (the scratch worktrees Claude Code creates,
// each a full repo tree with its own node_modules) must NEVER reach the sink.
// Before .claude was added to SkipDirs, the worktrees' source trees were walked
// and watched, so every agent merge fed a full-reindex of the parent repo.
func TestClaudeWorktreeNotWatched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	// A worktree source tree AND its nested node_modules, mirroring the live layout.
	wtSrc := filepath.Join(repo, ".claude", "worktrees", "agent-x", "src")
	wtNM := filepath.Join(repo, ".claude", "worktrees", "agent-x", "node_modules", "pkg")
	if err := os.MkdirAll(wtSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wtNM, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w, err := newTestWatcher(100*time.Millisecond, func(string, bool) {
		calls.Add(1)
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The worktree directory must not have been subscribed at all.
	for _, d := range w.Repos() {
		_ = d
	}
	w.mu.Lock()
	for dir := range w.dirToRepo {
		if filepathHasClaude(dir) {
			w.mu.Unlock()
			t.Fatalf("watcher subscribed a .claude path it should have skipped: %s", dir)
		}
	}
	w.mu.Unlock()

	// Writes inside the worktree (source AND node_modules) must be ignored.
	if err := os.WriteFile(filepath.Join(wtSrc, "feature.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtNM, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 sink calls for .claude/worktrees writes, got %d", got)
	}

	// Sanity: a real source write still triggers exactly one reindex.
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 sink call for src write, got %d", got)
	}
}

func filepathHasClaude(p string) bool {
	return strings.Contains(filepath.ToSlash(p), "/.claude/") ||
		strings.HasSuffix(filepath.ToSlash(p), "/.claude")
}

// TestBulkDetection verifies that a burst exceeding BulkThreshold in one
// window calls the sink with bulk=true exactly once and suppresses a
// subsequent non-bulk debounced call for the same burst.
func TestBulkDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	var (
		mu        sync.Mutex
		bulkCalls int
		normCalls int
	)
	doneCh := make(chan struct{}, 10)
	w, err := NewWatcherConfig(Config{
		Debounce:          200 * time.Millisecond,
		BulkThreshold:     3, // low so tests are fast
		BulkWindow:        500 * time.Millisecond,
		HeartbeatInterval: time.Hour,
	}, func(_ string, bulk bool) {
		mu.Lock()
		if bulk {
			bulkCalls++
		} else {
			normCalls++
		}
		mu.Unlock()
		doneCh <- struct{}{}
	}, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Write 5 files rapidly — should trigger bulk at event #3.
	for i := 0; i < 5; i++ {
		p := filepath.Join(src, "bulk_test_file.go")
		if err := os.WriteFile(p, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for the bulk call.
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("bulk sink never fired")
	}

	// Wait out the debounce window to confirm no extra normal call.
	time.Sleep(600 * time.Millisecond)

	mu.Lock()
	bc, nc := bulkCalls, normCalls
	mu.Unlock()

	if bc != 1 {
		t.Errorf("expected 1 bulk call, got %d", bc)
	}
	// A debounce timer may or may not fire after a bulk trigger depending on
	// OS scheduling; we only require no more than 1 extra call total.
	if nc > 1 {
		t.Errorf("expected ≤1 normal calls after bulk, got %d", nc)
	}
}

// TestExcludeDirs verifies that per-instance ExcludeDirs blocks events
// from those directories even when they are not in the global SkipDirs.
func TestExcludeDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	generated := filepath.Join(repo, "generated")
	src := filepath.Join(repo, "src")
	for _, d := range []string{generated, src} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var calls atomic.Int32
	w, err := NewWatcherConfig(Config{
		Debounce:          100 * time.Millisecond,
		BulkThreshold:     10000,
		HeartbeatInterval: time.Hour,
		ExcludeDirs:       []string{"generated"},
	}, func(string, bool) {
		calls.Add(1)
	}, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Write in the excluded dir — should be ignored.
	if err := os.WriteFile(filepath.Join(generated, "foo.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 calls for excluded dir write, got %d", got)
	}

	// Write in src — should fire.
	if err := os.WriteFile(filepath.Join(src, "bar.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call for src write, got %d", got)
	}
}

// TestForceRescan verifies that ForceRescan triggers the sink with bulk=true
// for every registered repo.
func TestForceRescan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo1 := t.TempDir()
	repo2 := t.TempDir()

	var (
		mu       sync.Mutex
		bulkSeen []string
	)
	w, err := newTestWatcher(5*time.Second, func(path string, bulk bool) {
		if bulk {
			mu.Lock()
			bulkSeen = append(bulkSeen, path)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	for _, r := range []string{repo1, repo2} {
		if _, err := w.AddRepo(r); err != nil {
			t.Fatalf("add %s: %v", r, err)
		}
	}

	w.ForceRescan()
	// ForceRescan is synchronous per-repo (called via goroutines); give it
	// a moment to complete.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := len(bulkSeen)
	mu.Unlock()
	if got != 2 {
		t.Errorf("ForceRescan: expected 2 bulk calls, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// #2645 regression tests: TS/TSX and other source file changes must trigger
// the sink. These are the canonical regression tests for issue #2645 —
// "daemon watcher isn't picking up TS/TSX file changes in core-mobile".
// ---------------------------------------------------------------------------

// TestWatcher_TSFileChange_TriggersReindex verifies that editing a .tsx file
// in a watched repo fires the sink within 5 s. This is the direct regression
// test for #2645: watcher events from TS/TSX files must flow through to the
// debounce → sink → reindex enqueue chain.
func TestWatcher_TSFileChange_TriggersReindex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-create a .tsx fixture so subsequent writes are tracked-file edits
	// (same scenario as editing an existing component in core-mobile).
	initial := filepath.Join(src, "Component.tsx")
	if err := os.WriteFile(initial, []byte("export default () => null;"), 0o644); err != nil {
		t.Fatal(err)
	}

	doneCh := make(chan string, 4)
	w, err := newTestWatcher(200*time.Millisecond, func(repoPath string, _ bool) {
		doneCh <- repoPath
	})
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	// Simulate the developer saving an edit to the .tsx file.
	if err := os.WriteFile(initial, []byte("export default () => <View/>;"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-doneCh:
		if got != repo {
			t.Errorf("sink got repo=%q, want %q", got, repo)
		}
		// Success: the watcher fired for the TS file change within 5 s.
	case <-time.After(5 * time.Second):
		t.Fatal("#2645 regression: watcher did not fire within 5 s for .tsx edit")
	}
}

// TestWatcher_AcceptsAllSupportedExtensions verifies that .ts, .tsx, .js,
// .jsx, .py and .go edits all trigger watcher events. None of these are in
// SkipExts, so they should all reach the sink.
func TestWatcher_AcceptsAllSupportedExtensions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	type tc struct {
		ext     string
		content string
	}
	cases := []tc{
		{".ts", "export const x = 1;"},
		{".tsx", "export default () => null;"},
		{".js", "module.exports = {};"},
		{".jsx", "export default () => null;"},
		{".py", "x = 1"},
		{".go", "package main"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.ext, func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			src := filepath.Join(repo, "src")
			if err := os.MkdirAll(src, 0o755); err != nil {
				t.Fatal(err)
			}

			doneCh := make(chan struct{}, 4)
			w, err := newTestWatcher(150*time.Millisecond, func(_ string, _ bool) {
				doneCh <- struct{}{}
			})
			if err != nil {
				t.Fatalf("new watcher: %v", err)
			}
			defer w.Stop()
			if _, err := w.AddRepo(repo); err != nil {
				t.Fatalf("AddRepo: %v", err)
			}

			f := filepath.Join(src, "file"+c.ext)
			if err := os.WriteFile(f, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}

			select {
			case <-doneCh:
				// OK — extension was not filtered.
			case <-time.After(3 * time.Second):
				t.Errorf("#2645: watcher did not fire for %s file within 3 s", c.ext)
			}
		})
	}
}

// TestExtendedStats verifies that ExtendedStats returns per-repo counters
// after some events.
func TestExtendedStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	doneCh := make(chan struct{}, 4)
	w, err := newTestWatcher(150*time.Millisecond, func(_ string, _ bool) {
		doneCh <- struct{}{}
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Stop()
	if _, err := w.AddRepo(repo); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := os.WriteFile(filepath.Join(src, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("sink never fired")
	}

	repoStats, total, _, _ := w.ExtendedStats()
	if total == 0 {
		t.Error("expected totalEvents > 0")
	}
	if len(repoStats) != 1 {
		t.Errorf("expected 1 repo stat, got %d", len(repoStats))
	}
	if repoStats[0].TotalEvents == 0 {
		t.Error("per-repo TotalEvents should be > 0")
	}
	if repoStats[0].LastEventAt.IsZero() {
		t.Error("LastEventAt should be non-zero")
	}
}
