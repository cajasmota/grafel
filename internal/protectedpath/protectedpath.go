// Package protectedpath is grafel's SINGLE authority on which filesystem
// locations must not be read.
//
// Before #6548 there were two denylists that disagreed:
//
//	internal/daemon/walk/protected.go   Music, Photos, Movies, Pictures, Library
//	internal/install/detect/protected*  Desktop, Documents, Downloads, Library,
//	                                    Movies, Music, Pictures, Public
//
// The walk list omitted Desktop/Documents/Downloads — exactly the folders that
// become file-provider (iCloud Drive) managed when macOS "Desktop & Documents
// Folders" sync is on — and no ancestor traversal consulted either list. That
// is how a user who never pointed grafel at iCloud was shown
// «"grafel" wants to access files managed by "iCloud Drive"»: canonicalizePath
// os.ReadDir'd every ancestor of every repo path, ~/Documents included.
//
// This package holds ONE table, the union of both lists, with each entry
// annotated by WHY it is protected. Two questions are asked of that one table,
// and the distinction matters:
//
//   - IsWalkProtected  — "may the indexer walk here?"  Media classes only.
//     A repo the user EXPLICITLY registered inside ~/Documents is a legitimate
//     thing to index, so the walk must not refuse it; a repo inside ~/Music or
//     a *.photoslibrary bundle is never source code (#5296) and is refused.
//   - IsTraversalProtected — "may INFERRED traversal read here?"  Full union.
//     Ancestor canonicalization, sibling scans and home enumeration were never
//     asked for by the user, so they may not read a TCC-gated folder at all.
//
// That is the "explicit path stays exempt" rule from #6548 expressed
// structurally: explicit roots go through the walk predicate, inferred reads
// go through the traversal predicate.
//
// The home-directory checks are macOS-only (TCC has no analogue elsewhere);
// the media-library-bundle basename check applies on every platform because it
// is harmless and keeps behaviour uniform in tests. Every predicate has an
// "…In" variant taking an explicit home and GOOS so tests can exercise it
// against a t.TempDir() fixture and never touch a real protected directory.
package protectedpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// class describes WHY a top-level $HOME subdirectory is protected.
type class int

const (
	// classMedia — a macOS media location. Never legitimate source-code
	// storage, so even an explicitly registered repo root here is refused
	// (#5296: descending into ~/Music or a *.photoslibrary pops the
	// Music/Photos privacy prompt).
	classMedia class = iota + 1
	// classTCC — TCC-gated but a perfectly legitimate place to keep a repo.
	// Inferred traversal must not read it; an explicit registration may.
	classTCC
)

// protectedHomeDirs is THE denylist: the union of the two former ones, keyed
// by basename of a directory sitting directly under $HOME. Kept small,
// constant and annotated so it stays trivially auditable.
var protectedHomeDirs = map[string]class{
	"Library":  classMedia, // also ~/Library/Mobile Documents == iCloud Drive
	"Movies":   classMedia,
	"Music":    classMedia,
	"Photos":   classMedia,
	"Pictures": classMedia,

	"Desktop":   classTCC, // iCloud "Desktop & Documents" sync makes these
	"Documents": classTCC, // file-provider managed — the #6548 prompt
	"Downloads": classTCC,
	"Public":    classTCC,
}

// mediaLibraryBundleSuffixes are the macOS media-library bundle extensions. A
// directory whose basename ends with one of these is a packaged media library;
// descending into it is what trips the Music/Photos TCC prompt, so it is
// hard-skipped by basename wherever it appears.
var mediaLibraryBundleSuffixes = []string{
	".musiclibrary",
	".photoslibrary",
	".tvlibrary",
	".aplibrary", // Aperture
	".migratedaplibrary",
}

// IsMediaLibraryBundle reports whether a directory basename is a macOS
// media-library bundle (*.musiclibrary, *.photoslibrary, ...).
func IsMediaLibraryBundle(base string) bool {
	lower := strings.ToLower(base)
	for _, suf := range mediaLibraryBundleSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}

// IsProtectedHomeDir reports whether name is a protected top-level $HOME
// subdirectory under the union denylist. Name comparison only — the caller is
// responsible for knowing the name really is a child of $HOME.
func IsProtectedHomeDir(name string) bool {
	_, ok := protectedHomeDirs[name]
	return ok
}

// HomeDir returns the user's home directory, or "" if it cannot be resolved.
func HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// IsWalkProtected reports whether the indexer/watcher walk must refuse
// absPath: it is at or under a media-class home folder, or is a media-library
// bundle. Symlinks are resolved first so a repo symlinked into ~/Music is
// still caught.
func IsWalkProtected(absPath string) (bool, string) {
	return IsWalkProtectedIn(absPath, HomeDir(), runtime.GOOS)
}

// IsWalkProtectedIn is IsWalkProtected against an explicit home and GOOS.
func IsWalkProtectedIn(absPath, home, goos string) (bool, string) {
	return protectedIn(absPath, home, goos, func(c class) bool { return c == classMedia })
}

// IsTraversalProtected reports whether INFERRED traversal (ancestor
// canonicalization, sibling scans, home enumeration) must not read absPath. It
// uses the full union: nothing the user did not explicitly point us at may
// read ~/Desktop, ~/Documents or ~/Downloads either (#6548).
func IsTraversalProtected(absPath string) (bool, string) {
	return IsTraversalProtectedIn(absPath, HomeDir(), runtime.GOOS)
}

// IsTraversalProtectedIn is IsTraversalProtected against an explicit home and
// GOOS.
func IsTraversalProtectedIn(absPath, home, goos string) (bool, string) {
	return protectedIn(absPath, home, goos, func(class) bool { return true })
}

// protectedIn is the shared core. want selects which classes count.
func protectedIn(absPath, home, goos string, want func(class) bool) (bool, string) {
	// Media-library bundle by basename — applies anywhere, any platform.
	if IsMediaLibraryBundle(filepath.Base(absPath)) {
		return true, "media-library bundle: " + filepath.Base(absPath)
	}

	// Resolve symlinks so a path symlinked into a protected folder is caught.
	// If resolution fails (broken symlink, race, not-yet-created path) fall
	// back to the literal path so the textual check still applies.
	resolved := absPath
	if r, err := filepath.EvalSymlinks(absPath); err == nil {
		resolved = r
	}
	if IsMediaLibraryBundle(filepath.Base(resolved)) {
		return true, "media-library bundle: " + filepath.Base(resolved)
	}

	// The protected-home checks are macOS-specific (TCC). Elsewhere these
	// folders carry no special privacy semantics.
	if goos != "darwin" || home == "" {
		return false, ""
	}

	for name, c := range protectedHomeDirs {
		if !want(c) {
			continue
		}
		protected := filepath.Join(home, name)
		// Canonicalize the protected base too: the home dir may itself
		// contain symlinked components (/var → /private/var under a temp
		// home in tests, or a relocated home), so compare resolved against
		// resolved.
		if r, err := filepath.EvalSymlinks(protected); err == nil {
			protected = r
		}
		if pathAtOrUnder(resolved, protected) {
			return true, "protected macOS folder: ~/" + name
		}
	}
	return false, ""
}

// IsProtectedScanParent reports whether enumerating parent for siblings would
// read $HOME itself or a protected folder (or anything inside one). Callers
// skip the scan when this is true.
func IsProtectedScanParent(parent string) bool {
	return IsProtectedScanParentIn(parent, HomeDir(), runtime.GOOS)
}

// IsProtectedScanParentIn is IsProtectedScanParent against an explicit home
// and GOOS.
func IsProtectedScanParentIn(parent, home, goos string) bool {
	if goos != "darwin" || home == "" {
		return false
	}
	if filepath.Clean(parent) == filepath.Clean(home) {
		return true
	}
	first, ok := firstHomeComponent(parent, home)
	if !ok {
		return false
	}
	return IsProtectedHomeDir(first)
}

// IsProtectedHomeChild reports whether the dirent name under parent is a
// protected folder that must not be descended into. It fires ONLY when parent
// IS the home directory, so explicitly classifying ~/Documents still descends
// into its own children, and a folder merely named "Documents" elsewhere on
// disk is unaffected.
func IsProtectedHomeChild(parent, name string) bool {
	return IsProtectedHomeChildIn(parent, name, HomeDir(), runtime.GOOS)
}

// IsProtectedHomeChildIn is IsProtectedHomeChild against an explicit home and
// GOOS.
func IsProtectedHomeChildIn(parent, name, home, goos string) bool {
	if goos != "darwin" || home == "" {
		return false
	}
	if filepath.Clean(parent) != filepath.Clean(home) {
		return false
	}
	return IsProtectedHomeDir(name)
}

// firstHomeComponent returns the first path component of p relative to home,
// and whether p is strictly under home. Component-wise: "~/Documentation"
// yields "Documentation", never "Documents".
func firstHomeComponent(p, home string) (string, bool) {
	rel, err := filepath.Rel(home, p)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		return "", false
	}
	if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
		rel = rel[:i]
	}
	return rel, true
}

// pathAtOrUnder reports whether p equals base or is nested under it. Both are
// cleaned and the comparison is component-wise, so "~/MusicStudio" is NOT
// treated as under "~/Music".
func pathAtOrUnder(p, base string) bool {
	p = filepath.Clean(p)
	base = filepath.Clean(base)
	if p == base {
		return true
	}
	return strings.HasPrefix(p, base+string(filepath.Separator))
}
