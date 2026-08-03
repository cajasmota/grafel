package gitmeta

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
// This is the hot path for grafel_whoami / ResolveCWD (#3325, epic #3648):
// before this cache every whoami call shelled out to git 5–10× (~0.5–2s under
// indexer contention); after it the steady-state call is a single os.Stat.
//
// WHAT THE HEAD-POINTER KEY ACTUALLY PROTECTS (#6079). This comment previously
// claimed "a commit ... always updates <gitdir>/HEAD". That is false, and the
// correction matters per-field:
//
//   - `git commit` ON THE BRANCH YOU ARE ALREADY ON does NOT touch <gitdir>/HEAD.
//     It writes refs/heads/<branch> and appends to logs/HEAD. HEAD is a symbolic
//     ref ("ref: refs/heads/main") and is rewritten only when the BRANCH changes
//     — checkout, switch, detach.
//   - Ref / IsWorktree / TopLevel / GitDir are therefore CORRECTLY protected:
//     each changes exactly when the symbolic ref or the gitdir layout changes,
//     which is exactly when this key changes.
//   - SHA is NOT protected: it advances on every commit, so it can be stale for
//     the whole lifetime of the key.
//
// Callers that need a per-commit-fresh SHA MUST use CaptureCachedFresh, which
// keys on the branch tip as well. The split is deliberate rather than widening
// this key for everyone: daemon.StateDirForRepo consumes only .Ref and is on the
// serving hot path — #6060 exists precisely because it used to fork git per
// request — so making its key commit-sensitive would re-fork Capture after every
// commit to fix a different caller's problem.
//
// Reindex is irrelevant to both keys — git metadata is independent of the graph
// — so neither cache needs wiring to the index-completion signal. When HEAD
// cannot be located (non-git dir) we fall through to a live Capture and do not
// cache, preserving exact prior behaviour for those paths.
var (
	captureMu sync.Mutex
	// captureCache is keyed on the HEAD pointer alone: Ref-correct, SHA-stale.
	captureCache = map[string]captureEntry{}
	// captureFreshCache is keyed on the HEAD pointer AND the commit token, so
	// every field including SHA is fresh. Kept in a SEPARATE map so a hit stored
	// by the cheap variant can never be served to a caller that asked for the
	// strict one.
	captureFreshCache = map[string]captureFreshEntry{}
)

type captureEntry struct {
	info     Info
	headStat headKey
}

type captureFreshEntry struct {
	info        Info
	headStat    headKey
	commitToken string
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

// resolveGitDirs returns repoPath's own git directory and the COMMON git
// directory that holds its refs, without invoking git. For a normal checkout
// both are "<repo>/.git". For a linked worktree, "<repo>/.git" is a gitdir file
// naming the worktree's private dir, and that dir's "commondir" file names the
// shared one (relative paths are resolved against the private dir). ok is false
// when repoPath is not a resolvable checkout.
func resolveGitDirs(repoPath string) (gitDir, commonDir string, ok bool) {
	dotGit := filepath.Join(repoPath, ".git")
	fi, err := os.Stat(dotGit)
	if err != nil {
		return "", "", false
	}
	if fi.IsDir() {
		return dotGit, dotGit, true
	}
	gitDir = readGitdirFile(dotGit)
	if gitDir == "" {
		return "", "", false
	}
	commonDir = gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		if cd := strings.TrimSpace(string(data)); cd != "" {
			if filepath.IsAbs(cd) {
				commonDir = filepath.Clean(cd)
			} else {
				commonDir = filepath.Clean(filepath.Join(gitDir, cd))
			}
		}
	}
	return gitDir, commonDir, true
}

// statToken renders a path's existence + mtime + size as a comparable token.
// A missing file yields a distinct "absent" token rather than an error, because
// absence is itself a meaningful, changeable state (packed-refs appears when
// refs are packed).
//
// Use contentToken instead for anything whose SIZE is constant, where mtime is
// the only discriminating component and two writes within one filesystem
// timestamp tick would collide.
func statToken(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fi.ModTime().UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatInt(fi.Size(), 10)
}

// contentToken renders a small file's exact CONTENT as a comparable token, with
// the same "-" sentinel for absence. Used for the loose branch tip, which is a
// fixed-width 41-byte file ("<40-hex sha>\n"): its size never changes, so a
// stat-based token would discriminate on mtime alone and two commits landing in
// one filesystem timestamp tick would share a token and serve a stale SHA. The
// tip's bytes ARE the commit id, so comparing them is exact — and at 41 bytes it
// costs the same open/read/close the HEAD pointer already pays.
func contentToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "-"
	}
	return strings.TrimSpace(string(data))
}

// commitToken identifies the state of the COMMIT repoPath's HEAD resolves to,
// without invoking git. It changes on every commit, and is the missing half of
// the HEAD-pointer key (#6079).
//
// Components, all cheap file operations:
//
//   - HEAD's raw bytes. Covers a branch switch and a detach; for a DETACHED
//     HEAD these bytes ARE the commit id, so nothing further is needed.
//   - the loose branch-tip file <commonDir>/refs/heads/<branch>, by CONTENT.
//     Its bytes are the commit id, so this component is exact. It is read rather
//     than stat'd deliberately: a loose ref is a fixed-width 41-byte file, so its
//     size never varies and a stat token would rest on mtime alone — two commits
//     inside one filesystem timestamp tick would then share a token and serve a
//     stale SHA. Absence is part of the token too (a packed ref that a commit
//     un-packs flips this component).
//   - <commonDir>/packed-refs, by stat: the branch tip may live there instead.
//     Stat is adequate here because that file's size does vary with its
//     contents, and it is rewritten whenever refs are packed or a packed ref
//     moves.
//
// ok is false only when the repo's git dirs or HEAD cannot be read at all, in
// which case CaptureCachedFresh declines to cache and runs a live Capture —
// failing toward freshness rather than serving an unverifiable memo.
func commitToken(repoPath string) (string, bool) {
	gitDir, commonDir, ok := resolveGitDirs(repoPath)
	if !ok {
		return "", false
	}
	headBytes, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(headBytes))

	var b strings.Builder
	b.WriteString(head)
	b.WriteByte(0)
	if rest, isSym := strings.CutPrefix(head, "ref:"); isSym {
		ref := strings.TrimSpace(rest)
		// Reject anything that could escape commonDir. A hostile/corrupt HEAD is
		// not a reason to build a path out of it; treat it as unresolvable and
		// let the caller run uncached.
		if ref == "" || strings.Contains(ref, "..") || filepath.IsAbs(ref) {
			return "", false
		}
		b.WriteString(contentToken(filepath.Join(commonDir, filepath.FromSlash(ref))))
		b.WriteByte(0)
	}
	b.WriteString(statToken(filepath.Join(commonDir, "packed-refs")))
	return b.String(), true
}

// CaptureCachedFresh returns the same value as Capture, memoized on a key that
// covers BOTH the HEAD pointer and the commit it resolves to. Unlike
// CaptureCached it is correct for Info.SHA across a same-branch commit (#6079),
// at the cost of one extra small file read and two os.Stat calls per call — no
// git subprocess on the steady-state path.
//
// Use this wherever a per-commit-accurate SHA is observable (ResolveCWD,
// grafel_whoami's indexed_sha). Use CaptureCached when only .Ref / .IsWorktree /
// .TopLevel matter and the call is hot (daemon.StateDirForRepo).
//
// When the repo's git state cannot be read (non-git directory, unreadable HEAD)
// it runs a live Capture and does not cache — identical observable behaviour to
// Capture for those inputs.
func CaptureCachedFresh(repoPath string) Info {
	if repoPath == "" {
		return Capture(repoPath)
	}
	key, ok := headPointerKey(repoPath)
	if !ok {
		return Capture(repoPath)
	}
	tok, ok := commitToken(repoPath)
	if !ok {
		// Cannot verify commit freshness — never serve a possibly-stale memo.
		return Capture(repoPath)
	}

	captureMu.Lock()
	if ent, hit := captureFreshCache[repoPath]; hit && ent.headStat == key && ent.commitToken == tok {
		captureMu.Unlock()
		return ent.info
	}
	captureMu.Unlock()

	info := Capture(repoPath)

	captureMu.Lock()
	captureFreshCache[repoPath] = captureFreshEntry{info: info, headStat: key, commitToken: tok}
	captureMu.Unlock()
	return info
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

// CaptureCached returns Capture's value memoized on the repo's HEAD-pointer
// mtime+size, eliminating the per-call git subprocess cost on the hot
// StateDirForRepo path. The cache self-invalidates whenever HEAD is REWRITTEN —
// checkout, branch-switch, detach.
//
// FRESHNESS CONTRACT (#6079), read this before adding a caller:
//
//	Ref, IsWorktree, TopLevel, GitDir — fresh. They change exactly when the
//	symbolic ref or the gitdir layout changes, which is exactly when the key
//	changes.
//	SHA — MAY BE STALE. A same-branch commit does not rewrite HEAD, so the key
//	is unchanged and the previous commit's SHA keeps being served.
//
// If you need SHA, call CaptureCachedFresh. Do not "fix" a stale SHA by
// widening this key: the hot caller here reads only .Ref.
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

// HeadToken returns an opaque token identifying the CURRENT state of repoPath's
// HEAD pointer, and whether one could be determined. The token changes whenever
// HEAD is rewritten — commit, checkout, branch-switch — and is otherwise stable,
// which makes it a pure-os.Stat invalidation signal for anything a caller
// derived from the repo's current ref.
//
// It is the same key CaptureCached memoizes on, exported (#6060) so callers that
// cache a DERIVED value (e.g. the resolved per-ref state directory) can
// invalidate on the same event without paying a Capture — including for
// non-git directories, where Capture is a wasted fork that CaptureCached cannot
// memoize. ok=false means "not a resolvable git checkout"; such a path has no
// ref to move, so a caller may treat its derived value as permanently valid.
func HeadToken(repoPath string) (string, bool) {
	if repoPath == "" {
		return "", false
	}
	key, ok := headPointerKey(repoPath)
	if !ok {
		return "", false
	}
	return key.path + "\x00" + key.mtime.UTC().Format(time.RFC3339Nano) + "\x00" + strconv.FormatInt(key.size, 10), true
}

// resetCaptureCacheForTest clears the memo. Test-only.
func resetCaptureCacheForTest() {
	captureMu.Lock()
	captureCache = map[string]captureEntry{}
	captureFreshCache = map[string]captureFreshEntry{}
	captureMu.Unlock()
}
