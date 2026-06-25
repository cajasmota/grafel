// Commit-coupling soft-edge layer — VCS-derived co-change signal (issue #21).
//
// # What
//
// At index time, mine the repo's git history (`git log --name-only`) to build a
// co-change matrix: for every commit, every unordered pair of files that
// appear together in that commit contributes +1 to a support counter. Pairs
// whose support meets a minimum threshold are emitted as `COMMIT_COUPLED`
// soft-edges between synthetic `File` entities, with
// `Properties = {confidence, support}` where `confidence = support /
// total_commits`.
//
// # Why
//
// Files that change together over time often share logical coupling that
// static analysis misses — a handler and its test, an infra YAML and the
// feature it follows, a JSON schema and its consumers. Surfacing this as a
// soft edge lets downstream algorithms (impact-radius, community detection,
// docs grouping) opt in to a richer view without changing default graph
// topology.
//
// # Soft-edge contract
//
// COMMIT_COUPLED edges are **append-only** and live on synthetic File
// entities (Kind="File", Properties["synthetic"]="true"). They do not
// influence:
//
//   - Pass 4 graph algorithms (community/centrality/PageRank). Those run
//     before this pass and operate on real CALLS/IMPORTS/etc. edges only.
//   - Module aggregation. The module-agg pass has already produced
//     DEPENDS_ON edges and does not see File entities.
//   - The HTTP endpoint resolver, process-flow BFS, or enrichment emitters.
//
// Consumers that *want* the signal (e.g. an opt-in impact-radius mode, a
// "what changes with me?" MCP tool) can filter by `Kind == "COMMIT_COUPLED"`.
// Consumers that ignore it see the synthetic File nodes only as detached
// containers — by convention they filter `entity.Kind != "File"` (mirroring
// the `entity.Kind != "Module"` pattern used elsewhere).
//
// # Algorithm
//
//  1. Detect git availability: `git -C <repo> rev-parse --is-inside-work-tree`.
//     If the repo is not a git working tree (or `git` is unavailable), the
//     pass logs a single line and returns an empty result.
//
//  2. Stream `git log --no-merges --pretty=format:%H --name-only` and group
//     lines into commits. Each commit is the set of files touched. Merge
//     commits are skipped — they aggregate many unrelated file changes and
//     would dominate the support count with spurious coupling.
//
//  3. For each commit, enumerate the unordered pairs (i<j) of its file set
//     and accumulate `support[pair]++`.
//
//  4. Filter: keep only pairs with `support >= MinSupport` (default 5). For
//     each kept pair, emit:
//     - Two synthetic `File` entities (if not already emitted).
//     - One `COMMIT_COUPLED` relationship between them, with
//     `Properties["support"] = "<n>"`,
//     `Properties["confidence"] = "<f>"` (formatted with 4 decimals).
//
// # Determinism
//
// The git log order is deterministic by commit hash, and we sort each commit's
// file list before enumerating pairs. Output entities and edges are sorted
// before append so the resulting graph.Document is byte-identical across runs
// on the same repo state.
//
// # Performance
//
// `git log --name-only` over a large repo (>10k commits) produces a few MB of
// stdout — we stream-parse it via bufio.Scanner so memory stays bounded. The
// pair enumeration is O(sum k^2) where k is the file count per commit; merge
// skipping bounds k in practice. For a repo with 10k non-merge commits at
// average k=5, that is 250k pair updates — sub-second.
//
// # Refs
//
// Fixes #21 (v1.1 commit-coupling layer).
package engine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/executil"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
)

// KindCommitCoupled is the relationship kind emitted by this pass.
const KindCommitCoupled = "COMMIT_COUPLED"

// KindFile is the synthetic entity kind used as endpoints for commit-coupling
// edges. We use "File" rather than reusing "Module" because module rollup is
// coarser than per-file granularity, and the co-change signal is inherently
// per-file.
const KindFile = "File"

// DefaultMinSupport is the default minimum number of commits a file pair
// must co-occur in before a COMMIT_COUPLED edge is emitted.
//
// Set per spec (#21): 5 is high enough to filter noise from one-off refactor
// commits that touch many files, and low enough to catch genuine recurring
// coupling on younger repos.
const DefaultMinSupport = 5

// DefaultGitLogTimeout caps the git-log subprocess to avoid pathological hangs
// on huge or wedged repos.
const DefaultGitLogTimeout = 60 * time.Second

// MaxFilesPerCommit is a guard against catastrophically large commits (vendor
// drops, lockfile mass-updates) that would explode pair enumeration. Commits
// touching more than this many files are skipped. 200 covers virtually all
// real-world feature commits.
const MaxFilesPerCommit = 200

// CommitCouplingConfig tunes the pass. Zero values fall back to defaults so
// callers can pass `CommitCouplingConfig{}` for stock behaviour.
type CommitCouplingConfig struct {
	// MinSupport is the minimum number of commits a pair must co-occur in
	// before we emit an edge. Defaults to DefaultMinSupport.
	MinSupport int

	// GitLogTimeout caps the git-log subprocess. Defaults to
	// DefaultGitLogTimeout.
	GitLogTimeout time.Duration

	// MaxFilesPerCommit skips commits touching more than this many files.
	// Defaults to MaxFilesPerCommit.
	MaxFilesPerCommit int
}

// DefaultCommitCouplingConfig returns the stock configuration.
func DefaultCommitCouplingConfig() CommitCouplingConfig {
	return CommitCouplingConfig{
		MinSupport:        DefaultMinSupport,
		GitLogTimeout:     DefaultGitLogTimeout,
		MaxFilesPerCommit: MaxFilesPerCommit,
	}
}

// CommitCouplingStats summarises a single Apply call.
type CommitCouplingStats struct {
	// TotalCommits is the number of commits scanned (post-filter:
	// excludes merges and oversize commits).
	TotalCommits int

	// SkippedMergeCommits counts merge commits dropped during scan.
	SkippedMergeCommits int

	// SkippedOversizeCommits counts commits dropped for exceeding
	// MaxFilesPerCommit.
	SkippedOversizeCommits int

	// CandidatePairs is the number of distinct file pairs observed at
	// least once.
	CandidatePairs int

	// FileEntities is the number of synthetic File entities emitted.
	FileEntities int

	// CoupledEdges is the number of COMMIT_COUPLED edges emitted.
	CoupledEdges int

	// Skipped is true when the pass returned without scanning (non-git
	// repo, git binary missing, or git error).
	Skipped bool

	// SkipReason records why the pass was skipped, when Skipped is true.
	SkipReason string
}

// ApplyCommitCoupling runs the pass over doc, mining commit history from
// repoPath. It appends synthetic File entities and COMMIT_COUPLED edges to
// doc.Entities / doc.Relationships and updates doc.Stats.
//
// The pass is best-effort: any git failure is logged in stats.SkipReason and
// the document is returned unchanged. Callers should treat a Skipped result
// as a non-error outcome.
func ApplyCommitCoupling(doc *graph.Document, repoPath string, cfg CommitCouplingConfig) CommitCouplingStats {
	stats := CommitCouplingStats{}
	if doc == nil {
		stats.Skipped = true
		stats.SkipReason = "nil document"
		return stats
	}

	cfg = withDefaults(cfg)

	// Detect git working tree. The check uses `git rev-parse` rather than
	// stat-ing .git because submodules and git-worktrees use a .git file
	// (pointing at the real gitdir) instead of a directory.
	if !isGitRoot(repoPath) {
		stats.Skipped = true
		stats.SkipReason = "not a git root"
		return stats
	}

	commits, scanStats, err := scanCommitHistory(repoPath, cfg)
	stats.SkippedMergeCommits = scanStats.SkippedMergeCommits
	stats.SkippedOversizeCommits = scanStats.SkippedOversizeCommits
	if err != nil {
		stats.Skipped = true
		stats.SkipReason = fmt.Sprintf("git log failed: %v", err)
		return stats
	}
	stats.TotalCommits = len(commits)
	if stats.TotalCommits == 0 {
		stats.Skipped = true
		stats.SkipReason = "no commits scanned"
		return stats
	}

	// Build the co-change support map.
	support := buildSupport(commits)
	stats.CandidatePairs = len(support)

	// Filter to high-support pairs.
	kept := filterPairs(support, cfg.MinSupport)
	if len(kept) == 0 {
		return stats
	}

	// Emit synthetic File entities for every endpoint touched by a kept
	// pair, then emit COMMIT_COUPLED edges. Dedup against any entities/
	// edges already in the document (idempotency).
	existingEntities := make(map[string]bool, len(doc.Entities))
	for k := range doc.Entities {
		existingEntities[doc.Entities[k].ID] = true
	}
	existingRels := make(map[string]bool, len(doc.Relationships))
	for k := range doc.Relationships {
		existingRels[doc.Relationships[k].ID] = true
	}

	totalCommits := stats.TotalCommits

	// Collect endpoint files in sorted order for deterministic emission.
	fileSet := make(map[string]struct{})
	for _, p := range kept {
		fileSet[p.a] = struct{}{}
		fileSet[p.b] = struct{}{}
	}
	sortedFiles := make([]string, 0, len(fileSet))
	for f := range fileSet {
		sortedFiles = append(sortedFiles, f)
	}
	sort.Strings(sortedFiles)

	for _, f := range sortedFiles {
		fid := fileEntityID(doc.Repo, f)
		if existingEntities[fid] {
			continue
		}
		existingEntities[fid] = true
		props := map[string]string{
			"path":      f,
			"repo":      doc.Repo,
			"synthetic": "true",
			"source":    "commit-coupling",
		}
		// Issue #2354 — stamp "module" on every synthetic File entity so these
		// late-appended nodes satisfy the module-coverage invariant checked by
		// TestModuleCoverage_AllEntitiesTagged. We pass nil for the MarkerSet;
		// module.Derive's depth-N fallback is sufficient for file-path rollup.
		props = module.EnsureModule(props, f, nil)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         fid,
			Name:       f,
			Kind:       KindFile,
			SourceFile: f,
			Properties: props,
		})
		stats.FileEntities++
	}

	// Sort kept pairs for deterministic edge emission.
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].a != kept[j].a {
			return kept[i].a < kept[j].a
		}
		return kept[i].b < kept[j].b
	})

	for _, pair := range kept {
		aID := fileEntityID(doc.Repo, pair.a)
		bID := fileEntityID(doc.Repo, pair.b)
		// Stable edge ID uses lexicographic ordering of endpoints so the
		// undirected pair always hashes the same way regardless of how the
		// `a`/`b` happen to be ordered upstream.
		fromID, toID := aID, bID
		if pair.b < pair.a {
			fromID, toID = bID, aID
		}
		eid := graph.RelationshipID(fromID, toID, KindCommitCoupled)
		if existingRels[eid] {
			continue
		}
		existingRels[eid] = true
		conf := float64(pair.support) / float64(totalCommits)
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     eid,
			FromID: fromID,
			ToID:   toID,
			Kind:   KindCommitCoupled,
			Properties: map[string]string{
				"support":    strconv.Itoa(pair.support),
				"confidence": strconv.FormatFloat(conf, 'f', 4, 64),
			},
		})
		stats.CoupledEdges++
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	return stats
}

// withDefaults fills zero-valued fields with their package defaults.
func withDefaults(cfg CommitCouplingConfig) CommitCouplingConfig {
	if cfg.MinSupport <= 0 {
		cfg.MinSupport = DefaultMinSupport
	}
	if cfg.GitLogTimeout <= 0 {
		cfg.GitLogTimeout = DefaultGitLogTimeout
	}
	if cfg.MaxFilesPerCommit <= 0 {
		cfg.MaxFilesPerCommit = MaxFilesPerCommit
	}
	return cfg
}

// isGitRoot reports whether repoPath is the root of a git working tree (i.e.
// the path returned by `git rev-parse --show-toplevel`).
//
// Two conditions must both hold:
//  1. repoPath is inside a git working tree (rev-parse --is-inside-work-tree).
//  2. repoPath IS the git toplevel (rev-parse --show-toplevel equals repoPath).
//
// Condition 2 prevents the commit-coupling pass from mining the surrounding
// repo's git history when repoPath is a subdirectory of a larger git repo
// (e.g. a test fixture nested inside the grafel repo itself). In that
// case git -C <subdir> walks up to the parent repo root, and the resulting
// 1000+ commit history from the wrong repo would flood the document with
// synthetic File entities for every file that has EVER appeared in the parent
// repo — none of which belong to the indexed fixture. See issue #2334.
func isGitRoot(repoPath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Step 1: quick guard — must be inside a working tree at all.
	insideCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	executil.NoWindow(insideCmd)
	insideOut, err := insideCmd.Output()
	if err != nil || strings.TrimSpace(string(insideOut)) != "true" {
		return false
	}

	// Step 2: confirm repoPath is the toplevel, not a subdirectory.
	// Use a fresh context so the 5-second budget isn't already exhausted.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	topCmd := exec.CommandContext(ctx2, "git", "-C", repoPath, "rev-parse", "--show-toplevel")
	executil.NoWindow(topCmd)
	topOut, err := topCmd.Output()
	if err != nil {
		return false
	}
	toplevel := filepath.Clean(strings.TrimSpace(string(topOut)))
	// Resolve symlinks on both sides before comparing: on macOS /var is a
	// symlink to /private/var, so filepath.Clean alone can produce a mismatch
	// when repoPath was obtained from os.MkdirTemp / t.TempDir. EvalSymlinks
	// is best-effort; fall back to the cleaned path if it fails.
	if resolved, err2 := filepath.EvalSymlinks(repoPath); err2 == nil {
		repoPath = resolved
	}
	if resolved, err2 := filepath.EvalSymlinks(toplevel); err2 == nil {
		toplevel = resolved
	}
	return filepath.Clean(repoPath) == filepath.Clean(toplevel)
}

// commitRecord is one scanned commit's set of files.
type commitRecord struct {
	files []string // sorted, deduped, repo-relative
}

// scanCommitHistory runs `git log --no-merges --pretty=format:%H --name-only`
// and returns one commitRecord per non-merge commit, skipping commits that
// exceed cfg.MaxFilesPerCommit.
func scanCommitHistory(repoPath string, cfg CommitCouplingConfig) ([]commitRecord, CommitCouplingStats, error) {
	stats := CommitCouplingStats{}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.GitLogTimeout)
	defer cancel()

	// --no-merges already filters merge commits at the git level. We still
	// surface a SkippedMergeCommits counter via a second cheap pass if we
	// ever decide to include them; for now it stays 0 by construction.
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "log",
		"--no-merges",
		"--pretty=format:__grafel_commit__:%H",
		"--name-only",
	)
	executil.NoWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, stats, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, stats, fmt.Errorf("start: %w", err)
	}

	commits, parseErr := parseGitLog(stdout, cfg.MaxFilesPerCommit, &stats)

	// Drain remaining output if parse aborted early, then wait.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return nil, stats, parseErr
	}
	if waitErr != nil {
		// Non-zero exit (e.g. empty repo) — surface stderr for debugging.
		return nil, stats, fmt.Errorf("git log exit: %v: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return commits, stats, nil
}

// parseGitLog streams a `git log --name-only` formatted stream and groups
// lines into commitRecord values. The format is:
//
//	__grafel_commit__:<HASH>
//	path/one
//	path/two
//	<blank line>
//	__grafel_commit__:<NEXT-HASH>
//	...
//
// Commits exceeding maxFiles are dropped and counted in stats.
func parseGitLog(r io.Reader, maxFiles int, stats *CommitCouplingStats) ([]commitRecord, error) {
	const marker = "__grafel_commit__:"
	sc := bufio.NewScanner(r)
	// git log can produce long file lines and very long commit lists; raise
	// the line buffer to 1 MiB so we never hit ErrTooLong on realistic repos.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var commits []commitRecord
	var cur []string
	inCommit := false

	flush := func() {
		if !inCommit {
			return
		}
		if len(cur) == 0 {
			// Commit with no file changes (e.g. an empty / git commit
			// --allow-empty). Skip — contributes nothing to coupling.
			return
		}
		if len(cur) > maxFiles {
			stats.SkippedOversizeCommits++
			return
		}
		// Dedup + sort for deterministic pair enumeration.
		sort.Strings(cur)
		deduped := cur[:0]
		var last string
		for i, p := range cur {
			if i > 0 && p == last {
				continue
			}
			deduped = append(deduped, p)
			last = p
		}
		commits = append(commits, commitRecord{files: append([]string(nil), deduped...)})
	}

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, marker) {
			flush()
			cur = cur[:0]
			inCommit = true
			continue
		}
		if !inCommit {
			continue
		}
		if line == "" {
			continue
		}
		cur = append(cur, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	flush()
	return commits, nil
}

// pairKey is an ordered (a < b) pair of file paths, used as a map key for the
// support counter. Using a struct keeps allocations down vs string concat.
type pairKey struct {
	a string
	b string
}

// keptPair carries a pair plus its support count out of the filter step.
type keptPair struct {
	a       string
	b       string
	support int
}

// buildSupport enumerates unordered pairs from every commit and returns the
// support map.
func buildSupport(commits []commitRecord) map[pairKey]int {
	support := make(map[pairKey]int)
	for _, c := range commits {
		// files are already sorted+deduped by parseGitLog; iterate i<j to
		// emit each unordered pair exactly once.
		n := len(c.files)
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				key := pairKey{a: c.files[i], b: c.files[j]}
				support[key]++
			}
		}
	}
	return support
}

// filterPairs returns the pairs whose support meets the threshold.
func filterPairs(support map[pairKey]int, minSupport int) []keptPair {
	kept := make([]keptPair, 0)
	for k, n := range support {
		if n >= minSupport {
			kept = append(kept, keptPair{a: k.a, b: k.b, support: n})
		}
	}
	return kept
}

// fileEntityID returns the stable 16-char hex ID used for synthetic File
// entities. The path is used as both name and sourceFile, mirroring how
// real Entity IDs encode source location.
func fileEntityID(repo, path string) string {
	return graph.EntityID(repo, KindFile, path, path)
}
