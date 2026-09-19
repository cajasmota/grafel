package cli

// doctor_staleness_test.go — unit tests for the stale-daemon scan logic
// introduced in issue #857 (Layer 3).
//
// The parse helpers (parsePsEo, parsePsAux) were removed in #932 and
// replaced by internal/process. This file now tests the classification
// logic (isOrphan, isTmp) and the end-to-end scan function.

import (
	"runtime"
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
	// PLATFORM. THIS TABLE WENT RED ON THE WINDOWS LEG OF PR #7281, and it is
	// the fourth instance of one defect on this branch. isStaleProc gates every
	// criterion on daemon.IsCanonicalBinaryPath, which opens with
	// filepath.IsAbs — and "/usr/local/lib/grafel/daemon/grafel" is absolute on
	// unix and NOT absolute on windows, which has no volume in it. So both
	// wantStale:true rows inverted there while every forbidden row passed for
	// the unrelated reason that nothing looked absolute.
	//
	// Three adversarial reviews and seven rounds enumerated these tables BY HAND
	// and fixed three of them; this one was in none of the lists, and CI found
	// it in twenty minutes. internal/testsupport.ScanUnroutedFixtures now
	// derives the list from code shape instead, and TestNoUnroutedFixtures_7268
	// in this package fails if a fifth table is ever added unrouted.
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")

	cases := []struct {
		name      string
		proc      staleProcess
		wantStale bool
		// tmpPrefix marks a row whose Exe carries a literal "/tmp" prefix.
		// Such a row CANNOT be routed through AbsFixture — criterion 1 is
		// strings.HasPrefix(exe, "/tmp/"), a byte comparison, so grafting a
		// volume on moves the fixture off the boundary — and it is therefore
		// not absolute on windows and is SKIPPED there rather than asserted.
		// Same flag, same meaning, as the three sibling tables.
		tmpPrefix bool
	}{
		{
			name: "orphan /tmp daemon",
			proc: staleProcess{PID: 1, PPID: 1, Exe: "/tmp/arch-test/grafel",
				IsOrphan: true, IsTmp: true},
			wantStale: true,
			tmpPrefix: true,
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
			// Exe is selfExe, which is ALREADY routed above and gets routed
			// again by the loop. That is safe precisely because AbsFixture is
			// idempotent (#7268 round 6): without that, this row would become
			// "C:C:/usr/local/bin/grafel", which windows' filepath.IsAbs
			// rejects, and the row would fail as "not absolute" on a path that
			// visibly starts with a volume.
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

	// COUNT FLOOR. A skipped subtest reports SUCCESS, and so does a row that
	// quietly stopped being routed. Declared so that adding the flag to a row —
	// which retires that row's coverage on every platform's windows leg — fails
	// loudly instead of passing quietly. Same pattern as the sibling tables.
	const tmpPrefixRows = 1
	flagged := 0
	for _, tc := range cases {
		if tc.tmpPrefix {
			flagged++
		}
	}
	if flagged != tmpPrefixRows {
		t.Fatalf("%d rows are marked tmpPrefix, want %d — change the constant deliberately, "+
			"or drop the flag", flagged, tmpPrefixRows)
	}
	wantRun := len(cases)
	if runtime.GOOS == "windows" {
		wantRun -= tmpPrefixRows
	}
	ran := 0

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tmpPrefix && runtime.GOOS == "windows" {
				t.Skip("criterion 1 is a hard-coded unix /tmp prefix; see tmpPrefix")
			}
			ran++
			proc := tc.proc
			if !tc.tmpPrefix {
				proc.Exe = testsupport.AbsFixture(proc.Exe)
			}
			got := isStaleProc(proc, selfExe)
			if got != tc.wantStale {
				t.Errorf("isStaleProc(%+v, %q) = %v, want %v", proc, selfExe, got, tc.wantStale)
			}
		})
	}

	if ran != wantRun {
		t.Errorf("%d of %d rows ran on %s, want %d — a widened skip grades less while still reporting ok",
			ran, len(cases), runtime.GOOS, wantRun)
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
	//
	// THE COUNT IS DECLARED, not derived from the literal. The first cut of this
	// guard compared len(scanned) against len(table) — but len(table) is the
	// very thing a regression shrinks, so both sides moved together and the
	// realistic regression (rows deleted from the literal) was invisible: it
	// caught only the artificial shape of substituting nil at the call while
	// leaving table intact. That is the same "if BOTH shrink together the test
	// still passes while grading less" hole this branch had already closed in
	// the sibling table — pinned in one file and left open in its neighbour.
	const wantCandidates = 2
	table := []process.Info{
		{PID: 35001, PPID: 1, Name: "helper",
			Exe: testsupport.AbsFixture("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")},
		{PID: 35002, PPID: 400, Name: "esbuild",
			Exe: testsupport.AbsFixture("/Users/jane/src/grafel/node_modules/.bin/esbuild")},
	}
	if len(table) != wantCandidates {
		t.Fatalf("the fixture table has %d rows, want %d — a row was added or dropped, which "+
			"changes what this test feeds the scanner", len(table), wantCandidates)
	}
	withProcs7268(t, table)
	killed := withNoKills(t)

	// THE SENTENCE ABOVE IS NOW OBSERVED. It was not: with an empty input,
	// "none found" is equally the output of a scan that had nothing to reject.
	// Asserting what the scanner actually RECEIVED separates "rejected two
	// candidates" from "was handed none", and it runs through production's
	// scanGrafelProcs rather than counting the literal above.
	scanned, err := scanGrafelProcs(-1)
	if err != nil {
		t.Fatalf("scanGrafelProcs: %v", err)
	}
	if len(scanned) != wantCandidates {
		t.Fatalf("the scan saw %d candidates, want %d — with an empty input this test's "+
			"'none found' proves nothing about the selection", len(scanned), wantCandidates)
	}

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
