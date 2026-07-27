//go:build !windows

package sched

// cancel_processgroup_5999_test.go — cancellation must kill the child's whole
// process GROUP, not just its pid (#5999).
//
// The heavy batch children are spawned with Setpgid (applyGroupAlgoNice), so a
// process group already exists around each of them. exec.CommandContext's
// DEFAULT Cancel is cmd.Process.Kill() — a single-pid SIGKILL — which leaves
// any grandchild the child forked alive. That is not a test artifact: the index
// child fans out `grafel extract` subprocesses and the link child shells out
// too, so a cancelled pass could leave live grandchildren behind holding the
// inherited stdout/stderr pipes. The runner drains those pipes to EOF before
// cmd.Wait(), so a surviving grandchild also wedges the runner itself: the call
// never returns, and the stage token it is holding is never released.
//
// The stand-in child below makes that deterministic rather than flaky: it forks
// a long `sleep` that inherits the pipes and records its pid, then blocks.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeGroupAlgoBinary points the group-algo runner at a shell-script stand-in,
// the group-algo analogue of fakeChildScript.
func fakeGroupAlgoBinary(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-group-algo-child.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake group-algo child: %v", err)
	}
	prev := groupAlgoChildBinary
	groupAlgoChildBinary = func() (string, error) { return path, nil }
	t.Cleanup(func() { groupAlgoChildBinary = prev })
}

// pidIsAlive reports whether pid still exists (signal 0 probe).
func pidIsAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func waitPidGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !pidIsAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !pidIsAlive(pid)
}

// readPidFile waits for the stand-in child's grandchild to record its pid, and
// registers a cleanup that kills it. The cleanup matters on the FAILURE path:
// when the group kill does not bind, every failing run would otherwise leave a
// live `sleep` behind, and a mutation sweep leaves a pile of them on the box.
func readPidFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild never recorded its pid in %s", path)
	return 0
}

// TestRunSubprocessLinks_CancelKillsGrandchildren is the deterministic form of
// the flaky #5999 failure: with a single-pid kill the backgrounded `sleep`
// survives, keeps the inherited stdout pipe open, and the runner blocks in its
// drain loop past the assertion window.
func TestRunSubprocessLinks_CancelKillsGrandchildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	fakeChildScript(t, "sleep 60 &\necho $! > "+pidFile+"\nsleep 60")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunSubprocessLinks(ctx, "g", nil) }()

	pid := readPidFile(t, pidFile)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run returned nil error, want a cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("runner still blocked 5s after cancel — grandchild pid %d alive=%v holding the inherited pipes",
			pid, pidIsAlive(pid))
	}

	if !waitPidGone(pid, 2*time.Second) {
		t.Fatalf("grandchild pid %d survived cancellation — the kill did not reach the process group", pid)
	}
}

// TestRunSubprocessGroupAlgo_CancelKillsGrandchildren pins the same contract on
// the other Setpgid child. group-algo does not fan out today, but it is spawned
// through the same hook, and a supervisor whose cancellation reaches only the
// direct child is the defect — not the particular argv it happens to run.
func TestRunSubprocessGroupAlgo_CancelKillsGrandchildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	fakeGroupAlgoBinary(t, "sleep 60 &\necho $! > "+pidFile+"\nsleep 60")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunSubprocessGroupAlgo(ctx, "g", nil) }()

	pid := readPidFile(t, pidFile)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled group-algo returned nil error, want a cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("group-algo runner still blocked 5s after cancel — grandchild pid %d alive=%v",
			pid, pidIsAlive(pid))
	}

	if !waitPidGone(pid, 2*time.Second) {
		t.Fatalf("group-algo grandchild pid %d survived cancellation — the kill did not reach the process group", pid)
	}
}
