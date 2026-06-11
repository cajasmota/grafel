package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
)

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		return 0x7FFFFFF0 // impossible-ish pid as a fallback
	}
	return cmd.Process.Pid
}

func TestPidStillAlive(t *testing.T) {
	if !pidStillAlive(os.Getpid()) {
		t.Fatal("current process must be alive")
	}
	if pidStillAlive(0) || pidStillAlive(-5) {
		t.Fatal("non-positive pids must report dead")
	}
	if pidStillAlive(deadPID(t)) {
		t.Skip("reaped child pid considered alive (kernel reuse); skipping")
	}
}

func TestWaitForExit_AlreadyDead(t *testing.T) {
	dead := deadPID(t)
	if pidStillAlive(dead) {
		t.Skip("child pid still alive; cannot assert exit")
	}
	if !waitForExit(dead, 200*time.Millisecond) {
		t.Fatal("waitForExit must return true for an already-dead pid")
	}
}

func TestWaitForExit_LiveProcessTimesOut(t *testing.T) {
	// A sleeper we control; it stays alive past the short budget.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleeper: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	if waitForExit(cmd.Process.Pid, 150*time.Millisecond) {
		t.Fatal("waitForExit must time out (return false) for a still-running process")
	}
}

func TestForceKill_TerminatesProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleeper: %v", err)
	}
	pid := cmd.Process.Pid
	if err := forceKill(pid); err != nil {
		t.Fatalf("forceKill: %v", err)
	}
	_, _ = cmd.Process.Wait() // reap
	if !waitForExit(pid, 2*time.Second) {
		t.Fatal("process should be dead after forceKill")
	}
	// forceKill on a non-positive pid is a safe no-op.
	if err := forceKill(0); err != nil {
		t.Fatalf("forceKill(0) should be a no-op, got %v", err)
	}
}

func TestIsUnixSocketPath(t *testing.T) {
	cases := map[string]bool{
		"/home/u/.archigraph/sockets/daemon.sock": true,
		"/tmp/x.sock":                              true,
		`\\.\pipe\archigraph-daemon-user`:          false,
	}
	for path, want := range cases {
		if got := isUnixSocketPath(path); got != want {
			t.Errorf("isUnixSocketPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestStartupReadinessBudget(t *testing.T) {
	t.Setenv("ARCHIGRAPH_START_READINESS", "")
	if got := startupReadinessBudget(); got != startupReadinessDefault {
		t.Fatalf("default budget = %v, want %v", got, startupReadinessDefault)
	}

	t.Setenv("ARCHIGRAPH_START_READINESS", "180s")
	if got := startupReadinessBudget(); got != 180*time.Second {
		t.Fatalf("override budget = %v, want 180s", got)
	}

	// Default must be well above the old 5 s cliff that false-failed cold
	// starts during the initial index pass (#4549).
	if startupReadinessDefault <= 60*time.Second {
		t.Fatalf("default readiness %v must exceed the observed ~82s index time", startupReadinessDefault)
	}

	t.Setenv("ARCHIGRAPH_START_READINESS", "garbage")
	if got := startupReadinessBudget(); got != startupReadinessDefault {
		t.Fatalf("invalid override should fall back to default, got %v", got)
	}

	t.Setenv("ARCHIGRAPH_START_READINESS", "-5s")
	if got := startupReadinessBudget(); got != startupReadinessDefault {
		t.Fatalf("negative override should fall back to default, got %v", got)
	}
}

func TestCleanStaleArtifacts_RemovesDeadPidfileAndSocket(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "daemon.pid")
	sockPath := filepath.Join(dir, "daemon.sock")

	dead := deadPID(t)
	if pidStillAlive(dead) {
		t.Skip("child pid still alive; cannot assert stale cleanup")
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(dead)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sockPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cleanStaleArtifacts(&out, daemon.Layout{PIDPath: pidPath, SocketPath: sockPath})

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("stale pidfile should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("stale socket should be removed, stat err = %v", err)
	}
}

func TestCleanStaleArtifacts_KeepsLivePidfile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "daemon.pid")
	// Our own pid is alive — cleanStaleArtifacts must NOT delete a live owner's
	// pidfile (it does not know it isn't archigraph; conservatively keep it).
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cleanStaleArtifacts(&out, daemon.Layout{PIDPath: pidPath, SocketPath: filepath.Join(dir, "daemon.sock")})

	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("live-owner pidfile must be preserved, stat err = %v", err)
	}
}
