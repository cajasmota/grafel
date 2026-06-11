package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoModModule_Standard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte("module github.com/example/myrepo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	got := parseGoModModule(path)
	if got != "github.com/example/myrepo" {
		t.Errorf("parseGoModModule = %q, want %q", got, "github.com/example/myrepo")
	}
}

func TestParseGoModModule_WithComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	content := "// comment line\nmodule github.com/owner/repo // inline comment\n\ngo 1.21\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	got := parseGoModModule(path)
	if got != "github.com/owner/repo" {
		t.Errorf("parseGoModModule with inline comment = %q, want %q", got, "github.com/owner/repo")
	}
}

func TestParseGoModModule_Absent(t *testing.T) {
	got := parseGoModModule("/nonexistent/go.mod")
	if got != "" {
		t.Errorf("parseGoModModule absent = %q, want empty", got)
	}
}

func TestParseGoModModule_NoModuleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte("go 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	got := parseGoModModule(path)
	if got != "" {
		t.Errorf("parseGoModModule no module line = %q, want empty", got)
	}
}

func TestGoModuleRoot_CachesResult(t *testing.T) {
	// Clear cache to avoid cross-test contamination.
	goModCacheMu.Lock()
	delete(goModCache, "")
	goModCacheMu.Unlock()

	// Empty repo root returns "" without panic.
	got := goModuleRoot("")
	if got != "" {
		t.Errorf("goModuleRoot empty repoRoot = %q, want empty", got)
	}
}

func TestGoModuleRoot_RealModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test/repo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	// Clear any cached entry for this dir (isolation).
	goModCacheMu.Lock()
	delete(goModCache, dir)
	goModCacheMu.Unlock()

	got := goModuleRoot(dir)
	if got != "example.com/test/repo" {
		t.Errorf("goModuleRoot = %q, want %q", got, "example.com/test/repo")
	}
	// Second call hits cache.
	got2 := goModuleRoot(dir)
	if got2 != got {
		t.Errorf("goModuleRoot cached = %q, want %q", got2, got)
	}
}

// --- #4705c: local-path replace directives ---

func TestParseGoModReplaces_SingleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	content := "module example.com/app\n\ngo 1.22\n\nreplace example.com/x => ./internal/x\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	reps := parseGoModReplaces(path)
	if len(reps) != 1 {
		t.Fatalf("want 1 replace, got %d (%+v)", len(reps), reps)
	}
	if reps[0].OldPath != "example.com/x" || reps[0].LocalDir != "internal/x" {
		t.Errorf("replace = %+v, want {example.com/x internal/x}", reps[0])
	}
}

func TestParseGoModReplaces_Block(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	content := "module example.com/app\n\nreplace (\n\texample.com/x => ./internal/x\n\texample.com/y v1.0.0 => ../y // sibling outside repo\n\texample.com/z v1.2.3 => example.com/zfork v1.4.0\n)\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	reps := parseGoModReplaces(path)
	// Only the local in-repo replacement survives: ../y escapes, zfork is a
	// network/version replacement.
	if len(reps) != 1 {
		t.Fatalf("want 1 in-repo replace, got %d (%+v)", len(reps), reps)
	}
	if reps[0].OldPath != "example.com/x" || reps[0].LocalDir != "internal/x" {
		t.Errorf("replace = %+v, want {example.com/x internal/x}", reps[0])
	}
}

func TestGoReplacePkgDir(t *testing.T) {
	reps := []goReplace{
		{OldPath: "example.com/x", LocalDir: "internal/x"},
		{OldPath: "example.com/rootmod", LocalDir: ""}, // replace => .
	}
	cases := []struct {
		in       string
		wantDir  string
		wantOK   bool
	}{
		{"example.com/x", "internal/x", true},
		{"example.com/x/sub/pkg", "internal/x/sub/pkg", true},
		{"example.com/rootmod/pkg", "pkg", true},
		{"example.com/rootmod", "", false}, // repo root itself, no dotted form
		{"example.com/other", "", false},
		{"github.com/stretchr/testify", "", false},
	}
	for _, c := range cases {
		dir, ok := goReplacePkgDir(c.in, reps)
		if dir != c.wantDir || ok != c.wantOK {
			t.Errorf("goReplacePkgDir(%q) = (%q,%v), want (%q,%v)", c.in, dir, ok, c.wantDir, c.wantOK)
		}
	}
}

func TestCleanGoLocalReplaceDir(t *testing.T) {
	cases := []struct {
		in      string
		wantDir string
		wantOK  bool
	}{
		{"./internal/x", "internal/x", true},
		{"./internal/x/", "internal/x", true},
		{".", "", true},
		{"../sibling", "", false},
		{"..", "", false},
		{"/abs/path", "", false},
		{"example.com/fork", "", false},
	}
	for _, c := range cases {
		dir, ok := cleanGoLocalReplaceDir(c.in)
		if dir != c.wantDir || ok != c.wantOK {
			t.Errorf("cleanGoLocalReplaceDir(%q) = (%q,%v), want (%q,%v)", c.in, dir, ok, c.wantDir, c.wantOK)
		}
	}
}
