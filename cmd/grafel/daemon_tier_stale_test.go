package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIsRepoDirtyAfter_FreshGraphDoesNotEnqueue locks in the P1 freshness gate
// (#5681): the reactive-reindex path (tierReloadCallback -> isRepoDirtyAfter)
// must NOT signal "dirty" — i.e. must NOT enqueue a reindex — when every source
// file is OLDER than the on-disk graph.fb. Only a genuinely newer source file
// makes the repo dirty. This prevents a boot/cold-wake reindex of a group whose
// graph is already fresh, one of the overlap sources that piled concurrent
// multi-GB index runs into the engine.
func TestIsRepoDirtyAfter_FreshGraphDoesNotEnqueue(t *testing.T) {
	repo := t.TempDir()

	srcDir := filepath.Join(repo, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(srcDir, "a.go")
	if err := os.WriteFile(srcFile, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The graph.fb stand-in — isRepoDirtyAfter only stats mtimes, not content.
	refFile := filepath.Join(repo, "graph.fb")
	if err := os.WriteFile(refFile, []byte("fb"), 0o644); err != nil {
		t.Fatal(err)
	}

	base := time.Now()
	older := base.Add(-1 * time.Hour)
	newer := base.Add(1 * time.Hour)

	// Deterministic mtimes: source older than graph.fb == FRESH graph.
	if err := os.Chtimes(srcFile, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(refFile, base, base); err != nil {
		t.Fatal(err)
	}

	if isRepoDirtyAfter(repo, refFile) {
		t.Fatal("fresh graph (all source older than graph.fb) must NOT be dirty — would enqueue a needless reindex on boot/cold-wake")
	}

	// Now make the source newer than graph.fb == STALE graph → must be dirty.
	if err := os.Chtimes(srcFile, newer, newer); err != nil {
		t.Fatal(err)
	}
	if !isRepoDirtyAfter(repo, refFile) {
		t.Fatal("stale graph (source newer than graph.fb) MUST be dirty so the reindex is enqueued")
	}
}

// TestIsRepoDirtyAfter_SkipDirsIgnored confirms a newer file under a skipped
// directory (e.g. node_modules) does NOT mark the repo dirty — matching the
// watcher's skip list — so vendored/build churn never triggers a reindex.
func TestIsRepoDirtyAfter_SkipDirsIgnored(t *testing.T) {
	repo := t.TempDir()

	refFile := filepath.Join(repo, "graph.fb")
	if err := os.WriteFile(refFile, []byte("fb"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	if err := os.Chtimes(refFile, base, base); err != nil {
		t.Fatal(err)
	}

	// A newer file, but inside node_modules (a skip dir).
	skipDir := filepath.Join(repo, "node_modules", "pkg")
	if err := os.MkdirAll(skipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	vendored := filepath.Join(skipDir, "index.js")
	if err := os.WriteFile(vendored, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(vendored, base.Add(1*time.Hour), base.Add(1*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if isRepoDirtyAfter(repo, refFile) {
		t.Fatal("a newer file under a skip dir (node_modules) must NOT mark the repo dirty")
	}
}
