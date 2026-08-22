package walk

import (
	"os"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/protectedpath"
)

// This file wires the directory walk used by BOTH the indexer (WalkRepo) and
// the reactive watcher (subscribeRepo) to grafel's single protected-path
// authority, internal/protectedpath. It used to carry its OWN denylist —
// Music, Photos, Movies, Pictures, Library — which disagreed with the one in
// internal/install/detect and, being the runtime path, was the shorter of the
// two. #6548 replaced both with the one authority; this file is now a thin
// adapter so no third convention can appear.
//
// It is defence-in-depth against the macOS privacy / TCC failure mode that
// triggered issue #5296: a registered repo path that walks into the user's
// protected media folders (~/Music, ~/Photos, ...) or into a *.musiclibrary /
// *.photoslibrary bundle causes macOS to pop a "grafel would like to access
// your Music / Photos / media library" permission prompt. A first-run public
// release must NEVER make a user see that prompt while indexing their code.
//
// Two distinct protections apply here:
//
//  1. IsProtectedPath — a HARD refusal: if a directory path resolves
//     (symlinks followed) to be at or under one of the user's protected home
//     MEDIA folders, it is never entered. If a REGISTERED REPO ROOT itself
//     resolves into one of these, the caller refuses to index it with a WARN.
//
//  2. IsMediaLibraryBundle — a by-name skip for the macOS media-library
//     bundles (*.musiclibrary, *.photoslibrary, ...) found ANYWHERE in the
//     tree. Descending into a *.photoslibrary is exactly what triggers the
//     Photos TCC prompt, so these are hard-skipped by basename regardless of
//     where they appear. The basename checks are harmless cross-platform; the
//     absolute-home checks are gated on darwin.
//
// NOTE on scope, deliberately chosen (#6548): the walk consults only the
// MEDIA classes of the authority's table, not the full union. The walk always
// runs against a repo root the user EXPLICITLY registered, and keeping a repo
// in ~/Documents or on the ~/Desktop is legitimate — refusing to index it
// would be a silent, permissive-direction regression. The full union applies
// to INFERRED traversal (protectedpath.IsTraversalProtected), which is what
// canonicalizePath and the sibling scans use.

// DefaultWatchDirCap is the default ceiling on the number of directories the
// watcher will subscribe to (and that the indexer treats as a "this is not a
// real code repo" tripwire) for a single repo. The live failure (#5296)
// registered 875 watch dirs on a 588MB non-code media tree — well under the
// original 5000 default, meaning the cap never actually caught its own
// origin case; the real media/asset protection is IsProtectedPath (the TCC
// guard) plus the .gitignore/.grafelignore/hardcoded-skip layers above,
// which stay in force regardless of this cap. Meanwhile a real large
// monorepo (measured: 9,120 directories) blew straight through the 5000
// default, silently truncating ~45% of the tree with no error. Raised to
// 100000 so legitimate large monorepos index fully while still keeping a
// high safety ceiling against genuinely pathological trees. Override via
// GRAFEL_WATCH_DIR_CAP.
const DefaultWatchDirCap = 100000

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
// media-library bundle (*.musiclibrary, *.photoslibrary, ...). Delegates to
// the protected-path authority.
func IsMediaLibraryBundle(base string) bool {
	return protectedpath.IsMediaLibraryBundle(base)
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
	return protectedpath.IsWalkProtected(absPath)
}

// isProtectedPathWithHome is the testable core of IsProtectedPath. home is the
// home directory to treat as protected; goos selects the platform gate.
func isProtectedPathWithHome(absPath, home, goos string) (bool, string) {
	return protectedpath.IsWalkProtectedIn(absPath, home, goos)
}
