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

// TestCPUSampler_ObserveComputesBoundedDelta is the RED test for the
// Linux-correct CPU% fix: the sampler must turn CUMULATIVE CPU-seconds into a
// bounded instantaneous PERCENTAGE via a delta across successive heartbeat
// writes — NOT stamp the raw ever-rising cumulative value (which on Linux
// would render "CPU 9843%" and climbing).
func TestCPUSampler_ObserveComputesBoundedDelta(t *testing.T) {
	var s cpuSampler
	t0 := time.Unix(1_000_000, 0)

	// First sample: baseline only, no interval to divide by → 0 (readout omits CPU).
	if pct := s.observe(10.0, t0); pct != 0 {
		t.Errorf("first observe = %v, want 0 (baseline only)", pct)
	}

	// Second sample: 4 CPU-seconds burned over 1 wall-second → 400% (a real,
	// multi-core-capable percentage), NOT the raw cumulative 14.
	pct := s.observe(14.0, t0.Add(1*time.Second))
	if pct == 14.0 {
		t.Fatalf("observe returned the raw cumulative value %v — must be a delta percentage", pct)
	}
	if got, want := pct, 400.0; got != want {
		t.Errorf("observe = %v%%, want %v%% (Δ4 cpu-sec / 1 wall-sec)", got, want)
	}

	// Third sample: 0.5 CPU-seconds over 2 wall-seconds → 25%.
	if got := s.observe(14.5, t0.Add(3*time.Second)); got != 25.0 {
		t.Errorf("observe = %v%%, want 25%%", got)
	}
}

// TestCPUSampler_GuardsFirstSampleAndZeroInterval covers the degrade-to-zero
// guards: a zero/negative wall interval and a counter reset both yield 0 (so
// the readout omits CPU that tick) rather than a divide-by-zero or a negative %.
func TestCPUSampler_GuardsFirstSampleAndZeroInterval(t *testing.T) {
	var s cpuSampler
	t0 := time.Unix(2_000_000, 0)
	_ = s.observe(5.0, t0) // baseline

	// Same wall-clock instant → zero interval → guard returns 0.
	if got := s.observe(9.0, t0); got != 0 {
		t.Errorf("zero-interval observe = %v, want 0", got)
	}
	// Note: the previous call still advanced the baseline to (9.0, t0). A CPU
	// counter reset (cpuSeconds < prev) over a positive interval → negative
	// delta → guard returns 0.
	if got := s.observe(1.0, t0.Add(1*time.Second)); got != 0 {
		t.Errorf("counter-reset observe = %v, want 0", got)
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
