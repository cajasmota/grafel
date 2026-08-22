// protected.go — macOS TCC-protected-folder guard for the sibling-repo scan
// (v0.1.8 bug: classifying a repo that lives directly under $HOME read the
// home dir and probed each protected sibling for .git, firing permission
// prompts).
//
// ClassifyPath scans the PARENT of the selected repo for "sibling git repos".
// When that parent is $HOME (a repo cloned straight into ~) — or a
// TCC-protected folder like ~/Documents — enumerating it and Stat-ing each
// child's .git reads INTO Desktop/Documents/Downloads and trips a macOS
// permission prompt during normal wizard use. A repo whose parent is the home
// dir has no meaningful "siblings" to offer anyway, so we simply skip the scan
// there.
//
// This file used to own a SECOND protected-path denylist that disagreed with
// the one in internal/daemon/walk. #6548 replaced both with the single
// authority in internal/protectedpath; this is now a thin adapter. The
// authority gates on GOOS at runtime, so the former darwin/!darwin build-tag
// pair is gone — behaviour off darwin is unchanged (every predicate is false).
package detect

import "github.com/cajasmota/grafel/internal/protectedpath"

// isProtectedScanParent reports whether enumerating `parent` for sibling repos
// would read the home dir itself or a macOS TCC-protected folder (or anything
// inside one). ClassifyPath skips the sibling scan when this is true.
func isProtectedScanParent(parent string) bool {
	return protectedpath.IsProtectedScanParent(parent)
}

// isProtectedHomeChild reports whether the dirent `name` under `parent` is a
// macOS TCC-protected folder that must NOT be descended into (ReadDir'd or
// have its .git Stat'd) during classification. It fires ONLY when `parent` IS
// the home directory, so:
//
//   - classifying $HOME skips its Documents/Downloads/Pictures/Music/… children
//     (the batch-prompt bug: childGitRepoNames Stat'd each child/.git and
//     scanPolyglotModules ReadDir'd each child for manifests), while
//   - explicitly classifying a protected folder itself (e.g. ~/Documents) still
//     descends into ITS children — their parent is ~/Documents, not $HOME — so
//     the deliberate single-prompt case keeps working, and
//   - a folder merely named "Documents" elsewhere on disk is unaffected.
func isProtectedHomeChild(parent, name string) bool {
	return protectedpath.IsProtectedHomeChild(parent, name)
}
