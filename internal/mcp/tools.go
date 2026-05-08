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
	"strings"
	"time"

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
func jsonResult(v any) *mcpapi.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpapi.NewToolResultError("marshal: " + err.Error())
	}
	return mcpapi.NewToolResultText(string(data))
}

// ---------------------------------------------------------------------------
// whoami
// ---------------------------------------------------------------------------

func (s *Server) handleWhoami(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	cwd := s.inferCWD(req)
	explicit := argString(req, "group", "")
	group, source, err := resolveGroup(s.State, explicit, cwd)
	if err != nil {
		return jsonResult(map[string]any{
			"group":         "",
			"repo":          "",
			"source":        "none",
			"registry_path": s.State.registry.Path,
			"error":         err.Error(),
		}), nil
	}
	repo := repoFromCWD(cwd)
	return jsonResult(map[string]any{
		"group":         group,
		"repo":          repo,
		"source":        source,
		"registry_path": s.State.registry.Path,
	}), nil
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func (s *Server) handleQueryGraph(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	question, err := req.RequireString("question")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 3)
	tokenBudget := argInt(req, "token_budget", 800)
	full := argBool(req, "full", false)
	repoFilter := argStringSlice(req, "repo_filter")
	contextFilter := contextFilterSet(argStringSlice(req, "context_filter"))
	mode := argString(req, "mode", "bfs")

	repos := reposToConsider(lg, repoFilter)
	if len(repos) == 0 {
		return mcpapi.NewToolResultText("# no repos loaded for this group\n"), nil
	}

	// Score across all repos in scope.
	all := []scored{}
	for _, r := range repos {
		hits := r.BM25.Search(question, 50)
		for _, h := range hits {
			all = append(all, scored{repo: r, hit: h})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].hit.Score > all[j].hit.Score })

	// "always-1" rule: if nothing matched but repos contain entities, return
	// the highest-PageRank entity as a single-result fallback so callers see
	// something rather than empty.
	if len(all) == 0 {
		fallback := pickFallback(repos)
		if fallback != nil {
			all = append(all, scored{repo: fallback.repo, hit: Hit{Entity: fallback.entity, Score: 0.0001}})
		}
	}

	if full {
		return jsonResult(map[string]any{
			"matches": serializeHits(all),
		}), nil
	}

	// Smart scoping: no filter, multiple repos -> per-repo top 3.
	if len(repoFilter) == 0 && len(lg.Repos) > 1 {
		return mcpapi.NewToolResultText(renderPerRepoSummary(all, lg)), nil
	}

	// Otherwise BFS-expand from each top hit and render compact.
	matched := len(all)
	keep := all
	if len(keep) > 25 {
		keep = keep[:25]
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
			adj := buildAdjacency(sc.repo.Doc, sc.repo.Repo)
			vis := bfs(adj, sc.hit.Entity.ID, depth, contextFilter)
			for nid, d := range vis {
				if nid == sc.hit.Entity.ID {
					continue
				}
				if e, ok := sc.repo.LabelIndex.ByID[nid]; ok {
					add(sc.repo.Repo, e, sc.hit.Score/float64(d+1))
				}
			}
			// Carry edges between visible nodes.
			for _, rel := range sc.repo.Doc.Relationships {
				if !seen[prefixedID(sc.repo.Repo, rel.FromID)] || !seen[prefixedID(sc.repo.Repo, rel.ToID)] {
					continue
				}
				from := sc.repo.LabelIndex.ByID[rel.FromID]
				to := sc.repo.LabelIndex.ByID[rel.ToID]
				if from == nil || to == nil {
					continue
				}
				visibleEdges = append(visibleEdges, renderEdge{From: from.Name, To: to.Name, Kind: rel.Kind})
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
	return mcpapi.NewToolResultText(renderCompact(rr, tokenBudget)), nil
}

// pickFallback returns the highest-pagerank entity across repos.
type fallbackPick struct {
	repo   *LoadedRepo
	entity *graph.Entity
}

func pickFallback(repos []*LoadedRepo) *fallbackPick {
	var best *fallbackPick
	bestPR := -1.0
	for _, r := range repos {
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
func serializeHits(all []scored) []map[string]any {
	out := make([]map[string]any, 0, len(all))
	for _, sc := range all {
		out = append(out, map[string]any{
			"id":          prefixedID(sc.repo.Repo, sc.hit.Entity.ID),
			"label":       sc.hit.Entity.Name,
			"repo":        sc.repo.Repo,
			"score":       sc.hit.Score,
			"source_file": sc.hit.Entity.SourceFile,
			"start_line":  sc.hit.Entity.StartLine,
			"kind":        stripScopePrefix(sc.hit.Entity.Kind),
		})
	}
	return out
}

// renderPerRepoSummary is the smart-scoping default for unfiltered, multi-repo queries.
func renderPerRepoSummary(all []scored, lg *LoadedGroup) string {
	perRepo := map[string][]Hit{}
	for _, sc := range all {
		perRepo[sc.repo.Repo] = append(perRepo[sc.repo.Repo], sc.hit)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# group: %s — per-repo top hits\n", lg.Name))
	names := make([]string, 0, len(perRepo))
	for r := range perRepo {
		names = append(names, r)
	}
	sort.Strings(names)
	for _, rn := range names {
		hits := perRepo[rn]
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
		if len(hits) > 3 {
			hits = hits[:3]
		}
		b.WriteString("\n## " + rn + "\n")
		for _, h := range hits {
			b.WriteString(fmt.Sprintf("%s  %s:%d\n", h.Entity.Name, h.Entity.SourceFile, h.Entity.StartLine))
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// describe
// ---------------------------------------------------------------------------

func (s *Server) handleGetNode(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	key, err := req.RequireString("label_or_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	g, _, _ := s.resolveAndGroup(req)
	allFindings := loadFindings(findingsMemDir(g, lg))
	// Cross-repo prefixed ID? Resolve repo first for unambiguous lookup.
	if rprefix, local := splitPrefixed(key); rprefix != "" {
		if r, ok := lg.Repos[rprefix]; ok && r.Doc != nil {
			if e, ok := r.LabelIndex.ByID[local]; ok {
				out := serializeEntity(r.Repo, e, len(repos) == 1)
				out["findings"] = findingsToJSON(findingsForEntity(allFindings, e.ID, prefixedID(r.Repo, e.ID)), 0)
				return jsonResult(out), nil
			}
		}
	}
	for _, r := range repos {
		if e := r.LabelIndex.Lookup(key); e != nil {
			out := serializeEntity(r.Repo, e, len(repos) == 1)
			out["findings"] = findingsToJSON(findingsForEntity(allFindings, e.ID, prefixedID(r.Repo, e.ID)), 0)
			return jsonResult(out), nil
		}
	}
	return mcpapi.NewToolResultError(fmt.Sprintf("not found: %s", key)), nil
}

// serializeEntity renders an entity as a map. When scopeIsOne, IDs are local;
// otherwise they're prefixed with <repo>::.
func serializeEntity(repo string, e *graph.Entity, scopeIsOne bool) map[string]any {
	id := e.ID
	if !scopeIsOne {
		id = prefixedID(repo, e.ID)
	}
	out := map[string]any{
		"id":             id,
		"label":          e.Name,
		"qualified_name": e.QualifiedName,
		"kind":           stripScopePrefix(e.Kind),
		"source_file":    e.SourceFile,
		"start_line":     e.StartLine,
		"end_line":       e.EndLine,
		"language":       e.Language,
		"repo":           repo,
	}
	if e.PageRank != nil {
		out["pagerank"] = *e.PageRank
	}
	if e.CommunityID != nil {
		out["community_id"] = *e.CommunityID
	}
	if len(e.Properties) > 0 {
		out["properties"] = e.Properties
	}
	return out
}

// ---------------------------------------------------------------------------
// related
// ---------------------------------------------------------------------------

func (s *Server) handleGetNeighbors(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	key, err := req.RequireString("node")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
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
	adj := buildAdjacency(startRepo.Doc, startRepo.Repo)
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
	for _, l := range lg.Links {
		sr, sl := splitPrefixed(l.Source)
		if sr == startRepo.Repo && sl == start.ID {
			out = append(out, map[string]any{
				"id":         l.Target,
				"label":      l.Target,
				"depth":      1,
				"cross_repo": true,
				"kind":       l.Kind,
			})
		}
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
			a := buildAdjacency(r.Doc, r.Repo)
			for _, e := range a.out[local] {
				out = append(out, edge{
					target: prefixedID(repo, e.target),
					kind:   e.kind,
					weight: -math.Log(0.95), // intra-repo confidence ~0.95
				})
			}
		}
		// cross-repo overlay
		for _, l := range lg.Links {
			if l.Source == node {
				conf := l.Confidence
				if conf <= 0 {
					conf = 0.7
				}
				out = append(out, edge{
					target: l.Target,
					kind:   l.Kind,
					weight: -math.Log(conf),
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
	out := []map[string]any{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for _, c := range r.Doc.Communities {
			out = append(out, map[string]any{
				"repo":         r.Repo,
				"id":           c.ID,
				"size":         c.Size,
				"modularity":   c.Modularity,
				"top_entities": c.TopEntities,
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
	nodeID, err := req.RequireString("node_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
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
	if e == nil {
		for _, r := range lg.Repos {
			if r.Doc == nil {
				continue
			}
			if hit := r.LabelIndex.Lookup(nodeID); hit != nil {
				e = hit
				lr = r
				break
			}
		}
	}
	if e == nil {
		return mcpapi.NewToolResultError("node not found"), nil
	}
	abs := e.SourceFile
	if !filepath.IsAbs(abs) && lr.Path != "" {
		abs = filepath.Join(lr.Path, e.SourceFile)
	}
	f, err := os.Open(abs)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	start := e.StartLine - contextLines
	if start < 1 {
		start = 1
	}
	end := e.EndLine + contextLines
	var b strings.Builder
	line := 0
	for scanner.Scan() {
		line++
		if line < start {
			continue
		}
		if line > end {
			break
		}
		b.WriteString(fmt.Sprintf("%5d  %s\n", line, scanner.Text()))
	}
	return mcpapi.NewToolResultText(b.String()), nil
}

// ---------------------------------------------------------------------------
// recent_activity
// ---------------------------------------------------------------------------

func (s *Server) handleRecentActivity(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	since := time.Time{}
	if v := argString(req, "since", ""); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	limit := argInt(req, "limit", 50)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type row struct {
		Repo  string    `json:"repo"`
		ID    string    `json:"id"`
		Label string    `json:"label"`
		File  string    `json:"file"`
		Mtime time.Time `json:"mtime"`
	}
	rows := []row{}
	for _, r := range repos {
		fileMtimes := map[string]time.Time{}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			abs := e.SourceFile
			if !filepath.IsAbs(abs) && r.Path != "" {
				abs = filepath.Join(r.Path, e.SourceFile)
			}
			mt, ok := fileMtimes[abs]
			if !ok {
				if info, err := os.Stat(abs); err == nil {
					mt = info.ModTime()
				}
				fileMtimes[abs] = mt
			}
			if mt.IsZero() || (!since.IsZero() && mt.Before(since)) {
				continue
			}
			rows = append(rows, row{
				Repo: r.Repo, ID: prefixedID(r.Repo, e.ID), Label: e.Name,
				File: e.SourceFile, Mtime: mt,
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Mtime.After(rows[j].Mtime) })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return jsonResult(rows), nil
}

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

func (s *Server) handleResolveLinkCandidate(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	cid, err := req.RequireString("candidate_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	decision, err := req.RequireString("decision")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
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
	if override := argString(req, "override_target", ""); override != "" {
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
		// noop on links file; remaining list drops it.
	default:
		return mcpapi.NewToolResultError("decision must be accept|reject"), nil
	}
	if err := writeLinkCandidates(g, remaining); err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]any{"candidate_id": cid, "decision": decision}), nil
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
	totals := map[string]any{}
	repoStats := []map[string]any{}
	totalE, totalR := 0, 0
	unavailable := []string{}
	for name, r := range lg.Repos {
		if r.Doc == nil {
			unavailable = append(unavailable, name+": "+r.loadErr)
			continue
		}
		totalE += len(r.Doc.Entities)
		totalR += len(r.Doc.Relationships)
		repoStats = append(repoStats, map[string]any{
			"repo":          name,
			"entities":      len(r.Doc.Entities),
			"relationships": len(r.Doc.Relationships),
			"communities":   len(r.Doc.Communities),
		})
	}
	totals["entities"] = totalE
	totals["relationships"] = totalR
	totals["repos"] = repoStats
	totals["cross_repo_links"] = len(lg.Links)
	if len(unavailable) > 0 {
		totals["unavailable"] = unavailable
	}
	return jsonResult(totals), nil
}

func (s *Server) handleGetTelemetry(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	return jsonResult(s.Tel.Snapshot()), nil
}
