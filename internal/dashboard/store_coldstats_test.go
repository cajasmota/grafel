package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestAggregateGroupStats_ColdIndexNoSidecar reproduces #5442 for the dashboard
// group overview: a repo indexed by the daemon's incremental path has graph.fb
// on disk but no graph-stats.json sidecar. aggregateGroupStats must report the
// persisted entity count and a real last-indexed time (read cheaply from the
// graph.fb header), not 0 entities / never indexed.
func TestAggregateGroupStats_ColdIndexNoSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)
	// Bust the 30s registry-stats cache across runs of this group name.
	registryStatsMu.Lock()
	delete(registryStatsCache, "coldgrp")
	registryStatsMu.Unlock()

	repoPath := filepath.Join(tmpDir, "coldrepo")
	os.MkdirAll(repoPath, 0o755)
	stateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(stateDir, 0o755)

	indexedAt := time.Now().Add(-9 * time.Minute).UTC().Truncate(time.Second)
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: indexedAt,
		Repo:        repoPath,
		Stats:       graph.Stats{Entities: 5, Relationships: 2, Files: 3},
		Entities: []graph.Entity{
			{ID: "a1", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "b2", Name: "B", Kind: "function", SourceFile: "b.go", Language: "go"},
			{ID: "c3", Name: "C", Kind: "function", SourceFile: "c.go", Language: "go"},
			{ID: "d4", Name: "D", Kind: "function", SourceFile: "d.go", Language: "go"},
			{ID: "e5", Name: "E", Kind: "function", SourceFile: "e.go", Language: "go"},
		},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "graph-stats.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no sidecar, stat err = %v", err)
	}

	repos := []registry.Repo{{Slug: "coldrepo", Path: repoPath}}
	entityCount, lastIndexed := aggregateGroupStats("coldgrp", repos)

	if entityCount != 5 {
		t.Errorf("entityCount = %d, want 5 (persisted count from graph.fb header)", entityCount)
	}
	if lastIndexed.IsZero() {
		t.Fatal("lastIndexed is zero, want the graph.fb header ComputedAt")
	}
	if !lastIndexed.Equal(indexedAt) {
		t.Errorf("lastIndexed = %v, want %v", lastIndexed, indexedAt)
	}
}

// TestAggregateGroupStats_TrulyNeverIndexed asserts the negative case: no
// graph.fb at all → 0 entities and a zero last-indexed time.
func TestAggregateGroupStats_TrulyNeverIndexed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)
	registryStatsMu.Lock()
	delete(registryStatsCache, "freshgrp")
	registryStatsMu.Unlock()

	repoPath := filepath.Join(tmpDir, "fresh")
	os.MkdirAll(repoPath, 0o755)

	repos := []registry.Repo{{Slug: "fresh", Path: repoPath}}
	entityCount, lastIndexed := aggregateGroupStats("freshgrp", repos)
	if entityCount != 0 {
		t.Errorf("entityCount = %d, want 0", entityCount)
	}
	if !lastIndexed.IsZero() {
		t.Errorf("lastIndexed = %v, want zero", lastIndexed)
	}
}
