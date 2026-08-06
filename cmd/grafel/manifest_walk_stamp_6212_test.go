package main

// #6212 — the manifest must be stamped from the hashes computed during the
// WALK, not from a re-hash at commit time.
//
// commitManifest used to call diff.UpdateManifest, which opens and SHA-256s
// every walked file AT COMMIT TIME. The document, however, was built from the
// bytes Run's walk read — and #6207 widened the gap between those two moments
// to span rename-detect, algo carry-forward, sortDocumentForEmission and the
// whole of WriteGraphGen. Anything written into the working tree inside that
// window was stamped into the manifest without ever being indexed, and the next
// pass hash-matched it and classified it UNCHANGED (diff.go isChanged), so the
// edit stayed invisible until the file was touched again or HEAD advanced.
//
// The realistic trigger is the watcher-driven fallback reindex — it fires
// BECAUSE files are changing, so a developer saving a file mid-reindex is the
// ordinary case.
//
// Both tests here drive the real scheduler fallback and use the writeGraphGen
// seam — the same seam #6207's review used to MEASURE the skew — as the point
// "after the walk, before the manifest commit". Assertions are on observable
// state: the stamp bytes, and a hash-call counter. Never on timing.
//
// The fixture helpers (fallbackManifestRepo, useInProcessIndex,
// declineIncrementalPass, ...) live in scheduler_fallback_manifest_6207_test.go.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// TestFallbackIndex_WriteAfterTheWalkIsNotStampedAsIndexed is the defect itself.
//
// A working-tree write lands between the walk that fed the document and the
// manifest commit. The graph therefore contains the OLD bytes. If the manifest
// records the NEW ones it is claiming to have indexed content that is not in
// the graph, and — worse than merely being wrong — that claim is self-
// concealing: the next pass hash-matches the file, calls it unchanged, and the
// edit is never picked up.
//
// The manifest may lag (stamping the old bytes re-presents the file as changed
// next pass, which is self-correcting). It may not LEAD.
func TestFallbackIndex_WriteAfterTheWalkIsNotStampedAsIndexed(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "walkstamp-midwrite", "main")
	useInProcessIndex(t)
	stateDir := daemon.StateDirForRepo(repo)

	const victim = "svc/thing.go"
	victimAbs := filepath.Join(repo, filepath.FromSlash(victim))
	indexedHash := sha256OfFile(t, victimAbs)

	assertNoManifestYet(t, stateDir)
	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")
	assertNoManifestYet(t, stateDir)

	// The seam: the walk has happened, the document is built, the graph is
	// about to be written and commitManifest has not run yet. A developer
	// saving a file lands exactly here.
	var midWriteHash string
	prevWriter := writeGraphGen
	writeGraphGen = func(dir string, doc *graph.Document) (string, error) {
		writeFixtureFile(t, repo, victim, "package svc\n\nfunc Thing() int { return 12345 }\n\nfunc AddedMidIndex() {}\n")
		midWriteHash = sha256OfFile(t, victimAbs)
		return prevWriter(dir, doc)
	}
	t.Cleanup(func() { writeGraphGen = prevWriter })

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}

	if midWriteHash == "" {
		t.Fatal("fixture is inert: the writeGraphGen seam never fired, so no write ever landed " +
			"inside the walk→commit window")
	}
	if midWriteHash == indexedHash {
		t.Fatal("fixture is inert: the mid-index write produced the same bytes as the indexed ones")
	}

	m := diff.LoadManifest(stateDir)
	got := m.Files[victim].SHA256
	if got == midWriteHash {
		t.Fatalf("the manifest stamped %s with the POST-WALK content (%s…) — those bytes were never "+
			"extracted, so the graph does not contain them, yet the next pass will hash-match this "+
			"stamp and classify the file UNCHANGED. The edit is invisible until the file is touched "+
			"again or HEAD advances (#6212).", victim, got[:12])
	}
	if got != indexedHash {
		t.Fatalf("the manifest stamped %s with %q, want the hash of the bytes the walk actually read "+
			"(%q). The manifest's contract is \"these bytes are what the graph was built from\" (#6212).",
			victim, got, indexedHash)
	}

	// The other three fixture files were untouched, so their stamps must still
	// be exact — the walk-time stamp is the WHOLE manifest, not a special case
	// for the raced file.
	for _, rel := range fallbackFixtureFiles {
		if rel == victim {
			continue
		}
		want := sha256OfFile(t, filepath.Join(repo, filepath.FromSlash(rel)))
		if m.Files[rel].SHA256 != want {
			t.Fatalf("manifest stamp for untouched file %q is %q, want %q (#6212)",
				rel, m.Files[rel].SHA256, want)
		}
	}
}

// TestCommitManifest_PerformsNoHashSweep pins the removal.
//
// Once the stamps come from the walk, commitManifest has nothing left to hash:
// it merges the walk's stamps into the manifest and reconciles membership, both
// of which are map work. The O(repo) SHA-256 sweep — 218 ms of the 738 ms a
// zero-change pass burns on a 3003-file fixture (#6206) — is simply gone.
//
// The observable is diff.HashCallCount, a counter incremented inside
// diff.hashFile, sampled at the writeGraphGen seam and again after the index
// returns. Everything between those two samples is the graph write plus
// commitManifest. Deliberately NOT a timing assertion: a wall-clock budget here
// is the test-that-cannot-fail shape this repo keeps producing.
func TestCommitManifest_PerformsNoHashSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	repo := fallbackManifestRepo(t, "walkstamp-nosweep", "main")
	useInProcessIndex(t)
	stateDir := daemon.StateDirForRepo(repo)

	assertNoManifestYet(t, stateDir)
	declineIncrementalPass(t, repo, "absent-graph-nonempty-tree")
	assertNoManifestYet(t, stateDir)

	var atSeam int64
	var seamFired bool
	prevWriter := writeGraphGen
	writeGraphGen = func(dir string, doc *graph.Document) (string, error) {
		atSeam = diff.HashCallCount()
		seamFired = true
		return prevWriter(dir, doc)
	}
	t.Cleanup(func() { writeGraphGen = prevWriter })

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}
	if !seamFired {
		t.Fatal("fixture is inert: writeGraphGen never ran, so nothing was sampled")
	}

	if swept := diff.HashCallCount() - atSeam; swept != 0 {
		t.Fatalf("commitManifest hashed %d file(s) after the graph write. It must stamp from the "+
			"hashes the walk already computed — re-hashing here is both the O(repo) sweep #6206 "+
			"wants gone AND the source of the post-walk skew (#6212).", swept)
	}

	// The sweep is gone and the manifest is still exact — the point is that the
	// work moved, not that it was dropped.
	assertManifestExact(t, repo, stateDir)
}
