package daemon

import (
	"bytes"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

// pinRuntimeMemLimit saves and restores the process-global Go soft memory
// limit so a test that calls applyMemoryLimit cannot leak a tight (or lax)
// limit into sibling tests.
func pinRuntimeMemLimit(t *testing.T) {
	t.Helper()
	prev := debug.SetMemoryLimit(-1) // -1 reads without changing
	debug.SetMemoryLimit(prev)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
}

// TestSplitMemLimitMB_SharesSumToTotal is the core arithmetic of #6045: the
// installation gets ONE budget, split across the two processes. serve+engine
// must equal the advertised total EXACTLY (engine absorbs the rounding), and
// neither plane may be handed the whole thing.
//
// This is the test that fails if the split is reverted to per-process-full.
func TestSplitMemLimitMB_SharesSumToTotal(t *testing.T) {
	// 2MB is a degenerate lower bound (the real floor is memLimitFloorMB);
	// it is here only to prove the arithmetic never hands one plane everything.
	for _, total := range []int64{2, 2048, 2560, 3000, 4096, 10000} {
		serve, engine := SplitMemLimitMB(total)
		if serve+engine != total {
			t.Errorf("total=%d: serve(%d)+engine(%d)=%d, want exactly %d",
				total, serve, engine, serve+engine, total)
		}
		if serve >= total {
			t.Errorf("total=%d: serve share %d must be strictly less than the total (per-process-full is the bug)", total, serve)
		}
		if engine >= total {
			t.Errorf("total=%d: engine share %d must be strictly less than the total (per-process-full is the bug)", total, engine)
		}
		if serve < 0 || engine < 0 {
			t.Errorf("total=%d: negative share serve=%d engine=%d", total, serve, engine)
		}
	}
}

// TestSplitMemLimitMB_EngineGetsMajority pins the RATIO, not just the sum:
// the engine is the write plane (scheduler/extraction/fbwriter) and does the
// heavy allocation, while serve's large data (graph_cache mmap) is off-heap
// and therefore not counted by GOMEMLIMIT at all. A 50/50 split would starve
// the engine, whose measured per-job peak heap is 1-1.5GB.
func TestSplitMemLimitMB_EngineGetsMajority(t *testing.T) {
	if MemLimitServeShare >= 0.5 {
		t.Fatalf("MemLimitServeShare=%v: the engine (write plane) must get the majority", MemLimitServeShare)
	}
	const total = int64(2560)
	serve, engine := SplitMemLimitMB(total)
	if engine <= serve {
		t.Fatalf("total=%d: engine share %d must exceed serve share %d", total, engine, serve)
	}
	wantServe := int64(float64(total) * MemLimitServeShare)
	if serve != wantServe {
		t.Errorf("serve share = %d, want %d (%.0f%% of %d)", serve, wantServe, MemLimitServeShare*100, total)
	}
	// The engine must still clear the measured 1.5GB per-job peak on the
	// default 2560MB ceiling.
	if engine < 1536 {
		t.Errorf("engine share %dMB is below the measured 1536MB per-job peak heap", engine)
	}
}

// TestSplitMemLimitMB_DisabledStaysDisabled: a non-positive total means the
// daemon-applied limit is off; splitting must not synthesize one.
func TestSplitMemLimitMB_DisabledStaysDisabled(t *testing.T) {
	for _, total := range []int64{0, -1} {
		serve, engine := SplitMemLimitMB(total)
		if serve > 0 || engine > 0 {
			t.Errorf("total=%d: want both shares <=0, got serve=%d engine=%d", total, serve, engine)
		}
	}
}

// TestMemPlaneShareOf_MonolithGetsWholeBudget: in monolith mode
// (GRAFEL_SPLIT_MODE=0) there is exactly ONE process, so it must get the whole
// installation budget. This is the test that fails if the split is applied
// unconditionally and monolith is accidentally halved.
func TestMemPlaneShareOf_MonolithGetsWholeBudget(t *testing.T) {
	const total = int64(2560)
	if got := memPlaneMonolith.shareOf(total); got != total {
		t.Errorf("monolith share = %d, want the whole budget %d", got, total)
	}
	serve, engine := SplitMemLimitMB(total)
	if got := memPlaneServe.shareOf(total); got != serve {
		t.Errorf("serve plane share = %d, want %d", got, serve)
	}
	if got := memPlaneEngine.shareOf(total); got != engine {
		t.Errorf("engine plane share = %d, want %d", got, engine)
	}
}

// applyAndCapture runs applyMemoryLimit for a plane against a JSON logger and
// returns the resulting runtime limit (bytes) plus the captured log text.
func applyAndCapture(t *testing.T, plane memPlane) (int64, string) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	applyMemoryLimit(logger, plane)
	return debug.SetMemoryLimit(-1), buf.String()
}

// TestApplyMemoryLimit_PerPlaneShare exercises the REAL applyMemoryLimit and
// asserts each plane sets the runtime limit to its SHARE, not the total — and
// that the log line says so. #6045 is a bug visible in the log, so the log
// content is part of the contract.
func TestApplyMemoryLimit_PerPlaneShare(t *testing.T) {
	pinRuntimeMemLimit(t)
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv(memLimitEnv, "10000") // deterministic total, no host-RAM dependency

	serveMB, engineMB := SplitMemLimitMB(10000)

	cases := []struct {
		name   string
		plane  memPlane
		wantMB int64
	}{
		{"serve", memPlaneServe, serveMB},
		{"engine", memPlaneEngine, engineMB},
		{"monolith", memPlaneMonolith, 10000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBytes, logged := applyAndCapture(t, tc.plane)
			if want := tc.wantMB * 1024 * 1024; gotBytes != want {
				t.Errorf("runtime limit = %d bytes (%dMB), want %d bytes (%dMB)",
					gotBytes, gotBytes/1024/1024, want, tc.wantMB)
			}
			// The log must report the applied share, the installation total,
			// and which plane applied it.
			for _, want := range []string{
				`"limit_mb":` + strconv.FormatInt(tc.wantMB, 10),
				`"total_mb":10000`,
				`"plane":"` + tc.plane.String() + `"`,
			} {
				if !strings.Contains(logged, want) {
					t.Errorf("log line missing %s\ngot: %s", want, logged)
				}
			}
		})
	}
}

// TestApplyMemoryLimit_DisabledPerPlane: with the limit disabled, no plane may
// set a limit (and the disabled log line must not claim one was applied).
func TestApplyMemoryLimit_DisabledPerPlane(t *testing.T) {
	pinRuntimeMemLimit(t)
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv(memLimitEnv, "4096")
	applyMemoryLimit(nil, memPlaneMonolith)
	before := debug.SetMemoryLimit(-1)

	t.Setenv(memLimitEnv, "off")
	for _, p := range []memPlane{memPlaneMonolith, memPlaneServe, memPlaneEngine} {
		applyMemoryLimit(nil, p)
		if got := debug.SetMemoryLimit(-1); got != before {
			t.Errorf("plane %s: disabled path changed the limit %d -> %d", p, before, got)
		}
	}
}

// TestMemLimitPlaneSummary_ReportsBothShares backs the operator-facing surface:
// grafel status must be able to print the effective total AND both shares.
// Reporting only one share (the current 2x-understating behaviour) fails here.
func TestMemLimitPlaneSummary_ReportsBothShares(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv(memLimitEnv, "10000")

	t.Setenv(SplitModeEnvVar, "1")
	total, serve, engine, _, split := MemLimitPlaneSummary()
	if !split {
		t.Fatal("split mode on: want split=true")
	}
	if total != 10000 {
		t.Errorf("total = %d, want 10000", total)
	}
	if serve+engine != total {
		t.Errorf("shares %d+%d do not sum to the advertised total %d", serve, engine, total)
	}
	if serve == total || engine == total {
		t.Errorf("a share equals the total (serve=%d engine=%d total=%d): status would understate real consumption by 2x", serve, engine, total)
	}

	t.Setenv(SplitModeEnvVar, "0")
	total, serve, engine, _, split = MemLimitPlaneSummary()
	if split {
		t.Fatal("monolith mode: want split=false")
	}
	if total != 10000 || serve != 10000 {
		t.Errorf("monolith: total=%d serve=%d, want the whole 10000 in one process", total, serve)
	}
	if engine != 0 {
		t.Errorf("monolith: engine share = %d, want 0 (no engine process)", engine)
	}
}
