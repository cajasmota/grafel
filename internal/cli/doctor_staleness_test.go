package cli

// doctor_staleness_test.go — unit tests for the stale-daemon scan logic
// introduced in issue #857 (Layer 3).
//
// The parse helpers (parsePsEo, parsePsAux) were removed in #932 and
// replaced by internal/process. This file now tests the classification
// logic (isOrphan, isTmp) and the end-to-end scan function.

import (
	"strings"
	"testing"
)

// TestStaleProcessClassification verifies the stale detection criteria:
// - orphan /tmp binary → stale
// - canonical binary with different path than self → stale (daemon)
// - same binary → not stale
func TestStaleProcessClassification(t *testing.T) {
	selfExe := "/usr/local/bin/grafel"

	cases := []struct {
		name      string
		proc      staleProcess
		wantStale bool
	}{
		{
			name: "orphan /tmp daemon",
			proc: staleProcess{PID: 1, PPID: 1, Exe: "/tmp/arch-test/grafel",
				IsOrphan: true, IsTmp: true},
			wantStale: true,
		},
		{
			// A grafel binary installed under a directory named "daemon",
			// running from a different path than self. This is criterion 2's
			// only genuine population: "daemon" is an argument, never part of
			// the exec path, so the substring can only come from a directory
			// component (see isStaleProc).
			name: "different canonical daemon binary",
			proc: staleProcess{PID: 2, PPID: 100, Exe: "/usr/local/lib/grafel/daemon/grafel",
				IsOrphan: false, IsTmp: false},
			wantStale: true,
		},
		{
			// WAS wantStale:true here until #7268. The basename
			// "grafel-daemon-old" is not the grafel binary — a renamed copy,
			// a third-party helper, anything. Selecting it sent SIGTERM to a
			// process on nothing but a path-substring match. Leaving such a
			// process running is the conservative failure and is the one we
			// choose.
			name: "non-grafel basename with a daemon substring — NOT killed",
			proc: staleProcess{PID: 5, PPID: 100, Exe: "/usr/local/bin/grafel-daemon-old",
				IsOrphan: false, IsTmp: false},
			wantStale: false,
		},
		{
			name: "same binary as self — not stale",
			proc: staleProcess{PID: 3, PPID: 100, Exe: selfExe,
				IsOrphan: false, IsTmp: false},
			wantStale: false,
		},
		{
			name: "non-/tmp, PPID=1 but not daemon in name",
			proc: staleProcess{PID: 4, PPID: 1, Exe: "/usr/local/bin/grafel",
				IsOrphan: true, IsTmp: false},
			// PPID=1 without IsTmp doesn't trigger criterion 1.
			// Exe == selfExe so criterion 2 is false.
			wantStale: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isStaleProc(tc.proc, selfExe)
			if got != tc.wantStale {
				t.Errorf("isStaleProc(%+v, %q) = %v, want %v", tc.proc, selfExe, got, tc.wantStale)
			}
		})
	}
}

// TestRunDoctorStaleDaemons_DryRunOutputsNoneWhenClean verifies the full
// runDoctorStaleDaemons function returns a clean "none found" message when
// no stale processes exist. We call it with kill=false (dry-run default).
//
// Note: this test can only run cleanly on a machine with no stale grafel
// daemons. On a developer machine with a real daemon it may log stale entries —
// that's correct behaviour, not a test failure.
func TestRunDoctorStaleDaemons_DryRunOutputsNoneWhenClean(t *testing.T) {
	var sb strings.Builder
	if err := runDoctorStaleDaemons(&sb, false); err != nil {
		t.Fatalf("runDoctorStaleDaemons: %v", err)
	}
	out := sb.String()
	t.Logf("doctor stale scan output:\n%s", out)
	// We only assert that the function returns without error. The output
	// content is environment-dependent (depends on what's running on this machine).
	// The meaningful coverage is that it doesn't panic or crash.
}
