package cli

// doctor_stale_identity_7268_test.go — identity gate on the `grafel doctor
// --kill-stale` selection predicate (issue #7268).
//
// THIS IS A KILL PATH. isStaleProc's output is fed straight to
// process.Kill (SIGTERM) by runDoctorStaleDaemons. The two failure
// directions are NOT symmetric and this file does not treat them as such:
//
//	permissive (dangerous) — a row selected that is not our binary sends
//	   SIGTERM to a stranger's process. Every such row below is a planted
//	   violation and is asserted NOT selected.
//	conservative (annoying) — a row not selected that is a genuine stale
//	   grafel daemon leaves an orphan behind. Pinned too, because a fix that
//	   selects nothing silently disables --kill-stale.
//
// The defect: the predicate decided "is this our binary" by testing whether
// the substring "daemon" appeared ANYWHERE in the exec path — a directory
// component counted. `/Users/jane smith/Library/grafel-daemon-helper/bin/helper`
// (measured on a real machine, see #7268) satisfies both the "grafel" needle
// that process.FindByName scans for and the "daemon" substring, and was
// therefore eligible for SIGTERM. daemon.IsCanonicalBinaryPath — the same
// gate findCanonicalDaemon has applied since #1719 — is now a precondition
// for every criterion.

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// staleFor builds the staleProcess a scan would produce for exe/ppid by running
// the REAL scanGrafelProcs over a one-row synthetic process table.
//
// IT MUST NOT DERIVE ANYTHING ITSELF. It used to, with a byte-identical
// hand-copy of scanGrafelProcs' own lines:
//
//	IsOrphan: ppid == 1,
//	IsTmp:    strings.HasPrefix(exe, "/tmp/") || exe == "/tmp",
//
// which made every /tmp-shaped row below grade THIS FILE'S copy of the rule
// instead of production's. The "/tmpfoo is not under /tmp" row and its
// `why: "prefix boundary: /tmp/ or exactly /tmp, not /tmp*"` therefore asserted
// a property nothing observed: widening production to
// strings.HasPrefix(exe, "/tmp") — which puts /tmpfoo/grafel, /tmpdir/grafel
// and /tmp-agent/grafel back on the SIGTERM list — left the whole package
// green. Routing through scanGrafelProcs is what makes the boundary row bite.
//
// scanGrafelProcs is handed myPID = -1 so no row is ever skipped as self; the
// seam is restored immediately rather than via t.Cleanup because the
// cross-product test calls this helper hundreds of times in one test.
func staleFor(t *testing.T, pid, ppid int, exe string) staleProcess {
	t.Helper()
	prev := findProcs
	findProcs = func(needle string) ([]process.Info, error) {
		// Assert the needle: a stub that ignores its argument leaves the search
		// term defining the candidate population graded by nothing, and
		// findProcs("") matches every process on the host (#7268 round-4).
		if needle != "grafel" {
			t.Errorf("scanGrafelProcs searched for %q, want \"grafel\"", needle)
		}
		return []process.Info{{PID: pid, PPID: ppid, Name: filepath.Base(exe), Exe: exe}}, nil
	}
	got, err := scanGrafelProcs(-1)
	findProcs = prev
	if err != nil {
		t.Fatalf("scanGrafelProcs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("scanGrafelProcs returned %d rows for one input, want 1", len(got))
	}
	return got[0]
}

func TestIsStaleProc_IdentityGate_7268(t *testing.T) {
	// PLATFORM. daemon.IsCanonicalBinaryPath opens with filepath.IsAbs, whose
	// answer is GOOS-dependent: "/usr/local/bin/grafel" is absolute on unix and
	// NOT absolute on windows, which has no volume in it. A table of bare unix
	// literals therefore grades nothing on windows — every forbidden row passes
	// for the unrelated reason that every path looked relative, and every
	// REQUIRED row inverts and goes red. Fixtures go through
	// testsupport.AbsFixture unless the row opts out below.
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")

	cases := []struct {
		name      string
		exe       string
		ppid      int
		wantStale bool
		why       string
		// Two kinds of row keep their exe byte-for-byte instead of routing it
		// through testsupport.AbsFixture. Round 4 conflated them under one
		// `asWritten` flag, which was wrong: they differ in whether the row
		// still grades anything on windows, and that is exactly what decides
		// whether it must be SKIPPED there.
		//
		// tmpPrefix — scanGrafelProcs asks strings.HasPrefix(exe, "/tmp/"), a
		//   byte comparison against a literal, so gluing a volume on moves the
		//   fixture off the boundary. Such a path is NOT absolute on windows,
		//   so the identity gate rejects it and the row's verdict is decided by
		//   the platform rather than by the criterion under test. UNIX ONLY.
		//
		//   CONSEQUENCE, WORTH STATING PLAINLY: every row that kills the
		//   `/tmp/` -> `/tmp` widening is a tmpPrefix row, so that mutant is
		//   DEAD on unix and would be ALIVE on windows. Criterion 1 is
		//   therefore not verified end-to-end on the windows leg, by
		//   construction rather than by oversight — the criterion is itself a
		//   hard-coded unix path and has no windows behaviour to grade.
		// relative — the row's whole point is that identity is not established
		//   for a relative path. Relative is relative on every platform, so
		//   these grade the same thing everywhere and are NOT skipped.
		tmpPrefix bool
		relative  bool
	}{
		// ── planted violations: NOT ours, must never be selected ──────────
		{
			name: "HEADLINE foreign helper under a grafel-daemon directory",
			// The exact path measured in #7268. Contains the FindByName needle
			// "grafel" and the substring "daemon", both in directory
			// components; the basename is "helper" — somebody else's binary.
			exe:       "/Users/jane smith/Library/grafel-daemon-helper/bin/helper",
			ppid:      501,
			wantStale: false,
			why:       "basename is not a grafel binary; selecting it SIGTERMs a stranger",
		},
		{
			name:      "foreign helper, daemon substring in the BASENAME",
			exe:       "/Users/jane/grafel/tools/bin/daemon-helper",
			ppid:      501,
			wantStale: false,
			why:       "boundary twin of the headline: daemon in the basename, still not our binary",
		},
		{
			name:      "foreign esbuild under a grafel project (the #1719 shape)",
			exe:       "/Users/jane/src/grafel/webui-v2/node_modules/@esbuild/darwin-arm64/bin/esbuild",
			ppid:      1,
			wantStale: false,
			why:       "PPID=1 alone must not select a foreign binary",
		},
		{
			name: "foreign orphan under /tmp — criterion 1 has the same gap",
			// #7268 names criterion 2 only. Criterion 1 (PPID=1 + /tmp) was
			// equally identity-free: this row is SIGTERM-eligible without the
			// gate even though "daemon" never appears in the path.
			exe:       "/tmp/grafel-fixtures/bin/fixture-server",
			tmpPrefix: true,
			ppid:      1,
			wantStale: false,
			why:       "criterion 1 planted violation",
		},
		{
			name:      "relative exec path is never selected",
			exe:       "grafel-daemon/grafel",
			relative:  true,
			ppid:      1,
			wantStale: false,
			why:       "a non-absolute path cannot be compared against /tmp or resolved to an identity",
		},
		{
			name:      "bare command name (Exe empty, Name fallback) is never selected",
			exe:       "grafel",
			relative:  true,
			ppid:      1,
			wantStale: false,
			why:       "#7211: a bare basename silently skips every path predicate",
		},

		// ── conservative direction: genuine stale daemons must STILL be selected
		{
			name:      "genuine orphaned daemon from a /tmp worktree",
			exe:       "/tmp/agent-worktree-1/grafel",
			tmpPrefix: true,
			ppid:      1,
			wantStale: true,
			why:       "criterion 1 positive control — the #857 population",
		},
		{
			name:      "genuine daemon installed under a directory named daemon",
			exe:       "/opt/grafel/daemon/bin/grafel",
			ppid:      100,
			wantStale: true,
			why:       "criterion 2 positive control — canonical basename, different binary than self",
		},
		{
			name:      "case variations on both the directory and the basename",
			exe:       "/opt/Grafel/Daemon/bin/GRAFEL",
			ppid:      100,
			wantStale: true,
			why:       "the shipped code lowercases; the basename gate must lowercase too",
		},

		// ── boundaries that must NOT be selected ──────────────────────────
		{
			name:      "canonical basename, no daemon component, not a /tmp orphan",
			exe:       "/usr/local/bin/grafel",
			ppid:      100,
			wantStale: false,
			why:       "a plain second grafel process (a concurrent CLI run) is not stale",
		},
		{
			name:      "canonical basename under /tmp but NOT an orphan",
			exe:       "/tmp/agent-worktree-1/grafel",
			tmpPrefix: true,
			ppid:      4242,
			wantStale: false,
			why:       "criterion 1 requires PPID=1; a live parent still owns it",
		},
		{
			name:      "self is never stale",
			exe:       selfExe,
			ppid:      100,
			wantStale: false,
			why:       "p.Exe == selfExe",
		},
		// The /tmp* family. These now run through scanGrafelProcs' real
		// derivation (see staleFor), so they grade the production prefix test
		// rather than a copy of it. Each is a canonical grafel basename with
		// PPID=1, so criterion 1 fires for all of them the moment the prefix
		// test is widened from "/tmp/" to "/tmp".
		{
			name:      "/tmpfoo is not under /tmp",
			exe:       "/tmpfoo/grafel",
			tmpPrefix: true,
			ppid:      1,
			wantStale: false,
			why:       "prefix boundary: /tmp/ or exactly /tmp, not /tmp*",
		},
		{
			name:      "/tmpdir is not under /tmp",
			exe:       "/tmpdir/grafel",
			tmpPrefix: true,
			ppid:      1,
			wantStale: false,
			why:       "prefix boundary — a sibling directory whose name merely starts with tmp",
		},
		{
			name:      "/tmp-agent is not under /tmp",
			exe:       "/tmp-agent/grafel",
			tmpPrefix: true,
			ppid:      1,
			wantStale: false,
			why:       "prefix boundary — a separator, not a hyphen, ends the /tmp component",
		},
	}

	// COUNT FLOORS. A skipped subtest reports SUCCESS, and so does a row that
	// silently stopped being routed through AbsFixture. Both flags are declared
	// here so that adding one to a row — M2 in the round-5 review set every row
	// to asWritten and the package stayed green — fails loudly instead of
	// retiring the row's coverage. Change these numbers deliberately.
	const (
		tmpPrefixRows = 6
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

	// On windows every tmpPrefix row is skipped, so the table must still run
	// the rest. Without this, widening the skip predicate would grade nothing
	// while the package stayed green.
	wantRun := len(cases)
	if runtime.GOOS == "windows" {
		wantRun -= tmpPrefixRows
	}
	ran := 0

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tmpPrefix && runtime.GOOS == "windows" {
				// NOT a diagnostic — a skip. Round 4 printed a message saying
				// the row grades nothing here and then called t.Errorf anyway,
				// which is how three required rows left the windows leg red.
				t.Skip("criterion 1 is a hard-coded unix /tmp prefix; see tmpPrefix")
			}
			ran++
			exe := tc.exe
			if !tc.tmpPrefix && !tc.relative {
				exe = testsupport.AbsFixture(exe)
				// Diagnostic for the windows leg: an unrouted or unroutable
				// fixture would otherwise fail as a confusing want/got
				// mismatch. filepath.IsAbs is the exact predicate the gate
				// applies first, so this names the cause.
				if !filepath.IsAbs(exe) {
					t.Fatalf("fixture %q is not absolute on %s — the row is not graded here; "+
						"either route it through testsupport.AbsFixture or mark it asWritten",
						exe, runtime.GOOS)
				}
			}
			p := staleFor(t, 7268, tc.ppid, exe)
			got := isStaleProc(p, selfExe)
			if got != tc.wantStale {
				verb := "SELECTED FOR SIGTERM"
				if tc.wantStale {
					verb = "NOT selected"
				}
				t.Errorf("isStaleProc(exe=%q, ppid=%d) = %v, want %v — %s was %s",
					exe, tc.ppid, got, tc.wantStale, exe, verb)
			}
		})
	}

	if ran != wantRun {
		t.Errorf("%d of %d rows ran on %s, want %d — a widened skip grades less while still reporting ok",
			ran, len(cases), runtime.GOOS, wantRun)
	}
}

// TestIsStaleProc_SelfIsNeverStale_7268 pins the p.Exe != selfExe half of
// criterion 2 in the only configuration where it is load-bearing: self's own
// path contains "daemon", so without the comparison the criterion would select
// the running binary. The main table cannot reach this — its selfExe has no
// "daemon" component, so criterion 2 is already false there for other reasons
// and the comparison is ungraded.
func TestIsStaleProc_SelfIsNeverStale_7268(t *testing.T) {
	selfExe := testsupport.AbsFixture("/opt/grafel/daemon/bin/grafel")

	if isStaleProc(staleFor(t, 1, 100, selfExe), selfExe) {
		t.Errorf("isStaleProc selected self (%q) for SIGTERM", selfExe)
	}
	// Control: the identical path at a different install root IS selected, so
	// the row above cannot pass merely because nothing matches this shape.
	other := testsupport.AbsFixture("/opt/grafel/daemon/bin.old/grafel")
	if !isStaleProc(staleFor(t, 2, 100, other), selfExe) {
		t.Errorf("isStaleProc did not select %q — criterion 2 is not firing at all, "+
			"so the self-exclusion above proves nothing", other)
	}
}

// TestIsStaleProc_SelectionImpliesCanonicalBasename_7268 states the invariant
// the table above samples: nothing whose basename is not a grafel binary may
// ever be selected, whatever combination of PPID, /tmp-ness and path
// substrings it carries. It enumerates the cross product rather than
// hand-picking rows, so a future criterion added to isStaleProc without the
// gate fails here even if nobody thinks to add a row above.
func TestIsStaleProc_SelectionImpliesCanonicalBasename_7268(t *testing.T) {
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel")

	// Absolutised so the BASENAME half of the gate is what rejects each row on
	// every platform. Left bare, a windows run rejects all of them at
	// filepath.IsAbs and the cross product grades nothing there.
	dirs := []string{
		testsupport.AbsFixture("/Users/jane smith/Library/grafel-daemon-helper/bin"),
		testsupport.AbsFixture("/opt/grafel/daemon"),
		testsupport.AbsFixture("/usr/local/grafel/DAEMON/bin"),
		testsupport.AbsFixture("/home/jane/grafel"),
		// /tmp-prefix dirs stay literal for the reason given at asWritten
		// above: a volume prefix would move them off the /tmp boundary. They
		// are consequently rejected at filepath.IsAbs on windows rather than
		// at the basename, which is a real limitation of these two rows and
		// not of the four above.
		"/tmp/grafel-daemon/bin",
		"/tmp",
	}
	bases := []string{"helper", "esbuild", "daemon", "daemonize", "grafel-daemon-old", "node", "grafeld"}
	ppids := []int{1, 100, 4242}

	for _, d := range dirs {
		for _, b := range bases {
			for _, ppid := range ppids {
				exe := d + "/" + b
				if daemon.IsCanonicalBinaryPath(exe) {
					t.Fatalf("fixture error: %q was meant to be a NON-grafel binary", exe)
				}
				if isStaleProc(staleFor(t, 1, ppid, exe), selfExe) {
					t.Errorf("isStaleProc selected %q (ppid=%d) for SIGTERM — basename %q is not a grafel binary",
						exe, ppid, filepath.Base(exe))
				}
			}
		}
	}
}
