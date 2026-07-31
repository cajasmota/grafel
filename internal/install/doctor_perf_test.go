//go:build perf

// Latency budget for RunQuickDoctor (#2211).
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// This is the tightest budget in the repo at 200ms. Note that the 100ms
// DaemonTimeout below is NOT part of that budget in practice: port 1 refuses
// the connection immediately, so the dial returns without the timeout ever
// elapsing and the whole call measures ~12ms. The 200ms is therefore ~16x
// headroom over the healthy path — but it is still 200ms of absolute wall
// clock on a shared runner, and it is a latency measurement rather than a
// correctness property. That RunQuickDoctor succeeds against a tampered or
// absent daemon is covered by the quick-mode tests in doctor_test.go, which
// stay in the release gate.
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
