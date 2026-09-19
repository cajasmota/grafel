package daemon

// selfdefense_canonical_test.go — classification tests for findCanonicalDaemon
// (#7211).
//
// These drive the REAL findCanonicalDaemon through the findProcs seam with a
// synthetic process table, rather than re-implementing its predicate in-test.
// That distinction is the point: the pre-existing #1719 test
// (TestFindCanonicalDaemon_EsbuildFalsePositive) asserts on its own copy of the
// basename rule and so cannot observe anything the shipped function does — it
// would pass unchanged with findCanonicalDaemon deleted. Every row below fails
// if the shipped classification changes.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// withProcs installs a synthetic process table for the duration of one test.
func withProcs(t *testing.T, procs []process.Info, err error) {
	t.Helper()
	prev := findProcs
	findProcs = func(string) ([]process.Info, error) { return procs, err }
	t.Cleanup(func() { findProcs = prev })
}

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
			// the truth is "unknown".
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
				{PID: otherPID, Name: "grafel", Exe: "/usr/local/bin/grafel"},
			},
			wantPID: otherPID,
			wantExe: "/usr/local/bin/grafel",
		},
		{
			name: "canonical daemon under a home go/bin is still matched",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "/home/user/go/bin/grafel"},
			},
			wantPID: otherPID,
			wantExe: "/home/user/go/bin/grafel",
		},
		{
			// The #857 exclusion must survive the fix.
			name: "tmp daemon is excluded",
			procs: []process.Info{
				{PID: otherPID, Name: "grafel", Exe: "/tmp/TestHarness_FixturesCorpus123/001/grafel"},
			},
			wantPID: 0,
			wantExe: "",
		},
		{
			name: "bare /tmp is excluded",
			procs: []process.Info{
				{PID: otherPID, Name: "tmp", Exe: "/tmp"},
			},
			wantPID: 0,
			wantExe: "",
		},
		{
			// The #1719 exclusion must survive the fix, now asserted through
			// the shipped function instead of a re-implementation.
			name: "esbuild under a directory named grafel is not canonical",
			procs: []process.Info{
				{PID: otherPID, Name: "esbuild", Exe: "/Users/user/Projects/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild"},
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
				{PID: otherPID + 1, Name: "grafel", Exe: "/usr/local/bin/grafel"},
			},
			wantPID: otherPID + 1,
			wantExe: "/usr/local/bin/grafel",
		},
		{
			// Ordering control in the other direction: a genuine daemon listed
			// first is returned even though an unknown-path entry follows.
			name: "genuine daemon first is returned",
			procs: []process.Info{
				{PID: otherPID + 1, Name: "grafel", Exe: "/usr/local/bin/grafel"},
				{PID: otherPID, Name: "grafel", Exe: ""},
			},
			wantPID: otherPID + 1,
			wantExe: "/usr/local/bin/grafel",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withProcs(t, tc.procs, nil)
			gotPID, gotExe := findCanonicalDaemon()
			if gotPID != tc.wantPID || gotExe != tc.wantExe {
				t.Errorf("findCanonicalDaemon() = (%d, %q), want (%d, %q)",
					gotPID, gotExe, tc.wantPID, tc.wantExe)
			}
		})
	}
}

// TestFindCanonicalDaemon_SkipsSelf pins that the scanning process never
// matches itself — without it, a canonical-path test binary would refuse its
// own startup.
func TestFindCanonicalDaemon_SkipsSelf(t *testing.T) {
	withProcs(t, []process.Info{
		{PID: os.Getpid(), Name: "grafel", Exe: "/usr/local/bin/grafel"},
	}, nil)
	if pid, exe := findCanonicalDaemon(); pid != 0 || exe != "" {
		t.Errorf("findCanonicalDaemon() matched self: (%d, %q)", pid, exe)
	}
}

// TestFindCanonicalDaemon_ScanError pins that an unreadable process table
// resolves to "no canonical daemon" rather than blocking startup.
func TestFindCanonicalDaemon_ScanError(t *testing.T) {
	withProcs(t, []process.Info{
		{PID: 62425, Name: "grafel", Exe: "/usr/local/bin/grafel"},
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

	// A genuine canonical daemon in the synthetic table.
	withProcs(t, []process.Info{
		{PID: 62425, Name: "grafel", Exe: "/usr/local/bin/grafel"},
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
	if !strings.Contains(gotCanonical.Error(), "62425") || !strings.Contains(gotCanonical.Error(), "/usr/local/bin/grafel") {
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
