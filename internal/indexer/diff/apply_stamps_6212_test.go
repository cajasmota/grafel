package diff

// #6212 — ApplyStamps is the manifest's new write path: the indexer hands it the
// hashes computed while the walk read each file, and it must record them and
// reconcile membership WITHOUT touching the filesystem.
//
// These are unit tests on the function's three obligations, because the
// end-to-end fixtures in cmd/grafel cannot see two of them: their walks never
// shrink (so nothing stale ever needs pruning) and their runs stamp every file
// (so the "keep what you were not given" rule is never exercised).

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func manifestWith(entries map[string]string) *Manifest {
	m := newManifest()
	for rel, sha := range entries {
		m.Files[rel] = FileEntry{SHA256: sha, Size: 1, Mtime: 1}
	}
	return m
}

func manifestKeys(m *Manifest) string {
	keys := make([]string, 0, len(m.Files))
	for k := range m.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// TestApplyStamps_RecordsWalkStampsWithoutHashingAnything is the removal itself.
//
// The O(repo) SHA-256 sweep the commit used to pay (218 ms of a zero-change
// pass's 738 ms on a 3003-file fixture, #6206) is gone because the hashes now
// arrive from the walk. The counter, not a stopwatch, is the proof: it must read
// zero even though a real file sits at the stamped path and would hash fine.
func TestApplyStamps_RecordsWalkStampsWithoutHashingAnything(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newManifest()
	before := HashCallCount()
	ApplyStamps(map[string]FileEntry{
		"a.go": {SHA256: "walktimehash", Size: 10, Mtime: 42},
	}, []string{"a.go"}, m)

	if n := HashCallCount() - before; n != 0 {
		t.Fatalf("ApplyStamps hashed %d file(s); it must do no file I/O at all — the walk already "+
			"paid for these hashes and re-taking them is what made the manifest able to describe "+
			"bytes that were never indexed (#6212)", n)
	}
	got := m.Files["a.go"]
	if got.SHA256 != "walktimehash" || got.Size != 10 || got.Mtime != 42 {
		t.Fatalf("stamp not recorded verbatim: %+v — the entry must be the walk's, not a re-derived "+
			"one (#6212)", got)
	}
}

// TestApplyStamps_PrunesEntriesNoLongerInTheWalk pins #5667's loop guard on the
// new path.
//
// An entry for a file that has left the gitignore-aware walk is otherwise
// immortal: it is reported "deleted" on every pass, perpetually trips the
// too-many-changed full-reindex fallback and pins the daemon in a reindex loop.
// UpdateManifestScoped carried this reconcile; ApplyStamps replaced it as the
// commit-path writer, so the guard has to come with it.
func TestApplyStamps_PrunesEntriesNoLongerInTheWalk(t *testing.T) {
	m := manifestWith(map[string]string{
		"live.go":        "old",
		"now-ignored.go": "stale",
	})

	ApplyStamps(map[string]FileEntry{
		"live.go": {SHA256: "fresh", Size: 2, Mtime: 2},
	}, []string{"live.go"}, m)

	if got := manifestKeys(m); got != "live.go" {
		t.Fatalf("manifest keys are %q, want \"live.go\" — an entry for a file no longer in the walk "+
			"survived. It re-presents as \"deleted\" on every pass and re-trips the too-many-changed "+
			"fallback forever (#5667)", got)
	}
	if m.Files["live.go"].SHA256 != "fresh" {
		t.Fatalf("live.go kept its old stamp %q, want the walk's %q", m.Files["live.go"].SHA256, "fresh")
	}
}

// TestApplyStamps_KeepsEntriesForWalkedFilesItWasNotGiven pins the delta case.
//
// On an incremental run the walk only reads the changed files, so only those
// carry a stamp. Every other walked file must keep the entry it already had —
// the same rule UpdateManifestScoped applies to its unhashed keepPaths, correct
// for the same reason: the caller has already established they did not change.
// Dropping them instead would make every unchanged file read as new on the next
// pass, which is #5667's loop from the other direction.
func TestApplyStamps_KeepsEntriesForWalkedFilesItWasNotGiven(t *testing.T) {
	m := manifestWith(map[string]string{
		"changed.go":   "old",
		"untouched.go": "baseline",
	})

	ApplyStamps(map[string]FileEntry{
		"changed.go": {SHA256: "new", Size: 3, Mtime: 3},
	}, []string{"changed.go", "untouched.go"}, m)

	if got := manifestKeys(m); got != "changed.go,untouched.go" {
		t.Fatalf("manifest keys are %q, want both files — a walked file with no stamp this pass lost "+
			"its baseline and will read as new forever (#6212)", got)
	}
	if got := m.Files["untouched.go"].SHA256; got != "baseline" {
		t.Fatalf("untouched.go's stamp changed to %q, want %q untouched", got, "baseline")
	}
	if got := m.Files["changed.go"].SHA256; got != "new" {
		t.Fatalf("changed.go's stamp is %q, want the walk's %q", got, "new")
	}
}
