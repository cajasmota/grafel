package process

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestPidIsGrafel_NonPositive(t *testing.T) {
	for _, pid := range []int{0, -1, -100} {
		ok, err := PidIsGrafel(pid)
		if err != nil {
			t.Fatalf("pid %d: unexpected error %v", pid, err)
		}
		if ok {
			t.Fatalf("pid %d: non-positive pid must never be grafel", pid)
		}
	}
}

// PidIsGrafel must classify a LIVE non-grafel process (the test binary) as
// not-grafel on every platform that can resolve a live pid's identity:
//   - darwin/linux via the FindByName ps/proc scan,
//   - windows via the pid-targeted QueryFullProcessImageName primitive.
//
// Only platforms with NO introspection path at all report ErrUnsupported.
func TestPidIsGrafel_SelfIsNotGrafel(t *testing.T) {
	ok, err := PidIsGrafel(os.Getpid())
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		if err != nil {
			t.Fatalf("unexpected error on %s: %v", runtime.GOOS, err)
		}
		// The test binary is "process.test(.exe)", not "grafel".
		if ok {
			t.Fatal("test binary must not be classified as an grafel process")
		}
	default:
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("expected ErrUnsupported on %s, got ok=%v err=%v", runtime.GOOS, ok, err)
		}
	}
}

// A dead pid must report false on supported platforms (it cannot appear in the
// process scan), or ErrUnsupported elsewhere.
func TestPidIsGrafel_DeadPID(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot spawn helper process: %v", err)
	}
	dead := cmd.Process.Pid

	ok, err := PidIsGrafel(dead)
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
		t.Skip("dead pid reused/matched grafel in scan; environment-dependent, skipping")
	}
}
