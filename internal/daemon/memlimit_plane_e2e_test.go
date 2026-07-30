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
	waitDaemonReady(t, layout.SocketPath, 20*time.Second)
	return sb.String()
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
