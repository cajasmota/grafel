package daemon

// guard_6053_liveness_test.go — no private process-liveness probes in this
// package's production code.
//
// #6053: reaper.go carried its own pidAliveProbe, a signal-0 existence check
// whose own doc comment said "portable on darwin/linux". On Windows
// (*os.Process).Signal returns EWINDOWS for anything but Kill, so that probe
// answered "dead" for EVERY live pid. watchreg.Sweep checks !Alive BEFORE the
// ownership comparison, so on Windows every registered `grafel watch` entry
// was classified dead and dropped each sweep — the daemon lost its whole
// watcher inventory, and the fail-closed orphan contract #5933 established was
// unreachable dead code there. internal/process.IsAlive has had a correct
// OpenProcess/GetExitCodeProcess implementation for Windows all along;
// pidfile.go's pidAlive already used it, the reaper did not.
//
// The behavioural difference is invisible on unix — both probes are right
// there — so no unix test can catch a reintroduction, which is exactly how the
// second copy survived review. This guard can: it fails the moment a signal-0
// liveness probe reappears in package daemon. The platform split belongs in
// internal/process and nowhere else.

import (
	"os"
	"strings"
	"testing"
)

func TestNoPrivateSignalZeroLivenessProbe(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	scanned := 0
	sawReaper := false
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if name == "reaper.go" {
			sawReaper = true
		}
		src := string(b)
		for _, needle := range []string{"syscall.Signal(0)", "syscall.Signal(0x0)"} {
			if strings.Contains(src, needle) {
				t.Errorf("%s contains %q — a signal-0 liveness probe always reports DEAD "+
					"on Windows. Use process.IsAlive (pidAlive); the platform split lives "+
					"in internal/process (#6053).", name, needle)
			}
		}
	}

	// A guard that scanned nothing proves nothing.
	if scanned == 0 || !sawReaper {
		t.Fatalf("guard scanned %d production files and %s reaper.go — wrong working directory?",
			scanned, map[bool]string{true: "saw", false: "did not see"}[sawReaper])
	}
}
