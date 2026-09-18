//go:build darwin || linux

package daemon_test

import (
	"context"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// memLimitTotalForPlaneTests is a deterministic installation-wide budget
// pinned via GRAFEL_DAEMON_MEMLIMIT_MB so these tests do not depend on the
// host's RAM.
const memLimitTotalForPlaneTests = 10000

// pinProcessMemLimit saves + restores the process-global Go soft memory limit
// and RESETS it to the Go default for the duration of the test.
//
// The reset matters: sibling tests in this package start in-process daemons,
// which call applyMemoryLimit and leave the limit set. Without the reset,
// waitForRuntimeMemLimit would return that stale value immediately and the
// test would pass or fail on someone else's number — the whole suite would be
// a fixture that cannot exhibit what it claims to check.
func pinProcessMemLimit(t *testing.T) {
	t.Helper()
	prev := debug.SetMemoryLimit(math.MaxInt64)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
}

// waitForRuntimeMemLimit polls until the process-global soft limit moves off
// the Go default (math.MaxInt64), i.e. until the daemon under test has run
// applyMemoryLimit. Returns the limit in MB, or -1 on timeout.
func waitForRuntimeMemLimit(timeout time.Duration) int64 {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if v := debug.SetMemoryLimit(-1); v != math.MaxInt64 {
			return v / 1024 / 1024
		}
		time.Sleep(10 * time.Millisecond)
	}
	return -1
}

// TestRunServe_AppliesServeShareNotWholeBudget binds the fix to the path that
// actually runs (#6045). With split mode ON, RunServe's in-process serve plane
// must set GOMEMLIMIT to its SHARE of the installation budget — not the whole
// advertised figure, which is what made the real ceiling 2x what
// `grafel status` reported.
func TestRunServe_AppliesServeShareNotWholeBudget(t *testing.T) {
	pinProcessMemLimit(t)
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(daemon.EnvDisableSelfDefense, "1")
	t.Setenv("GRAFEL_SPLIT_MODE", "1")
	t.Setenv(daemon.EnvStatusHeartbeatSeconds, "1")
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("GRAFEL_DAEMON_MEMLIMIT_MB", strconv.Itoa(memLimitTotalForPlaneTests))

	// Substitute the in-binary engine helper for a real grafel binary.
	restore := daemon.SetEngineChildCommandForTest(func(selfExe, root string) *exec.Cmd {
		cmd := exec.Command(selfExe, "-test.run=TestEngineChildHelper", "-test.timeout=120s")
		cmd.Env = append(os.Environ(),
			"GRAFEL_ENGINE_CHILD_HELPER=1",
			daemon.EnvRoot+"="+root,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	})
	defer restore()

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.RunServe(ctx, daemon.ServeConfig{Config: daemon.Config{
			Layout: layout,
			Index: func(a proto.IndexArgs) (string, string, error) {
				return a.RepoPath + "/.grafel/graph.json", `{"ok":true}`, nil
			},
		}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Log("RunServe did not exit within 15s")
		}
	})

	gotMB := waitForRuntimeMemLimit(20 * time.Second)
	if gotMB < 0 {
		t.Fatal("serve never applied a Go soft memory limit")
	}
	wantServe, _ := daemon.SplitMemLimitMB(memLimitTotalForPlaneTests)
	if gotMB != wantServe {
		t.Errorf("serve plane applied %dMB, want its share %dMB of the %dMB installation budget",
			gotMB, wantServe, memLimitTotalForPlaneTests)
	}
	if gotMB == memLimitTotalForPlaneTests {
		t.Errorf("serve plane took the WHOLE %dMB budget — this is the #6045 per-process doubling",
			memLimitTotalForPlaneTests)
	}
}

// TestRunServe_MonolithGetsWholeBudget: with the GRAFEL_SPLIT_MODE=0 escape
// hatch there is exactly ONE process, so it must keep the whole installation
// budget. Halving it here would be a silent regression for every operator on
// the documented escape hatch.
func TestRunServe_MonolithGetsWholeBudget(t *testing.T) {
	pinProcessMemLimit(t)
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(daemon.EnvDisableSelfDefense, "1")
	t.Setenv("GRAFEL_SPLIT_MODE", "0")
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("GRAFEL_DAEMON_MEMLIMIT_MB", strconv.Itoa(memLimitTotalForPlaneTests))

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.RunServe(ctx, daemon.ServeConfig{Config: daemon.Config{
			Layout: layout,
			Index: func(a proto.IndexArgs) (string, string, error) {
				return a.RepoPath + "/.grafel/graph.json", `{"ok":true}`, nil
			},
		}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Log("RunServe (monolith) did not exit within 15s")
		}
	})

	gotMB := waitForRuntimeMemLimit(20 * time.Second)
	if gotMB < 0 {
		t.Fatal("monolith serve never applied a Go soft memory limit")
	}
	if gotMB != memLimitTotalForPlaneTests {
		t.Errorf("monolith applied %dMB, want the whole %dMB budget (one process, no split)",
			gotMB, memLimitTotalForPlaneTests)
	}
}

// syncBuf is a concurrency-safe io.Writer for capturing daemon log output.
type syncBuf struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// runServeCapturingLog starts RunServe with a captured logger and a NON-ZERO
// RSS admission-control budget, waits for the socket, and returns the log text.
func runServeCapturingLog(t *testing.T, splitMode string) string {
	t.Helper()
	root := shortTempRoot(t)
	t.Setenv(daemon.EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(daemon.EnvDisableSelfDefense, "1")
	t.Setenv("GRAFEL_SPLIT_MODE", splitMode)
	t.Setenv(daemon.EnvStatusHeartbeatSeconds, "1")

	if splitMode != "0" {
		restore := daemon.SetEngineChildCommandForTest(func(selfExe, root string) *exec.Cmd {
			cmd := exec.Command(selfExe, "-test.run=TestEngineChildHelper", "-test.timeout=120s")
			cmd.Env = append(os.Environ(),
				"GRAFEL_ENGINE_CHILD_HELPER=1",
				daemon.EnvRoot+"="+root,
			)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			return cmd
		})
		t.Cleanup(restore)
	}

	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if err := daemon.EnsureLayout(layout); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	sb := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.RunServe(ctx, daemon.ServeConfig{Config: daemon.Config{
			Layout: layout,
			Logger: slog.New(slog.NewTextHandler(sb, nil)),
			// A NON-ZERO budget is what makes this fixture able to exhibit the
			// thing it checks: with 0 the admission-control line is never
			// logged by any plane and the assertion would be vacuous.
			MaxRSSBudgetMB: 2048,
			// The scheduler (and therefore the admission-control line) only
			// comes up when SchedulerIndex is wired.
			SchedulerIndex: func(ctx context.Context, repo, ref string) error { return nil },
			Index: func(a proto.IndexArgs) (string, string, error) {
				return a.RepoPath + "/.grafel/graph.json", `{"ok":true}`, nil
			},
		}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Log("RunServe did not exit within 15s")
		}
	})
	// Socket-dialability is NOT the condition this test depends on (#7222).
	// run() binds the socket (server.go "startup: socket-listen done") BEFORE
	// it reaches the engine-plane block that logs the RSS marker, so a capture
	// taken at dial time can legitimately predate the marker — on a loaded
	// runner the capture wins the race and the positive control below fails
	// spuriously. waitDaemonReady stays only as an early, well-labelled
	// diagnostic for a daemon that never binds at all.
	waitDaemonReady(t, layout.SocketPath, 20*time.Second)

	// serveStartupComplete is logged by run() AFTER the
	// `if plane != planeServeOnly { startEnginePlane(...) }` block, in BOTH
	// modes, and startEnginePlane logs the RSS marker synchronously before it
	// returns. So this line is a happens-AFTER anchor for the marker:
	//   - monolith: marker missing once the anchor is present means the
	//     fixture genuinely cannot exhibit it (a real control failure), not
	//     that we looked too early;
	//   - split: the anchor proves serve ran PAST the point where the engine
	//     plane would have been started, so the forbidden assertion is
	//     judging a decision that has actually been made.
	logged, ok := waitForLogMarker(sb.String, serveStartupComplete, 60*time.Second)
	if !ok {
		t.Fatalf("serve (GRAFEL_SPLIT_MODE=%s) never logged %q within 60s: startup never reached the point past the engine-plane block, so neither half of this test can be judged\n%s",
			splitMode, serveStartupComplete, logged)
	}
	return logged
}

// serveStartupComplete is run()'s post-engine-plane startup line, as rendered
// by slog's TextHandler (server.go: logger.Info("ready", "socket", ..., "pid",
// ...)). It is the only message that RENDERS as `msg=ready`: server.go:504
// also logs "engine: ready", which TextHandler renders quoted
// (`msg="engine: ready"`) because of the space, so it cannot match. That
// distinction rests on slog's quoting, and this fixture additionally leaves
// the engine child's stdout/stderr unset so the child's log never reaches sb
// at all (unlike helperEngineCommand in supervise_test.go, which wires them).
// A future test that pipes the child's stderr into the same buffer would be
// depending on the quoting alone.
const serveStartupComplete = "msg=ready"

// waitForLogMarker polls get() until marker appears or timeout elapses. It
// returns the last snapshot it read and whether the marker was found — a
// timeout is reported as NOT found, never as success, so a caller cannot
// silently proceed on a log that never reached the state it needs.
func waitForLogMarker(get func() string, marker string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for {
		s := get()
		if strings.Contains(s, marker) {
			return s, true
		}
		if !time.Now().Before(deadline) {
			return s, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The three tests below grade waitForLogMarker itself. The e2e test above can
// only ever exercise its happy path (the anchor always arrives on an idle
// machine), so without these a permissive rewrite — "timed out, call it found"
// or "never poll, answer from the first read" — would be invisible: the e2e
// test would stay green while the sequencing guarantee it rests on was gone.

func TestWaitForLogMarker_TimeoutIsNotSuccess(t *testing.T) {
	const text = "startup: socket-listen done\n"
	start := time.Now()
	got, ok := waitForLogMarker(func() string { return text }, "msg=ready", 200*time.Millisecond)
	elapsed := time.Since(start)
	if ok {
		t.Errorf("reported the marker as found in a log that never contained it — a caller would proceed on a log that never reached the state it needs")
	}
	if got != text {
		t.Errorf("returned snapshot %q, want the last log it read (%q) so the caller can print it", got, text)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("gave up after %s on a 200ms deadline — it is not waiting for the marker at all", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %s on a 200ms deadline — the caller's timeout is not being honoured", elapsed)
	}
}

func TestWaitForLogMarker_WaitsForALateMarker(t *testing.T) {
	sb := &syncBuf{}
	sb.Write([]byte("startup: socket-listen done\n"))
	const late = 200 * time.Millisecond
	timer := time.AfterFunc(late, func() { sb.Write([]byte("msg=ready socket=/x pid=1\n")) })
	t.Cleanup(func() { timer.Stop() })

	start := time.Now()
	got, ok := waitForLogMarker(sb.String, "msg=ready", 10*time.Second)
	elapsed := time.Since(start)
	if !ok {
		t.Fatalf("missed a marker written %s after the call — a single early read, not a poll\n%s", late, got)
	}
	if !strings.Contains(got, "msg=ready") {
		t.Errorf("returned a snapshot without the marker it reported finding:\n%s", got)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("returned found after %s, before the marker was written at %s — it cannot have observed it", elapsed, late)
	}
}

// TestWaitForLogMarker_AnchorFollowsTheRSSMarker grades the ANCHOR, not the
// helper's mechanics: it replays run()'s startup emission order through the
// same slog TextHandler the e2e fixture uses and requires that the snapshot
// serveStartupComplete stops on ALREADY CONTAINS the RSS marker. Reverting the
// constant to the pre-#7222 proxy ("startup: socket-listen done"), or to any
// earlier line, fails here — whereas the e2e test alone cannot tell the two
// apart: on an idle machine "ready" is already in the buffer by the time the
// old capture point was reached.
//
// LIMITATION: this MODELS run()'s emission order, it does not derive it. It
// pins "the anchor is not earlier than the marker", not "run() still emits
// them in that order". The latter residual is only covered by running the e2e
// test against a tree whose scheduler is artificially delayed (the #7222
// review probe); nothing committed covers it.
func TestWaitForLogMarker_AnchorFollowsTheRSSMarker(t *testing.T) {
	sb := &syncBuf{}
	lg := slog.New(slog.NewTextHandler(sb, nil))
	// run()'s order: socket bound, THEN the engine plane arms the budget,
	// THEN "ready". (server.go: socket-listen done -> startEnginePlane ->
	// logger.Info("ready", ...); engineplane.go logs the marker before it
	// returns.)
	lg.Info("startup: socket-listen done")
	go func() {
		time.Sleep(50 * time.Millisecond)
		lg.Info("scheduler: RSS-budget admission control enabled", "budget_mb", 2048)
		time.Sleep(50 * time.Millisecond)
		lg.Info("ready", "socket", "/x", "pid", 1)
	}()

	got, ok := waitForLogMarker(sb.String, serveStartupComplete, 10*time.Second)
	if !ok {
		t.Fatalf("anchor %q never matched run()'s startup log:\n%s", serveStartupComplete, got)
	}
	if !strings.Contains(got, "RSS-budget admission control enabled") {
		t.Errorf("anchor %q matched a snapshot that PREDATES the RSS marker — that is a proxy for startup, not a happens-after anchor:\n%s", serveStartupComplete, got)
	}
}

func TestWaitForLogMarker_ReturnsAsSoonAsThePresentMarkerIsSeen(t *testing.T) {
	start := time.Now()
	_, ok := waitForLogMarker(func() string { return "msg=ready socket=/x pid=1\n" }, "msg=ready", 30*time.Second)
	elapsed := time.Since(start)
	if !ok {
		t.Fatal("did not find a marker that was present on the first read")
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %s to return on an already-present marker — it is sleeping out the deadline instead of polling", elapsed)
	}
}

// TestRSSBudgetAdmissionControl_IsEnginePlaneOnly is the companion check to the
// GOMEMLIMIT split (#6045 item 3). The reporter saw
// `scheduler: RSS-budget admission control enabled budget_mb=2048` and assumed
// it was doubled the same way. It is NOT: the budget belongs to the scheduler,
// which lives in the engine plane, and split-mode serve skips the engine plane
// entirely — so exactly ONE process ever arms it. This test pins that so a
// future change that starts the engine plane inside serve cannot silently
// reintroduce the doubling for the RSS budget.
func TestRSSBudgetAdmissionControl_IsEnginePlaneOnly(t *testing.T) {
	const marker = "RSS-budget admission control enabled"

	// Monolith: exactly one process, and it MUST arm the budget. (This half is
	// what proves the fixture can exhibit the marker at all.)
	if logged := runServeCapturingLog(t, "0"); !strings.Contains(logged, marker) {
		t.Fatalf("monolith: expected %q in serve's log (fixture cannot exhibit the marker)\n%s", marker, logged)
	}

	// Split: serve must NOT arm it — the engine child owns the scheduler.
	logged := runServeCapturingLog(t, "1")
	if strings.Contains(logged, marker) {
		t.Errorf("split-mode serve armed the RSS budget too — that is a second per-process doubling\n%s", logged)
	}
}

// TestEngineMemLimitHelper is the subprocess entrypoint for
// TestRunEngine_AppliesEngineShare. It runs the REAL daemon.RunEngine, waits
// for the runtime soft limit to move off the Go default, prints it, and exits.
// Inert unless GRAFEL_ENGINE_MEMLIMIT_HELPER=1.
func TestEngineMemLimitHelper(t *testing.T) {
	if os.Getenv("GRAFEL_ENGINE_MEMLIMIT_HELPER") != "1" {
		return
	}
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("helper layout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = daemon.RunEngine(ctx, daemon.EngineConfig{Config: daemon.Config{Layout: layout}})
	}()
	mb := waitForRuntimeMemLimit(30 * time.Second)
	// Deliberately os.Exit: RunEngine blocks until SIGTERM and we only need
	// the limit it applied.
	os.Stdout.WriteString("ENGINE_MEMLIMIT_MB=" + strconv.FormatInt(mb, 10) + "\n")
	os.Exit(0)
}

var engineMemLimitRe = regexp.MustCompile(`ENGINE_MEMLIMIT_MB=(-?\d+)`)

// TestRunEngine_AppliesEngineShare binds the engine half of the fix to the
// path that actually runs: a real daemon.RunEngine in a separate process must
// apply its SHARE, not the whole installation budget.
func TestRunEngine_AppliesEngineShare(t *testing.T) {
	root := shortTempRoot(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("self exe: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestEngineMemLimitHelper", "-test.timeout=90s")
	cmd.Env = append(os.Environ(),
		"GRAFEL_ENGINE_MEMLIMIT_HELPER=1",
		"GRAFEL_ENGINE_CHILD_HELPER=",
		daemon.EnvRoot+"="+root,
		"GRAFEL_HOME="+root,
		daemon.EnvDisableSelfDefense+"=1",
		"GOMEMLIMIT=",
		"GRAFEL_DAEMON_MEMLIMIT_MB="+strconv.Itoa(memLimitTotalForPlaneTests),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("engine helper subprocess: %v\n%s", err, out)
	}
	m := engineMemLimitRe.FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("helper did not report a limit\n%s", out)
	}
	gotMB, _ := strconv.ParseInt(m[1], 10, 64)
	_, wantEngine := daemon.SplitMemLimitMB(memLimitTotalForPlaneTests)
	if gotMB != wantEngine {
		t.Errorf("engine plane applied %dMB, want its share %dMB of the %dMB installation budget",
			gotMB, wantEngine, memLimitTotalForPlaneTests)
	}
	if gotMB == memLimitTotalForPlaneTests {
		t.Errorf("engine plane took the WHOLE %dMB budget — this is the #6045 per-process doubling",
			memLimitTotalForPlaneTests)
	}
	// The log line the reporter saw must now carry the plane + the total.
	logged := string(out)
	for _, want := range []string{"plane=engine", "total_mb=" + strconv.Itoa(memLimitTotalForPlaneTests)} {
		if !strings.Contains(logged, want) {
			t.Errorf("engine soft-limit log line missing %q\n%s", want, logged)
		}
	}
}
