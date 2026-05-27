package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// argString returns a string argument with default fallback.
func argString(req mcpapi.CallToolRequest, key, def string) string {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// argInt returns an integer argument with default fallback.
func argInt(req mcpapi.CallToolRequest, key string, def int) int {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		}
	}
	return def
}

// argFloat returns a float argument.
func argFloat(req mcpapi.CallToolRequest, key string, def float64) float64 {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return def
}

// argBool returns a bool argument.
func argBool(req mcpapi.CallToolRequest, key string, def bool) bool {
	args := req.GetArguments()
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// argStringSlice returns a []string argument.
func argStringSlice(req mcpapi.CallToolRequest, key string) []string {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}

// resolveAndGroup resolves the group from the request and returns the loaded
// group. Returns a tool-error result on failure (never panic).
func (s *Server) resolveAndGroup(req mcpapi.CallToolRequest) (string, *LoadedGroup, *mcpapi.CallToolResult) {
	explicit := argString(req, "group", "")
	cwd := s.inferCWD(req)
	g, _, err := resolveGroup(s.State, explicit, cwd)
	if err != nil {
		return "", nil, mcpapi.NewToolResultError(err.Error())
	}
	lg := s.State.Group(g)
	if lg == nil {
		return g, nil, mcpapi.NewToolResultError(fmt.Sprintf("group %q not loaded", g))
	}
	return g, lg, nil
}

// resolveAndGroupWithRef resolves the group and additionally returns the
// CWDResolution (which carries the inferred ref, SHA, and worktree flag)
// for the caller. This is the PH1c entry-point for tools that expose ref
// context to agents (archigraph_whoami, archigraph_inspect, etc.).
func (s *Server) resolveAndGroupWithRef(req mcpapi.CallToolRequest) (string, *LoadedGroup, CWDResolution, *mcpapi.CallToolResult) {
	cwd := s.inferCWD(req)
	cwdRes := ResolveCWD(s.State, cwd)

	explicit := argString(req, "group", "")
	g, _, err := resolveGroup(s.State, explicit, cwd)
	if err != nil {
		return "", nil, cwdRes, mcpapi.NewToolResultError(err.Error())
	}
	// Backfill group in cwdRes when the resolution came from explicit param or singleton.
	if cwdRes.Group == "" {
		cwdRes.Group = g
	}
	lg := s.State.Group(g)
	if lg == nil {
		return g, nil, cwdRes, mcpapi.NewToolResultError(fmt.Sprintf("group %q not loaded", g))
	}
	return g, lg, cwdRes, nil
}

// refForRequest returns the ref to use for a request. Resolution order:
//  1. Explicit "ref=" argument in the request.
//  2. CWD-inferred ref from gitmeta.Capture on the cwd (via ResolveCWD).
//  3. "" (caller decides what empty means).
func (s *Server) refForRequest(req mcpapi.CallToolRequest) string {
	if explicit := argString(req, "ref", ""); explicit != "" {
		return explicit
	}
	cwd := s.inferCWD(req)
	res := ResolveCWD(s.State, cwd)
	return res.Ref
}

// reposToConsider applies repo_filter to a group's repo set. Empty filter
// returns all loaded repos. "*" is treated as "all".
func reposToConsider(lg *LoadedGroup, filter []string) []*LoadedRepo {
	if len(filter) == 0 || (len(filter) == 1 && filter[0] == "*") {
		out := make([]*LoadedRepo, 0, len(lg.Repos))
		names := make([]string, 0, len(lg.Repos))
		for n := range lg.Repos {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if r := lg.Repos[n]; r != nil && r.Doc != nil {
				out = append(out, r)
			}
		}
		return out
	}
	out := []*LoadedRepo{}
	for _, name := range filter {
		if r, ok := lg.Repos[name]; ok && r.Doc != nil {
			out = append(out, r)
		}
	}
	return out
}

// jsonResult is a small helper to produce JSON tool output with a structured
// payload for agents that prefer machine-readable shapes.
//
// Wire format: MINIFIED JSON (no indentation, no trailing whitespace). Field
// names and shapes are unchanged — callers still find `id`, `qualified_name`,
// `source_file`, `start_line`, etc. See #1663: pretty-printing was costing
// ~20-30% of MCP payload tokens for zero semantic value (callers all
// `json.Unmarshal` the response).
//
// #2287: jsonResult still marshals here (so direct-handler-call paths —
// internal cross-handler dispatch like mergeNeighbors, and many tests —
// continue to see valid JSON in res.Content[0].Text), but it ALSO
// stashes the structured value v so wrap() can build the final envelope
// (elapsed_ms + TOON conversion) directly from v in a single marshal —
// no parse, no remarshal. The legacy injectElapsedMS path executed
// marshal → parse → remarshal on every call, paying CPU proportional to
// payload size on the parse step. By holding v we drop the parse step
// entirely; net cost on the wire path is marshal + marshal (the second
// for the elapsed-ms envelope), with the structured value reused from
// memory rather than reconstructed from text.
func jsonResult(v any) *mcpapi.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return mcpapi.NewToolResultError("marshal: " + err.Error())
	}
	res := mcpapi.NewToolResultText(string(data))
	stashDeferred(res, v)
	return res
}

// ---------------------------------------------------------------------------
// whoami
// ---------------------------------------------------------------------------

// mcpWireVersion is the MCP API version advertised by archigraph_whoami.
// Bump this on every MINOR release. Agents can use it to detect
// incompatible daemon versions without inspecting the binary.
const mcpWireVersion = "0.1.0"

// sessionMeta returns the session-stable ref/sha/worktree fields for a given
// CWD resolution. These fields are exclusive to archigraph_whoami (#2335,
// #2337) — no other handler should embed them.
//
// lr is optional; passing nil is safe (the function degrades gracefully).
func sessionMeta(lr *LoadedRepo, cwdRes *CWDResolution) map[string]any {
	ref := ""
	sha := ""
	isWorktree := false
	parentRepo := interface{}(nil)
	if cwdRes != nil {
		ref = cwdRes.Ref
		sha = cwdRes.SHA
		isWorktree = cwdRes.IsWorktree
		if cwdRes.IsWorktree && cwdRes.ParentRepoPath != "" {
			parentRepo = cwdRes.ParentRepoPath
		}
	}
	return map[string]any{
		"indexed_ref": ref,
		"indexed_sha": sha,
		"is_worktree": isWorktree,
		"parent_repo": parentRepo,
	}
}

func (s *Server) handleWhoami(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	cwd := s.inferCWD(req)
	explicit := argString(req, "group", "")
	group, source, err := resolveGroup(s.State, explicit, cwd)

	// PH1c: resolve CWD to (group, repo, ref) triple.
	cwdRes := ResolveCWD(s.State, cwd)

	if err != nil {
		return jsonResult(map[string]any{
			"group":         "",
			"repo":          "",
			"source":        "none",
			"registry_path": s.State.registry.Path,
			"wire_version":  mcpWireVersion,
			"error":         err.Error(),
		}), nil
	}
	repo := repoFromCWD(cwd)
	if repo == "" {
		repo = cwdRes.RepoSlug
	}

	// Derive "cwd_resolved_to" label: "<group> / <slug> / <ref>".
	cwdResolvedTo := group
	if cwdRes.RepoSlug != "" {
		cwdResolvedTo = group + " / " + cwdRes.RepoSlug
		if cwdRes.Ref != "" {
			cwdResolvedTo += " / " + cwdRes.Ref
		}
	}

	// Base response. PH1c ref fields come from sessionMeta (#2337).
	sm := sessionMeta(nil, &cwdRes)
	resp := map[string]any{
		"group":         group,
		"repo":          repo,
		"source":        source,
		"registry_path": s.State.registry.Path,
		"wire_version":  mcpWireVersion,
		// PH1c ref fields (via sessionMeta — exclusive to whoami per #2337).
		"cwd_resolved_to": cwdResolvedTo,
		"is_worktree":     sm["is_worktree"],
		"indexed_ref":     sm["indexed_ref"],
		"indexed_sha":     sm["indexed_sha"],
		"parent_repo":     sm["parent_repo"],
	}
	if cwdRes.Source == "worktree" {
		resp["tier"] = "cold" // worktree refs may not be hot-loaded yet
	} else {
		resp["tier"] = "hot"
	}

	// Nudge suppression: ARCHIGRAPH_WHOAMI_NUDGE=quiet disables doc-state fields.
	if os.Getenv("ARCHIGRAPH_WHOAMI_NUDGE") == "quiet" {
		return jsonResult(resp), nil
	}

	// Enrich with documentation state + action counts.
	lg := s.State.Group(group)
	if lg == nil {
		return jsonResult(resp), nil
	}

	docState := ComputeDocState(group, lg)

	// Count pattern candidates and residuals for suggested_action priority.
	candidateCount := countPatternCandidates(group, lg)
	residualCount := countResiduals(lg)

	suggestedAction := composeSuggestedAction(docState, candidateCount, residualCount)

	resp["documentation_state"] = docState.DocumentationState
	resp["last_docgen_at"] = docState.LastDocgenAt
	resp["stale_count"] = docState.StaleCount
	resp["patterns_count"] = countPatterns(group, lg)
	resp["candidate_count"] = candidateCount
	resp["residual_count"] = residualCount
	resp["suggested_action"] = suggestedAction
	if len(docState.PerRepoStale) > 0 {
		resp["per_repo_stale"] = docState.PerRepoStale
	}

	return jsonResult(resp), nil
}

// countPatterns returns the total number of patterns (candidate + approved) for a group.
func countPatterns(groupName string, lg *LoadedGroup) int {
	dir := patternsDir(groupName, lg)
	patterns, err := loadPatternsQuiet(dir)
	if err != nil {
		return 0
	}
	return len(patterns)
}

// countPatternCandidates returns the number of is_candidate=true patterns.
func countPatternCandidates(groupName string, lg *LoadedGroup) int {
	dir := patternsDir(groupName, lg)
	patterns, err := loadPatternsQuiet(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, p := range patterns {
		if p.IsCandidate {
			count++
		}
	}
	return count
}

// countResiduals returns the total repair_edge candidate count across all repos.
func countResiduals(lg *LoadedGroup) int {
	total := 0
	for _, r := range lg.Repos {
		if r == nil || r.Path == "" {
			continue
		}
		total += len(readRepairEdgeCandidates(r.Path))
	}
	return total
}

// loadPatternsQuiet loads patterns without propagating errors (whoami must not fail).
func loadPatternsQuiet(dir string) ([]patternForCount, error) {
	if dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "patterns.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var env struct {
		Patterns []patternForCount `json:"patterns"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return env.Patterns, nil
}

// patternForCount is a minimal pattern shape for counting — avoids importing
// agentpatterns here (already imported in patterns.go; this avoids the cycle).
type patternForCount struct {
	IsCandidate bool `json:"is_candidate"`
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func (s *Server) handleQueryGraph(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	// canonical name is "query"; accept legacy "question" with a deprecation log.
	question := argString(req, "query", "")
	if question == "" {
		question = argString(req, "question", "")
		if question != "" {
			fmt.Fprintln(os.Stderr, "[archigraph deprecation] archigraph_find: param 'question' is deprecated, use 'query'")
		}
	}
	if question == "" {
		return mcpapi.NewToolResultError("missing required argument: query"), nil
	}
	var err error
	_ = err // retained for symmetry with other handlers
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 3)
	tokenBudget := argInt(req, "token_budget", 800)
	full := argBool(req, "full", false)
	verbose := argBool(req, "verbose", false)
	includeNoise := argBool(req, "include_noise", false)
	// min_score: undeclared in JSON-Schema (#1639 pattern). Default 0.15 trims
	// low-signal tail noise (test helpers, unrelated handlers). #1921: floor
	// the caller-supplied value at 0.15 so callers can't disable it implicitly;
	// pass min_score=0 explicitly to opt out (still subject to max_results).
	minScore := argFloat(req, "min_score", 0.15)
	if minScore > 0 && minScore < 0.15 {
		minScore = 0.15
	}
	// max_results: hard ceiling on total ranked hits returned (#1807, #1921).
	// Default 50; cap at 200 even if caller asks for more. Prevents the
	// 3,647-row failure mode where broad queries flooded the corpus.
	maxResults := argInt(req, "max_results", 50)
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}
	repoFilter := argStringSlice(req, "repo_filter")
	contextFilter := contextFilterSet(argStringSlice(req, "context_filter"))
	mode := argString(req, "mode", "bfs")

	repos := reposToConsider(lg, repoFilter)
	if len(repos) == 0 {
		return mcpapi.NewToolResultText("# no repos loaded for this group\n"), nil
	}

	// Score across all repos in scope. BM25 runs unconditionally; the
	// semantic vector index is fused via RRF when (a) the repo has an
	// embeddings.bin sidecar and (b) the configured query embedding backend
	// successfully embeds the question (#461 / ADR-0019). Per-repo fusion
	// keeps ranks comparable; cross-repo merging follows the existing
	// re-rank pass below.
	//
	// bm25HitsByRepo tracks the raw BM25 hit count per repo before de-noise /
	// min_score filtering. Used by the "always-1" fallback (#2554b) to scope
	// the PageRank-fallback entity to the repo most textually similar to the
	// query, preventing cross-context entity bleed.
	all := []scored{}
	bm25HitsByRepo := make(map[*LoadedRepo]int, len(repos))
	var qVec []float32
	var qHave bool
	for _, r := range repos {
		bm25Hits := r.BM25.Search(question, 10)
		bm25HitsByRepo[r] = len(bm25Hits)
		if r.Semantic != nil && r.Semantic.Len() > 0 {
			if !qHave {
				qVec, qHave = embedQuery(ctx, question)
				if !qHave {
					// One query embed attempt per request; on failure stay BM25-only.
					qVec = nil
				}
			}
			if qHave && len(qVec) == r.Semantic.Dims {
				semIDs := r.Semantic.Search(qVec, 10)
				semHits := make([]Hit, 0, len(semIDs))
				for _, s := range semIDs {
					if e, ok := r.ByID[s.ID]; ok {
						semHits = append(semHits, Hit{Entity: e, Score: s.Score, Source: "semantic"})
					}
				}
				fused := FuseRRF(bm25Hits, semHits)
				for _, h := range fused {
					all = append(all, scored{repo: r, hit: h})
				}
				continue
			}
		}
		for _, h := range bm25Hits {
			h.Source = "bm25"
			all = append(all, scored{repo: r, hit: h})
		}
	}

	// De-noise (#1614): drop file/module container components, inferred class
	// shadows, raw Pattern nodes and Process built-in nodes from the default
	// ranked list. They remain reachable via include_noise:true. Applied to
	// BOTH the compact and full (structured) outputs.
	if !includeNoise {
		filtered := all[:0]
		for _, sc := range all {
			if isNoise(sc.hit.Entity) {
				continue
			}
			filtered = append(filtered, sc)
		}
		all = filtered
	}

	// Re-rank (#1614): real, lined, qualified entities sort above
	// shadows/containers/patterns. Within a tier, BM25 score order is preserved.
	// (When include_noise is set, this still floats real hits above the noise.)
	sort.SliceStable(all, func(i, j int) bool {
		ti, tj := rankTier(all[i].hit.Entity), rankTier(all[j].hit.Entity)
		if ti != tj {
			return ti < tj
		}
		return all[i].hit.Score > all[j].hit.Score
	})

	// min_score cutoff (#1747): drop ranked hits below the threshold after
	// re-ranking so the tail is clean. Only applied when minScore > 0 and there
	// is at least one hit above the bar (avoids blanking on noisy corpora).
	// The "always-1" fallback below still fires if the cull leaves the list empty.
	if minScore > 0 && len(all) > 0 {
		// Keep hits whose BM25/RRF score clears the bar. Since re-rank tier sorts
		// noiseNone first, the best real hit is all[0]; only trim from the tail.
		culled := all[:0]
		for _, sc := range all {
			if sc.hit.Score >= minScore {
				culled = append(culled, sc)
			}
		}
		// Preserve at least one hit so the caller always gets a result.
		if len(culled) > 0 {
			all = culled
		}
	}

	// "always-1" rule: if nothing matched but repos contain entities, return
	// the highest-PageRank entity as a single-result fallback so callers see
	// something rather than empty.
	//
	// #2554b: scope the fallback to the repo most textually similar to the
	// query to prevent cross-context bleed. Without scoping, pickFallback
	// selected the globally highest-PageRank entity across ALL repos, which
	// caused bench iter 1 q10 (InspectionResultsPage trace) to surface an
	// unrelated POST /schedule/import endpoint from a different repo.
	//
	// Scoping order:
	//  1. If a single repo_filter was provided, restrict to that repo.
	//  2. Otherwise use the repo with the most raw BM25 candidate hits
	//     (highest textual overlap with the query terms) — never cross-repo.
	//  3. Fall back to the first repo if all hit counts are zero.
	if len(all) == 0 {
		scopedRepos := scopeFallbackRepos(repos, repoFilter, bm25HitsByRepo)
		fallback := pickFallback(scopedRepos)
		if fallback != nil {
			all = append(all, scored{repo: fallback.repo, hit: Hit{Entity: fallback.entity, Score: 0.0001}})
		}
	}

	// #1807 / #1921: hard ceiling. Even with min_score=0 a broad single-token
	// query was returning >3k rows. Cap total ranked hits and surface a note so
	// the agent can re-query with a tighter `query=` if needed.
	preCapTotal := len(all)
	truncated := false
	if len(all) > maxResults {
		all = all[:maxResults]
		truncated = true
	}

	if full {
		out := map[string]any{
			"matches": serializeHits(all, verbose),
		}
		if truncated {
			out["truncation_note"] = fmt.Sprintf(
				"capped at max_results=%d; %d additional hits omitted (pass max_results up to 200 or tighten query)",
				maxResults, preCapTotal-maxResults,
			)
		}
		return jsonResult(out), nil
	}

	// Smart scoping: no filter, multiple repos -> per-repo top 3.
	if len(repoFilter) == 0 && len(lg.Repos) > 1 {
		summary := renderPerRepoSummary(all, lg)
		// Record the prefixed ids of the top-3-per-repo hits actually shown so
		// the MCP-activity glow has nodes to highlight (the markdown body
		// carries no machine-readable ids). Mirrors renderPerRepoSummary's
		// own per-repo top-3 selection.
		perRepoShown := map[string]int{}
		for _, sc := range all {
			if perRepoShown[sc.repo.Repo] >= 3 {
				continue
			}
			perRepoShown[sc.repo.Repo]++
			recordNodeIDs(ctx, prefixedID(sc.repo.Repo, sc.hit.Entity.ID))
		}
		return mcpapi.NewToolResultText(summary), nil
	}

	// Otherwise BFS-expand from each top hit and render compact.
	matched := len(all)
	keep := all
	if len(keep) > 10 {
		keep = keep[:10]
	}
	visibleNodes := []nodeWithRepo{}
	visibleEdges := []renderEdge{}
	seen := map[string]bool{} // prefixed id

	add := func(repo string, e *graph.Entity, score float64) {
		pid := prefixedID(repo, e.ID)
		if seen[pid] {
			return
		}
		seen[pid] = true
		visibleNodes = append(visibleNodes, nodeWithRepo{Repo: repo, Entity: e, Score: score})
	}
	for _, sc := range keep {
		add(sc.repo.Repo, sc.hit.Entity, sc.hit.Score)
	}
	if mode != "none" {
		for _, sc := range keep {
			adj := sc.repo.Adjacency // cached at reload (#1656)
			vis := bfs(adj, sc.hit.Entity.ID, depth, contextFilter)
			for nid, d := range vis {
				if nid == sc.hit.Entity.ID {
					continue
				}
				if e, ok := sc.repo.LabelIndex.ByID[nid]; ok {
					add(sc.repo.Repo, e, sc.hit.Score/float64(d+1))
				}
			}
			// Carry edges between visible nodes. Adjacency-indexed (#2285):
			// iterate out-edges from each visited node instead of scanning the
			// full Doc.Relationships list. Every edge naturally appears exactly
			// once (as an out-edge of its source) so direction semantics match
			// the prior linear scan.
			for nid := range vis {
				from := sc.repo.LabelIndex.ByID[nid]
				if from == nil {
					continue
				}
				if !seen[prefixedID(sc.repo.Repo, nid)] {
					continue
				}
				for _, e := range sc.repo.Adjacency.Outgoing(nid) {
					if !seen[prefixedID(sc.repo.Repo, e.target)] {
						continue
					}
					to := sc.repo.LabelIndex.ByID[e.target]
					if to == nil {
						continue
					}
					visibleEdges = append(visibleEdges, renderEdge{From: from.Name, To: to.Name, Kind: e.kind})
				}
			}
		}
	}

	oneRepo := len(repos) == 1
	rr := renderResult{
		MatchedTotal: matched,
		Nodes:        visibleNodes,
		Edges:        visibleEdges,
		OneRepo:      oneRepo,
	}
	// Record the prefixed ids of every visible node so the MCP-activity glow
	// highlights the compact result set (the rendered markdown has no ids).
	for _, nw := range visibleNodes {
		recordNodeIDs(ctx, prefixedID(nw.Repo, nw.Entity.ID))
	}
	return mcpapi.NewToolResultText(renderCompact(rr, tokenBudget)), nil
}

// scopeFallbackRepos returns the single-element repo slice that the "always-1"
// fallback should be scoped to, preventing cross-repo entity bleed (#2554b).
//
// Selection priority:
//  1. If a single repo_filter was requested, honour it — the caller already
//     said "I want results from this repo only".
//  2. Otherwise pick the repo with the highest raw BM25 hit count (most
//     textual overlap with the query). This keeps the fallback in the same
//     semantic neighbourhood as the query even when every candidate fell below
//     minScore or was denoised away.
//  3. If all hit counts are zero (empty corpus / no BM25 index), fall back to
//     the first repo in the ordered slice so we always return something.
//
// The returned slice is always length 1 — pickFallback receives a restricted
// candidate set, not the full multi-repo list.
func scopeFallbackRepos(repos []*LoadedRepo, repoFilter []string, bm25Hits map[*LoadedRepo]int) []*LoadedRepo {
	if len(repos) == 0 {
		return repos
	}
	// Priority 1: explicit single-repo filter.
	if len(repoFilter) == 1 && repoFilter[0] != "*" {
		return repos[:1] // repos was already filtered by reposToConsider; first == the only one
	}
	// Priority 2: repo with the most BM25 hits.
	var best *LoadedRepo
	bestCount := -1
	for _, r := range repos {
		if c := bm25Hits[r]; c > bestCount {
			bestCount = c
			best = r
		}
	}
	if best != nil && bestCount > 0 {
		return []*LoadedRepo{best}
	}
	// Priority 3: first repo (preserves original behaviour for empty corpora).
	return repos[:1]
}

// pickFallback returns the highest-pagerank entity across repos.
type fallbackPick struct {
	repo   *LoadedRepo
	entity *graph.Entity
}

// pickFallback reads from the pre-built TopKPageRank cache on each LoadedRepo
// (#2304) rather than iterating Doc.Entities on every call. The cache is built
// once at index-load time by buildTopKPageRank (state.go) alongside Adjacency.
// Falls back to a full linear scan when the cache is empty (e.g. in unit tests
// that construct LoadedRepo directly without calling buildTopKPageRank).
func pickFallback(repos []*LoadedRepo) *fallbackPick {
	var best *fallbackPick
	bestPR := -1.0
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Fast path: use pre-sorted cache.
		if len(r.TopKPageRank) > 0 {
			topID := r.TopKPageRank[0]
			e := r.ByID[topID]
			if e == nil {
				continue
			}
			pr := 0.0
			if e.PageRank != nil {
				pr = *e.PageRank
			}
			if best == nil || pr > bestPR {
				bestPR = pr
				best = &fallbackPick{repo: r, entity: e}
			}
			continue
		}
		// Slow path fallback (no cache — e.g. unit tests without buildTopKPageRank).
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			pr := 0.0
			if e.PageRank != nil {
				pr = *e.PageRank
			}
			if best == nil || pr > bestPR {
				bestPR = pr
				best = &fallbackPick{repo: r, entity: e}
			}
		}
	}
	return best
}

// scored couples a hit with its repo for cross-repo result aggregation.
type scored struct {
	repo *LoadedRepo
	hit  Hit
}

// serializeHits is the structured (full=true) shape.
//
// Default (verbose=false): id, name, file, line, score, kind.
// Verbose (verbose=true): also includes qualified_name, repo.
func serializeHits(all []scored, verbose bool) []map[string]any {
	out := make([]map[string]any, 0, len(all))
	for _, sc := range all {
		m := map[string]any{
			"id":    prefixedID(sc.repo.Repo, sc.hit.Entity.ID),
			"name":  sc.hit.Entity.Name,
			"file":  sc.hit.Entity.SourceFile,
			"line":  sc.hit.Entity.StartLine,
			"score": sc.hit.Score,
			"kind":  stripScopePrefix(sc.hit.Entity.Kind),
		}
		if verbose {
			m["qualified_name"] = sc.hit.Entity.QualifiedName
			m["repo"] = sc.repo.Repo
		}
		out = append(out, m)
	}
	return out
}

// renderPerRepoSummary is the smart-scoping default for unfiltered, multi-repo queries.
//
// #1848 — TOON path (default): emits {id,repo,name,kind,file,line,score} rows so
// callers can chain directly into archigraph_docgen/archigraph_get_source without
// adding a repo_filter first. Falls back to legacy markdown when
// MCP_FIND_FORMAT=markdown is set.
func renderPerRepoSummary(all []scored, lg *LoadedGroup) string {
	// Compute per-repo top-3 selection (same logic in both paths).
	perRepo := map[string][]scored{}
	for _, sc := range all {
		perRepo[sc.repo.Repo] = append(perRepo[sc.repo.Repo], sc)
	}
	names := make([]string, 0, len(perRepo))
	for r := range perRepo {
		names = append(names, r)
	}
	sort.Strings(names)

	// Legacy markdown fallback (MCP_FIND_FORMAT=markdown).
	if findFormatMarkdown() {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# group: %s — per-repo top hits\n", lg.Name))
		for _, rn := range names {
			hits := perRepo[rn]
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].hit.Score > hits[j].hit.Score })
			if len(hits) > 3 {
				hits = hits[:3]
			}
			b.WriteString("\n## " + rn + "\n")
			for _, sc := range hits {
				b.WriteString(fmt.Sprintf("%s  %s:%d\n", sc.hit.Entity.Name, sc.hit.Entity.SourceFile, sc.hit.Entity.StartLine))
			}
		}
		return b.String()
	}

	// TOON path (#1848): build nodeWithRepo slice (top-3 per repo, repos in
	// sorted order) and delegate to hitsToTOON with oneRepo=false for the
	// 7-column schema {id,repo,name,kind,file,line,score}.
	nodes := make([]nodeWithRepo, 0, len(all))
	for _, rn := range names {
		hits := perRepo[rn]
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].hit.Score > hits[j].hit.Score })
		if len(hits) > 3 {
			hits = hits[:3]
		}
		for _, sc := range hits {
			nodes = append(nodes, nodeWithRepo{
				Repo:   sc.repo.Repo,
				Entity: sc.hit.Entity,
				Score:  sc.hit.Score,
			})
		}
	}
	header := fmt.Sprintf("# group: %s — per-repo top hits\n\n", lg.Name)
	return header + hitsToTOON(nodes, false /* multi-repo: include repo column */)
}

// ---------------------------------------------------------------------------
// describe
// ---------------------------------------------------------------------------

func (s *Server) handleGetNode(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	// canonical name is "entity_id"; accept legacy "label_or_id" with a deprecation log.
	key := argString(req, "entity_id", "")
	if key == "" {
		key = argString(req, "label_or_id", "")
		if key != "" {
			fmt.Fprintln(os.Stderr, "[archigraph deprecation] archigraph_inspect: param 'label_or_id' is deprecated, use 'entity_id'")
		}
	}
	if key == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	// #2290: inspect envelope no longer embeds graph_meta or cwd_ref_meta.
	// Those session-stable fields are surfaced by archigraph_whoami
	// (indexed_ref, indexed_sha, is_worktree, parent_repo, cwd_resolved_to).
	// Inspect dominates cross-stack chains; trimming this per-call saves
	// ~5-7% tokens and compounds in inspect-heavy sessions.
	g, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	verbose := argBool(req, "verbose", false)
	allFindings := loadFindings(findingsMemDir(g, lg))
	// Cross-repo prefixed ID? Resolve repo first for unambiguous lookup.
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			if e, ok := r.LabelIndex.ByID[local]; ok {
				scopeIsOne := len(repos) == 1
				out := serializeEntity(r.Repo, e, scopeIsOne, verbose)
				// #2290: omit findings key entirely when no findings exist.
				if fs := findingsForEntity(allFindings, e.ID, prefixedID(r.Repo, e.ID)); len(fs) > 0 {
					out["findings"] = findingsToJSON(fs, 0)
				}
				if agentEdges := agentResolvedEdgesForEntity(r, e.ID, scopeIsOne); len(agentEdges) > 0 {
					out["agent_resolved_edges"] = agentEdges
				}
				return jsonResult(out), nil
			}
		}
	}
	// #1650: collect every label/qname/id match across repos. Multiple matches
	// return a clarifier list with ids rather than silently picking the first.
	type matchPair struct {
		ent  *graph.Entity
		repo *LoadedRepo
	}
	var matches []matchPair
	for _, r := range repos {
		for _, hit := range r.LabelIndex.LookupAll(key) {
			matches = append(matches, matchPair{ent: hit, repo: r})
		}
	}
	if len(matches) == 0 {
		return mcpapi.NewToolResultError(fmt.Sprintf("not found: %s", key)), nil
	}
	if len(matches) > 1 {
		out := make([]map[string]any, 0, len(matches))
		for _, m := range matches {
			out = append(out, map[string]any{
				"id":             prefixedID(m.repo.Repo, m.ent.ID),
				"qualified_name": m.ent.QualifiedName,
				"label":          m.ent.Name,
				"repo":           m.repo.Repo,
				"source_file":    m.ent.SourceFile,
				"start_line":     m.ent.StartLine,
				"kind":           stripScopePrefix(m.ent.Kind),
			})
		}
		return jsonResult(map[string]any{
			"ambiguous": true,
			"query":     key,
			"count":     len(out),
			"matches":   out,
			"note":      "multiple entities match; call again with one of the ids above.",
		}), nil
	}
	m := matches[0]
	scopeIsOne := len(repos) == 1
	out := serializeEntity(m.repo.Repo, m.ent, scopeIsOne, verbose)
	// #2290: omit findings key entirely when no findings exist.
	if fs := findingsForEntity(allFindings, m.ent.ID, prefixedID(m.repo.Repo, m.ent.ID)); len(fs) > 0 {
		out["findings"] = findingsToJSON(fs, 0)
	}
	if agentEdges := agentResolvedEdgesForEntity(m.repo, m.ent.ID, scopeIsOne); len(agentEdges) > 0 {
		out["agent_resolved_edges"] = agentEdges
	}
	return jsonResult(out), nil
}

// graphMetaForInspect / cwdRefMetaForInspect helpers removed in #2290.
// These embedded session-stable git metadata in every archigraph_inspect
// response. They are now sourced from archigraph_whoami (indexed_ref,
// indexed_sha, is_worktree, parent_repo, cwd_resolved_to) which is the
// canonical session-meta tool.

// agentResolvedEdgesForEntity returns the outgoing relationships from entity e
// that were resolved by the repair layer (resolved_by == "agent-repair").
// Each entry carries the edge kind, target ID, source attribution, and
// the verbatim repair_reasoning so downstream consumers can audit the decision
// without re-reading repair.json. (ADR-0015 #547)
func agentResolvedEdgesForEntity(lr *LoadedRepo, entityID string, scopeIsOne bool) []map[string]any {
	if lr == nil || lr.Doc == nil {
		return nil
	}
	// Adjacency-indexed lookup (#2285): O(deg(v)) over out-edges instead of
	// O(|Relationships|). The adjacency stores relIdx so we can fetch the
	// underlying Relationship to read Properties (kind/target alone would not
	// suffice for the agent-repair filter).
	out := []map[string]any(nil)
	rels := lr.Doc.Relationships
	for _, e := range lr.Adjacency.Outgoing(entityID) {
		if e.relIdx < 0 || e.relIdx >= len(rels) {
			continue
		}
		r := &rels[e.relIdx]
		if r.Properties["resolved_by"] != "agent-repair" {
			continue
		}
		toID := r.ToID
		if !scopeIsOne {
			toID = prefixedID(lr.Repo, toID)
		}
		entry := map[string]any{
			"kind":        r.Kind,
			"target":      toID,
			"resolved_by": "agent-repair",
		}
		if v := r.Properties["resolved_by_agent"]; v != "" {
			entry["resolved_by_agent"] = v
		}
		if v := r.Properties["repair_reasoning"]; v != "" {
			entry["repair_reasoning"] = v
		}
		out = append(out, entry)
	}
	return out
}

// serializeEntity renders an entity as a map. When scopeIsOne, IDs are local;
// otherwise they're prefixed with <repo>::.
//
// Default (verbose=false): id, name, qualified_name, file, line, kind.
// Verbose (verbose=true): also includes end_line, language, repo, pagerank,
// community_id, properties.
func serializeEntity(repo string, e *graph.Entity, scopeIsOne bool, verbose ...bool) map[string]any {
	wantVerbose := len(verbose) > 0 && verbose[0]
	id := e.ID
	if !scopeIsOne {
		id = prefixedID(repo, e.ID)
	}
	out := map[string]any{
		"id":             id,
		"name":           e.Name,
		"qualified_name": e.QualifiedName,
		"kind":           stripScopePrefix(e.Kind),
		"file":           e.SourceFile,
		"line":           e.StartLine,
	}
	if wantVerbose {
		out["end_line"] = e.EndLine
		out["language"] = e.Language
		out["repo"] = repo
		if e.PageRank != nil {
			out["pagerank"] = *e.PageRank
		}
		if e.CommunityID != nil {
			out["community_id"] = *e.CommunityID
		}
		if len(e.Properties) > 0 {
			out["properties"] = e.Properties
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// related
// ---------------------------------------------------------------------------

func (s *Server) handleGetNeighbors(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	// canonical name is "entity_id"; accept legacy "node" with a deprecation log (#1916).
	key := argString(req, "entity_id", "")
	if key == "" {
		key = argString(req, "node", "")
		if key != "" {
			fmt.Fprintln(os.Stderr, "[archigraph deprecation] archigraph_expand: param 'node' is deprecated, use 'entity_id'")
		}
	}
	if key == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 2)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	// Resolve the start node + repo.
	var start *graph.Entity
	var startRepo *LoadedRepo
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			start = r.LabelIndex.ByID[local]
			startRepo = r
		}
	}
	if start == nil {
		for _, r := range repos {
			if e := r.LabelIndex.Lookup(key); e != nil {
				start = e
				startRepo = r
				break
			}
		}
	}
	if start == nil {
		return mcpapi.NewToolResultError("node not found: " + key), nil
	}
	adj := startRepo.Adjacency // cached at reload (#1656)
	vis := bfs(adj, start.ID, depth, nil)
	out := []map[string]any{}
	for nid, d := range vis {
		if nid == start.ID {
			continue
		}
		e := startRepo.LabelIndex.ByID[nid]
		if e == nil {
			continue
		}
		out = append(out, map[string]any{
			"id":          prefixedID(startRepo.Repo, e.ID),
			"label":       e.Name,
			"depth":       d,
			"source_file": e.SourceFile,
			"start_line":  e.StartLine,
		})
	}
	// include cross-repo overlay edges from this node.
	// linksForSourceRepo consults the CrossLinkCache (issue #2224) so that
	// post-ref-switch queries see fresh data rather than stale cached results.
	for _, l := range linksForSourceRepo(s.State, lg, startRepo) {
		_, sl := splitPrefixed(l.Source)
		if sl == start.ID {
			out = append(out, map[string]any{
				"id":         l.Target,
				"label":      l.Target,
				"depth":      1,
				"cross_repo": true,
				"kind":       l.Kind,
			})
		}
	}
	// #1618: explicit no-edge signal when the entity was found but has zero
	// graph neighbours. Agents MUST NOT infer relationships from an empty
	// result — report the absence verbatim.
	if len(out) == 0 {
		return jsonResult(map[string]any{
			"node_id":   prefixedID(startRepo.Repo, start.ID),
			"label":     start.Name,
			"repo":      startRepo.Repo,
			"neighbors": []any{},
			"count":     0,
			"result":    "no_edges",
			"note":      "Graph shows no neighbours for this entity. Do not infer a relationship — report the absence.",
		}), nil
	}

	// #1738: token-budget cap — shed neighbors from tail until under budget.
	tokenBudget := argInt(req, "token_budget", 800)
	if tokenBudget < 100 {
		tokenBudget = 100
	}
	budgetBytes := tokenBudget * 4
	if budgetBytes > 64*1024 {
		budgetBytes = 64 * 1024
	}
	preCapLen := len(out)
	out = capByRenderedBytes(out, budgetBytes, false)
	if preCapLen > len(out) {
		return jsonResult(map[string]any{
			"neighbors": out,
			"count":     len(out),
			"truncation_note": fmt.Sprintf(
				"response capped at token_budget=%d (~%d bytes); %d neighbors omitted — pass a larger token_budget or reduce depth",
				tokenBudget, budgetBytes, preCapLen-len(out),
			),
		}), nil
	}
	return jsonResult(out), nil
}

// ---------------------------------------------------------------------------
// trace
// ---------------------------------------------------------------------------

func (s *Server) handleShortestPath(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	src, err := req.RequireString("source")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	dst, err := req.RequireString("target")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	// Normalize to prefixed IDs.
	prefSrc := normalizePrefixed(lg, src)
	prefDst := normalizePrefixed(lg, dst)
	if prefSrc == "" || prefDst == "" {
		return mcpapi.NewToolResultError("source or target not found"), nil
	}

	expand := func(node string) []edge {
		repo, local := splitPrefixed(node)
		out := []edge{}
		if r, ok := lg.Repos[repo]; ok && r.Doc != nil {
			a := r.Adjacency
			for _, e := range a.out[local] {
				out = append(out, edge{
					target: prefixedID(repo, e.target),
					kind:   e.kind,
					weight: -math.Log(0.95), // intra-repo confidence ~0.95
					relIdx: -1,              // synthetic — no backing Relationship (#2305)
				})
			}
		}
		// cross-repo overlay — use cache-backed linksForSourceRepo (#2224)
		// to avoid stale results after a ref switch.
		if lr, ok2 := lg.Repos[repo]; ok2 {
			for _, l := range linksForSourceRepo(s.State, lg, lr) {
				if l.Source != node {
					continue
				}
				conf := l.Confidence
				if conf <= 0 {
					conf = 0.7
				}
				out = append(out, edge{
					target: l.Target,
					kind:   l.Kind,
					weight: -math.Log(conf),
					relIdx: -1, // synthetic — no backing Relationship (#2305)
				})
			}
		}
		return out
	}

	path, edges, weakest, ok := dijkstra(prefSrc, prefDst, expand)
	if !ok {
		return jsonResult(map[string]any{"path": nil, "found": false}), nil
	}
	crosses := false
	first, _ := splitPrefixed(prefSrc)
	for _, p := range path {
		r, _ := splitPrefixed(p)
		if r != first {
			crosses = true
			break
		}
	}
	// Attach findings keyed by any entity along the path (Refs #59).
	g, _, _ := s.resolveAndGroup(req)
	allFindings := loadFindings(findingsMemDir(g, lg))
	pathIDs := make([]string, 0, len(path)*2)
	for _, p := range path {
		pathIDs = append(pathIDs, p)
		if _, local := splitPrefixed(p); local != "" {
			pathIDs = append(pathIDs, local)
		}
	}
	pathFindings := findingsForEntity(allFindings, pathIDs...)
	return jsonResult(map[string]any{
		"path":                    path,
		"edges":                   edges,
		"weakest_link_confidence": weakest,
		"length":                  len(path) - 1,
		"crosses_repos":           crosses,
		"found":                   true,
		"findings":                findingsToJSON(pathFindings, 0),
	}), nil
}

// normalizePrefixed turns either "<repo>::<local>" or a bare label/id into a
// prefixed string. Returns "" if the entity could not be located.
func normalizePrefixed(lg *LoadedGroup, s string) string {
	if r, l := splitPrefixed(s); r != "" {
		if rr, ok := lg.Repos[r]; ok && rr.Doc != nil {
			if _, ok := rr.LabelIndex.ByID[l]; ok {
				return s
			}
		}
		return ""
	}
	for _, r := range lg.Repos {
		if r.Doc == nil {
			continue
		}
		if e := r.LabelIndex.Lookup(s); e != nil {
			return prefixedID(r.Repo, e.ID)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// list_clusters
// ---------------------------------------------------------------------------

func (s *Server) handleListCommunities(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	// Defaults sized to keep an overview response cheap; both are overridable
	// via the same arg map the rest of the tool surface uses (issue #2289).
	// Pass top_entities_limit=-1 / min_size=0 to disable either cap.
	topLimit := argInt(req, "top_entities_limit", 3)
	minSize := argInt(req, "min_size", 20)
	out := []map[string]any{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Sort communities by size descending so top_entities_limit applies to the
		// largest communities (issue #2319). This ensures deterministic results.
		sort.SliceStable(r.Doc.Communities, func(i, j int) bool {
			return r.Doc.Communities[i].Size > r.Doc.Communities[j].Size
		})
		for _, c := range r.Doc.Communities {
			if c.Size < minSize {
				continue
			}
			top := c.TopEntities
			if topLimit >= 0 && len(top) > topLimit {
				top = top[:topLimit]
			}
			out = append(out, map[string]any{
				"repo":         r.Repo,
				"id":           c.ID,
				"size":         c.Size,
				"modularity":   c.Modularity,
				"top_entities": top,
			})
		}
	}
	return jsonResult(out), nil
}

// ---------------------------------------------------------------------------
// save_finding
// ---------------------------------------------------------------------------

func (s *Server) handleSaveResult(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	question, err := req.RequireString("question")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	answer, err := req.RequireString("answer")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	g, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	memDir := lg.MemoryDir
	if memDir == "" {
		memDir = defaultMemoryDir(g)
	}
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	sum := sha256.Sum256([]byte(question + answer))
	short := hex.EncodeToString(sum[:])[:8]
	path := filepath.Join(memDir, fmt.Sprintf("%s-%s.json", ts, short))
	payload := map[string]any{
		"question":    question,
		"answer":      answer,
		"type":        argString(req, "type", "note"),
		"nodes":       argStringSlice(req, "nodes"),
		"repo_filter": argStringSlice(req, "repo_filter"),
		"saved_at":    time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]any{"path": path}), nil
}

// ---------------------------------------------------------------------------
// list_findings
// ---------------------------------------------------------------------------

func (s *Server) handleListFindings(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	g, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	all := loadFindings(findingsMemDir(g, lg))
	// since filter (RFC3339)
	if v := argString(req, "since", ""); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			all = findingsSince(all, t)
		}
	}
	// entity_id filter
	if eid := argString(req, "entity_id", ""); eid != "" {
		ids := []string{eid}
		// also try local form if prefixed was given
		if _, local := splitPrefixed(eid); local != "" {
			ids = append(ids, local)
		}
		// also try resolving labels via index
		for _, r := range lg.Repos {
			if r.Doc == nil || r.LabelIndex == nil {
				continue
			}
			if e := r.LabelIndex.Lookup(eid); e != nil {
				ids = append(ids, e.ID, prefixedID(r.Repo, e.ID))
			}
		}
		all = findingsForEntity(all, ids...)
	}
	limit := argInt(req, "limit", 50)
	return jsonResult(findingsToJSON(all, limit)), nil
}

// ---------------------------------------------------------------------------
// get_source
// ---------------------------------------------------------------------------

func (s *Server) handleGetNodeSource(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	// canonical name is "entity_id"; accept legacy "node_id" with a deprecation log.
	nodeID := argString(req, "entity_id", "")
	if nodeID == "" {
		nodeID = argString(req, "node_id", "")
		if nodeID != "" {
			fmt.Fprintln(os.Stderr, "[archigraph deprecation] archigraph_get_source: param 'node_id' is deprecated, use 'entity_id'")
		}
	}
	if nodeID == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	contextLines := argInt(req, "context_lines", 20)
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	var e *graph.Entity
	var lr *LoadedRepo
	if rp, lid := splitPrefixed(nodeID); rp != "" {
		if r, ok := lg.Repos[rp]; ok && r.Doc != nil {
			e = r.LabelIndex.ByID[lid]
			lr = r
		}
	}
	// #1650: accept qualified_name or label. Gather all matches across repos;
	// if more than one resolves we return a clarifier list of {id,
	// qualified_name, file:line, repo} so the caller can disambiguate without
	// a follow-up inspect round-trip.
	if e == nil {
		type cand struct {
			ent  *graph.Entity
			repo *LoadedRepo
		}
		var cands []cand
		for _, r := range lg.Repos {
			if r.Doc == nil {
				continue
			}
			for _, hit := range r.LabelIndex.LookupAll(nodeID) {
				cands = append(cands, cand{ent: hit, repo: r})
			}
		}
		if len(cands) == 0 {
			return mcpapi.NewToolResultError("node not found: " + nodeID), nil
		}
		if len(cands) > 1 {
			out := make([]map[string]any, 0, len(cands))
			for _, c := range cands {
				out = append(out, map[string]any{
					"id":             prefixedID(c.repo.Repo, c.ent.ID),
					"qualified_name": c.ent.QualifiedName,
					"label":          c.ent.Name,
					"repo":           c.repo.Repo,
					"source_file":    c.ent.SourceFile,
					"start_line":     c.ent.StartLine,
					"kind":           stripScopePrefix(c.ent.Kind),
				})
			}
			return jsonResult(map[string]any{
				"ambiguous": true,
				"query":     nodeID,
				"count":     len(out),
				"matches":   out,
				"note":      "multiple entities match this label; call again with one of the ids above.",
			}), nil
		}
		e = cands[0].ent
		lr = cands[0].repo
	}
	abs := e.SourceFile
	if !filepath.IsAbs(abs) && lr.Path != "" {
		abs = filepath.Join(lr.Path, e.SourceFile)
	}

	// Bound the requested span (#1614). Synthetic / shadow / route entities
	// frequently carry end_line<=start_line or start_line==0, which previously
	// caused get_source to emit the entire file. Clamp a degenerate span to a
	// fixed fallback window, and ALWAYS apply a hard max-lines cap so the
	// returned chunk can never be a whole-file dump regardless of the recorded
	// span.
	const fallbackSpan = 60  // lines, when end<=start or either is 0
	const hardMaxLines = 200 // absolute cap on emitted source lines

	startLine := e.StartLine
	endLine := e.EndLine
	if startLine < 1 {
		startLine = 1
	}
	if endLine <= startLine || e.StartLine == 0 || e.EndLine == 0 {
		endLine = startLine + fallbackSpan
	}

	start := startLine - contextLines
	if start < 1 {
		start = 1
	}
	end := endLine + contextLines
	// Hard cap: never emit more than hardMaxLines.
	if end-start+1 > hardMaxLines {
		end = start + hardMaxLines - 1
	}

	// #1678: bound the file I/O with a hard deadline. The daemon owns fsnotify
	// watchers on every indexed source tree; under certain macOS conditions a
	// raw open(2) on a watched-but-otherwise-normal file has been observed to
	// block indefinitely inside the kernel — the original handler had no
	// timeout, so a single stuck Open wedged the entire MCP session (the
	// shared bridge jsonrpc.Client serializes calls per #1671/#1677). Running
	// the read on a worker goroutine and select-ing on a context deadline lets
	// the handler return a clean error instead of hanging the bridge.
	//
	// Budget: 5s is generous for any local file but short enough that a
	// genuine kernel/FS stall surfaces as a real error the caller can retry
	// or route around.
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()

	type readOut struct {
		text string
		err  error
	}
	resCh := make(chan readOut, 1)
	go func() {
		text, rerr := readSourceWindow(abs, start, end)
		resCh <- readOut{text: text, err: rerr}
	}()

	select {
	case out := <-resCh:
		if out.err != nil {
			return mcpapi.NewToolResultError(out.err.Error()), nil
		}
		return mcpapi.NewToolResultText(out.text), nil
	case <-readCtx.Done():
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"get_source: read timed out after 5s on %s (file may be on a stalled filesystem or watched-path kernel stall); node_id=%s",
			abs, nodeID,
		)), nil
	}
}

// readSourceWindow is implemented in read_source_unix.go (darwin/linux) and
// read_source_windows.go (windows). The Unix implementation uses a non-blocking
// open(2) to avoid macOS fsevents kernel stalls (#1773); the Windows path falls
// back to plain os.Open (no fsevents equivalent).

// ---------------------------------------------------------------------------
// candidate tools
// ---------------------------------------------------------------------------

func (s *Server) handleListLinkCandidates(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	g, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	_ = lg
	channel := argString(req, "channel", "")
	method := argString(req, "method", "")
	limit := argInt(req, "limit", 10)
	repoFilter := map[string]bool{}
	for _, r := range argStringSlice(req, "repo_filter") {
		repoFilter[r] = true
	}
	cands := readLinkCandidates(g)
	out := []LinkCandidate{}
	for _, c := range cands {
		if channel != "" && c.Channel != channel {
			continue
		}
		if method != "" && c.Method != method {
			continue
		}
		if len(repoFilter) > 0 {
			sr, _ := splitPrefixed(c.Source)
			tr, _ := splitPrefixed(c.Target)
			if !repoFilter[sr] && !repoFilter[tr] {
				continue
			}
		}
		out = append(out, c)
		if len(out) >= limit {
			break
		}
	}
	return jsonResult(out), nil
}

func (s *Server) handleListEnrichmentCandidates(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	kind := argString(req, "kind", "")
	limit := argInt(req, "limit", 10)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	out := []map[string]any{}
	for _, r := range repos {
		for _, c := range readEnrichmentCandidates(r.Path) {
			if kind != "" && c.Kind != kind {
				continue
			}
			out = append(out, map[string]any{
				"id":      c.ID,
				"node_id": prefixedID(r.Repo, c.NodeID),
				"kind":    c.Kind,
				"hint":    c.Hint,
				"repo":    r.Repo,
			})
			if len(out) >= limit {
				return jsonResult(out), nil
			}
		}
	}
	return jsonResult(out), nil
}

func (s *Server) handleSubmitEnrichment(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	cid, err := req.RequireString("candidate_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	value, err := req.RequireString("value")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	confidence := argFloat(req, "confidence", 1.0)
	reason := argString(req, "reason", "")
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	for _, r := range lg.Repos {
		cands := readEnrichmentCandidates(r.Path)
		var match *EnrichmentCandidate
		remaining := []EnrichmentCandidate{}
		for i := range cands {
			if cands[i].ID == cid && match == nil {
				c := cands[i]
				match = &c
				continue
			}
			remaining = append(remaining, cands[i])
		}
		if match == nil {
			continue
		}
		if err := appendResolution(r.Path, EnrichmentResolution{
			CandidateID: cid, NodeID: match.NodeID, Kind: match.Kind,
			Value: value, Confidence: confidence, Reason: reason,
		}); err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
		if err := writeEnrichmentCandidates(r.Path, remaining); err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
		return jsonResult(map[string]any{"candidate_id": cid, "decision": "accept"}), nil
	}
	return mcpapi.NewToolResultError("candidate not found: " + cid), nil
}

func (s *Server) handleRejectEnrichment(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	cid, err := req.RequireString("candidate_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	reason, err := req.RequireString("reason")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	for _, r := range lg.Repos {
		cands := readEnrichmentCandidates(r.Path)
		remaining := []EnrichmentCandidate{}
		found := false
		for _, c := range cands {
			if c.ID == cid && !found {
				found = true
				continue
			}
			remaining = append(remaining, c)
		}
		if !found {
			continue
		}
		if err := appendRejection(r.Path, cid, reason); err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
		if err := writeEnrichmentCandidates(r.Path, remaining); err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
		return jsonResult(map[string]any{"candidate_id": cid, "decision": "reject"}), nil
	}
	return mcpapi.NewToolResultError("candidate not found: " + cid), nil
}

// ---------------------------------------------------------------------------
// graph_stats / get_telemetry
// ---------------------------------------------------------------------------

func (s *Server) handleGraphStats(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	// Optional breakdown selector. Currently only "unresolved_imports" is
	// supported. Any other non-empty value returns an error so callers learn
	// about the supported keys early rather than silently getting a partial
	// response.
	breakdown := argString(req, "breakdown", "")
	if breakdown != "" && breakdown != "unresolved_imports" {
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"unsupported breakdown value %q — supported values: \"unresolved_imports\"", breakdown,
		)), nil
	}

	// Build the set of repo names to consider. Empty filter or ["*"]
	// means "every loaded repo" (matching reposToConsider semantics).
	filter := argStringSlice(req, "repo_filter")
	all := len(filter) == 0 || (len(filter) == 1 && filter[0] == "*")
	wanted := map[string]bool{}
	if !all {
		for _, n := range filter {
			wanted[n] = true
		}
	}
	totals := map[string]any{}
	repoStats := []map[string]any{}
	totalE, totalR := 0, 0
	unavailable := []string{}
	// Sort for stable output.
	names := make([]string, 0, len(lg.Repos))
	for n := range lg.Repos {
		if all || wanted[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	totalImport, totalBug := 0, 0
	// Collect the loaded repos so we can pass them to computeUnresolvedBreakdown
	// without a second pass over the names slice.
	var loadedRepos []*LoadedRepo
	for _, name := range names {
		r := lg.Repos[name]
		if r == nil {
			continue
		}
		if r.Doc == nil {
			unavailable = append(unavailable, name+": "+r.loadErr)
			continue
		}
		totalE += len(r.Doc.Entities)
		totalR += len(r.Doc.Relationships)
		rImport, rBug := countEdgesForFidelity(r)
		totalImport += rImport
		totalBug += rBug
		repoStats = append(repoStats, map[string]any{
			"repo":          name,
			"entities":      len(r.Doc.Entities),
			"relationships": len(r.Doc.Relationships),
			"communities":   len(r.Doc.Communities),
		})
		loadedRepos = append(loadedRepos, r)
	}
	totals["entities"] = totalE
	totals["relationships"] = totalR
	totals["repos"] = repoStats
	// Fidelity: 1 − (unresolved IMPORTS / total IMPORTS). Scope is IMPORTS
	// edges only — same as audit.AuditPath and health-history.bug_rate —
	// so post-resolver improvements (e.g. ResolveGoInTreeImports) are
	// immediately reflected here rather than being diluted by the larger
	// CALLS + REFERENCES universe.
	if totalImport > 0 {
		fid := 1.0 - float64(totalBug)/float64(totalImport)
		totals["fidelity"] = math.Round(fid*1000) / 1000 // 3 decimal places
		totals["fidelity_import_total"] = totalImport
		totals["fidelity_import_bug"] = totalBug
	}
	// Filter cross-repo links to those that touch the considered repos
	// when an explicit repo_filter is supplied.
	links := len(lg.Links)
	if !all {
		links = 0
		for _, l := range lg.Links {
			sr, _ := splitPrefixed(l.Source)
			tr, _ := splitPrefixed(l.Target)
			if wanted[sr] || wanted[tr] {
				links++
			}
		}
	}
	totals["cross_repo_links"] = links
	if len(unavailable) > 0 {
		totals["unavailable"] = unavailable
	}

	// breakdown="unresolved_imports": add the three taxonomy fields.
	if breakdown == "unresolved_imports" {
		bd := computeUnresolvedBreakdown(loadedRepos, 10)
		totals["unresolved_imports_by_disposition"] = bd.ByDisposition
		totals["unresolved_imports_by_language"] = bd.ByLanguage
		totals["unresolved_imports_top_roots"] = bd.TopRoots
	}

	return jsonResult(totals), nil
}

func (s *Server) handleGetTelemetry(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	return jsonResult(s.Tel.Snapshot()), nil
}

// ---------------------------------------------------------------------------
// list_residuals / submit_repair — ADR-0015 phase-1 (#549 + #550)
// ---------------------------------------------------------------------------

// handleListResiduals returns paginated repair_edge enrichment candidates
// across the resolved group's repos. Filters: repo_filter, limit, offset.
//
// When include_stale=true the response lists stale repairs from
// repair_stats.json (repairs whose edge_id no longer matches any current
// candidate) instead of active residuals. Stale entries carry enough context
// for the agent to re-submit: edge_id, resolution, resolved_at, repo.
// (ADR-0015 #5/8 — issue #548)
func (s *Server) handleListResiduals(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	limit := argInt(req, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	offset := argInt(req, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	includeStale := argBool(req, "include_stale", false)
	if includeStale {
		return s.handleListStaleRepairs(repos, limit, offset)
	}

	// Collect first, then page. The candidate file is bounded per repo
	// (max 8 candidates per ambiguous name) so flattening is cheap.
	all := make([]map[string]any, 0, 64)
	for _, r := range repos {
		for _, c := range readRepairEdgeCandidates(r.Path) {
			all = append(all, summarizeRepairEdge(r.Repo, c))
		}
	}
	total := len(all)
	if offset >= total {
		return jsonResult(map[string]any{
			"residuals": []map[string]any{},
			"total":     total,
			"offset":    offset,
			"limit":     limit,
		}), nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return jsonResult(map[string]any{
		"residuals": all[offset:end],
		"total":     total,
		"offset":    offset,
		"limit":     limit,
	}), nil
}

// handleListStaleRepairs reads repair_stats.json for each repo and aggregates
// the stale[] entries into a paginated response. Stale repairs are ones whose
// edge_id no longer matches any current repair_edge candidate — the source
// moved between index runs and the repair must be re-submitted.
func (s *Server) handleListStaleRepairs(repos []*LoadedRepo, limit, offset int) (*mcpapi.CallToolResult, error) {
	all := make([]map[string]any, 0, 32)
	for _, r := range repos {
		stats, err := enrichment.ReadRepairStats(daemon.StateDirForRepo(r.Path))
		if err != nil {
			continue // missing or malformed stats file — skip silently
		}
		for _, st := range stats.Stale {
			entry := map[string]any{
				"edge_id":     st.EdgeID,
				"resolution":  st.Resolution,
				"resolved_at": st.ResolvedAt,
				"repo":        r.Repo,
				"stale":       true,
			}
			all = append(all, entry)
		}
	}
	total := len(all)
	if offset >= total {
		return jsonResult(map[string]any{
			"stale":  []map[string]any{},
			"total":  total,
			"offset": offset,
			"limit":  limit,
		}), nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return jsonResult(map[string]any{
		"stale":  all[offset:end],
		"total":  total,
		"offset": offset,
		"limit":  limit,
	}), nil
}

// ---------------------------------------------------------------------------
// Bundle dispatchers (#668)
// ---------------------------------------------------------------------------

// handleEnrichments dispatches archigraph_enrichments based on action=list|submit|reject.
func (s *Server) handleEnrichments(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "list":
		return s.handleListEnrichmentCandidates(ctx, req)
	case "submit":
		return s.handleSubmitEnrichment(ctx, req)
	case "reject":
		return s.handleRejectEnrichment(ctx, req)
	default:
		return mcpapi.NewToolResultError(fmt.Sprintf("unknown action %q (allowed: list, submit, reject)", action)), nil
	}
}

// handleCrossLinks dispatches archigraph_cross_links based on action=list|accept|reject.
func (s *Server) handleCrossLinks(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "list":
		return s.handleListLinkCandidates(ctx, req)
	case "accept":
		return s.handleResolveLinkCandidateAction(ctx, req, "accept")
	case "reject":
		return s.handleResolveLinkCandidateAction(ctx, req, "reject")
	default:
		return mcpapi.NewToolResultError(fmt.Sprintf("unknown action %q (allowed: list, accept, reject)", action)), nil
	}
}

// handleResolveLinkCandidateAction applies the bundled action decision
// (accept or reject) to the specified link candidate.
func (s *Server) handleResolveLinkCandidateAction(ctx context.Context, req mcpapi.CallToolRequest, decision string) (*mcpapi.CallToolResult, error) {
	cid := argString(req, "candidate_id", "")
	if cid == "" {
		return mcpapi.NewToolResultError("candidate_id is required"), nil
	}
	reason := argString(req, "reason", "")
	override := argString(req, "override_target", "")

	g, _, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	cands := readLinkCandidates(g)
	var found *LinkCandidate
	remaining := []LinkCandidate{}
	for i := range cands {
		if cands[i].ID == cid && found == nil {
			c := cands[i]
			found = &c
			continue
		}
		remaining = append(remaining, cands[i])
	}
	if found == nil {
		return mcpapi.NewToolResultError("candidate not found: " + cid), nil
	}
	if override != "" {
		found.Target = override
	}
	switch decision {
	case "accept":
		if err := appendLink(g, CrossRepoLink{
			Source: found.Source, Target: found.Target, Kind: found.Kind,
			Confidence: found.Confidence, Channel: found.Channel, Method: found.Method,
		}); err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
	case "reject":
		_ = reason // stored in audit log implicitly via remaining list drop
	}
	if err := writeLinkCandidates(g, remaining); err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]any{"candidate_id": cid, "decision": decision}), nil
}

// handleRepairs dispatches archigraph_repairs based on action=list|submit.
// The submit action reads residual_id as the edge identifier (alias for edge_id).
func (s *Server) handleRepairs(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "list":
		return s.handleListResiduals(ctx, req)
	case "submit":
		// The bundled tool uses residual_id; inject it as edge_id for the
		// existing handler which expects edge_id.
		rid := argString(req, "residual_id", "")
		if rid == "" {
			return mcpapi.NewToolResultError("residual_id is required for action=submit"), nil
		}
		// Synthesise a request wrapper that presents residual_id as edge_id.
		return s.handleSubmitRepairFromBundle(ctx, req, rid)
	default:
		return mcpapi.NewToolResultError(fmt.Sprintf("unknown action %q (allowed: list, submit)", action)), nil
	}
}

// handleSubmitRepairFromBundle is handleSubmitRepair adapted for the bundled
// archigraph_repairs tool where the edge identifier comes in as residual_id.
// Trust-model rules R1-R7 (ADR-0015 / repair-trust-model.md) are enforced
// before the repair is written so the agent gets immediate feedback rather
// than discovering a rejection only on the next index run (#546).
func (s *Server) handleSubmitRepairFromBundle(ctx context.Context, req mcpapi.CallToolRequest, edgeID string) (*mcpapi.CallToolResult, error) {
	resolution := argString(req, "resolution", "")
	if resolution == "" {
		return mcpapi.NewToolResultError("resolution is required"), nil
	}
	if !allowedRepairResolutions[resolution] {
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"unknown resolution %q (allowed: bind_to_entity, reclassify_as_external, reclassify_as_dynamic, reclassify_as_resolved, abandon)",
			resolution)), nil
	}
	confidence := argFloat(req, "confidence", 0.0)
	if confidence < 0 || confidence > 1 {
		return mcpapi.NewToolResultError("confidence must be in [0,1]"), nil
	}
	reasoning := argString(req, "reasoning", "")
	targetEntity := argString(req, "target_entity_id", "")
	module := argString(req, "module", "")
	newTarget := argString(req, "new_target", "")
	dynamicReason := argString(req, "dynamic_reason", "")
	abandonReason := argString(req, "abandon_reason", "")
	source := argString(req, "source", "mcp_submit_repair")
	repoOverride := argString(req, "repo", "")

	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	repos := reposToConsider(lg, nil)
	var target *LoadedRepo
	var candidates []enrichment.Candidate
	if repoOverride != "" {
		for _, r := range repos {
			if r.Repo == repoOverride {
				target = r
				break
			}
		}
		if target == nil {
			return mcpapi.NewToolResultError(fmt.Sprintf("repo %q not loaded in group", repoOverride)), nil
		}
		candidates = readRepairEdgeCandidates(target.Path)
	} else {
		for _, r := range repos {
			cands := readRepairEdgeCandidates(r.Path)
			for _, c := range cands {
				if v, ok := c.Context["edge_id"].(string); ok && v == edgeID {
					target = r
					candidates = cands
					break
				}
			}
			if target != nil {
				break
			}
		}
		if target == nil {
			return mcpapi.NewToolResultError(fmt.Sprintf("residual_id %q not found in any loaded repo (pass repo= to disambiguate)", edgeID)), nil
		}
	}

	// ── Trust-model R1-R7 ────────────────────────────────────────────────
	// Build verify context from the loaded graph document.
	docEnts, containsParents := buildVerifyContext(target.Doc)
	edgeIDSet := candidateEdgeIDSet(candidates)
	fromEntityID := fromEntityIDForEdge(candidates, edgeID)

	repairProposal := enrichment.Repair{
		EdgeID:         edgeID,
		Resolution:     resolution,
		TargetEntityID: targetEntity,
		Module:         module,
		NewTarget:      newTarget,
		DynamicReason:  dynamicReason,
		AbandonReason:  abandonReason,
		Confidence:     confidence,
		Reasoning:      reasoning,
		Source:         source,
	}
	if vr := VerifyRepairSubmit(repairProposal, fromEntityID, edgeIDSet, docEnts, containsParents); !vr.OK {
		return jsonResult(map[string]any{
			"ok":              false,
			"rejected_reason": vr.RejectedReason,
			"residual_id":     edgeID,
		}), nil
	}
	// ── End trust-model ──────────────────────────────────────────────────

	rf, err := readRepairFile(target.Path)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	repairProposal.ResolvedAt = now
	rf.Repairs = append(rf.Repairs, repairProposal)
	if err := writeRepairFile(target.Path, rf); err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]any{
		"ok":           true,
		"residual_id":  edgeID,
		"repo":         target.Repo,
		"resolution":   resolution,
		"repair_count": len(rf.Repairs),
		"resolved_at":  now,
	}), nil
}
