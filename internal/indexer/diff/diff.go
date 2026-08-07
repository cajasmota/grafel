// Package diff provides diff-aware (incremental) re-indexing for grafel.
//
// On every full rebuild every source file in a repo is re-processed, even when
// only a handful changed. For a 1 500-file repo with 5 edited files that is
// ~1 495 wasted AST parses. This package tracks per-file SHA-256 content
// hashes in a small manifest persisted to `.grafel/file-index.json` and
// exposes helpers that tell the indexer which files actually changed since the
// last run.
//
// Design goals
//
//   - Zero-overhead on full rebuild: if the manifest is absent or
//     Incremental=false the indexer behaves exactly as before.
//   - Conservative cross-file invalidation: any file that imports a changed
//     file is also marked dirty, so cross-file reference resolution cannot
//     yield stale results.
//   - Git-aware shortcut: when the repo is a git repository, `git diff
//     --name-only HEAD` provides the changed-file list in O(1) without
//     reading every file. Falls back to hash comparison otherwise.
//   - Full-rebuild escape hatch: callers pass Incremental=false (the
//     `grafel rebuild --full` flag) to skip all diffing.
//
// Manifest format (`.grafel/file-index.json`):
//
//	{
//	  "version": 1,
//	  "indexed_at": "2026-05-21T10:00:00Z",
//	  "git_commit": "abc1234",          // empty when not a git repo
//	  "files": {
//	    "src/foo.go": {
//	      "sha256": "e3b0c44298fc1c14...",
//	      "size":   1234,
//	      "mtime":  1716288000000000000
//	    }
//	  }
//	}
package diff

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// Version is the manifest schema version. Increment when the JSON shape changes
// in a backwards-incompatible way; the loader discards manifests with a
// different version (triggering a full rebuild that re-creates the manifest).
const Version = 1

// manifestFile is the name of the per-repo manifest inside the state directory.
const manifestFile = "file-index.json"

// ManifestFileName is the exported basename of the per-repo diff manifest.
// Callers outside this package that must COPY or REMOVE the manifest as an
// artifact — rather than read it through LoadManifest — need the basename:
// #5964's worktree graph seeding copies the parent ref's manifest alongside
// its graph so the child's first incremental pass has the parent's baseline.
// Seeding a graph without its manifest is silently wrong, so the coupling is
// named here rather than re-spelled as a literal at each call site (it was
// already re-spelled once, in internal/daemon/state_migrate.go).
const ManifestFileName = manifestFile

// MaxExtractRetries bounds how many EXTRA passes a file whose extraction failed
// is forced back into the changed set before the manifest is allowed to treat it
// as settled (#6209).
//
// Two is a deliberate middle: enough to ride out a transient failure (an
// oversubscribed machine, a partially-written file, a cancelled child) without
// making a genuinely unparseable file — an unsupported dialect, a vendored
// minified blob, a generated file the grammar chokes on — cost a re-read and a
// re-parse on every pass for the life of the repo. Once the budget is spent the
// file goes quiet until its BYTES change, which is the only new information
// that could plausibly change the outcome.
const MaxExtractRetries = 2

// FileEntry holds the hash + metadata for one indexed source file.
type FileEntry struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"` // UnixNano

	// Failures counts CONSECUTIVE passes on which this file was read and
	// stamped but its EXTRACTION failed, so the graph does not contain its
	// entities even though the stamp says the bytes were indexed (#6209).
	//
	// It exists because those two facts are otherwise indistinguishable on
	// disk. A stamp means "the graph was built from these bytes"; for a failed
	// file that claim is false, and it is self-concealing — the next pass
	// hash-matches the stamp, calls the file unchanged, and the missing entity
	// set is never noticed, let alone healed.
	//
	// Non-zero and within MaxExtractRetries forces the file back into the
	// changed set (see RetryDue). Reset to zero by any successful extraction —
	// ApplyStamps overwrites the whole entry — and reset to one when the file's
	// content changes, because different bytes earn a fresh budget.
	//
	// omitempty: the overwhelmingly common value is 0, and a manifest is
	// written once per pass per repo.
	Failures int `json:"failures,omitempty"`
}

// Manifest is the on-disk representation of the per-repo file index.
type Manifest struct {
	Version   int       `json:"version"`
	IndexedAt time.Time `json:"indexed_at"`
	GitCommit string    `json:"git_commit,omitempty"`
	// GitCommitFull is the FULL 40-char HEAD commit SHA at the time this
	// manifest was saved (#5727/#5729-W1). GitCommit above is the abbreviated
	// (short) form used by the incremental diff range-check; GitCommitFull is
	// surfaced by grafel_index_status / `grafel status` as an unambiguous
	// indexed_commit so a caller never has to disambiguate a short-SHA prefix
	// collision. Empty when not a git repo or the git call fails/times out.
	GitCommitFull string               `json:"git_commit_full,omitempty"`
	Files         map[string]FileEntry `json:"files"`
}

// LoadManifest reads the manifest from stateDir. Returns an empty manifest
// (ready to accept new entries) when the file is absent, malformed, or has a
// version mismatch.
func LoadManifest(stateDir string) *Manifest {
	path := filepath.Join(stateDir, manifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return newManifest()
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version != Version {
		return newManifest()
	}
	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}
	return &m
}

// SaveManifest atomically writes m to stateDir, stamping GitCommit /
// GitCommitFull from repoPath's LIVE HEAD at save time (best-effort; empty when
// not a git repo). Returns nil on success.
//
// Live HEAD is the right stamp only for a caller whose pass actually built the
// graph from the tree at that commit. A caller that indexed NOTHING — or that
// knows which commit it read, because it captured it before doing the work —
// must use SaveManifestAtCommit instead. See #5822 ②.
func SaveManifest(stateDir, repoPath string, m *Manifest) error {
	return SaveManifestAtCommit(stateDir, m, headCommit(repoPath), headCommitFull(repoPath))
}

// SaveManifestAtCommit atomically writes m to stateDir, stamping GitCommit /
// GitCommitFull from the caller-supplied commit rather than from live HEAD.
//
// WHY THE COMMIT IS A PARAMETER (#5822 ②). GitCommit answers exactly one
// question — "which commit is the persisted graph built from?" — and only the
// caller knows the answer. SaveManifest's live-HEAD read answers a DIFFERENT
// question, "which commit is checked out at this instant", and the two diverge
// on every path that writes a manifest without building a graph from HEAD. The
// too-many-changed reject in internal/extractors is the load-bearing one: it
// re-stamps files and returns a full-reindex REQUEST, having changed no entity,
// so live HEAD there records a commit the graph has never contained. This is
// the same correction #6212 made for the per-FILE stamps, which likewise moved
// from "hash the path at commit time" to "record what the pass actually read".
//
// short and full must describe the SAME commit; pass both or neither. Empty
// strings are written through as empty (the non-git case), NOT treated as
// "leave whatever m already holds" — a save must never leave the two fields
// disagreeing with what the caller asked for.
func SaveManifestAtCommit(stateDir string, m *Manifest, short, full string) error {
	m.IndexedAt = time.Now().UTC()
	m.GitCommit = short
	m.GitCommitFull = full

	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}

	// Atomic write: write to a temp file then rename.
	tmp := filepath.Join(stateDir, manifestFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write manifest tmp: %w", err)
	}
	dst := filepath.Join(stateDir, manifestFile)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}

// newManifest returns an empty, valid manifest.
func newManifest() *Manifest {
	return &Manifest{
		Version: Version,
		Files:   make(map[string]FileEntry),
	}
}

// Filter partitions relPaths into (changed, unchanged).
//
// A file is "changed" when:
//   - It has no entry in the manifest (new file), or
//   - Its mtime or size differs from the manifest entry AND its SHA-256
//     content hash differs (two-stage check: fast stat, then hash only on
//     mtime/size mismatch).
//
// relPaths must be repo-relative (forward-slash, no leading slash) as
// returned by walk.WalkRepo. absRepo is the absolute repo root; it is
// joined with each relPath to form the absolute path for stat/hash.
//
// Cross-file invalidation: any relPath whose basename (import target) appears
// as a changed file's basename is also marked changed. This is a conservative
// approximation — a proper import-graph traversal is left for a future pass.
func Filter(absRepo string, relPaths []string, manifest *Manifest) (changed, unchanged []string) {
	// Phase 1: classify each file as dirty or clean.
	dirty := make(map[string]bool, len(relPaths))
	for _, rel := range relPaths {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if isChanged(abs, rel, manifest) {
			dirty[rel] = true
		}
	}

	// Phase 2: cross-file invalidation.
	// Build a set of base names (without extension) of dirty files, then mark
	// any file whose own base name suffix-matches a dirty name as also dirty.
	// This catches "anyone that might import a changed module".
	dirtyBases := make(map[string]bool, len(dirty))
	for rel := range dirty {
		dirtyBases[moduleBase(rel)] = true
	}
	for _, rel := range relPaths {
		if dirty[rel] {
			continue
		}
		if dirtyBases[moduleBase(rel)] {
			dirty[rel] = true
		}
	}

	changed = make([]string, 0, len(dirty))
	unchanged = make([]string, 0, len(relPaths)-len(dirty))
	for _, rel := range relPaths {
		if dirty[rel] {
			changed = append(changed, rel)
		} else {
			unchanged = append(unchanged, rel)
		}
	}
	return changed, unchanged
}

// UpdateManifest records the current on-disk state for every file in
// relPaths into m. Call this after a successful index write so the next
// incremental run has accurate baseline hashes.
//
// This is the O(repo) form: it opens and SHA-256s every path it is given. Use
// it where the caller has just processed the whole repo anyway. A caller that
// already knows which files changed — and is about to discard the graph work,
// not commit it — wants UpdateManifestScoped instead (#6201).
func UpdateManifest(absRepo string, relPaths []string, m *Manifest) {
	UpdateManifestScoped(absRepo, relPaths, relPaths, m)
}

// UpdateManifestScoped re-stamps only hashPaths, while reconciling manifest
// MEMBERSHIP against keepPaths.
//
// The split exists because the two halves of UpdateManifest have wildly
// different costs and only one of them is always needed (#6201):
//
//   - Re-stamping is O(files hashed) and requires reading each file. On a
//     3003-file fixture the full-repo sweep measured ~220 ms of the 696 ms that
//     the too-many-changed reject path used to burn and then throw away.
//   - The membership prune is a pair of map walks — effectively free — and it
//     is the half that #5667 needs: an entry for a file no longer in the
//     gitignore-aware walk is otherwise immortal, is reported as "deleted" on
//     every pass, perpetually trips the too-many-changed fallback, and pins the
//     daemon in a reindex loop.
//
// Passing the changed-file set as hashPaths and the full walk as keepPaths
// therefore keeps both loop guards (#5667's prune and #5668's stamp refresh for
// the files that actually moved) at O(delta) instead of O(repo). Files in
// keepPaths but not hashPaths keep whatever stamp they already had, which is
// correct precisely because the caller has established they did not change.
func UpdateManifestScoped(absRepo string, hashPaths, keepPaths []string, m *Manifest) {
	var mu sync.Mutex
	// Best-effort parallel hash for large repos.
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, rel := range hashPaths {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			abs := filepath.Join(absRepo, filepath.FromSlash(r))
			entry, err := hashFile(abs)
			if err != nil {
				return
			}
			mu.Lock()
			// #6209 — a re-hash must not silently clear the retry budget. This
			// writes a fresh FileEntry, whose Failures is zero, over an entry
			// that may be carrying a count; the too-many-changed reject at
			// incremental.go passes exactly the changed set, and a retry-due
			// file is IN it by construction. Zeroing there restarts the budget
			// on every reject, which is the unbounded retry this design
			// rejected, reached by accident.
			//
			// Same rule as ApplyStampsAndFailures: identical bytes keep the
			// count, different bytes earn a fresh budget (here, zero — the file
			// changed, so it is being re-extracted on its own merits anyway).
			if prev, ok := m.Files[r]; ok && prev.SHA256 == entry.SHA256 {
				entry.Failures = prev.Failures
			}
			m.Files[r] = entry
			mu.Unlock()
		}(rel)
	}
	wg.Wait()

	reconcileMembership(keepPaths, m)
}

// ApplyStamps records stamps that were computed ELSEWHERE — at walk time, from
// the bytes the extraction pipeline actually read — and then reconciles
// membership against keepPaths exactly as UpdateManifestScoped does.
//
// It performs NO file I/O. That is the whole point (#6212): the manifest's
// contract is "these bytes are what the graph was built from", and a re-hash at
// commit time cannot honour it, because the document was built from the bytes
// the walk read and the working tree may have moved on since. Hashing at the
// point of reading makes the contract true BY CONSTRUCTION rather than true
// whenever no write happened to land in a multi-second window. It also deletes
// the O(repo) SHA-256 sweep the commit used to pay (#6206).
//
// Files in keepPaths with no stamp keep whatever entry they already had — the
// same rule UpdateManifestScoped applies to its unhashed keepPaths, and correct
// for the same reason: the caller has established they did not change.
func ApplyStamps(stamps map[string]FileEntry, keepPaths []string, m *Manifest) {
	if m.Files == nil {
		m.Files = make(map[string]FileEntry, len(stamps))
	}
	for rel, e := range stamps {
		m.Files[rel] = e
	}
	reconcileMembership(keepPaths, m)
}

// ApplyStampsAndFailures is ApplyStamps plus the record of which of those
// stamped files FAILED to extract on this pass (#6209).
//
// A stamp asserts "the graph was built from these bytes". For a file whose
// extractor returned an error that assertion is false — the pipeline read the
// bytes and produced nothing from them — and it is self-concealing: the next
// pass hash-matches the stamp, classifies the file unchanged, and the missing
// entity set is never noticed. Dropping the stamp instead would heal that, but
// only by making a DETERMINISTICALLY unparseable file re-read and re-parse on
// every pass forever. So the entry stays and carries a consecutive-failure
// count, and retryDue spends it over the next MaxExtractRetries passes.
//
// The count must survive ApplyStamps, which overwrites whole entries, so the
// prior counts are snapshotted first. Two resets are deliberate:
//
//   - A file that is NOT in failed keeps whatever ApplyStamps wrote, i.e. a
//     zero count. One successful extraction clears the history; the count is
//     "consecutive", not "lifetime".
//   - A file that failed again but at DIFFERENT bytes restarts at one. New
//     content is the only new information that could change the outcome, so it
//     earns a fresh budget rather than inheriting a spent one.
//
// failed holds repo-relative paths, the same key space as stamps. Paths absent
// from the manifest after the stamp+prune — never stamped at all, or pruned out
// of the walk — are skipped: an absent entry already re-presents as new, which
// is the stronger signal.
//
// It returns the paths whose count CROSSED MaxExtractRetries on this call —
// the moment each file stops being retried and its missing entities become
// permanent until someone edits it. That crossing is the one event in this
// mechanism a human should hear about, and it happens exactly once per file, so
// the caller logs it rather than the manifest quietly absorbing it.
func ApplyStampsAndFailures(stamps map[string]FileEntry, keepPaths []string, failed map[string]bool, m *Manifest) (exhausted []string) {
	prior := make(map[string]FileEntry, len(failed))
	for rel := range failed {
		if e, ok := m.Files[rel]; ok {
			prior[rel] = e
		}
	}

	ApplyStamps(stamps, keepPaths, m)

	for rel := range failed {
		e, ok := m.Files[rel]
		if !ok {
			continue
		}
		if p, had := prior[rel]; had && p.SHA256 == e.SHA256 {
			e.Failures = p.Failures + 1
		} else {
			e.Failures = 1
		}
		m.Files[rel] = e
		if e.Failures == MaxExtractRetries+1 {
			exhausted = append(exhausted, rel)
		}
	}
	sort.Strings(exhausted)
	return exhausted
}

// RetryDue reports whether an entry's extraction failure is still within its
// retry budget, and must therefore be forced back into the changed set even
// though its bytes are unchanged (#6209).
//
// Both bounds matter. Failures == 0 is the ordinary case and must not be
// disturbed. Failures > MaxExtractRetries is the file that has had its chances:
// leaving it dirty forever would re-read and re-parse it on every pass for the
// life of the repo, which is a worse trade than the gap it was fixing.
//
// EXPORTED BECAUSE ONE UNION IS NOT ENOUGH. internal/extractors.TryIncremental —
// the daemon's per-tick path — re-checks content hashes of its own in the Step-3
// AST-hash gate, downstream of everything this package decides. That gate
// compares the same SHA-256 over the same bytes, so a failed file (whose bytes
// are unchanged by construction) is dropped straight back out unless the caller
// consults this predicate too. Any future gate that asks "did the bytes change?"
// has to ask this as well, or the retry dies there silently.
func RetryDue(e FileEntry) bool {
	return e.Failures > 0 && e.Failures <= MaxExtractRetries
}

// reconcileMembership drops entries for files no longer in the walked set (#5667)
// so the manifest cannot retain stale records — e.g. a file that became
// gitignored (build artifacts now excluded by walk.WalkRepo). All callers pass
// the complete current walk as keepPaths, so membership in it is authoritative.
// Without this prune an entry, once added, is immortal: a now-ignored file is
// reported as "deleted" on every pass, perpetually tripping the
// too-many-changed full-reindex fallback and pinning the daemon.
func reconcileMembership(keepPaths []string, m *Manifest) {
	want := make(map[string]struct{}, len(keepPaths))
	for _, r := range keepPaths {
		want[r] = struct{}{}
	}
	for k := range m.Files {
		if _, ok := want[k]; !ok {
			delete(m.Files, k)
		}
	}
}

// GitChangedFilesSince uses `git diff --name-only fromCommit..HEAD` to return
// the set of repo-relative paths that differ between fromCommit and the
// current HEAD. This is the commit-RANGE counterpart to GitChangedFiles
// (which only sees working-tree-vs-HEAD): it is what detects a HEAD advance
// (fetch+reset / checkout / pull) that leaves a clean working tree against
// the new HEAD, where `git diff --name-only HEAD` reports nothing even
// though the indexed graph is still pinned at fromCommit (#5710).
//
// Returns an error when the diff cannot be computed (e.g. fromCommit is no
// longer reachable — shallow clone, rebase, gc) so the caller can treat the
// range as UNCONFIRMED rather than silently assuming nothing changed.
func GitChangedFilesSince(repoPath, fromCommit string) (map[string]bool, error) {
	if fromCommit == "" {
		return nil, fmt.Errorf("git diff range: empty fromCommit")
	}
	if _, ok := gitmeta.RunGitBoundedC(repoPath, "rev-parse", "--is-inside-work-tree"); !ok {
		return nil, nil // not a git repo (or git wedged) — not an error
	}

	rangeSpec := fromCommit + "..HEAD"
	diffOut, ok := gitmeta.RunGitBoundedC(repoPath, "diff", "--name-only", rangeSpec)
	if !ok {
		return nil, fmt.Errorf("git diff %s: bounded git failed (timeout, unreachable commit, or no HEAD)", rangeSpec)
	}

	changed := make(map[string]bool)
	sc := bufio.NewScanner(bytes.NewReader(diffOut))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			changed[line] = true
		}
	}
	return changed, nil
}

// HeadCommit returns the short HEAD commit hash for the repo at repoPath, or
// empty string if git is unavailable or this is not a git repo. Exported
// wrapper around headCommit for callers outside this package (e.g. the
// incremental extractor's HEAD-advance detection, #5710) that need to compare
// the manifest's last-indexed commit against the repo's current HEAD.
func HeadCommit(repoPath string) string {
	return headCommit(repoPath)
}

// GitChangedFiles uses `git diff --name-only HEAD` to return the set of
// repo-relative paths changed since the last HEAD commit. Returns nil when
// the repo is not a git repository or git is not available.
func GitChangedFiles(repoPath string) (map[string]bool, error) {
	// Verify this is a git repo. Bounded (#5286): a stuck git child during heavy
	// churn must not wedge the index worker — a timeout here is treated as "not
	// a git repo" so FilterWithGit falls back to hash comparison.
	if _, ok := gitmeta.RunGitBoundedC(repoPath, "rev-parse", "--is-inside-work-tree"); !ok {
		return nil, nil // not a git repo (or git wedged) — not an error
	}

	// git diff --name-only HEAD: tracked files that differ from HEAD.
	// Bounded: on timeout/error return an error so the caller falls back to the
	// full hash-based scan instead of hanging on a U-state git child (#5286).
	diffOut, ok := gitmeta.RunGitBoundedC(repoPath, "diff", "--name-only", "HEAD")
	if !ok {
		// HEAD may not exist in a brand-new repo, or git timed out; either way
		// signal a full-rebuild fallback rather than blocking.
		return nil, fmt.Errorf("git diff HEAD: bounded git failed (timeout or no HEAD)")
	}

	// git ls-files --others --exclude-standard: untracked new files (best-effort,
	// bounded). A timeout here just means we miss untracked files this pass.
	untrackedOut, _ := gitmeta.RunGitBoundedC(repoPath, "ls-files", "--others", "--exclude-standard")

	changed := make(map[string]bool)
	for _, buf := range [][]byte{diffOut, untrackedOut} {
		sc := bufio.NewScanner(bytes.NewReader(buf))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				// git outputs forward-slash paths already on all platforms.
				changed[line] = true
			}
		}
	}
	return changed, nil
}

// FilterWithGit is like Filter but uses git status as a fast first pass when
// the repo is a git repository. Only files reported by git as changed are
// handed to the hash-based Filter; the rest are assumed unchanged.
//
// Falls back to hash-based Filter when:
//   - The repo is not a git repository.
//   - git is not available.
//   - The last manifest commit equals HEAD (nothing changed according to git)
//     but there are new files not yet tracked.
func FilterWithGit(absRepo string, relPaths []string, manifest *Manifest) (changed, unchanged []string) {
	gitChanged, err := GitChangedFiles(absRepo)
	if err != nil || gitChanged == nil {
		// git unavailable or repo is not a git repo — fall back to hash comparison.
		return Filter(absRepo, relPaths, manifest)
	}

	// HEAD-ADVANCE UNION (#5964).
	//
	// GitChangedFiles only ever answers "working tree vs the CURRENT HEAD".
	// Trusting it alone is sound only while the manifest was written at the
	// commit the repo is on NOW. Whenever HEAD has moved since — a
	// fetch+reset / checkout / pull, or a worktree state dir SEEDED with its
	// parent ref's manifest (#5964) — every file that differs by a commit
	// looks clean to that diff and is never hashed, so its stale entities
	// survive into the graph silently.
	//
	// So union in the commit-RANGE diff manifest.GitCommit..HEAD. One extra
	// bounded `git diff`, and only when HEAD actually moved. When the range
	// cannot be computed at all — manifest.GitCommit unreachable after a gc,
	// shallow clone or history rewrite — we must NOT read that as "nothing
	// changed": fall back to the full hash comparison, which is slower but
	// cannot be silently stale.
	//
	// internal/extractors.TryIncremental performs the same union for its own
	// path (#5710). Doing it here covers Index()+WithIncremental too, which
	// had no HEAD-advance handling at all.
	if manifest != nil && manifest.GitCommit != "" {
		if head := headCommit(absRepo); head != "" && head != manifest.GitCommit {
			since, serr := GitChangedFilesSince(absRepo, manifest.GitCommit)
			if serr != nil {
				return Filter(absRepo, relPaths, manifest)
			}
			for rel := range since {
				gitChanged[rel] = true
			}
		}
	}

	// FAILED-EXTRACTION UNION (#6209).
	//
	// The isChanged gate alone is not enough on this path, because this path
	// does not consult isChanged for everything: a file git does not report is
	// trusted as unchanged and never hashed. A file whose extraction failed has
	// unchanged BYTES by construction — that is the whole problem — so git is
	// silent about it and the retry would never fire on the git-aware path,
	// which is the one the daemon and Index()+WithIncremental both take.
	//
	// Union it in here so it reaches isChanged, which then applies the same
	// budget. Costs one map walk over the manifest and, in the overwhelmingly
	// common case of no failures, adds nothing to the changed set.
	if manifest != nil {
		for rel, e := range manifest.Files {
			if RetryDue(e) {
				gitChanged[rel] = true
			}
		}
	}

	// git-aware path: files reported by git go through hash-based check;
	// files NOT reported by git are trusted as unchanged.
	var gitDirty, gitClean []string
	for _, rel := range relPaths {
		if gitChanged[rel] {
			gitDirty = append(gitDirty, rel)
		} else {
			gitClean = append(gitClean, rel)
		}
	}

	// Hash-check only the git-reported dirty files.
	dirtySet := make(map[string]bool)
	for _, rel := range gitDirty {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		if isChanged(abs, rel, manifest) {
			dirtySet[rel] = true
		}
	}

	// Cross-file invalidation within the git-dirty set.
	dirtyBases := make(map[string]bool, len(dirtySet))
	for rel := range dirtySet {
		dirtyBases[moduleBase(rel)] = true
	}
	// Re-check git-clean files only when a dirty base matches.
	var secondPassClean []string
	for _, rel := range gitClean {
		if dirtyBases[moduleBase(rel)] {
			dirtySet[rel] = true
		} else {
			secondPassClean = append(secondPassClean, rel)
		}
	}

	changed = make([]string, 0, len(dirtySet))
	unchanged = make([]string, 0, len(secondPassClean))
	for _, rel := range relPaths {
		if dirtySet[rel] {
			changed = append(changed, rel)
		}
	}
	unchanged = secondPassClean
	return changed, unchanged
}

// Stats holds incremental-run statistics surfaced to the caller.
type Stats struct {
	Total     int // total files discovered
	Changed   int // files that will be re-processed
	Unchanged int // files skipped (cache hit)
}

// CacheHitRate returns the cache-hit percentage (0–100).
func (s Stats) CacheHitRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return 100.0 * float64(s.Unchanged) / float64(s.Total)
}

// isChanged returns true when relPath must be re-extracted (new file, or mtime
// and size changed with a differing hash).
func isChanged(absPath, relPath string, manifest *Manifest) bool {
	entry, ok := manifest.Files[relPath]
	if !ok {
		return true // new file
	}
	// #6209: the bytes may be identical and the file still not be in the graph,
	// because the extractor that ran on them failed. Retry it, within budget,
	// before the hash gets a chance to say "unchanged" and hide the gap.
	if RetryDue(entry) {
		return true
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return true // assume changed on error
	}
	if info.Size() == entry.Size && info.ModTime().UnixNano() == entry.Mtime {
		return false // fast path: unchanged
	}
	// mtime or size differs — verify with hash.
	newEntry, err := hashFile(absPath)
	if err != nil {
		return true
	}
	return newEntry.SHA256 != entry.SHA256
}

// hashCalls counts every hashFile invocation in the process.
//
// It exists so a test can assert that a code path performs NO file hashing at
// all — the #6212 acceptance criterion for commitManifest, and the shape #6206
// asks for in general ("assert on observable work — a hash counter, a seam —
// never on elapsed time"). A wall-clock budget is the test-that-cannot-fail
// shape; a counter that must read zero cannot pass for the wrong reason.
//
// Monotonic and process-wide: callers compare two samples, never the absolute.
var hashCalls atomic.Int64

// HashCallCount returns the number of files hashed by this package so far.
// See hashCalls for why it is exported.
func HashCallCount() int64 { return hashCalls.Load() }

// HashFile is hashFile, exported for the callers that must produce a manifest
// stamp for a file the extraction pipeline never reads — a binary, an oversized
// file, an unsupported extension (#6212). Everything the pipeline DOES read is
// stamped with StampBytes, from the bytes in hand, without reopening the file.
func HashFile(path string) (FileEntry, error) { return hashFile(path) }

// StampBytes builds the manifest entry for content a caller already holds.
//
// This is the #6212 primitive: the hash is over the bytes PASSED IN — the ones
// the graph is being built from — never over a re-read of the path. Re-reading
// is the whole defect, because "the same file" at two different moments is not
// the same bytes, and the manifest that results claims content the graph does
// not contain.
//
// statSize/statMtime must come from a stat taken BEFORE the read. They feed
// isChanged's fast path only. A stat taken after the read can pair post-write
// metadata with pre-write content, and the next pass then takes the fast path
// and calls the file unchanged; a pre-read stat can only be staler than the
// bytes, which routes the next pass to the hash instead. statSize < 0 means the
// stat failed, and len(content) stands in.
func StampBytes(content []byte, statSize, statMtime int64) FileEntry {
	sum := sha256.Sum256(content)
	if statSize < 0 {
		statSize = int64(len(content))
	}
	return FileEntry{
		SHA256: hex.EncodeToString(sum[:]),
		Size:   statSize,
		Mtime:  statMtime,
	}
}

// hashFile computes the SHA-256 of the file at path and returns a FileEntry.
func hashFile(path string) (FileEntry, error) {
	hashCalls.Add(1)
	f, err := os.Open(path)
	if err != nil {
		return FileEntry{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return FileEntry{}, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return FileEntry{}, err
	}
	return FileEntry{
		SHA256: hex.EncodeToString(h.Sum(nil)),
		Size:   info.Size(),
		Mtime:  info.ModTime().UnixNano(),
	}, nil
}

// moduleBase returns the stem of a file path without extension, used for
// conservative cross-file invalidation (e.g. "src/user.go" → "user").
func moduleBase(relPath string) string {
	base := filepath.Base(relPath)
	if ext := filepath.Ext(base); ext != "" {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

// headCommit returns the short HEAD commit hash for the repo at repoPath, or
// empty string if git is unavailable or this is not a git repo.
func headCommit(repoPath string) string {
	// Bounded (#5286): never let a stuck git child hang the index worker.
	out, ok := gitmeta.RunGitBoundedC(repoPath, "rev-parse", "--short", "HEAD")
	if !ok {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// headCommitFull returns the FULL (40-char) HEAD commit hash for the repo at
// repoPath, or empty string if git is unavailable or this is not a git repo.
// Companion to headCommit (short); both are captured in the same SaveManifest
// call so they always agree on which commit they describe (#5727/#5729-W1).
func headCommitFull(repoPath string) string {
	out, ok := gitmeta.RunGitBoundedC(repoPath, "rev-parse", "HEAD")
	if !ok {
		return ""
	}
	return strings.TrimSpace(string(out))
}
