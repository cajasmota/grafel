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
	"fmt"
	"os"
	"os/exec"
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

// Budgets for the cancellation contract. These are DEADLOCK budgets, not
// latency budgets: the failure mode #5999 describes is a runner that blocks
// FOREVER because a surviving grandchild holds the inherited stdout pipe open,
// and a grandchild that is never reaped because the kill went to a single pid
// instead of the process group. Neither failure gets faster on a fast machine,
// so the budgets are sized to never fire on a busy one — the original 5s/2s
// pair produced a false red on a loaded macOS runner and passed on retry.
//
// Do not tighten these to "assert cancellation is fast". If cancellation
// latency ever needs a bound, that is a separate perf-tagged test.
const (
	cancelUnblockBudget  = 30 * time.Second
	grandchildReapBudget = 10 * time.Second
)

// pidIsAlive reports whether pid still exists (signal 0 probe).
func pidIsAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// trackedHolder is a process the test is following, together with the process
// group it belonged to WHILE IT WAS ALIVE.
//
// Capturing the pgid up front is the whole point. The macOS release-gate
// failure that prompted these diagnostics reported `alive=false`: by the time
// anything wanted to explain the block, Getpgid(pid) could only return ESRCH,
// so a diagnostic that reads the group at failure time explains nothing exactly
// when it is needed. Read it while the process is still there.
type trackedHolder struct {
	pid  int
	pgid int // 0 == could not be read
}

// trackHolder waits for the stand-in child to record a pid, registers its
// cleanup (see readPidFile) and captures its process group while it lives.
func trackHolder(t *testing.T, pidFile string) trackedHolder {
	t.Helper()
	h := trackedHolder{pid: readPidFile(t, pidFile)}
	if pgid, err := syscall.Getpgid(h.pid); err == nil {
		h.pgid = pgid
	}
	return h
}

// describeHolders renders what is still standing when a cancelled runner has
// not returned. `alive=false` on the one pid these tests track was, on the
// macOS CI failure that prompted this helper, the whole of the evidence — and
// it is the least informative half of it: the runner blocks until EVERY holder
// of the inherited pipes is gone, and the tracked process is only one candidate.
// This names the rest.
//
// Two outcomes, and they diagnose different bugs. If the child's process group
// still has members, the group kill did not bind (a #5999 regression). If the
// group is empty and the runner is STILL blocked, the holder escaped the group
// — which no process-group kill can fix, and which boundPostCancelDrain is what
// bounds (see cancel_escaped_pipe_test.go).
func describeHolders(h trackedHolder) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tracked pid %d: alive=%v, pgid-when-alive=%d (escaped=%v)\n",
		h.pid, pidIsAlive(h.pid), h.pgid, h.pgid == h.pid && h.pgid != 0)
	if h.pgid == 0 {
		fmt.Fprintf(&b, "no process group was captured for pid %d — cannot enumerate survivors\n", h.pid)
		return b.String()
	}
	// NOT `ps -g`: BSD/macOS reads that as a process-group selector, but
	// procps-ng (Ubuntu, where the full -race suite also runs) documents -g as
	// session or effective group NAME, so it would silently select the wrong
	// set. Enumerate everything and filter on the PGID column ourselves.
	out, err := exec.Command("ps", "-eo", "pid,ppid,pgid,stat,args").CombinedOutput()
	if err != nil {
		fmt.Fprintf(&b, "ps failed: %v\n", err)
		return b.String()
	}
	survivors := 0
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if pgid, convErr := strconv.Atoi(f[2]); convErr != nil || pgid != h.pgid {
			continue
		}
		fmt.Fprintf(&b, "  survivor: %s\n", strings.TrimSpace(line))
		survivors++
	}
	if survivors == 0 {
		fmt.Fprintf(&b, "  no survivors in pgid %d — the group kill bound; the holder is OUTSIDE the group\n", h.pgid)
	}
	return b.String()
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
	// 30s, not 5s. This is FIXTURE SETUP, not the assertion: it waits for the
	// stand-in child to fork a grandchild and write its pid. Under a loaded
	// full-suite run that process start took longer than 5s and the test failed
	// here — before reaching anything it exists to check. Widening cannot mask a
	// bug: if the pid is never written the t.Fatalf below still fires, just
	// later. The real assertions have their own budgets (see the const block).
	deadline := time.Now().Add(30 * time.Second)
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

	holder := trackHolder(t, pidFile)
	pid := holder.pid
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run returned nil error, want a cancellation error")
		}
	case <-time.After(cancelUnblockBudget):
		t.Fatalf("runner still blocked %s after cancel — grandchild pid %d alive=%v holding the inherited pipes\n%s",
			cancelUnblockBudget, pid, pidIsAlive(pid), describeHolders(holder))
	}

	if !waitPidGone(pid, grandchildReapBudget) {
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

	holder := trackHolder(t, pidFile)
	pid := holder.pid
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled group-algo returned nil error, want a cancellation error")
		}
	case <-time.After(cancelUnblockBudget):
		t.Fatalf("group-algo runner still blocked %s after cancel — grandchild pid %d alive=%v\n%s",
			cancelUnblockBudget, pid, pidIsAlive(pid), describeHolders(holder))
	}

	if !waitPidGone(pid, grandchildReapBudget) {
		t.Fatalf("group-algo grandchild pid %d survived cancellation — the kill did not reach the process group", pid)
	}
}

// TestDescribeHolders_NamesSurvivorsWhenTheTrackedPidIsDead pins the diagnostic
// against the exact shape of the failure it exists for: the tracked process is
// already DEAD (`alive=false`) and something else is still holding the pipes.
// An earlier version read the process group at failure time, so in precisely
// this case Getpgid returned ESRCH and it printed nothing useful.
func TestDescribeHolders_NamesSurvivorsWhenTheTrackedPidIsDead(t *testing.T) {
	// A stand-in group: a shell leading its own process group, a background
	// sleep whose pid we track, and a foreground sleep we do not.
	cmd := exec.Command("/bin/sh", "-c", "sleep 60 & echo $! ; sleep 60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Reap the whole group whatever happens.
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	var tracked int
	if _, err := fmt.Fscan(out, &tracked); err != nil {
		t.Fatalf("read background pid: %v", err)
	}
	h := trackHolder2(t, tracked)
	if h.pgid != cmd.Process.Pid {
		t.Fatalf("fixture: background sleep pgid=%d, want the child's pid %d", h.pgid, cmd.Process.Pid)
	}

	// Kill ONLY the tracked process, leaving the rest of its group standing:
	// the `alive=false, something still holds the pipe` shape.
	if err := syscall.Kill(tracked, syscall.SIGKILL); err != nil {
		t.Fatalf("kill tracked pid: %v", err)
	}
	if !waitPidGone(tracked, 5*time.Second) {
		t.Skip("tracked pid did not become unreachable; cannot stage the alive=false case")
	}

	got := describeHolders(h)
	if !strings.Contains(got, "alive=false") {
		t.Fatalf("diagnostic does not report the tracked pid as dead:\n%s", got)
	}
	if !strings.Contains(got, "survivor:") {
		t.Fatalf("diagnostic named no survivors, though the rest of pgid %d is still running — "+
			"this is the failure mode it exists for:\n%s", h.pgid, got)
	}
	if !strings.Contains(got, "sleep 60") {
		t.Fatalf("diagnostic does not name what survived:\n%s", got)
	}
}

// trackHolder2 captures a live pid's process group without going through a pid
// file (trackHolder's fixture path).
func trackHolder2(t *testing.T, pid int) trackedHolder {
	t.Helper()
	h := trackedHolder{pid: pid}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("getpgid(%d): %v", pid, err)
	}
	h.pgid = pgid
	return h
}
