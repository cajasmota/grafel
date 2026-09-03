package extractors_test

// #6779 — the incremental reindex path is the dominant graph-stats.json writer
// in production: the daemon runs it on every watched edit. What it stamps for
// the kind vocabulary therefore decides whether the stale-vocabulary report
// survives normal use.
//
// It must stamp BOTH ways, and the second is the one that matters:
//
//   - a pass over an already-current graph keeps it current (otherwise every
//     keystroke would make doctor cry stale about a healthy repo);
//   - a pass over an OLDER graph keeps it OLDER, because incremental carries
//     unchanged files' entities forward verbatim — they are still spelled by
//     the build that first wrote them. Stamping this build's version there
//     would clear the warning without migrating one single kind.

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// runIncrementalWithPriorStamp6779 seeds a graph plus a sidecar carrying the
// given vocabulary stamp, edits one file, runs the real incremental pass, and
// returns the stamp on the sidecar it wrote.
func runIncrementalWithPriorStamp6779(t *testing.T, priorStamp int) int {
	t.Helper()
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc OldFunc() {}\n")
	writeFile(t, repo, "svc/other.go", "package svc\n\nfunc Untouched() {}\n")
	a := graph.EntityID("test-repo", "SCOPE.Operation", "Untouched", "svc/other.go")
	entities := []graph.Entity{
		{ID: a, Name: "Untouched", Kind: "SCOPE.Operation", SourceFile: "svc/other.go", Language: "go"},
	}
	buildMinimalGraph(t, stateDir, entities, nil)
	seedManifest(t, repo, stateDir)

	prior := &graph.GraphStatsSidecar{
		Version:               1,
		ComputedAt:            time.Now(),
		TotalEntities:         len(entities),
		KindVocabularyVersion: priorStamp,
	}
	if err := graph.WriteSidecar(graph.SidecarPath(stateDir), prior, false); err != nil {
		t.Fatal(err)
	}

	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc NewFunc() {}\n")

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("premise: TryIncremental fell back (%s) — no incremental sidecar was written", res.FallbackReason)
	}
	side, err := graph.LoadSidecar(stateDir)
	if err != nil {
		t.Fatalf("load sidecar after incremental: %v", err)
	}
	if side.ExtractMS <= 0 {
		t.Fatalf("premise: the sidecar was not refreshed by this pass (extract_ms=%d)", side.ExtractMS)
	}
	return side.KindVocabularyVersion
}

// TestIncremental_KeepsCurrentKindVocabulary — an incremental pass over a
// current graph must not demote it.
func TestIncremental_KeepsCurrentKindVocabulary(t *testing.T) {
	got := runIncrementalWithPriorStamp6779(t, types.KindVocabularyVersion)
	if got != types.KindVocabularyVersion {
		t.Fatalf("incremental stamped v%d over a CURRENT graph, want v%d — a watched edit would make doctor report a healthy repo as stale",
			got, types.KindVocabularyVersion)
	}
}

// TestIncremental_DoesNotLaunderOlderKindVocabulary — an incremental pass over
// an OLDER graph must not promote it. This is the laundering direction: the
// entities from unchanged files were never re-extracted, so their kinds are
// exactly as stale as they were before the pass ran.
func TestIncremental_DoesNotLaunderOlderKindVocabulary(t *testing.T) {
	older := types.KindVocabularyVersion - 1
	got := runIncrementalWithPriorStamp6779(t, older)
	if got != older {
		t.Fatalf("incremental stamped v%d over a graph written under v%d — a single watched edit laundered a stale vocabulary into a current one without respelling any carried-forward entity",
			got, older)
	}
}
