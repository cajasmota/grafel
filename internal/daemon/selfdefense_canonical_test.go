package daemon

// selfdefense_canonical_test.go — classification tests for findCanonicalDaemon
// (#7211).
//
// These drive the REAL findCanonicalDaemon through the findProcs seam with a
// synthetic process table, rather than re-implementing its predicate in-test.
// That distinction is the point: the pre-existing #1719 test
// (TestFindCanonicalDaemon_EsbuildFalsePositive) asserts on its own copy of the
// basename rule and so cannot observe anything the shipped function does — it
// passes with findCanonicalDaemon stubbed to return (0, ""). Every row below
// fails if the shipped classification changes.
//
// PLATFORM NOTE. In production findCanonicalDaemon never reaches its loop on
// windows: process.FindByName returns ErrUnsupported there and the function
// returns (0, "") first. The findProcs seam deliberately bypasses that, which
// is what makes the classification gradable at all — and also what makes these
// tests platform-sensitive, because findCanonicalDaemon consults
// filepath.IsAbs, whose answer is GOOS-dependent. "/usr/local/bin/grafel" is
// absolute on unix and NOT absolute on windows (no volume). Fixtures therefore
// go through absFixture so the rows grade the same logic on every platform
// rather than passing on windows for the unrelated reason that every path
// looked relative. See TestFindCanonicalDaemon_AbsolutenessIsPlatformSpecific,
// which pins that distinction directly.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// absFixture makes a unix-style absolute test path absolute for the RUNNING
// platform, so a fixture is classified by the guard under test rather than
// rejected wholesale by filepath.IsAbs on windows.
//
// filepath.Join("/", "usr", …) does NOT work for this: it yields `\usr\…`,
// which still has no volume and is still not absolute on windows. A volume is
// required, so one is taken from os.TempDir() (absolute on every platform).
func absFixture(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	vol := filepath.VolumeName(os.TempDir())
	if vol == "" {
		vol = "C:"
	}
	return vol + path
}

// withProcs installs a synthetic process table for the duration of one test.
func withProcs(t *testing.T, procs []process.Info, err error) {
	t.Helper()
	prev := findProcs
	findProcs = func(string) ([]process.Info, error) { return procs, err }
	t.Cleanup(func() { findProcs = prev })
}

// A NOTE ON WHAT THIS TABLE CANNOT GRADE: isTmpPath's second arm
// (`path == "/tmp"`) is unreachable from findCanonicalDaemon, so no fixture
// here can kill a mutant that deletes it. filepath.Base("/tmp") is "tmp",
// which is not in canonicalBasenames, so a process whose Exe is exactly
// "/tmp" is rejected by the basename gate whether or not isTmpPath excluded
// it first. An earlier revision of this file had a "bare /tmp is excluded"
// row that looked like it covered the arm and did not — it passed for a
// reason unrelated to the guard it named, and deleting the arm left the full
// package green. That is a property of the production code, not a weakness in
// this table. See the PR discussion on #7211.
//
// TestFindCanonicalDaemon_Classification is the grading table for #7211.
//
// Both directions are scored deliberately. The restrictive rows pin the fix
// (an unknown executable path is not canonical); the permissive rows pin what
// the fix must NOT break — a genuine canonical daemon still has to be found,
// because failing to find one lets a /tmp daemon displace the user's real one,
// which is the harm the whole check exists to prevent.
func TestFindCanonicalDaemon_Classification(t *testing.T) {
	const otherPID = 62425 // not os.Getpid(); the self-skip is covered separately

	tests := []struct {
		name    string
		procs   []process.Info
		wantPID int
		wantExe string
		// unixOnly marks a row whose premise is isTmpPath's hard-coded "/tmp/"
		// exclusion zone. That predicate is Unix-only by construction, and the
		// row cannot be made portable: prepending a volume stops the path
		// being a /tmp path at all, while leaving it volume-less makes it
		// non-absolute on windows, so the row would pass there via the
		// absoluteness guard instead of the exclusion it names — vacuous in
		// exactly the way this file warns about above.
		unixOnly bool
	}{
		{
			// The #7211 defect itself: an exiting sibling whose
			// /proc/<pid>/exe is already gone while /proc/<pid>/comm still
			// reads "grafel". Before the fix, cmdBin fell back to the bare
			// name, isTmpPath("grafel") was false, and the basename match
			// returned it as the user's canonical daemon.
			name: "empty Exe with grafel comm is not canonical",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "", ExeErr: errors.New("readlink /proc/62425/exe: no such file or directory")},
			},
			wantPID: 0,
			wantExe: "",
		},
		{
			// Same shape, no error recorded (a platform that simply cannot
			// supply a path). Empty still means unknown, still not canonical.
			name: "empty Exe with no ExeErr is not canonical",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: ""},
			},
			wantPID: 0,
			wantExe: "",
		},
		{
			// A non-empty but relative path is equally unusable: isTmpPath is
			// a prefix test, so "bin/grafel" reads as "not under /tmp" when
			// the truth is "unknown". Relative on every platform.
			name: "relative Exe is not canonical",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "bin/grafel"},
			},
			wantPID: 0,
			wantExe: "",
		},
		{
			// PERMISSIVE DIRECTION. A real installed daemon must still be
			// found, or the /tmp anti-displacement protection is gone.
			name: "genuine canonical daemon is still matched",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: absFixture("/usr/local/bin/grafel")},
			},
			wantPID: otherPID,
			wantExe: absFixture("/usr/local/bin/grafel"),
		},
		{
			name: "canonical daemon under a home go/bin is still matched",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: absFixture("/home/user/go/bin/grafel")},
			},
			wantPID: otherPID,
			wantExe: absFixture("/home/user/go/bin/grafel"),
		},
		{
			// The #857 exclusion must survive the fix.
			name: "tmp daemon is excluded",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "/tmp/TestHarness_FixturesCorpus123/001/grafel"},
			},
			wantPID:  0,
			wantExe:  "",
			unixOnly: true,
		},
		{
			// Minimal form directly under /tmp — the boundary of the prefix
			// arm, where the excluded directory has no intervening component.
			name: "daemon directly under /tmp is excluded",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "/tmp/grafel"},
			},
			wantPID:  0,
			wantExe:  "",
			unixOnly: true,
		},
		{
			// PERMISSIVE DIRECTION, and the row that grades the trailing slash
			// in isTmpPath's "/tmp/" prefix. Without it the prefix would also
			// swallow sibling directories whose names merely START with "tmp",
			// excluding a genuine canonical daemon and silently disabling the
			// whole anti-displacement protection for anyone installed there.
			name: "a directory merely starting with tmp is not under /tmp",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "/tmpfoo/grafel"},
			},
			wantPID:  otherPID,
			wantExe:  "/tmpfoo/grafel",
			unixOnly: true,
		},
		{
			// The #1719 exclusion must survive the fix, now asserted through
			// the shipped function instead of a re-implementation.
			name: "esbuild under a directory named grafel is not canonical",
			procs: []process.Info{
				{PID: otherPID, Name: "esbuild", Exe: absFixture("/Users/user/Projects/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild")},
			},
			wantPID: 0,
			wantExe: "",
		},
		{
			// An unknown-path entry must not shadow a genuine canonical daemon
			// later in the table: the fix has to `continue`, not return early.
			name: "empty Exe entry does not mask a later genuine daemon",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: ""},
				{PID: otherPID + 1, Name: "grafel", Exe: absFixture("/usr/local/bin/grafel")},
			},
			wantPID: otherPID + 1,
			wantExe: absFixture("/usr/local/bin/grafel"),
		},
		{
			// Ordering control in the other direction: a genuine daemon listed
			// first is returned even though an unknown-path entry follows.
			name: "genuine daemon first is returned",
			procs: []process.Info{
				{PID: otherPID + 1, Name: "grafel", Exe: absFixture("/usr/local/bin/grafel")},
				{PID: otherPID, Name: "grafel", Exe: ""},
			},
			wantPID: otherPID + 1,
			wantExe: absFixture("/usr/local/bin/grafel"),
		},
	}

	// unixOnlyRows states, independently of the unixOnly flags themselves, how
	// many rows depend on isTmpPath's hard-coded unix exclusion zone. It is
	// deliberately a second, separate statement of the same fact: if a row
	// gains or loses the flag without this number moving, the cross-check
	// below fails instead of the table quietly grading less.
	const unixOnlyRows = 3

	flagged := 0
	for _, tc := range tests {
		if tc.unixOnly {
			flagged++
		}
	}
	if flagged != unixOnlyRows {
		t.Fatalf("%d rows are marked unixOnly, want %d — change unixOnlyRows deliberately, or drop the flag",
			flagged, unixOnlyRows)
	}

	wantRun := len(tests)
	if runtime.GOOS == "windows" {
		wantRun -= unixOnlyRows
	}

	ran := 0
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unixOnly && runtime.GOOS == "windows" {
				t.Skip("isTmpPath's exclusion zone is a hard-coded unix path; see unixOnly")
			}
			ran++
			withProcs(t, tc.procs, nil)
			gotPID, gotExe := findCanonicalDaemon()
			if gotPID != tc.wantPID || gotExe != tc.wantExe {
				t.Errorf("findCanonicalDaemon() = (%d, %q), want (%d, %q)",
					gotPID, gotExe, tc.wantPID, tc.wantExe)
			}
		})
	}

	// A skipped subtest reports SUCCESS. Without this guard the skip predicate
	// could widen — to `if tc.unixOnly`, or to every row — and the package
	// would stay green while grading nothing, which is the same hole as a
	// t.Skipf standing in for a t.Fatalf. Assert how many rows actually ran on
	// this platform, so skipping more than the unix/windows split justifies is
	// loud rather than invisible.
	if ran != wantRun {
		t.Errorf("%d of %d rows ran on %s, want %d — a widened skip grades less while still reporting ok",
			ran, len(tests), runtime.GOOS, wantRun)
	}
}

// TestFindCanonicalDaemon_AbsolutenessIsPlatformSpecific pins that the guard
// consults the RUNNING platform's notion of an absolute path, and exists so the
// next person to add a fixture cannot quietly hard-code a unix path again.
//
// The #7211 table originally did exactly that and went red on the windows leg:
// "/usr/local/bin/grafel" has no volume, so filepath.IsAbs reports false there
// and every "a genuine daemon is still matched" row was skipped by the
// absoluteness guard. This test asserts both halves of that difference rather
// than leaving it to be rediscovered by CI.
//
// It has a platform branch but deliberately NO skip, so it does not need the
// executed-row guard that TestFindCanonicalDaemon_Classification carries: both
// branches end in real assertions, and the two platforms expect opposite
// verdicts on the same fixture (windows: not canonical; unix: canonical). That
// makes an inverted or mis-targeted branch fail on whichever platform runs it
// instead of passing silently — measured, by inverting the condition and
// confirming the test goes red rather than green.
func TestFindCanonicalDaemon_AbsolutenessIsPlatformSpecific(t *testing.T) {
	const pid = 62425

	// Absolute on whatever platform is running: always canonical.
	platformAbs := absFixture("/usr/local/bin/grafel")
	withProcs(t, []process.Info{{PID: pid, Name: "grafel", Exe: platformAbs}}, nil)
	if gotPID, gotExe := findCanonicalDaemon(); gotPID != pid || gotExe != platformAbs {
		t.Errorf("platform-absolute %q: got (%d, %q), want (%d, %q)",
			platformAbs, gotPID, gotExe, pid, platformAbs)
	}

	// Absolute on unix only — no volume, so windows says it is relative.
	const unixOnlyAbs = "/usr/local/bin/grafel"
	withProcs(t, []process.Info{{PID: pid, Name: "grafel", Exe: unixOnlyAbs}}, nil)
	gotPID, gotExe := findCanonicalDaemon()
	if runtime.GOOS == "windows" {
		if gotPID != 0 || gotExe != "" {
			t.Errorf("windows: volume-less %q must not be canonical; got (%d, %q)",
				unixOnlyAbs, gotPID, gotExe)
		}
		// Sanity: the two fixtures really are different strings here, or the
		// assertion above is testing the same thing twice.
		if platformAbs == unixOnlyAbs {
			t.Errorf("absFixture did not add a volume on windows: %q", platformAbs)
		}
		return
	}
	if gotPID != pid || gotExe != unixOnlyAbs {
		t.Errorf("unix: %q must be canonical; got (%d, %q)", unixOnlyAbs, gotPID, gotExe)
	}
	if platformAbs != unixOnlyAbs {
		t.Errorf("absFixture must be a no-op off windows; got %q", platformAbs)
	}
}

// TestFindCanonicalDaemon_SkipsSelf pins that the scanning process never
// matches itself — without it, a canonical-path test binary would refuse its
// own startup.
func TestFindCanonicalDaemon_SkipsSelf(t *testing.T) {
	withProcs(t, []process.Info{
		{PID: os.Getpid(), Name: "grafel", Exe: absFixture("/usr/local/bin/grafel")},
	}, nil)
	if pid, exe := findCanonicalDaemon(); pid != 0 || exe != "" {
		t.Errorf("findCanonicalDaemon() matched self: (%d, %q)", pid, exe)
	}
}

// TestFindCanonicalDaemon_ScanError pins that an unreadable process table
// resolves to "no canonical daemon" rather than blocking startup.
func TestFindCanonicalDaemon_ScanError(t *testing.T) {
	withProcs(t, []process.Info{
		{PID: 62425, Name: "grafel", Exe: absFixture("/usr/local/bin/grafel")},
	}, errors.New("readdir /proc: permission denied"))
	if pid, exe := findCanonicalDaemon(); pid != 0 || exe != "" {
		t.Errorf("findCanonicalDaemon() = (%d, %q) despite a scan error, want (0, \"\")", pid, exe)
	}
}

// TestSelfDefenseCheck_ConsultsFindCanonicalDaemon is the control one level up
// required by the repo's own rule that a predicate test does not pin its call
// site. It proves SelfDefenseCheck's refusal is derived from the process scan
// — mutating the classification must change what SelfDefenseCheck does.
//
// It exercises the path only when the test binary genuinely lives under /tmp
// (os.Executable is not injectable here); elsewhere it asserts the complement,
// which is the whole of SelfDefenseCheck's contract for a canonical binary.
func TestSelfDefenseCheck_ConsultsFindCanonicalDaemon(t *testing.T) {
	t.Setenv(EnvDisableSelfDefense, "")

	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}

	canonExe := absFixture("/usr/local/bin/grafel")

	// A genuine canonical daemon in the synthetic table.
	withProcs(t, []process.Info{
		{PID: 62425, Name: "grafel", Exe: canonExe},
	}, nil)
	gotCanonical := SelfDefenseCheck(nil)

	if !isTmpPath(self) {
		// Canonical binary: no restriction regardless of what is running.
		if gotCanonical != nil {
			t.Fatalf("SelfDefenseCheck refused a non-/tmp binary %q: %v", self, gotCanonical)
		}
		return
	}

	// /tmp binary: the refusal must name the pid the scan produced.
	if gotCanonical == nil {
		t.Fatal("SelfDefenseCheck returned nil for a /tmp binary while a canonical daemon was in the process table")
	}
	if !strings.Contains(gotCanonical.Error(), "62425") || !strings.Contains(gotCanonical.Error(), canonExe) {
		t.Errorf("refusal does not report the scanned process: %v", gotCanonical)
	}

	// Now the #7211 shape: the only other grafel process has an unknown path.
	// SelfDefenseCheck must let this binary start.
	withProcs(t, []process.Info{
		{PID: 62425, Name: "grafel", Exe: "", ExeErr: errors.New("no such file or directory")},
	}, nil)
	if err := SelfDefenseCheck(nil); err != nil {
		t.Errorf("SelfDefenseCheck refused startup over an unknown-path process: %v", err)
	}
}
