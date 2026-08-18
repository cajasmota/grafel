package dashboard

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// #6325 F1: defaultUpdateRunner spawns the console-subsystem grafel.exe from
// INSIDE the daemon process (reachable from POST /api/updates/apply and
// /api/updates/refresh-rules). Since #6320 the daemon itself has no console,
// so Windows allocates a fresh one for this child and clicking "update" in the
// dashboard pops a console window. Every other daemon-resident spawn in the
// tree already calls executil.NoWindow; this was the lone gap.
//
// The behavioural assertion (CREATE_NO_WINDOW actually set on the CreationFlags)
// lives in handlers_updates_nowindow_windows_test.go — executil.NoWindow is a
// no-op on every other GOOS (internal/executil/nowindow_other.go), so a runtime
// assertion here would pass vacuously on macOS/Linux and would keep passing
// with the fix deleted. This source-level guard is the honest portable half:
// it fails on any platform the moment the call is removed.
func TestNewUpdateCmd_AppliesNoWindow_SourceGuard(t *testing.T) {
	body := funcBodySource6325(t, "handlers_updates.go", "newUpdateCmd")
	if !strings.Contains(body, "executil.NoWindow(") {
		t.Fatalf("newUpdateCmd does not call executil.NoWindow — the daemon-spawned "+
			"`grafel update` child pops a console window on Windows (#6325 F1)\n%s", body)
	}
}

// TestDefaultUpdateRunner_UsesNewUpdateCmd pins the wiring: the no-window
// treatment is worthless if the runner that the SSE handlers actually reach
// builds its own exec.Cmd on the side.
func TestDefaultUpdateRunner_UsesNewUpdateCmd(t *testing.T) {
	body := funcBodySource6325(t, "handlers_updates.go", "defaultUpdateRunner")
	if !strings.Contains(body, "newUpdateCmd(") {
		t.Fatalf("defaultUpdateRunner no longer routes through newUpdateCmd, so it "+
			"bypasses the CREATE_NO_WINDOW treatment (#6325 F1)\n%s", body)
	}
	if strings.Contains(body, "exec.CommandContext(") || strings.Contains(body, "exec.Command(") {
		t.Fatalf("defaultUpdateRunner builds its own exec.Cmd; the console-hiding flag "+
			"belongs on every path (#6325 F1)\n%s", body)
	}
}

// TestNewUpdateCmd_TargetsSelfExecutable is the portable behavioural half: the
// command really is `<self> <args...>`.
func TestNewUpdateCmd_TargetsSelfExecutable(t *testing.T) {
	cmd, err := newUpdateCmd(context.Background(), []string{"update", "--refresh-rules-lite"})
	if err != nil {
		t.Fatalf("newUpdateCmd: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if cmd.Path != self {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, self)
	}
	want := []string{self, "update", "--refresh-rules-lite"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
		}
	}
}

// funcBodySource6325 returns the source text of the named top-level function
// in the named file of this package.
func funcBodySource6325(t *testing.T, file, name string) string {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		return string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
	}
	t.Fatalf("function %s not found in %s", name, file)
	return ""
}
