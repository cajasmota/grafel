package gitmeta

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// captureCache memoizes Capture results to avoid the ~5 git subprocesses per
// call (~80ms+ each under daemon load). git HEAD metadata — ref, SHA, and
// worktree status — only changes when HEAD moves (commit, checkout,
// branch-switch), and every such event rewrites the on-disk HEAD pointer. We
// therefore key the cache on (repoPath, HEAD-pointer mtime): a cache hit is
// valid as long as HEAD has not been rewritten since the entry was captured.
//
// This is the hot path for archigraph_whoami / ResolveCWD (#3325, epic #3648):
// before this cache every whoami call shelled out to git 5–10× (~0.5–2s under
// indexer contention); after it the steady-state call is a single os.Stat.
//
// Correctness: the only inputs that change Capture's output are the HEAD ref,
// the commit it points at, and the gitdir/commondir relationship. A commit or
// checkout always updates <gitdir>/HEAD (its mtime advances); a branch-switch
// likewise. Reindex is irrelevant — git metadata is independent of the graph —
// so this cache does not need wiring to the index-completion signal. When HEAD
// cannot be located (non-git dir) we fall through to a live Capture and do not
// cache, preserving exact prior behaviour for those paths.
var (
	captureMu    sync.Mutex
	captureCache = map[string]captureEntry{}
)

type captureEntry struct {
	info     Info
	headStat headKey
}

// headKey identifies the state of a repo's HEAD pointer. A zero value means
// "HEAD could not be stat'd" and is treated as a cache miss (never cached) so
// that ambiguous states always re-run the live Capture.
type headKey struct {
	path  string
	mtime time.Time
	size  int64
}

// headPointerKey locates the HEAD file governing repoPath and returns a key
// describing its current state. For a normal checkout this is
// "<repo>/.git/HEAD"; for a linked worktree "<repo>/.git" is a gitdir file
// pointing at the worktree's private git dir whose HEAD is what changes on
// checkout. We resolve both without invoking git: stat the worktree's own HEAD
// when discoverable, else the .git entry itself (whose mtime advances on
// checkout in both layouts).
func headPointerKey(repoPath string) (headKey, bool) {
	dotGit := filepath.Join(repoPath, ".git")
	fi, err := os.Stat(dotGit)
	if err != nil {
		return headKey{}, false
	}

	if fi.IsDir() {
		// Normal checkout: .git/HEAD moves on every commit/checkout.
		head := filepath.Join(dotGit, "HEAD")
		if hi, err := os.Stat(head); err == nil {
			return headKey{path: head, mtime: hi.ModTime(), size: hi.Size()}, true
		}
		// Bare-ish / unusual layout: fall back to the .git dir mtime.
		return headKey{path: dotGit, mtime: fi.ModTime(), size: fi.Size()}, true
	}

	// Linked worktree: .git is a gitdir file. Resolve the private gitdir and
	// stat its HEAD; the gitdir-file's own mtime is a safe fallback because a
	// checkout in a worktree rewrites that worktree's private HEAD.
	if gd := readGitdirFile(dotGit); gd != "" {
		head := filepath.Join(gd, "HEAD")
		if hi, err := os.Stat(head); err == nil {
			return headKey{path: head, mtime: hi.ModTime(), size: hi.Size()}, true
		}
	}
	return headKey{path: dotGit, mtime: fi.ModTime(), size: fi.Size()}, true
}

// readGitdirFile parses a worktree ".git" gitdir-file ("gitdir: <path>\n") and
// returns the referenced directory, or "" on any error.
func readGitdirFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	const prefix = "gitdir:"
	s := string(data)
	i := indexOf(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	// Trim leading spaces and trailing whitespace/newline.
	start := 0
	for start < len(rest) && (rest[start] == ' ' || rest[start] == '\t') {
		start++
	}
	end := len(rest)
	for end > start && (rest[end-1] == '\n' || rest[end-1] == '\r' || rest[end-1] == ' ' || rest[end-1] == '\t') {
		end--
	}
	return rest[start:end]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// CaptureCached returns the same value as Capture but memoizes the result
// keyed on the repo's HEAD-pointer mtime, eliminating the per-call git
// subprocess cost on the hot whoami / ResolveCWD path. The cache self-
// invalidates whenever HEAD is rewritten (commit / checkout / branch-switch).
//
// When HEAD cannot be located (non-git directory) it runs a live Capture and
// does not cache — identical observable behaviour to Capture for those inputs.
func CaptureCached(repoPath string) Info {
	if repoPath == "" {
		return Capture(repoPath)
	}
	key, ok := headPointerKey(repoPath)
	if !ok {
		// Not a resolvable git checkout: behave exactly like Capture.
		return Capture(repoPath)
	}

	captureMu.Lock()
	if ent, hit := captureCache[repoPath]; hit && ent.headStat == key {
		captureMu.Unlock()
		return ent.info
	}
	captureMu.Unlock()

	info := Capture(repoPath)

	captureMu.Lock()
	captureCache[repoPath] = captureEntry{info: info, headStat: key}
	captureMu.Unlock()
	return info
}

// resetCaptureCacheForTest clears the memo. Test-only.
func resetCaptureCacheForTest() {
	captureMu.Lock()
	captureCache = map[string]captureEntry{}
	captureMu.Unlock()
}
