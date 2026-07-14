package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestPopulateProcessMetrics_ReportsCurrentProcessRSS is the RED test for the
// wizard CPU/RAM readout: the engine-liveness heartbeat writer must stamp its
// OWN process's RSS (in MB) onto the statusfile.File it writes. RSS is the
// must-have signal (it shows the multi-GB enrichment-phase peak); CPU% is
// best-effort and may legitimately be 0 on a platform/measurement hiccup, so
// only RSSMB is asserted strictly here.
func TestPopulateProcessMetrics_ReportsCurrentProcessRSS(t *testing.T) {
	f := &statusfile.File{EnginePID: os.Getpid()}
	populateProcessMetrics(f)

	if f.RSSMB <= 0 {
		t.Fatalf("RSSMB = %d, want > 0 for the current (live) process", f.RSSMB)
	}
	// CPUPct must never be negative — best-effort zero is fine, but a negative
	// value would indicate a broken sample rather than "unavailable".
	if f.CPUPct < 0 {
		t.Errorf("CPUPct = %v, want >= 0", f.CPUPct)
	}
}

// TestStartEngineLivenessHeartbeat_PopulatesRSS proves the production
// heartbeat writer (not just the helper in isolation) publishes RSSMB>0 onto
// the on-disk engine-liveness sidecar, so a wizard TUI reading
// EngineLivenessStatus sees the metric with no further wiring.
func TestStartEngineLivenessHeartbeat_PopulatesRSS(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	root := t.TempDir()

	stop := startEngineLivenessHeartbeat(root, 0, nil, nil)
	t.Cleanup(stop)

	// The writer's first write happens inside its own goroutine (fired right
	// after startup, before the ticker loop) — poll briefly rather than racing
	// it, matching the pattern in TestOnRepoStatesChanged_TriggersStatusFileRefresh.
	deadline := time.Now().Add(2 * time.Second)
	var f *statusfile.File
	var fresh bool
	for time.Now().Before(deadline) {
		f, fresh = EngineLivenessStatus(root)
		if fresh && f != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !fresh || f == nil {
		t.Fatalf("EngineLivenessStatus: fresh=%v f=%v", fresh, f)
	}
	if f.RSSMB <= 0 {
		t.Errorf("RSSMB = %d, want > 0", f.RSSMB)
	}
}
