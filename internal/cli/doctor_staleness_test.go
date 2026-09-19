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

	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/testsupport"
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

// TestRunDoctorStaleDaemons_DryRunOutputsNoneWhenClean verifies that
// runDoctorStaleDaemons reports "none found" when nothing is stale.
//
// IT USED TO RUN AGAINST THE REAL HOST (#7268 round 5). It installed neither
// seam, so findProcs enumerated the live process table and killProc was still
// the real SIGTERM — a reviewer's panic probe fired here and named the user's
// launchd-managed daemon as the process the loop had reached. Its only defence
// was the kill=false argument: one token, ungraded, on a machine where this
// repo's own agents routinely leave orphaned /tmp/<worktree>/grafel processes
// with PPID=1 that satisfy criterion 1 with no predicate regression at all.
//
// It also asserted nothing. The comment conceded the output was
// "environment-dependent" and the body only checked for a nil error, so on a
// developer machine with a real daemon the "none found" the name promises was
// not a property of anything. With a synthetic table both problems go away: the
// assertion becomes real AND the scan can no longer see a host process.
func TestRunDoctorStaleDaemons_DryRunOutputsNoneWhenClean(t *testing.T) {
	// A non-empty table of processes that are all somebody else's binary, so
	// "none found" is the selection talking rather than an empty input.
	withProcs7268(t, []process.Info{
		{PID: 35001, PPID: 1, Name: "helper",
			Exe: testsupport.AbsFixture("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")},
		{PID: 35002, PPID: 400, Name: "esbuild",
			Exe: testsupport.AbsFixture("/Users/jane/src/grafel/node_modules/.bin/esbuild")},
	})
	killed := withNoKills(t)

	var sb strings.Builder
	if err := runDoctorStaleDaemons(&sb, false); err != nil {
		t.Fatalf("runDoctorStaleDaemons: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "none found") {
		t.Errorf("want the 'stale daemons: none found' line; got:\n%s", out)
	}
	if strings.Contains(out, " pid=") {
		t.Errorf("a table of foreign binaries produced kill candidates:\n%s", out)
	}
	if len(*killed) != 0 {
		t.Errorf("dry run signalled PIDs %v", *killed)
	}
}
