// Package watch implements the daemon's reactive fsnotify-driven file
// watcher (Phase B). For each registered repo the watcher subscribes
// recursively to the working tree, applies a coarse skip list to keep
// the watch count bounded, and emits a single coalesced event after a
// per-repo debounce window has elapsed.
//
// The package is intentionally agnostic to what happens when a repo
// settles — it just calls back into a caller-supplied function. The
// scheduler in internal/daemon/sched is the natural consumer.
package watch

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon/walk"
)

// SkipDirs is the static list of directory basenames the watcher never
// recurses into. Source trees pin most of their churn behind these
// names; recursing into them would balloon the inotify watch count
// without producing any signal we care about.
//
// We honour these by basename so any depth matches (e.g. nested
// node_modules under a monorepo). This list now also covers iOS/Android
// build artifacts and prior-tool outputs (issue #805).
var SkipDirs = map[string]struct{}{
	// VCS
	".archigraph": {},
	".git":        {},
	".hg":         {},
	".svn":        {},
	// Agent scratch / linked git worktrees (#3648). Tools like Claude Code
	// keep ephemeral checkouts under .claude/worktrees/, each a full repo
	// tree with its own ~500MB node_modules. These are high-churn (a fresh
	// branch every few minutes) and must never be walked or watched — doing
	// so multiplies the watch set and feeds continuous reindex thrash on the
	// parent repo. node_modules below is already skipped by basename, but
	// skipping .claude outright also drops the worktrees' source trees, which
	// otherwise rely solely on the consuming repo gitignoring .claude/worktrees.
	".claude": {},
	// JS / TS
	"node_modules":  {},
	".next":         {},
	".nuxt":         {},
	"dist":          {},
	"out":           {},
	"coverage":      {},
	".expo":         {},
	".expo-shared":  {},
	".parcel-cache": {},
	".turbo":        {},
	// Python
	"__pycache__":   {},
	".pytest_cache": {},
	".mypy_cache":   {},
	".tox":          {},
	// Python virtualenvs
	".venv": {},
	"venv":  {},
	// Go / Rust / JVM
	"vendor": {},
	"target": {},
	"build":  {},
	// iOS / Xcode / CocoaPods
	"Pods":        {},
	"DerivedData": {},
	"xcuserdata":  {},
	".swiftpm":    {},
	// Android / Gradle
	".gradle":  {},
	"captures": {},
	// Mobile build outputs
	"APK":      {},
	"IPA":      {},
	"Builds":   {},
	"Releases": {},
	// Prior-tool outputs
	"graphify-out":    {},
	"gfleet-out":      {},
	".archigraph-out": {},
	// IDE
	".idea":   {},
	".vscode": {},
	// Generated code (MANIFEST §25, D24)
	"_generated": {},
	// Build / cache (S4 #2154: aggressive filter)
	".cache":   {},
	".vite":    {},
	".esbuild": {},
}

// SkipExts is the suffix list for files we never re-index for. Lock
// files, IDE cache files and the daemon's own outputs would otherwise
// trigger debounce loops.
var SkipExts = map[string]struct{}{
	".lock":  {},
	".tmp":   {},
	".swp":   {},
	".swo":   {},
	".bak":   {},
	".orig":  {},
	".log":   {},
	".pyc":   {},
	".class": {},
	".o":     {},
	".a":     {},
	".so":    {},
	".dylib": {},
	".dll":   {},
	".exe":   {},
}

// ShouldSkipDir reports whether a directory basename is on the skip
// list. It delegates to the canonical walk.IsHardcodedSkip so the
// watcher and the indexer use the identical extended set (issue #805).
func ShouldSkipDir(base string) bool {
	if _, ok := SkipDirs[base]; ok {
		return true
	}
	// Delegate to walk package for suffix-based rules (*.egg-info, etc.)
	return walk.IsHardcodedSkip(base)
}

// ShouldSkipPath reports whether a path event should be dropped before
// it ever reaches the debouncer. We drop:
//   - paths that traverse any SkipDirs basename, and
//   - file extensions on SkipExts (with a special case for vim swap
//     files which look like .foo.swp).
func ShouldSkipPath(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, part := range parts {
		if ShouldSkipDir(part) {
			return true
		}
	}
	ext := strings.ToLower(filepath.Ext(p))
	if _, ok := SkipExts[ext]; ok {
		return true
	}
	// Vim creates `4913` test files and `.foo.swp` style swap files;
	// the latter is caught by .swp above, the former is rare enough to
	// ignore (it lives at most for a few ms).
	return false
}
