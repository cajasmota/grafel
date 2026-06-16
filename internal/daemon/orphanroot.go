// orphanroot.go — orphan top-level store-root GC (issue #5263, epic #5234).
//
// # Problem
//
// The vanished-repo Reaper (reaper.go, #3680) only GCs the stores of repos the
// daemon is CURRENTLY TRACKING (registered repos + active worktree children)
// whose directory has disappeared. The dead-ref sweeper (deadref.go, #5236)
// only reclaims dead REFS *within* a still-present, still-tracked repo. Neither
// reclaims a top-level store root that is tracked by NOTHING anymore: a
// `<store>/<slug>-<hash>/` slot for a worktree/repo that was indexed once, then
// removed from the registry (or whose worktree was deleted) so it never appears
// in TrackedRepos again. On core-backend-v3 the live store grew to ~12GB across
// 357 top-level roots, most of them such orphans.
//
// # The store-root ↔ source-path mapping (the key design decision)
//
// A root is `<store>/<slug>-<hash>` where hash = sha256(canonical(absPath))[:16]
// (see state_path.go). The hash is ONE-WAY and roots do NOT self-record their
// source path on disk (graph.json carries only a human label + is_worktree, not
// the absolute path). So there is NO authoritative reverse mapping from a root
// back to a filesystem path.
//
// Therefore attribution is done in the FORWARD direction: enumerate every KNOWN
// source path (registered-group repos + their git worktrees — both still-present
// AND already-vanished), compute RepoBaseDir(path) for each, and build a map
// root → source path. An on-disk root is then attributed as:
//
//   - maps to a known path that STILL EXISTS  → KEEP (live).
//   - maps to a known path that is GONE        → ORPHAN candidate (reapable).
//   - maps to NO known path                    → source path UNDETERMINABLE →
//                                                 KEEP (fail-closed).
//
// # Guards (mirror the ref-GC safety model exactly — do not over-delete)
//
//   - A root whose source path still exists is NEVER reaped.
//   - A root mapping to a registered/live group repo (or its primary) is NEVER
//     reaped — such a path is in the known set and (if present) exists.
//   - Grace window: a root whose newest graph artifact mtime is within
//     GraceWindow (default 24h) is kept, so an in-flight / just-finished index
//     pass is never raced into deletion.
//   - Fail-closed: if a root cannot be attributed to a known source path, or
//     its liveness is otherwise undeterminable, it is KEPT and logged — never
//     reaped. The store-root base being unreadable skips the whole sweep.
//
// This is intentionally conservative so it is safe to run continuously while
// the daemon serves the rewrite agent. When in doubt, KEEP.
package daemon

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// OrphanRootConfig wires the orphan top-level store-root sweep. All hooks are
// optional except KnownSourcePaths; a nil hook is simply skipped.
type OrphanRootConfig struct {
	// KnownSourcePaths returns every source path the daemon has any knowledge
	// of: registered-group repos and their git worktrees, INCLUDING ones whose
	// directory has already vanished. The forward map RepoBaseDir(path) → path
	// built from this set is the ONLY way a root is attributed to a path; a root
	// not covered here is treated as undeterminable (fail-closed KEEP).
	// Required; nil makes the sweep a no-op.
	KnownSourcePaths func() []string

	// StoreRootBase returns the directory that directly contains the top-level
	// roots (its immediate sub-dirs are the per-repo slots). nil → the package
	// StoreRootBase() (honours GRAFEL_DAEMON_ROOT).
	StoreRootBase func() string

	// RootForPath maps a source path to its top-level store root. nil →
	// RepoBaseDir. Injected in tests to align with a fixture store layout.
	RootForPath func(path string) string

	// PathExists reports whether a source path still exists on disk as a
	// directory. nil → repoExists (fail-SAFE: any stat error other than
	// not-exist is treated as "exists" so a flaky FS never reaps a live root).
	PathExists func(path string) bool

	// ReadRootManifest reads a root's recorded source-path manifest (#5267).
	// Returns (manifest, present, err); present=false for a legacy root with no
	// manifest. nil → ReadRootManifest. When a manifest IS present, its recorded
	// source_path is preferred over the forward map, so a root whose source path
	// was removed from the registry (and thus no longer hashes forward) is still
	// attributable. Injected in tests.
	ReadRootManifest func(rootDir string) (RootManifest, bool, error)

	// AllowUndeterminableReap, when true, lets the OPERATOR opt-in reap path
	// (#5268) consider roots whose source path is undeterminable (legacy +
	// path-gone + not in the forward map), bounded by ReapOlderThan. The
	// PERIODIC reaper NEVER sets this — it stays fail-closed. Default false.
	AllowUndeterminableReap bool

	// ReapOlderThan bounds the opt-in undeterminable reap: only an
	// undeterminable root whose newest graph artifact mtime is older than this
	// is eligible. Ignored unless AllowUndeterminableReap is true. A zero/negative
	// value with AllowUndeterminableReap set reaps NOTHING (an explicit age
	// bound is required to reap undeterminable roots).
	ReapOlderThan time.Duration

	// Tier, when non-nil, has Forget(repoPath) called for every reaped orphan
	// root so any lingering in-memory slots leave the tier accounting.
	Tier TierForgetter

	// DropReaderForRoot, when non-nil, releases any cached mmap readers tied to
	// the reaped source path so resident graphs are released.
	DropReaderForRoot func(repoPath string)

	// GraceWindow protects a root whose newest graph artifact mtime is newer
	// than now-grace from reaping. Default (zero): 24h. Negative disables the
	// grace guard (tests).
	GraceWindow time.Duration

	// Now returns the current time; nil → time.Now. Injected in tests.
	Now func() time.Time

	// Logger for sweep diagnostics. nil → a default stderr logger.
	Logger *slog.Logger
}

// OrphanRootVerdict is the per-root attribution emitted by the dry-run /
// operator path. It is also the unit the prune path acts on.
type OrphanRootVerdict struct {
	// Root is the absolute top-level store-root directory.
	Root string
	// SourcePath is the attributed source path, or "" when undeterminable.
	SourcePath string
	// PathKnown is true when Root mapped to a known source path (via the
	// recorded manifest source_path OR the forward map).
	PathKnown bool
	// FromManifest is true when SourcePath came from the root's recorded
	// `root.json` manifest (#5267) rather than the forward hash map.
	FromManifest bool
	// Undeterminable is true when the source path could not be determined at all
	// (no manifest AND not in the forward map). Such a root is KEPT by default
	// and only reaped under the operator opt-in (#5268).
	Undeterminable bool
	// PathExists is true when the attributed source path exists on disk.
	PathExists bool
	// RefCount is the number of stored refs under <root>/refs/.
	RefCount int
	// SizeBytes is the on-disk size of the root tree.
	SizeBytes int64
	// AgeOfNewest is how long ago the newest graph artifact under the root was
	// written (0 when none found).
	AgeOfNewest time.Duration
	// WithinGrace is true when the newest artifact is inside the grace window.
	WithinGrace bool
	// Verdict is "KEEP" or "ORPHAN" (would-prune / pruned).
	Verdict string
	// Reason is a short human explanation of the verdict.
	Reason string
}

// IsOrphan reports whether this root is a prune candidate under the full safety
// predicate: attributed to a known source path, that path is GONE, and it is
// outside the grace window. Anything undeterminable or live or recently-indexed
// is NOT an orphan (fail-closed KEEP).
func (v OrphanRootVerdict) IsOrphan() bool { return v.Verdict == "ORPHAN" }

// OrphanRootResult summarises one prune sweep.
type OrphanRootResult struct {
	// RootsScanned is the number of on-disk top-level roots inspected.
	RootsScanned int
	// RootsReaped is the number of orphan roots removed.
	RootsReaped int
	// SlotsForgotten is the number of tier slots dropped.
	SlotsForgotten int
	// FreedBytes is the total bytes reclaimed from removed roots.
	FreedBytes int64
	// Kept is the number of roots kept (live, undeterminable, or in grace).
	Kept int
}

// OrphanRootSweeper reaps top-level store roots that map to a vanished source
// path and to no live group/primary, using the same conservative safety model
// as the ref GC.
type OrphanRootSweeper struct {
	cfg    OrphanRootConfig
	logger *slog.Logger
}

// NewOrphanRootSweeper constructs an OrphanRootSweeper. Call Sweep (prune) or
// Attribute (dry-run) directly; the Reaper can drive Sweep on the shared cadence.
func NewOrphanRootSweeper(cfg OrphanRootConfig) *OrphanRootSweeper {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "orphanroot")
	}
	if cfg.StoreRootBase == nil {
		cfg.StoreRootBase = StoreRootBase
	}
	if cfg.RootForPath == nil {
		cfg.RootForPath = RepoBaseDir
	}
	if cfg.PathExists == nil {
		cfg.PathExists = repoExists
	}
	if cfg.ReadRootManifest == nil {
		cfg.ReadRootManifest = ReadRootManifest
	}
	if cfg.GraceWindow == 0 {
		cfg.GraceWindow = 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &OrphanRootSweeper{cfg: cfg, logger: logger}
}

// rootToPath builds the forward attribution map root → source path from the
// known source paths. When two paths hash to the same root (should not happen
// in practice), an EXISTING path wins so a live root is never mis-attributed to
// a vanished sibling.
func (s *OrphanRootSweeper) rootToPath() map[string]string {
	out := map[string]string{}
	if s.cfg.KnownSourcePaths == nil {
		return out
	}
	for _, p := range s.cfg.KnownSourcePaths() {
		if p == "" {
			continue
		}
		root := filepath.Clean(s.cfg.RootForPath(p))
		if root == "" {
			continue
		}
		if existing, ok := out[root]; ok {
			// Prefer the path that still exists.
			if s.cfg.PathExists(existing) {
				continue
			}
		}
		out[root] = p
	}
	return out
}

// Attribute enumerates every on-disk top-level store root and returns its
// verdict WITHOUT removing anything (the dry-run / operator view). It applies
// the exact safety predicate the prune path uses, so a root reported ORPHAN
// here is precisely what --prune would remove.
func (s *OrphanRootSweeper) Attribute() []OrphanRootVerdict {
	base := s.cfg.StoreRootBase()
	entries, err := os.ReadDir(base)
	if err != nil {
		// Store base unreadable / missing — nothing to attribute. Fail-closed:
		// we never guess at roots we cannot enumerate.
		if !errors.Is(err, os.ErrNotExist) {
			s.logger.Warn("orphanroot: store base unreadable — skipping sweep (fail-closed)", "base", base, "err", err)
		}
		return nil
	}

	r2p := s.rootToPath()
	graceCutoff := s.cfg.Now().Add(-s.cfg.GraceWindow)

	var out []OrphanRootVerdict
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		root := filepath.Join(base, e.Name())
		v := OrphanRootVerdict{Root: root}

		// Attribution priority (#5267): prefer the root's RECORDED source path
		// (root.json manifest) over the one-way forward map. A recorded path is
		// authoritative even when the path was removed from the registry / git
		// worktree set and so no longer hashes forward to this root.
		var src string
		var known bool
		if mfst, present, mErr := s.cfg.ReadRootManifest(root); mErr == nil && present && mfst.SourcePath != "" {
			src = mfst.SourcePath
			known = true
			v.FromManifest = true
		} else {
			// Legacy root (no manifest) → fall back to the forward map.
			src, known = r2p[filepath.Clean(root)]
		}
		v.SourcePath = src
		v.PathKnown = known
		v.Undeterminable = !known

		// Cheap diagnostics for the operator view.
		v.RefCount = countRefs(filepath.Join(root, "refs"))
		if sz, serr := dirSizeHygiene(root); serr == nil {
			v.SizeBytes = sz
		}
		newest, hasArtifact := newestArtifactMTime(root)
		if hasArtifact {
			v.AgeOfNewest = s.cfg.Now().Sub(newest)
			// Grace guard uses the same cutoff semantics as deadref.
			v.WithinGrace = s.cfg.GraceWindow >= 0 && !newest.Before(graceCutoff)
		}

		// Record liveness for the operator view (and reuse in the switch).
		if known {
			v.PathExists = s.cfg.PathExists(src)
		}

		switch {
		case known && v.PathExists:
			// Source path exists (covers live group/primary repos) → KEEP. This
			// guard precedes the undeterminable branch so a root with a recorded
			// manifest whose path STILL EXISTS is never reaped, even in opt-in mode.
			v.Verdict = "KEEP"
			v.Reason = "source path exists on disk"
		case known && v.WithinGrace:
			// Known-but-gone yet recently indexed → KEEP (race guard).
			v.Verdict = "KEEP"
			v.Reason = "source path gone but recently indexed (grace window)"
		case known:
			// Known, vanished, not live, outside grace → ORPHAN. With a recorded
			// manifest this is now attributable WITHOUT the forward map.
			v.Verdict = "ORPHAN"
			if v.FromManifest {
				v.Reason = "recorded source path gone and maps to no live group/primary"
			} else {
				v.Reason = "source path gone and maps to no live group/primary"
			}
		case s.cfg.AllowUndeterminableReap && s.reapableUndeterminable(v):
			// Operator opt-in (#5268): undeterminable + gone + older than the
			// explicit age bound → ORPHAN. Recoverable (re-indexes if needed).
			v.Verdict = "ORPHAN"
			v.Reason = "source path undeterminable and untouched beyond --older-than — operator opt-in reap (re-indexes if needed)"
		case v.WithinGrace:
			// Undeterminable but recently indexed → KEEP even in opt-in mode.
			v.Verdict = "KEEP"
			v.Reason = "source path undeterminable; recently indexed (grace window)"
		default:
			// Undeterminable source path → FAIL-CLOSED KEEP (default + periodic).
			v.Verdict = "KEEP"
			v.Reason = "source path undeterminable (no manifest; no known repo/worktree maps to this root) — fail-closed"
		}
		out = append(out, v)
	}
	return out
}

// reapableUndeterminable reports whether an UNDETERMINABLE root is eligible for
// the operator opt-in reap (#5268). It is the only path that can reap a root
// whose source path is undeterminable, and it requires an EXPLICIT positive age
// bound: a root is reapable only when its newest graph artifact is OLDER than
// ReapOlderThan. With a zero/negative bound nothing is reaped (an explicit
// --older-than is mandatory). A root with no artifact at all has unknown age and
// is conservatively NOT reaped.
func (s *OrphanRootSweeper) reapableUndeterminable(v OrphanRootVerdict) bool {
	if !s.cfg.AllowUndeterminableReap {
		return false
	}
	if s.cfg.ReapOlderThan <= 0 {
		return false
	}
	if v.AgeOfNewest <= 0 {
		// No datable artifact → unknown age → keep (conservative).
		return false
	}
	return v.AgeOfNewest > s.cfg.ReapOlderThan
}

// Sweep enumerates the store roots and PRUNES the orphans (path-gone, not-live,
// outside grace). Roots that are live, undeterminable, or in grace are kept.
// Returns what it reclaimed. Safe to call directly from tests or the reaper.
func (s *OrphanRootSweeper) Sweep() OrphanRootResult {
	var res OrphanRootResult
	if s.cfg.KnownSourcePaths == nil {
		return res
	}
	for _, v := range s.Attribute() {
		res.RootsScanned++
		if !v.IsOrphan() {
			res.Kept++
			continue
		}
		// Reap: remove the on-disk root tree + any in-mem slot.
		sz, rmErr := removeRootTree(v.Root)
		if rmErr != nil {
			s.logger.Warn("orphanroot: root removal failed (non-fatal)", "root", v.Root, "src", v.SourcePath, "err", rmErr)
			res.Kept++
			continue
		}
		res.RootsReaped++
		if sz > 0 {
			res.FreedBytes += sz
		}
		s.logger.Info("orphanroot: reaped orphan store root",
			"root", v.Root, "src", v.SourcePath, "freed_bytes", sz)

		if s.cfg.DropReaderForRoot != nil && v.SourcePath != "" {
			s.cfg.DropReaderForRoot(v.SourcePath)
		}
		if s.cfg.Tier != nil && v.SourcePath != "" {
			res.SlotsForgotten += s.cfg.Tier.Forget(v.SourcePath)
		}
	}
	if res.RootsReaped > 0 {
		s.logger.Info("orphanroot: sweep complete",
			"roots_scanned", res.RootsScanned,
			"roots_reaped", res.RootsReaped,
			"slots_forgotten", res.SlotsForgotten,
			"freed_bytes", res.FreedBytes,
			"kept", res.Kept)
	}
	return res
}

// countRefs returns the number of immediate sub-directories under refsDir (the
// stored refs). A missing/unreadable dir returns 0.
func countRefs(refsDir string) int {
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// newestArtifactMTime walks root for the newest graph.fb / graph.json mtime
// across all refs. Returns (zero, false) when no artifact is found.
func newestArtifactMTime(root string) (time.Time, bool) {
	var newest time.Time
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != "graph.fb" && name != "graph.json" {
			return nil
		}
		fi, ferr := d.Info()
		if ferr != nil {
			return nil
		}
		if mt := fi.ModTime(); mt.After(newest) {
			newest = mt
			found = true
		}
		return nil
	})
	return newest, found
}

// removeRootTree deletes root and returns the bytes it freed. A non-existent
// dir is not an error (returns 0 freed). Mirrors removeRefStore.
func removeRootTree(root string) (int64, error) {
	sz, err := dirSizeHygiene(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		sz = 0
	}
	if rmErr := os.RemoveAll(root); rmErr != nil {
		return 0, rmErr
	}
	return sz, nil
}
