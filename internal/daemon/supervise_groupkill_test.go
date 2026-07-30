//go:build darwin || linux

package daemon

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestEngineChildGroupHelper is the subprocess entrypoint for
// TestEngineSupervisor_DrainKillsGrandchild: it forks a "grandchild" (a plain
// `sleep`) that inherits the helper's process group (no Setpgid of its own),
// writes the grandchild's pid to GRAFEL_GRANDCHILD_PIDFILE, and then blocks.
// It does not forward any signal to the grandchild itself — the grandchild's
// survival past drain depends ENTIRELY on whether the supervisor's
// SIGTERM/SIGKILL was delivered to the engine child's process group or just
// to the child's own pid. This is the #6044 item-4 regression shape: a
// serve-supervised child that fans out its own subprocesses (mirrors the
// `grafel extract` fanout #5999 fixed for the batch scheduler).
func TestEngineChildGroupHelper(t *testing.T) {
	if os.Getenv("GRAFEL_ENGINE_GROUP_HELPER") != "1" {
		return
	}
	gc := exec.Command("sleep", "300")
	if err := gc.Start(); err != nil {
		t.Fatalf("start grandchild: %v", err)
	}
	if pidFile := os.Getenv("GRAFEL_GRANDCHILD_PIDFILE"); pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(gc.Process.Pid)), 0o600)
	}
	// Block until this process itself is signalled (default disposition
	// terminates on SIGTERM/SIGKILL — no handler needed). Long enough to
	// outlast the test's drain timeout if left running.
	time.Sleep(300 * time.Second)
}

// helperGroupEngineCommand spawns TestEngineChildGroupHelper in its own
// process group (mirroring production engineChildSysProcAttr) and arranges
// for the grandchild pid to land in a file the test can read.
func helperGroupEngineCommand(grandchildPIDFile string) func(selfExe, root string) *exec.Cmd {
	return func(selfExe, root string) *exec.Cmd {
		cmd := exec.Command(selfExe, "-test.run=TestEngineChildGroupHelper", "-test.timeout=60s")
		cmd.Env = append(os.Environ(),
			"GRAFEL_ENGINE_GROUP_HELPER=1",
			"GRAFEL_GRANDCHILD_PIDFILE="+grandchildPIDFile,
			EnvRoot+"="+root,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = engineChildSysProcAttr()
		return cmd
	}
}

// TestEngineSupervisor_DrainKillsGrandchild is the pin for #6044 item 4: a
// graceful drain (sup.stop()) must take down not just the supervised engine
// child but everything in its process group, including grandchildren it
// forked itself. Before the group-directed signalTerminate/signalKill fix,
// terminateChild signalled only the child's own pid (p.Signal /
// cmd.Process.Kill single-target), so the grandchild — reparented but never
// signalled — survived the drain as an orphan.
func TestEngineSupervisor_DrainKillsGrandchild(t *testing.T) {
	root := isolateSupervisorEnv(t)
	pidFile := root + "/grandchild.pid"
	defer SetEngineChildCommandForTest(helperGroupEngineCommand(pidFile))()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := newTestSupervisor(t, root)
	sup.drainTimeout = 3 * time.Second
	if err := sup.start(ctx); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}

	// Wait for the grandchild pidfile to appear (helper has forked `sleep`).
	if !waitFor(t, 10*time.Second, func() bool {
		_, err := os.Stat(pidFile)
		return err == nil
	}) {
		t.Fatal("grandchild pidfile never appeared — helper did not start")
	}
	gcPID := readIntFile(t, pidFile)
	if gcPID <= 0 {
		t.Fatalf("invalid grandchild pid read from %s: %d", pidFile, gcPID)
	}
	if !pidAliveUnix(gcPID) {
		t.Fatalf("grandchild pid %d not alive right after spawn", gcPID)
	}
	childPID := sup.getChildPID()
	if childPID == 0 {
		t.Fatal("supervisor reports no engine child pid")
	}

	sup.stop()

	if !waitFor(t, 6*time.Second, func() bool { return !pidAliveUnix(childPID) }) {
		t.Fatalf("engine child %d still alive after drain", childPID)
	}
	if !waitFor(t, 6*time.Second, func() bool { return !pidAliveUnix(gcPID) }) {
		t.Fatalf("grandchild %d (forked by the engine child) survived the drain — orphaned, "+
			"which is the exact #6044 item-4 defect: the drain signal was not group-directed", gcPID)
	}
}

func readIntFile(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// TestSignalTerminate_IsGroupDirected is a narrower unit-level pin: it spawns
// a child in its own process group with a grandchild inside that same group,
// and asserts signalTerminate alone (not the full supervisor drain path)
// reaches the grandchild too.
func TestSignalTerminate_IsGroupDirected(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	// Parent shell forks the grandchild `sleep` inside its own new group.
	parent := exec.Command("sh", "-c", "sleep 300 & echo $! ; wait")
	parent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := parent.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := parent.Start(); err != nil {
		t.Fatalf("start parent shell: %v", err)
	}
	buf := make([]byte, 32)
	n, _ := stdout.Read(buf)
	gcPID, convErr := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if convErr != nil || gcPID <= 0 {
		_ = parent.Process.Kill()
		t.Fatalf("could not parse grandchild pid from shell output %q: %v", string(buf[:n]), convErr)
	}
	if !waitFor(t, 3*time.Second, func() bool { return pidAliveUnix(gcPID) }) {
		t.Fatalf("grandchild %d never came up", gcPID)
	}

	if err := signalTerminate(parent.Process); err != nil {
		t.Fatalf("signalTerminate: %v", err)
	}

	if !waitFor(t, 3*time.Second, func() bool { return !pidAliveUnix(gcPID) }) {
		_ = syscall.Kill(-parent.Process.Pid, syscall.SIGKILL)
		t.Fatalf("grandchild %d survived signalTerminate — not group-directed", gcPID)
	}
	_ = parent.Wait()
}
