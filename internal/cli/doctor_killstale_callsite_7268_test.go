package cli

// doctor_killstale_callsite_7268_test.go — grades the CALL SITE of isStaleProc,
// not the predicate (#7268).
//
// WHY THIS FILE EXISTS. doctor_stale_identity_7268_test.go enumerates
// isStaleProc thoroughly, but the predicate is not what sends SIGTERM:
// runDoctorStaleDaemons is. With the predicate table as the only coverage,
// changing the call site to
//
//	if isStaleProc(p, selfExe) || p.PID != 0 {
//
// — which selects EVERY process process.FindByName("grafel") returns, i.e.
// exactly the population #7268 exists to protect — left `go test ./internal/cli/`
// at EXIT=0 with zero failures. A predicate that is graded and a consumer that
// is not buys nothing on a kill path.
//
// WHAT IS ASSERTED. The OUTPUT, not an internal counter: the `pid=` lines
// runDoctorStaleDaemons prints. Those lines are the thing that also drives
// process.Kill in the kill=true branch, one loop iteration apart, so an
// unexpected line here is an unexpected SIGTERM there.
//
// WHY kill=false. The kill=true branch really does SIGTERM whatever it
// selected; a synthetic table with invented PIDs must never reach it. Dry-run
// shares the selection loop verbatim, which is the half under test.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// absFixture7268 makes a unix-style absolute test path absolute for the
// RUNNING platform, so a fixture is classified by the identity gate under test
// rather than rejected wholesale by filepath.IsAbs on windows. filepath.Join
// does NOT work for this: it yields `\usr\…`, which has no volume and is still
// not absolute on windows, so every row would pass there for a reason
// unrelated to the guard it names.
func absFixture7268(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	vol := filepath.VolumeName(os.TempDir())
	if vol == "" {
		vol = "C:"
	}
	return vol + path
}

// withProcs7268 installs a synthetic process table for one test.
func withProcs7268(t *testing.T, procs []process.Info) {
	t.Helper()
	prev := findProcs
	findProcs = func(string) ([]process.Info, error) { return procs, nil }
	t.Cleanup(func() { findProcs = prev })
}

func TestRunDoctorStaleDaemons_SelectsOnlyOurBinary_7268(t *testing.T) {
	const (
		strangerPID    = 31001 // the #7268 headline row — must NOT be listed
		esbuildPID     = 31002 // #1719's false positive — must NOT be listed
		bystanderPID   = 31003 // a real grafel CLI, not stale — must NOT be listed
		staleTmpPID    = 31004 // genuine orphaned /tmp daemon — MUST be listed
		staleDirPID    = 31005 // genuine daemon under a "daemon" dir — MUST be listed
		staleOrphanPID = 31006 // ditto but PPID=1 — MUST be listed, WITH the orphan note

		// The /tmp* boundary family. Canonical basename, PPID=1, so criterion 1
		// fires for every one of them the moment scanGrafelProcs' prefix test is
		// widened from "/tmp/" to "/tmp" — precisely the widening that survived
		// the first round, because the predicate test derived IsTmp with its own
		// hand-copy of the rule and this call-site test had no boundary row.
		tmpfooPID   = 31007
		tmpdirPID   = 31008
		tmpAgentPID = 31009
		tmpExactPID = 31010
	)

	stranger := absFixture7268("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")
	esbuild := absFixture7268("/Users/jane/src/grafel/node_modules/@esbuild/darwin-arm64/bin/esbuild")
	bystander := absFixture7268("/usr/local/bin/grafel")
	staleTmp := "/tmp/agent-worktree-1/grafel" // criterion 1 is a literal /tmp prefix test
	staleDir := absFixture7268("/opt/grafel/daemon/bin/grafel")
	staleOrphan := absFixture7268("/opt/grafel/daemon/bin.old/grafel")

	table := []process.Info{
		{PID: strangerPID, PPID: 1, Name: "helper", Exe: stranger},
		{PID: esbuildPID, PPID: 1, Name: "esbuild", Exe: esbuild},
		{PID: bystanderPID, PPID: 400, Name: "grafel", Exe: bystander},
		{PID: staleDirPID, PPID: 400, Name: "grafel", Exe: staleDir},
		{PID: staleOrphanPID, PPID: 1, Name: "grafel", Exe: staleOrphan},

		// /tmp* boundary rows, carrying literal "/tmp"-prefixed paths rather
		// than absFixture7268 ones: the derivation is a byte comparison against
		// "/tmp/", so prefixing a volume would move the row off the boundary
		// entirely. The consequence is that on windows these are rejected by
		// the identity gate's absoluteness half and grade nothing — the same
		// limitation the criterion-1 positive row has, and the reason the /tmp
		// arm is ungradable on that platform.
		{PID: tmpfooPID, PPID: 1, Name: "grafel", Exe: "/tmpfoo/grafel"},
		{PID: tmpdirPID, PPID: 1, Name: "grafel", Exe: "/tmpdir/grafel"},
		{PID: tmpAgentPID, PPID: 1, Name: "grafel", Exe: "/tmp-agent/grafel"},
		// Exactly "/tmp". This row does NOT grade the `exe == "/tmp"` half of
		// the derivation, and is not claimed to: filepath.Base("/tmp") is
		// "tmp", not in canonicalBasenames, so the identity gate rejects it
		// before criterion 1 is consulted. Deleting that half of the OR leaves
		// this row green (measured, not assumed — see the report on #7268). It
		// is a forbidden row for the path shape, not coverage of the arm.
		{PID: tmpExactPID, PPID: 1, Name: "tmp", Exe: "/tmp"},
	}
	// Criterion 1 is unreachable on windows and cannot be made reachable by a
	// fixture: it is a literal "/tmp/" prefix test, and a path carrying that
	// prefix has no volume and so fails the identity gate's absoluteness half
	// there. Adding the row anyway would grade nothing on windows while
	// silently changing the expected count, so it is omitted rather than
	// faked. Criterion 2 (staleDir) grades the call site on every platform.
	wantListed := []int{staleDirPID, staleOrphanPID}
	if runtime.GOOS != "windows" {
		table = append(table, process.Info{PID: staleTmpPID, PPID: 1, Name: "grafel", Exe: staleTmp})
		wantListed = append(wantListed, staleTmpPID)
	}
	withProcs7268(t, table)

	var buf bytes.Buffer
	if err := runDoctorStaleDaemons(&buf, false); err != nil {
		t.Fatalf("runDoctorStaleDaemons: %v", err)
	}
	out := buf.String()

	// Positive control FIRST. Without it every "must not be listed" assertion
	// below is satisfied by a scan that listed nothing at all — a stubbed
	// predicate, an unreached seam, or an os.Executable() failure that returns
	// early with a warning would all read as a pass.
	for _, want := range wantListed {
		if !strings.Contains(out, fmt.Sprintf(" pid=%d", want)) {
			t.Fatalf("genuine stale daemon pid=%d is NOT listed; output:\n%s\n"+
				"the selection never ran, so the exclusions below grade nothing", want, out)
		}
	}

	for _, bad := range []struct {
		pid int
		exe string
		why string
	}{
		{strangerPID, stranger, "#7268 headline: a stranger's helper that merely LIVES under a grafel-named directory"},
		{esbuildPID, esbuild, "#1719: an esbuild binary inside a project named grafel"},
		{bystanderPID, bystander, "a healthy second grafel process — not stale by either criterion"},
		{tmpfooPID, "/tmpfoo/grafel", `/tmpfoo is a SIBLING of /tmp, not a path under it — the prefix test is "/tmp/"`},
		{tmpdirPID, "/tmpdir/grafel", "/tmpdir is a sibling of /tmp, not a path under it"},
		{tmpAgentPID, "/tmp-agent/grafel", "/tmp-agent is a sibling of /tmp, not a path under it"},
		{tmpExactPID, "/tmp", `exactly /tmp is a directory, and "tmp" is not a grafel basename`},
	} {
		if strings.Contains(out, fmt.Sprintf(" pid=%d", bad.pid)) {
			t.Errorf("pid=%d (%s) was listed for SIGTERM — %s\noutput:\n%s",
				bad.pid, bad.exe, bad.why, out)
		}
	}

	// staleProcess.IsOrphan's ONLY consumer is this note (doctor.go's kill-loop
	// print); isStaleProc reads p.PPID directly and never touches the field. So
	// the note is where scanGrafelProcs' `IsOrphan: p.PPID == 1` derivation is
	// gradable at all, and both directions are asserted: a PPID=1 row must
	// carry it (kills `IsOrphan: false`) and a PPID=400 row must not (kills
	// `IsOrphan: true`, and any widening such as `p.PPID <= 1`).
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(ln, fmt.Sprintf(" pid=%d", staleOrphanPID)):
			if !strings.Contains(ln, "[orphan: PPID=1]") {
				t.Errorf("pid=%d has PPID=1 but its line carries no orphan note: %q", staleOrphanPID, ln)
			}
		case strings.Contains(ln, fmt.Sprintf(" pid=%d", staleDirPID)):
			if strings.Contains(ln, "[orphan: PPID=1]") {
				t.Errorf("pid=%d has PPID=400 but its line is marked an orphan: %q", staleDirPID, ln)
			}
		}
	}

	// Anchored with a leading space on purpose: the printed line is
	// "  pid=%-6d ppid=%-6d …", so an unanchored "pid=" substring matches
	// inside "ppid=" too and counts every row twice.
	// Exact count, so a widening that also drags in some row this table did not
	// name fails here rather than hiding behind the three it did.
	if got := strings.Count(out, " pid="); got != len(wantListed) {
		t.Errorf("listed %d processes, want exactly %d (%v); output:\n%s",
			got, len(wantListed), wantListed, out)
	}
}

// TestRunDoctorStaleDaemons_ForeignOnlyTableListsNothing_7268 pins the other
// end: the "none found" path is reached through the same seam with a NON-empty
// table, so a call site that selects unconditionally cannot hide behind an
// empty process table.
func TestRunDoctorStaleDaemons_ForeignOnlyTableListsNothing_7268(t *testing.T) {
	withProcs7268(t, []process.Info{
		{PID: 32001, PPID: 1, Name: "helper", Exe: absFixture7268("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")},
		{PID: 32002, PPID: 1, Name: "fixture-server", Exe: "/tmp/grafel-fixtures/bin/fixture-server"},
	})

	var buf bytes.Buffer
	if err := runDoctorStaleDaemons(&buf, false); err != nil {
		t.Fatalf("runDoctorStaleDaemons: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, " pid=") {
		t.Errorf("a table of foreign processes only produced kill candidates:\n%s", out)
	}
	if !strings.Contains(out, "none found") {
		t.Errorf("expected the 'stale daemons: none found' line; got:\n%s", out)
	}
}
