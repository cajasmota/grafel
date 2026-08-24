// #6579: canonicalizePath's protected-path gate is consulted per segment and
// BEFORE readDirBounded, so the prompt-causing enumeration is already gated
// (#6548). What was not gated is the SPELLING of the protected segment.
//
// canonicalizePath recovers each segment's on-disk casing by reading its
// parent. When that parent read does not complete — the launchd-context
// permission stall / slow-FS degrade documented at #5330 — the segment keeps
// the casing the user typed. On macOS's case-insensitive APFS `~/documents` is
// `~/Documents`, but the protected-path table compared byte-exactly, so the
// gate said "not protected" and canonicalizePath os.ReadDir'd the
// iCloud-managed folder anyway. That is the reported consent dialog, reached
// through the one path where the casing recovery cannot help.
//
// These tests assert the SET OF DIRECTORIES readDirBounded is invoked on, not
// the returned path: the read is the defect, the return value is not.
//
// No test here reads a real ~/Documents or ~/Library: the home is injected and
// the fixture lives under t.TempDir().
package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordReads swaps readDirFunc for one that appends every directory it is
// asked for to the returned slice pointer. deny, when non-empty, makes the
// read of that exact directory fail — simulating the #5330 stall that leaves a
// segment with the casing the user typed.
func recordReads(t *testing.T, deny string) *[]string {
	t.Helper()
	var read []string
	orig := readDirFunc
	readDirFunc = func(dir string) ([]os.DirEntry, error) {
		read = append(read, dir)
		if deny != "" && filepath.Clean(dir) == filepath.Clean(deny) {
			return nil, errors.New("simulated permission stall (#5330)")
		}
		return os.ReadDir(dir)
	}
	t.Cleanup(func() { readDirFunc = orig })
	return &read
}

// assertNotRead fails naming the directory that was enumerated.
func assertNotRead(t *testing.T, read []string, forbidden string) {
	t.Helper()
	for _, dir := range read {
		if dir == forbidden || strings.HasPrefix(dir, forbidden+string(filepath.Separator)) {
			t.Errorf("canonicalizePath enumerated protected directory %q (#6579); reads = %v", dir, read)
		}
	}
}

// TestCanonicalizePath_SkipsProtectedDirTypedInLowerCase is the #6579
// regression. The casing recovery for the protected segment cannot run (its
// parent's read fails), so the gate sees the spelling the user typed. It must
// still refuse.
func TestCanonicalizePath_SkipsProtectedDirTypedInLowerCase(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(filepath.Join(docs, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHomeOnDarwin(t, home)

	// $HOME's read stalls, so "documents" never gets folded up to "Documents".
	read := recordReads(t, home)

	got := canonicalizePath(filepath.Join(home, "documents", "proj"))

	assertNotRead(t, *read, filepath.Join(home, "documents"))
	assertNotRead(t, *read, docs)
	if got == "" {
		t.Fatal("canonicalizePath returned empty")
	}
}

// The literal folder from the report: ~/Library/Mobile Documents is iCloud
// Drive. It is classMedia (refused at or under), so neither ~/Library nor
// anything beneath it may be enumerated — in any casing.
func TestCanonicalizePath_NeverEnumeratesICloudDrive(t *testing.T) {
	for _, spelling := range []string{"Library", "library", "LIBRARY"} {
		t.Run(spelling, func(t *testing.T) {
			home := t.TempDir()
			icloud := filepath.Join(home, spelling, "Mobile Documents", "com~apple~CloudDocs")
			if err := os.MkdirAll(filepath.Join(icloud, "proj"), 0o755); err != nil {
				t.Fatal(err)
			}
			fakeHomeOnDarwin(t, home)
			read := recordReads(t, "")

			canonicalizePath(filepath.Join(icloud, "proj"))

			assertNotRead(t, *read, filepath.Join(home, spelling))
		})
	}
}

// A *.photoslibrary bundle is refused wherever it appears, not only under a
// protected home folder (#5296).
func TestCanonicalizePath_NeverEnumeratesPhotosLibraryBundle(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(home, "Projects", "Family.photoslibrary")
	if err := os.MkdirAll(filepath.Join(bundle, "originals"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHomeOnDarwin(t, home)
	read := recordReads(t, "")

	canonicalizePath(filepath.Join(bundle, "originals"))

	assertNotRead(t, *read, bundle)
}

// Permissive direction, at the read-set level: an ordinary path must still be
// fully enumerated, or the gate has stopped canonicalizing every repo on the
// machine (and #2086's one-repo-one-store-root property goes with it).
func TestCanonicalizePath_OrdinaryPathIsStillEnumerated(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "Projects", "grafel")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHomeOnDarwin(t, home)
	read := recordReads(t, "")

	if got := canonicalizePath(filepath.Join(home, "Projects", "GRAFEL")); got != repo {
		t.Errorf("canonicalizePath = %q, want %q", got, repo)
	}
	want := filepath.Join(home, "Projects")
	found := false
	for _, dir := range *read {
		if dir == want {
			found = true
		}
	}
	if !found {
		t.Errorf("canonicalizePath never enumerated %q; reads = %v", want, *read)
	}
}
