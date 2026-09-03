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
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// priorSidecar6779 describes the graph-stats.json a repo already has when the
// incremental pass runs over it.
type priorSidecar6779 struct {
	// stamp is the vocabulary version in the prior sidecar.
	stamp int
	// absent seeds NO prior sidecar at all — the shape a worktree seeded by
	// internal/daemon lands in, since seedArtifactRels copies the graph
	// artifact and the diff manifest but not graph-stats.json.
	absent bool
	// corrupt seeds a prior sidecar that cannot be decoded.
	corrupt bool
}

// runIncrementalPass6779 seeds a graph plus the described prior sidecar, edits
// one file, runs the real incremental pass, and returns the stamp on the
// sidecar it wrote.
func runIncrementalPass6779(t *testing.T, prior priorSidecar6779) (stamp int, stateDir string) {
	t.Helper()
	repo := t.TempDir()
	stateDir = t.TempDir()

	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc OldFunc() {}\n")
	writeFile(t, repo, "svc/other.go", "package svc\n\nfunc Untouched() {}\n")
	a := graph.EntityID("test-repo", "SCOPE.Operation", "Untouched", "svc/other.go")
	entities := []graph.Entity{
		{ID: a, Name: "Untouched", Kind: "SCOPE.Operation", SourceFile: "svc/other.go", Language: "go"},
	}
	buildMinimalGraph(t, stateDir, entities, nil)
	seedManifest(t, repo, stateDir)

	switch {
	case prior.absent:
		// nothing to write
	case prior.corrupt:
		if err := os.WriteFile(graph.SidecarPath(stateDir), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
	default:
		side := &graph.GraphStatsSidecar{
			Version:               1,
			ComputedAt:            time.Now(),
			TotalEntities:         len(entities),
			KindVocabularyVersion: prior.stamp,
		}
		if err := graph.WriteSidecar(graph.SidecarPath(stateDir), side, false); err != nil {
			t.Fatal(err)
		}
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
	return side.KindVocabularyVersion, stateDir
}

// TestIncremental_KeepsCurrentKindVocabulary — an incremental pass over a
// current graph must not demote it.
func TestIncremental_KeepsCurrentKindVocabulary(t *testing.T) {
	got, _ := runIncrementalPass6779(t, priorSidecar6779{stamp: types.KindVocabularyVersion})
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
	got, _ := runIncrementalPass6779(t, priorSidecar6779{stamp: older})
	if got != older {
		t.Fatalf("incremental stamped v%d over a graph written under v%d — a single watched edit laundered a stale vocabulary into a current one without respelling any carried-forward entity",
			got, older)
	}
}

// TestIncremental_DoesNotLaunderWhenPriorSidecarIsMissing is the case the
// first cut of this mechanism got wrong, and it is LIVE: the carry-forward sat
// inside the `if priorSide, err := LoadSidecar(...)` branch, so a repo with a
// graph and no sidecar skipped it entirely and got stamped with this build's
// version.
//
// internal/daemon/worktree_seed.go's seedArtifactRels copies the graph
// artifact and diff.ManifestFileName and not graph-stats.json, so every #5964
// worktree seed is exactly this shape: it first reads as a false `older`
// (over-firing on a repo seeded from a perfectly current parent), then one
// watched edit flipped it to a false `current`. Both directions wrong, one
// root cause — no prior sidecar meant "current" to the writer and "older" to
// the reader.
//
// An incremental pass re-extracts only the CHANGED files, so it can never
// legitimately claim the current vocabulary out of nothing.
func TestIncremental_DoesNotLaunderWhenPriorSidecarIsMissing(t *testing.T) {
	got, stateDir := runIncrementalPass6779(t, priorSidecar6779{absent: true})
	if got != 0 {
		t.Fatalf("incremental stamped v%d over a graph with NO prior sidecar, want 0 — a pass that re-extracted only the changed files cannot claim this build's vocabulary",
			got)
	}

	// The same measurement from the READER's end, on the artefacts this pass
	// actually wrote. Before the fix this read "older" before the pass and
	// "current" after it: one watched edit, and the whole repo stopped being
	// reported. It must still read older.
	state, stored := graph.KindVocabularyStateForDir(stateDir)
	if state != graph.KindVocabularyOlder {
		t.Fatalf("after an incremental pass over a graph with no prior sidecar, doctor would read %q (stored v%d) — the report was silenced without a single kind being respelled",
			state, stored)
	}
}

// TestIncremental_DoesNotLaunderWhenPriorSidecarIsUnreadable — same rule for a
// sidecar that exists but cannot be decoded. An unreadable stamp is not
// evidence of currency; the fail-safe direction costs a reindex the user
// chooses to run, the other direction is the silent-empty-result defect.
func TestIncremental_DoesNotLaunderWhenPriorSidecarIsUnreadable(t *testing.T) {
	got, _ := runIncrementalPass6779(t, priorSidecar6779{corrupt: true})
	if got != 0 {
		t.Fatalf("incremental stamped v%d over a graph whose prior sidecar is corrupt, want 0", got)
	}
}

// TestIncremental_ClampsAFutureStampToThisBuild — a prior sidecar from a NEWER
// build must not be copied forward verbatim, because this pass just respelled
// the changed files in THIS build's vocabulary. min(prior, current) is the
// honest answer in that direction too.
//
// This is also the only case that exercises the carry-forward with a non-equal
// non-zero prior stamp while KindVocabularyVersion is 1.
func TestIncremental_ClampsAFutureStampToThisBuild(t *testing.T) {
	got, _ := runIncrementalPass6779(t, priorSidecar6779{stamp: types.KindVocabularyVersion + 98})
	if got != types.KindVocabularyVersion {
		t.Fatalf("incremental stamped v%d from a future prior stamp, want v%d", got, types.KindVocabularyVersion)
	}
}
