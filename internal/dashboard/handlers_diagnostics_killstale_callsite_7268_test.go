package dashboard

// handlers_diagnostics_killstale_callsite_7268_test.go — grades the CALL SITE
// of isStaleDiagnosticsProc, not the predicate (#7268).
//
// WHY THIS FILE EXISTS. handlers_diagnostics_killstale_7268_test.go enumerates
// isStaleDiagnosticsProc thoroughly, but the predicate is not what sends
// SIGTERM: handleDiagnosticsKillStale is, and no test drove that handler at
// all. A call site that ignores the predicate — selecting every process
// process.FindByName("grafel") returns, i.e. exactly the population #7268
// exists to protect — left `go test ./internal/dashboard/` at EXIT=0.
//
// WHAT IS ASSERTED. The handler's OUTPUT: the KillStaleReply.Killed list on
// the wire. That list is built in the same loop iteration that calls
// process.Kill when dry_run is absent, so an unexpected entry here is an
// unexpected SIGTERM there.
//
// WHY dry_run=true. The live branch really does SIGTERM whatever it selected;
// a synthetic table with invented PIDs must never reach it. dry_run shares the
// selection loop and the reply construction verbatim — only the Kill call is
// skipped — which is the half under test.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// withProcsDash7268 installs a synthetic process table for one test.
//
// It ASSERTS THE NEEDLE, for the reason internal/cli's twin does: a stub that
// ignores its argument leaves the search term — which DEFINES the candidate
// population every guard downstream then filters — graded by nothing, and
// findProcs("") makes process.FindByName match every process on the host, the
// widest possible input to a SIGTERM path (#7268 round-4).
func withProcsDash7268(t *testing.T, procs []process.Info) {
	t.Helper()
	prev := findProcs
	findProcs = func(needle string) ([]process.Info, error) {
		if needle != "grafel" {
			t.Errorf("handleDiagnosticsKillStale searched for %q, want \"grafel\" — an empty "+
				"or wrong needle changes which processes are candidates for SIGTERM", needle)
		}
		return procs, nil
	}
	t.Cleanup(func() { findProcs = prev })
}

// withNoKills points the kill seam at a recorder and returns the recorded PIDs.
//
// Installed by EVERY test here, dry-run ones included. The tables invent PIDs
// (41001+) that on a real host belong to somebody else, and before this seam
// existed the only thing between them and SIGTERM was `dry_run=true` in the URL
// plus the handler's own ungraded one-line parse of it. A regression in that
// one line would have had the SUITE signal real processes.
func withNoKills(t *testing.T) *[]int {
	t.Helper()
	var killed []int
	prev := killProc
	killProc = func(pid int) error {
		killed = append(killed, pid)
		return nil
	}
	t.Cleanup(func() { killProc = prev })
	return &killed
}

func killStaleDryRun(t *testing.T) KillStaleReply {
	t.Helper()
	return callKillStale(t, "?dry_run=true")
}

func callKillStale(t *testing.T, query string) KillStaleReply {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics/kill-stale"+query, nil)
	(&Server{}).handleDiagnosticsKillStale(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var reply KillStaleReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode reply: %v; body: %s", err, rec.Body.String())
	}
	return reply
}

func TestHandleDiagnosticsKillStale_SelectsOnlyOurBinary_7268(t *testing.T) {
	const (
		strangerPID  = 41001 // the #7268 headline row — must NOT be selected
		esbuildPID   = 41002 // #1719's false positive — must NOT be selected
		bystanderPID = 41003 // a real grafel CLI, not stale — must NOT be selected
		staleTmpPID  = 41004 // genuine orphaned /tmp daemon — MUST be selected
		staleDirPID  = 41005 // genuine daemon under a "daemon" dir — MUST be selected
	)

	stranger := testsupport.AbsFixture("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")
	esbuild := testsupport.AbsFixture("/Users/jane/src/grafel/node_modules/@esbuild/darwin-arm64/bin/esbuild")
	bystander := testsupport.AbsFixture("/usr/local/bin/grafel")
	staleTmp := "/tmp/agent-worktree-1/grafel" // criterion 1 is a literal /tmp prefix test
	staleDir := testsupport.AbsFixture("/opt/grafel/daemon/bin/grafel")

	// B2: the self exclusion, graded by nothing before this row — every PID in
	// this table was synthetic and none could equal os.Getpid(), so
	// `if p.PID == myPID` → `&& false` passed the package. Criterion 1
	// (PPID==1 && isTmp) never consults selfExe, so a /tmp-installed daemon
	// with PPID==1 serving this endpoint would SIGTERM ITSELF if this
	// regressed. The row carries a canonical basename under a "daemon"
	// directory, so it WOULD be selected were the PID check not there.
	selfPID := os.Getpid()

	table := []process.Info{
		{PID: selfPID, PPID: 1, Name: "grafel", Exe: staleDir},
		{PID: strangerPID, PPID: 1, Name: "helper", Exe: stranger},
		{PID: esbuildPID, PPID: 1, Name: "esbuild", Exe: esbuild},
		{PID: bystanderPID, PPID: 400, Name: "grafel", Exe: bystander},
		{PID: staleDirPID, PPID: 400, Name: "grafel", Exe: staleDir},
	}
	// Criterion 1 is unreachable on windows and cannot be made reachable by a
	// fixture: it is a literal "/tmp/" prefix test, and a path carrying that
	// prefix has no volume and so fails the identity gate's absoluteness half
	// there. Omitted rather than faked; criterion 2 (staleDir) grades the call
	// site on every platform.
	want := map[int]bool{staleDirPID: true}
	if runtime.GOOS != "windows" {
		table = append(table, process.Info{PID: staleTmpPID, PPID: 1, Name: "grafel", Exe: staleTmp})
		want[staleTmpPID] = true
	}
	withProcsDash7268(t, table)
	killed := withNoKills(t)

	reply := killStaleDryRun(t)
	if !reply.DryRun {
		t.Fatalf("reply.DryRun = false for ?dry_run=true")
	}

	got := map[int]string{}
	for _, kp := range reply.Killed {
		got[kp.PID] = kp.Exe
	}

	// Positive control FIRST: without it, "the stranger was not selected" is
	// satisfied by a handler that selected nothing at all.
	for pid := range want {
		if _, ok := got[pid]; !ok {
			t.Fatalf("genuine stale daemon pid=%d is NOT in reply.Killed (%v) — the "+
				"selection never ran, so the exclusions below grade nothing", pid, reply.Killed)
		}
	}

	for _, bad := range []struct {
		pid int
		why string
	}{
		{strangerPID, "#7268 headline: a stranger's helper that merely LIVES under a grafel-named directory"},
		{esbuildPID, "#1719: an esbuild binary inside a project named grafel"},
		{bystanderPID, "a healthy second grafel process — not stale by either criterion"},
		{selfPID, "the running process is excluded by PID — selecting it means the endpoint SIGTERMs its own daemon"},
	} {
		if exe, ok := got[bad.pid]; ok {
			t.Errorf("pid=%d (%s) was selected for SIGTERM — %s", bad.pid, exe, bad.why)
		}
	}

	if len(reply.Killed) != len(want) {
		t.Errorf("reply.Killed has %d entries, want exactly %d; got %v",
			len(reply.Killed), len(want), reply.Killed)
	}
	// dry_run must not claim a kill happened — and, since the killProc seam
	// exists, must not have ATTEMPTED one either. The second assertion is the
	// one that makes dry-run a checked property rather than an assumption.
	for _, kp := range reply.Killed {
		if kp.Killed {
			t.Errorf("pid=%d reported Killed=true under dry_run=true", kp.PID)
		}
	}
	if len(*killed) != 0 {
		t.Errorf("dry_run=true signalled PIDs %v", *killed)
	}
}

// TestHandleDiagnosticsKillStale_ForeignOnlyTableSelectsNothing_7268 pins the
// other end with a NON-empty table, so a call site that selects
// unconditionally cannot hide behind an empty process table.
func TestHandleDiagnosticsKillStale_ForeignOnlyTableSelectsNothing_7268(t *testing.T) {
	withProcsDash7268(t, []process.Info{
		{PID: 42001, PPID: 1, Name: "helper", Exe: testsupport.AbsFixture("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")},
		{PID: 42002, PPID: 1, Name: "fixture-server", Exe: "/tmp/grafel-fixtures/bin/fixture-server"},
	})

	killed := withNoKills(t)
	reply := killStaleDryRun(t)
	if len(reply.Killed) != 0 {
		t.Errorf("a table of foreign processes only produced kill candidates: %v", reply.Killed)
	}
	if len(*killed) != 0 {
		t.Errorf("nothing was selected yet PIDs %v were signalled", *killed)
	}
}

// TestHandleDiagnosticsKillStale_LiveBranchAndDryRunParse_7268 grades the
// SIGTERM branch and the dry_run parse that gates it (#7268 round-4, F5).
//
// Both were unreachable before the killProc seam: the only safe way to drive
// the handler was dry_run=true, which skips the branch, and
// `dryRun := r.URL.Query().Get("dry_run") == "true"` — the single line deciding
// whether a real SIGTERM goes out — was asserted by nothing. Nothing is
// signalled here; killProc is a recorder.
func TestHandleDiagnosticsKillStale_LiveBranchAndDryRunParse_7268(t *testing.T) {
	const staleDirPID = 43001
	stale := testsupport.AbsFixture("/opt/grafel/daemon/bin/grafel")
	install := func(t *testing.T) *[]int {
		withProcsDash7268(t, []process.Info{
			{PID: staleDirPID, PPID: 400, Name: "grafel", Exe: stale},
			{PID: os.Getpid(), PPID: 1, Name: "grafel", Exe: stale},
		})
		return withNoKills(t)
	}

	// Every spelling that is NOT exactly "true" means a LIVE run. These pin the
	// parse in the dangerous direction: if any of them were read as a dry run
	// the endpoint would silently stop killing, and if the comparison were
	// dropped entirely every dry-run caller would start killing.
	for _, q := range []string{"", "?dry_run=false", "?dry_run=TRUE", "?dry_run=1", "?other=true"} {
		t.Run("live"+q, func(t *testing.T) {
			killed := install(t)
			reply := callKillStale(t, q)
			if reply.DryRun {
				t.Fatalf("query %q reported DryRun=true; only the exact string \"true\" is a dry run", q)
			}
			if len(*killed) != 1 || (*killed)[0] != staleDirPID {
				t.Fatalf("query %q signalled %v, want exactly [%d] — self must never be signalled",
					q, *killed, staleDirPID)
			}
			if len(reply.Killed) != 1 || !reply.Killed[0].Killed {
				t.Errorf("query %q: reply does not report the kill: %+v", q, reply.Killed)
			}
		})
	}

	t.Run("dry_run=true kills nothing", func(t *testing.T) {
		killed := install(t)
		reply := callKillStale(t, "?dry_run=true")
		if !reply.DryRun {
			t.Fatalf("?dry_run=true reported DryRun=false")
		}
		if len(*killed) != 0 {
			t.Fatalf("?dry_run=true signalled %v", *killed)
		}
		if len(reply.Killed) != 1 || reply.Killed[0].Killed {
			t.Errorf("dry run must still LIST the candidate, unkilled: %+v", reply.Killed)
		}
	})
}

// TestHandleDiagnosticsKillStale_ReportsKillFailure_7268 pins the error arm: a
// failing kill is surfaced in KillErr and never reported as Killed.
func TestHandleDiagnosticsKillStale_ReportsKillFailure_7268(t *testing.T) {
	const staleDirPID = 44001
	withProcsDash7268(t, []process.Info{
		{PID: staleDirPID, PPID: 400, Name: "grafel",
			Exe: testsupport.AbsFixture("/opt/grafel/daemon/bin/grafel")},
	})
	prev := killProc
	killProc = func(int) error { return errors.New("operation not permitted") }
	t.Cleanup(func() { killProc = prev })

	reply := callKillStale(t, "")
	if len(reply.Killed) != 1 {
		t.Fatalf("want one candidate, got %+v", reply.Killed)
	}
	if reply.Killed[0].Killed {
		t.Errorf("a failing kill reported Killed=true: %+v", reply.Killed[0])
	}
	if reply.Killed[0].KillErr != "operation not permitted" {
		t.Errorf("KillErr = %q, want the kill error surfaced", reply.Killed[0].KillErr)
	}
}

// TestKillProcDefaultIsGuarded_7268 grades the WIRING — see the identical test
// in internal/cli for why the function being graded in internal/process is not
// enough, and why this asserts function IDENTITY rather than the panic (the
// behavioural form sends the SIGTERM in exactly the failure mode it detects).
//
// ONE DASHBOARD-SPECIFIC CAVEAT, worth knowing before debugging a future
// mystery: the guard fires via panic, and net/http's conn.serve RECOVERS panics
// from handlers. Every test here drives the handler directly through
// httptest.NewRecorder, so the panic propagates to the test and this assertion
// works. A future test that stood the mux up with httptest.NewServer would
// instead see the guard's message swallowed into a connection error. No signal
// is sent either way — the guard still refuses before reaching process.Kill —
// but the diagnosis degrades from a named pid to "EOF".
//
// IT REJECTS EVEN A SEMANTICALLY EQUIVALENT WRAPPER, deliberately. A future
// refactor writing `var killProc = func(pid int) error { log(pid); return
// process.KillGuarded(pid) }` behaves identically and still fails here, because
// the assertion is function IDENTITY and a closure has its own code pointer.
// That strictness is the point — it is what makes this the sole grader of the
// wiring line — but it will look like a spurious failure to whoever writes that
// refactor, so: the fix is to keep the seam's DEFAULT as a bare reference to
// process.KillGuarded and put the wrapper somewhere else, or to change this
// test deliberately having re-established how the guard still applies.
//
// The control below establishes that the comparison DISCRIMINATES, not that
// `want` is the right anchor: rewriting want to reflect.ValueOf(killProc) is
// ALIVE and no assertion inside a test whose body IS the comparison can catch
// that. Worth knowing so the control is not read as stronger than it is.
func TestKillProcDefaultIsGuarded_7268(t *testing.T) {
	got := reflect.ValueOf(killProc).Pointer()
	want := reflect.ValueOf(process.KillGuarded).Pointer()
	if got != want {
		t.Fatal("killProc's default is not process.KillGuarded — any test that drives the " +
			"live branch without installing withNoKills now sends a REAL SIGTERM to whatever " +
			"holds the pid its synthetic process table invented")
	}
	if reflect.ValueOf(process.Kill).Pointer() == want {
		t.Fatal("process.Kill and process.KillGuarded compare equal — the identity check is vacuous")
	}
}
