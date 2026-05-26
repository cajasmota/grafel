// migrate_test.go — integration tests for RunMigrateInRepo (issue #2216).
package docgen

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateInRepoDocsDir verifies that an in-repo docs/ dir with marker files
// is moved to the canonical store.
func TestMigrateInRepoDocsDir(t *testing.T) {
	home := t.TempDir()
	projectRoot := t.TempDir()

	// Create in-repo docs/ with a marker file.
	docsDir := filepath.Join(projectRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(docsDir, ".plan.md"))
	writeFile(t, filepath.Join(docsDir, "index.md"))

	opts := MigrateOptions{
		Group:   "testgroup",
		HomeDir: home,
		Yes:     true,
		GroupConfigLoader: func(group string) ([]MigrateRepo, error) {
			return []MigrateRepo{{Slug: "myrepo", Path: projectRoot}}, nil
		},
		GroupsLoader: func() ([]string, error) { return []string{"testgroup"}, nil },
	}

	result, err := RunMigrateInRepo(opts)
	if err != nil {
		t.Fatalf("RunMigrateInRepo: %v", err)
	}

	// The in-repo docs/ should have been moved.
	if _, statErr := os.Stat(docsDir); statErr == nil {
		t.Errorf("in-repo docs/ should have been removed after migration")
	}

	// Canonical destination should exist.
	dst := filepath.Join(home, "docs", "testgroup", "myrepo")
	if _, statErr := os.Stat(dst); statErr != nil {
		t.Errorf("canonical dst should exist: %s: %v", dst, statErr)
	}

	if len(result.Migrated) != 1 {
		t.Errorf("expected 1 migration, got %d: %v", len(result.Migrated), result.Migrated)
	}
}

// TestMigrateInRepoStagingRun verifies that an orphaned staging run is moved
// to .staging-recovered/ in the canonical store.
func TestMigrateInRepoStagingRun(t *testing.T) {
	home := t.TempDir()
	projectRoot := t.TempDir()

	// Create an orphaned staging run.
	stagingRun := filepath.Join(projectRoot, ".archigraph", "staging", "2026-05-25-abcd1234")
	writeFile(t, filepath.Join(stagingRun, "index.md"))

	opts := MigrateOptions{
		Group:   "testgroup",
		HomeDir: home,
		Yes:     true,
		GroupConfigLoader: func(group string) ([]MigrateRepo, error) {
			return []MigrateRepo{{Slug: "myrepo", Path: projectRoot}}, nil
		},
		GroupsLoader: func() ([]string, error) { return []string{"testgroup"}, nil },
	}

	result, err := RunMigrateInRepo(opts)
	if err != nil {
		t.Fatalf("RunMigrateInRepo: %v", err)
	}

	// Original staging run should be gone.
	if _, statErr := os.Stat(stagingRun); statErr == nil {
		t.Errorf("orphaned staging run should have been moved")
	}

	// Recovered destination should exist.
	dst := filepath.Join(home, "docs", "testgroup", ".staging-recovered", "2026-05-25-abcd1234")
	if _, statErr := os.Stat(dst); statErr != nil {
		t.Errorf("staging-recovered dst should exist: %s: %v", dst, statErr)
	}

	if len(result.Migrated) != 1 {
		t.Errorf("expected 1 migration, got %d: %v", len(result.Migrated), result.Migrated)
	}
}

// TestMigrateInRepoIdempotent verifies that migrating an already-empty repo
// is a no-op.
func TestMigrateInRepoIdempotent(t *testing.T) {
	home := t.TempDir()
	projectRoot := t.TempDir()

	opts := MigrateOptions{
		Group:   "testgroup",
		HomeDir: home,
		Yes:     true,
		GroupConfigLoader: func(group string) ([]MigrateRepo, error) {
			return []MigrateRepo{{Slug: "myrepo", Path: projectRoot}}, nil
		},
		GroupsLoader: func() ([]string, error) { return []string{"testgroup"}, nil },
	}

	for i := 0; i < 2; i++ {
		result, err := RunMigrateInRepo(opts)
		if err != nil {
			t.Fatalf("RunMigrateInRepo pass %d: %v", i+1, err)
		}
		if len(result.Migrated) != 0 {
			t.Errorf("pass %d: expected no migrations, got %d", i+1, len(result.Migrated))
		}
	}
}

// TestMigrateInRepoUserDecline verifies that when the user declines, nothing is moved.
func TestMigrateInRepoUserDecline(t *testing.T) {
	home := t.TempDir()
	projectRoot := t.TempDir()

	docsDir := filepath.Join(projectRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(docsDir, ".plan.md"))

	opts := MigrateOptions{
		Group:   "testgroup",
		HomeDir: home,
		Yes:     false,
		ConfirmFn: func(_ string) bool {
			return false // always decline
		},
		GroupConfigLoader: func(group string) ([]MigrateRepo, error) {
			return []MigrateRepo{{Slug: "myrepo", Path: projectRoot}}, nil
		},
		GroupsLoader: func() ([]string, error) { return []string{"testgroup"}, nil },
	}

	result, err := RunMigrateInRepo(opts)
	if err != nil {
		t.Fatalf("RunMigrateInRepo: %v", err)
	}

	// Nothing moved.
	if _, statErr := os.Stat(docsDir); statErr != nil {
		t.Errorf("in-repo docs/ should still exist after user decline")
	}
	if len(result.Migrated) != 0 {
		t.Errorf("expected 0 migrations, got %d", len(result.Migrated))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

// TestMigrateInRepoBacksUpExistingCanonical verifies that if the canonical
// destination already exists, it is backed up before overwriting.
func TestMigrateInRepoBacksUpExistingCanonical(t *testing.T) {
	home := t.TempDir()
	projectRoot := t.TempDir()

	// Pre-create the canonical destination.
	canonical := filepath.Join(home, "docs", "testgroup", "myrepo")
	writeFile(t, filepath.Join(canonical, "old.md"))

	// Create an in-repo docs/ to migrate.
	docsDir := filepath.Join(projectRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(docsDir, ".plan.md"))
	writeFile(t, filepath.Join(docsDir, "new.md"))

	opts := MigrateOptions{
		Group:   "testgroup",
		HomeDir: home,
		Yes:     true,
		GroupConfigLoader: func(group string) ([]MigrateRepo, error) {
			return []MigrateRepo{{Slug: "myrepo", Path: projectRoot}}, nil
		},
		GroupsLoader: func() ([]string, error) { return []string{"testgroup"}, nil },
	}

	result, err := RunMigrateInRepo(opts)
	if err != nil {
		t.Fatalf("RunMigrateInRepo: %v", err)
	}

	// New canonical must contain new.md.
	if _, statErr := os.Stat(filepath.Join(canonical, "new.md")); statErr != nil {
		t.Errorf("new canonical should contain new.md")
	}

	// Backup must exist.
	if len(result.Migrated) != 1 || result.Migrated[0].Backup == "" {
		t.Errorf("expected backup to be set; got result.Migrated=%v", result.Migrated)
	}
	if _, statErr := os.Stat(result.Migrated[0].Backup); statErr != nil {
		t.Errorf("backup path should exist: %s", result.Migrated[0].Backup)
	}
}

// TestLooksLikeDocgenOutput tests the heuristic detection function.
func TestLooksLikeDocgenOutput(t *testing.T) {
	tmp := t.TempDir()

	// Empty dir: not docgen output.
	if looksLikeDocgenOutput(tmp) {
		t.Error("empty dir should not look like docgen output")
	}

	// Dir with .plan.md: docgen output.
	writeFile(t, filepath.Join(tmp, ".plan.md"))
	if !looksLikeDocgenOutput(tmp) {
		t.Error("dir with .plan.md should look like docgen output")
	}

	// Different dir with .tier0-xxx subdir.
	tmp2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp2, ".tier0-2026-05-25"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikeDocgenOutput(tmp2) {
		t.Error("dir with .tier0-* subdir should look like docgen output")
	}
}
