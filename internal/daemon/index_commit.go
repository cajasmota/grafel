package daemon

import (
	"strings"

	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// IndexedCommitInfo is the exact git commit a repo's on-disk graph was built
// from, plus whether that commit still matches the repo's current HEAD
// (#5727, #5729-W1 status plane). It lets a caller answer "is the graph
// stale?" from already-recorded state — no reindex or full graph decode
// required.
type IndexedCommitInfo struct {
	// Commit is the full 40-char SHA of the indexed commit, or "" if unknown
	// (never indexed, non-git repo, or indexed before this field existed).
	Commit string
	// CommitShort is the abbreviated form of Commit (12 chars), or "" under
	// the same conditions as Commit. May be populated even when Commit is
	// empty for graphs written before the full-SHA field existed (#5727) —
	// legacy graphs only ever recorded the short form.
	CommitShort string
	// AtHead is true when the indexed commit matches the repo's current
	// committed HEAD. False when never indexed, HEAD has advanced past the
	// indexed commit, or HEAD cannot be resolved (non-git repo / git error).
	//
	// AtHead reflects COMMITTED state only: it compares against `git rev-parse
	// HEAD` and does NOT account for uncommitted working-tree changes. A repo
	// with a dirty tree can still report AtHead=true even though the on-disk
	// graph does not reflect those unstaged edits (review #5734 non-blocking
	// #5). Consumers wanting working-tree freshness should additionally check
	// the diff manifest / graph.fb mtime against the source files.
	AtHead bool
}

// IndexedCommitForRepo reports the commit repoPath's on-disk graph was
// indexed at, and whether it is still current.
//
// Resolution order (cheapest / most authoritative first):
//  1. The diff-manifest sidecar (.grafel-state/file-index.json, written by
//     internal/indexer/diff.SaveManifest on every index / incremental run) —
//     carries both GitCommit (short) and GitCommitFull (#5727/#5729-W1).
//  2. Fallback: the graph.fb header's IndexedSHA (short only) via
//     graph.PersistedStatsFromDir — a cheap header-only read (no full graph
//     decode), covering graphs produced by a path that never wrote the diff
//     manifest (e.g. a very old graph, or one written before incremental
//     indexing was enabled for the repo).
//
// The current-HEAD comparison shells out to `git rev-parse HEAD` (the FULL
// 40-char SHA) under gitmeta's bounded timeout — this is an on-demand RPC-path
// helper (grafel_index_status / `grafel status`), NOT the poll-safe
// status-file read path, which must never shell out (see internal/statusfile).
func IndexedCommitForRepo(repoPath string) IndexedCommitInfo {
	var info IndexedCommitInfo

	stateDir := StateDirForRepo(repoPath)
	if stateDir != "" {
		m := diff.LoadManifest(stateDir)
		info.CommitShort = m.GitCommit
		info.Commit = m.GitCommitFull
	}

	if info.CommitShort == "" && stateDir != "" {
		if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
			// graph.fb records only the short SHA (gitmeta uses --short=12).
			info.CommitShort = ps.IndexedSHA
		}
	}

	if info.CommitShort == "" && info.Commit == "" {
		return info
	}

	// Compare against the FULL current HEAD SHA. Comparing short forms was
	// buggy: the graph.fb fallback records a 12-char short (gitmeta
	// --short=12) while `git rev-parse --short HEAD` yields git's default
	// (~7 chars), so they never matched → AtHead always false for
	// manifest-less graphs (review #5734 non-blocking #4). A full SHA plus a
	// prefix check normalizes every short-length variant: the recorded short
	// SHA (7 or 12 chars) is always a prefix of the full HEAD SHA when they
	// refer to the same commit.
	headFull, ok := gitmeta.RunGitBounded(repoPath, "rev-parse", "HEAD")
	if !ok || headFull == "" {
		return info
	}
	switch {
	case info.Commit != "":
		info.AtHead = strings.EqualFold(headFull, info.Commit)
	default:
		info.AtHead = strings.HasPrefix(strings.ToLower(headFull), strings.ToLower(info.CommitShort))
	}
	return info
}
