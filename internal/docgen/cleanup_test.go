// cleanup_test.go — integration tests for RunDocgenCleanup (issue #2216).
package docgen

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupFakeHome creates a temporary home dir with the canonical docgen layout.
// Returns: homeDir, archigraphRoot (= homeDir/.archigraph), and a cleanup func.
func setupFakeHome(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	return home
}

// writeFile creates a file with dummy content at path, ensuring parent dirs exist.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// setDirModTime changes a directory's modification time.
func setDirModTime(t *testing.T, dir string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(dir, mt, mt); err != nil {
		t.Fatalf("Chtimes %s: %v", dir, err)
	}
}

// TestCleanupRemovesStaleBackups verifies that .previous-* directories older
// than the max-age threshold are removed.
func TestCleanupRemovesStaleBackups(t *testing.T) {
	home := setupFakeHome(t)
	docsRoot := filepath.Join(home, "docs")

	// Create a stale backup: mygroup.previous-20260101T000000Z/
	staleBackup := filepath.Join(docsRoot, "mygroup.previous-20260101T000000Z")
	writeFile(t, filepath.Join(staleBackup, "index.md"))

	// Create a fresh backup: mygroup.previous-<recent>/
	freshBackup := filepath.Join(docsRoot, "mygroup.previous-20990101T000000Z")
	writeFile(t, filepath.Join(freshBackup, "index.md"))

	// Make the stale backup actually old on disk.
	stale := time.Now().Add(-8 * 24 * time.Hour)
	setDirModTime(t, staleBackup, stale)
	// Fresh backup: mod time is future / now — no need to set.

	opts := CleanupOptions{
		Group:   "mygroup",
		MaxAge:  7 * 24 * time.Hour,
		HomeDir: home,
		DryRun:  false,
	}
	result, err := RunDocgenCleanup(opts)
	if err != nil {
		t.Fatalf("RunDocgenCleanup: %v", err)
	}

	// Stale backup should be removed.
	if _, statErr := os.Stat(staleBackup); statErr == nil {
		t.Errorf("stale backup should have been removed: %s", staleBackup)
	}
	// Fresh backup must survive.
	if _, statErr := os.Stat(freshBackup); statErr != nil {
		t.Errorf("fresh backup should not have been removed: %s", freshBackup)
	}

	if len(result.RemovedPaths) != 1 {
		t.Errorf("expected 1 removed path, got %d: %v", len(result.RemovedPaths), result.RemovedPaths)
	}
}

// TestCleanupFreshBackupPreserved verifies that a backup that is only 6 days
// old (below the 7-day threshold) is not removed.
func TestCleanupFreshBackupPreserved(t *testing.T) {
	home := setupFakeHome(t)
	docsRoot := filepath.Join(home, "docs")

	freshBackup := filepath.Join(docsRoot, "mygroup.previous-20990101T000000Z")
	writeFile(t, filepath.Join(freshBackup, "index.md"))

	// 6-day-old backup is still fresh.
	setDirModTime(t, freshBackup, time.Now().Add(-6*24*time.Hour))

	opts := CleanupOptions{
		Group:   "mygroup",
		MaxAge:  7 * 24 * time.Hour,
		HomeDir: home,
	}
	result, err := RunDocgenCleanup(opts)
	if err != nil {
		t.Fatalf("RunDocgenCleanup: %v", err)
	}

	if _, statErr := os.Stat(freshBackup); statErr != nil {
		t.Errorf("6-day-old backup should have been preserved: %s", freshBackup)
	}
	if len(result.RemovedPaths) != 0 {
		t.Errorf("expected 0 removed paths, got %d: %v", len(result.RemovedPaths), result.RemovedPaths)
	}
}

// TestCleanupDryRun verifies that --dry-run reports paths without removing them.
func TestCleanupDryRun(t *testing.T) {
	home := setupFakeHome(t)
	docsRoot := filepath.Join(home, "docs")

	staleBackup := filepath.Join(docsRoot, "mygroup.previous-20260101T000000Z")
	writeFile(t, filepath.Join(staleBackup, "index.md"))
	setDirModTime(t, staleBackup, time.Now().Add(-8*24*time.Hour))

	opts := CleanupOptions{
		Group:   "mygroup",
		MaxAge:  7 * 24 * time.Hour,
		HomeDir: home,
		DryRun:  true,
	}
	result, err := RunDocgenCleanup(opts)
	if err != nil {
		t.Fatalf("RunDocgenCleanup dry-run: %v", err)
	}

	// Dry-run: path reported but file must still exist.
	if len(result.RemovedPaths) != 1 {
		t.Errorf("dry-run: expected 1 reported path, got %d", len(result.RemovedPaths))
	}
	if _, statErr := os.Stat(staleBackup); statErr != nil {
		t.Errorf("dry-run: backup should not have been removed: %s", staleBackup)
	}
}

// TestCleanupGroupScope verifies that --group scopes removal to matching group
// backups and leaves other groups untouched.
func TestCleanupGroupScope(t *testing.T) {
	home := setupFakeHome(t)
	docsRoot := filepath.Join(home, "docs")

	staleA := filepath.Join(docsRoot, "groupA.previous-20260101T000000Z")
	writeFile(t, filepath.Join(staleA, "index.md"))
	setDirModTime(t, staleA, time.Now().Add(-9*24*time.Hour))

	staleB := filepath.Join(docsRoot, "groupB.previous-20260101T000000Z")
	writeFile(t, filepath.Join(staleB, "index.md"))
	setDirModTime(t, staleB, time.Now().Add(-9*24*time.Hour))

	// Cleanup only groupA.
	opts := CleanupOptions{
		Group:   "groupA",
		MaxAge:  7 * 24 * time.Hour,
		HomeDir: home,
	}
	result, err := RunDocgenCleanup(opts)
	if err != nil {
		t.Fatalf("RunDocgenCleanup: %v", err)
	}

	// groupA stale backup removed.
	if _, statErr := os.Stat(staleA); statErr == nil {
		t.Errorf("groupA stale backup should have been removed")
	}
	// groupB must survive.
	if _, statErr := os.Stat(staleB); statErr != nil {
		t.Errorf("groupB stale backup should not have been removed (different group scope)")
	}

	if len(result.RemovedPaths) != 1 {
		t.Errorf("expected 1 removed path, got %d: %v", len(result.RemovedPaths), result.RemovedPaths)
	}
}

// TestCleanupStagingRuns verifies that stale staging run dirs are removed.
func TestCleanupStagingRuns(t *testing.T) {
	home := setupFakeHome(t)

	// Create a fake project root with a staging dir.
	projectRoot := t.TempDir()
	stagingBase := filepath.Join(projectRoot, ".archigraph", "staging")

	// Stale run: run_id date is 10+ days ago.
	staleRunID := "2020-01-01-aabbccdd"
	staleRun := filepath.Join(stagingBase, staleRunID)
	writeFile(t, filepath.Join(staleRun, "index.md"))
	// Also write the .group marker.
	if err := os.WriteFile(filepath.Join(staleRun, ".group"), []byte("mygroup"), 0o644); err != nil {
		t.Fatalf("write .group: %v", err)
	}

	// Fresh run: today's run_id.
	freshRunID := "2099-12-31-deadbeef"
	freshRun := filepath.Join(stagingBase, freshRunID)
	writeFile(t, filepath.Join(freshRun, "index.md"))
	if err := os.WriteFile(filepath.Join(freshRun, ".group"), []byte("mygroup"), 0o644); err != nil {
		t.Fatalf("write .group: %v", err)
	}

	opts := CleanupOptions{
		MaxAge:       7 * 24 * time.Hour,
		HomeDir:      home,
		ProjectRoots: []string{projectRoot},
	}
	result, err := RunDocgenCleanup(opts)
	if err != nil {
		t.Fatalf("RunDocgenCleanup: %v", err)
	}

	// Stale run removed.
	if _, statErr := os.Stat(staleRun); statErr == nil {
		t.Errorf("stale staging run should have been removed: %s", staleRun)
	}
	// Fresh run preserved.
	if _, statErr := os.Stat(freshRun); statErr != nil {
		t.Errorf("fresh staging run should not have been removed: %s", freshRun)
	}

	if len(result.RemovedPaths) != 1 {
		t.Errorf("expected 1 removed path, got %d: %v", len(result.RemovedPaths), result.RemovedPaths)
	}
}

// TestCleanupIdempotent verifies that running cleanup twice on an already-clean
// tree is a no-op.
func TestCleanupIdempotent(t *testing.T) {
	home := setupFakeHome(t)

	opts := CleanupOptions{
		MaxAge:  7 * 24 * time.Hour,
		HomeDir: home,
	}
	for i := 0; i < 3; i++ {
		result, err := RunDocgenCleanup(opts)
		if err != nil {
			t.Fatalf("RunDocgenCleanup pass %d: %v", i+1, err)
		}
		if len(result.RemovedPaths) != 0 {
			t.Errorf("pass %d: expected no removals on empty tree, got %d", i+1, len(result.RemovedPaths))
		}
	}
}

// TestParseRunIDDate exercises the run_id date parser.
func TestParseRunIDDate(t *testing.T) {
	cases := []struct {
		runID string
		valid bool
	}{
		{"2026-05-25-a3b4c5d6", true},
		{"2020-01-01-00000000", true},
		{"invalid", false},
		{"", false},
		{"2026-13-01-aaaaaaaa", false}, // month 13 is invalid
	}
	for _, tc := range cases {
		t := t
		t.Run(tc.runID, func(t *testing.T) {
			got := parseRunIDDate(tc.runID)
			if tc.valid && got.IsZero() {
				t.Errorf("expected non-zero time for %q", tc.runID)
			}
			if !tc.valid && !got.IsZero() {
				t.Errorf("expected zero time for %q, got %v", tc.runID, got)
			}
		})
	}
}
