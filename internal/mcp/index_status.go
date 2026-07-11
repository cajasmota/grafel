// grafel_index_status — per-repo index freshness (#5433).
//
// The global is_indexing flag (grafel_stats) is a single process-wide bool: an
// agent that polls it to decide "is my repo ready?" is blocked by ANY repo's
// indexing, including unrelated ones — head-of-line blocking across independent
// repos in multi-agent / multi-worktree setups.
//
// This tool exposes PER-REPO index state so an agent gates on its own repo. It
// is deliberately LIGHTWEIGHT: it reads ONLY the scheduler's published snapshot
// (via the indexstate leaf bridge) and the in-memory registry. It does NOT load
// or assemble the group graph, so it is cheap to poll on a tight loop.
package mcp

import (
	"context"
	"path/filepath"
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// indexStatusRepo is one repo's wire-shape row in grafel_index_status.
type indexStatusRepo struct {
	Repo       string `json:"repo"`
	Group      string `json:"group,omitempty"`
	State      string `json:"state"`
	IndexedRef string `json:"indexed_ref,omitempty"`
	HeadRef    string `json:"head_ref,omitempty"`
	Dirty      bool   `json:"dirty"`

	// #5727/#5729-W1: the exact commit the on-disk graph was indexed at, plus
	// whether it still matches HEAD. Sourced from daemon.IndexedCommitForRepo
	// (diff-manifest sidecar, falling back to the graph.fb header). Empty/false
	// when never indexed or the graph predates this field.
	IndexedCommit      string `json:"indexed_commit,omitempty"`
	IndexedCommitShort string `json:"indexed_commit_short,omitempty"`
	AtHead             bool   `json:"at_head,omitempty"`
}

// indexStatusReply is the grafel_index_status response envelope.
type indexStatusReply struct {
	Repos []indexStatusRepo `json:"repos"`
	// AnyIndexing is true if at least one of the RETURNED repos is currently
	// indexing or dirty (after filters apply). An agent gating on its own repo
	// should NOT use this — it should check its repo's state==current — but it
	// is a convenient summary when no filter is set.
	AnyIndexing bool `json:"any_indexing"`
	// Parsing is the number of IN-PROCESS tree-sitter parses running in the
	// daemon right now (#5630). The reactive incremental reindex re-parses
	// changed files inside the daemon process — work that registers in NEITHER
	// AnyIndexing's per-repo states NOR concurrency.indexing (the IndexGate).
	// Before this field a daemon CPU-pinned in ts_parser_parse looked idle.
	// Reported regardless of any filter (it is process-global, not per-repo).
	Parsing int `json:"parsing"`
	// Busy is the true daemon-activity signal (#5630/#5631): an index job, a
	// group-algo pass, OR an in-process parse is running. A consumer that needs
	// "is grafel quiet?" should gate on this, not on any_indexing alone — the
	// latter only reflects scheduler index freshness, not in-process parsing.
	Busy bool `json:"busy"`
	// Concurrency mirrors the daemon-wide index-concurrency gate (#5493) so a
	// caller can see how a many-module group is draining: "indexing N, queued M"
	// with a cap of GRAFEL_INDEX_CONCURRENCY. Reported regardless of any filter.
	Concurrency indexConcurrency `json:"concurrency"`
}

// indexConcurrency is the wire shape for the gate counts (#5493).
type indexConcurrency struct {
	// Active is the number of module/repo indexes running concurrently right now.
	Active int `json:"indexing"`
	// Queued is the number of indexes waiting for a free slot.
	Queued int `json:"queued"`
	// Cap is the configured concurrency limit (GRAFEL_INDEX_CONCURRENCY).
	Cap int `json:"cap"`
}

// handleIndexStatus answers grafel_index_status. Optional args:
//
//	repo  — substring OR exact match against the repo path (case-insensitive).
//	group — exact group name; only repos in that group are returned.
//
// Each row reports state ∈ {current, queued, indexing, dirty}, plus indexed_ref
// (last completed index's ref) and head_ref (ref the pending/in-flight work
// targets). An agent gates on its repo with: state=="current" && indexed_ref
// (when both refs are known) == head_ref.
func (s *Server) handleIndexStatus(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	repoFilter := strings.TrimSpace(argString(req, "repo", ""))
	groupFilter := strings.TrimSpace(argString(req, "group", ""))

	// #5685: gate on group resolution exactly like grafel_orient. Route through
	// the same resolveGroup cascade (explicit group= → cwd → registry → singleton)
	// and surface its error VERBATIM. Without this, a call with no cwd and no
	// group= silently fell through to an un-scoped scan that read as an empty /
	// "nothing indexed" result instead of the actionable "pass `group=<name>`"
	// message. When a group IS resolvable (explicit or from cwd/singleton) this
	// returns nil and the existing snapshot logic below runs unchanged.
	if _, _, err := resolveGroup(s.State, groupFilter, s.inferCWD(req)); err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}

	// Build a repo-path → group index from the registry so each row can be
	// attributed to its group. The scheduler keys on the same on-disk path the
	// registry stores, so an exact-path match attaches the group.
	pathToGroup := s.repoPathToGroup()

	// #5729 PR3: source per-repo state from the status-plane sidecar
	// (internal/statusfile), NOT indexstate.RepoStates(). In split mode serve
	// has no in-process scheduler at all — indexstate would be permanently
	// empty there — so the status file (written by whichever process is
	// running the engine plane, monolith OR the standalone engine child) is
	// now the ONE source both modes read, giving identical answers in either
	// mode (the ordering-guard property this PR exists to deliver).
	seen := make(map[string]bool, len(pathToGroup))
	out := indexStatusReply{Repos: make([]indexStatusRepo, 0, len(pathToGroup))}

	addRow := func(path, group string) {
		if seen[path] {
			return
		}
		seen[path] = true
		if groupFilter != "" && group != groupFilter {
			return
		}
		if repoFilter != "" && !repoMatches(path, repoFilter) {
			return
		}

		if f, ok := daemon.RepoStatusFile(path); ok {
			state := f.State
			if state == "" {
				// Tolerant reader: a status file written by a pre-PR3 engine
				// (or one whose scheduler never recorded a transition for
				// this repo) carries no State — a materialized, non-pending
				// repo is "current", the same default RepoStates() implied
				// by simply having no entry.
				state = indexstate.StateCurrent
			}
			row := indexStatusRepo{
				Repo:       path,
				Group:      group,
				State:      state,
				IndexedRef: f.IndexedRef,
				HeadRef:    f.HeadRef,
				Dirty:      f.Dirty,
			}
			ci := daemon.IndexedCommitForRepo(path)
			row.IndexedCommit = ci.Commit
			row.IndexedCommitShort = ci.CommitShort
			row.AtHead = ci.AtHead
			out.Repos = append(out.Repos, row)
			if state == indexstate.StateIndexing || state == indexstate.StateDirty {
				out.AnyIndexing = true
			}
			return
		}

		// #5710: disk-backed fallback. A repo indexed by a PREVIOUS daemon
		// lifetime, or via `grafel rebuild` (which bypasses Scheduler.runIndex),
		// may never have a status file at all even though a materialized graph
		// exists on disk. Without this, grafel_index_status disagreed with the
		// CLI (`grafel status`, which reads the on-disk store/sidecars via
		// ComputeStatusSummary): MCP reported repos:[] for a repo the CLI
		// showed as fully indexed. A disk-only row is by definition idle
		// (`current`) — it never contributes to AnyIndexing.
		if row, ok := diskFallbackRow(path, group); ok {
			out.Repos = append(out.Repos, row)
		}
	}

	for path, group := range pathToGroup {
		addRow(path, group)
	}
	// #5729 PR3: also surface a WORKTREE CHILD the engine's scheduler tracks
	// but that was never separately registered — i.e. a repoPath the status
	// plane knows about that lives UNDER one of this server's own registered
	// repos. Deliberately narrower than "every sidecar on disk": this server
	// (and its test doubles) may share a machine-wide $GRAFEL_HOME with
	// other, wholly unrelated repos/daemons, and those must never leak into
	// this group's results. Best-effort: an error here (e.g. the status dir
	// missing entirely — engine never ran) just means nothing extra to add.
	if all, err := statusfile.ReadAll(); err == nil {
		for _, f := range all {
			if daemon.IsEngineLivenessRecord(f) || f.RepoPath == "" {
				continue
			}
			if group, known := pathToGroup[f.RepoPath]; known {
				addRow(f.RepoPath, group)
				continue
			}
			if parent, ok := descendantOfKnownRepo(f.RepoPath, pathToGroup); ok {
				addRow(f.RepoPath, pathToGroup[parent])
			}
		}
	}

	// #5493/#5630: surface the daemon-wide gate counts + parse/busy signal
	// from the engine-liveness sidecar rather than indexstate's in-process
	// globals — engine_liveness is stale/missing gracefully (Concurrency all
	// zero, Busy=false, Parsing=0: "unknown", not a crash or stale garbage)
	// when the engine is down, starting, or degraded.
	var engineFresh bool
	if layout, lerr := daemon.DefaultLayout(); lerr == nil {
		if lf, fresh := daemon.EngineLivenessStatus(layout.Root); fresh && lf != nil {
			engineFresh = true
			out.Concurrency = indexConcurrency{Active: lf.ConcurrencyActive, Queued: lf.ConcurrencyQueued, Cap: lf.ConcurrencyCap}
			out.Parsing = lf.ParseInFlight
			out.Busy = lf.Busy
			if lf.ParseInFlight > 0 {
				out.AnyIndexing = true
			}
		}
	}
	if !engineFresh {
		// No fresh engine-liveness sidecar (no engine plane/heartbeat has ever
		// run against this daemon root — e.g. a test harness driving
		// indexstate directly, or a real engine that is down/starting).
		// Falling back to the process-global indexstate record preserves
		// pre-#5729-PR3 behavior for an in-process scheduler with no
		// heartbeat writer running yet, rather than reporting stale zeros
		// when the live data is actually available right here in-process.
		snap := indexstate.Get()
		conc := indexstate.GetIndexConcurrency()
		out.Concurrency = indexConcurrency{Active: conc.Active, Queued: conc.Queued, Cap: conc.Cap}
		out.Parsing = snap.ParseInFlight
		out.Busy = snap.Busy
		if snap.ParseInFlight > 0 {
			out.AnyIndexing = true
		}
	}
	return jsonResult(out), nil
}

// diskFallbackRow synthesizes a `current` grafel_index_status row for
// repoPath by reading the on-disk graph's HEADER only — NEVER a full decode.
// grafel_index_status is a frequent, must-be-fast status probe; on a large
// repo (hundreds of thousands of entities) a full graph.LoadGraphFromDir
// would O(N)-decode+alloc every entity and relationship just to read one
// header string, discarded immediately. Instead this uses the same cheap
// header path as graph.PersistedStatsFromDir: fbreader.Open + LoadGraphMeta,
// which reads header fields off the mmap WITHOUT touching any vector.
//
// Note this is NOT the CLI's `grafel status` code path — that
// (ComputeStatusSummary → graph-stats.json sidecar, falling back to
// graph.PersistedStatsFromDir) is likewise header/sidecar-only; we agree with
// it on the answer while avoiding any full-graph decode.
//
// Returns ok=false when neither graph.fb nor graph.json exists for repoPath
// — i.e. the repo is genuinely never-indexed, so no row should be fabricated.
func diskFallbackRow(repoPath, group string) (indexStatusRepo, bool) {
	graphPath, _ := daemon.FindGraphFile(repoPath)
	if graphPath == "" {
		return indexStatusRepo{}, false
	}
	row := indexStatusRepo{
		Repo:  repoPath,
		Group: group,
		State: indexstate.StateCurrent,
	}
	// Best-effort: attach the indexed ref if the graph.fb header carries Phase
	// 0 git metadata (#2088). Read via the cheap header path — no entity or
	// relationship is decoded. Only attempted for an actual graph.fb file:
	// fbreader.Open interprets the bytes as FlatBuffers and PANICS on a
	// graph.json (or otherwise non-fb) file, so a json-only repo is skipped
	// here — the row is still valid without the ref. A disk-only row has no
	// in-flight work, so head_ref mirrors indexed_ref (nothing pending beyond
	// what's indexed).
	if strings.HasSuffix(graphPath, ".fb") {
		if r, err := fbreader.Open(graphPath); err == nil {
			meta := r.LoadGraphMeta()
			r.Close()
			row.IndexedRef = meta.IndexedRef
			row.HeadRef = meta.IndexedRef
		}
	}
	// #5727/#5729-W1: attach the indexed commit + freshness for a disk-only
	// row too, so a repo indexed via `grafel rebuild` (bypassing the
	// scheduler) still reports indexed_commit/at_head.
	ci := daemon.IndexedCommitForRepo(repoPath)
	row.IndexedCommit = ci.Commit
	row.IndexedCommitShort = ci.CommitShort
	row.AtHead = ci.AtHead
	return row, true
}

// descendantOfKnownRepo reports whether path is a strict subdirectory of any
// repo path already known to known (a pathToGroup-shaped map), returning the
// matching parent's key. Used to scope a full-disk statusfile.ReadAll() scan
// down to genuine worktree children of THIS server's own registered repos —
// never an arbitrary unrelated repo that happens to share the same
// $GRAFEL_HOME (e.g. another project's daemon on the same machine, or a bare
// test harness with no HOME isolation at all).
func descendantOfKnownRepo(path string, known map[string]string) (parent string, ok bool) {
	for k := range known {
		if k == "" || k == path {
			continue
		}
		rel, err := filepath.Rel(k, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return k, true
	}
	return "", false
}

// repoPathToGroup builds a path→group lookup from the registry. A repo path may
// appear in only one group in practice; if it appears in several, the last one
// scanned wins (group attribution is best-effort metadata, not a gate).
func (s *Server) repoPathToGroup() map[string]string {
	m := map[string]string{}
	if s == nil || s.State == nil {
		return m
	}
	for gName, grp := range s.State.registry.Groups {
		for _, r := range grp.Repos {
			if r.Path != "" {
				m[r.Path] = gName
			}
		}
	}
	return m
}

// repoMatches reports whether the repo path satisfies the caller's repo filter.
// Matches if the filter equals the path, OR is a case-insensitive substring of
// it (so "acme_core" matches "/Users/x/Projects/acme_core").
func repoMatches(path, filter string) bool {
	if path == filter {
		return true
	}
	return strings.Contains(strings.ToLower(path), strings.ToLower(filter))
}
