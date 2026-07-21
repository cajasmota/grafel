// deadref.go — dead-ref / dead-worktree store GC (issue #5236, epic #5234).
//
// # Problem
//
// The vanished-repo Reaper (reaper.go, #3680) only GCs whole REPOS whose
// directory disappears from disk. It does NOT reclaim dead REFS *within* a
// still-present repo. Every branch the rewrite agent ever indexed leaves a
// `~/.grafel/store/<slug-hash>/refs/<ref-safe>/graph.fb` behind, and the tier
// Manager keeps the matching in-memory slot. When the agent runs
// `git branch -d X` or removes a worktree, that ref's bytes + resident graph
// should be reclaimed — but nothing does it. On acme-backend-v3 this grew to
// 252 stored refs / 13GB.
//
// # Fix
//
// DeadRefSweeper reconciles, per still-present tracked repo, the set of STORED
// refs (sub-directories under <repoBaseDir>/refs/) against the set of LIVE
// refs reported by git (branches + tags + worktree-checked-out refs, plus the
// repo's primary/default branch). For any stored ref that git no longer knows
// about it:
//
//  1. RemoveAll's the ref's store dir (refs/<ref-safe>/), reclaiming its bytes,
//  2. drops the cached mmap reader (DropReader) so the resident graph for that
//     ref is released,
//  3. forgets the tier slot (ForgetRef) so it leaves the in-memory accounting.
//
// # Guards (do not over-delete)
//
//   - Primary/default ref (main/master/…) is NEVER reaped, even if git
//     enumeration somehow omits it.
//   - Grace window: a ref whose graph.fb was written within GraceWindow
//     (default 24h) is kept, so an in-flight / just-finished index pass is
//     never raced into deletion.
//   - Retention cap: the grace window alone has no backstop against a
//     high-churn workload that creates+deletes many transient refs (e.g. the
//     rewrite agent's `merge-NNNN` branches). Each is indexed, then deleted
//     from git minutes later, but its fresh graph.fb keeps it grace-protected
//     for 24h — so ~1GB of dead-ref graphs piled up on acme-backend-v3 (#5440).
//     RetentionCap bounds the number of dead-in-git refs the grace window may
//     protect per repo: the N most-recently-indexed are kept, the rest are
//     reaped immediately. Live/primary/HEAD/worktree refs never count toward
//     the cap and are never reaped by it. The cap is env-tunable via
//     GRAFEL_REF_RETENTION_CAP (default 8; 0 keeps none; negative disables the
//     backstop) — see EnvRefRetentionCap.
//   - Fail-closed: if git ref enumeration fails for a repo, that repo is
//     skipped entirely — nothing is reaped. A flaky/locked git can never cause
//     a live ref's graph to be nuked.
//   - The _unknown sentinel ref is never reaped (it is not a real branch).
package daemon

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
)

// RefForgetter is the narrow slice of tier.Manager the dead-ref sweeper needs:
// drop the single slot for (repoPath, ref) from the in-memory accounting.
// Implemented by *tier.Manager.ForgetRef.
type RefForgetter interface {
	ForgetRef(repoPath, ref string) bool
}

// DeadRefConfig wires the dead-ref / dead-worktree store sweep. All hooks are
// optional except TrackedRepos and LiveRefs; a nil hook is simply skipped.
type DeadRefConfig struct {
	// TrackedRepos returns the absolute paths of repos the daemon tracks. Only
	// still-present repos are swept (a vanished repo is handled wholesale by the
	// Reaper). Required; nil makes the sweep a no-op.
	TrackedRepos func() []string

	// LiveRefs returns the set of refs git currently knows about for repoPath —
	// branches, tags, and refs checked out in linked worktrees — together with
	// the repo's primary/default branch. The bool is the FAIL-CLOSED signal:
	// false means git enumeration failed and the repo MUST be skipped (no ref
	// reaped). Required; nil makes the sweep a no-op.
	LiveRefs func(repoPath string) (refs map[string]struct{}, ok bool)

	// PrimaryRef returns the repo's primary/default ref (e.g. "main"). It is
	// never reaped regardless of LiveRefs. nil → no extra primary guard beyond
	// whatever LiveRefs already includes.
	PrimaryRef func(repoPath string) string

	// RefsDirForRepo returns <repoBaseDir>/refs for repoPath — the directory
	// whose immediate sub-directories are the stored refs (ref-safe encoded).
	// Required; nil makes the sweep a no-op.
	RefsDirForRepo func(repoPath string) string

	// DropReader, when non-nil, releases the cached mmap'd fbreader for a reaped
	// (repoPath, ref) so the resident graph leaves memory. Wired to the MCP
	// graph cache's per-ref invalidation in production.
	DropReader func(repoPath, ref string)

	// Tier, when non-nil, has ForgetRef(repoPath, ref) called for every reaped
	// ref so its slot leaves the in-memory tier accounting.
	Tier RefForgetter

	// GraceWindow protects a ref whose graph.fb mtime is newer than now-grace
	// from being reaped, avoiding races with in-flight indexing. Default (zero):
	// 24h. A negative value disables the grace guard (tests).
	GraceWindow time.Duration

	// RetentionCap bounds the number of dead-in-git ref graphs the grace window
	// may protect per repo. When more than RetentionCap dead-in-git refs are
	// grace-protected, the oldest (by graph.fb mtime) beyond the cap are reaped
	// immediately — the backstop against high-churn transient-ref creation
	// (#5440). Live/primary/HEAD/worktree refs never count toward the cap.
	// Default (zero): resolved from GRAFEL_REF_RETENTION_CAP, falling back to
	// DefaultRefRetentionCap. A negative value disables the cap.
	RetentionCap int

	// Now returns the current time; nil → time.Now. Injected in tests.
	Now func() time.Time

	// Logger for sweep diagnostics. nil → the sweeper inherits the reaper's.
	Logger *slog.Logger
}

// DeadRefResult summarises one dead-ref sweep.
type DeadRefResult struct {
	// ReposScanned is the number of still-present tracked repos inspected.
	ReposScanned int
	// ReposSkipped is the number of repos skipped because git ref enumeration
	// failed (fail-closed) or had no refs/ store dir.
	ReposSkipped int
	// RefsReaped is the number of stored refs whose dir was removed.
	RefsReaped int
	// SlotsForgotten is the number of tier slots dropped.
	SlotsForgotten int
	// FreedBytes is the total bytes reclaimed from deleted ref store dirs.
	FreedBytes int64
	// CapEvicted is the number of grace-protected dead refs reaped by the
	// retention cap (i.e. they would have survived on the grace window alone).
	CapEvicted int
}

// DefaultRefRetentionCap is the default ceiling on grace-protected dead-in-git
// ref graphs kept per repo. Picked as a small backstop: enough headroom for a
// handful of genuinely in-flight reindexes, low enough that a high-churn
// transient-ref workload cannot pile up ~1GB of dead graphs (#5440).
const DefaultRefRetentionCap = 8

// RefRetentionCapEnv is the env var an operator may set to override
// DefaultRefRetentionCap. A lower value (e.g. 4) shrinks the dead-ref footprint
// on a machine with heavy transient-ref churn; 0 keeps only live/primary/HEAD/
// worktree refs; a negative value disables the cap backstop entirely (the grace
// window alone then governs retention). See EnvRefRetentionCap.
const RefRetentionCapEnv = "GRAFEL_REF_RETENTION_CAP"

// EnvRefRetentionCap resolves the dead-ref retention cap, honouring the
// GRAFEL_REF_RETENTION_CAP override. Semantics:
//
//   - unset / empty            → DefaultRefRetentionCap (8)
//   - a valid non-negative int → that value (0 = keep no grace-protected dead
//     refs; only live/primary/HEAD/worktree refs survive)
//   - a valid negative int     → that value, which DeadRefSweeper treats as
//     "cap disabled" (the existing RetentionCap < 0 backstop-off semantics)
//   - unparseable garbage      → DefaultRefRetentionCap (fail-safe)
//
// Mirrors the GRAFEL_TIER_*/GRAFEL_EXTRACT_GOMAXPROCS env-reading pattern, but
// permits 0 and negative values, which those positive-only helpers reject.
func EnvRefRetentionCap() int {
	s := strings.TrimSpace(os.Getenv(RefRetentionCapEnv))
	if s == "" {
		return DefaultRefRetentionCap
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return DefaultRefRetentionCap
	}
	return n
}

// DeadRefSweeper reclaims store dirs + resident graphs for refs/worktrees that
// git no longer knows about, within still-present repos.
type DeadRefSweeper struct {
	cfg    DeadRefConfig
	logger *slog.Logger
}

// NewDeadRefSweeper constructs a DeadRefSweeper. Call Sweep directly (the
// Reaper drives it on the shared cadence) or in tests.
func NewDeadRefSweeper(cfg DeadRefConfig) *DeadRefSweeper {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "deadref")
	}
	if cfg.GraceWindow == 0 {
		cfg.GraceWindow = 24 * time.Hour
	}
	if cfg.RetentionCap == 0 {
		// Zero means "caller did not set it" — resolve from the environment
		// (GRAFEL_REF_RETENTION_CAP), which itself falls back to
		// DefaultRefRetentionCap when unset. An operator who explicitly wants a
		// cap of 0 (keep no grace-protected dead refs) sets the env to "0";
		// callers that need a literal 0 regardless of env can pass a negative
		// value, which disables the backstop.
		cfg.RetentionCap = EnvRefRetentionCap()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &DeadRefSweeper{cfg: cfg, logger: logger}
}

// Sweep runs one reconciliation pass synchronously and returns what it reaped.
func (s *DeadRefSweeper) Sweep() DeadRefResult {
	var res DeadRefResult
	if s.cfg.TrackedRepos == nil || s.cfg.LiveRefs == nil || s.cfg.RefsDirForRepo == nil {
		return res
	}
	for _, repo := range s.cfg.TrackedRepos() {
		if repo == "" {
			continue
		}
		// Only sweep repos still present on disk; vanished repos are GCed
		// wholesale by the Reaper.
		if !repoExists(repo) {
			continue
		}
		s.sweepRepo(repo, &res)
	}
	if res.RefsReaped > 0 {
		s.logger.Info("deadref: sweep complete",
			"repos_scanned", res.ReposScanned,
			"repos_skipped", res.ReposSkipped,
			"refs_reaped", res.RefsReaped,
			"cap_evicted", res.CapEvicted,
			"slots_forgotten", res.SlotsForgotten,
			"freed_bytes", res.FreedBytes)
	}
	return res
}

// sweepRepo reconciles one still-present repo's stored refs against git.
func (s *DeadRefSweeper) sweepRepo(repo string, res *DeadRefResult) {
	refsDir := s.cfg.RefsDirForRepo(repo)
	if refsDir == "" {
		res.ReposSkipped++
		return
	}
	stored, err := os.ReadDir(refsDir)
	if err != nil {
		// No refs/ dir yet (repo not indexed) or unreadable — nothing to do.
		res.ReposSkipped++
		return
	}

	// FAIL-CLOSED: if git enumeration fails, skip the whole repo.
	live, ok := s.cfg.LiveRefs(repo)
	if !ok {
		s.logger.Warn("deadref: git ref enumeration failed — skipping repo (fail-closed)", "repo", repo)
		res.ReposSkipped++
		return
	}
	res.ReposScanned++

	// Primary/default ref is never reaped.
	var primary string
	if s.cfg.PrimaryRef != nil {
		primary = s.cfg.PrimaryRef(repo)
	}

	graceCutoff := s.cfg.Now().Add(-s.cfg.GraceWindow)

	// graceHeld collects dead-in-git refs that the grace window protects, so the
	// retention cap can evict the oldest beyond the cap as a backstop (#5440).
	var graceHeld []graceHeldRef

	for _, e := range stored {
		if !e.IsDir() {
			continue
		}
		refSafe := e.Name()
		if refSafe == "_unknown" {
			continue // sentinel — not a real branch.
		}
		ref := RefSafeDecode(refSafe)

		// Guard: primary ref.
		if ref != "" && ref == primary {
			continue
		}
		// Guard: still a live ref/tag/worktree-checked-out ref.
		if _, alive := live[ref]; alive {
			continue
		}
		refDir := filepath.Join(refsDir, refSafe)
		// Guard: grace window — defer recently-indexed refs to the retention-cap
		// pass below rather than keeping them unconditionally. The cap evicts the
		// oldest of these when there are too many (high-churn transient refs);
		// the rest are kept exactly as before.
		if s.cfg.GraceWindow >= 0 && recentlyIndexed(refDir, graceCutoff) {
			graceHeld = append(graceHeld, graceHeldRef{ref: ref, dir: refDir, mtime: refGraphMtime(refDir)})
			continue
		}

		s.reapRef(repo, ref, refDir, res, false)
	}

	// Retention-cap backstop: of the grace-protected dead refs, keep only the
	// RetentionCap most-recently-indexed; reap the rest (oldest first).
	if s.cfg.RetentionCap >= 0 && len(graceHeld) > s.cfg.RetentionCap {
		// Newest first so the head of the slice is the keep-set.
		sort.Slice(graceHeld, func(i, j int) bool {
			return graceHeld[i].mtime.After(graceHeld[j].mtime)
		})
		for _, h := range graceHeld[s.cfg.RetentionCap:] {
			s.logger.Info("deadref: grace-protected dead ref over retention cap — reaping", "repo", repo, "ref", h.ref, "cap", s.cfg.RetentionCap)
			if s.reapRef(repo, h.ref, h.dir, res, true) {
				res.CapEvicted++
			}
		}
		for _, h := range graceHeld[:s.cfg.RetentionCap] {
			s.logger.Info("deadref: ref dead in git but recently indexed — keeping (grace window, within cap)", "repo", repo, "ref", h.ref)
		}
	} else {
		for _, h := range graceHeld {
			s.logger.Info("deadref: ref dead in git but recently indexed — keeping (grace window)", "repo", repo, "ref", h.ref)
		}
	}
}

// graceHeldRef is a dead-in-git ref kept by the grace window, a candidate for
// retention-cap eviction.
type graceHeldRef struct {
	ref   string
	dir   string
	mtime time.Time
}

// reapRef removes a ref's store dir, drops its cached reader, and forgets its
// tier slot, updating res. Returns true when the store dir was removed. The
// caller logs the higher-level reason; reapRef logs the reap itself.
func (s *DeadRefSweeper) reapRef(repo, ref, refDir string, res *DeadRefResult, capEvict bool) bool {
	sz, rmErr := s.removeRefStore(refDir)
	if rmErr != nil {
		s.logger.Warn("deadref: ref store removal failed (non-fatal)", "repo", repo, "ref", ref, "dir", refDir, "err", rmErr)
		return false
	}
	res.RefsReaped++
	if sz > 0 {
		res.FreedBytes += sz
	}
	s.logger.Info("deadref: reaped dead ref", "repo", repo, "ref", ref, "dir", refDir, "freed_bytes", sz, "cap_evicted", capEvict)

	if s.cfg.DropReader != nil {
		s.cfg.DropReader(repo, ref)
	}
	if s.cfg.Tier != nil && s.cfg.Tier.ForgetRef(repo, ref) {
		res.SlotsForgotten++
	}
	return true
}

// refGraphMtime returns the newest graph.fb / graph.json mtime under refDir, or
// the zero time when neither exists. Used to order grace-protected refs for the
// retention cap (oldest evicted first).
func refGraphMtime(refDir string) time.Time {
	var newest time.Time
	// #5891: resolve the active generation (flat graph.fb fallback) so a
	// gen-layout ref reports its real freshness, not mtime-zero.
	for _, p := range []string{graph.CurrentGraphPath(refDir), filepath.Join(refDir, "graph.json")} {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

// recentlyIndexed reports whether the ref dir holds a graph.fb (or graph.json)
// whose mtime is at/after cutoff — i.e. it was indexed inside the grace window.
// A missing graph file is treated as NOT recent (eligible for reaping).
func recentlyIndexed(refDir string, cutoff time.Time) bool {
	// #5891: resolve the active generation so a freshly-indexed gen-layout ref
	// is correctly protected by the grace window (a fixed graph.fb no longer
	// exists after migration).
	for _, p := range []string{graph.CurrentGraphPath(refDir), filepath.Join(refDir, "graph.json")} {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !fi.ModTime().Before(cutoff) {
			return true
		}
	}
	return false
}

// LiveGitRefs is the production LiveRefs enumerator (#5236). It returns the set
// of refs git currently knows about for repoPath — local branches, tags, and
// the branches checked out in any linked worktree — together with the repo's
// primary/default branch.
//
// FAIL-CLOSED contract: the bool is false when git enumeration cannot be
// trusted (repoPath is not a git repo, or `for-each-ref` produced no output and
// the repo is unreadable). The caller MUST skip the repo on false so a flaky or
// locked git can never strand a live ref into deletion. We treat a non-git path
// and an empty top-level as failure; a real repo with zero non-primary refs
// still returns true (with the primary branch present).
func LiveGitRefs(repoPath string) (map[string]struct{}, bool) {
	// Sanity: must be a git work tree.
	top := gitmeta.RunGit(repoPath, "rev-parse", "--show-toplevel")
	if top == "" {
		return nil, false
	}

	refs := make(map[string]struct{})

	// Local branches + tags. (Remote-tracking refs are intentionally excluded:
	// they are never indexed as their own slot.)
	out := gitmeta.RunGit(repoPath, "for-each-ref", "--format=%(refname:short)",
		"refs/heads", "refs/tags")
	for _, line := range strings.Split(out, "\n") {
		if r := strings.TrimSpace(line); r != "" {
			refs[r] = struct{}{}
		}
	}

	// Branches checked out in linked worktrees. `worktree list --porcelain`
	// emits a `branch refs/heads/<name>` line per worktree.
	wtOut := gitmeta.RunGit(repoPath, "worktree", "list", "--porcelain")
	for _, line := range strings.Split(wtOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "branch ") {
			b := strings.TrimPrefix(line, "branch ")
			b = strings.TrimPrefix(b, "refs/heads/")
			if b != "" {
				refs[b] = struct{}{}
			}
		}
	}

	// Always keep HEAD's current ref and the primary/default branch.
	if cur := gitmeta.RunGit(repoPath, "symbolic-ref", "--short", "HEAD"); cur != "" {
		refs[cur] = struct{}{}
	}
	if p := PrimaryGitRef(repoPath); p != "" {
		refs[p] = struct{}{}
	}

	return refs, true
}

// PrimaryGitRef returns the repo's primary/default branch ("" if undetermined).
// Prefers origin/HEAD; falls back to the conventional names.
func PrimaryGitRef(repoPath string) string {
	if originHead := gitmeta.RunGit(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD", "--short"); originHead != "" {
		if parts := strings.SplitN(originHead, "/", 2); len(parts) == 2 {
			return parts[1]
		}
	}
	cur := gitmeta.RunGit(repoPath, "symbolic-ref", "--short", "HEAD")
	switch cur {
	case "main", "master", "trunk":
		return cur
	}
	// Probe for a conventional default even when HEAD is on a feature branch.
	for _, name := range []string{"main", "master", "trunk"} {
		if gitmeta.RunGit(repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+name) != "" {
			return name
		}
	}
	return cur
}

// RefsDirForRepo returns <repoBaseDir>/refs for repoPath — the directory whose
// immediate sub-dirs are the ref-safe-encoded stored refs. This mirrors the
// store layout used by StateDirForRepoRef.
func RefsDirForRepo(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	abs = filepath.Clean(abs)
	return filepath.Join(repoBaseDir(abs), "refs")
}

// removeRefStore deletes refDir and returns the bytes it freed. A non-existent
// dir is not an error (returns 0 freed).
func (s *DeadRefSweeper) removeRefStore(refDir string) (int64, error) {
	sz, err := dirSizeHygiene(refDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		sz = 0
	}
	if rmErr := os.RemoveAll(refDir); rmErr != nil {
		return 0, rmErr
	}
	return sz, nil
}
