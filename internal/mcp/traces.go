// grafel_traces tool — process-flow query surface (#724).
//
// Sub-actions:
//
//	list   — return ranked Process entities loaded for the resolved group
//	get    — return the full step chain for a specific process_id
//	follow — ad-hoc forward BFS from an entry_point_id over the live CALLS
//	         edges (does not require a pre-computed Process)
//
// The list/get paths consume the SCOPE.Process entities + STEP_IN_PROCESS
// edges emitted by Pass 7 (engine.RunProcessFlow). The follow path runs
// a depth-bounded BFS at query time so agents can probe entry points that
// weren't pre-emitted (e.g. ones below the per-fixture ranking threshold).
package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// processEntityKind is the entity kind written by engine.RunProcessFlow.
// Duplicated here as a literal to avoid an internal/engine import.
const (
	processEntityKind = "SCOPE.Process"
	stepInProcessEdge = "STEP_IN_PROCESS"
	entryPointOfEdge  = "ENTRY_POINT_OF"
	// defaultFlowMinSteps mirrors engine.DefaultFlowMinSteps (#1639): flows
	// shorter than this are excluded from the default trace list. Override
	// with min_steps=0 to include every flow. Literal to avoid an
	// internal/engine import.
	defaultFlowMinSteps = 4
)

// handleTraces dispatches grafel_traces to one of its sub-actions.
//
// action is optional; omitting it defaults to "list" for discoverability.
// Valid values: list (ranked process flows), get (full step chain for a
// process by id), follow (ad-hoc forward BFS from an entry-point entity).
func (s *Server) handleTraces(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action := argString(req, "action", "list")
	switch strings.ToLower(action) {
	case "list":
		return s.handleTracesList(ctx, req)
	case "get":
		return s.handleTracesGet(ctx, req)
	case "follow":
		return s.handleTracesFollow(ctx, req)
	default:
		return mcpapi.NewToolResultError("action must be one of: list|get|follow"), nil
	}
}

// handleTracesList returns the top-ranked Process entities in the group,
// optionally filtered to cross_stack=true and capped to limit.
func (s *Server) handleTracesList(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	limit := argInt(req, "limit", 10)
	if limit <= 0 {
		limit = 10
	}
	crossOnly := argBool(req, "cross_stack_only", false)
	// #1639 — short-flow filter. Flows with fewer than min_steps steps are
	// excluded from the default list (they are usually helper calls, not
	// meaningful end-to-end processes). Defaults to defaultFlowMinSteps;
	// pass min_steps=0 to include every flow.
	minSteps := argInt(req, "min_steps", defaultFlowMinSteps)
	if minSteps < 0 {
		minSteps = defaultFlowMinSteps
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type listItem struct {
		ProcessID   string   `json:"process_id"`
		Repo        string   `json:"repo"`
		Label       string   `json:"label"`
		EntryID     string   `json:"entry_id"`
		EntryName   string   `json:"entry_name"`
		TerminalID  string   `json:"terminal_id"`
		StepCount   int      `json:"step_count"`
		CrossStack  bool     `json:"cross_stack"`
		ChainLabels []string `json:"chain_labels"`
		SourceFile  string   `json:"source_file,omitempty"`
	}

	var items []listItem
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			cs := e.PropGet("cross_stack") == "true"
			if crossOnly && !cs {
				continue
			}
			sc, _ := strconv.Atoi(e.PropGet("step_count"))
			// #1639 — exclude trivial short flows from the default list;
			// cross-repo flows are exempt (meaningful even when short).
			if sc < minSteps && !cs {
				continue
			}
			items = append(items, listItem{
				ProcessID:   prefixedID(r.Repo, e.ID),
				Repo:        r.Repo,
				Label:       e.Name,
				EntryID:     e.PropGet("entry_id"),
				EntryName:   e.PropGet("entry_name"),
				TerminalID:  e.PropGet("terminal_id"),
				StepCount:   sc,
				CrossStack:  cs,
				ChainLabels: splitChainLabels(e.PropGet("chain_labels")),
				SourceFile:  e.SourceFile,
			})
		}
	}

	// Sort: cross-stack first, then by step_count desc, then by label.
	sort.Slice(items, func(i, j int) bool {
		if items[i].CrossStack != items[j].CrossStack {
			return items[i].CrossStack
		}
		if items[i].StepCount != items[j].StepCount {
			return items[i].StepCount > items[j].StepCount
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		items = items[:limit]
	}

	// #1738: token-budget cap — shed processes from tail until under budget.
	tokenBudget := argInt(req, "token_budget", 800)
	if tokenBudget < 100 {
		tokenBudget = 100
	}
	budgetBytes := tokenBudget * 4
	if budgetBytes > 64*1024 {
		budgetBytes = 64 * 1024
	}
	preCapLen := len(items)
	items = capByRenderedBytes(items, budgetBytes, false)

	resp := map[string]any{
		"processes": items,
		"count":     len(items),
	}
	if preCapLen > len(items) {
		resp["truncation_note"] = fmt.Sprintf(
			"response capped at token_budget=%d (~%d bytes); %d processes omitted — pass a larger token_budget or use limit=N",
			tokenBudget, budgetBytes, preCapLen-len(items),
		)
	}
	return jsonResult(resp), nil
}

// handleTracesGet returns the full step chain for one Process entity.
func (s *Server) handleTracesGet(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	pid, err := req.RequireString("process_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	verbose := argBool(req, "verbose", false)
	// process_id may be either prefixed ("repo::local") or bare local id.
	repoHint, local := splitPrefixed(pid)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = pid
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind || e.ID != target {
				continue
			}
			// #1905 — bridge steps in cross-repo flows live in companion repos.
			// Build a cross-repo lookup so bridge step entities are enriched with
			// name/file/line/repo from the correct companion repo instead of being
			// emitted as bare {id, node_id, step_index} stubs.
			xrLookup := buildGroupCrossRepoLookup(lg, r.Repo)
			steps := buildProcessStepsWithCrossRepo(r, e, xrLookup, verbose)
			return jsonResult(map[string]any{
				"process_id":  prefixedID(r.Repo, e.ID),
				"repo":        r.Repo,
				"label":       e.Name,
				"entry_id":    e.PropGet("entry_id"),
				"entry_name":  e.PropGet("entry_name"),
				"terminal_id": e.PropGet("terminal_id"),
				"cross_stack": e.PropGet("cross_stack") == "true",
				"steps":       steps,
				"found":       true,
			}), nil
		}
	}
	return jsonResult(map[string]any{"found": false, "process_id": pid}), nil
}

// handleTracesFollow runs an ad-hoc forward BFS over the loaded CALLS
// graph from the given entry_point_id. Used when an agent wants a trace
// from an entity that wasn't selected as an entry candidate.
func (s *Server) handleTracesFollow(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entry, err := req.RequireString("entry_point_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	maxDepth := argInt(req, "max_depth", 8)
	if maxDepth <= 0 || maxDepth > 10 {
		maxDepth = 8
	}
	branch := argInt(req, "branching_factor", 3)
	if branch <= 0 || branch > 4 {
		branch = 3
	}
	verbose := argBool(req, "verbose", false)

	repoHint, local := splitPrefixed(entry)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}
	// Find which repo owns the entity. We follow CALLS edges within that
	// single repo — cross-repo overlay walks are tracked but not expanded
	// (those belong to the cross-stack detection on Process entities).
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entry
		}
		// #1656: O(1) lookup via cached ByID instead of an O(N) scan over
		// every entity in the repo to find the entry point.
		byID := r.getByID()
		entryEnt := byID[target]
		if entryEnt == nil {
			continue
		}
		chains := followCallsBFS(target, maxDepth, branch, r.getCallsAdj())
		// Materialise the chains into step lists with labels.
		// Default (verbose=false): step_index, node_id, name, file, line.
		// Verbose (verbose=true): also includes kind.
		out := make([]map[string]any, 0, len(chains))
		for _, c := range chains {
			steps := make([]map[string]any, 0, len(c))
			for i, id := range c {
				prefID := prefixedID(r.Repo, id)
				step := map[string]any{
					"id":         prefID,
					"step_index": i,
					"node_id":    prefID,
				}
				if e, ok := byID[id]; ok {
					step["name"] = e.Name
					step["file"] = e.SourceFile
					if e.StartLine > 0 {
						step["line"] = e.StartLine
					}
					if verbose {
						step["kind"] = e.Kind
					}
				}
				steps = append(steps, step)
			}
			out = append(out, map[string]any{
				"step_count":  len(c),
				"terminal_id": prefixedID(r.Repo, c[len(c)-1]),
				"steps":       steps,
			})
		}
		// #1618: explicit no-edge signal when the entity was found but BFS
		// yielded no call chains. Agents MUST NOT infer a plausible flow —
		// report the absence verbatim.
		result := map[string]any{
			"entry_point_id": prefixedID(r.Repo, entryEnt.ID),
			"repo":           r.Repo,
			"max_depth":      maxDepth,
			"branching":      branch,
			"chains":         out,
			"count":          len(out),
		}
		if len(out) == 0 {
			result["result"] = "no_outgoing_calls"
			result["note"] = "Graph shows no outgoing CALLS edges from this entity. Do not infer a flow — report the absence."
		}
		return jsonResult(result), nil
	}
	return mcpapi.NewToolResultError("entry_point_id not found in any loaded repo"), nil
}

// crossRepoLookup is a function that resolves an entity ID that may belong
// to a companion repo. Returns the owning repo slug and entity pointer, or
// ("", nil) when the entity is not found in any companion repo.
// Passed to buildProcessSteps so bridge steps carry the correct repo prefix
// and full metadata (#1905).
type crossRepoLookup func(id string) (repoSlug string, e *graph.Entity)

// buildGroupCrossRepoLookup constructs a crossRepoLookup from all repos in a
// LoadedGroup, excluding the seed repo (seedRepo). Companion repos are searched
// in deterministic order so the result is stable. Entities in the seed repo are
// already covered by the byID map passed to buildProcessSteps; this lookup
// handles bridge steps whose entities live elsewhere in the group.
func buildGroupCrossRepoLookup(lg *LoadedGroup, seedRepo string) crossRepoLookup {
	// Pre-collect companion repos in sorted order for determinism.
	type companion struct {
		slug string
		byID map[string]*graph.Entity
	}
	var companions []companion
	for slug, r := range lg.Repos {
		if slug == seedRepo || r == nil || r.Doc == nil {
			continue
		}
		companions = append(companions, companion{slug, r.getByID()})
	}
	sort.Slice(companions, func(i, j int) bool { return companions[i].slug < companions[j].slug })

	return func(id string) (string, *graph.Entity) {
		for _, c := range companions {
			if e, ok := c.byID[id]; ok {
				return c.slug, e
			}
		}
		return "", nil
	}
}

// buildProcessSteps reconstructs the ordered step list for one Process
// from its STEP_IN_PROCESS edges, falling back to the `chain` property
// when the edges are missing.
//
// Default (verbose=false): id, step_index, node_id, name, file, line.
// Verbose (verbose=true): also includes kind.
//
// The id field carries the full ADR-0009 prefixed entity ID ("<repo>::<hex>")
// so callers can pass it directly to grafel_get_source without a separate
// grafel_inspect round-trip (#1744). node_id (local ID) is preserved for
// backward compatibility.
//
// crossRepo, when non-nil, is consulted for step entity IDs not found in r.ByID.
// This is the cross-repo companion lookup needed to enrich bridge steps in
// flows that extend across repo boundaries (#1905). When crossRepo is nil the
// function behaves identically to the single-repo path.
//
// Migrated from (doc *graph.Document, proc *graph.Entity, byID map[string]*graph.Entity,
// repo string) to *LoadedRepo in #2307 — all callers already hold a *LoadedRepo.
func buildProcessSteps(r *LoadedRepo, proc *graph.Entity, verbose ...bool) []map[string]any {
	return buildProcessStepsWithCrossRepo(r, proc, nil, verbose...)
}

// buildProcessStepsWithCrossRepo is the cross-repo-aware variant of
// buildProcessSteps (#1905). Pass a non-nil crossRepo to resolve bridge step
// entities that live in companion repos.
func buildProcessStepsWithCrossRepo(r *LoadedRepo, proc *graph.Entity, crossRepo crossRepoLookup, verbose ...bool) []map[string]any {
	wantVerbose := len(verbose) > 0 && verbose[0]
	repo := r.Repo
	byID := r.getByID()
	type indexed struct {
		idx int
		id  string
	}
	var ordered []indexed
	// Consume the pre-built STEP_IN_PROCESS adjacency index (#2417).
	// StepAdj is always populated by reloadLocked (state.go) since PR #2439.
	for _, se := range r.getStepAdj()[proc.ID] {
		ordered = append(ordered, indexed{se.idx, se.toID})
	}
	if len(ordered) == 0 {
		// Fallback to the chain property if the edges weren't emitted.
		ids := strings.Split(proc.PropGet("chain"), ",")
		for i, id := range ids {
			if id != "" {
				ordered = append(ordered, indexed{i, id})
			}
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].idx < ordered[j].idx })
	out := make([]map[string]any, 0, len(ordered))
	for _, o := range ordered {
		// Determine which repo owns this step entity. Seed repo first (#1905):
		// bridge steps carry IDs that live in companion repos and must use the
		// companion's slug for the prefixed id field, not the seed repo slug.
		stepRepo := repo
		var ent *graph.Entity
		if e, ok := byID[o.id]; ok {
			ent = e
		} else if crossRepo != nil {
			// Not found in the seed repo — look across companion repos.
			if slug, e := crossRepo(o.id); e != nil {
				stepRepo = slug
				ent = e
			}
		}

		step := map[string]any{
			"id":         prefixedID(stepRepo, o.id),
			"step_index": o.idx,
			"node_id":    o.id,
		}
		if ent != nil {
			step["name"] = ent.Name
			step["file"] = ent.SourceFile
			if ent.StartLine > 0 {
				step["line"] = ent.StartLine
			}
			if wantVerbose {
				step["kind"] = ent.Kind
			}
			// Stamp the owning repo on every step so consumers can distinguish
			// seed-repo steps from bridge steps without parsing the id prefix.
			step["repo"] = stepRepo
		}
		out = append(out, step)
	}
	return out
}

// followCallsBFS is the query-time equivalent of engine.bfsTraces — it
// walks forward CALLS edges from entry, bounded by maxDepth and
// branching, and returns each terminal chain.
//
// callsAdj must be non-nil; it is cached on LoadedRepo at reload time (#1656).
func followCallsBFS(entry string, maxDepth, branch int, callsAdj map[string][]string) [][]string {
	out := make(map[string][]string)
	type fr struct {
		chain []string
		seen  map[string]bool
	}
	adj := callsAdj
	work := []fr{{chain: []string{entry}, seen: map[string]bool{entry: true}}}
	for len(work) > 0 {
		f := work[len(work)-1]
		work = work[:len(work)-1]
		cur := f.chain[len(f.chain)-1]
		ns := adj[cur]
		if len(ns) == 0 || len(f.chain) > maxDepth {
			term := f.chain[len(f.chain)-1]
			if prev, ok := out[term]; !ok || len(prev) < len(f.chain) {
				out[term] = append([]string(nil), f.chain...)
			}
			continue
		}
		capped := ns
		if len(capped) > branch {
			capped = capped[:branch]
		}
		extended := false
		for _, n := range capped {
			if f.seen[n] {
				continue
			}
			extended = true
			newSeen := make(map[string]bool, len(f.seen)+1)
			for k := range f.seen {
				newSeen[k] = true
			}
			newSeen[n] = true
			work = append(work, fr{chain: append(append([]string(nil), f.chain...), n), seen: newSeen})
		}
		if !extended {
			term := f.chain[len(f.chain)-1]
			if prev, ok := out[term]; !ok || len(prev) < len(f.chain) {
				out[term] = append([]string(nil), f.chain...)
			}
		}
	}
	chains := make([][]string, 0, len(out))
	for _, c := range out {
		chains = append(chains, c)
	}
	sort.Slice(chains, func(i, j int) bool {
		if len(chains[i]) != len(chains[j]) {
			return len(chains[i]) > len(chains[j])
		}
		return strings.Join(chains[i], ",") < strings.Join(chains[j], ",")
	})
	return chains
}

func splitChainLabels(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, " → ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
