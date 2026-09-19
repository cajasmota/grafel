package dashboard

// handlers_diagnostics_killstale_7268_test.go — identity gate on the
// POST /api/diagnostics/kill-stale selection predicate (issue #7268).
//
// THIS IS A KILL PATH. isStaleDiagnosticsProc's output is fed straight to
// process.Kill (SIGTERM), so the permissive direction is the dangerous one and
// is scored first here: a selected row that is not our binary signals a
// stranger's process. The conservative direction is pinned too — a fix that
// selects nothing silently disables the endpoint.
//
// The dashboard is a SEPARATE implementation from internal/cli's isStaleProc
// (#7258), so a verdict on one says nothing about the other; these rows are
// asserted against the dashboard's own predicate, not inherited from the CLI
// test.

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestIsStaleDiagnosticsProc_IdentityGate_7268(t *testing.T) {
	// PLATFORM. daemon.IsCanonicalBinaryPath opens with filepath.IsAbs, whose
	// answer is GOOS-dependent: "/usr/local/bin/grafel" is absolute on unix and
	// NOT absolute on windows, which has no volume in it. Left bare, this table
	// grades nothing on windows — the forbidden rows pass because every path
	// looked relative, and the three `want: true` rows invert and go red. Rows
	// go through testsupport.AbsFixture unless asWritten says otherwise.
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")

	cases := []struct {
		name string
		exe  string
		ppid int
		want bool
		// Two kinds of row keep exe byte-for-byte; they are NOT the same and
		// round 4 wrongly conflated them under one flag.
		//
		// tmpPrefix — the production test is strings.HasPrefix(exe, "/tmp/"), a
		//   byte comparison, so a volume prefix moves the fixture off the
		//   boundary. Such a path is not absolute on windows, so the identity
		//   gate decides the row instead of the criterion under test. UNIX ONLY,
		//   and skipped there rather than asserted.
		// relative — being relative IS the property under test, and it holds on
		//   every platform, so these are not skipped.
		tmpPrefix bool
		relative  bool
	}{
		// permissive direction — planted violations, must NOT be selected
		{"HEADLINE foreign helper under a grafel-daemon dir",
			"/Users/jane smith/Library/grafel-daemon-helper/bin/helper", 501, false, false, false},
		{"foreign binary named daemon-helper", "/Users/jane/grafel/bin/daemon-helper", 501, false, false, false},
		{"foreign orphan under /tmp (criterion 1)", "/tmp/grafel-fixtures/bin/fixture-server", 1, false, true, false},
		{"foreign esbuild under a grafel project", "/Users/jane/grafel/node_modules/.bin/esbuild", 1, false, false, false},
		{"relative path", "grafel-daemon/grafel", 1, false, false, true},
		{"bare command name", "grafel", 1, false, false, true},

		// conservative direction — genuine stale daemons must STILL be selected
		{"genuine orphaned /tmp daemon", "/tmp/agent-worktree-1/grafel", 1, true, true, false},
		{"genuine daemon under a directory named daemon", "/opt/grafel/daemon/bin/grafel", 100, true, false, false},
		{"case variations", "/opt/Grafel/Daemon/bin/GRAFEL", 100, true, false, false},

		// boundaries
		{"plain second grafel process", "/usr/local/bin/grafel", 100, false, false, false},
		{"canonical under /tmp with a live parent", "/tmp/agent-worktree-1/grafel", 4242, false, true, false},
		{"self", selfExe, 100, false, false, false}, // selfExe is already absolutised
		// The CLI twin rejects /tmpfoo; this predicate used to accept it via
		// exe[:4] == "/tmp". Held here so the two cannot drift apart again.
		// Unlike the CLI, isStaleDiagnosticsProc derives isTmp INSIDE the
		// function under test, so this row grades the production prefix test
		// directly — widening it to strings.HasPrefix(exe, "/tmp") fails here
		// (measured). The CLI twin needed a call-site row for the same
		// coverage because its derivation lives in scanGrafelProcs.
		{"/tmpfoo orphan is not a /tmp orphan", "/tmpfoo/grafel", 1, false, true, false},
		{"/tmpdir orphan is not a /tmp orphan", "/tmpdir/grafel", 1, false, true, false},
		{"/tmp-agent orphan is not a /tmp orphan", "/tmp-agent/grafel", 1, false, true, false},
		// NOT COVERAGE OF THE `exe == "/tmp"` ARM, and not claimed to be:
		// filepath.Base("/tmp") is "tmp", which is not in canonicalBasenames,
		// so the identity gate rejects this before isTmp is consulted.
		// Deleting that half of the OR leaves the package green (measured).
		// The arm is unreachable from this predicate, exactly as isTmpPath's
		// second arm is unreachable from findCanonicalDaemon (#7211).
		{"exactly /tmp is a directory, not our binary", "/tmp", 1, false, true, false},
	}

	// COUNT FLOORS — a skipped subtest reports SUCCESS, and so does a row that
	// quietly stopped being routed through AbsFixture. Declared so that adding
	// a flag to a row fails loudly rather than retiring its coverage.
	const (
		tmpPrefixRows = 7
		relativeRows  = 2
	)
	gotTmp, gotRel := 0, 0
	for _, tc := range cases {
		if tc.tmpPrefix {
			gotTmp++
		}
		if tc.relative {
			gotRel++
		}
		if tc.tmpPrefix && tc.relative {
			t.Fatalf("row %q is both tmpPrefix and relative; they mean different things", tc.name)
		}
	}
	if gotTmp != tmpPrefixRows || gotRel != relativeRows {
		t.Fatalf("%d tmpPrefix / %d relative rows, want %d / %d — a flag was added or dropped, "+
			"which silently changes what this table grades", gotTmp, gotRel, tmpPrefixRows, relativeRows)
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
			exe := tc.exe
			if !tc.tmpPrefix && !tc.relative {
				exe = testsupport.AbsFixture(exe)
				// Diagnostic for the windows leg; see the CLI twin.
				if !filepath.IsAbs(exe) {
					t.Fatalf("fixture %q is not absolute on %s — the row is not graded here; "+
						"either route it through testsupport.AbsFixture or mark it asWritten",
						exe, runtime.GOOS)
				}
			}
			if got := isStaleDiagnosticsProc(exe, tc.ppid, selfExe); got != tc.want {
				t.Errorf("isStaleDiagnosticsProc(%q, ppid=%d) = %v, want %v", exe, tc.ppid, got, tc.want)
			}
		})
	}

	if ran != wantRun {
		t.Errorf("%d of %d rows ran on %s, want %d — a widened skip grades less while still reporting ok",
			ran, len(cases), runtime.GOOS, wantRun)
	}
}

// TestIsStaleDiagnosticsProc_NeverSelectsForeignBinary_7268 enumerates the cross product of
// the directory shapes that satisfy process.FindByName("grafel") with
// non-grafel basenames, and requires none of them to be SIGTERM-eligible.
func TestIsStaleDiagnosticsProc_NeverSelectsForeignBinary_7268(t *testing.T) {
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")

	// Absolutised so the BASENAME half of the gate is what rejects each row on
	// every platform; left bare, a windows run rejects all of them at
	// filepath.IsAbs and the cross product grades nothing there.
	dirs := []string{
		testsupport.AbsFixture("/Users/jane smith/Library/grafel-daemon-helper/bin"),
		testsupport.AbsFixture("/opt/grafel/daemon"),
		testsupport.AbsFixture("/usr/local/grafel/DAEMON/bin"),
		testsupport.AbsFixture("/home/jane/grafel"),
		// /tmp-prefix dirs stay literal (a volume prefix moves them off the
		// /tmp boundary), so on windows these three are rejected at
		// filepath.IsAbs rather than at the basename.
		"/tmp/grafel-daemon/bin",
		"/tmp",
		"/tmpfoo/grafel",
	}
	bases := []string{"helper", "esbuild", "daemon", "daemonize", "grafel-daemon-old", "node", "grafeld"}

	for _, d := range dirs {
		for _, b := range bases {
			for _, ppid := range []int{1, 100, 4242} {
				exe := d + "/" + b
				if isStaleDiagnosticsProc(exe, ppid, selfExe) {
					t.Errorf("selected %q (ppid=%d) for SIGTERM — not a grafel binary", exe, ppid)
				}
			}
		}
	}
}
