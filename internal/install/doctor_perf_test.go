//go:build perf

// Latency budget for RunQuickDoctor (#2211).
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// This is the tightest budget in the repo: 200ms total, of which 100ms is the
// daemon dial timeout the test itself configures, leaving ~100ms of slack for
// process start, SHA hashing and everything the runner is doing at the same
// time. It is a latency measurement, not a correctness property — that
// RunQuickDoctor succeeds against a tampered/absent daemon is covered by the
// quick-mode tests in doctor_test.go, which stay in the release gate.
//
//	go test -tags perf ./internal/install/ -run QuickMode_Timing -v

package install_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install"
)

func TestDoctorQuickMode_Timing(t *testing.T) {
	env := newDoctorTestEnv(t)

	var buf bytes.Buffer
	opts := install.QuickOptions{
		StatePath:     env.statePath,
		DaemonPort:    1, // unreachable
		DaemonTimeout: 100 * time.Millisecond,
		Out:           &buf,
	}

	start := time.Now()
	if err := install.RunQuickDoctor(opts); err != nil {
		t.Errorf("RunQuickDoctor: %v", err)
	}
	elapsed := time.Since(start)

	// Budget: 200ms (100ms daemon timeout + SHA + overhead).
	if elapsed > 200*time.Millisecond {
		t.Errorf("RunQuickDoctor took %s, want <200ms", elapsed)
	}
	t.Logf("quick-doctor elapsed: %s", elapsed)
}
