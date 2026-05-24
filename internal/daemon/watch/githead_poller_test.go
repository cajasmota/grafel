package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// initGitRepo creates a minimal git repo under dir for testing.
// Returns the absolute path.
func initGitRepo(t *testing.T) string {
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
	run("config", "user.email", "test@client-fixture-a.test")
	run("config", "user.name", "Test")
	// Initial commit so HEAD is a valid ref.
	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// switchBranch creates and checks out a new branch in dir.
func switchBranch(t *testing.T, dir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", "-b", branch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b %s: %v\n%s", branch, err, out)
	}
}

// commitFile adds a new file and commits it, advancing HEAD SHA.
func commitFile(t *testing.T, dir, name string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	f := filepath.Join(dir, name)
	if err := os.WriteFile(f, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "add "+name)
}

// TestGitHeadPoller_BranchSwitch verifies that switching branches emits
// exactly one BranchSwitchEvent with the correct old/new ref.
func TestGitHeadPoller_BranchSwitch(t *testing.T) {
	repoDir := initGitRepo(t)

	var (
		mu     sync.Mutex
		events []BranchSwitchEvent
	)

	// Fast poll interval for tests.
	p := NewGitHeadPoller(50*time.Millisecond, func(ev BranchSwitchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Switch to a new branch.
	switchBranch(t, repoDir, "feat/test-branch")

	// Wait up to 1 second for the event to arrive.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	got := make([]BranchSwitchEvent, len(events))
	copy(got, events)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("no BranchSwitchEvent emitted after branch switch")
	}
	ev := got[0]
	if ev.RepoPath != repoDir {
		t.Errorf("RepoPath: want %s, got %s", repoDir, ev.RepoPath)
	}
	if ev.OldRef != "main" {
		t.Errorf("OldRef: want main, got %q", ev.OldRef)
	}
	if ev.NewRef != "feat/test-branch" {
		t.Errorf("NewRef: want feat/test-branch, got %q", ev.NewRef)
	}
	if ev.OldSHA == "" {
		t.Error("OldSHA should be non-empty")
	}
}

// TestGitHeadPoller_NoSpuriousEvents verifies that the poller does NOT emit
// events when HEAD has not changed.
func TestGitHeadPoller_NoSpuriousEvents(t *testing.T) {
	repoDir := initGitRepo(t)

	var count atomic.Int32
	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {
		count.Add(1)
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Let 5 poll cycles pass without any branch change.
	time.Sleep(300 * time.Millisecond)

	if n := count.Load(); n != 0 {
		t.Errorf("unexpected events emitted with no branch change: got %d", n)
	}
}

// TestGitHeadPoller_SHAChange verifies that committing on the same branch
// (advancing the SHA without changing the ref name) also fires an event.
func TestGitHeadPoller_SHAChange(t *testing.T) {
	repoDir := initGitRepo(t)

	var (
		mu     sync.Mutex
		events []BranchSwitchEvent
	)
	p := NewGitHeadPoller(50*time.Millisecond, func(ev BranchSwitchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Make a new commit on main (SHA advances, ref stays "main").
	commitFile(t, repoDir, "new-file.txt")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	got := make([]BranchSwitchEvent, len(events))
	copy(got, events)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("no event emitted after new commit (SHA change)")
	}
	ev := got[0]
	if ev.OldSHA == ev.NewSHA {
		t.Errorf("SHA should have changed: old=%s new=%s", ev.OldSHA, ev.NewSHA)
	}
	// Ref name stays "main" in both.
	if ev.OldRef != "main" || ev.NewRef != "main" {
		t.Errorf("ref should remain main: old=%q new=%q", ev.OldRef, ev.NewRef)
	}
}

// TestGitHeadPoller_RemoveRepo verifies that removing a repo stops event delivery.
func TestGitHeadPoller_RemoveRepo(t *testing.T) {
	repoDir := initGitRepo(t)

	var count atomic.Int32
	p := NewGitHeadPoller(50*time.Millisecond, func(_ BranchSwitchEvent) {
		count.Add(1)
	}, nil)
	p.AddRepo(repoDir)
	p.Start()
	defer p.Stop()

	// Remove the repo, then switch branches.
	p.RemoveRepo(repoDir)
	switchBranch(t, repoDir, "feat/after-remove")

	time.Sleep(300 * time.Millisecond)

	if n := count.Load(); n != 0 {
		t.Errorf("received %d events after RemoveRepo, want 0", n)
	}
}
