package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// #6179 F6: TestWatchExitClassification iterates a table the test itself
// declares, so it can only check the reasons it already knows about — a fifth
// exit reason, or a new bare `return err` inside runWatch, passes it unchanged
// while silently reintroducing exactly the defect #6179 is about. These tests
// read the actual source instead, so they fail on code that exists rather than
// on code the test happened to enumerate.

// parseWatchFiles returns the parsed AST for the watch sources.
func parseWatchFiles(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, name := range []string{"watch.go", "watch_flap.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		out[name] = f
	}
	return fset, out
}

func findFunc(f *ast.File, name string) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && fd.Recv == nil {
			return fd
		}
	}
	return nil
}

// TestEveryWatchExitReasonIsClassified walks the watchExitReason const block in
// the source and requires each declared constant to appear as a key in the
// watchExitRespawn map literal. Adding a reason without classifying it is the
// exact shape of the original defect — an exit path whose respawn behaviour was
// decided by accident.
func TestEveryWatchExitReasonIsClassified(t *testing.T) {
	_, files := parseWatchFiles(t)
	f := files["watch.go"]

	// Collect declared const names of type watchExitReason.
	declared := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != "watchExitReason" {
				continue
			}
			for _, name := range vs.Names {
				declared[name.Name] = true
			}
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("found no watchExitReason constants — has the type been renamed?")
	}

	// Collect the keys of the watchExitRespawn map literal.
	classified := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != "watchExitRespawn" || i >= len(vs.Values) {
				continue
			}
			cl, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range cl.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok {
						classified[id.Name] = true
					}
				}
			}
		}
		return true
	})

	for name := range declared {
		if !classified[name] {
			t.Errorf("exit reason %s is declared but has no entry in watchExitRespawn — "+
				"every way `grafel watch` can terminate must be classified deliberately, "+
				"because launchd decides whether to respawn purely from the exit status (#6179)", name)
		}
	}
	for name := range classified {
		if !declared[name] {
			t.Errorf("watchExitRespawn classifies %s, which is not a declared watchExitReason", name)
		}
	}
}

// TestRunWatchReturnsOnlyThroughWatchExit walks every return statement in
// runWatch and requires it to be a call to watchExit or a call to a helper that
// itself returns a watchExit result. A bare `return err` there is how the
// original defect looked: an exit whose status nobody chose.
func TestRunWatchReturnsOnlyThroughWatchExit(t *testing.T) {
	fset, files := parseWatchFiles(t)
	fn := findFunc(files["watch.go"], "runWatch")
	if fn == nil {
		t.Fatal("runWatch not found")
	}

	// Helpers whose own returns are themselves watchExit results, verified
	// separately (recordWatchStart is checked below).
	allowedHelpers := map[string]bool{"recordWatchStart": true}

	// `if stop, err := recordWatchStart(repo); stop { return err }` is the
	// legitimate way to propagate a verified helper's already-classified
	// result. Collect the body ranges of such if-statements so a bare
	// `return err` inside one is accepted — and only there.
	type span struct{ lo, hi token.Pos }
	var helperGuards []span
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || ifs.Init == nil {
			return true
		}
		as, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && allowedHelpers[id.Name] {
			helperGuards = append(helperGuards, span{ifs.Body.Pos(), ifs.Body.End()})
		}
		return true
	})
	insideHelperGuard := func(p token.Pos) bool {
		for _, s := range helperGuards {
			if p >= s.lo && p <= s.hi {
				return true
			}
		}
		return false
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		// Do not descend into nested function literals: their returns belong
		// to the closure, not to runWatch's exit status.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		pos := fset.Position(ret.Pos())
		if len(ret.Results) != 1 {
			t.Errorf("%s: runWatch returns %d values; expected exactly one error routed "+
				"through watchExit (#6179)", pos, len(ret.Results))
			return true
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			// Propagating an already-classified result out of a verified
			// helper's guard is the one non-call form allowed.
			if _, isIdent := ret.Results[0].(*ast.Ident); isIdent && insideHelperGuard(ret.Pos()) {
				return true
			}
			t.Errorf("%s: runWatch has a return that does not go through watchExit — every "+
				"exit path must classify itself, or launchd's respawn decision is made by "+
				"accident (#6179)", pos)
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || (id.Name != "watchExit" && !allowedHelpers[id.Name]) {
			t.Errorf("%s: runWatch returns the result of %s; only watchExit (or a verified "+
				"helper) may decide the exit status (#6179)", pos, exprName(call.Fun))
		}
		return true
	})
}

// TestRecordWatchStartReturnsThroughWatchExit is the companion for the one
// helper runWatch is allowed to return directly.
func TestRecordWatchStartReturnsThroughWatchExit(t *testing.T) {
	fset, files := parseWatchFiles(t)
	fn := findFunc(files["watch_flap.go"], "recordWatchStart")
	if fn == nil {
		t.Fatal("recordWatchStart not found")
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		if id, ok := ret.Results[0].(*ast.Ident); ok && id.Name == "nil" {
			return true // "not flapping" is a legitimate nil
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			t.Errorf("%s: recordWatchStart returns a non-nil value that is not a watchExit call",
				fset.Position(ret.Pos()))
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "watchExit" {
			t.Errorf("%s: recordWatchStart returns %s; must go through watchExit",
				fset.Position(ret.Pos()), exprName(call.Fun))
		}
		return true
	})
}

// TestSignalHandlerArmedBeforeAnyFallibleWork pins #6179 F2 structurally.
//
// Until signal.Notify runs, a SIGTERM kills the process by default action —
// death BY a signal, which launchd and systemd both read as an unsuccessful
// exit and respawn. The "a stop request means exit 0 and stay stopped" contract
// therefore holds only from the moment Notify is installed, and it must not sit
// behind a filesystem stat or anything else that can block.
//
// The end-to-end signal test cannot catch a regression here: it raises SIGTERM
// in-process, so the handler is always already armed by the time it fires.
//
// DO NOT over-trust this test. It asserts a PROXY — that signal.Notify's source
// byte offset precedes os.Stat's inside runWatch — not the property anyone
// actually cares about, which is "armed before any SIGTERM can arrive". Those
// are different, and the gap is measured, not theoretical: even with this test
// green there is a ~70ms window before runWatch is entered at all (Go runtime
// init, cobra tree construction, flag parsing) during which a SIGTERM kills the
// process by default action and the supervisor respawns it. Closing that would
// mean arming signals in main(), which is deliberately not done — see the
// argument at cmd/grafel/main.go's quick-doctor guard. This test is worth
// keeping because it kills the specific regression of moving Notify back below
// the stat; it is not evidence that the window is zero.
func TestSignalHandlerArmedBeforeAnyFallibleWork(t *testing.T) {
	fset, files := parseWatchFiles(t)
	fn := findFunc(files["watch.go"], "runWatch")
	if fn == nil {
		t.Fatal("runWatch not found")
	}

	notifyOffset, statOffset := -1, -1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		off := fset.Position(call.Pos()).Offset
		switch {
		case pkg.Name == "signal" && sel.Sel.Name == "Notify" && notifyOffset < 0:
			notifyOffset = off
		case pkg.Name == "os" && sel.Sel.Name == "Stat" && statOffset < 0:
			statOffset = off
		}
		return true
	})

	if notifyOffset < 0 {
		t.Fatal("runWatch no longer calls signal.Notify — SIGTERM would kill the watcher by " +
			"default action, which both launchd and systemd respawn (#6179 F2)")
	}
	if statOffset >= 0 && notifyOffset > statOffset {
		t.Errorf("signal.Notify (offset %d) runs AFTER os.Stat (offset %d); a SIGTERM arriving "+
			"in that window kills the watcher by signal, which the supervisor reads as an "+
			"unsuccessful exit and respawns (#6179 F2)", notifyOffset, statOffset)
	}
}

// TestQuickDoctorSkippedForWatch pins the other half of F2: the preflight in
// cmd/grafel/main.go ran before cobra dispatch, so it was ~100ms during which
// no watcher signal handler existed — and 140 daemon health probes fired at
// login for no reader.
func TestQuickDoctorSkippedForWatch(t *testing.T) {
	b, err := os.ReadFile("../../cmd/grafel/main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(b)
	i := strings.Index(src, "runQuickDoctorHook()")
	if i < 0 {
		t.Skip("quick-doctor hook no longer present")
	}
	// The guard is the `if` immediately preceding the call.
	start := strings.LastIndex(src[:i], "if ")
	if start < 0 {
		t.Fatal("could not locate the quick-doctor guard")
	}
	guard := src[start:i]
	if !strings.Contains(guard, `"watch"`) {
		t.Errorf("the quick-doctor preflight is not skipped for `watch`; it runs before cobra "+
			"dispatch, so it is dead time during which the watcher has no signal handler, and "+
			"at 140 repos it is 140 daemon probes at login (#6179 F2)\nguard: %s", guard)
	}
}

func exprName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprName(v.X) + "." + v.Sel.Name
	}
	return "<expr>"
}
