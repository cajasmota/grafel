package extractors_test

// #6212 — a file that GENUINELY CHANGED but that the extractor pipeline declines
// must still be re-stamped, or it becomes an immortal entry (#5668).
//
// This pins WHERE the stamp is taken in TryIncremental's re-extraction loop, a
// property the rest of the #6212 suite cannot see. The stamp sits immediately
// after the successful read and BEFORE the classifier/extractor `continue`s
// below it, because "the bytes this pass saw" is the correct claim whatever the
// pipeline went on to do with them. Move it below those continues — which reads
// as tidier, since only extracted files would then be stamped — and the loop
// this test describes opens up.
//
// The mechanism, end to end: the file is in the walk, git reports it changed, it
// enters reallyChanged, the classifier returns no language, the loop continues,
// nothing stamps it, and Step 9 (which no longer sweeps the repo — that is the
// whole point of #6212) leaves the pre-edit hash in place. Next pass it reads as
// changed again. And the next. It counts against GRAFEL_INCREMENTAL_MAX_FILES on
// every tick, so a handful of them permanently trips the too-many-changed
// fallback and pins the daemon in a full-reindex loop.
//
// This is not exotic. A developer edits LICENSE; a build regenerates a .golden
// fixture; a data file is refreshed. Originally built as a review probe, which
// measured 4 of 5 such files going stale with the stamp moved below the continue.

import (
	"context"
	"io"
	"log"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// TestIncremental_ClassifierSkippedChangedFileIsRestamped drives real edits to
// files the classifier has no language for through a successful TryIncremental.
//
// The candidates deliberately span the ways a walked file ends up unextractable:
// a .golden fixture, a bare LICENSE, a .txt, a .dat. notes.md is included as the
// control — markdown classifies to a real language, so it takes the extracted
// path and must be re-stamped for the ordinary reason. If every candidate were
// unextractable, a mutant that stamped nothing at all would be indistinguishable
// from one that stamped only the extracted files.
func TestIncremental_ClassifierSkippedChangedFileIsRestamped(t *testing.T) {
	isolateHome(t)
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "victim.go", "package p\n\nfunc Victim() int { return 1 }\n")
	candidates := []string{"notes.md", "data.txt", "blob.dat", "fixture.golden", "LICENSE"}
	for _, c := range candidates {
		writeFile(t, repo, c, "original content for "+c+"\n")
	}

	initGitRepo(t, repo)
	gitCommitAll(t, repo, "base")

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "Victim", "victim.go"),
			Name: "Victim", Kind: "SCOPE.Operation", SourceFile: "victim.go", Language: "go"},
	}, nil)
	seedManifest(t, repo, stateDir)

	before := map[string]string{}
	mb := diff.LoadManifest(stateDir)
	for _, c := range candidates {
		before[c] = mb.Files[c].SHA256
	}

	// Every candidate genuinely changes, so every one of them is something the
	// next pass must NOT be told it has already indexed.
	for _, c := range candidates {
		writeFile(t, repo, c, "EDITED BY A DEVELOPER: "+c+"\n")
	}

	logger := log.New(io.Discard, "", 0)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if !res.Done {
		t.Fatalf("fixture is inert: the pass did not succeed (fallback=%q), so Step 9 never ran",
			res.FallbackReason)
	}

	after := diff.LoadManifest(stateDir)
	checked, stale := 0, 0
	for _, c := range candidates {
		if before[c] == "" {
			// Not in the walk at all (a skip layer excluded it) — nothing to say.
			t.Logf("not in the walk, skipped: %s", c)
			continue
		}
		checked++
		got := after.Files[c].SHA256
		want := shaOf("EDITED BY A DEVELOPER: " + c + "\n")
		if got == want {
			continue
		}
		stale++
		t.Errorf("%s kept a stamp that is not its post-edit content (got %q, want %q). It is in the "+
			"walk and it genuinely changed, so it re-presents as changed on EVERY subsequent pass, "+
			"counts against GRAFEL_INCREMENTAL_MAX_FILES every tick and pins the daemon in a "+
			"full-reindex loop — #5668's immortal entry, on the daemon's primary path. The stamp "+
			"must be taken on the successful READ, above the classifier/extractor continues, not "+
			"only for the files that reach extraction (#6212).", c, got, want)
	}
	if checked == 0 {
		t.Fatal("fixture is inert: none of the candidate files were in the walk, so nothing was asserted")
	}
	if stale == 0 {
		t.Logf("all %d walked candidates re-stamped", checked)
	}
}
