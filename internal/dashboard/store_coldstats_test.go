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

// TestAggregateGroupStats_SegmentSetUnreadableHeader is the RED test for
// #5915 J2 slice-2: a SEGMENT-SET repo (graph.<gen>/ dir + manifest.json,
// no flat graph.fb) whose segment file cannot be opened as a FlatBuffers
// graph (graph.PersistedStatsFromDir returns ok=false, mirroring a corrupt
// single .fb file) must still fall back to the manifest.json mtime via
// graph.CurrentGraphMtime as last-indexed -- not the zero time from the old
// os.Stat(graph.CurrentGraphPath(stateDir)) gate, which only ever resolves a
// flat .fb path and is therefore always absent for a segment-set repo.
func TestAggregateGroupStats_SegmentSetUnreadableHeader(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)
	registryStatsMu.Lock()
	delete(registryStatsCache, "seggrp")
	registryStatsMu.Unlock()

	repoPath := filepath.Join(tmpDir, "segrepo")
	os.MkdirAll(repoPath, 0o755)
	stateDir := daemon.StateDirForRepo(repoPath)
	os.MkdirAll(stateDir, 0o755)

	mtime := time.Now().Add(-9 * time.Minute).UTC().Truncate(time.Second)
	genDir := filepath.Join(stateDir, graph.GenDirName(7))
	segName := graph.SegmentFileName(0)
	// The manifest names a segment file that is never written -- opening it
	// fails cleanly (no such file), so PersistedStatsFromDir returns ok=false
	// even though the manifest + current pointer are otherwise valid.
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: segName, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: "aa1", MaxKey: "aa1",
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifestPath := filepath.Join(genDir, graph.ManifestFileName)
	if err := os.Chtimes(manifestPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(7)); err != nil {
		t.Fatalf("write current: %v", err)
	}
	if _, ok := graph.PersistedStatsFromDir(stateDir); ok {
		t.Fatal("precondition: PersistedStatsFromDir must fail to open the corrupt segment")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "graph-stats.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no sidecar, stat err = %v", err)
	}

	repos := []registry.Repo{{Slug: "segrepo", Path: repoPath}}
	_, lastIndexed := aggregateGroupStats("seggrp", repos)

	if lastIndexed.IsZero() {
		t.Fatal("lastIndexed is zero, want the segment-set manifest.json mtime")
	}
	if !lastIndexed.Equal(mtime) {
		t.Errorf("lastIndexed = %v, want %v", lastIndexed, mtime)
	}
}
