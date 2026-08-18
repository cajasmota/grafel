package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #6325 F3: the wrapper was published with os.WriteFile, which truncates in
// place. taskWrapperPath() is a pure function of the fixed taskName, so the
// path is stable across versions — the already-registered task points at that
// exact file, and the truncate-then-write window is readable by the OS at
// arbitrary times. A LogonTrigger firing or a RestartOnFailure retry landing
// mid-write hands wscript.exe a truncated .vbs.
//
// This asserts the property that distinguishes temp+rename from truncate:
// the destination gets a NEW inode, so any handle already resolved to the old
// file keeps seeing complete, valid old content.
func TestWriteServiceArtifact_ReplacesRatherThanTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "com.grafel.daemon.vbs")

	const oldContent = "OLD WRAPPER CONTENT — a complete, runnable script\n"
	if err := os.WriteFile(path, []byte(oldContent), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Hold a handle open across the write, the way a scheduler-launched
	// wscript.exe would — on unix only, and the reason is worth stating
	// plainly because it bounds what F3 actually buys on the platform this
	// code runs on.
	//
	// "A handle opened before the write still reads the complete previous
	// content" is a POSIX property. rename(2) leaves the old inode alive for
	// anyone already holding it. Windows has no equivalent: atomicfile goes
	// renameAtomic -> os.Rename -> syscall.Rename ->
	// MoveFileEx(MOVEFILE_REPLACE_EXISTING), which needs DELETE access on the
	// destination, and Go's syscall.Open opens with a sharemode of
	// FILE_SHARE_READ|FILE_SHARE_WRITE and no FILE_SHARE_DELETE
	// (go1.26 src/syscall/syscall_windows.go:395, unconditional). So this
	// exact scenario does not merely behave differently on Windows — it
	// deterministically fails the rename, burns the ~40x5ms retry budget and
	// surfaces as an install error. It is skipped because the property is
	// false there, not because it is flaky.
	//
	// What F3 buys on Windows is therefore the weaker but still real
	// guarantee: the destination is never OBSERVABLE half-written. The
	// failure mode for a scheduler launch racing an install changes from a
	// silently truncated .vbs (a dead daemon with no console and no log) to a
	// loud, retried, reported install error. That is the whole point.
	//
	// The inode-identity assertion below is the platform-independent half and
	// runs everywhere.
	var f *os.File
	if runtime.GOOS != "windows" {
		f, err = os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	}

	newContent := []byte("NEW WRAPPER CONTENT\n")
	if err := writeServiceArtifact(path, newContent, 0o600); err != nil {
		t.Fatalf("writeServiceArtifact: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("writeServiceArtifact wrote through the SAME file identity — it truncated the " +
			"live file in place instead of temp+rename; a concurrent scheduler launch can read " +
			"a half-written wrapper (#6325 F3)")
	}

	// The pre-existing handle must still see the complete old file (unix only,
	// per the note above).
	if f != nil {
		buf := make([]byte, len(oldContent)+len(newContent))
		n, _ := f.ReadAt(buf, 0)
		if string(buf[:n]) != oldContent {
			t.Errorf("a handle opened before the write now reads %q; with temp+rename it must still "+
				"read the complete previous content %q (#6325 F3)", string(buf[:n]), oldContent)
		}
	}

	// And the destination really holds the new bytes.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newContent) {
		t.Errorf("destination = %q, want %q", got, newContent)
	}

	// No temp debris left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("writeServiceArtifact left temp debris in the task directory: %v", names)
	}
}

// TestWriteUnit_UsesAtomicArtifactWrites is the source-level half: WriteUnit
// itself is Windows-only, so this is what a macOS/Linux developer can run. It
// also pins the comment correction — the pre-#6325 rationale claimed writing
// the wrapper first preserved the previous action's script, which was false.
func TestWriteUnit_UsesAtomicArtifactWrites(t *testing.T) {
	src := readSourceFile6325(t, "schtasks_windows.go")
	body := funcBodySource6325svc(t, "schtasks_windows.go", "WriteUnit")
	if strings.Contains(body, "os.WriteFile(") {
		t.Errorf("WriteUnit still publishes an artifact with os.WriteFile, which truncates the "+
			"live file in place (#6325 F3)\n%s", body)
	}
	if strings.Count(body, "writeServiceArtifact(") != 2 {
		t.Errorf("WriteUnit must publish BOTH the wrapper and the task XML through "+
			"writeServiceArtifact (#6325 F3)\n%s", body)
	}
	// The false rationale must be gone. It is load-bearing: whoever reads it
	// next would conclude the ordering already protects the live action.
	if strings.Contains(src, "still points at its previous valid action") {
		t.Error("the wrong ordering rationale is still in schtasks_windows.go — taskWrapperPath() " +
			"is a pure function of the fixed taskName, so the path is stable and the old write " +
			"destroyed the live action's script rather than preserving it (#6325 F3)")
	}
}

func readSourceFile6325(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	return string(b)
}

func funcBodySource6325svc(t *testing.T, file, name string) string {
	t.Helper()
	src := []byte(readSourceFile6325(t, file))
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
