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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cajasmota/grafel/internal/daemon/walk"
)

// envExtraSkipDirs holds additional directory basenames parsed once from
// the GRAFEL_WATCH_EXTRA_SKIP_DIRS environment variable (comma-separated).
// This is the global, ops-tunable escape hatch for sites whose build
// tooling writes to a dir not covered by the built-in SkipDirs set (#5392).
var (
	envExtraSkipOnce sync.Once
	envExtraSkipDirs map[string]struct{}
)

func extraSkipDirsFromEnv() map[string]struct{} {
	envExtraSkipOnce.Do(func() {
		envExtraSkipDirs = make(map[string]struct{})
		for _, d := range strings.Split(os.Getenv("GRAFEL_WATCH_EXTRA_SKIP_DIRS"), ",") {
			if d = strings.TrimSpace(d); d != "" {
				envExtraSkipDirs[d] = struct{}{}
			}
		}
	})
	return envExtraSkipDirs
}

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
	".grafel": {},
	".git":    {},
	".hg":     {},
	".svn":    {},
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
	// Mobile build outputs (#5392: an Android AAB/gradle build under
	// acme-mobile churned these dirs and tripped a continuous reindex
	// loop → 20GB heap thrash. AAB/ in particular is the project's
	// Android App Bundle output dir).
	"APK":      {},
	"AAB":      {},
	"IPA":      {},
	"Builds":   {},
	"Releases": {},
	// Flutter / Dart
	".dart_tool": {},
	// Prior-tool outputs
	"graphify-out": {},
	"gfleet-out":   {},
	".grafel-out":  {},
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
	// Mobile build artifacts (#5392). These are large binary outputs of
	// an app build; writing them must never trip a reindex.
	".aab": {},
	".apk": {},
	".ipa": {},
	".aar": {},
}

// SkipBaseGlobs is the list of basename glob patterns whose matches we
// never re-index for. Unlike SkipExts (which keys on the final
// extension only), these match anywhere in the basename so multi-dot
// generated files like `schema.generated.ts` or `api.g.dart` are
// dropped at the watcher event boundary (#5392).
var SkipBaseGlobs = []string{
	"*.generated.*", // foo.generated.ts, foo.generated.go, ...
	"*.gen.*",       // foo.gen.go
	"*.g.dart",      // Flutter codegen
	"*.freezed.dart",
	"*.pb.go",   // protobuf
	"*.pb.dart", // protobuf
}

// ShouldSkipDir reports whether a directory basename is on the skip
// list. It delegates to the canonical walk.IsHardcodedSkip so the
// watcher and the indexer use the identical extended set (issue #805).
func ShouldSkipDir(base string) bool {
	if _, ok := SkipDirs[base]; ok {
		return true
	}
	// Ops-tunable global escape hatch (#5392).
	if _, ok := extraSkipDirsFromEnv()[base]; ok {
		return true
	}
	// TCC guard (#5296): never recurse into macOS media-library bundles
	// (*.musiclibrary, *.photoslibrary, ...) — descending trips the privacy
	// prompt. Checked by basename so it matches at any depth.
	if walk.IsMediaLibraryBundle(base) {
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
	// Multi-dot generated-file globs (e.g. *.generated.*, *.g.dart).
	base := filepath.Base(filepath.ToSlash(p))
	for _, g := range SkipBaseGlobs {
		if ok, _ := filepath.Match(g, base); ok {
			return true
		}
	}
	// Vim creates `4913` test files and `.foo.swp` style swap files;
	// the latter is caught by .swp above, the former is rare enough to
	// ignore (it lives at most for a few ms).
	return false
}

// ShouldSkipPathForRepo is the repo-aware event-boundary filter. It is a
// superset of ShouldSkipPath: in addition to the static SkipDirs /
// SkipExts / SkipBaseGlobs rules it consults the repo's .gitignore (and
// per-repo .grafel/watch.json) so that a file event under a gitignored
// path NEVER triggers a reindex (#5392).
//
// This matters because the static lists can only cover *well-known*
// artifact names. A repo may gitignore arbitrary build/output dirs
// (e.g. AAB/, app/build/, *.aab) that the watcher's directory-level
// subscription already declines to watch — but events can still surface
// for them via a watched parent directory (e.g. a Create event for a new
// gitignored subdir, or a file written at the repo root). Honouring
// .gitignore at the event boundary drops that churn cheaply.
//
// repoPath must be the absolute repo root; p must be inside it. When
// repoPath is empty or p is not under it, this degrades to ShouldSkipPath.
func ShouldSkipPathForRepo(repoPath, p string) bool {
	if ShouldSkipPath(p) {
		return true
	}
	if repoPath == "" {
		return false
	}
	rel, err := filepath.Rel(repoPath, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return false
	}
	s := loadRepoIgnoreState(repoPath)
	// Walk the path and each ancestor directory: a file is ignored if it
	// OR any ancestor dir matches .gitignore (a `build/` rule ignores the
	// dir, so everything beneath it is ignored too) or a per-repo
	// exclude_dirs basename.
	segs := strings.Split(rel, "/")
	for i := 1; i <= len(segs); i++ {
		sub := strings.Join(segs[:i], "/")
		if _, ok := s.extraSkip[segs[i-1]]; ok {
			return true
		}
		if skip, _ := s.gitignore.MatchDir(sub); skip {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The per-directory verdict, in ONE place (#6932 arm B)
// ---------------------------------------------------------------------------
//
// subscribeRepo decides, directory by directory, what the watch set is. The
// inotify budget probe has to project the cost of exactly that set without
// subscribing it. Two walks asking the question two ways is how a projection
// drifts from the thing it claims to project, so the layers below are defined
// once and both walks call them.
//
// What is NOT in here, and why: the TCC protected-path guard and the watch-dir
// cap. The first applies to the repo root as well as to its subdirectories and
// the second is stateful (it depends on how many directories the walk has
// already taken), so both stay at their call sites, applied in the same order
// by both walks. The equality of the two answers is not asserted by
// construction — it is measured, by
// TestProjectInotifyBudget_MatchesWhatSubscriptionActuallyTakes.

// watchSkipKind names which layer refused a directory. The distinction exists
// because the subscription walk logs the three cases differently.
type watchSkipKind int

const (
	// watchSkipNone: the walk takes a watch on this directory.
	watchSkipNone watchSkipKind = iota
	// watchSkipExcluded: hard-coded SkipDirs / walk.IsHardcodedSkip, or the
	// per-instance ExcludeDirs set.
	watchSkipExcluded
	// watchSkipIgnored: .gitignore plus the per-repo .grafel/watch.json.
	watchSkipIgnored
)

// watchDirSkip applies the subscription walk's per-directory skip layers to one
// directory p under repo root abs. The repo root itself is never skipped by
// these layers, matching the walk it is extracted from.
func watchDirSkip(abs, p string, extraSkip map[string]struct{}) (watchSkipKind, string) {
	if p == abs {
		return watchSkipNone, ""
	}
	base := filepath.Base(p)
	// Layer 1 + 2: hard-coded + per-instance excludes.
	if ShouldSkipDir(base) {
		return watchSkipExcluded, "skip list"
	}
	if _, ok := extraSkip[base]; ok {
		return watchSkipExcluded, "instance exclude"
	}
	// Layer 3: .gitignore + per-repo watch.json.
	relPath, relErr := filepath.Rel(abs, p)
	if relErr != nil {
		return watchSkipNone, ""
	}
	relPath = filepath.ToSlash(relPath)
	if skip, reason := ShouldSkipDirGitignore(abs, p, relPath); skip {
		return watchSkipIgnored, reason
	}
	return watchSkipNone, ""
}
