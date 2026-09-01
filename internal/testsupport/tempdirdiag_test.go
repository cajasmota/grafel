package testsupport

// tempdirdiag_test.go — coverage for the parts of the #6512 diagnostic that
// CAN be exercised off Windows.
//
// What these tests bind: the residual ENUMERATION (does it name every leftover
// path, and does it get file-vs-directory right?), the RENDERING (does the CI
// log distinguish `dir ` from `file`?), the REMOVAL PROBE (does it report
// per-path outcomes deepest-first?), and the platform GUARD (does the helper
// add nothing on a non-Windows leg?).
//
// What they cannot bind, and this is stated rather than papered over: the
// Restart Manager holder lookup and the exclusive-open probe are Windows-only
// and are not compiled on this machine, so nothing here observes them. Nor can
// anything here reproduce the failure the diagnostic exists to explain —
// POSIX unlinks open files, which is the entire reason #6512 is invisible off
// Windows. Those two facts are the honest limit of this file.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// plantResidue builds a directory that looks like the #6512 residue: a TempDir
// base holding a `001` child which is itself non-empty. Returns the base.
func plantResidue(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	child := filepath.Join(base, "001")
	if err := os.MkdirAll(filepath.Join(child, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "settings.json"), []byte(`{"daemon_go_memory_limit_mb":8192}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "nested", "leaf.txt"), []byte("leaf"), 0o644); err != nil {
		t.Fatal(err)
	}
	return base
}

// TestEnumerateResidualNamesEveryLeftoverAndItsKind is the core claim: given a
// non-empty directory, the diagnostic reports the FULL PATH of every entry and
// whether it is a file or a directory. #6512 cannot currently tell whether
// `001` had a real child or was empty and still undeletable; this is the
// function that answers it.
func TestEnumerateResidualNamesEveryLeftoverAndItsKind(t *testing.T) {
	base := plantResidue(t)

	entries, err := EnumerateResidual(base)
	if err != nil {
		t.Fatalf("EnumerateResidual: %v", err)
	}

	wantDirs := map[string]bool{
		base:                                 true,
		filepath.Join(base, "001"):           true,
		filepath.Join(base, "001", "nested"): true,
	}
	wantFiles := map[string]bool{
		filepath.Join(base, "001", "settings.json"):      true,
		filepath.Join(base, "001", "nested", "leaf.txt"): true,
	}

	got := map[string]bool{} // path -> isDir
	for _, e := range entries {
		if _, dup := got[e.Path]; dup {
			t.Errorf("path reported twice: %s", e.Path)
		}
		got[e.Path] = e.IsDir
	}

	for p := range wantDirs {
		isDir, ok := got[p]
		if !ok {
			t.Errorf("directory %s missing from enumeration", p)
			continue
		}
		if !isDir {
			t.Errorf("%s reported as a file; it is a directory", p)
		}
		delete(got, p)
	}
	for p := range wantFiles {
		isDir, ok := got[p]
		if !ok {
			t.Errorf("file %s missing from enumeration", p)
			continue
		}
		if isDir {
			t.Errorf("%s reported as a directory; it is a file", p)
		}
		delete(got, p)
	}
	if len(got) != 0 {
		t.Errorf("enumeration reported entries that do not exist: %v", got)
	}
}

// TestEnumerateResidualRecordsSizeForFiles pins that the report carries enough
// to tell a real leftover from an empty placeholder.
func TestEnumerateResidualRecordsSizeForFiles(t *testing.T) {
	base := plantResidue(t)
	entries, err := EnumerateResidual(base)
	if err != nil {
		t.Fatalf("EnumerateResidual: %v", err)
	}
	want := filepath.Join(base, "001", "nested", "leaf.txt")
	for _, e := range entries {
		if e.Path != want {
			continue
		}
		if e.Size != int64(len("leaf")) {
			t.Fatalf("size for %s: want %d got %d", want, len("leaf"), e.Size)
		}
		if e.ModTime.IsZero() {
			t.Fatalf("mtime for %s is zero; a residual file's mtime is how a holder outside the test is spotted", want)
		}
		return
	}
	t.Fatalf("%s not enumerated", want)
}

// TestEnumerateResidualSilentOnCleanTree is the negative half: a healthy run
// (the TempDir base was removed) must produce NOTHING, or the diagnostic would
// print on every green build and be tuned out — which is the failure mode
// #6512 spends several paragraphs warning about.
func TestEnumerateResidualSilentOnCleanTree(t *testing.T) {
	base := filepath.Join(t.TempDir(), "removed-by-removeall")
	entries, err := EnumerateResidual(base)
	if err != nil {
		t.Fatalf("a non-existent root is the healthy case, not an error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want no entries for a removed base, got %d: %v", len(entries), entries)
	}
}

// TestEnumerateResidualHandlesAFileRoot guards the shape where the base itself
// is somehow not a directory: it must be reported, not walked.
func TestEnumerateResidualHandlesAFileRoot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := EnumerateResidual(p)
	if err != nil {
		t.Fatalf("EnumerateResidual: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != p || entries[0].IsDir {
		t.Fatalf("want exactly one file entry for %s, got %+v", p, entries)
	}
}

// TestFormatResidualDistinguishesFilesFromDirectories binds the rendered CI
// log, not just the struct: the reader of a red Windows leg sees this text and
// nothing else, so the file/dir distinction has to survive formatting.
func TestFormatResidualDistinguishesFilesFromDirectories(t *testing.T) {
	base := plantResidue(t)
	entries, err := EnumerateResidual(base)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatResidual(base, entries, nil)

	dirLine := "[dir ] " + filepath.Join(base, "001")
	fileLine := "[file] " + filepath.Join(base, "001", "settings.json")
	if !strings.Contains(out, dirLine) {
		t.Errorf("rendered report does not tag %s as a directory:\n%s", filepath.Join(base, "001"), out)
	}
	if !strings.Contains(out, fileLine) {
		t.Errorf("rendered report does not tag settings.json as a file:\n%s", out)
	}
	if !strings.Contains(out, "residual entries under "+base) {
		t.Errorf("rendered report does not name the scanned base:\n%s", out)
	}
}

// TestRemovalProbeReportsPerPathOutcome pins the probe that separates the two
// causes the raw #6512 log cannot distinguish: a transient delete-pending
// state (REMOVED-ON-RETRY) from a genuinely held handle (STILL-HELD). On POSIX
// every removal succeeds, so this test binds the reporting and the
// deepest-first ordering, which is what makes the parent removals possible.
func TestRemovalProbeReportsPerPathOutcome(t *testing.T) {
	base := plantResidue(t)
	entries, err := EnumerateResidual(base)
	if err != nil {
		t.Fatal(err)
	}
	out := RemovalProbe(entries)

	for _, p := range []string{
		filepath.Join(base, "001", "nested", "leaf.txt"),
		filepath.Join(base, "001", "nested"),
		filepath.Join(base, "001", "settings.json"),
		filepath.Join(base, "001"),
		base,
	} {
		if !strings.Contains(out, "REMOVED-ON-RETRY "+p) {
			t.Errorf("probe did not report %s as removed on retry; deepest-first ordering is what makes parent removal possible:\n%s", p, out)
		}
	}
	if _, err := os.Stat(base); err == nil {
		t.Errorf("probe reported removal but %s still exists", base)
	}
}

// TestTempDirWithCleanupDiagnosticIsADropInForTempDir binds the substitution
// itself: the exported helper must hand back a directory indistinguishable
// from t.TempDir()'s, because the #6512 call sites in memlimit_test.go write
// settings.json into it and read it back. It says nothing about whether the
// diagnostic was armed — that is
// TestDiagnosticNotArmedOffWindows in tempdirdiag_ordering_test.go, which
// counts the registered cleanups.
func TestTempDirWithCleanupDiagnosticIsADropInForTempDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this test binds the non-Windows path")
	}
	dir := TempDirWithCleanupDiagnostic(t)
	if dir == "" {
		t.Fatal("helper returned an empty directory")
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("helper must return a usable directory, exactly as t.TempDir() does: %v", err)
	}
	// A t.TempDir()-shaped path: the base plus a numbered subdirectory.
	if filepath.Base(dir) != "001" {
		t.Errorf("want a t.TempDir()-shaped path ending in 001, got %s", dir)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("the returned directory must be writable: %v", err)
	}
}

// TestDescribeHoldersIsInertOffWindows records what this machine can and
// cannot observe. Off Windows the stub is what compiles, and it must say so
// rather than pretending to have looked.
func TestDescribeHoldersIsInertOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Restart Manager implementation is not exercised off Windows")
	}
	out := describeHolders([]string{"/nonexistent/path"})
	if !strings.Contains(out, "not applicable on this platform") {
		t.Fatalf("the non-Windows stub must state that it did not look, got %q", out)
	}
}
