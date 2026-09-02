package extractors_test

// #6212 on the path that actually runs.
//
// TryIncremental's only non-test caller is the daemon scheduler
// (cmd/grafel/daemon.go), so this is what executes on every tick where the delta
// is small — the common case, and precisely the "developer saves a file mid-
// reindex" scenario the issue is about. Step 9 used to call
// diff.UpdateManifest(absRepo, allFiles, manifest), re-hashing the whole repo
// off disk long after the bytes it extracted were read: the window spans the
// re-extraction, the scoped resolve, the merge, the flow recompute, the
// canonical sort and all of WriteGraphGen.
//
// Both tests below drive the real TryIncremental to a SUCCESSFUL pass (Done ==
// true), which is the branch that was unfixed. Assertions are on the manifest's
// bytes and on surviving poison stamps — observable work, never elapsed time.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

func shaOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// incrementalStampFixture builds a committed repo with one file that will be
// edited plus a set of provably-clean ones, a minimal prior graph, and a correct
// manifest baseline. It returns the repo, the state dir and the clean file list.
func incrementalStampFixture(t *testing.T, nStable int) (repo, stateDir string, stable []string) {
	t.Helper()
	isolateHome(t)
	repo = t.TempDir()
	stateDir = t.TempDir()

	// Distinct basename stems: FilterWithGit invalidates by stem (moduleBase),
	// so a shared one would drag a "clean" file into the changed set.
	stable = make([]string, nStable)
	for i := range stable {
		stable[i] = fmt.Sprintf("stable_%02d.go", i)
		writeFile(t, repo, stable[i], fmt.Sprintf("package p\n\nfunc Stable%02d() {}\n", i))
	}
	writeFile(t, repo, "victim.go", "package p\n\nfunc Victim() int { return 1 }\n")

	initGitRepo(t, repo)
	gitCommitAll(t, repo, "base")

	buildMinimalGraph(t, stateDir, []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "Victim", "victim.go"),
			Name: "Victim", Kind: "SCOPE.Operation", SourceFile: "victim.go", Language: "go"},
	}, nil)
	seedManifest(t, repo, stateDir)
	return repo, stateDir, stable
}

// TestIncremental_Success_DoesNotStampWritesThatLandAfterExtraction is the
// defect, on the daemon's primary path.
//
// The pass extracts version 2 of the file. Version 3 is written into the working
// tree at the writeGraphGen seam — after the extraction, before Step 9. The graph
// therefore contains version 2's entities. If the manifest records version 3 it
// is claiming to have indexed content that is not in the graph, and the claim is
// self-concealing: the next pass hash-matches version 3, classifies the file
// UNCHANGED, and the edit is never picked up.
func TestIncremental_Success_DoesNotStampWritesThatLandAfterExtraction(t *testing.T) {
	repo, stateDir, _ := incrementalStampFixture(t, 3)

	const extracted = "package p\n\nfunc Victim() int { return 2 }\n"
	const landedMidPass = "package p\n\nfunc Victim() int { return 3 }\n\nfunc SavedMidReindex() {}\n"
	writeFile(t, repo, "victim.go", extracted)

	// prev is assigned by the swap below and read only when the seam FIRES,
	// which is during TryIncremental — long after the assignment.
	var seamFired bool
	var prev extractors.GraphGenWriter
	var restore func()
	restore, prev = extractors.SwapWriteGraphGen(func(dir string, doc *graph.Document) (string, fbwriter.UndeclaredKindReport, error) {
		seamFired = true
		writeFile(t, repo, "victim.go", landedMidPass)
		return prev(dir, doc)
	})
	t.Cleanup(restore)

	logger := log.New(io.Discard, "", 0)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if !res.Done {
		t.Fatalf("fixture is inert: the pass did not succeed (fallback=%q), so Step 9 never ran",
			res.FallbackReason)
	}
	if !seamFired {
		t.Fatal("fixture is inert: writeGraphGen never ran, so no write landed inside the window")
	}

	got := diff.LoadManifest(stateDir).Files["victim.go"].SHA256
	if got == shaOf(landedMidPass) {
		t.Fatalf("Step 9 stamped victim.go with the content that landed AFTER extraction. Those " +
			"bytes are not in the graph, yet the next pass hash-matches this stamp and classifies " +
			"the file UNCHANGED — the save is invisible until the file is touched again or HEAD " +
			"advances. This is the daemon's primary path (#6212).")
	}
	if want := shaOf(extracted); got != want {
		t.Fatalf("victim.go stamped %q, want the hash of the bytes this pass actually extracted "+
			"(%q) — the manifest must describe what the graph was built from (#6212)", got, want)
	}
}

// TestIncremental_Success_DoesNotSweepUnchangedFiles is the removal, on the
// success path.
//
// #6201 took the full-repo sweep off the too-many-changed REJECT; the successful
// pass kept one. Stamping from the extraction loop's own bytes removes it there
// too, which makes Step 9 O(delta) instead of O(repo).
//
// Same proof shape as #6201's, and it needs no seam: the clean files are
// committed, untouched and not reported by `git diff --name-only HEAD`, so the
// change detector never opens them. Only an indiscriminate full-repo sweep can
// overwrite a poison stamp, so surviving poison IS the evidence that none ran.
func TestIncremental_Success_DoesNotSweepUnchangedFiles(t *testing.T) {
	const nStable = 6
	repo, stateDir, stable := incrementalStampFixture(t, nStable)

	const poison = "poison-stamp-6212"
	m := diff.LoadManifest(stateDir)
	for _, rel := range stable {
		e := m.Files[rel]
		e.SHA256 = poison
		m.Files[rel] = e
	}
	preVictim := m.Files["victim.go"].SHA256
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("save poisoned manifest: %v", err)
	}

	writeFile(t, repo, "victim.go", "package p\n\nfunc Victim() int { return 42 }\n")

	logger := log.New(io.Discard, "", 0)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	if !res.Done {
		t.Fatalf("fixture is inert: the pass did not succeed (fallback=%q)", res.FallbackReason)
	}

	after := diff.LoadManifest(stateDir)

	// PROPERTY 1 — no full-repo sweep on the success path.
	swept := 0
	for _, rel := range stable {
		e, ok := after.Files[rel]
		if !ok {
			t.Fatalf("clean file %s was pruned from the manifest by a successful pass", rel)
		}
		if e.SHA256 != poison {
			swept++
		}
	}
	if swept != 0 {
		t.Fatalf("a successful incremental pass hashed %d/%d provably-clean files (poison stamp "+
			"overwritten) — Step 9 still re-hashes the whole repo to reproduce stamps it already "+
			"holds (#6212, #6206)", swept, nStable)
	}

	// PROPERTY 2 — the #5668 loop guard survives the removal. The file that DID
	// change must be re-stamped, or it re-presents as changed on every pass,
	// counts against the too-many-changed limit and pins the daemon in a loop.
	e, ok := after.Files["victim.go"]
	if !ok {
		t.Fatal("the changed file is missing from the manifest entirely")
	}
	if e.SHA256 == preVictim || e.SHA256 == "" {
		t.Fatalf("the changed file kept its stale stamp %q — dropping the sweep also dropped the "+
			"#5668 reindex loop guard", e.SHA256)
	}
}
