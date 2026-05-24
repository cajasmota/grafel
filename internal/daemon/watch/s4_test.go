// Package watch — S4 tests (#2154): aggressive watcher filter + git-diff
// ref-change validation.
package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeGitRepo creates a real git repo with an initial commit and returns
// its absolute path. Re-uses initGitRepo from githead_poller_test.go (same
// package) but avoids importing it — just re-declare the helper name here.
func makeGitRepoS4(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "s4@client-fixture-a.test")
	run("config", "user.name", "S4Test")
	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# s4 test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// gitAdd stages + commits a file in dir.
func gitCommitFileS4(t *testing.T, dir, name, content string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "add "+name)
}

// currentSHA returns the abbreviated commit SHA at HEAD.
func currentSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--short=12", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	s := string(out)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// ---------------------------------------------------------------------------
// A. Strict pre-subscription filter tests
// ---------------------------------------------------------------------------

// TestSkipDirsExtended verifies the newly-added hard-excludes from S4.
func TestSkipDirsExtended(t *testing.T) {
	cases := map[string]bool{
		".cache":   true,
		".vite":    true,
		".esbuild": true,
		// pre-existing
		".archigraph":   true,
		"node_modules":  true,
		"__pycache__":   true,
		".pytest_cache": true,
		".gradle":       true,
		".idea":         true,
		".vscode":       true,
		// should NOT be skipped
		"src":      false,
		"internal": false,
		"cmd":      false,
		"lib":      false,
		"app":      false,
	}
	for dir, wantSkip := range cases {
		if got := ShouldSkipDir(dir); got != wantSkip {
			t.Errorf("ShouldSkipDir(%q) = %v, want %v", dir, got, wantSkip)
		}
	}
}

// TestGitignoreRespected verifies that ShouldSkipDirGitignore respects
// a root .gitignore file.
func TestGitignoreRespected(t *testing.T) {
	dir := t.TempDir()
	// Write a .gitignore that excludes "private/" but not "src/".
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("private/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Evict cache in case this dir was used before.
	evictRepoIgnoreState(dir)

	skip, reason := ShouldSkipDirGitignore(dir, filepath.Join(dir, "private"), "private")
	if !skip {
		t.Errorf("expected 'private' to be skipped by .gitignore, got skip=false (reason=%q)", reason)
	}

	skip2, _ := ShouldSkipDirGitignore(dir, filepath.Join(dir, "src"), "src")
	if skip2 {
		t.Errorf("expected 'src' NOT to be skipped")
	}
}

// TestPerRepoWatchJSON verifies exclude_dirs and include_only_dirs.
func TestPerRepoWatchJSON(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, ".archigraph")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := RepoWatchConfig{
		ExcludeDirs:     []string{"fixtures"},
		IncludeOnlyDirs: []string{"src", "internal"},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(archDir, "watch.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	evictRepoIgnoreState(dir)

	// 'fixtures' should be excluded.
	skip, reason := ShouldSkipDirGitignore(dir, filepath.Join(dir, "fixtures"), "fixtures")
	if !skip {
		t.Errorf("expected fixtures to be excluded by watch.json, reason=%q", reason)
	}

	// 'docs' is not in include_only_dirs → should be excluded.
	skip2, reason2 := ShouldSkipDirGitignore(dir, filepath.Join(dir, "docs"), "docs")
	if !skip2 {
		t.Errorf("expected docs to be excluded by include_only_dirs, reason=%q", reason2)
	}

	// 'src' is in include_only_dirs → should NOT be excluded.
	skip3, _ := ShouldSkipDirGitignore(dir, filepath.Join(dir, "src"), "src")
	if skip3 {
		t.Errorf("expected src to pass include_only_dirs filter")
	}
}

// TestWatcherSkipsGitignored verifies end-to-end that a directory excluded
// by .gitignore does not receive fsnotify subscriptions and events from it
// do not fire the sink.
func TestWatcherSkipsGitignored(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	repo := t.TempDir()

	// Write .gitignore that excludes "generated/".
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	generatedDir := filepath.Join(repo, "generated")
	srcDir := filepath.Join(repo, "src")
	for _, d := range []string{generatedDir, srcDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Evict so loadRepoIgnoreState picks up the new .gitignore.
	evictRepoIgnoreState(repo)

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

	// Write into the gitignored dir — should NOT fire the sink.
	if err := os.WriteFile(filepath.Join(generatedDir, "foo.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Errorf("expected 0 sink calls for gitignored dir write, got %d", n)
	}

	// Write into src — should fire.
	if err := os.WriteFile(filepath.Join(srcDir, "bar.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 sink call for src write, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// B. Git-driven ref-change validation tests
// ---------------------------------------------------------------------------

// TestClassifyRefChange_NoSourceChanges verifies that a commit touching only
// non-source files (e.g. .log) returns ReindexNone.
func TestClassifyRefChange_NoSourceChanges(t *testing.T) {
	dir := makeGitRepoS4(t)
	oldSHA := currentSHA(t, dir)

	// Commit only a .log file (hits SkipExts → not a source file).
	gitCommitFileS4(t, dir, "build.log", "build output\n")
	newSHA := currentSHA(t, dir)

	hint, files := classifyRefChange(dir, oldSHA, newSHA, nil)
	if hint != ReindexNone {
		t.Errorf("expected ReindexNone, got %d; files=%v", hint, files)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 changed source files, got %v", files)
	}
}

// TestClassifyRefChange_SmallDiff verifies that a commit touching a small
// number of source files returns ReindexSmall.
func TestClassifyRefChange_SmallDiff(t *testing.T) {
	dir := makeGitRepoS4(t)
	oldSHA := currentSHA(t, dir)

	// Commit 3 .go files — well under smallDiffThreshold.
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		gitCommitFileS4(t, dir, name, "package main\n")
	}
	// Combine into a single measurable diff: just two SHAs apart.
	oldSHA2 := oldSHA
	newSHA := currentSHA(t, dir)

	hint, files := classifyRefChange(dir, oldSHA2, newSHA, nil)
	if hint != ReindexSmall {
		t.Errorf("expected ReindexSmall, got %d; files=%v", hint, files)
	}
	if len(files) == 0 {
		t.Error("expected at least 1 changed source file")
	}
}

// TestClassifyRefChange_LargeDiff verifies that >20 source files returns
// ReindexFull.
func TestClassifyRefChange_LargeDiff(t *testing.T) {
	dir := makeGitRepoS4(t)
	oldSHA := currentSHA(t, dir)

	// Commit 25 .go files in one shot.
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for i := 0; i < 25; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file%d.go", i))
		_ = os.WriteFile(name, []byte("package main\n"), 0o644)
	}
	run("add", ".")
	run("commit", "-m", "large diff")
	newSHA := currentSHA(t, dir)

	hint, _ := classifyRefChange(dir, oldSHA, newSHA, nil)
	if hint != ReindexFull {
		t.Errorf("expected ReindexFull for >%d files, got %d", smallDiffThreshold, hint)
	}
}

// TestClassifyRefChange_Unknown verifies that empty SHAs return ReindexUnknown.
func TestClassifyRefChange_Unknown(t *testing.T) {
	dir := makeGitRepoS4(t)
	hint, _ := classifyRefChange(dir, "", "abc123", nil)
	if hint != ReindexUnknown {
		t.Errorf("expected ReindexUnknown for empty oldSHA, got %d", hint)
	}
}

// TestPoller_SkipsNoSourceReindex verifies that when a commit touches only
// non-source files the poller emits zero BranchSwitchEvents to the sink.
func TestPoller_SkipsNoSourceReindex(t *testing.T) {
	dir := makeGitRepoS4(t)

	var count atomic.Int32
	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {
		count.Add(1)
	}, nil)
	p.AddRepo(dir)
	p.Start()
	defer p.Stop()

	// Commit only a .log file → no source changes → poller should suppress.
	gitCommitFileS4(t, dir, "ci.log", "logs\n")

	// Wait for multiple poll cycles.
	time.Sleep(400 * time.Millisecond)

	if n := count.Load(); n != 0 {
		t.Errorf("expected 0 sink calls for non-source commit, got %d", n)
	}
}

// TestPoller_EmitsForSourceReindex verifies that a commit touching a .go
// file does trigger a BranchSwitchEvent with the correct hint.
func TestPoller_EmitsForSourceReindex(t *testing.T) {
	dir := makeGitRepoS4(t)

	var events []BranchSwitchEvent
	var evMu sync.Mutex
	p := NewGitHeadPoller(50*time.Millisecond, func(ev BranchSwitchEvent) {
		evMu.Lock()
		events = append(events, ev)
		evMu.Unlock()
	}, nil)
	p.AddRepo(dir)
	p.Start()
	defer p.Stop()

	gitCommitFileS4(t, dir, "main.go", "package main\n")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		evMu.Lock()
		n := len(events)
		evMu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	evMu.Lock()
	got := make([]BranchSwitchEvent, len(events))
	copy(got, events)
	evMu.Unlock()

	if len(got) == 0 {
		t.Fatal("expected at least 1 BranchSwitchEvent for source commit")
	}
	ev := got[0]
	if ev.ReindexHint == ReindexNone {
		t.Errorf("ReindexHint should not be ReindexNone for source-file commit")
	}
	if len(ev.ChangedFiles) == 0 {
		t.Error("ChangedFiles should be non-empty")
	}
}
