//go:build linux

package process

// process_linux_exe_test.go — #7211: readProcInfo must not silently swallow the
// /proc/<pid>/exe readlink error.
//
// The distinction matters because comm and stat keep reading for a zombie or
// mid-exit process while the exe link is already gone. Before the fix the
// caller received Info{Exe: ""} indistinguishable from a successful read of a
// process with no path, and selfdefense's findCanonicalDaemon fell back to the
// bare comm — misclassifying an exiting sibling as the canonical daemon.

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestReadProcInfo_SelfHasExeAndNoError is the positive control: for a healthy,
// readable process the fix must change nothing — Exe is populated and ExeErr is
// nil. Without this row a fix that unconditionally blanked Exe would pass the
// zombie assertion below.
func TestReadProcInfo_SelfHasExeAndNoError(t *testing.T) {
	info, err := readProcInfo(os.Getpid())
	if err != nil {
		t.Fatalf("readProcInfo(self): %v", err)
	}
	if info.Exe == "" {
		t.Errorf("readProcInfo(self).Exe is empty; want the test binary's path")
	}
	if info.ExeErr != nil {
		t.Errorf("readProcInfo(self).ExeErr = %v, want nil", info.ExeErr)
	}
	if info.Name == "" {
		t.Errorf("readProcInfo(self).Name is empty")
	}
}

// TestReadProcInfo_ZombieReportsExeError reproduces the #7211 window directly.
// A child that has exited but not been reaped is a zombie: the kernel drops
// /proc/<pid>/exe (readlink returns ENOENT) while /proc/<pid>/comm and
// /proc/<pid>/stat still read. The caller must be able to tell that apart from
// a real answer.
func TestReadProcInfo_ZombieReportsExeError(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start child: %v", err)
	}
	pid := cmd.Process.Pid
	// Deliberately do NOT Wait until the assertions are done — the child stays
	// a zombie until it is reaped, so the window below is not a race.
	t.Cleanup(func() { _ = cmd.Wait() })

	deadline := time.Now().Add(5 * time.Second)
	var info Info
	for {
		var err error
		info, err = readProcInfo(pid)
		if err != nil {
			t.Fatalf("readProcInfo(%d) failed outright: %v (comm/stat should still read for a zombie)", pid, err)
		}
		if info.ExeErr != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d never became a zombie with an unreadable exe link (Exe=%q)", pid, info.Exe)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if info.Exe != "" {
		t.Errorf("Exe = %q, want \"\" when the exe link is unreadable", info.Exe)
	}
	if info.Name == "" {
		t.Errorf("Name is empty; comm should still be readable for a zombie")
	}
	if info.PID != pid {
		t.Errorf("PID = %d, want %d", info.PID, pid)
	}
}
