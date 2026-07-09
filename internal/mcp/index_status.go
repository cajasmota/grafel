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
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// indexStatusRepo is one repo's wire-shape row in grafel_index_status.
type indexStatusRepo struct {
	Repo       string `json:"repo"`
	Group      string `json:"group,omitempty"`
	State      string `json:"state"`
	IndexedRef string `json:"indexed_ref,omitempty"`
	HeadRef    string `json:"head_ref,omitempty"`
	Dirty      bool   `json:"dirty"`
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

	states := indexstate.RepoStates()
	out := indexStatusReply{Repos: make([]indexStatusRepo, 0, len(states))}
	for _, st := range states {
		group := pathToGroup[st.Path]

		if groupFilter != "" && group != groupFilter {
			continue
		}
		if repoFilter != "" && !repoMatches(st.Path, repoFilter) {
			continue
		}

		row := indexStatusRepo{
			Repo:       st.Path,
			Group:      group,
			State:      st.State,
			IndexedRef: st.IndexedRef,
			HeadRef:    st.HeadRef,
			Dirty:      st.Dirty,
		}
		out.Repos = append(out.Repos, row)
		if st.State == indexstate.StateIndexing || st.State == indexstate.StateDirty {
			out.AnyIndexing = true
		}
	}
	// #5493: surface the daemon-wide gate counts so "indexing 2, queued 28" is
	// visible (a 30-module group draining 2-at-a-time, not stalled).
	ic := indexstate.GetIndexConcurrency()
	out.Concurrency = indexConcurrency{Active: ic.Active, Queued: ic.Queued, Cap: ic.Cap}
	// #5630: surface in-process parse activity + the true busy signal so a daemon
	// CPU-pinned in ts_parser_parse no longer reports idle. Process-global, so it
	// is reported regardless of the repo/group filter and OR-ed into AnyIndexing
	// (an in-process incremental reindex IS indexing work, even though it never
	// touched the scheduler's per-repo state or the IndexGate).
	snap := indexstate.Get()
	out.Parsing = snap.ParseInFlight
	out.Busy = snap.Busy
	if snap.ParseInFlight > 0 {
		out.AnyIndexing = true
	}
	return jsonResult(out), nil
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
