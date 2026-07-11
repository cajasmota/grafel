package install

// doctor_engine_test.go — whitebox tests for checkEngineLiveness (ADR-0024
// PR5, epic #5729). These exercise checkEngineLiveness directly via an
// injected engineLivenessDeps so they never touch a real daemon root, a real
// engine.pid file, or a real statusfile on disk, and never depend on whether
// GRAFEL_SPLIT_MODE happens to be set in the test environment.
//
// Scenarios (per the PR5 TDD requirement):
//   (a) monolith mode (no engine.pid)      → NO "engine down" warning
//   (b) split mode, fresh heartbeat         → healthy
//   (c) split mode, stale heartbeat         → "engine degraded"
//   (d) split mode, version skew            → "engine degraded" / version skew

import (
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

func fakeEngineLivenessDeps(t *testing.T, pid int, pidErr error, f *statusfile.File, fErr error, stale time.Duration) engineLivenessDeps {
	t.Helper()
	return engineLivenessDeps{
		root: func() (string, error) { return t.TempDir(), nil },
		readEnginePID: func(string) (int, error) {
			if pidErr != nil {
				return 0, pidErr
			}
			return pid, nil
		},
		readLiveness: func(string) (*statusfile.File, error) {
			if fErr != nil {
				return nil, fErr
			}
			return f, nil
		},
		staleAfter: func() time.Duration { return stale },
	}
}

// (a) monolith mode: no engine.pid at all — must be OK, Info severity, and
// must NOT say "engine down"/"degraded".
func TestCheckEngineLiveness_MonolithMode_NoEngineDownWarning(t *testing.T) {
	deps := fakeEngineLivenessDeps(t, 0, os.ErrNotExist, nil, nil, 15*time.Second)
	cr := checkEngineLiveness(&State{}, deps)

	if !cr.OK {
		t.Fatalf("monolith mode must report OK=true, got drift=%v", cr.Drift)
	}
	if cr.Severity != SeverityInfo {
		t.Errorf("monolith mode severity = %q, want info", cr.Severity)
	}
	for _, d := range cr.Drift {
		if containsFold(d, "engine down") || containsFold(d, "degraded") {
			t.Errorf("monolith mode must never say engine down/degraded, got: %q", d)
		}
	}
}

// (b) split mode, fresh heartbeat, matching pid, matching version → healthy.
func TestCheckEngineLiveness_SplitMode_FreshHeartbeat_Healthy(t *testing.T) {
	f := &statusfile.File{
		EnginePID:   4242,
		HeartbeatAt: time.Now(),
		Version:     "v9.9.9",
	}
	deps := fakeEngineLivenessDeps(t, 4242, nil, f, nil, 15*time.Second)
	state := &State{DaemonVersion: "v9.9.9"}
	cr := checkEngineLiveness(state, deps)

	if !cr.OK {
		t.Fatalf("fresh heartbeat + matching pid + matching version must be OK, got drift=%v", cr.Drift)
	}
}

// (c) split mode, stale heartbeat → "engine degraded".
func TestCheckEngineLiveness_SplitMode_StaleHeartbeat_Degraded(t *testing.T) {
	f := &statusfile.File{
		EnginePID:   4242,
		HeartbeatAt: time.Now().Add(-1 * time.Hour),
		Version:     "v9.9.9",
	}
	deps := fakeEngineLivenessDeps(t, 4242, nil, f, nil, 15*time.Second)
	state := &State{DaemonVersion: "v9.9.9"}
	cr := checkEngineLiveness(state, deps)

	if cr.OK {
		t.Fatal("stale heartbeat must not be OK")
	}
	if cr.Severity != SeverityWarning {
		t.Errorf("stale heartbeat severity = %q, want warning", cr.Severity)
	}
	if len(cr.Drift) == 0 || !containsFold(cr.Drift[0], "degraded") {
		t.Errorf("stale heartbeat drift = %v, want mention of 'degraded'", cr.Drift)
	}
}

// split mode, liveness pid mismatch (stale liveness file from a since-reaped
// engine, before a new one has stamped fresh liveness) → degraded.
func TestCheckEngineLiveness_SplitMode_PIDMismatch_Degraded(t *testing.T) {
	f := &statusfile.File{
		EnginePID:   9999, // doesn't match engine.pid below
		HeartbeatAt: time.Now(),
		Version:     "v9.9.9",
	}
	deps := fakeEngineLivenessDeps(t, 4242, nil, f, nil, 15*time.Second)
	cr := checkEngineLiveness(&State{}, deps)

	if cr.OK {
		t.Fatal("pid mismatch must not be OK")
	}
	if cr.Severity != SeverityWarning {
		t.Errorf("pid mismatch severity = %q, want warning", cr.Severity)
	}
}

// (d) split mode, version skew (serve's recorded binary_version differs from
// the engine child's self-reported version) → degraded / version-skew note.
func TestCheckEngineLiveness_SplitMode_VersionSkew_Degraded(t *testing.T) {
	f := &statusfile.File{
		EnginePID:   4242,
		HeartbeatAt: time.Now(),
		Version:     "v1.0.0-old-engine",
	}
	deps := fakeEngineLivenessDeps(t, 4242, nil, f, nil, 15*time.Second)
	state := &State{DaemonVersion: "v2.0.0-new-serve"}
	cr := checkEngineLiveness(state, deps)

	if cr.OK {
		t.Fatal("version skew must not be OK")
	}
	if len(cr.Drift) == 0 || !containsFold(cr.Drift[0], "version skew") {
		t.Errorf("version-skew drift = %v, want mention of 'version skew'", cr.Drift)
	}
}

// Liveness statusfile unreadable while engine.pid IS present (split mode
// active but the liveness file vanished/corrupted) → degraded, not a panic,
// not a false "healthy".
func TestCheckEngineLiveness_SplitMode_LivenessUnreadable_Degraded(t *testing.T) {
	deps := fakeEngineLivenessDeps(t, 4242, nil, nil, os.ErrNotExist, 15*time.Second)
	cr := checkEngineLiveness(&State{}, deps)

	if cr.OK {
		t.Fatal("unreadable liveness file while engine.pid is present must not be OK")
	}
	if cr.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", cr.Severity)
	}
}

// Cannot resolve daemon root at all → Info, not a crash, not "engine down".
func TestCheckEngineLiveness_RootResolutionFails_Info(t *testing.T) {
	deps := engineLivenessDeps{
		root:          func() (string, error) { return "", os.ErrPermission },
		readEnginePID: func(string) (int, error) { return 0, os.ErrNotExist },
		readLiveness:  func(string) (*statusfile.File, error) { return nil, os.ErrNotExist },
		staleAfter:    func() time.Duration { return 15 * time.Second },
	}
	cr := checkEngineLiveness(&State{}, deps)
	if cr.Severity != SeverityInfo {
		t.Errorf("severity = %q, want info", cr.Severity)
	}
}

// ── looksLikePreSplitUnit ──────────────────────────────────────────────────

func TestLooksLikePreSplitUnit_LaunchdLegacy(t *testing.T) {
	if !looksLikePreSplitUnit("<string>daemon</string>") {
		t.Error("must detect legacy launchd daemon argument")
	}
}

func TestLooksLikePreSplitUnit_LaunchdCurrent(t *testing.T) {
	if looksLikePreSplitUnit("<string>serve</string>") {
		t.Error("must not flag current 'serve' plist as pre-split")
	}
}

func TestLooksLikePreSplitUnit_SystemdLegacy(t *testing.T) {
	unit := "Description=grafel knowledge-graph daemon\nExecStart=/usr/local/bin/grafel daemon\n"
	if !looksLikePreSplitUnit(unit) {
		t.Error("must detect legacy systemd ExecStart ...daemon")
	}
}

func TestLooksLikePreSplitUnit_SystemdCurrent(t *testing.T) {
	unit := "Description=grafel knowledge-graph daemon\nExecStart=/usr/local/bin/grafel serve\n"
	if looksLikePreSplitUnit(unit) {
		t.Error("must not flag current systemd unit as pre-split (Description containing 'daemon' must not false-positive)")
	}
}

func TestLooksLikePreSplitUnit_WindowsLegacy(t *testing.T) {
	if !looksLikePreSplitUnit("<Arguments>daemon</Arguments>") {
		t.Error("must detect legacy Windows Task Scheduler daemon argument")
	}
}

func TestLooksLikePreSplitUnit_WindowsCurrent(t *testing.T) {
	if looksLikePreSplitUnit("<Arguments>serve</Arguments>") {
		t.Error("must not flag current Windows task XML as pre-split")
	}
}

// containsFold is a tiny case-insensitive substring helper local to this test
// file (avoids importing strings.Contains + strings.ToLower boilerplate at
// every call site above).
func containsFold(s, substr string) bool {
	return len(s) >= len(substr) && indexFold(s, substr) >= 0
}

func indexFold(s, substr string) int {
	sl := []rune(s)
	bl := []rune(substr)
	for i := 0; i+len(bl) <= len(sl); i++ {
		match := true
		for j := range bl {
			a, b := sl[i+j], bl[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
