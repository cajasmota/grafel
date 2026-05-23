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
