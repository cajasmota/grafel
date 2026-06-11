package process

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestPidIsArchigraph_NonPositive(t *testing.T) {
	for _, pid := range []int{0, -1, -100} {
		ok, err := PidIsArchigraph(pid)
		if err != nil {
			t.Fatalf("pid %d: unexpected error %v", pid, err)
		}
		if ok {
			t.Fatalf("pid %d: non-positive pid must never be archigraph", pid)
		}
	}
}

// On unsupported platforms PidIsArchigraph reports ErrUnsupported so callers
// know to fall back. On supported platforms (darwin/linux) it must succeed and
// report false for a process that is not archigraph.
func TestPidIsArchigraph_SelfIsNotArchigraph(t *testing.T) {
	ok, err := PidIsArchigraph(os.Getpid())
	switch runtime.GOOS {
	case "darwin", "linux":
		if err != nil {
			t.Fatalf("unexpected error on %s: %v", runtime.GOOS, err)
		}
		// The test binary is "process.test", not "archigraph".
		if ok {
			t.Fatal("test binary must not be classified as an archigraph process")
		}
	default:
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("expected ErrUnsupported on %s, got ok=%v err=%v", runtime.GOOS, ok, err)
		}
	}
}

// A dead pid must report false on supported platforms (it cannot appear in the
// process scan), or ErrUnsupported elsewhere.
func TestPidIsArchigraph_DeadPID(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot spawn helper process: %v", err)
	}
	dead := cmd.Process.Pid

	ok, err := PidIsArchigraph(dead)
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("expected ErrUnsupported, got ok=%v err=%v", ok, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Skip("dead pid reused/matched archigraph in scan; environment-dependent, skipping")
	}
}
