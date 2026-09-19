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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

// absFixtureDash7268 makes a unix-style absolute test path absolute for the
// RUNNING platform. Without it every fixture is rejected on windows by the
// identity gate's absoluteness half, and each row would pass there for a
// reason unrelated to the guard it names.
func absFixtureDash7268(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	vol := filepath.VolumeName(os.TempDir())
	if vol == "" {
		vol = "C:"
	}
	return vol + path
}

// withProcsDash7268 installs a synthetic process table for one test.
func withProcsDash7268(t *testing.T, procs []process.Info) {
	t.Helper()
	prev := findProcs
	findProcs = func(string) ([]process.Info, error) { return procs, nil }
	t.Cleanup(func() { findProcs = prev })
}

func killStaleDryRun(t *testing.T) KillStaleReply {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics/kill-stale?dry_run=true", nil)
	(&Server{}).handleDiagnosticsKillStale(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var reply KillStaleReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode reply: %v; body: %s", err, rec.Body.String())
	}
	if !reply.DryRun {
		t.Fatalf("reply.DryRun = false — the handler would have SIGTERMed the synthetic PIDs")
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

	stranger := absFixtureDash7268("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")
	esbuild := absFixtureDash7268("/Users/jane/src/grafel/node_modules/@esbuild/darwin-arm64/bin/esbuild")
	bystander := absFixtureDash7268("/usr/local/bin/grafel")
	staleTmp := "/tmp/agent-worktree-1/grafel" // criterion 1 is a literal /tmp prefix test
	staleDir := absFixtureDash7268("/opt/grafel/daemon/bin/grafel")

	table := []process.Info{
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

	reply := killStaleDryRun(t)

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
	} {
		if exe, ok := got[bad.pid]; ok {
			t.Errorf("pid=%d (%s) was selected for SIGTERM — %s", bad.pid, exe, bad.why)
		}
	}

	if len(reply.Killed) != len(want) {
		t.Errorf("reply.Killed has %d entries, want exactly %d; got %v",
			len(reply.Killed), len(want), reply.Killed)
	}
	// dry_run must not claim a kill happened.
	for _, kp := range reply.Killed {
		if kp.Killed {
			t.Errorf("pid=%d reported Killed=true under dry_run=true", kp.PID)
		}
	}
}

// TestHandleDiagnosticsKillStale_ForeignOnlyTableSelectsNothing_7268 pins the
// other end with a NON-empty table, so a call site that selects
// unconditionally cannot hide behind an empty process table.
func TestHandleDiagnosticsKillStale_ForeignOnlyTableSelectsNothing_7268(t *testing.T) {
	withProcsDash7268(t, []process.Info{
		{PID: 42001, PPID: 1, Name: "helper", Exe: absFixtureDash7268("/Users/jane smith/Library/grafel-daemon-helper/bin/helper")},
		{PID: 42002, PPID: 1, Name: "fixture-server", Exe: "/tmp/grafel-fixtures/bin/fixture-server"},
	})

	reply := killStaleDryRun(t)
	if len(reply.Killed) != 0 {
		t.Errorf("a table of foreign processes only produced kill candidates: %v", reply.Killed)
	}
}
