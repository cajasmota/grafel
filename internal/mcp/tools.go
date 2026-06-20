package mcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/enrichment"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/substrate"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// argMinConfidence reads the universal --min_confidence flag (Phase 1C, #2769).
// Default 0.0 means "no filter". Values are clamped to [0, 1].
func argMinConfidence(req mcpapi.CallToolRequest) float64 {
	v := argFloat(req, "min_confidence", 0.0)
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// entityPassesConfidence reports whether an Entity's effective confidence
// clears the threshold. A threshold of 0 always passes (issue #2769 default).
func entityPassesConfidence(e *graph.Entity, minConfidence float64) bool {
	if minConfidence <= 0 || e == nil {
		return true
	}
	return types.EffectiveConfidence(e.Confidence) >= minConfidence
}

// relPassesConfidence reports whether a Relationship's effective confidence
// clears the threshold.
func relPassesConfidence(r *graph.Relationship, minConfidence float64) bool {
	if minConfidence <= 0 || r == nil {
		return true
	}
	return types.EffectiveConfidence(r.Confidence) >= minConfidence
}

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
// context to agents (grafel_whoami, grafel_inspect, etc.).
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

// mcpWireVersion is the MCP API version advertised by grafel_whoami.
// Bump this on every MINOR release. Agents can use it to detect
// incompatible daemon versions without inspecting the binary.
const mcpWireVersion = "0.1.0"

// sessionMeta returns the session-stable ref/sha/worktree fields for a given
// CWD resolution. These fields are exclusive to grafel_whoami (#2335,
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

	// Fix #2: when an explicit group= was provided, indexed_ref/indexed_sha must
	// reflect the queried group's canonical repo, not the cwd's repo. We derive
	// them from the group's loaded repos (first alphabetically) so the index
	// fields correspond to the group being queried.
	//
	// If no explicit group was given, fall back to the cwd-derived values as
	// before — that preserves all existing behaviour.
	var indexedRef, indexedSHA string
	if explicit != "" {
		// Resolve ref/sha from the queried group's repos (not cwd).
		indexedRef, indexedSHA = groupIndexedRefSHA(s.State, group)
	} else {
		indexedRef = cwdRes.Ref
		indexedSHA = cwdRes.SHA
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
		// Fix #2: indexed_ref/indexed_sha now key off the queried group when
		// an explicit group= is supplied (not the cwd's repo).
		"indexed_ref": indexedRef,
		"indexed_sha": indexedSHA,
		"parent_repo": sm["parent_repo"],
	}
	if cwdRes.Source == "worktree" {
		resp["tier"] = "cold" // worktree refs may not be hot-loaded yet
	} else {
		resp["tier"] = "hot"
	}

	// Nudge suppression: GRAFEL_WHOAMI_NUDGE=quiet disables doc-state fields.
	if os.Getenv("GRAFEL_WHOAMI_NUDGE") == "quiet" {
		return jsonResult(resp), nil
	}

	// Enrich with documentation state + action counts.
	lg := s.State.Group(group)
	if lg == nil {
		return jsonResult(resp), nil
	}

	// Fix #1: add an index-state block so agents always see that entities are
	// present. Without this, a fully-indexed group read as "empty / never
	// indexed" because the response only contained docgen fields — causing
	// agents to fall back to grep (the "docgen-trap").
	//
	// entity_count and relationship_count mirror the totals reported by
	// grafel_stats so the two tools always agree. tests_edges is the count
	// of TESTS-kind relationships aggregated across all loaded repos.
	totalE, totalR, totalTests := groupIndexCounts(lg)
	resp["entity_count"] = totalE       // top-level for immediate visibility
	resp["relationship_count"] = totalR // top-level for immediate visibility
	resp["index"] = map[string]any{
		"entity_count":       totalE,
		"relationship_count": totalR,
		"tests_edges":        totalTests,
		"indexed_sha":        indexedSHA,
		"indexed_ref":        indexedRef,
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

// groupIndexCounts returns the total entity count, relationship count, and
// TESTS-edge count aggregated across all loaded repos in the group. These
// values are computed the same way grafel_stats does so the two tools
// always agree (Fix #1, the "docgen-trap" in grafel_whoami).
//
// All three counts are read from pre-computed fields on LoadedRepo that are
// populated once at graph-load time (on mtime change). This makes whoami O(1)
// per repo rather than O(N) over all relationships (#3325 perf fix).
func groupIndexCounts(lg *LoadedGroup) (entities, relationships, testsEdges int) {
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		entities += len(r.Doc.Entities)
		relationships += len(r.Doc.Relationships)
		testsEdges += r.TestsEdgeCount
	}
	return
}

// groupIndexedRefSHA returns the git ref and abbreviated SHA for the canonical
// repo of a group. When the group has multiple repos the first one in
// alphabetical slug order is used (deterministic). This is used by Fix #2 so
// that an explicit group= query returns the queried group's ref/sha rather than
// the cwd's ref/sha.
//
// Performance (#3372): the loaded graph already stores the ref/SHA it was
// indexed at in Doc.IndexedRef / Doc.IndexedSHA. Reading those fields is O(1)
// with no subprocess. This is also MORE correct: indexed_sha should reflect
// the SHA the graph was actually built from, not the current HEAD (which may
// be ahead of what is indexed).
//
// gitmeta.Capture is retained ONLY as a fallback for graphs built before the
// IndexedSHA field was added (pre-PH0, #2088) — those will have an empty SHA.
//
// Returns ("", "") when the group is unknown or has no loaded repos.
func groupIndexedRefSHA(s *State, groupName string) (ref, sha string) {
	if s == nil {
		return "", ""
	}

	// Fast path: read ref/SHA from the resident loaded graph (O(1), no subprocess).
	lg := s.Group(groupName)
	if lg != nil && len(lg.Repos) > 0 {
		slugs := make([]string, 0, len(lg.Repos))
		for slug := range lg.Repos {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			lr := lg.Repos[slug]
			if lr == nil || lr.Doc == nil {
				continue
			}
			if lr.Doc.IndexedSHA != "" {
				// Graph was indexed after PH0 (#2088) — use the stored values.
				return lr.Doc.IndexedRef, lr.Doc.IndexedSHA
			}
			// Graph predates the IndexedSHA field — fall through to gitmeta.
			if lr.Path != "" {
				meta := gitmeta.CaptureCached(lr.Path)
				return meta.Ref, meta.SHA
			}
		}
	}

	// Fallback: no loaded group yet (graph not yet read into memory). Resolve
	// via the registry entry so the first-ever whoami call before graph load
	// still returns something meaningful.
	gentry, ok := s.registry.Groups[groupName]
	if !ok {
		return "", ""
	}
	slugs := make([]string, 0, len(gentry.Repos))
	for slug := range gentry.Repos {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		repo := gentry.Repos[slug]
		if repo.Path == "" {
			continue
		}
		meta := gitmeta.CaptureCached(repo.Path)
		return meta.Ref, meta.SHA
	}
	return "", ""
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
			fmt.Fprintln(os.Stderr, "[grafel deprecation] grafel_find: param 'question' is deprecated, use 'query'")
		}
	}
	if question == "" {
		return mcpapi.NewToolResultError("missing required argument: query"), nil
	}
	var err error
	_ = err // retained for symmetry with other handlers
	resolvedGroup, lg, errRes := s.resolveAndGroup(req)
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
	crossRepo := argBool(req, "cross_repo", false)
	contextFilter := contextFilterSet(argStringSlice(req, "context_filter"))
	mode := argString(req, "mode", "bfs")
	minConfidence := argMinConfidence(req) // #2769 Phase 1C

	// #2643: default to cwd-resolved repo to avoid cross-repo noise.
	// Priority order:
	//   1. repo_filter set explicitly → use it (existing behaviour).
	//   2. cross_repo=true           → search all repos (new opt-in).
	//   3. neither, cwd resolves to THIS group → filter to the cwd-resolved repo.
	//   4. neither, cwd unresolved / other group → search all repos in the
	//      resolved group (graceful fallback).
	//
	// #4286: the cwd-repo default must only apply when the cwd actually resolves
	// to the group we're serving. resolveAndGroup honors an explicit `group=`
	// param, so the served group can differ from the cwd's group (e.g. caller is
	// inside repo of group A but passes group=B). In that case ResolveCWD returns
	// a RepoSlug from group A that does NOT exist in group B's loaded repo set,
	// so reposToConsider yields zero repos → "no repos loaded for this group".
	// The other group-scoped tools (search_entities, neighbors, stats, …) never
	// pin to a cwd repo, so they serve an explicit group correctly. We align find
	// with them: skip the cwd-repo default unless the cwd's group matches the
	// served group.
	if len(repoFilter) == 0 && !crossRepo {
		cwd := s.inferCWD(req)
		cwdRes := ResolveCWD(s.State, cwd)
		if cwdRes.RepoSlug != "" && cwdRes.Group == resolvedGroup {
			repoFilter = []string{cwdRes.RepoSlug}
		}
		// If cwd doesn't resolve, or resolves to a different group, repoFilter
		// stays nil → reposToConsider returns all repos in the served group.
	}

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
		bm25Hits := r.getBM25().Search(question, 10)
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
				byID := r.getByID()
				for _, s := range semIDs {
					if e, ok := byID[s.ID]; ok {
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

	// min_score cutoff (#1747 / #5289): drop ranked hits below the threshold
	// after re-ranking so the tail is clean. Applied honestly: when minScore > 0,
	// every hit below the bar is removed — even if that empties the list.
	//
	// #5289: the prior implementation kept the ENTIRE sub-threshold list when the
	// cull would leave it empty ("preserve at least one hit"). For a
	// zero-selectivity query (terms that BM25-match weakly or not at all), every
	// candidate scored below 0.15, so the guard preserved up to `limit` near-zero
	// (displayed score=0.00) hits per repo. Those leaked seeds then drove an
	// unbounded depth-3 BFS that dumped the whole repo (6915 nodes / 2.6M-edge
	// summary / ~113s) — see the bounded-expansion logic below. We now let the
	// cull empty the list; the scoped "always-1" fallback (a SINGLE node) handles
	// the empty case, and a zero-selectivity result is surfaced honestly rather
	// than expanded.
	if minScore > 0 && len(all) > 0 {
		// Keep hits whose BM25/RRF score clears the bar. Since re-rank tier sorts
		// noiseNone first, the best real hit is all[0]; only trim from the tail.
		culled := all[:0]
		for _, sc := range all {
			if sc.hit.Score >= minScore {
				culled = append(culled, sc)
			}
		}
		all = culled
	}

	// #2769 Phase 1C: drop hits whose entity confidence falls below the
	// caller-supplied threshold. Default 0 = no-op.
	if minConfidence > 0 && len(all) > 0 {
		culled := all[:0]
		for _, sc := range all {
			if entityPassesConfidence(sc.hit.Entity, minConfidence) {
				culled = append(culled, sc)
			}
		}
		all = culled
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
	// lowConfidence is set when the only result is the always-1 fallback (no hit
	// cleared min_score). In that case we return the single fallback node WITHOUT
	// BFS expansion and with a hint, instead of expanding a non-match into the
	// whole repo (#5289).
	lowConfidence := false
	if len(all) == 0 {
		lowConfidence = true
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
		if lowConfidence {
			out["low_confidence"] = true
			out["hint"] = noStrongMatchHint
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
	expandTruncated := false

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
	// #5289: never BFS-expand a low-confidence fallback. The always-1 fallback
	// node is a poor textual match injected only so the caller sees *something*;
	// expanding it depth-3 over a densely-connected repo is exactly the
	// pathological whole-repo dump (6915 nodes / 2.6M edges / ~113s) we are
	// fixing. Return just the single fallback node with a hint instead.
	if mode != "none" && !lowConfidence {
		for _, sc := range keep {
			// #5289: stop once the visible set reaches the node budget so a
			// borderline-selective seed (a low-but-above-floor hit that fans out
			// to a hub) can't expand into the whole repo. Both the BFS work and
			// the O(visited * out-degree) edge-carry below are bounded by this.
			if len(visibleNodes) >= findMaxVisibleNodes {
				expandTruncated = true
				break
			}
			adj := sc.repo.getAdjacency() // built lazily, cached until reload (#3367)
			// bfsBounded caps the per-seed visited set; combined with the
			// visibleNodes ceiling this keeps a non-selective query well under a
			// second instead of the ~113s unbounded case.
			vis, vt := bfsBounded(adj, sc.hit.Entity.ID, depth, contextFilter, findMaxBFSNodesPerSeed)
			if vt {
				expandTruncated = true
			}
			for nid, d := range vis {
				if nid == sc.hit.Entity.ID {
					continue
				}
				if len(visibleNodes) >= findMaxVisibleNodes {
					expandTruncated = true
					break
				}
				if e, ok := sc.repo.LabelIndex.ByID[nid]; ok {
					add(sc.repo.Repo, e, sc.hit.Score/float64(d+1))
				}
			}
			// Carry edges between visible nodes. Adjacency-indexed (#2285):
			// iterate out-edges from each visited node instead of scanning the
			// full Doc.Relationships list. Every edge naturally appears exactly
			// once (as an out-edge of its source) so direction semantics match
			// the prior linear scan. Bounded by findMaxVisibleEdges (#5289).
			for nid := range vis {
				if len(visibleEdges) >= findMaxVisibleEdges {
					expandTruncated = true
					break
				}
				from := sc.repo.LabelIndex.ByID[nid]
				if from == nil {
					continue
				}
				if !seen[prefixedID(sc.repo.Repo, nid)] {
					continue
				}
				for _, e := range adj.Outgoing(nid) {
					if len(visibleEdges) >= findMaxVisibleEdges {
						expandTruncated = true
						break
					}
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
	switch {
	case lowConfidence:
		rr.TruncatedNote = noStrongMatchHint
	case expandTruncated:
		rr.TruncatedNote = fmt.Sprintf(
			"expansion capped (>%d nodes / %d edges) — broad query; tighten `query=` or use grafel_neighbors/grafel_topology for full structure",
			findMaxVisibleNodes, findMaxVisibleEdges,
		)
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

// pickFallback reads from the TopKPageRank cache on each LoadedRepo (#2304)
// rather than iterating Doc.Entities on every call. The cache is built LAZILY
// on first ranking use via r.getTopKPageRank() (#3367) — pickFallback is the
// only consumer, so the heavy PageRank sort never runs on the cheap-call path.
// Falls back to a full linear scan when the cache is empty (e.g. a repo whose
// entities carry no PageRank, where buildTopKPageRank returns a nil slice).
func pickFallback(repos []*LoadedRepo) *fallbackPick {
	var best *fallbackPick
	bestPR := -1.0
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Fast path: use pre-sorted cache (built lazily on first ranking use).
		if topK := r.getTopKPageRank(); len(topK) > 0 {
			topID := topK[0]
			e := r.getByID()[topID]
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

// #5289 expansion bounds. A zero-/low-selectivity grafel_find query used to
// leak sub-min_score seeds (displayed score=0.00) into an unbounded depth-3 BFS
// that visited the whole repo (6915 nodes) and emitted a multi-million-edge
// summary, taking ~113s. These caps keep the compact BFS expansion + edge-carry
// bounded so even a borderline-selective query returns in well under a second.
// Selective queries are unaffected: real matches above min_score rarely touch
// these ceilings, and the small-subgraph common case never does.
const (
	// findMaxVisibleNodes caps the total node set rendered in the compact path
	// (seeds + BFS-expanded neighbours across all seeds).
	findMaxVisibleNodes = 400
	// findMaxBFSNodesPerSeed caps the per-seed visited set inside bfsBounded so a
	// single high-degree hub can't fan out to thousands of nodes.
	findMaxBFSNodesPerSeed = 200
	// findMaxVisibleEdges caps the edge-summary computation (O(visited*out-deg)).
	findMaxVisibleEdges = 1500
)

// noStrongMatchHint is surfaced when no entity cleared the min_score floor and
// the result is only the always-1 fallback (#5289). Returned instead of a
// whole-repo dump so the caller knows to retry rather than treating a
// non-selective result as a real subgraph.
const noStrongMatchHint = "no strong matches above min_score — the query terms don't match any entity names; try different/more specific terms, or use grafel_orient / grafel_topology to explore the repo"

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
// callers can chain directly into grafel_docgen/grafel_get_source without
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
			fmt.Fprintln(os.Stderr, "[grafel deprecation] grafel_inspect: param 'label_or_id' is deprecated, use 'entity_id'")
		}
	}
	if key == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	// #2290: inspect envelope no longer embeds graph_meta or cwd_ref_meta.
	// Those session-stable fields are surfaced by grafel_whoami
	// (indexed_ref, indexed_sha, is_worktree, parent_repo, cwd_resolved_to).
	// Inspect dominates cross-stack chains; trimming this per-call saves
	// ~5-7% tokens and compounds in inspect-heavy sessions.
	g, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	verbose := argBool(req, "verbose", false)
	// #2640: include_unresolved=false (default) filters calls[] rows where the
	// target entity could not be resolved (empty target_path or bare repo prefix).
	includeUnresolved := argBool(req, "include_unresolved", false)
	// #4832: opt-in control-flow attribution on outbound CALLS edges
	// (conditional/condition/in_loop). Off by default so the inspect payload is
	// byte-identical (#2828 token-reduction respected).
	includeCallContexts := includeWants(req, "call_contexts")
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
				// #2634: line-precise CALLS edges.
				// #2640: filter unresolved by default.
				out["calls"] = inspectOutboundCalls(r, e, scopeIsOne, includeUnresolved, includeCallContexts)
				// #2641: called_by always present (empty array when no callers).
				out["called_by"] = inspectInboundCalls(r, e, scopeIsOne)
				// #2666: discriminator comparison sites surfaced from DISCRIMINATES_ON
				// edges. Section is omitted entirely when no discriminator edges exist.
				if discs := inspectDiscriminators(r, e, scopeIsOne); len(discs) > 0 {
					out["discriminators"] = discs
				}
				// #3870: dependency-injection edges (INJECTED_INTO / BINDS).
				// Section omitted entirely when the entity has no DI edges.
				if di := inspectDIEdges(r, e, scopeIsOne); len(di) > 0 {
					out["di_edges"] = di
				}
				// #3897: full semantic, non-structural edge set (JOINS_COLLECTION,
				// GRAPH_RELATES, DEPENDS_ON_SERVICE, THROWS/CATCHES, DATA_FLOWS_TO,
				// …). Generalizes the DI-only #3870 projection; omitted when empty.
				if sem := inspectSemanticEdges(r, e, scopeIsOne, isSemanticEdgeKind); len(sem) > 0 {
					out["semantic_edges"] = sem
				}
				// #2642: metadata block with index provenance.
				out["metadata"] = inspectMetadata(r)
				// #3833: surface MRO resolution for inherited members.
				if inh := inspectInheritance(r, e); inh != nil {
					out["inheritance"] = inh
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
	// #2634: line-precise CALLS edges.
	// #2640: filter unresolved by default.
	out["calls"] = inspectOutboundCalls(m.repo, m.ent, scopeIsOne, includeUnresolved, includeCallContexts)
	// #2641: called_by always present (empty array when no callers).
	out["called_by"] = inspectInboundCalls(m.repo, m.ent, scopeIsOne)
	// #2666: discriminator comparison sites surfaced from DISCRIMINATES_ON
	// edges. Section is omitted entirely when no discriminator edges exist.
	if discs := inspectDiscriminators(m.repo, m.ent, scopeIsOne); len(discs) > 0 {
		out["discriminators"] = discs
	}
	// #3870: dependency-injection edges (INJECTED_INTO / BINDS).
	// Section omitted entirely when the entity has no DI edges.
	if di := inspectDIEdges(m.repo, m.ent, scopeIsOne); len(di) > 0 {
		out["di_edges"] = di
	}
	// #3897: full semantic, non-structural edge set (JOINS_COLLECTION,
	// GRAPH_RELATES, DEPENDS_ON_SERVICE, THROWS/CATCHES, DATA_FLOWS_TO, …).
	// Generalizes the DI-only #3870 projection; omitted when empty.
	if sem := inspectSemanticEdges(m.repo, m.ent, scopeIsOne, isSemanticEdgeKind); len(sem) > 0 {
		out["semantic_edges"] = sem
	}
	// #2642: metadata block with index provenance.
	out["metadata"] = inspectMetadata(m.repo)
	// #3833: surface MRO resolution for inherited members.
	if inh := inspectInheritance(m.repo, m.ent); inh != nil {
		out["inheritance"] = inh
	}
	return jsonResult(out), nil
}

// graphMetaForInspect / cwdRefMetaForInspect helpers removed in #2290.
// These embedded session-stable git metadata in every grafel_inspect
// response. They are now sourced from grafel_whoami (indexed_ref,
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
	for _, e := range lr.getAdjacency().Outgoing(entityID) {
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

// ---------------------------------------------------------------------------
// #2634 — line-precise CALLS / called_by edges for grafel_inspect
// ---------------------------------------------------------------------------

// isUnresolvedCallEdge reports whether an outbound CALLS edge should be
// considered unresolved — i.e. the target entity could not be found in the
// graph. An edge is unresolved when:
//   - target_path is empty (no source file resolved), OR
//   - the target ID contains no hex chars after the repo prefix (bare "::")
func isUnresolvedCallEdge(targetPath, targetID string) bool {
	if targetPath == "" {
		return true
	}
	// Check for bare repo prefix pattern: "repo::" with nothing after.
	if idx := strings.Index(targetID, "::"); idx >= 0 {
		suffix := targetID[idx+2:]
		if suffix == "" {
			return true
		}
		// Entity IDs contain hex chars; if suffix has none, it's unresolved.
		hasHex := false
		for _, c := range suffix {
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
				hasHex = true
				break
			}
		}
		if !hasHex {
			return true
		}
	}
	return false
}

// inspectOutboundCalls returns outbound CALLS edges from entity e with line
// numbers read from the edge Properties["line"] set by extractors.
// Each entry: {target, target_path, line, via}.
//
// When includeUnresolved is false (the default), edges where the target entity
// could not be resolved are omitted. When true, all edges are returned and
// unresolved ones carry an additional "unresolved: true" field. (#2640)
//
// When includeCallContexts is true (opt-in via include=call_contexts, #4832),
// each resolved-line call entry additionally carries the control-flow context
// of its call site — conditional/condition/in_loop — computed from the caller's
// source window via the same enclosing-block classifier part (a) used for
// effect_contexts. Default off so the inspect payload is byte-identical (#2828).
func inspectOutboundCalls(lr *LoadedRepo, e *graph.Entity, scopeIsOne bool, includeUnresolved, includeCallContexts bool) []map[string]any {
	if lr == nil || lr.Doc == nil {
		return []map[string]any{}
	}
	out := []map[string]any{}
	rels := lr.Doc.Relationships
	byID := lr.getByID()
	for _, ed := range lr.getAdjacency().Outgoing(e.ID) {
		if !strings.EqualFold(ed.kind, "CALLS") {
			continue
		}
		targetID := ed.target
		if !scopeIsOne {
			targetID = prefixedID(lr.Repo, ed.target)
		}
		entry := map[string]any{
			"target":      targetID,
			"target_path": "",
		}
		// Resolve target path from entity index.
		if tgt := byID[ed.target]; tgt != nil {
			entry["target_path"] = tgt.SourceFile
			entry["target"] = tgt.Name
			if !scopeIsOne {
				entry["target"] = prefixedID(lr.Repo, ed.target)
			}
		}
		// #2640: check whether this edge is unresolved.
		targetPath, _ := entry["target_path"].(string)
		targetVal, _ := entry["target"].(string)
		unresolved := isUnresolvedCallEdge(targetPath, targetVal)
		if unresolved && !includeUnresolved {
			continue
		}
		// Line number from relationship properties.
		lineNum := 0
		if ed.relIdx >= 0 && ed.relIdx < len(rels) {
			if v := rels[ed.relIdx].Properties["line"]; v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					lineNum = n
				}
			}
			if v := rels[ed.relIdx].Properties["via"]; v != "" {
				entry["via"] = v
			}
		}
		entry["line"] = lineNum
		if unresolved {
			entry["unresolved"] = true
		}
		out = append(out, entry)
	}
	// #4832 — opt-in control-flow attribution of each call site. Reuses the same
	// enclosing-block classifier part (a) applied to effects: read the CALLER's
	// source window once, classify every call-site line against its enclosing
	// conditional/loop blocks, and stamp conditional/condition/in_loop on each
	// entry. Unconditional (top-level) calls carry conditional=false with no
	// condition, matching how part (a) represented unconditional effects.
	if includeCallContexts {
		attachCallContexts(out, lr, e)
	}
	return out
}

// attachCallContexts stamps conditional/condition/in_loop on each outbound-call
// entry that has a positive "line", using substrate.CallContextsFor against the
// caller entity e's source window. No-op (entries unchanged) when the source is
// unreadable or the language has no block detector — honest-partial, so callers
// never see a false "unconditional" for an unsupported language. (#4832)
func attachCallContexts(entries []map[string]any, lr *LoadedRepo, e *graph.Entity) {
	if len(entries) == 0 || lr == nil || e == nil {
		return
	}
	lang := substrate.LanguageForPath(e.SourceFile)
	start, end := branchSourceSpan(e)
	if start <= 0 {
		return
	}
	abs := e.SourceFile
	if !filepath.IsAbs(abs) && lr.Path != "" {
		abs = filepath.Join(lr.Path, e.SourceFile)
	}
	src, err := readRawSourceWindow(abs, start, end)
	if err != nil || src == "" {
		return
	}
	callLines := make([]int, 0, len(entries))
	for _, entry := range entries {
		if ln, ok := entry["line"].(int); ok && ln > 0 {
			callLines = append(callLines, ln)
		}
	}
	ctxs := substrate.CallContextsFor(lang, src, start, callLines)
	for _, entry := range entries {
		ln, ok := entry["line"].(int)
		if !ok || ln <= 0 {
			continue
		}
		if cc, found := ctxs[ln]; found {
			entry["conditional"] = true
			if cc.Condition != "" {
				entry["condition"] = cc.Condition
			}
			if cc.InLoop {
				entry["in_loop"] = true
			}
		} else {
			entry["conditional"] = false
		}
	}
}

// inspectInboundCalls returns inbound CALLS edges (callers of entity e) with
// line numbers and a short context snippet from the caller's source.
// Each entry: {source, source_path, line, context}.
//
// Always returns a non-nil slice (empty when no callers) so called_by is
// always present in the response (#2641).
func inspectInboundCalls(lr *LoadedRepo, e *graph.Entity, scopeIsOne bool) []map[string]any {
	if lr == nil || lr.Doc == nil {
		return []map[string]any{}
	}
	// lineCache maps absolute source path → slice of trimmed line strings (0-indexed).
	lineCache := map[string][]string{}

	out := []map[string]any{}
	rels := lr.Doc.Relationships
	byID := lr.getByID()
	for _, ed := range lr.getAdjacency().Incoming(e.ID) {
		if !strings.EqualFold(ed.kind, "CALLS") {
			continue
		}
		callerID := ed.target // Incoming: ed.target is the FromID (the caller)
		callerEntity := byID[callerID]

		sourceID := callerID
		sourcePath := ""
		if callerEntity != nil {
			sourcePath = callerEntity.SourceFile
			sourceID = callerEntity.Name
		}
		if !scopeIsOne {
			sourceID = prefixedID(lr.Repo, callerID)
		}

		entry := map[string]any{
			"source":      sourceID,
			"source_path": sourcePath,
		}

		lineNum := 0
		if ed.relIdx >= 0 && ed.relIdx < len(rels) {
			if v := rels[ed.relIdx].Properties["line"]; v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					lineNum = n
				}
			}
		}
		entry["line"] = lineNum

		// Context snippet: read line from disk lazily if we have a valid line
		// and a resolvable source path.
		ctx := ""
		if lineNum > 0 && sourcePath != "" && lr.Path != "" {
			abs := sourcePath
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(lr.Path, sourcePath)
			}
			lines, cached := lineCache[abs]
			if !cached {
				lines = readSourceLines(abs)
				lineCache[abs] = lines
			}
			if lineNum-1 < len(lines) {
				raw := strings.TrimSpace(lines[lineNum-1])
				if len(raw) > 40 {
					raw = raw[:40]
				}
				ctx = raw
			}
		}
		entry["context"] = ctx
		out = append(out, entry)
	}
	return out
}

// inspectDiscriminators returns rows for every DISCRIMINATES_ON edge attached
// to entity e (#2666). Each row carries:
//
//	file:line   — the entity's source file + the edge Properties["line"]
//	literal     — Properties["literal"]: the RHS literal value (e.g. "2", "periodic")
//	other_side  — the synthetic var stub on the other side of the edge
//	              (ToID for outgoing edges, FromID for incoming)
//
// Returns nil when the entity has no DISCRIMINATES_ON edges so the inspect
// envelope can omit the section entirely.
func inspectDiscriminators(lr *LoadedRepo, e *graph.Entity, scopeIsOne bool) []map[string]any {
	if lr == nil || lr.Doc == nil || e == nil {
		return nil
	}
	out := []map[string]any{}
	rels := lr.Doc.Relationships
	emit := func(ed edge, otherIsTarget bool) {
		if ed.relIdx < 0 || ed.relIdx >= len(rels) {
			return
		}
		r := &rels[ed.relIdx]
		line := 0
		if v := r.Properties["line"]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				line = n
			}
		}
		literal := r.Properties["literal"]
		other := ed.target
		if !scopeIsOne {
			// Synthetic "var:<name>" stubs are not prefixed (they are not
			// per-repo entities); leave them as-is for cross-repo scope so
			// agents can correlate hits across repos.
			if !strings.HasPrefix(other, "var:") {
				other = prefixedID(lr.Repo, other)
			}
		}
		fileLine := ""
		if e.SourceFile != "" && line > 0 {
			fileLine = fmt.Sprintf("%s:%d", e.SourceFile, line)
		} else if e.SourceFile != "" {
			fileLine = e.SourceFile
		}
		entry := map[string]any{
			"file_line":  fileLine,
			"line":       line,
			"literal":    literal,
			"other_side": other,
		}
		_ = otherIsTarget // direction encoded by which adjacency list we walked
		out = append(out, entry)
	}
	adj := lr.getAdjacency()
	for _, ed := range adj.Outgoing(e.ID) {
		if !strings.EqualFold(ed.kind, "DISCRIMINATES_ON") {
			continue
		}
		emit(ed, true)
	}
	for _, ed := range adj.Incoming(e.ID) {
		if !strings.EqualFold(ed.kind, "DISCRIMINATES_ON") {
			continue
		}
		emit(ed, false)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// semanticEdgeKinds is the configurable set of NON-STRUCTURAL, semantically
// meaningful relationship kinds that the MCP read tools project in inspect's
// `semantic_edges` section and in expand's per-neighbour annotation. (#3897,
// generalizing the DI-only #3870/#3894 projection.)
//
// These edges are emitted by the per-language extractors and wired into the
// graph, but before #3897 no MCP read tool surfaced them — inspect projected
// only CALLS (calls/called_by), DISCRIMINATES_ON (discriminators) and the DI
// subset (di_edges); expand annotated only CALLS depth + the DI subset. A node
// could carry a JOINS_COLLECTION / GRAPH_RELATES / DEPENDS_ON_SERVICE / error-
// flow edge and the rewrite agent would never see it.
//
// Deliberately EXCLUDED (handled elsewhere or pure scaffolding):
//   - CALLS              — has dedicated calls/called_by sections + expand depth.
//   - DISCRIMINATES_ON   — has the dedicated `discriminators` section (#2666).
//   - IMPORTS / CONTAINS — structural scaffolding, high-volume + low-signal.
//
// The map keys are stored UPPER-cased; membership is matched case-insensitively
// against the on-graph relationship casing (buildAdjacency copies r.Kind
// verbatim). The DI kinds (INJECTED_INTO / BINDS) are members so the generalized
// projection is a strict superset of the #3870 behaviour.
var semanticEdgeKinds = map[string]struct{}{
	string(types.RelationshipKindJoinsCollection):  {},
	string(types.RelationshipKindGraphRelates):     {},
	string(types.RelationshipKindDependsOnService): {},
	string(types.RelationshipKindThrows):           {},
	string(types.RelationshipKindCatches):          {},
	string(types.RelationshipKindModifiesTable):    {},
	string(types.RelationshipKindAccessesTable):    {},
	string(types.RelationshipKindQueries):          {},
	string(types.RelationshipKindRenders):          {},
	string(types.RelationshipKindUsesTranslation):  {},
	string(types.RelationshipKindTriggers):         {},
	string(types.RelationshipKindEnqueues):         {},
	string(types.RelationshipKindPublishesTo):      {},
	string(types.RelationshipKindSubscribesTo):     {},
	string(types.RelationshipKindCaches):           {},
	string(types.RelationshipKindInvalidates):      {},
	string(types.RelationshipKindGatedBy):          {},
	string(types.RelationshipKindHandlesCommand):   {},
	string(types.RelationshipKindDataFlowsTo):      {},
	string(types.RelationshipKindInjectedInto):     {},
	string(types.RelationshipKindBinds):            {},
	string(types.RelationshipKindDependsOnConfig):  {},
	// #4307 (Layer 1 of epic #4294): the documentation-link edge emitted by the
	// markdown ingest pass (SCOPE.Section --MENTIONS--> code entity). It is a
	// genuine semantic relation ("this code is documented in that section"), not
	// structural scaffolding like CONTAINS/IMPORTS, so it belongs in the
	// projected set: inspect lists it under semantic_edges, neighbors annotates
	// each row with semantic_kind=MENTIONS, and the inbound walk
	// (find_callers / neighbors direction=in) surfaces the documenting Section as
	// a predecessor of the code entity (via isInboundNeighborKind, which accepts
	// any isSemanticEdgeKind). The outbound walk (Section → code) already
	// traversed every out-edge, so this makes the edge_kind label symmetric.
	string(types.RelationshipKindMentions): {},
}

// isSemanticEdgeKind reports whether k is one of the projected semantic edge
// kinds (case-insensitive against the on-graph casing). (#3897)
func isSemanticEdgeKind(k string) bool {
	_, ok := semanticEdgeKinds[strings.ToUpper(k)]
	return ok
}

// diEdgeKinds is the dependency-injection SUBSET of semanticEdgeKinds, retained
// so the backward-compatible `di_edges` section / di_kind|di_direction
// annotation can be derived without re-listing the kinds. (#3870, #3897)
func isDIEdgeKind(k string) bool {
	return strings.EqualFold(k, string(types.RelationshipKindInjectedInto)) ||
		strings.EqualFold(k, string(types.RelationshipKindBinds))
}

// inspectSemanticEdges returns rows for every projected semantic edge attached
// to entity e (see semanticEdgeKinds). Generalizes inspectDIEdges (#3870) to
// cover the full meaningful, non-structural edge set (#3897).
//
// Each row carries:
//
//	kind      — the on-graph relationship kind verbatim (e.g. "JOINS_COLLECTION")
//	direction — "outbound" (this entity is the FromID) or "inbound" (ToID)
//	other     — the entity on the other side (ToID for outbound, FromID inbound),
//	            repo-prefixed when scopeIsOne is false
//	line      — Properties["line"] when the extractor recorded one (0 otherwise)
//
// Returns nil when the entity has no semantic edges so the inspect envelope can
// omit the section entirely (mirrors inspectDiscriminators, #2666). The `accept`
// predicate selects which kinds to emit — callers pass isSemanticEdgeKind for
// the full `semantic_edges` set or isDIEdgeKind for the legacy `di_edges` subset.
func inspectSemanticEdges(lr *LoadedRepo, e *graph.Entity, scopeIsOne bool, accept func(string) bool) []map[string]any {
	if lr == nil || lr.Doc == nil || e == nil {
		return nil
	}
	out := []map[string]any{}
	rels := lr.Doc.Relationships
	emit := func(ed edge, direction string) {
		other := ed.target
		if !scopeIsOne {
			other = prefixedID(lr.Repo, other)
		}
		// ed.kind preserves the on-graph relationship casing (buildAdjacency
		// copies r.Kind verbatim), so consumers can match the constant directly.
		entry := map[string]any{
			"kind":      ed.kind,
			"direction": direction,
			"other":     other,
			"line":      0,
		}
		if ed.relIdx >= 0 && ed.relIdx < len(rels) {
			if v := rels[ed.relIdx].Properties["line"]; v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					entry["line"] = n
				}
			}
		}
		out = append(out, entry)
	}
	adj := lr.getAdjacency()
	for _, ed := range adj.Outgoing(e.ID) {
		if accept(ed.kind) {
			emit(ed, "outbound")
		}
	}
	for _, ed := range adj.Incoming(e.ID) {
		if accept(ed.kind) {
			emit(ed, "inbound")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// inspectDIEdges is the backward-compatible DI-only projection (#3870), now a
// thin wrapper over the generalized inspectSemanticEdges (#3897). Returns rows
// for INJECTED_INTO / BINDS edges only, preserving the legacy `di_edges` shape.
func inspectDIEdges(lr *LoadedRepo, e *graph.Entity, scopeIsOne bool) []map[string]any {
	return inspectSemanticEdges(lr, e, scopeIsOne, isDIEdgeKind)
}

// semanticNeighbor describes the semantic edge connecting a start node to one of
// its direct neighbours (used to annotate grafel_expand rows). (#3897)
type semanticNeighbor struct {
	kind      string // on-graph relationship casing (e.g. "JOINS_COLLECTION")
	direction string // "outbound" (start is FromID) or "inbound" (start is ToID)
}

// diNeighbor is the DI-specific alias retained for the legacy di_kind/
// di_direction annotation. (#3870)
type diNeighbor = semanticNeighbor

// directSemanticEdges indexes the projected semantic edges directly incident on
// startID, keyed by the neighbour entity id. Outbound edges take precedence over
// inbound when an id appears on both sides. Returns an empty (non-nil) map when
// there are no matching edges. The `accept` predicate selects the kind subset
// (isSemanticEdgeKind for the full set, isDIEdgeKind for the DI subset). (#3897)
func directSemanticEdges(adj *adjacency, startID string, accept func(string) bool) map[string]semanticNeighbor {
	res := map[string]semanticNeighbor{}
	if adj == nil {
		return res
	}
	for _, ed := range adj.Incoming(startID) {
		if accept(ed.kind) {
			res[ed.target] = semanticNeighbor{kind: ed.kind, direction: "inbound"}
		}
	}
	for _, ed := range adj.Outgoing(startID) {
		if accept(ed.kind) {
			res[ed.target] = semanticNeighbor{kind: ed.kind, direction: "outbound"}
		}
	}
	return res
}

// directDIEdges is the backward-compatible DI-only neighbour index (#3870), now
// delegating to directSemanticEdges with the DI subset predicate. (#3897)
func directDIEdges(adj *adjacency, startID string) map[string]diNeighbor {
	return directSemanticEdges(adj, startID, isDIEdgeKind)
}

// inspectMetadata returns index-staleness fields for the inspect response.
// Surfaces indexed_at and age_seconds so agents know whether the line-number
// data might be stale. (#2642)
//
// #2780: indexed_ref and indexed_sha are deliberately NOT included here. Those
// session-stable provenance fields are the exclusive domain of grafel_whoami
// (per the #2290/#2335 cleanup). Re-embedding them in inspect — a per-call,
// inspect-heavy handler — both wastes tokens and violates the single-source
// contract enforced by TestNoSessionMetaInNonWhoamiHandlers. The staleness
// signal (indexed_at/age_seconds) is what #2642 actually needed and is retained.
func inspectMetadata(lr *LoadedRepo) map[string]any {
	meta := map[string]any{
		"indexed_at":  "",
		"age_seconds": 0,
	}
	if lr == nil || lr.Doc == nil {
		return meta
	}
	if !lr.Doc.GeneratedAt.IsZero() {
		indexedAt := lr.Doc.GeneratedAt.UTC()
		meta["indexed_at"] = indexedAt.Format(time.RFC3339)
		meta["age_seconds"] = int(time.Since(indexedAt).Seconds())
	}
	return meta
}

// readSourceLines opens path and returns all lines as a slice (0-indexed).
// Returns nil on any error — callers treat nil as "no lines available".
func readSourceLines(path string) []string {
	f, err := openSourceFile(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
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
			fmt.Fprintln(os.Stderr, "[grafel deprecation] grafel_expand: param 'node' is deprecated, use 'entity_id'")
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
	adj := startRepo.getAdjacency() // built lazily, cached until reload (#3367)
	vis := bfs(adj, start.ID, depth, nil)
	// #3870: dependency-injection edges (INJECTED_INTO / BINDS) are emitted by
	// the per-language DI extractors and live in the graph, but neighbors
	// previously dropped the connecting edge kind entirely — so a consumer could
	// not tell a provider→consumer injection from a plain CALLS neighbour. Index
	// the start node's direct DI edges by neighbour id so the matching output
	// rows can be annotated with {di_kind, di_direction} below.
	diByNeighbor := directDIEdges(adj, start.ID)
	// #3897: same idea, generalized to the full semantic edge set. Annotates the
	// neighbour row with {semantic_kind, semantic_direction} for ANY projected
	// non-structural edge (JOINS_COLLECTION, GRAPH_RELATES, DEPENDS_ON_SERVICE,
	// THROWS/CATCHES, DATA_FLOWS_TO, …) — not just DI. di_kind/di_direction are
	// retained above for backward compatibility on the DI subset.
	semByNeighbor := directSemanticEdges(adj, start.ID, isSemanticEdgeKind)
	out := []map[string]any{}
	for nid, d := range vis {
		if nid == start.ID {
			continue
		}
		e := startRepo.LabelIndex.ByID[nid]
		if e == nil {
			continue
		}
		row := map[string]any{
			"id":          prefixedID(startRepo.Repo, e.ID),
			"label":       e.Name,
			"depth":       d,
			"source_file": e.SourceFile,
			"start_line":  e.StartLine,
		}
		if di, ok := diByNeighbor[nid]; ok {
			row["di_kind"] = di.kind
			row["di_direction"] = di.direction
		}
		if sem, ok := semByNeighbor[nid]; ok {
			row["semantic_kind"] = sem.kind
			row["semantic_direction"] = sem.direction
		}
		out = append(out, row)
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
				// #3628 — extraction-confidence honesty marker so a consumer can
				// tell a fully-resolved cross-repo edge from a heuristic /
				// inferred one. Absent on-disk ⇒ "resolved" (see EdgeConfidence).
				"confidence": l.EdgeConfidence(),
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
			a := r.getAdjacency()
			for _, e := range a.out[local] {
				out = append(out, edge{
					target: prefixedID(repo, e.target),
					kind:   e.kind,
					weight: -math.Log(0.95), // intra-repo confidence ~0.95
					relIdx: -1,              // synthetic — no backing Relationship (#2305)
				})
			}
			// #3834 (MRO T4): splice the inherited-member INHERITS hop so a
			// shortest path that arrives at a bodyless inherited stub can reach
			// the DEFINING base/mixin member (in-repo body, or the external pack
			// contract leaf) instead of dead-ending. The defining member is a
			// normal node whose own out-edges expand() picks up on the next pop.
			for _, me := range mroOutboundEdges(r, local) {
				out = append(out, edge{
					target: prefixedID(repo, me.Target),
					kind:   me.Kind,
					weight: -math.Log(0.95),
					relIdx: -1, // synthetic — no backing Relationship (#2305)
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
	// #3834 (MRO T4): a synthetic external-contract id ("inherits-ext:<FQN>")
	// is not a stored entity, so it resolves to no LabelIndex hit above. It is
	// nonetheless a legitimate trace target — the resolution endpoint for an
	// inherited member whose body lives in an external library. Bind it to the
	// repo whose inherited stub projects it (the only repo that can reach it).
	if isExternalContractID(s) {
		for _, r := range lg.Repos {
			if r.Doc == nil {
				continue
			}
			for i := range r.Doc.Entities {
				for _, me := range mroOutboundEdges(r, r.Doc.Entities[i].ID) {
					if me.External && me.Target == s {
						return prefixedID(r.Repo, s)
					}
				}
			}
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
// orient (#4290) — graph-orientation analysis
// ---------------------------------------------------------------------------

// handleOrient surfaces a "where do I start reading this codebase?" view built
// from Pass-4 attributes already on the graph (betweenness Centrality,
// PageRank, CommunityID) plus cheap inline degree/boundary computation. Three
// parts: key entities (structural hubs/bridges), cross-cutting edges (boundary
// crossers), and templated orientation questions. See
// internal/graph/orientation.go for the analysis.
//
// Per-repo: each repo in scope gets its own analysis block (communities and
// centrality are repo-local). Optional caps: top_entities, top_edges,
// max_questions (all default to the production values in
// graph.DefaultOrientationOptions).
func (s *Server) handleOrient(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	def := graph.DefaultOrientationOptions()
	opts := graph.OrientationOptions{
		TopEntities:  argInt(req, "top_entities", def.TopEntities),
		TopEdges:     argInt(req, "top_edges", def.TopEdges),
		MaxQuestions: argInt(req, "max_questions", def.MaxQuestions),
	}
	out := []map[string]any{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		res := graph.AnalyzeOrientation(r.Doc.Entities, r.Doc.Relationships, opts)
		out = append(out, map[string]any{
			"repo":                  r.Repo,
			"key_entities":          res.KeyEntities,
			"cross_cutting_edges":   res.CrossCutEdges,
			"orientation_questions": res.Questions,
		})
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
	// type filter (#2810): isolate a single finding kind, e.g.
	// "security_finding" so the security-audit skill can query just the
	// promoted SecurityFinding records without re-scanning notes.
	if typ := argString(req, "type", ""); typ != "" {
		all = findingsOfType(all, typ)
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
			fmt.Fprintln(os.Stderr, "[grafel deprecation] grafel_get_source: param 'node_id' is deprecated, use 'entity_id'")
		}
	}
	if nodeID == "" {
		return mcpapi.NewToolResultError("missing required argument: entity_id"), nil
	}
	// #2828: default context_lines lowered 20→8. Live telemetry showed
	// get_source at ~1,035 tok/call; 40 lines of surrounding padding per call
	// dominated the cost while the entity's own span is what callers read.
	// Callers that need more surrounding context pass context_lines explicitly.
	contextLines := argInt(req, "context_lines", 8)
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	// #4272 / #1650 — hardened entity resolution. resolveSourceEntity accepts an
	// id, qualified_name, or label, each optionally "<repo>::"-prefixed, and
	// tries id → qname → label → qname-suffix across the group for both the raw
	// arg and its prefix-stripped local form (the prefix gap that erroneously
	// returned "node not found" — the same class fixed for effective_contract in
	// #4243). A label matching >1 distinct entity yields a clarifier list; a
	// genuine miss yields a clearer error (attempted forms + did-you-mean) so the
	// caller can self-correct rather than re-erroring.
	resn := resolveSourceEntity(lg, nodeID)
	if resn.notFound != "" {
		return mcpapi.NewToolResultError(resn.notFound), nil
	}
	if len(resn.ambiguous) > 0 {
		return jsonResult(ambiguousMatches(resn.ambiguous, nodeID)), nil
	}
	e := resn.entity
	lr := resn.repo

	// #3833 — MRO-aware resolution. When the entity is an INHERITED member (a
	// bodyless DRF synthetic, or a method resolved via EXTENDS to a base that
	// defines it) resolve it to the DEFINING class body instead of returning the
	// subclass file:
	//   - external library base (DRF mixin) -> synthesize the pack contract as
	//     an explicit "external body" stub (no in-repo source exists);
	//   - in-repo base -> redirect get_source to the base's real body span.
	// Honest-partial: an unresolved inherited member falls through to the
	// current behaviour (the subclass entity's own span).
	if res := resolveMember(lr, e); res.IsInherited() {
		switch res.Provenance {
		case provInheritedExternal:
			return mcpapi.NewToolResultText(synthesizeExternalBody(res)), nil
		case provInheritedInRepo:
			if res.DefiningEntity != nil {
				// Read the base's real body; prepend an explicit provenance
				// header so the caller knows it is the inherited definition.
				header := fmt.Sprintf(
					"# grafel: %s.%s is INHERITED — body defined by %s (resolved via EXTENDS)\n",
					res.OwningClass, res.Member, res.DefiningClass,
				)
				e = res.DefiningEntity
				abs := e.SourceFile
				if !filepath.IsAbs(abs) && lr.Path != "" {
					abs = filepath.Join(lr.Path, e.SourceFile)
				}
				body, rerr := readInheritedBody(ctx, abs, e, contextLines)
				if rerr != nil {
					return mcpapi.NewToolResultError(rerr.Error()), nil
				}
				return mcpapi.NewToolResultText(header + body), nil
			}
		}
	}

	abs := e.SourceFile
	if !filepath.IsAbs(abs) && lr.Path != "" {
		abs = filepath.Join(lr.Path, e.SourceFile)
	}

	// #2828 / #1614 — bound the requested span and SIGNAL any truncation.
	// computeSourceSpan clamps a degenerate span (synthetic/shadow/route
	// entities frequently carry end<=start or a 0 bound, which previously
	// emitted the whole file), applies the caller's opt-in start_line/end_line
	// range and max_lines head cap, and enforces the absolute hard-max ceiling
	// so a single call can never be a whole-file dump. The returned sourceSpan
	// records whether the emitted window is a strict prefix of the available
	// span so we can append a precise "request lines X-Y" continuation hint
	// instead of the pre-#2828 SILENT 200-line clamp ([no-silent-caps]).
	sp := computeSourceSpan(e, readSourceWindowOpts(req, contextLines))
	start, end := sp.start, sp.end

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
		// #2828: append the continuation hint only when a window was actually
		// clamped (truncationMarker returns "" otherwise, so the common-case
		// payload is unchanged).
		return mcpapi.NewToolResultText(out.text + sp.truncationMarker(prefixedID(lr.Repo, e.ID))), nil
	case <-readCtx.Done():
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"get_source: read timed out after 5s on %s (file may be on a stalled filesystem or watched-path kernel stall); node_id=%s",
			abs, nodeID,
		)), nil
	}
}

// readInheritedBody computes the windowed source span for entity e (reusing
// the same degenerate-span clamp + hard cap + 5s read-timeout discipline as the
// main get_source path) and returns the body text. Used by the #3833 MRO
// redirect to read an in-repo base class's defining body.
func readInheritedBody(ctx context.Context, abs string, e *graph.Entity, contextLines int) (string, error) {
	// #2828: reuse the shared span policy (degenerate-span clamp + hard ceiling)
	// for the inherited-body redirect. The inherited path does not accept the
	// per-call start_line/end_line/max_lines opt-ins (the caller targeted the
	// subclass member, not the resolved base span), so only context_lines is
	// passed.
	sp := computeSourceSpan(e, sourceWindowOpts{contextLines: contextLines})
	start, end := sp.start, sp.end

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
		return out.text, out.err
	case <-readCtx.Done():
		return "", fmt.Errorf("get_source: read timed out after 5s on %s (inherited body)", abs)
	}
}

// readSourceWindow is implemented in read_source_unix.go (darwin || linux) and
// read_source_other.go (!darwin && !linux, i.e. windows + BSDs). The Unix
// implementation uses a non-blocking open(2) to avoid macOS fsevents kernel
// stalls (#1773); the other path falls back to plain os.Open (no fsevents
// equivalent).

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
	linkCount := len(lg.Links)
	if !all {
		linkCount = 0
		for _, l := range lg.Links {
			sr, _ := splitPrefixed(l.Source)
			tr, _ := splitPrefixed(l.Target)
			if wanted[sr] || wanted[tr] {
				linkCount++
			}
		}
	}
	totals["cross_repo_links"] = linkCount
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

	// #2669: surface the HTTP-pass resolve-strategy telemetry persisted to
	// <group>-link-pass-stats.json next to <group>-links.json. Missing file
	// is silently elided (group has not been indexed since the counters
	// shipped, or the link pass hasn't run yet).
	if lg.LinksFile != "" {
		statsPath := linkPassStatsPathForLinksFile(lg.LinksFile)
		if doc, err := links.ReadLinkPassStats(statsPath); err == nil && doc != nil && doc.HTTPSummary != nil {
			http := map[string]any{
				"cross_repo_resolve_attempts": doc.HTTPSummary.Attempts,
			}
			if len(doc.HTTPSummary.HitsByStrategy) > 0 {
				http["cross_repo_resolve_hits_by_strategy"] = doc.HTTPSummary.HitsByStrategy
			}
			if len(doc.HTTPSummary.MissesByReason) > 0 {
				http["cross_repo_resolve_misses_by_reason"] = doc.HTTPSummary.MissesByReason
			}
			totals["http_resolve"] = http
		}
	}

	// P5 (dogfooding report): surface live reindex state so a coordinator can
	// query grafel_stats instead of polling `ps aux` for hot grafel processes.
	// Sourced from the process-global indexstate record the daemon's scheduler
	// updates on every in-flight transition. When the MCP server runs outside a
	// daemon (e.g. `grafel mcp serve` stdio with no scheduler) the count is 0,
	// so is_indexing is reported false — accurate for that mode.
	ix := indexstate.Get()
	totals["is_indexing"] = ix.IsIndexing
	if ix.IsIndexing {
		totals["indexing_in_flight"] = ix.InFlight
		if !ix.StartedAt.IsZero() {
			totals["indexing_started_at"] = ix.StartedAt.UTC().Format(time.RFC3339)
		}
	}

	return jsonResult(totals), nil
}

// linkPassStatsPathForLinksFile derives the link-pass-stats sidecar path
// from the canonical links.json path. The convention is
// `<group>-link-pass-stats.json` next to `<group>-links.json`, matching
// PathsFor in internal/links. (#2669)
func linkPassStatsPathForLinksFile(linksFile string) string {
	const linksSuffix = "-links.json"
	base := filepath.Base(linksFile)
	if !strings.HasSuffix(base, linksSuffix) {
		return ""
	}
	group := strings.TrimSuffix(base, linksSuffix)
	return filepath.Join(filepath.Dir(linksFile), group+"-link-pass-stats.json")
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

// handleEnrichments dispatches grafel_enrichments based on action=list|submit|reject.
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

// handleCrossLinks dispatches grafel_cross_links based on action=list|accept|reject.
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

// handleRepairs dispatches grafel_repairs based on action=list|submit.
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
// grafel_repairs tool where the edge identifier comes in as residual_id.
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
