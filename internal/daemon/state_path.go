// Per-repo state path resolution for issue #745.
//
// Background: ADR-0007 co-locates per-repo state in `<repo>/.grafel/`.
// That default is preserved for ordinary user installs. When multiple
// agents run with isolated daemons via GRAFEL_DAEMON_ROOT, the
// daemon socket and registry are already isolated, but the per-repo
// state directory is shared — two agents indexing the same fixture
// race on `<repo>/.grafel/graph.json` and corrupt each other's
// results.
//
// When GRAFEL_DAEMON_ROOT is set, StateDirForRepo returns a
// daemon-private state directory at
//
//	$GRAFEL_DAEMON_ROOT/state/<sha256(abs_repo_path)[:16]>/
//
// instead of `<repo>/.grafel/`. The fixture's own `.grafel/`
// directory is never touched by the daemon under this mode, so a
// pristine read-only corpus stays pristine even across many parallel
// agents.
//
// Identifier choice: sha256 of the absolute repo path, first 16 hex
// chars. Reasons:
//   - Deterministic (same input → same output across processes & hosts).
//   - Filesystem-safe (16 hex chars, no separators or shell metachars).
//   - Collision-resistant (2^64 namespace; far above any realistic
//     fixture count on a single host).
//   - Opaque (does not leak repo paths into shared tmp).
//
// Group-level config that lives co-located by design (group.json
// manifests written by the wizard) is NOT routed through this helper —
// it stays at `<repo>/.grafel/group.json` so it can be discovered
// by walking up from a CWD regardless of which daemon is running.
//
// Per-ref layout (PH1a of epic #2087 / issue #2089):
// Graph artifacts are now stored under a per-branch sub-directory:
//
//	<store>/<slug>-<hash>/refs/<ref-safe>/graph.fb
//
// where <ref-safe> is the branch name with "/" replaced by "%2F"
// (URL-percent encoding, 2-way round-trip). The sentinel "_unknown" is
// used when no ref is available (detached HEAD, pre-metadata graphs).
//
// StateDirForRepo remains the single entry point for existing callers;
// it reads the current HEAD ref via gitmeta.Capture and delegates to
// StateDirForRepoRef.
package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// canonicalCache caches (inputPath → canonicalPath) resolutions so that
// every daemon startup pays the os.ReadDir cost at most once per unique
// input path. Paths do not change casing during a daemon's lifetime.
var canonicalCache sync.Map // map[string]string

// readDirFunc is the directory-read primitive used by canonicalizePath.
// It is a package var so tests can inject a slow/blocking implementation
// to exercise the timeout guard without a real stuck filesystem.
var readDirFunc = os.ReadDir

// defaultCanonicalizeTimeout bounds the per-segment os.ReadDir call in
// canonicalizePath. On case-insensitive filesystems this read is
// effectively instant; the timeout only ever fires when an ancestor
// directory's FS call hangs (an iCloud/Spotlight/TCC stall, a slow or
// unresponsive mount, or a launchd-context permission stall). When it
// fires we degrade to preserving the input casing rather than blocking
// the daemon's startup forever (#5330).
const defaultCanonicalizeTimeout = 3 * time.Second

// canonicalizeTimeout returns the per-segment ReadDir timeout, honouring
// the GRAFEL_CANONICALIZE_TIMEOUT_MS override. A zero, negative, or
// unparseable value falls back to defaultCanonicalizeTimeout.
func canonicalizeTimeout() time.Duration {
	if v := os.Getenv("GRAFEL_CANONICALIZE_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultCanonicalizeTimeout
}

// readDirBounded runs readDirFunc(dir) under a timeout. It returns the
// entries (or read error) when the read completes in time, or a third
// return of false when the read did not finish before the deadline.
//
// The read runs in its own goroutine writing to a 1-deep buffered
// channel, so on a genuinely wedged filesystem that goroutine is simply
// abandoned: it holds no lock, blocks nothing else, and its eventual
// (or never) send cannot leak memory beyond the single buffered slot.
// This is what lets daemon startup proceed instead of deadlocking on a
// slow FS (#5330).
func readDirBounded(dir string, timeout time.Duration) (entries []os.DirEntry, err error, ok bool) {
	type result struct {
		entries []os.DirEntry
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		e, rdErr := readDirFunc(dir)
		ch <- result{e, rdErr}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.entries, r.err, true
	case <-timer.C:
		return nil, nil, false
	}
}

// canonicalizePath returns the path with the actual on-disk casing of
// each component. On case-insensitive filesystems (APFS, NTFS) two
// inputs that differ only in casing refer to the same directory but
// sha256(path) would produce different hashes. canonicalizePath walks
// each segment top-down via os.ReadDir and substitutes the real entry
// name so that "UpVate" and "upvate" both canonicalize to whichever
// casing the filesystem holds (e.g. "UpVate").
//
// If a segment is not found on disk (path may be virtual or not yet
// created) the input casing is preserved for that segment and all
// subsequent ones.
//
// The result is cached in a sync.Map keyed by the input path. This is
// safe because path casing never changes while the daemon runs.
func canonicalizePath(absPath string) string {
	if absPath == "" {
		return absPath
	}
	// Fast path: already cached.
	if v, ok := canonicalCache.Load(absPath); ok {
		return v.(string)
	}

	// Split into volume (e.g. "C:" on Windows, "" on Unix) + segments.
	vol := filepath.VolumeName(absPath)
	rest := absPath[len(vol):]

	// Trim leading separator so Split doesn't produce empty leading element.
	rest = strings.TrimLeft(rest, string(filepath.Separator))
	segments := strings.Split(rest, string(filepath.Separator))

	timeout := canonicalizeTimeout()
	canonical := vol + string(filepath.Separator)
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		// Try to find the real on-disk name for this segment, bounded by a
		// timeout. A single ancestor whose os.ReadDir hangs (iCloud/Spotlight/
		// TCC stall, slow mount, launchd-context permission stall) must never
		// deadlock daemon startup (#5330) — on timeout we take the same
		// degrade path as a read error below: preserve the input casing for
		// this and all remaining segments.
		entries, err, completed := readDirBounded(canonical, timeout)
		if !completed {
			slog.Warn("canonicalizePath: os.ReadDir timed out; preserving input casing",
				"dir", canonical, "timeout", timeout, "path", absPath)
			canonical = filepath.Join(canonical, seg)
			continue
		}
		if err != nil {
			// Directory doesn't exist or isn't readable; preserve input
			// casing for this segment and all remaining segments.
			canonical = filepath.Join(canonical, seg)
			continue
		}
		found := false
		for _, e := range entries {
			if equalFold(e.Name(), seg) {
				canonical = filepath.Join(canonical, e.Name())
				found = true
				break
			}
		}
		if !found {
			// Segment not present; preserve input casing.
			canonical = filepath.Join(canonical, seg)
		}
	}

	canonicalCache.Store(absPath, canonical)
	return canonical
}

// equalFold reports whether a and b are equal under Unicode case-folding.
// filepath.Match on case-insensitive OSes uses the same folding; we
// replicate it here to avoid depending on the OS for the comparison.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		// Fast reject: UTF-8 lengths differ — can't be equal under fold
		// unless multi-byte fold produces same length, which is not the
		// case for the ASCII range we care about.  For non-ASCII we fall
		// through to the rune loop.
	}
	for len(a) > 0 && len(b) > 0 {
		ra, sza := utf8.DecodeRuneInString(a)
		rb, szb := utf8.DecodeRuneInString(b)
		if ra != rb {
			// Try simple ASCII fold first (most common case).
			if ra >= 'A' && ra <= 'Z' {
				ra += 'a' - 'A'
			}
			if rb >= 'A' && rb <= 'Z' {
				rb += 'a' - 'A'
			}
			if ra != rb {
				return false
			}
		}
		a = a[sza:]
		b = b[szb:]
	}
	return len(a) == 0 && len(b) == 0
}

// repoStateHash returns a deterministic, path-safe identifier for a
// repo path. The path is canonicalized via canonicalizePath before
// hashing so that case variants on case-insensitive filesystems (APFS,
// NTFS) always produce the same hash — fixing the case-collision store
// duplicate bug (#2086).
//
// Callers MUST pass an absolute, lexically-clean path
// (filepath.Abs + filepath.Clean).
func repoStateHash(absRepoPath string) string {
	canonical := canonicalizePath(absRepoPath)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:8]) // 16 hex chars
}

// homeDir resolves the grafel home directory, honouring the
// GRAFEL_HOME override (matching registry.HomeDir) and falling
// back to ~/.grafel. Kept dependency-light so this hot-path
// helper does not pull in the registry package.
func homeDir() string {
	if override := os.Getenv("GRAFEL_HOME"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Last-ditch fallback so we never write into a repo by accident.
		return filepath.Join(os.TempDir(), ".grafel")
	}
	return filepath.Join(home, ".grafel")
}

// StoreDir returns the root of the daemon's external graph store —
// the single source of truth for where generated graph artifacts live
// when no isolated GRAFEL_DAEMON_ROOT is in effect.
//
//	$GRAFEL_HOME (or ~/.grafel)/store
//
// Issue #1626: graph artifacts (graph.fb, graph.json, enrichments,
// links, metadata) are NEVER written into the repo working tree any
// more — they live under the store, keyed by repo. Keeping them out of
// the tree (a) stops them polluting user repos and (b) breaks the
// fb-vs-json mtime-drift reindex loop, since the watcher can no longer
// observe its own output.
func StoreDir() string {
	return filepath.Join(homeDir(), "store")
}

var unsafeSlugChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// repoSlug derives a short, human-readable, path-safe label from a repo
// path so the store layout is browsable (e.g. "my-service-1a2b3c4d…").
// The trailing hash guarantees uniqueness even when two repos share a
// basename.
func repoSlug(absRepoPath string) string {
	base := filepath.Base(absRepoPath)
	base = unsafeSlugChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-._")
	if base == "" {
		base = "repo"
	}
	if len(base) > 48 {
		base = base[:48]
	}
	return base + "-" + repoStateHash(absRepoPath)
}

// RefSafeEncode converts a git ref name (branch/tag) into a
// filesystem-safe directory name component. The "/" separator is
// percent-encoded as "%2F" so the round-trip is deterministic and
// reversible. All other characters that are legal in git ref names are
// also legal in directory names on Linux/macOS/Windows, so no further
// encoding is needed.
//
// Examples:
//
//	"main"          → "main"
//	"feat/foo-bar"  → "feat%2Ffoo-bar"
//	""              → "_unknown"
func RefSafeEncode(ref string) string {
	if ref == "" {
		return "_unknown"
	}
	return strings.ReplaceAll(ref, "/", "%2F")
}

// RefSafeDecode reverses RefSafeEncode. "_unknown" is returned as "".
func RefSafeDecode(safe string) string {
	if safe == "_unknown" {
		return ""
	}
	return strings.ReplaceAll(safe, "%2F", "/")
}

// repoBaseDir returns the per-repo slot in the store (without the
// refs/<ref-safe>/ suffix). This is the top-level directory created for
// the repo — it holds the refs/ sub-tree and legacy flat artifacts
// during migration.
func repoBaseDir(absRepoPath string) string {
	if root := os.Getenv(EnvRoot); root != "" {
		return filepath.Join(root, "state", repoStateHash(absRepoPath))
	}
	return filepath.Join(StoreDir(), repoSlug(absRepoPath))
}

// RepoBaseDir is the exported wrapper around repoBaseDir: it returns the
// top-level store root directory for repoPath (the `<slug>-<hash>/` slot, or
// `state/<hash>/` under GRAFEL_DAEMON_ROOT). The path is absolutised + cleaned
// first so callers get the same root the writer used. This is the forward
// (path → root) half of the store-root ↔ source-path mapping the orphan-root
// GC relies on (#5263): the hash is one-way, so the ONLY authoritative way to
// attribute a root to a path is to compute the expected root for every KNOWN
// live source path and treat the rest as undeterminable.
func RepoBaseDir(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	return repoBaseDir(filepath.Clean(abs))
}

// StoreRootBase returns the directory that DIRECTLY contains the top-level
// store roots — `<store>` normally, or `<GRAFEL_DAEMON_ROOT>/state` under an
// isolated daemon root. Its immediate sub-directories are the per-repo roots
// returned by RepoBaseDir. The orphan-root GC enumerates this directory.
func StoreRootBase() string {
	if root := os.Getenv(EnvRoot); root != "" {
		return filepath.Join(root, "state")
	}
	return StoreDir()
}

// StateDirForRepoRef returns the per-ref state directory for repoPath
// and a specific git ref:
//
//	<store>/<slug>-<hash>/refs/<ref-safe>/
//
// When ref is empty the sentinel directory "refs/_unknown/" is used.
// The directory is NOT created here; callers that write should
// os.MkdirAll the returned path.
func StateDirForRepoRef(repoPath, ref string) string {
	if repoPath == "" {
		return ""
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	abs = filepath.Clean(abs)
	return filepath.Join(repoBaseDir(abs), "refs", RefSafeEncode(ref))
}

// StateDirForRepo returns the directory that holds per-repo state
// (graph.fb, graph.json, repair.json, enrichment-*.json, links, …) for
// repoPath.
//
// Resolution (issue #1626 + PH1a #2089):
//   - The current HEAD ref is captured via gitmeta.Capture so the path
//     resolves to the per-branch sub-directory introduced by PH1a.
//   - When GRAFEL_DAEMON_ROOT is set (isolated daemons, parallel
//     agents, tests): `$GRAFEL_DAEMON_ROOT/state/<hash>/refs/<ref>/`.
//   - Otherwise: `$GRAFEL_HOME (or ~/.grafel)/store/<slug>-<hash>/refs/<ref>/`.
//
// Graph artifacts are NO LONGER written into `<repo>/.grafel/`.
// Pre-existing in-repo state is relocated transparently by
// MigrateInRepoState (called from the load path). Pre-PH1a flat stores
// are relocated into the per-ref sub-directory by MigrateToRefStore
// (called from daemon startup).
//
// The directory is NOT created here; callers that write should
// os.MkdirAll the returned path.
func StateDirForRepo(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	meta := gitmeta.Capture(repoPath)
	return StateDirForRepoRef(repoPath, meta.Ref)
}

// LegacyInRepoStateDir returns the historical co-located state directory
// `<repo>/.grafel/`. Used only by the migration path to find and
// relocate pre-#1626 artifacts. New code MUST use StateDirForRepo.
func LegacyInRepoStateDir(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	return filepath.Join(repoPath, ".grafel")
}

// GraphPathForRepo is a convenience wrapper that returns the
// canonical graph.json path inside the per-repo state directory.
func GraphPathForRepo(repoPath string) string {
	return filepath.Join(StateDirForRepo(repoPath), "graph.json")
}

// GraphFBPathForRepo returns the path to graph.fb (FlatBuffers binary format)
// inside the per-repo state directory.
func GraphFBPathForRepo(repoPath string) string {
	return filepath.Join(StateDirForRepo(repoPath), "graph.fb")
}

// findGraphFileInDir checks dir for graph.fb / graph.json and returns the
// path + modtime of the newest one. Returns ("", 0) if neither exists.
func findGraphFileInDir(dir string) (path string, modtime int64) {
	fbPath := filepath.Join(dir, "graph.fb")
	jsonPath := filepath.Join(dir, "graph.json")

	fbInfo, fbErr := os.Stat(fbPath)
	jsonInfo, jsonErr := os.Stat(jsonPath)

	if fbErr == nil {
		fbMtime := fbInfo.ModTime().UnixNano()
		if jsonErr == nil {
			jsonMtime := jsonInfo.ModTime().UnixNano()
			if fbMtime >= jsonMtime {
				return fbPath, fbMtime
			}
			return jsonPath, jsonMtime
		}
		return fbPath, fbMtime
	}
	if jsonErr == nil {
		return jsonPath, jsonInfo.ModTime().UnixNano()
	}
	return "", 0
}

// FindGraphFile checks for the newest graph file (graph.fb preferred over
// graph.json) for repoPath and returns its path and modification time.
// Returns ("", 0) if neither file exists. The returned modtime is in
// nanoseconds since epoch.
//
// PH1a: checks the per-ref directory (StateDirForRepo → StateDirForRepoRef).
func FindGraphFile(repoPath string) (path string, modtime int64) {
	stateDir := StateDirForRepo(repoPath)
	return findGraphFileInDir(stateDir)
}

// FindGraphFileAnyRef resolves a queryable graph file for repoPath, preferring
// the current HEAD ref's per-ref directory but falling back to the newest
// graph.fb/graph.json found under ANY indexed ref for the repo.
//
// Why this exists (#3648): a group registered via `group add --index` is
// indexed ONCE, at the repo's HEAD ref at that moment, and — unless watchers
// were installed (they default to OFF for `group add`, ON for the interactive
// wizard) — nothing reindexes when HEAD subsequently moves. When the MCP server
// later resolves the per-ref state dir from the *current* HEAD ref it lands on
// an empty (never-indexed) ref directory, so FindGraphFile returns "" and the
// repo's Doc stays nil. Every repo-scoped tool (find/inspect/expand/…) then
// reports "no repos loaded for this group" even though a fully-indexed graph
// exists one ref-directory over. The wizard/install path avoids this only
// incidentally, because its watchers keep the current-ref dir fresh.
//
// Resolution order:
//  1. The current HEAD ref's dir (fast path; matches FindGraphFile exactly).
//  2. The newest graph file across all sibling refs/<ref>/ dirs for the repo.
//
// The fallback is freshness-safe for a read-only query surface: it serves the
// most recently indexed graph the repo has, rather than nothing. Returns
// ("", 0) when no ref directory holds a graph file. The returned path's parent
// directory is the state dir the caller should read sidecars from.
func FindGraphFileAnyRef(repoPath string) (path string, modtime int64) {
	if repoPath == "" {
		return "", 0
	}
	// 1. Current-ref fast path.
	if p, mt := findGraphFileInDir(StateDirForRepo(repoPath)); p != "" {
		return p, mt
	}
	// 2. Scan every indexed ref dir and keep the newest graph file.
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	abs = filepath.Clean(abs)
	refsDir := filepath.Join(repoBaseDir(abs), "refs")
	entries, rdErr := os.ReadDir(refsDir)
	if rdErr != nil {
		return "", 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, mt := findGraphFileInDir(filepath.Join(refsDir, e.Name()))
		if p != "" && mt > modtime {
			path, modtime = p, mt
		}
	}
	return path, modtime
}

// WarnCaseCollisions scans the store directory for store slots whose
// directory name (slug-hash) does not match the hash derived from the
// canonical fleet repo path. This detects legacy store dirs created
// before canonicalizePath was introduced (#2086), where a
// case-variant of the path produced a different hash and a duplicate
// store slot.
//
// repoPaths is the list of repo paths registered in the fleet. For
// each repo the function computes the current (canonical) slug-hash
// and compares it to every existing store entry that shares the same
// base name (repo basename). Mismatches are returned as a list of
// (stale-dir, canonical-dir) pairs so the caller can log a warning.
//
// The function does NOT auto-merge or delete the stale dirs — that
// is reserved for `grafel cleanup --case-merge`. Manual cleanup:
// rm -rf the stale dir and let the daemon reindex.
//
// Returns nil when storeDir is empty, doesn't exist, or no collisions
// are found.
func WarnCaseCollisions(storeDir string, repoPaths []string) [][2]string {
	if storeDir == "" {
		return nil
	}
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		return nil
	}

	// Build a set of current canonical slugs for fast lookup.
	canonical := make(map[string]string, len(repoPaths)) // slug → repoPath
	for _, rp := range repoPaths {
		if rp == "" {
			continue
		}
		abs, err := filepath.Abs(rp)
		if err != nil {
			abs = rp
		}
		abs = filepath.Clean(abs)
		slug := repoSlug(abs)
		canonical[slug] = abs
	}

	// For each repo, also find store entries with the same base name but
	// a different hash (i.e. a hash computed from a case-variant path).
	// We do this by checking every on-disk entry against the list of known
	// repos, matching by the base-name prefix.
	var collisions [][2]string
	for _, rp := range repoPaths {
		if rp == "" {
			continue
		}
		abs, err := filepath.Abs(rp)
		if err != nil {
			abs = rp
		}
		abs = filepath.Clean(abs)

		expectedSlug := repoSlug(abs)
		base := unsafeSlugChars.ReplaceAllString(filepath.Base(abs), "-")
		base = strings.Trim(base, "-._")
		if base == "" {
			base = "repo"
		}
		if len(base) > 48 {
			base = base[:48]
		}
		prefix := base + "-"

		for _, e := range entries {
			name := e.Name()
			// Only compare store slots that share the same base-name prefix
			// but have a different hash suffix.
			if strings.HasPrefix(name, prefix) && name != expectedSlug {
				staleDir := filepath.Join(storeDir, name)
				canonicalDir := filepath.Join(storeDir, expectedSlug)
				collisions = append(collisions, [2]string{staleDir, canonicalDir})
			}
		}
	}
	return collisions
}
