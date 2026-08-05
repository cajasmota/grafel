package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
	"testing"
)

// #6179 F1: the fix to the unit template is worthless unless an upgrade path
// rewrites the units already on disk. These tests pin the two hooks that make
// that happen, because both are easy to delete without any other test noticing.

// TestRefreshState_ReconcilesWatcherUnits pins the hook the curl installer
// reaches. install.sh's record_install_state runs `grafel install
// --refresh-state` with the NEWLY placed binary — the only automatic point in
// the curl upgrade path where new-template code executes. If runRefreshState
// stops calling the reconcile, a curl upgrade silently leaves every watcher
// unit stale and the storm survives the fix.
func TestRefreshState_ReconcilesWatcherUnits(t *testing.T) {
	withSandboxHome(t)

	calls := 0
	var gotBin string
	prev := reconcileWatcherUnits
	reconcileWatcherUnits = func(_ io.Writer, bin string) {
		calls++
		gotBin = bin
	}
	t.Cleanup(func() { reconcileWatcherUnits = prev })

	// No install.json in the sandbox: RefreshState reports HadState=false and
	// returns nil. The reconcile must run anyway — the units on disk are stale
	// regardless of whether install.json happens to exist.
	if err := runRefreshState(&bytes.Buffer{}); err != nil {
		t.Fatalf("runRefreshState: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runRefreshState called the watcher reconcile %d times, want 1 — without it "+
			"a curl upgrade never rewrites the stale watcher units (#6179 F1)", calls)
	}
	if gotBin == "" {
		t.Error("reconcile was called without a binary path; the rendered unit body includes it")
	}
}

// TestSelfUpdate_ReInvokesNewBinaryForWatcherUnits pins the second hook.
// RunUpdate's post-swap step is RunCopy, which never touches watcher units, and
// the pre-swap Apply in the legacy loop renders the OLD template. Re-invoking
// the new binary is the only way `grafel update` reaches the new template.
func TestSelfUpdate_ReInvokesNewBinaryForWatcherUnits(t *testing.T) {
	src := readSource(t, "update.go")
	fn := funcBody(t, src, "runSelfUpdate")
	if !strings.Contains(fn, "refreshWatcherUnitsWithNewBinary") {
		t.Fatal("runSelfUpdate no longer refreshes watcher units with the new binary (#6179 F1)")
	}
	// It has to run before the Skipped early-return, or the very machine that
	// most needs the repair — already on the latest binary, stale units from an
	// update that predates this fix — never gets it.
	iCall := strings.Index(fn, "refreshWatcherUnitsWithNewBinary")
	iSkip := strings.Index(fn, "result.Skipped")
	if iSkip >= 0 && iCall > iSkip {
		t.Errorf("the watcher reconcile runs after the result.Skipped early-return; a machine "+
			"already on the latest binary with stale units would never be repaired (#6179 F1)\n%s", fn)
	}
}

// TestUpdateLegacyLoop_SkipsWatchers: the pre-swap Apply must not write watcher
// units. It runs with the OLD binary, so it would render the OLD template and
// re-register all 140 units with the settings being replaced — then the
// post-swap reconcile would do it all again. Two notification bursts to reach
// what one reaches.
func TestUpdateLegacyLoop_SkipsWatchers(t *testing.T) {
	src := readSource(t, "update.go")
	fn := funcBody(t, src, "newUpdateCmd")
	if !strings.Contains(fn, "SkipWatchers: true") {
		t.Fatalf("update's pre-swap install.Apply does not set SkipWatchers; it would write "+
			"the OLD unit template with the OLD binary (#6179 F1)\n%s", fn)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// funcBody returns the source text of the named top-level func.
func funcBody(t *testing.T, src, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name || fd.Body == nil {
			continue
		}
		return src[fset.Position(fd.Body.Pos()).Offset:fset.Position(fd.Body.End()).Offset]
	}
	t.Fatalf("func %s not found", name)
	return ""
}
