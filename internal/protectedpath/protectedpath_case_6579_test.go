// #6579: the protected-path table was compared case-SENSITIVELY on
// filesystems that fold case.
//
// macOS ships a case-insensitive APFS volume by default, so `~/documents`,
// `~/Documents` and `~/DOCUMENTS` are one directory — and reading any of those
// spellings pops the same «"grafel" wants to access files managed by "iCloud
// Drive"» consent dialog. The table keyed the denylist by exact basename and
// compared ancestors with a byte-exact prefix test, so every non-canonical
// spelling of a protected folder came back NOT protected and the read went
// ahead. The gate was permissive in exactly the direction that re-opens #6548.
//
// The class distinction from #6576 must survive the fix: media is refused for
// BOTH predicates, TCC only for the traversal (inferred) predicate, so pointing
// grafel at a repo inside ~/documents stays a legitimate instruction.
//
// No test here touches a real protected directory: every home is a t.TempDir()
// and the folders are never created — the predicates are pure path logic.
package protectedpath

import (
	"path/filepath"
	"strings"
	"testing"
)

// caseVariants returns the spellings of name a user can type on a
// case-insensitive filesystem and still land on the same directory.
func caseVariants(name string) []string {
	return []string{
		name,                              // Documents
		lower(name),                       // documents
		upper(name),                       // DOCUMENTS
		lower(name[:1]) + name[1:],        // documents (leading fold)
		upper(name[:1]) + lower(name[1:]), // Documents
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

// TestIsTraversalProtectedIn_FoldsCaseOnDarwin is the #6579 regression for the
// inferred-traversal predicate: every case variant of every protected folder,
// and of any path under one, must be refused.
func TestIsTraversalProtectedIn_FoldsCaseOnDarwin(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Library", "Movies", "Music", "Photos", "Pictures", "Desktop", "Documents", "Downloads", "Public"} {
		for _, variant := range caseVariants(name) {
			dir := filepath.Join(home, variant)
			if protected, _ := IsTraversalProtectedIn(dir, home, "darwin"); !protected {
				t.Errorf("IsTraversalProtectedIn(~/%s) = false, want true — on a case-insensitive "+
					"filesystem that is the same directory as ~/%s and reading it re-opens #6548", variant, name)
			}
			deep := filepath.Join(dir, "Mobile Documents", "com~apple~CloudDocs", "proj")
			if protected, _ := IsTraversalProtectedIn(deep, home, "darwin"); !protected {
				t.Errorf("IsTraversalProtectedIn(%q) = false, want true (under ~/%s)", deep, variant)
			}
		}
	}
}

// TestIsWalkProtectedIn_FoldsCaseOnDarwin is the same regression for the media
// predicate: `~/music/band-site` is `~/Music/band-site`, and descending into it
// pops the Music privacy prompt (#5296).
func TestIsWalkProtectedIn_FoldsCaseOnDarwin(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Library", "Movies", "Music", "Photos", "Pictures"} {
		for _, variant := range caseVariants(name) {
			repo := filepath.Join(home, variant, "band-site")
			if protected, _ := IsWalkProtectedIn(repo, home, "darwin"); !protected {
				t.Errorf("IsWalkProtectedIn(%q) = false, want true — ~/%s is ~/%s on a "+
					"case-insensitive filesystem (#5296)", repo, variant, name)
			}
		}
	}
}

// The class distinction (#6576) must survive the fold: the walk predicate is
// MEDIA-ONLY, so a repo the user explicitly registered inside ~/documents is
// still indexable. Folding must not widen the walk predicate into the TCC set.
func TestIsWalkProtectedIn_TCCClassStaysExemptUnderFolding(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Public"} {
		for _, variant := range caseVariants(name) {
			repo := filepath.Join(home, variant, "grafel")
			if protected, reason := IsWalkProtectedIn(repo, home, "darwin"); protected {
				t.Errorf("IsWalkProtectedIn(%q) = true (%s); an EXPLICITLY registered repo under "+
					"~/%s must stay indexable (#6548 explicit-path exemption)", repo, reason, variant)
			}
		}
	}
}

// The sibling-scan predicates read the same table by BASENAME, so they carry
// the same hole: the wizard classifying `~/documents/proj` would enumerate the
// real ~/Documents looking for sibling repos. internal/install/detect delegates
// to these verbatim, so this is where that consumer is observed.
func TestScanParentAndHomeChild_FoldCaseOnDarwin(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Library", "Movies", "Music", "Photos", "Pictures", "Desktop", "Documents", "Downloads", "Public"} {
		for _, variant := range caseVariants(name) {
			parent := filepath.Join(home, variant)
			if !IsProtectedScanParentIn(parent, home, "darwin") {
				t.Errorf("IsProtectedScanParentIn(~/%s) = false, want true — enumerating it for "+
					"sibling repos reads the real ~/%s (#6579)", variant, name)
			}
			if !IsProtectedScanParentIn(filepath.Join(parent, "proj"), home, "darwin") {
				t.Errorf("IsProtectedScanParentIn(~/%s/proj) = false, want true", variant)
			}
			if !IsProtectedHomeChildIn(home, variant, home, "darwin") {
				t.Errorf("IsProtectedHomeChildIn($HOME, %q) = false, want true — classifying $HOME "+
					"would descend into it (#6579)", variant)
			}
			if !IsProtectedHomeDir(variant) {
				t.Errorf("IsProtectedHomeDir(%q) = false, want true", variant)
			}
		}
	}
	// A differently-spelled $HOME must still be recognised as $HOME.
	if !IsProtectedScanParentIn(strings.ToUpper(home), home, "darwin") {
		t.Errorf("IsProtectedScanParentIn(upper-cased $HOME) = false; enumerating $HOME itself is " +
			"the v0.1.8 batch-prompt bug and must be refused in any spelling")
	}
	// Permissive direction for these two as well.
	for _, name := range []string{"Documentation", "Projects", "src"} {
		if IsProtectedScanParentIn(filepath.Join(home, name), home, "darwin") {
			t.Errorf("IsProtectedScanParentIn(~/%s) = true, want false", name)
		}
		if IsProtectedHomeChildIn(home, name, home, "darwin") {
			t.Errorf("IsProtectedHomeChildIn($HOME, %q) = true, want false", name)
		}
	}
}

// Permissive direction: folding must not swallow ordinary neighbours. These
// are distinct directories under any casing rule and must stay readable, or
// canonicalization silently stops for real repos.
func TestFoldingDoesNotOverReach(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{
		"Documentation", "documentation", "MusicStudio", "musicstudio",
		"Librarian", "Public-Site", "Projects", "src", "Downloaded",
	} {
		p := filepath.Join(home, dir)
		if protected, reason := IsTraversalProtectedIn(p, home, "darwin"); protected {
			t.Errorf("IsTraversalProtectedIn(~/%s) = true (%s), want false", dir, reason)
		}
	}
}

// The folding rule must stay the SAME rule internal/pathboundary applies
// (caseInsensitiveFS/eq/containedIn there). #6579 is precisely what happens
// when the containment half folds and the protection half does not, so the
// rule itself is pinned here rather than left to drift. It cannot be shared by
// import: pathboundary imports this package, so the dependency only goes one
// way.
func TestCaseInsensitiveFSMatchesPathboundaryRule(t *testing.T) {
	for goos, want := range map[string]bool{
		"darwin":  true,
		"windows": true,
		"linux":   false,
		"freebsd": false,
		"openbsd": false,
	} {
		if got := caseInsensitiveFS(goos); got != want {
			t.Errorf("caseInsensitiveFS(%q) = %v, want %v — internal/pathboundary folds case on "+
				"darwin and windows only; a protection table that disagrees with the containment "+
				"check is #6579", goos, got, want)
		}
	}
}

// The table is TCC-specific and stays macOS-only: on Linux "documents" and
// "Documents" are genuinely different directories and neither carries privacy
// semantics.
func TestFoldingStaysDarwinOnly(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Documents", "documents", "Music", "music"} {
		p := filepath.Join(home, name)
		if protected, reason := IsTraversalProtectedIn(p, home, "linux"); protected {
			t.Errorf("IsTraversalProtectedIn(~/%s, goos=linux) = true (%s), want false", name, reason)
		}
	}
}
