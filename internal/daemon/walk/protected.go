package walk

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// This file implements the shared "TCC guard" for the directory walk used by
// BOTH the indexer (WalkRepo) and the reactive watcher (subscribeRepo). It is
// defence-in-depth against the macOS privacy / TCC failure mode that triggered
// issue #5296: a registered repo path that walks into the user's protected
// media folders (~/Music, ~/Photos, ...) or into a *.musiclibrary /
// *.photoslibrary bundle causes macOS to pop a "grafel would like to access
// your Music / Photos / media library" permission prompt. A first-run public
// release must NEVER make a user see that prompt while indexing their code.
//
// Two distinct protections live here:
//
//  1. ProtectedHomeSubdir — a HARD refusal: if a directory path resolves
//     (symlinks followed) to be at or under one of the user's protected home
//     media folders, it is never entered. If a REGISTERED REPO ROOT itself
//     resolves into one of these, the caller refuses to index it with a WARN.
//
//  2. IsMediaLibraryBundle — a by-name skip for the macOS media-library
//     bundles (*.musiclibrary, *.photoslibrary, *.tvlibrary, ...) found
//     ANYWHERE in the tree. Descending into a *.photoslibrary is exactly what
//     triggers the Photos TCC prompt, so these are hard-skipped by basename
//     regardless of where they appear. The basename checks are harmless
//     cross-platform; the absolute-home checks are gated on darwin.

// protectedHomeBasenames are the top-level home subdirectories macOS protects
// behind TCC. Walking into any of them can trigger a privacy prompt. The list
// is intentionally small and constant so it is trivially auditable.
var protectedHomeBasenames = []string{
	"Music",
	"Photos",
	"Movies",
	"Pictures",
	"Library",
}

// mediaLibraryBundleSuffixes are the macOS media-library bundle extensions.
// A directory whose basename ends with one of these is a packaged media
// library; descending into it is what trips the Music/Photos TCC prompt.
var mediaLibraryBundleSuffixes = []string{
	".musiclibrary",
	".photoslibrary",
	".tvlibrary",
	".aplibrary", // Aperture
	".migratedaplibrary",
}

// DefaultWatchDirCap is the default ceiling on the number of directories the
// watcher will subscribe to (and that the indexer treats as a "this is not a
// real code repo" tripwire) for a single repo. The live failure registered
// 875 watch dirs on a 588MB non-code tree; a real code repo's source-only dir
// count (after skip-list pruning) is almost always well under this. It is
// generous so legitimate large monorepos still index fully. Override via
// GRAFEL_WATCH_DIR_CAP.
const DefaultWatchDirCap = 5000

// WatchDirCap returns the effective per-repo watch-dir ceiling, honouring the
// GRAFEL_WATCH_DIR_CAP environment override. A value <= 0 disables the cap.
func WatchDirCap() int {
	if v := strings.TrimSpace(os.Getenv("GRAFEL_WATCH_DIR_CAP")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return DefaultWatchDirCap
}

// IsMediaLibraryBundle reports whether a directory basename is a macOS
// media-library bundle (*.musiclibrary, *.photoslibrary, ...). These are
// hard-skipped by name anywhere in the tree, cross-platform (harmless on
// non-darwin since the names don't occur there).
func IsMediaLibraryBundle(base string) bool {
	lower := strings.ToLower(base)
	for _, suf := range mediaLibraryBundleSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}

// homeDir returns the user's home directory, or "" if it cannot be resolved.
// Split out so tests can exercise IsProtectedPath against a fake home via the
// withHome variant.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// IsProtectedPath reports whether absPath is at or under one of the user's
// protected macOS home media folders (~/Music, ~/Photos, ~/Movies,
// ~/Pictures, ~/Library), OR is itself a media-library bundle. Symlinks are
// resolved FIRST so a repo symlinked into ~/Music is still caught.
//
// On non-darwin the absolute-home checks are skipped (these folders are not
// TCC-protected elsewhere), but the media-library-bundle name check still
// applies because it is harmless and keeps behaviour uniform in tests.
func IsProtectedPath(absPath string) (bool, string) {
	return isProtectedPathWithHome(absPath, homeDir(), runtime.GOOS)
}

// isProtectedPathWithHome is the testable core of IsProtectedPath. home is the
// home directory to treat as protected; goos selects the platform gate.
func isProtectedPathWithHome(absPath, home, goos string) (bool, string) {
	// Media-library bundle by basename — applies anywhere, any platform.
	if IsMediaLibraryBundle(filepath.Base(absPath)) {
		return true, "media-library bundle: " + filepath.Base(absPath)
	}

	// Resolve symlinks so a repo (or subdir) symlinked into a protected
	// folder is caught. If resolution fails (broken symlink, race), fall
	// back to the literal path so we still apply the textual check.
	resolved := absPath
	if r, err := filepath.EvalSymlinks(absPath); err == nil {
		resolved = r
	}

	// A media-library bundle revealed only after symlink resolution.
	if IsMediaLibraryBundle(filepath.Base(resolved)) {
		return true, "media-library bundle: " + filepath.Base(resolved)
	}

	// The protected-home checks are macOS-specific (TCC). Off darwin these
	// folders carry no special privacy semantics.
	if goos != "darwin" {
		return false, ""
	}
	if home == "" {
		return false, ""
	}

	for _, name := range protectedHomeBasenames {
		protected := filepath.Join(home, name)
		// Canonicalize the protected base too: on macOS the home dir may
		// itself contain symlinked components (e.g. /var → /private/var under
		// a temp home in tests, or a relocated home), so we must compare
		// resolved-against-resolved.
		if r, err := filepath.EvalSymlinks(protected); err == nil {
			protected = r
		}
		if pathAtOrUnder(resolved, protected) {
			return true, "protected macOS folder: ~/" + name
		}
	}
	return false, ""
}

// pathAtOrUnder reports whether p is equal to base or nested under it. Both
// are cleaned; comparison is component-wise so "~/MusicStudio" is NOT treated
// as under "~/Music".
func pathAtOrUnder(p, base string) bool {
	p = filepath.Clean(p)
	base = filepath.Clean(base)
	if p == base {
		return true
	}
	return strings.HasPrefix(p, base+string(filepath.Separator))
}
