package main

// #6779 NON-VACUITY — the stale-vocabulary check must work against a graph
// produced by a REAL index, not only against a hand-built struct.
//
// This is the specific way #6757 arm C's first fix was silently vacuous: its
// test constructed a zero-valued sidecar instead of going through the write
// path, so it proved the reader could read a field nothing was proven to
// write. Here the whole chain runs for real — runIndexInternal walks a repo,
// extracts it, writes graph.fb and graph-stats.json through the shipping
// writers — and the assertions are made against those bytes on disk.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// realIndex6779 runs a real index over a one-file Go repo and returns the state
// dir the shipping daemon layout put the graph in.
func realIndex6779(t *testing.T) string {
	t.Helper()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	src := "package main\n\ntype Widget struct{ Name string }\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runIndexInternal([]string{
		"--repo=" + repo,
		"--skip-pass=graph-algo,commit-couple,embed",
	}); code != 0 {
		t.Fatalf("runIndexInternal exited %d", code)
	}

	stateDir := daemon.StateDirForRepo(repo)
	// PREMISE GUARD: a real graph really landed here. Without this every
	// assertion below could be satisfied by an empty directory.
	if _, err := os.Stat(graph.SidecarPath(stateDir)); err != nil {
		t.Fatalf("premise: no graph-stats.json in %s: %v", stateDir, err)
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("premise: real stored graph does not load: %v", err)
	}
	if len(doc.Entities) == 0 {
		t.Fatalf("premise: real index produced no entities")
	}
	return stateDir
}

// TestRealIndexStampsKindVocabulary pins the WRITE half: a real index stamps
// this build's vocabulary version into the sidecar it writes, and the check
// reads that real graph back as current.
func TestRealIndexStampsKindVocabulary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	stateDir := realIndex6779(t)

	side, err := graph.LoadSidecar(stateDir)
	if err != nil {
		t.Fatalf("load sidecar: %v", err)
	}
	if side.KindVocabularyVersion != types.KindVocabularyVersion {
		t.Fatalf("sidecar stamp = %d, want %d — the index write path is not stamping the vocabulary",
			side.KindVocabularyVersion, types.KindVocabularyVersion)
	}

	state, stored := graph.KindVocabularyStateForDir(stateDir)
	if state != graph.KindVocabularyCurrent {
		t.Fatalf("freshly indexed graph reads as %q (stored v%d), want %q",
			state, stored, graph.KindVocabularyCurrent)
	}
}

// TestRealIndexGraphDetectedAsOlderVocabulary pins the READ half against the
// same real artefacts: take the graph a real index just wrote, put the sidecar
// back into the shape a PRE-#6779 build left on disk (no vocabulary stamp at
// all — everything else untouched), and the check must call it older.
//
// The graph.fb, the `current` pointer and every entity in them are the real
// index's output and are not modified; only the stamp the older binary never
// wrote is removed. That is exactly the on-disk state of a group indexed
// before this mechanism shipped.
func TestRealIndexGraphDetectedAsOlderVocabulary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	stateDir := realIndex6779(t)

	side, err := graph.LoadSidecar(stateDir)
	if err != nil {
		t.Fatalf("load sidecar: %v", err)
	}
	side.KindVocabularyVersion = 0
	if err := graph.WriteSidecar(graph.SidecarPath(stateDir), side, false); err != nil {
		t.Fatalf("rewrite sidecar: %v", err)
	}

	state, stored := graph.KindVocabularyStateForDir(stateDir)
	if state != graph.KindVocabularyOlder {
		t.Fatalf("real graph with a pre-#6779 sidecar reads as %q (stored v%d), want %q",
			state, stored, graph.KindVocabularyOlder)
	}

	// And the graph is still perfectly loadable — which is the entire point:
	// nothing about this failure is visible from the graph itself.
	if doc, lerr := graph.LoadGraphFromDir(stateDir); lerr != nil || len(doc.Entities) == 0 {
		t.Fatalf("premise: the stale-vocabulary graph should still load fine (err=%v)", lerr)
	}
}
