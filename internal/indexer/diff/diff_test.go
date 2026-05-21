package diff_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/indexer/diff"
)

// writeFile creates a file in dir with the given content. Returns the
// repo-relative path (forward-slash).
func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// TestFilter_AllNewFiles checks that every file is marked changed when the
// manifest is empty (first run).
func TestFilter_AllNewFiles(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "a/foo.go", "package a")
	writeFile(t, repo, "a/bar.go", "package a")

	manifest := diff.LoadManifest(t.TempDir()) // empty state dir → empty manifest

	changed, unchanged := diff.Filter(repo, []string{"a/foo.go", "a/bar.go"}, manifest)
	if len(changed) != 2 {
		t.Errorf("want 2 changed, got %d", len(changed))
	}
	if len(unchanged) != 0 {
		t.Errorf("want 0 unchanged, got %d", len(unchanged))
	}
}

// TestFilter_NoChanges verifies that after updating the manifest with all
// files, a second Filter call returns no changed files.
func TestFilter_NoChanges(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	relPaths := []string{"main.go", "util.go"}
	writeFile(t, repo, "main.go", "package main")
	writeFile(t, repo, "util.go", "package main")

	// Populate manifest.
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, relPaths, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload and filter.
	m2 := diff.LoadManifest(stateDir)
	changed, unchanged := diff.Filter(repo, relPaths, m2)
	if len(changed) != 0 {
		t.Errorf("want 0 changed, got %d: %v", len(changed), changed)
	}
	if len(unchanged) != 2 {
		t.Errorf("want 2 unchanged, got %d", len(unchanged))
	}
}

// TestFilter_OneChanged verifies that modifying one file marks only that file
// (plus cross-file-invalidated peers sharing the same base) as changed.
func TestFilter_OneChanged(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	relPaths := []string{"svc/user.go", "svc/order.go", "cmd/main.go"}
	writeFile(t, repo, "svc/user.go", "package svc\ntype User struct{}")
	writeFile(t, repo, "svc/order.go", "package svc")
	writeFile(t, repo, "cmd/main.go", "package main")

	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, relPaths, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Modify user.go.
	time.Sleep(2 * time.Millisecond) // ensure mtime differs
	writeFile(t, repo, "svc/user.go", "package svc\ntype User struct{ Name string }")

	m2 := diff.LoadManifest(stateDir)
	changed, unchanged := diff.Filter(repo, relPaths, m2)

	// svc/user.go must be changed.
	changedSet := make(map[string]bool)
	for _, c := range changed {
		changedSet[c] = true
	}
	if !changedSet["svc/user.go"] {
		t.Error("expected svc/user.go to be changed")
	}
	// cmd/main.go must be unchanged (unrelated base name "main" vs "user").
	unchangedSet := make(map[string]bool)
	for _, u := range unchanged {
		unchangedSet[u] = true
	}
	if !unchangedSet["cmd/main.go"] {
		t.Error("expected cmd/main.go to be unchanged")
	}
}

// TestSaveAndLoadManifest ensures the manifest round-trips correctly.
func TestSaveAndLoadManifest(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	writeFile(t, repo, "hello.ts", "export const x = 1")

	m := diff.LoadManifest(stateDir)
	if len(m.Files) != 0 {
		t.Error("expected empty manifest from missing file")
	}

	diff.UpdateManifest(repo, []string{"hello.ts"}, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save: %v", err)
	}

	m2 := diff.LoadManifest(stateDir)
	if m2.Version != diff.Version {
		t.Errorf("version mismatch: got %d want %d", m2.Version, diff.Version)
	}
	entry, ok := m2.Files["hello.ts"]
	if !ok {
		t.Fatal("hello.ts missing from loaded manifest")
	}
	if entry.SHA256 == "" {
		t.Error("SHA256 not populated")
	}
	if entry.Size == 0 {
		t.Error("Size not populated")
	}
}

// TestStats_CacheHitRate checks the cache-hit percentage calculation.
func TestStats_CacheHitRate(t *testing.T) {
	s := diff.Stats{Total: 100, Changed: 5, Unchanged: 95}
	if got := s.CacheHitRate(); got != 95.0 {
		t.Errorf("want 95.0, got %f", got)
	}
	zero := diff.Stats{}
	if got := zero.CacheHitRate(); got != 0 {
		t.Errorf("want 0, got %f", got)
	}
}

// TestLoadManifest_VersionMismatch checks that an outdated manifest is
// discarded (returns empty).
func TestLoadManifest_VersionMismatch(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "file-index.json")
	// Write a manifest with version 999.
	if err := os.WriteFile(path, []byte(`{"version":999,"files":{"foo.go":{"sha256":"abc","size":1,"mtime":1}}}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := diff.LoadManifest(stateDir)
	if len(m.Files) != 0 {
		t.Errorf("expected empty manifest after version mismatch, got %d files", len(m.Files))
	}
}
