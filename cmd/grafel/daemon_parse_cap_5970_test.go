package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/caps"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/process"
)

// daemon_parse_cap_5970_test.go — the daemon's OWN in-process parse gate is
// sized from the background core budget (#5970).
//
// WHY THESE TESTS LOOK LIKE THIS. #5972 and #5973 both landed "the 25% budget"
// and both left cmd/grafel/daemon.go sizing its parse gate from
// runtime.GOMAXPROCS(0) — 100% of the daemon's runtime parallelism, which on a
// default install is half the box and on a GRAFEL_DAEMON_GOMAXPROCS-less,
// unknown-core host is all of it. A test that asserts process.IndexCoreBudget()
// returns 3 on a 12-core machine would have passed throughout. So these tests
// assert two things the helper-level test cannot:
//
//  1. the value that actually reaches indexstate (the gate the parse calls
//     block on) when the DAEMON installs it, and
//  2. that runDaemonMode — the one function every `grafel serve` / `grafel
//     engine` process runs — is what installs it, and does not compute a cap
//     of its own alongside.
//
// (2) is a source-level assertion on purpose. runDaemonMode binds sockets,
// spawns children and blocks forever, so it cannot be called from a unit test;
// without the structural check the behavioural test would pass against a
// daemon that quietly kept its old cap — the exact failure mode this issue is.

// TestDaemonParseCap_IsTheBackgroundCoreBudget pins the SIZE of the daemon's
// parse cap: the canonical 25%-of-machine background budget, not the daemon's
// GOMAXPROCS.
func TestDaemonParseCap_IsTheBackgroundCoreBudget(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")

	for _, hostCPU := range []int{1, 2, 4, 8, 12, 16, 32, 64} {
		want := process.IndexCoreBudgetFor(hostCPU)
		if got := daemonParseCap(hostCPU, 0); got != want {
			t.Errorf("daemonParseCap(hostCPU=%d) = %d, want %d (25%% budget)", hostCPU, got, want)
		}
	}
	// The concrete number the issue is about: a 12-core laptop.
	if got := daemonParseCap(12, 0); got != 3 {
		t.Errorf("daemonParseCap(12) = %d, want 3", got)
	}
	// And it must be strictly below the daemon's own runtime parallelism on any
	// host big enough for the two to differ — that gap IS the bug.
	if got := daemonParseCap(12, 0); got >= 12 {
		t.Errorf("daemonParseCap(12) = %d, want < 12 (must not be GOMAXPROCS-shaped)", got)
	}
}

// TestDaemonParseCap_NeverUnbounded guards the floor: 0 means "unbounded" to
// the gate, which is the opposite of what a degenerate core count must produce.
func TestDaemonParseCap_NeverUnbounded(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")
	for _, hostCPU := range []int{-4, 0, 1} {
		if got := daemonParseCap(hostCPU, 0); got < 1 {
			t.Errorf("daemonParseCap(%d) = %d, want >= 1", hostCPU, got)
		}
	}
}

// TestDaemonParseCap_OperatorOverrideWinsUnclamped pins the escape hatch: an
// explicit GRAFEL_DAEMON_GOMAXPROCS is an operator decision and is honoured
// as-is, in BOTH directions — tighter than the budget and looser than it.
func TestDaemonParseCap_OperatorOverrideWinsUnclamped(t *testing.T) {
	cases := []struct {
		env     string
		hostCPU int
		want    int
	}{
		{"2", 12, 2},   // tighter than the 25% budget (3)
		{"9", 12, 9},   // looser than the budget, and below host cores
		{"12", 12, 12}, // "give the daemon the whole box": never clamped back to 3
		{"0", 12, 3},   // invalid/zero falls through to the budget
		{"junk", 12, 3},
	}
	for _, c := range cases {
		t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", c.env)
		if got := daemonParseCap(c.hostCPU, 0); got != c.want {
			t.Errorf("GRAFEL_DAEMON_GOMAXPROCS=%q daemonParseCap(%d) = %d, want %d",
				c.env, c.hostCPU, got, c.want)
		}
	}
}

// TestInstallDaemonParseCap_ReachesTheGate is the behavioural half of (1): the
// value the daemon computes is the value in force on indexstate's semaphore —
// the thing an actual ts_parser_parse blocks on.
func TestInstallDaemonParseCap_ReachesTheGate(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })
	indexstate.SetParseConcurrency(0)

	installed := installDaemonParseCap(12, nil)
	if installed != 3 {
		t.Fatalf("installDaemonParseCap(12) reported %d, want 3", installed)
	}
	if got := indexstate.ParseConcurrencyCap(); got != 3 {
		t.Fatalf("gate cap after install = %d, want 3", got)
	}
	if got := indexstate.EffectiveParseConcurrencyCap(); got != 3 {
		t.Fatalf("effective gate cap after install = %d, want 3 (background, no foreground hold)", got)
	}
}

// TestRunDaemonMode_InstallsTheBackgroundParseCap is (2): the structural pin.
// It asserts against the AST of runDaemonMode — the single function `grafel
// serve` and `grafel engine` both run — that the cap installed on the daemon
// path is the one this issue fixed, and that no second, GOMAXPROCS-sized
// computation sits beside it.
func TestRunDaemonMode_InstallsTheBackgroundParseCap(t *testing.T) {
	fn := funcDeclInDaemonGo(t, "runDaemonMode")

	call := findCall(fn, "installDaemonParseCap")
	if call == nil {
		t.Fatal("runDaemonMode does not call installDaemonParseCap: the daemon's " +
			"in-process parse gate is not sized from the background core budget (#5970)")
	}
	// A structural "the call exists" check is value-blind: it passes against
	// installDaemonParseCap(runtime.NumCPU() * 4), which on a 12-core box
	// installs a cap of 12 — the original bug's exact magnitude. Pin the
	// argument too: the daemon must pass the HOST core count, unscaled, and let
	// daemonParseCap (unit-tested above across core counts) apply the policy.
	if len(call.Args) != 2 {
		t.Fatalf("installDaemonParseCap called with %d args, want 2 (hostCPU, capStore)", len(call.Args))
	}
	if got := exprString(call.Args[0]); got != "runtime.NumCPU()" {
		t.Errorf("runDaemonMode passes hostCPU=%s to installDaemonParseCap, want runtime.NumCPU() "+
			"— a scaled or GOMAXPROCS-derived argument re-opens #5970 with the call still in place", got)
	}
	// The cpu.json pin is an operator override with the same precedence as
	// GRAFEL_DAEMON_GOMAXPROCS; passing a nil store here would silently drop it.
	if got := exprString(call.Args[1]); got == "nil" {
		t.Error("runDaemonMode passes a nil caps store to installDaemonParseCap: " +
			"the cpu.json daemon-GOMAXPROCS pin would be silently ignored")
	}
	// A direct SetParseConcurrency here would be a second, competing cap — the
	// shape of the original bug (parseCap := runtime.GOMAXPROCS(0)).
	if bodyCallsSelector(fn, "indexstate", "SetParseConcurrency") {
		t.Error("runDaemonMode calls indexstate.SetParseConcurrency directly; the parse " +
			"cap must be resolved through installDaemonParseCap (#5970)")
	}
}

// TestForegroundInProcessIndexPathsLiftTheCap pins the foreground exemption on
// the two daemon-side entrypoints that run a USER-AWAITED index inside the
// daemon process: the synchronous `grafel index` RPC and the rebuild core (whose
// indexFn runs in-process when GRAFEL_SUBPROCESS_INDEXER=0). Capping those at
// 25% would throttle work a human is sitting and waiting for, which the policy
// explicitly exempts.
func TestForegroundInProcessIndexPathsLiftTheCap(t *testing.T) {
	for _, name := range []string{"daemonIndexFunc", "daemonRebuildFuncCore"} {
		fn := funcDeclInDaemonGo(t, name)
		if !bodyCallsSelector(fn, "indexstate", "BeginForegroundParse") {
			t.Errorf("%s does not take a foreground parse hold: user-awaited in-process "+
				"indexing would run under the background 25%% cap (#5970)", name)
		}
	}
}

// TestRebuildForegroundHoldIsGatedOnTheInProcessPath is the counterweight to
// the test above. The lift is PROCESS-WIDE: while it is held, background
// parsing is uncapped too (see indexstate.BeginForegroundParse). On the shipped
// default the rebuild forks its per-repo index and its link pass does no
// tree-sitter parsing, so an unconditional hold there would suspend the 25%
// budget for the whole multi-minute rebuild window and buy that rebuild
// nothing — while the watcher-driven reindex, which the barge does not gate,
// keeps parsing. So the hold must be taken only on the in-process opt-out.
func TestRebuildForegroundHoldIsGatedOnTheInProcessPath(t *testing.T) {
	fn := funcDeclInDaemonGo(t, "daemonRebuildFuncCore")

	stmt := enclosingIfStmt(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "BeginForegroundParse"
	})
	if stmt == nil {
		t.Fatal("daemonRebuildFuncCore takes its foreground parse hold unconditionally; " +
			"it must be gated on !sched.SubprocessIndexEnabled() so the default (forking) " +
			"path does not suspend the background parse budget for nothing (#5970)")
	}
	cond := exprString(stmt.Cond)
	if cond != "!sched.SubprocessIndexEnabled()" {
		t.Errorf("rebuild foreground hold is gated on %q, want !sched.SubprocessIndexEnabled()", cond)
	}
}

// TestDaemonParseCap_HonoursCPUJSONPin pins the cpu.json half of the operator
// override. cpu.json is the live-mutable, SIGHUP-reloadable surface for the
// daemon's CPU knobs; a pin there that the parse gate ignores is a
// silently-ineffective operator control — the hardest kind to diagnose.
func TestDaemonParseCap_HonoursCPUJSONPin(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")

	// cpu.json pin beats the 25% budget, in both directions.
	if got := daemonParseCap(64, 1); got != 1 {
		t.Errorf("daemonParseCap(64, cpu.json=1) = %d, want 1 (budget would be 16)", got)
	}
	if got := daemonParseCap(12, 10); got != 10 {
		t.Errorf("daemonParseCap(12, cpu.json=10) = %d, want 10 (never clamped to the budget)", got)
	}
	// Unset/degenerate cpu.json falls through to the budget.
	for _, fileVal := range []int{0, -1} {
		if got := daemonParseCap(12, fileVal); got != 3 {
			t.Errorf("daemonParseCap(12, cpu.json=%d) = %d, want the budget 3", fileVal, got)
		}
	}
	// env still outranks cpu.json — same precedence resolveDaemonGOMAXPROCSWith
	// documents for the GOMAXPROCS knob itself.
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "5")
	if got := daemonParseCap(12, 10); got != 5 {
		t.Errorf("daemonParseCap with env=5, cpu.json=10 = %d, want 5 (env > cpu.json)", got)
	}
}

// TestInstallDaemonParseCap_ReadsCPUJSONFromStore closes the loop from the file
// on disk to the value on the gate: the pin only counts if installDaemonParseCap
// actually loads it.
func TestInstallDaemonParseCap_ReadsCPUJSONFromStore(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })
	indexstate.SetParseConcurrency(0)

	dir := t.TempDir()
	path := filepath.Join(dir, "cpu.json")
	if err := os.WriteFile(path, []byte(`{"daemon_gomaxprocs": 1}`), 0o644); err != nil {
		t.Fatalf("write cpu.json: %v", err)
	}
	store := caps.NewStore(path)
	if cfg, err := store.Load(); err != nil || cfg.DaemonGOMAXPROCSValue() != 1 {
		t.Fatalf("caps store did not read the pin back (val=%v err=%v); fixture is wrong, not the code",
			func() int { c, _ := store.Load(); return c.DaemonGOMAXPROCSValue() }(), err)
	}

	if got := installDaemonParseCap(64, store); got != 1 {
		t.Fatalf("installDaemonParseCap(64, cpu.json=1) = %d, want 1", got)
	}
	if got := indexstate.ParseConcurrencyCap(); got != 1 {
		t.Fatalf("gate cap = %d, want 1 (the operator's cpu.json pin)", got)
	}
}

// TestForegroundLiftIsScopedNotGlobal proves the exemption is a scoped hold and
// not a permanent un-capping: after a foreground unit of work completes, the
// daemon's background ceiling is back in force.
func TestForegroundLiftIsScopedNotGlobal(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })
	indexstate.SetParseConcurrency(0)

	installDaemonParseCap(12, nil)
	release := indexstate.BeginForegroundParse()
	if got := indexstate.EffectiveParseConcurrencyCap(); got != 0 {
		t.Fatalf("effective cap during foreground work = %d, want 0 (uncapped)", got)
	}
	release()
	if got := indexstate.EffectiveParseConcurrencyCap(); got != 3 {
		t.Fatalf("effective cap after foreground work = %d, want the background 3", got)
	}
}

// --- AST helpers -----------------------------------------------------------

func funcDeclInDaemonGo(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "daemon.go", nil, 0)
	if err != nil {
		t.Fatalf("parse daemon.go: %v", err)
	}
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
			return fd
		}
	}
	t.Fatalf("func %s not found in cmd/grafel/daemon.go", name)
	return nil
}

// findCall returns the first call to the bare (non-selector) function name in
// fn's body, or nil.
func findCall(fn *ast.FuncDecl, name string) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != nil {
			return found == nil
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
			found = call
		}
		return true
	})
	return found
}

// enclosingIfStmt returns the innermost *ast.IfStmt in fn whose body contains a
// node satisfying match, or nil when no such node exists or it is not inside an
// if at all. Used to assert that a statement is CONDITIONAL, not merely present.
func enclosingIfStmt(fn *ast.FuncDecl, match func(ast.Node) bool) *ast.IfStmt {
	var found *ast.IfStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		hit := false
		ast.Inspect(ifs.Body, func(inner ast.Node) bool {
			if inner != nil && match(inner) {
				hit = true
			}
			return true
		})
		// Keep the innermost enclosing if: a later (deeper) candidate starts
		// after the one already recorded.
		if hit && (found == nil || ifs.Pos() > found.Pos()) {
			found = ifs
		}
		return true
	})
	return found
}

// exprString renders an expression back to source so an argument can be
// asserted on.
func exprString(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return ""
	}
	return b.String()
}

// bodyCallsSelector reports whether fn's body contains a call to pkg.name.
func bodyCallsSelector(fn *ast.FuncDecl, pkg, name string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Match both `pkg.Name(...)` and `defer pkg.Name(...)()`.
		fun := call.Fun
		if inner, ok := fun.(*ast.CallExpr); ok {
			fun = inner.Fun
		}
		sel, ok := fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if ok && id.Name == pkg && sel.Sel.Name == name {
			found = true
		}
		return true
	})
	return found
}

// TestDaemonParseCapDocumentsWhyNotGOMAXPROCS is a cheap guard against the
// regression returning by copy-paste: the daemon must not derive its parse cap
// from runtime.GOMAXPROCS again. Checked on the resolved value, using the real
// host, so it fails on any machine where the two differ.
func TestDaemonParseCapDocumentsWhyNotGOMAXPROCS(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_GOMAXPROCS", "")
	if runtime.NumCPU() < 8 {
		t.Skipf("host has %d cores; budget and GOMAXPROCS can legitimately coincide", runtime.NumCPU())
	}
	got := daemonParseCap(runtime.NumCPU(), 0)
	if got >= runtime.GOMAXPROCS(0) {
		t.Fatalf("daemonParseCap(%d) = %d >= GOMAXPROCS %d: the cap is GOMAXPROCS-shaped again (#5970)",
			runtime.NumCPU(), got, runtime.GOMAXPROCS(0))
	}
	if got != process.IndexCoreBudget() {
		t.Fatalf("daemonParseCap(NumCPU) = %d, want process.IndexCoreBudget() = %d",
			got, process.IndexCoreBudget())
	}
}
