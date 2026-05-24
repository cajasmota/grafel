package gitmeta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// TestProbeRepo_nonSparse verifies that a regular full-checkout repo
// is not flagged as sparse.
func TestProbeRepo_nonSparse(t *testing.T) {
	dir := initBareRepo(t, "main")
	si := gitmeta.ProbeRepo(dir)
	if si.IsSparse {
		t.Errorf("ProbeRepo: expected IsSparse=false for a full checkout")
	}
}

// TestProbeRepo_nonGit verifies that a plain directory returns zero SparseInfo.
func TestProbeRepo_nonGit(t *testing.T) {
	dir := t.TempDir()
	si := gitmeta.ProbeRepo(dir)
	if si.IsSparse {
		t.Error("ProbeRepo: expected IsSparse=false for a non-git directory")
	}
}

// TestProbeRepo_sparseEnabled creates a repo with core.sparseCheckout=true
// and a sparse-checkout pattern file, then verifies ProbeRepo detects it.
func TestProbeRepo_sparseEnabled(t *testing.T) {
	dir := initBareRepo(t, "main")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Enable sparse-checkout via git config.
	run("config", "core.sparseCheckout", "true")

	// Write a pattern file.
	gitDir := filepath.Join(dir, ".git")
	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	patternContent := "# comment\nservices/payments\nservices/orders\n"
	if err := os.WriteFile(filepath.Join(infoDir, "sparse-checkout"), []byte(patternContent), 0o644); err != nil {
		t.Fatal(err)
	}

	si := gitmeta.ProbeRepo(dir)
	if !si.IsSparse {
		t.Fatal("ProbeRepo: expected IsSparse=true when core.sparseCheckout=true")
	}
	if len(si.Patterns) != 2 {
		t.Errorf("ProbeRepo: expected 2 patterns, got %d: %v", len(si.Patterns), si.Patterns)
	}
	if si.Patterns[0] != "services/payments" || si.Patterns[1] != "services/orders" {
		t.Errorf("ProbeRepo: unexpected patterns: %v", si.Patterns)
	}
}

// TestIsPathIncluded_nonSparse verifies every path is included for a non-sparse repo.
func TestIsPathIncluded_nonSparse(t *testing.T) {
	si := gitmeta.SparseInfo{IsSparse: false}
	paths := []string{"cmd/main.go", "internal/foo/bar.go", "README.md"}
	for _, p := range paths {
		if !gitmeta.IsPathIncluded(si, p) {
			t.Errorf("IsPathIncluded: expected true for %q on non-sparse repo", p)
		}
	}
}

// TestIsPathIncluded_sparseConeModePatterns tests cone-mode prefix matching.
func TestIsPathIncluded_sparseConeModePatterns(t *testing.T) {
	si := gitmeta.SparseInfo{
		IsSparse: true,
		ConeMode: true,
		Patterns: []string{"services/payments", "libs/shared"},
	}

	included := []string{
		"services/payments/handler.go",
		"services/payments/sub/deep.go",
		"libs/shared/utils.go",
	}
	excluded := []string{
		"services/orders/handler.go",
		"cmd/main.go",
		"internal/other.go",
	}

	for _, p := range included {
		if !gitmeta.IsPathIncluded(si, p) {
			t.Errorf("IsPathIncluded: expected true for %q", p)
		}
	}
	for _, p := range excluded {
		if gitmeta.IsPathIncluded(si, p) {
			t.Errorf("IsPathIncluded: expected false for %q", p)
		}
	}
}

// TestIsPathIncluded_emptyPatterns verifies all paths excluded when sparse but no patterns.
func TestIsPathIncluded_emptyPatterns(t *testing.T) {
	si := gitmeta.SparseInfo{IsSparse: true, Patterns: nil}
	if gitmeta.IsPathIncluded(si, "cmd/main.go") {
		t.Error("IsPathIncluded: expected false when sparse with empty patterns")
	}
}

// TestCoverageStatus verifies the two states.
func TestCoverageStatus(t *testing.T) {
	full := gitmeta.SparseInfo{IsSparse: false}
	if got := full.CoverageStatus(); got != gitmeta.CoverageStatusFull {
		t.Errorf("CoverageStatus: got %q, want %q", got, gitmeta.CoverageStatusFull)
	}

	partial := gitmeta.SparseInfo{IsSparse: true}
	if got := partial.CoverageStatus(); got != gitmeta.CoverageStatusPartial {
		t.Errorf("CoverageStatus: got %q, want %q", got, gitmeta.CoverageStatusPartial)
	}
}
