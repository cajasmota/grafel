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

// foldedHomeDirs is protectedHomeDirs keyed by lower-cased basename, so the
// table can be consulted with whatever spelling reached us. See caseInsensitiveFS.
var foldedHomeDirs = func() map[string]class {
	m := make(map[string]class, len(protectedHomeDirs))
	for name, c := range protectedHomeDirs {
		m[strings.ToLower(name)] = c
	}
	return m
}()

// caseInsensitiveFS reports whether goos's default filesystem folds case, i.e.
// whether `~/documents` and `~/Documents` name the same directory (#6579).
//
// This is the SAME rule internal/pathboundary applies in its own
// caseInsensitiveFS/eq/containedIn, deliberately spelled the same way. It is
// duplicated rather than imported because pathboundary imports this package
// (#6576 routes Climb through IsTraversalProtected) — sharing the identifier
// the other way would be an import cycle. It takes goos rather than reading
// runtime.GOOS so every predicate here stays exercisable from any CI leg.
//
// Why it matters: macOS ships a case-insensitive APFS volume by default, so a
// user can type — and a tool can be handed — any spelling of a protected folder
// and land on the same TCC-gated, iCloud-managed directory. pathboundary's
// containment check already folded; this table's lookup did not, so a climb
// could be told "contained in $HOME" and then "not protected" for one and the
// same `~/documents`. That is the permissive direction, and it re-opens the
// «"grafel" wants to access files managed by "iCloud Drive"» prompt of #6548
// and the Music/Photos prompt of #5296.
//
// A deliberately case-SENSITIVE macOS volume is the accepted cost: there,
// folding can refuse a genuinely distinct `~/documents`. Refusing an inferred
// read is the safe direction (canonicalizePath just skips casing recovery), the
// configuration is rare, and the surrounding code already assumes darwin folds
// case — internal/daemon's caseInsensitiveFS makes exactly the same call.
//
// The windows arm is currently unreachable through this package: every
// predicate that consults the table is darwin-gated first, because these
// folders carry TCC semantics nowhere else. It is stated anyway so the rule is
// the rule, and it is pinned by test rather than left to drift.
func caseInsensitiveFS(goos string) bool {
	return goos == "darwin" || goos == "windows"
}

// isProtectedHomeDir reports whether name is a protected top-level $HOME
// subdirectory under the union denylist. Name comparison only — the caller is
// responsible for knowing the name really is a child of $HOME.
//
// The comparison is case-insensitive, unconditionally and with no goos of its
// own. That is safe ONLY because it is unexported: both callers
// (isProtectedScanParentIn, isProtectedHomeChildIn — via their exported
// wrappers) gate on darwin before reaching it, and the compiler now enforces
// that nothing else can. It used to be exported, where the same sentence was an
// assertion no compiler checked and `IsProtectedHomeDir("documents")` answered
// true on Linux. Nothing outside this package ever called it; the two adapters
// in internal/daemon/walk and internal/install/detect delegate to the
// path-taking predicates instead. If an external caller is ever wanted, give it
// a goos parameter rather than re-exporting this as-is (#6579).
func isProtectedHomeDir(name string) bool {
	_, ok := foldedHomeDirs[strings.ToLower(name)]
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
		if pathAtOrUnder(resolved, protected, caseInsensitiveFS(goos)) {
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
	if samePath(parent, home, caseInsensitiveFS(goos)) {
		return true
	}
	first, ok := firstHomeComponent(parent, home, caseInsensitiveFS(goos))
	if !ok {
		return false
	}
	return isProtectedHomeDir(first)
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
	if !samePath(parent, home, caseInsensitiveFS(goos)) {
		return false
	}
	return isProtectedHomeDir(name)
}

// samePath reports whether p and q name the same directory, folding case when
// the filesystem does (#6579).
func samePath(p, q string, fold bool) bool {
	p, q = filepath.Clean(p), filepath.Clean(q)
	if fold {
		return strings.EqualFold(p, q)
	}
	return p == q
}

// firstHomeComponent returns the first path component of p relative to home,
// and whether p is strictly under home. Component-wise: "~/Documentation"
// yields "Documentation", never "Documents". When fold is set both the home
// prefix and the returned component are lower-cased, so a differently-spelled
// home still resolves and the caller's table lookup (itself case-insensitive)
// still hits (#6579).
func firstHomeComponent(p, home string, fold bool) (string, bool) {
	if fold {
		p, home = strings.ToLower(p), strings.ToLower(home)
	}
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
// treated as under "~/Music". When fold is set the comparison ignores case,
// because on that filesystem the two spellings are one directory (#6579).
func pathAtOrUnder(p, base string, fold bool) bool {
	p = filepath.Clean(p)
	base = filepath.Clean(base)
	if fold {
		p = strings.ToLower(p)
		base = strings.ToLower(base)
	}
	if p == base {
		return true
	}
	return strings.HasPrefix(p, base+string(filepath.Separator))
}
