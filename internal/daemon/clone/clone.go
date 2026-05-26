// Package clone implements the PH7 clone-from-parent optimisation for
// new branch indexing (part of epic #2087 / issue #2099).
//
// When a new ref has never been indexed (no graph.fb present), this
// package checks whether a close ancestor ref's graph can be seeded as
// a starting point. If the diff between the ancestor and the new ref is
// small (≤ ARCHIGRAPH_CLONE_MAX_FILES, default 20), we:
//
//  1. Copy the parent's graph.fb (and side-cars) to the new ref's store dir.
//  2. Patch the metadata header (indexed_ref / indexed_sha / computed_at) by
//     doing a streaming read-modify-write through graph.LoadGraphFromDir +
//     fbwriter.WriteAtomic (no in-place FB mutation required).
//  3. Re-extract only the changed files: remove stale entities/rels for
//     those files, run the language extractor callback, merge new results.
//  4. Persist the updated graph.fb atomically.
//
// Typical speedup on a 6k-entity repo with 5 changed files: ~5 s → ~180 ms.
//
// If any precondition fails, or any step errors, the partially-built
// graph is removed and the caller falls through to a full reindex.
//
// Env:
//
//	ARCHIGRAPH_CLONE_MAX_FILES  – max changed-file count to attempt clone
//	                              (default 20, maximum 100).
package clone

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	daemon "github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/gitmeta"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

// defaultMaxFiles is the default upper bound on the diff size that
// triggers clone-from-parent instead of full reindex.
const defaultMaxFiles = 20

// maxAllowedFiles is an absolute cap so a misconfigured env-var cannot
// cause the optimisation to run on very large diffs.
const maxAllowedFiles = 100

// maxMergeBaseAgeDays is how old the merge-base commit can be (in days)
// before we refuse to clone from it.
const maxMergeBaseAgeDays = 30

// gitCmdTimeout is the per-git-command timeout used throughout this
// package. 5 s is generous for local operations (diff, merge-base).
const gitCmdTimeout = 5 * time.Second

// sidecarFiles are the state-dir files copied alongside graph.fb.
var sidecarFiles = []string{
	"graph-stats.json",
	"enrichment-candidates.json",
	"repair.json",
}

// Result is returned by TryClone. It describes whether the clone path
// was taken and the timing.
type Result struct {
	// Done is true when the clone succeeded and the new ref is now indexed.
	Done bool
	// ParentRef is the ancestor ref that was used as the seed.
	ParentRef string
	// ChangedFiles is the number of files that were re-extracted.
	ChangedFiles int
	// Took is the wall-clock duration of the clone operation.
	Took time.Duration
}

// Config wires TryClone. ReExtractFiles is required for a useful clone.
type Config struct {
	// Logger receives structured log lines. nil → os.Stderr.
	Logger *slog.Logger

	// ReExtractFiles re-indexes a list of files within a repo and returns
	// the updated graph document. The passed document is the cloned base;
	// the callback removes stale entities/rels for the changed files and
	// re-runs the language extractor for each file.
	//
	// Required. If nil, TryClone always returns Result{Done:false}.
	ReExtractFiles func(repoPath string, changedFiles []string, base *graph.Document) (*graph.Document, error)
}

// TryClone attempts the clone-from-parent optimisation for (repoPath, newRef).
//
// Preconditions (all must hold; any failure → returns Result{Done:false}):
//  1. refs/<newRef>/graph.fb must NOT exist (new ref has no index).
//  2. A parent ref candidate must exist with a graph.fb on disk.
//  3. The merge-base commit between the parent and newRef must be within the
//     last maxMergeBaseAgeDays days.
//  4. git diff --name-only <parent>...newRef must return ≤ maxFiles files.
//
// On any step error: the partial graph is deleted, and (Result{Done:false}, nil)
// is returned so the caller falls through to full reindex. Errors are only
// returned for programming-level misconfigurations (nil callback, etc.); all
// git/IO failures are absorbed and surfaced as the abort path.
func TryClone(repoPath, newRef string, cfg Config) (Result, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "clone")
	}

	slug := filepath.Base(repoPath)
	abort := func(reason string) (Result, error) {
		logger.Info("clone-from-parent: ABORTED → full reindex", "repo", slug, "ref", newRef, "reason", reason)
		return Result{Done: false}, nil
	}

	if cfg.ReExtractFiles == nil {
		return abort("ReExtractFiles callback not configured")
	}

	// ------------------------------------------------------------------ //
	// Condition 1: new ref must have no existing graph.fb.
	// ------------------------------------------------------------------ //
	newRefDir := daemon.StateDirForRepoRef(repoPath, newRef)
	newGraphFB := filepath.Join(newRefDir, "graph.fb")
	if _, err := os.Stat(newGraphFB); err == nil {
		return abort("graph.fb already exists for new ref — skipping clone")
	}

	// ------------------------------------------------------------------ //
	// Condition 2 + 4: find best parent candidate and check diff size.
	// ------------------------------------------------------------------ //
	maxFiles := resolveMaxFiles()
	parentRef, changedFiles, err := findParent(repoPath, newRef, maxFiles)
	if err != nil {
		return abort(fmt.Sprintf("findParent: %v", err))
	}
	if parentRef == "" {
		return abort("no suitable parent ref with graph.fb and small-enough diff found")
	}
	// findParent already enforces the ≤ maxFiles guarantee, but be explicit.
	if len(changedFiles) > maxFiles {
		return abort(fmt.Sprintf("diff too large: %d files > limit %d", len(changedFiles), maxFiles))
	}

	// ------------------------------------------------------------------ //
	// Condition 3: merge-base age check.
	// ------------------------------------------------------------------ //
	if !mergeBaseRecent(repoPath, newRef, parentRef, maxMergeBaseAgeDays) {
		return abort(fmt.Sprintf("merge-base with %s is older than %d days", parentRef, maxMergeBaseAgeDays))
	}

	// ------------------------------------------------------------------ //
	// Step 2: copy parent's graph.fb + sidecars to new ref dir.
	// ------------------------------------------------------------------ //
	t0 := time.Now()
	parentRefDir := daemon.StateDirForRepoRef(repoPath, parentRef)
	parentGraphFB := filepath.Join(parentRefDir, "graph.fb")

	if err := os.MkdirAll(newRefDir, 0o755); err != nil {
		return abort(fmt.Sprintf("mkdir new ref dir: %v", err))
	}
	if err := copyFile(parentGraphFB, newGraphFB); err != nil {
		_ = os.Remove(newGraphFB)
		return abort(fmt.Sprintf("copy graph.fb: %v", err))
	}
	for _, sc := range sidecarFiles {
		src := filepath.Join(parentRefDir, sc)
		dst := filepath.Join(newRefDir, sc)
		if _, serr := os.Stat(src); serr == nil {
			_ = copyFile(src, dst) // best-effort; failures don't block indexing
		}
	}

	// Any step after this that fails must clean up so we don't leave a
	// half-built graph that might be served.
	cleanup := func() {
		os.Remove(newGraphFB)
		for _, sc := range sidecarFiles {
			os.Remove(filepath.Join(newRefDir, sc))
		}
	}

	// ------------------------------------------------------------------ //
	// Step 3: read-modify-write — load document, patch metadata header.
	//
	// Metadata update strategy: streaming read-modify-write.
	// We load the cloned graph.fb into a *graph.Document (O(N) decode via
	// loadFBDocument), update the three metadata fields, then rewrite via
	// fbwriter.WriteAtomic. This avoids in-place FlatBuffers mutation (which
	// requires knowing the exact byte offsets of string tables, error-prone
	// and fragile across schema changes).
	//
	// The FlatBuffers library panics on malformed input (out-of-bounds slice
	// access). We recover here so a corrupt parent graph never takes down the
	// daemon process — it just triggers the abort/fallback path.
	// ------------------------------------------------------------------ //
	doc, loadErr := safeLoadGraph(newRefDir)
	if loadErr != nil {
		cleanup()
		return abort(fmt.Sprintf("load cloned graph: %v", loadErr))
	}

	meta := gitmeta.Capture(repoPath)
	doc.IndexedRef = newRef
	if meta.SHA != "" {
		doc.IndexedSHA = meta.SHA
	}
	doc.GeneratedAt = time.Now().UTC()

	// ------------------------------------------------------------------ //
	// Step 4: re-extract changed files.
	// ------------------------------------------------------------------ //
	if len(changedFiles) > 0 {
		updated, rerr := cfg.ReExtractFiles(repoPath, changedFiles, doc)
		if rerr != nil {
			cleanup()
			return abort(fmt.Sprintf("re-extract %d changed files: %v", len(changedFiles), rerr))
		}
		doc = updated
	}

	// Recount aggregate stats (entity/rel counts may have changed).
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	// ------------------------------------------------------------------ //
	// Step 6: persist updated graph.fb atomically.
	// ------------------------------------------------------------------ //
	if err := fbwriter.WriteAtomic(newGraphFB, doc); err != nil {
		cleanup()
		return abort(fmt.Sprintf("write graph.fb: %v", err))
	}

	took := time.Since(t0)
	logger.Info("clone-from-parent: OK skip_full_reindex=true", "repo", slug, "ref", newRef, "from", parentRef, "changed_files", len(changedFiles), "took", took.Truncate(time.Millisecond))

	return Result{
		Done:         true,
		ParentRef:    parentRef,
		ChangedFiles: len(changedFiles),
		Took:         took,
	}, nil
}

// ------------------------------------------------------------------ //
// Internal helpers
// ------------------------------------------------------------------ //

// resolveMaxFiles reads ARCHIGRAPH_CLONE_MAX_FILES, clamped to
// [1, maxAllowedFiles]. Falls back to defaultMaxFiles.
func resolveMaxFiles() int {
	if s := os.Getenv("ARCHIGRAPH_CLONE_MAX_FILES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			if n > maxAllowedFiles {
				return maxAllowedFiles
			}
			return n
		}
	}
	return defaultMaxFiles
}

// findParent identifies the best parent ref candidate for newRef within
// repoPath. Returns ("", nil, nil) when no viable parent exists with a
// small-enough diff. The returned files list is the exact diff between
// parentRef and newRef.
//
// Candidate priority:
//  1. "main" (then "master")
//  2. "develop" if it exists
//  3. Remaining local branches (alphabetical)
//
// We select the candidate whose diff count is smallest and ≤ maxFiles.
// The parent's graph.fb must be present on disk (any tier).
func findParent(repoPath, newRef string, maxFiles int) (string, []string, error) {
	candidates := candidateRefs(repoPath)

	bestRef := ""
	var bestFiles []string

	for _, candidate := range candidates {
		if candidate == newRef {
			continue
		}
		// Graph.fb must be present on disk.
		candDir := daemon.StateDirForRepoRef(repoPath, candidate)
		if _, err := os.Stat(filepath.Join(candDir, "graph.fb")); err != nil {
			continue
		}
		// Compute diff.
		files, err := gitDiffFiles(repoPath, candidate, newRef)
		if err != nil {
			continue // git failure — skip this candidate
		}
		if len(files) > maxFiles {
			continue
		}
		if bestRef == "" || len(files) < len(bestFiles) {
			bestRef = candidate
			bestFiles = files
		}
	}
	return bestRef, bestFiles, nil
}

// candidateRefs returns an ordered list of refs to try as parent
// candidates. Fixed high-priority entries come first; then all other
// local branches in alphabetical order.
func candidateRefs(repoPath string) []string {
	priority := []string{"main", "master", "develop"}
	prioritySet := make(map[string]bool, len(priority))
	for _, p := range priority {
		prioritySet[p] = true
	}

	// All local branch names.
	all := gitmeta.RunGit(repoPath, "branch", "--format=%(refname:short)")
	var extra []string
	for _, b := range strings.Split(all, "\n") {
		b = strings.TrimSpace(b)
		if b != "" && !prioritySet[b] {
			extra = append(extra, b)
		}
	}
	return append(priority, extra...)
}

// gitDiffFiles returns the relative-path list of files that differ
// between fromRef and toRef (git diff --name-only fromRef...toRef).
// Returns an error only on git command failure.
func gitDiffFiles(repoPath, fromRef, toRef string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only",
		fromRef+"..."+toRef)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s...%s: %w", fromRef, toRef, err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// mergeBaseRecent returns true if the merge-base commit between ref1
// and ref2 is newer than ageDays days. Returns false on any git failure.
func mergeBaseRecent(repoPath, ref1, ref2 string, ageDays int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "merge-base", ref1, ref2)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	baseCommit := strings.TrimSpace(string(out))
	if baseCommit == "" {
		return false
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel2()
	cmd2 := exec.CommandContext(ctx2, "git", "show", "-s", "--format=%ct", baseCommit)
	cmd2.Dir = repoPath
	out2, err := cmd2.Output()
	if err != nil {
		return false
	}
	// git show --format=%ct can prepend a blank line before the format output.
	for _, line := range strings.Split(string(out2), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		epoch, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return false
		}
		age := time.Since(time.Unix(epoch, 0))
		return age <= time.Duration(ageDays)*24*time.Hour
	}
	return false
}

// safeLoadGraph loads a graph.Document from stateDir, recovering from any
// FlatBuffers panic (the library panics on malformed input). Returns an error
// for both IO failures and panics so callers can treat corrupt graphs as
// "not usable" without crashing the daemon.
func safeLoadGraph(stateDir string) (doc *graph.Document, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("FlatBuffers panic (corrupt graph.fb): %v", r)
			doc = nil
		}
	}()
	return graph.LoadGraphFromDir(stateDir)
}

// copyFile copies src to dst atomically (write .tmp then rename).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src %s: %w", src, err)
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy %s→%s: %w", src, tmp, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s→%s: %w", tmp, dst, err)
	}
	return nil
}
