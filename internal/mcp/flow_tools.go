// flow_tools.go — MCP handlers for flow-aware graph traversal tools (issue #1252).
//
// Implements:
//   - grafel_find_callers       — what calls this entity (inbound edges, N hops)
//   - grafel_find_callees       — what does this entity call (outbound edges, N hops)
//   - grafel_impact_radius      — entities affected if this one changes, with risk score
//   - grafel_subgraph           — unified subgraph tool (format=raw|markdown) (#1754)
//   - grafel_find_dead_code     — unreferenced public operations carrying a dead-code marker
//
// All handlers operate against the in-memory LoadedGroup data — no HTTP calls.
package mcp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// neighborTruncationNote builds the truncation note for find_callers /
// find_callees when the token-budget cap shed neighbors (#1738, #3648).
//
// Pre-#3648 the note only reported a raw count ("15 callers omitted"), which
// gave the agent no way to tell whether it had lost the production callers it
// needed or merely some test callers. Because neighbors are now sorted
// production-first, the dropped tail skews toward test callers — the note makes
// that explicit ("15 callers omitted: 12 test, 3 production") so the caller
// knows exactly what is missing and how to recover it.
//
// kind is the plural neighbor noun ("callers" or "callees").
func neighborTruncationNote(kind string, tokenBudget, budgetBytes, dropped, droppedProd, droppedTest int) string {
	breakdown := ""
	if droppedProd > 0 || droppedTest > 0 {
		breakdown = fmt.Sprintf(" (%d test, %d production)", droppedTest, droppedProd)
	}
	hint := "pass a larger token_budget or reduce depth"
	if droppedProd > 0 {
		hint = "production " + kind + " were dropped — raise token_budget or reduce depth to see them"
	} else if droppedTest > 0 {
		hint = "only test " + kind + " were dropped; raise token_budget if you need them"
	}
	return fmt.Sprintf(
		"response capped at token_budget=%d (~%d bytes); %d %s omitted%s — %s",
		tokenBudget, budgetBytes, dropped, kind, breakdown, hint,
	)
}

// ---------------------------------------------------------------------------
// grafel_neighbors (#1753) — unified callers + callees + both.
// ---------------------------------------------------------------------------

// handleNeighbors is the unified neighbors tool that subsumes find_callers and
// find_callees behind a `direction` discriminator. It defers to the existing
// handlers for direction=in / direction=out to preserve byte-for-byte response
// shape, and merges the two when direction=both.
func (s *Server) handleNeighbors(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	dir := strings.ToLower(strings.TrimSpace(argString(req, "direction", "both")))
	switch dir {
	case "in", "inbound", "callers":
		return s.handleFindCallers(ctx, req)
	case "out", "outbound", "callees":
		return s.handleFindCallees(ctx, req)
	case "", "both", "all":
		// #2325: cross-handler dispatch goes through the structured-return
		// seam so mergeNeighbors does NOT have to parse wire bytes. The
		// outer wire wrapping happens once, at the end, via jsonResult.
		inVal, inErr := s.findCallersStructured(ctx, req)
		outVal, outErr := s.findCalleesStructured(ctx, req)
		return mergeNeighbors(inVal, inErr, outVal, outErr), nil
	default:
		return mcpapi.NewToolResultError("invalid direction: " + dir + " (want in|out|both)"), nil
	}
}

// mergeNeighbors combines callers + callees structured envelopes into one
// record. Either input may be nil (error from the inner handler); if both are
// nil it returns the inbound error (or the outbound one if inbound was OK).
//
// #2325: this used to parse `res.Content[0].(TextContent).Text` to recover
// the structured value the inner handlers had just marshaled. With
// findCallersStructured / findCalleesStructured exposing the typed value
// directly, the parse step is gone — the only marshal happens at the wire
// boundary inside jsonResult.
func mergeNeighbors(in map[string]any, inErr *mcpapi.CallToolResult, out map[string]any, outErr *mcpapi.CallToolResult) *mcpapi.CallToolResult {
	if in == nil && out == nil {
		if inErr != nil {
			return inErr
		}
		return outErr
	}
	merged := map[string]any{}
	pick := in
	if pick == nil {
		pick = out
	}
	for _, k := range []string{"entity_id", "entity_name", "repo", "depth"} {
		if v, ok := pick[k]; ok {
			merged[k] = v
		}
	}
	if in != nil {
		if v, ok := in["callers"]; ok {
			merged["callers"] = v
		}
		if v, ok := in["truncation_note"]; ok {
			merged["callers_truncation_note"] = v
		}
		if v, ok := in["omitted"]; ok {
			merged["callers_omitted"] = v
		}
	}
	if out != nil {
		if v, ok := out["callees"]; ok {
			merged["callees"] = v
		}
		if v, ok := out["truncation_note"]; ok {
			merged["callees_truncation_note"] = v
		}
		if v, ok := out["omitted"]; ok {
			merged["callees_omitted"] = v
		}
	}
	merged["direction"] = "both"
	return jsonResult(merged)
}

// ---------------------------------------------------------------------------
// grafel_find_callers
// ---------------------------------------------------------------------------

// handleFindCallers returns entities that call (directly or transitively) the
// given entity. It walks the inbound adjacency up to `depth` hops and returns
// results grouped by hop distance so the agent can see the call fan-in at each
// level.
//
// Wire wrapper: defers all work to findCallersStructured, then marshals via
// jsonResult. Internal cross-handler callers (e.g. mergeNeighbors) call the
// structured variant directly and skip the wire round-trip (#2325).
func (s *Server) handleFindCallers(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	v, errRes := s.findCallersStructured(ctx, req)
	if errRes != nil {
		return errRes, nil
	}
	return jsonResult(v), nil
}

// findCallersStructured is the non-wire variant of handleFindCallers. It
// returns the structured result map directly (or a *CallToolResult for the
// error path). Internal cross-handler dispatch (mergeNeighbors) calls this
// instead of the wire handler so no wire bytes are parsed back into a map
// for the merge — closes #2325.
func (s *Server) findCallersStructured(_ context.Context, req mcpapi.CallToolRequest) (map[string]any, *mcpapi.CallToolResult) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return nil, mcpapi.NewToolResultError(err.Error())
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return nil, errRes
	}
	// #2665: route-literal resolution. When entity_id starts with "/" AND no
	// entity matches by ID/name across loaded repos, treat it as a route
	// literal: find NAVIGATES_TO edges whose ToID is "route:<literal>" (or
	// whose Properties["route"] matches) and return the push-site callers
	// directly. This makes find_callers discoverable for in-app navigation
	// without users having to remember the dedicated grafel_navigates tool.
	if strings.HasPrefix(entityID, "/") && !entityExistsAnywhere(lg, entityID) {
		if res := s.tryFindCallersByRoute(req, lg, entityID); res != nil {
			return res, nil
		}
	}
	depth := argInt(req, "depth", 1)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	verbose := argBool(req, "verbose", false)

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	// #5314: shared name/qualified-name resolution. probe is the un-prefixed
	// entity argument. resolveEntityArg first tries the exact hex id (happy
	// path, unchanged), then falls back to a unique Name/QualifiedName match,
	// and returns disambiguation candidates when the name is ambiguous. A
	// genuine miss leaves (nil,"",nil) so the verbatim "entity not found" error
	// below still fires. Restricting `repos`/`local` to the resolved entity
	// keeps the downstream BFS and output shape byte-for-byte identical.
	probe := local
	if probe == "" {
		probe = entityID
	}
	if rr, rl, disambig := resolveEntityArg(repos, entityID, probe); disambig != nil {
		return nil, disambig
	} else if rr != nil {
		repos = []*LoadedRepo{rr}
		local = rl
	}

	// Default shape: id, name, file, line, hop_count.
	// Verbose shape: also includes kind, repo.
	type caller struct {
		EntityID   string `json:"id"`
		Name       string `json:"name"`
		Kind       string `json:"kind,omitempty"`
		Repo       string `json:"repo,omitempty"`
		SourceFile string `json:"file,omitempty"`
		StartLine  int    `json:"line,omitempty"`
		HopCount   int    `json:"hop_count"`
		// EdgeKind is the on-graph relationship kind of the inbound edge via
		// which this caller was discovered (CALLS, INJECTED_INTO, THROWS,
		// CATCHES, REFERENCES, IMPORTS, …). #4242: pre-fix, find_callers only
		// walked inboundRefKinds (CALLS/REFERENCES/IMPORTS/TESTS/…), silently
		// dropping every non-CALLS semantic predecessor (INJECTED_INTO, THROWS,
		// CATCHES, JOINS_COLLECTION, …) that the callees/out side already
		// surfaces — so the rewrite oracle falsely concluded those edges were
		// unmodeled. The walk now includes all semantic kinds AND labels the
		// kind per neighbor so a consumer can tell INJECTED_INTO from CALLS.
		EdgeKind string `json:"edge_kind,omitempty"`
		// ViaInherits marks a caller reached by a reverse-INHERITS hop (#3834):
		// an inheriting subclass stub that exposes this (base) member's
		// behaviour, not a direct CALLS-site of it.
		ViaInherits bool `json:"via_inherits,omitempty"`
		// isTest is excluded from the wire shape (json:"-") — it is an internal
		// ranking/accounting signal only. #3648: production callers outrank test
		// callers so they survive the token-budget cap, and the truncation note
		// reports how many of each kind were dropped.
		isTest bool `json:"-"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.getByID()
		if _, ok := byID[target]; !ok {
			continue
		}

		// BFS over inbound-only adjacency, restricted to reference kinds
		// (CALLS, REFERENCES, TESTS, ROUTES_TO, etc.). CONTAINS is excluded:
		// a module/file that CONTAINS an entity is not a caller. Without this
		// filter find_callers was returning CONTAINS-linked parent nodes and
		// other structural edges as fake "callers" (#1915).
		//
		// #2039: track the edge kind that *discovered* each node. When the
		// discovering edge is REFERENCES or IMPORTS, a file/module CONTAINER
		// source is a legitimate caller — post-#2020 file entities own these
		// edges (e.g. `core/admin.py` REFERENCES Models, `views.py` IMPORTS
		// HasPermission, `__init__.py` IMPORTS a re-exported module). The
		// noiseContainer filter below must NOT drop those.
		adj := r.getAdjacency()
		visited := map[string]int{target: 0}
		// discoveredVia[id] = edge kind via which `id` was first reached on
		// the inbound BFS. Used below to decide whether to allow file/module
		// container sources through the noise filter.
		discoveredVia := map[string]string{}
		frontier := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range frontier {
				for _, e := range adj.in[n] {
					// #4242: accept every semantic predecessor edge, not just the
					// inboundRefKinds allow-list. The out/callees side already
					// surfaces all kinds; mirroring it here is what makes the
					// reverse direction symmetric. Pure-structural edges
					// (CONTAINS — every entity is contained by its module) carry
					// no caller signal and stay excluded (see #1915).
					if !isInboundNeighborKind(e.kind) {
						continue
					}
					if _, seen := visited[e.target]; seen {
						continue
					}
					visited[e.target] = d + 1
					discoveredVia[e.target] = e.kind
					next = append(next, e.target)
				}
				// #3834 (MRO T4): reverse-INHERITS. When `n` is a DEFINING base
				// member, the subclasses that inherit it (their bodyless stubs)
				// are legitimate callers — they expose `n`'s behaviour under
				// their own class. Surface them so neighbors(in) on a base method
				// reaches the inheriting subclasses.
				for _, stub := range mroInboundEdges(r, n) {
					if _, seen := visited[stub]; seen {
						continue
					}
					visited[stub] = d + 1
					discoveredVia[stub] = inheritsEdgeKind
					next = append(next, stub)
				}
			}
			frontier = next
			if len(frontier) == 0 {
				break
			}
		}

		callers := []caller{}
		for id, d := range visited {
			if id == target {
				continue
			}
			dk := discoveredVia[id]
			isFileRefEdge := dk == "REFERENCES" || dk == "IMPORTS"
			// #3834: a reverse-INHERITS caller is a (bodyless) inheriting stub.
			// It is a legitimate caller of the base member, so it must survive
			// the shadow/container noise filter exactly like a file-ref edge.
			isInheritsEdge := dk == inheritsEdgeKind
			e := byID[id]
			if e == nil {
				// #2015: previously a nil byID lookup silently dropped the
				// caller. In production this hides legitimate file-level
				// callers whose IMPORTS/REFERENCES edge FromID never got
				// rewritten from the raw file path to the stamped FileEntity
				// hex ID (cross-repo linker / resolver rewrite gap). When the
				// discovering edge is REFERENCES or IMPORTS the source IS a
				// file or module — emit a synthetic caller using the id as
				// both id and name so the signal reaches the agent. Without
				// this, find_callers returns N-1 callers for any model whose
				// admin.py / __init__.py source isn't an indexed entity.
				//
				// #4288: extend the same synthetic-fallback to projected semantic
				// predecessors whose far side is unresolved (symmetric with the
				// callees-side JOINS_COLLECTION fix). inspect's semantic_edges emits
				// these regardless of resolution; neighbors must not silently drop
				// them.
				//
				// #5475: extend the fallback to the REMAINING real caller edge kinds
				// too (CALLS / TESTS / IMPLEMENTS / HANDLES / ROUTES_TO / … and the
				// inheritance kinds). Every id that reaches this loop was discovered
				// via isInboundNeighborKind, which is precisely the "this is a real
				// caller relationship" gate — pure-structural noise (CONTAINS /
				// DECLARES / DEFINES) is already excluded upstream. So when byID[id]
				// is nil because the edge's FromID was never rewritten from a raw
				// path / cross-repo placeholder to a stamped entity id (the linker /
				// resolver rewrite gap), the caller is REAL and must not be silently
				// dropped: emit a synthetic caller using the id, exactly as #2015 /
				// #4288 do, so find_callers stops returning N-1 callers. The
				// remaining guard only filters the genuinely-structural inbound kinds
				// that should never have produced a caller in the first place.
				if !isFileRefEdge && !isSemanticEdgeKind(dk) && !isInboundNeighborKind(dk) {
					continue
				}
				name := id
				if i := strings.LastIndexByte(name, '/'); i >= 0 {
					name = name[i+1:]
				}
				callers = append(callers, caller{
					EntityID: prefixedID(r.Repo, id),
					Name:     name,
					HopCount: d,
					EdgeKind: dk, // #4242: label the discovering edge kind.
					isTest:   isTestFileMCP(id),
				})
				continue
			}
			// #1614: drop file/module CONTAINER components and inferred
			// shadows. Callers should be operation/component-level referencers,
			// not the synthetic file node that "contains" the call. (q02/q03/q10)
			//
			// #2039: exception — when the discovering inbound edge is
			// REFERENCES or IMPORTS, a file/module container IS the legitimate
			// caller (file-level reference / import edges live on the file
			// entity post-#2020).
			//
			// #2015 (this PR): noiseShadow was previously dropped
			// unconditionally; in practice the bodiless-component classifier
			// (StartLine==0 && QualifiedName=="") can capture *real* file or
			// module containers whose subtype property got dropped during
			// fb-load / record-conversion. When the discovering edge is
			// REFERENCES or IMPORTS the source is a real referencer — keep it.
			switch classifyNoise(e) {
			case noiseShadow:
				if !isFileRefEdge && !isInheritsEdge {
					continue
				}
			case noiseContainer:
				if !isFileRefEdge {
					continue
				}
			}
			c := caller{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				HopCount:   d,
				EdgeKind:   dk, // #4242: label the discovering edge kind.
				isTest:     isTestFileMCP(e.SourceFile),
			}
			if isInheritsEdge {
				c.ViaInherits = true
			}
			if verbose {
				c.Kind = stripScopePrefix(e.Kind)
				c.Repo = r.Repo
			}
			callers = append(callers, c)
		}

		// #2577/#2591: rank callers by call frequency (descending).
		//
		// Frequency is the sum of edge weights for CALLS edges pointing from
		// each source to target. edge.weight is 1.0 per raw edge, OR the
		// numeric value of Properties["count"] when the extractor deduplicates
		// multiple call sites into a single edge with a count property.
		//
		// Summing weights (not counting raw edges) means both representation
		// styles are handled uniformly:
		//   - Extractor emits N duplicate CALLS edges → each weight=1 → sum=N
		//   - Extractor emits 1 CALLS edge with Properties["count"]="N" → weight=N → sum=N
		callFrequency := make(map[string]float64)
		for _, e := range adj.in[target] {
			if e.kind == "CALLS" {
				callFrequency[e.target] += e.weight
			}
		}

		sort.Slice(callers, func(i, j int) bool {
			if callers[i].HopCount != callers[j].HopCount {
				return callers[i].HopCount < callers[j].HopCount
			}
			// #3648: within the same hop level, production callers rank ahead of
			// test callers so they survive the token-budget cap. A high-degree
			// node (e.g. 34 callers, default budget ~800 tokens) previously shed
			// the tail in insertion order, silently dropping production callers
			// while keeping test ones. Production-first ordering guarantees the
			// callers an agent most needs to reason about are the last to go.
			if callers[i].isTest != callers[j].isTest {
				return !callers[i].isTest // production (false) before test (true)
			}
			// Within the same hop level, sort by call frequency (descending).
			// Extract unprefixed ID from EntityID for frequency lookup.
			idI := callers[i].EntityID
			idJ := callers[j].EntityID
			if _, local := splitPrefixed(idI); local != "" {
				idI = local
			}
			if _, local := splitPrefixed(idJ); local != "" {
				idJ = local
			}
			freqI := callFrequency[idI]
			freqJ := callFrequency[idJ]
			if freqI != freqJ {
				return freqI > freqJ // descending
			}
			// Tie-break alphabetically by name.
			return callers[i].Name < callers[j].Name
		})

		root := byID[target]
		rootName := target
		if root != nil {
			rootName = root.Name
		}

		// #1738: token-budget cap — shed callers from tail until under budget.
		tokenBudget := argInt(req, "token_budget", 800)
		if tokenBudget < 100 {
			tokenBudget = 100
		}
		budgetBytes := tokenBudget * 4
		if budgetBytes > 64*1024 {
			budgetBytes = 64 * 1024
		}
		preCapLen := len(callers)
		// #3648: capture the dropped tail so the truncation note can report a
		// production/test breakdown. callers is already sorted production-first,
		// so the dropped slice is the cap boundary onward.
		precap := callers
		callers = capByRenderedBytes(callers, budgetBytes, false)
		droppedTest, droppedProd := 0, 0
		for _, c := range precap[len(callers):] {
			if c.isTest {
				droppedTest++
			} else {
				droppedProd++
			}
		}

		// #1618: distinguish "entity found, zero callers" from "entity not found".
		// An empty callers array with result="no_incoming_edges" is an explicit
		// graph signal — agents MUST NOT infer a plausible relationship to fill
		// the gap. Report the absence verbatim.
		result := map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"depth":       depth,
			"callers":     callers,
			"count":       len(callers),
		}
		if preCapLen > len(callers) {
			result["truncation_note"] = neighborTruncationNote(
				"callers", tokenBudget, budgetBytes, preCapLen-len(callers), droppedProd, droppedTest,
			)
			result["omitted"] = map[string]any{
				"total":      preCapLen - len(callers),
				"production": droppedProd,
				"test":       droppedTest,
			}
		}
		if len(callers) == 0 && preCapLen == 0 {
			result["result"] = "no_incoming_edges"
			result["note"] = "Graph shows no callers for this entity within the requested depth. Do not infer a relationship — report the absence."
		}
		return result, nil
	}
	return nil, mcpapi.NewToolResultError("entity not found: " + entityID)
}

// ---------------------------------------------------------------------------
// grafel_find_callees
// ---------------------------------------------------------------------------

// handleFindCallees returns entities called by the given entity. It walks the
// outbound adjacency up to `depth` hops, returning results grouped by hop
// distance so the agent sees the call fan-out at each level.
//
// Wire wrapper: see handleFindCallers for the structured-variant rationale
// (#2325).
func (s *Server) handleFindCallees(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	v, errRes := s.findCalleesStructured(ctx, req)
	if errRes != nil {
		return errRes, nil
	}
	return jsonResult(v), nil
}

// findCalleesStructured is the non-wire variant of handleFindCallees (#2325).
func (s *Server) findCalleesStructured(_ context.Context, req mcpapi.CallToolRequest) (map[string]any, *mcpapi.CallToolResult) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return nil, mcpapi.NewToolResultError(err.Error())
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return nil, errRes
	}
	depth := argInt(req, "depth", 1)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	verbose := argBool(req, "verbose", false)

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	// #5314: shared name/qualified-name resolution (see findCallersStructured).
	probe := local
	if probe == "" {
		probe = entityID
	}
	if rr, rl, disambig := resolveEntityArg(repos, entityID, probe); disambig != nil {
		return nil, disambig
	} else if rr != nil {
		repos = []*LoadedRepo{rr}
		local = rl
	}

	// Default shape: id, name, file, line, hop_count.
	// Verbose shape: also includes kind, repo.
	type callee struct {
		EntityID   string `json:"id"`
		Name       string `json:"name"`
		Kind       string `json:"kind,omitempty"`
		Repo       string `json:"repo,omitempty"`
		SourceFile string `json:"file,omitempty"`
		StartLine  int    `json:"line,omitempty"`
		HopCount   int    `json:"hop_count"`
		// EdgeKind is the on-graph relationship kind of the outbound edge via
		// which this callee was discovered (CALLS, INJECTED_INTO, THROWS, …).
		// #4242: symmetric with the callers side so a consumer can tell an
		// INJECTED_INTO callee from a CALLS one.
		EdgeKind string `json:"edge_kind,omitempty"`
		// ViaInherits marks a callee reached by hopping through an inherited
		// member's INHERITS edge (#3834) — i.e. the node is a defining base/
		// mixin member, not a direct CALLS target of the queried entity.
		ViaInherits bool `json:"via_inherits,omitempty"`
		// isTest: internal ranking/accounting signal only (json:"-"). #3648.
		isTest bool `json:"-"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.getByID()
		if _, ok := byID[target]; !ok {
			continue
		}

		// BFS over outbound-only adjacency.
		//
		// #3834 (MRO T4): when the walk reaches an inherited-member STUB (empty
		// CALLS edges — the body lives on a base/mixin), splice a synthetic
		// INHERITS hop to the DEFINING member so callees reach the real base
		// implementation instead of dead-ending at the bodyless node.
		// mroExternal records synthetic external-contract nodes (DRF mixin) so
		// they render without a backing indexed entity; mroVia marks the
		// INHERITS-discovered ids so the wire shape can label the hop.
		adj := r.getAdjacency()
		visited := map[string]int{target: 0}
		mroExternal := map[string]*graph.Entity{}
		mroVia := map[string]bool{}
		// discoveredVia[id] = on-graph edge kind via which `id` was first reached
		// on the outbound BFS — used to label each callee with its edge_kind so
		// the out side is symmetric with the callers side (#4242).
		discoveredVia := map[string]string{}
		frontier := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range frontier {
				for _, e := range adj.out[n] {
					if _, seen := visited[e.target]; seen {
						continue
					}
					visited[e.target] = d + 1
					discoveredVia[e.target] = e.kind
					next = append(next, e.target)
				}
				for _, me := range mroOutboundEdges(r, n) {
					if _, seen := visited[me.Target]; seen {
						continue
					}
					visited[me.Target] = d + 1
					mroVia[me.Target] = true
					discoveredVia[me.Target] = inheritsEdgeKind // #4242
					if me.External && me.Contract != nil {
						mroExternal[me.Target] = me.Contract
					}
					// An external contract is a leaf — no fabricated callees;
					// continue the walk only from in-repo defining members.
					if !me.External {
						next = append(next, me.Target)
					}
				}
			}
			frontier = next
			if len(frontier) == 0 {
				break
			}
		}

		callees := []callee{}
		for id, d := range visited {
			if id == target {
				continue
			}
			e := byID[id]
			if e == nil {
				e = mroExternal[id]
			}
			if e == nil {
				// #4288: a semantic out-edge (JOINS_COLLECTION, GRAPH_RELATES,
				// DEPENDS_ON_SERVICE, …) may point at a far-side id that has no
				// backing indexed entity — the real upvate-core case is a
				// DataAccess node JOINS_COLLECTION-ing a class id (Class:Inspection)
				// that was never stamped as an Entity. inspect's semantic_edges
				// section emits that edge regardless of target resolution; neighbors
				// previously DROPPED it here, producing the inspect-vs-neighbors gap.
				// Emit a synthetic callee using the id as both id and name so the
				// semantic neighbour surfaces with its edge_kind, exactly like the
				// callers-side file-ref synthetic fallback (#2015). Pure-structural
				// or CALLS targets that fail to resolve are NOT fabricated — only
				// the projected semantic kinds, to avoid leaking dangling refs.
				dk := discoveredVia[id]
				if !isSemanticEdgeKind(dk) {
					continue
				}
				name := id
				if i := strings.LastIndexByte(name, '/'); i >= 0 {
					name = name[i+1:]
				}
				callees = append(callees, callee{
					EntityID: prefixedID(r.Repo, id),
					Name:     name,
					HopCount: d,
					EdgeKind: dk,
					isTest:   isTestFileMCP(id),
				})
				continue
			}
			c := callee{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				HopCount:   d,
				EdgeKind:   discoveredVia[id], // #4242: label the discovering edge kind.
				isTest:     isTestFileMCP(e.SourceFile),
			}
			if mroVia[id] {
				c.ViaInherits = true
			}
			if verbose {
				c.Kind = stripScopePrefix(e.Kind)
				c.Repo = r.Repo
			}
			callees = append(callees, c)
		}
		sort.Slice(callees, func(i, j int) bool {
			if callees[i].HopCount != callees[j].HopCount {
				return callees[i].HopCount < callees[j].HopCount
			}
			// #3648: production callees outrank test callees so they survive the
			// token-budget cap (mirrors find_callers).
			if callees[i].isTest != callees[j].isTest {
				return !callees[i].isTest
			}
			return callees[i].Name < callees[j].Name
		})

		root := byID[target]
		rootName := target
		if root != nil {
			rootName = root.Name
		}

		// #1738: token-budget cap — shed callees from tail until under budget.
		tokenBudget := argInt(req, "token_budget", 800)
		if tokenBudget < 100 {
			tokenBudget = 100
		}
		budgetBytes := tokenBudget * 4
		if budgetBytes > 64*1024 {
			budgetBytes = 64 * 1024
		}
		preCapLen := len(callees)
		precap := callees // #3648: retain pre-cap slice for the dropped breakdown.
		callees = capByRenderedBytes(callees, budgetBytes, false)
		droppedTest, droppedProd := 0, 0
		for _, c := range precap[len(callees):] {
			if c.isTest {
				droppedTest++
			} else {
				droppedProd++
			}
		}

		// #1618: distinguish "entity found, zero callees" from "entity not found".
		// An empty callees array with result="no_outgoing_edges" is an explicit
		// graph signal — agents MUST NOT infer a plausible relationship to fill
		// the gap. Report the absence verbatim.
		result := map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"depth":       depth,
			"callees":     callees,
			"count":       len(callees),
		}
		if preCapLen > len(callees) {
			result["truncation_note"] = neighborTruncationNote(
				"callees", tokenBudget, budgetBytes, preCapLen-len(callees), droppedProd, droppedTest,
			)
			result["omitted"] = map[string]any{
				"total":      preCapLen - len(callees),
				"production": droppedProd,
				"test":       droppedTest,
			}
		}
		if len(callees) == 0 && preCapLen == 0 {
			result["result"] = "no_outgoing_edges"
			result["note"] = "Graph shows no callees for this entity. Do not infer a relationship — report the absence."
		}
		return result, nil
	}
	return nil, mcpapi.NewToolResultError("entity not found: " + entityID)
}

// ---------------------------------------------------------------------------
// grafel_impact_radius
// ---------------------------------------------------------------------------

// impactRiskScore computes a heuristic risk score [0.0, 1.0] for an affected
// entity. Higher means "more risky to touch". Factors:
//   - in-degree (more callers → higher blast radius if it breaks)
//   - is the entity a public API endpoint or topic publisher
//   - lack of test coverage indicator
//
// Test-coverage signal (#3974): an entity is treated as covered when EITHER it
// carries a positive "test_coverage" property OR it has ≥1 inbound TESTS edge
// (a real test→SUT relation from test→SUT extraction, #3754/#3855). Relying on
// the property alone falsely flagged genuinely-tested entities (e.g. AuthService
// with inbound TESTS edges) as "no test coverage". A test-spec entity itself
// (isTestEntity) is not production code, so the no-coverage penalty never
// applies to it.
func impactRiskScore(e *graph.Entity, inDegree int, hasInboundTests, isTestEntity bool) float64 {
	score := 0.0

	// In-degree contribution: log-scale, max contribution 0.5.
	if inDegree > 0 {
		// ln(inDegree+1)/ln(51) caps at 1.0 for inDegree=50, then clamp at 0.5.
		contrib := 0.0
		for n := inDegree + 1; n > 1; n /= 2 {
			contrib += 0.1
		}
		if contrib > 0.5 {
			contrib = 0.5
		}
		score += contrib
	}

	// API boundary: endpoints and topics are higher risk.
	k := strings.ToLower(e.Kind)
	if strings.Contains(k, "http_endpoint") || strings.Contains(k, "endpoint") ||
		strings.Contains(k, "topic") || strings.Contains(k, "queue") {
		score += 0.25
	}

	// No test coverage: increase risk — but only for production entities that
	// have neither a positive coverage property nor an inbound TESTS edge.
	if !isTestEntity && !entityHasTestCoverage(e, hasInboundTests) {
		score += 0.25
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

// entityHasTestCoverage reports whether an entity has genuine test linkage.
// True when EITHER a positive "test_coverage" property is present (non-empty,
// not "0"/"none") OR the entity has ≥1 inbound TESTS edge. Centralised so the
// risk score and the risk_reason string stay in agreement (#3974).
func entityHasTestCoverage(e *graph.Entity, hasInboundTests bool) bool {
	if hasInboundTests {
		return true
	}
	cov := e.Properties["test_coverage"]
	return cov != "" && cov != "0" && cov != "none"
}

// isTestSpecEntity reports whether an entity is itself test/spec code rather
// than production code to flag (#3974). A test-spec entity must not be labelled
// "no test coverage": it is not a unit under test. We use the same test-file
// convention predicate as dead-code analysis, plus an explicit Pattern/Test
// kind check for spec-pattern nodes that may not live in a conventional path.
func isTestSpecEntity(e *graph.Entity) bool {
	if isTestFileMCP(e.SourceFile) {
		return true
	}
	k := strings.ToLower(e.Kind)
	if strings.Contains(k, "test") || strings.Contains(k, "spec") {
		return true
	}
	return false
}

// impactRadiusMaxResults bounds the affected-set returned for a single
// impact_radius call. A pathological high-degree node (e.g. a base class or a
// utility imported everywhere) can have thousands of transitive dependents; we
// keep the top-ranked slice and emit an honest truncation marker rather than
// returning an unbounded payload. The cap is generous so normal nodes are never
// truncated.
const impactRadiusMaxResults = 500

// impactCandidate is a disambiguation entry returned when entity_id resolves to
// more than one entity by name. It carries enough to let the caller re-issue
// the request against a precise ID.
type impactCandidate struct {
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Repo       string `json:"repo"`
	SourceFile string `json:"source_file,omitempty"`
}

// impactResolution is the outcome of resolving the entity_id argument against
// the loaded group. Exactly one of {repo+localID set, candidates non-empty,
// neither (not found)} holds.
type impactResolution struct {
	repo       *LoadedRepo // resolved repo when a unique entity was found
	localID    string      // resolved local (un-prefixed) entity ID
	candidates []impactCandidate
}

// resolveImpactTarget resolves the entity_id argument to a concrete entity.
//
// Resolution order, designed to convert the dominant error class (agents
// passing a name, a stale ID, or an ambiguous symbol) into graceful results:
//  1. Exact local-ID match in any in-scope repo → unique resolution.
//  2. Else exact Name (or QualifiedName) match across in-scope repos:
//     - exactly one → unique resolution (the entity_id was a name, not an ID).
//     - more than one → return candidates for disambiguation.
//  3. Else nothing matched → empty resolution (caller emits a graceful
//     not-found result, NOT an error).
//
// `target` is the un-prefixed probe (local part of a repo:ID, or the raw
// entity_id). `repos` is the already repo-hint-filtered candidate set.
func resolveImpactTarget(repos []*LoadedRepo, target string) impactResolution {
	// Pass 1: exact ID match (the happy path — unchanged semantics).
	for _, r := range repos {
		if r == nil || r.Doc == nil {
			continue
		}
		if _, ok := r.getByID()[target]; ok {
			return impactResolution{repo: r, localID: target}
		}
	}

	// Pass 2: exact Name / QualifiedName match. Collect every match so we can
	// distinguish "unique by name" from "ambiguous".
	var byName []impactCandidate
	type hit struct {
		repo *LoadedRepo
		id   string
	}
	var hits []hit
	for _, r := range repos {
		if r == nil || r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Name == target || e.QualifiedName == target {
				hits = append(hits, hit{repo: r, id: e.ID})
				byName = append(byName, impactCandidate{
					EntityID:   prefixedID(r.Repo, e.ID),
					Name:       e.Name,
					Kind:       stripScopePrefix(e.Kind),
					Repo:       r.Repo,
					SourceFile: e.SourceFile,
				})
			}
		}
	}
	switch {
	case len(hits) == 1:
		return impactResolution{repo: hits[0].repo, localID: hits[0].id}
	case len(hits) > 1:
		return impactResolution{candidates: byName}
	}

	// Pass 2.5 (#5475): fuzzy fall-through. The exact id + exact name passes
	// above are intentionally strict, but agents routinely pass a reference that
	// grafel_find resolves and find_callers misses (e.g. a different-case name, a
	// partial / suffix of a qualified name like "svc.Target", or a substring).
	// grafel_find (handleSearchEntities) resolves these via a case-insensitive
	// substring match on Name/QualifiedName, ranking an exact case-insensitive
	// name first. Reuse that SAME logic here, but only as a fallback once the
	// strict passes have missed — so the exact-match happy path is byte-for-byte
	// unchanged and only genuine "would-have-errored" calls are rescued. We drop
	// de-noise entities (file/module containers, shadows, schema fields) so the
	// fuzzy candidate set stays signal, mirroring grafel_find's default ranking.
	fuzzy := fuzzyMatchEntities(repos, probeForFuzzy(target))
	switch len(fuzzy.hits) {
	case 0:
		// Nothing matched even fuzzily.
		return impactResolution{}
	case 1:
		return impactResolution{repo: fuzzy.hits[0].repo, localID: fuzzy.hits[0].id}
	default:
		// A fuzzy probe legitimately matches many entities; surface the same
		// disambiguation envelope the exact-name-ambiguous branch uses so the
		// agent can re-issue against a precise entity_id rather than getting a
		// bare error.
		return impactResolution{candidates: fuzzy.candidates}
	}
}

// probeForFuzzy normalises the fuzzy probe. An empty/whitespace probe must NOT
// match every entity (substring "" matches all), so it is returned unchanged
// and fuzzyMatchEntities short-circuits on it.
func probeForFuzzy(target string) string { return strings.TrimSpace(target) }

// fuzzyHit is a single fuzzy entity match.
type fuzzyHit struct {
	repo *LoadedRepo
	id   string
}

// fuzzyMatchResult carries the fuzzy matches plus pre-built disambiguation
// candidates (only meaningful when len(hits) > 1).
type fuzzyMatchResult struct {
	hits       []fuzzyHit
	candidates []impactCandidate
}

// fuzzyMatchEntities is the SHARED fuzzy name lookup, extracted from grafel_find
// (handleSearchEntities, #5475). It performs a case-insensitive substring match
// on Name / QualifiedName across the in-scope repos, dropping de-noise entities,
// and orders the matches so an exact case-insensitive Name match sorts first
// (identical to handleSearchEntities' sort). When exactly one entity matches the
// probe as a case-insensitive Name (and no other is exactly-named), that single
// entity is treated as the unique resolution even if the substring also matched
// other entities — this is the common "Caller" vs "CallerFactory" case.
//
// It is intentionally pure (no Server / request state) so resolveImpactTarget
// and handleSearchEntities can both consume it without duplicating the match
// rules that historically drift apart between the two surfaces.
func fuzzyMatchEntities(repos []*LoadedRepo, probe string) fuzzyMatchResult {
	if probe == "" {
		return fuzzyMatchResult{}
	}
	ql := strings.ToLower(probe)

	var exact []fuzzyHit
	var exactC []impactCandidate
	var sub []fuzzyHit
	var subC []impactCandidate
	for _, r := range repos {
		if r == nil || r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if isNoise(e) {
				continue
			}
			nameL := strings.ToLower(e.Name)
			qnL := strings.ToLower(e.QualifiedName)
			if !strings.Contains(nameL, ql) && !strings.Contains(qnL, ql) {
				continue
			}
			cand := impactCandidate{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
			}
			if nameL == ql || qnL == ql {
				exact = append(exact, fuzzyHit{repo: r, id: e.ID})
				exactC = append(exactC, cand)
				continue
			}
			sub = append(sub, fuzzyHit{repo: r, id: e.ID})
			subC = append(subC, cand)
		}
	}
	// A unique exact-(case-insensitive) name/qualified-name match wins outright,
	// even if substrings also matched — this mirrors grafel_find floating the
	// exact name to the top and is what makes "caller" resolve to "Caller".
	if len(exact) == 1 {
		return fuzzyMatchResult{hits: exact[:1], candidates: exactC[:1]}
	}
	if len(exact) > 1 {
		return fuzzyMatchResult{hits: exact, candidates: exactC}
	}
	return fuzzyMatchResult{hits: sub, candidates: subC}
}

// resolveEntityArg is the SHARED entity_id resolution path for the neighbor
// tools (grafel_find_callers / grafel_find_callees / grafel_neighbors). It
// converts the dominant ~35% error class — agents passing a bare name or a
// fully qualified name instead of the opaque "<repo>::<hash>" entity_id — into
// a successful resolution (#5314).
//
// Behaviour, layered on top of resolveImpactTarget:
//  1. Exact local-ID match in any in-scope repo → (repo, localID, nil). This is
//     the historical happy path; semantics are unchanged so there is no
//     regression for callers that already pass the hex id.
//  2. Unique Name / QualifiedName match → (repo, localID, nil). The name-based
//     call now resolves instead of erroring.
//  3. Ambiguous (same name on >1 entity) → (nil, "", disambiguation result):
//     a well-formed `ambiguous` envelope listing the candidate entity_ids so
//     the agent can re-issue against a precise id, NOT a bare error.
//  4. No match → (nil, "", nil): the caller emits its own verbatim
//     `entity not found` error (the genuine not-found case).
//
// `probe` is the un-prefixed entity argument (local part of "<repo>::<id>" or
// the raw entity_id). `repos` is the already repoHint-scoped candidate set, so
// group/cwd/repo scoping and cross_repo semantics are honoured by construction.
func resolveEntityArg(repos []*LoadedRepo, entityID, probe string) (*LoadedRepo, string, *mcpapi.CallToolResult) {
	res := resolveImpactTarget(repos, probe)
	if len(res.candidates) > 0 {
		return nil, "", jsonResult(map[string]any{
			"entity_id":  entityID,
			"resolved":   false,
			"ambiguous":  true,
			"candidates": res.candidates,
			"count":      0,
			"reason": fmt.Sprintf("entity_id %q is ambiguous: %d entities share this name. "+
				"Re-run with one of the precise entity_id values in `candidates`.",
				entityID, len(res.candidates)),
		})
	}
	if res.repo == nil {
		// Genuine not-found — caller emits its own verbatim error.
		return nil, "", nil
	}
	return res.repo, res.localID, nil
}

// handleImpactRadius returns all entities that would be affected if the given
// entity changes — a "change blast radius" analysis. Each result carries a
// risk_score [0,1] indicating how dangerous that particular affected entity
// is. Results are sorted by risk_score descending so agents can prioritise.
//
// Reliability (#3925): the dominant error class was input-driven — a missing
// entity, a name passed where an ID was expected, or an ambiguous symbol all
// returned a hard tool error. These are now converted into well-formed results:
// a graceful empty set with a `reason` for not-found, a `candidates` list for
// ambiguity, and an honest `truncated` marker for very high-degree nodes.
func (s *Server) handleImpactRadius(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	hops := argInt(req, "hops", 2)
	if hops < 1 {
		hops = 1
	}
	if hops > 6 {
		hops = 6
	}

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	probe := local
	if probe == "" {
		probe = entityID
	}
	resolution := resolveImpactTarget(repos, probe)
	// Ambiguous: entity_id matched multiple entities by name. Return candidates
	// for disambiguation instead of erroring (#3925).
	if len(resolution.candidates) > 0 {
		return jsonResult(map[string]any{
			"entity_id":  entityID,
			"resolved":   false,
			"ambiguous":  true,
			"candidates": resolution.candidates,
			"affected":   []any{},
			"count":      0,
			"reason": fmt.Sprintf("entity_id %q is ambiguous: %d entities share this name. "+
				"Re-run with one of the precise entity_id values in `candidates`.",
				entityID, len(resolution.candidates)),
		}), nil
	}
	// Not found: no entity matched by ID or name. Return a graceful empty
	// result with a reason instead of erroring (#3925).
	if resolution.repo == nil {
		return jsonResult(map[string]any{
			"entity_id": entityID,
			"resolved":  false,
			"affected":  []any{},
			"count":     0,
			"reason": fmt.Sprintf("no entity matched %q by ID or name in the loaded graph. "+
				"Use grafel_find or grafel_search_entities to locate the entity, "+
				"then pass its exact entity_id.", entityID),
		}), nil
	}

	type affected struct {
		EntityID   string  `json:"entity_id"`
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		Repo       string  `json:"repo"`
		SourceFile string  `json:"source_file,omitempty"`
		HopCount   int     `json:"hop_count"`
		RiskScore  float64 `json:"risk_score"`
		RiskReason string  `json:"risk_reason,omitempty"`
	}

	// Unique resolution: compute the impact set on the resolved repo + target.
	r := resolution.repo
	target := resolution.localID
	byID := r.getByID()

	// Precompute in-degree for risk scoring, broken down by caller kind.
	// namedCallerMap counts inbound edges whose source is a named operation
	// (Function, Method, Class, Component, Operation, etc.).
	// moduleCallerMap counts inbound edges whose source is a file/module
	// container node (SCOPE.Component, SCOPE.Module, File, Module, etc.).
	// totalDegreeMap is the simple sum of all inbound edges (used for scoring).
	namedCallerMap := map[string]int{}
	moduleCallerMap := map[string]int{}
	totalDegreeMap := map[string]int{}
	// inboundTestsMap counts inbound TESTS edges per entity (#3974). An entity
	// with ≥1 inbound TESTS edge has genuine test linkage and must not be
	// labelled "no test coverage" regardless of its test_coverage property.
	inboundTestsMap := map[string]int{}
	for i := range r.Doc.Relationships {
		rel := &r.Doc.Relationships[i]
		totalDegreeMap[rel.ToID]++
		if rel.Kind == "TESTS" {
			inboundTestsMap[rel.ToID]++
		}
		if src := byID[rel.FromID]; src != nil {
			if isModuleFileEntity(src) {
				moduleCallerMap[rel.ToID]++
			} else {
				namedCallerMap[rel.ToID]++
			}
		} else {
			// Source not in byID — treat as named to avoid under-counting.
			namedCallerMap[rel.ToID]++
		}
	}

	// Impact radius = entities that transitively depend on `target`.
	// We walk the INBOUND graph from target: callers of callers. The walk is
	// bounded by `hops` (≤6) and the visited set, so it terminates on any
	// graph; high-degree nodes are handled by the result cap below.
	adj := r.getAdjacency()
	visited := map[string]int{target: 0}
	frontier := []string{target}
	for d := 0; d < hops; d++ {
		next := []string{}
		for _, n := range frontier {
			for _, e := range adj.in[n] {
				if _, seen := visited[e.target]; seen {
					continue
				}
				visited[e.target] = d + 1
				next = append(next, e.target)
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}

	results := []affected{}
	for id, d := range visited {
		if id == target {
			continue
		}
		e := byID[id]
		if e == nil {
			continue
		}
		hasTests := inboundTestsMap[id] > 0
		isTestSpec := isTestSpecEntity(e)
		risk := impactRiskScore(e, totalDegreeMap[id], hasTests, isTestSpec)
		reason := buildRiskReason(e, namedCallerMap[id], moduleCallerMap[id], totalDegreeMap[id], hasTests, isTestSpec)
		results = append(results, affected{
			EntityID:   prefixedID(r.Repo, e.ID),
			Name:       e.Name,
			Kind:       stripScopePrefix(e.Kind),
			Repo:       r.Repo,
			SourceFile: e.SourceFile,
			HopCount:   d,
			RiskScore:  risk,
			RiskReason: reason,
		})
	}
	// Sort by risk descending, then hop ascending, then name.
	sort.Slice(results, func(i, j int) bool {
		if results[i].RiskScore != results[j].RiskScore {
			return results[i].RiskScore > results[j].RiskScore
		}
		if results[i].HopCount != results[j].HopCount {
			return results[i].HopCount < results[j].HopCount
		}
		return results[i].Name < results[j].Name
	})

	// Bound the payload for pathological high-degree nodes. We keep the
	// top-ranked slice (highest risk first) and emit an honest truncation
	// marker rather than returning an unbounded result (#3925).
	totalAffected := len(results)
	truncated := false
	if len(results) > impactRadiusMaxResults {
		results = results[:impactRadiusMaxResults]
		truncated = true
	}

	root := byID[target]
	rootName := target
	if root != nil {
		rootName = root.Name
	}
	out := map[string]any{
		"entity_id":   prefixedID(r.Repo, target),
		"entity_name": rootName,
		"repo":        r.Repo,
		"hops":        hops,
		"resolved":    true,
		"affected":    results,
		"count":       len(results),
		"tip":         "risk_score 0.0–1.0: higher means the affected entity is more sensitive to breakage from changes in the root entity.",
	}
	if truncated {
		out["truncated"] = true
		out["total_affected"] = totalAffected
		out["truncation_note"] = fmt.Sprintf(
			"high-degree node: %d entities are affected; returning the top %d by risk_score. "+
				"Narrow with a smaller `hops` or inspect specific neighbors.",
			totalAffected, impactRadiusMaxResults)
	}
	return jsonResult(out), nil
}

// buildRiskReason produces a short human-readable reason string for the risk score.
// namedCallers is the count of inbound edges from named operation entities (Function,
// Method, Class, Component, Operation, etc.). moduleNodes is the count from file/module
// container entities (SCOPE.Component, SCOPE.Module, File, Module, etc.). total is
// their sum. When the two counts differ we emit a qualified breakdown so consumers
// understand how much of the in-degree is actual named callers vs structural noise.
// hasInboundTests reports whether the entity has ≥1 inbound TESTS edge, and
// isTestEntity reports whether the entity is itself test/spec code (#3974). The
// "no test coverage" label is suppressed when the entity has genuine test
// linkage (positive coverage property OR an inbound TESTS edge) and is never
// emitted for a test-spec entity, which is not production code under test.
func buildRiskReason(e *graph.Entity, namedCallers, moduleNodes, total int, hasInboundTests, isTestEntity bool) string {
	parts := []string{}
	if total > 5 {
		if moduleNodes == 0 {
			// All callers are named — simple, unambiguous message.
			parts = append(parts, fmt.Sprintf("high in-degree (%d named callers)", namedCallers))
		} else {
			// Mixed: qualify the breakdown so consumers are not misled.
			parts = append(parts, fmt.Sprintf("high in-degree (%d named callers, %d module/file nodes; total %d inbound edges)", namedCallers, moduleNodes, total))
		}
	}
	k := strings.ToLower(e.Kind)
	if strings.Contains(k, "http_endpoint") || strings.Contains(k, "endpoint") {
		parts = append(parts, "API boundary")
	} else if strings.Contains(k, "topic") || strings.Contains(k, "queue") {
		parts = append(parts, "message channel")
	}
	// Only flag missing test coverage for production entities with no genuine
	// test linkage. An inbound TESTS edge or a positive coverage property means
	// the entity IS tested; a test-spec entity is not a unit to flag (#3974).
	if !isTestEntity && !entityHasTestCoverage(e, hasInboundTests) {
		parts = append(parts, "no test coverage")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// ---------------------------------------------------------------------------
// grafel_subgraph (unified, #1754)
// ---------------------------------------------------------------------------

// subgraphRawMaxNodes bounds the format=raw node expansion (#3924). A
// high-degree hub reached at depth>1 can fan out to thousands of nodes,
// exploding both the BFS and the raw-JSON serialization past token limits.
// The cap is generous so ordinary subgraphs are returned complete; only the
// pathological tail is bounded, and the bound is reported honestly via the
// "truncated" flag + "truncation_note". Callers may raise it via max_nodes.
const subgraphRawMaxNodes = 1500

// handleSubgraph is the unified handler for grafel_subgraph.
// format="raw"      → JSON graph (nodes + edges), identical output to old get_subgraph.
// format="markdown" → LLM-friendly Markdown summary, identical output to old summarize_subgraph.
// Both legacy tools delegate here as deprecated trampolines.
func (s *Server) handleSubgraph(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	format := argString(req, "format", "raw")
	switch format {
	case "raw":
		return s.subgraphRaw(entityID, req)
	case "markdown":
		return s.subgraphMarkdown(entityID, req)
	default:
		return mcpapi.NewToolResultError("format must be \"raw\" or \"markdown\"; got: " + format), nil
	}
}

// subgraphRaw returns all nodes and edges within depth hops of entity_id.
// Extracted from the former handleGetSubgraph (dashboard_tools.go).
func (s *Server) subgraphRaw(entityID string, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	// maxNodes bounds the pathological high-degree tail (#3924): a hub reached
	// at depth>1 can fan out to thousands of nodes, blowing up both the BFS and
	// the raw-JSON serialization (the rewrite agent's "depth 2 can exceed token
	// limits"). The cap is generous so the common case is never touched; only
	// the explosive tail is bounded, and truncation is reported honestly. A
	// caller may raise it via max_nodes for an explicit deeper pull.
	maxNodes := argInt(req, "max_nodes", subgraphRawMaxNodes)
	if maxNodes < 1 {
		maxNodes = subgraphRawMaxNodes
	}
	repos := reposToConsider(lg, nil)
	repoHint, local := splitPrefixed(entityID)
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
			target = entityID
		}
		byID := r.getByID()
		if _, ok := byID[target]; !ok {
			continue
		}
		adj := r.getAdjacency()
		visited, nodesTruncated := bfsBounded(adj, target, depth, nil, maxNodes)
		byID2 := r.getByID()
		type nodeOut struct {
			EntityID   string `json:"entity_id"`
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			SourceFile string `json:"source_file,omitempty"`
			StartLine  int    `json:"start_line,omitempty"`
			Depth      int    `json:"depth"`
		}
		type edgeOut struct {
			FromID string `json:"from_id"`
			ToID   string `json:"to_id"`
			Kind   string `json:"kind"`
		}
		var nodes []nodeOut
		nodeSet := map[string]bool{}
		for id, d := range visited {
			if e := byID2[id]; e != nil {
				nodes = append(nodes, nodeOut{
					EntityID:   prefixedID(r.Repo, e.ID),
					Name:       e.Name,
					Kind:       e.Kind,
					SourceFile: e.SourceFile,
					StartLine:  e.StartLine,
					Depth:      d,
				})
			}
			nodeSet[id] = true
		}
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].Depth != nodes[j].Depth {
				return nodes[i].Depth < nodes[j].Depth
			}
			return nodes[i].Name < nodes[j].Name
		})
		// Collect edges via the adjacency index rather than scanning the full
		// r.Doc.Relationships table. The old scan was O(total repo
		// relationships) on every call — a fixed cost that dominated the p95
		// tail. Walking adj.out of the (bounded) visited set is
		// O(edges incident to the subgraph). adj.out preserves direction
		// (from→to), so the from/to/kind tuples the rewrite agent's
		// format=raw consumer relies on are emitted unchanged. (#3924)
		var edges []edgeOut
		seen := map[string]bool{}
		for from := range nodeSet {
			for _, e := range adj.Outgoing(from) {
				if !nodeSet[e.target] {
					continue
				}
				key := from + ">" + e.target + ":" + e.kind
				if seen[key] {
					continue
				}
				seen[key] = true
				edges = append(edges, edgeOut{
					FromID: prefixedID(r.Repo, from),
					ToID:   prefixedID(r.Repo, e.target),
					Kind:   e.kind,
				})
			}
		}
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].FromID != edges[j].FromID {
				return edges[i].FromID < edges[j].FromID
			}
			if edges[i].ToID != edges[j].ToID {
				return edges[i].ToID < edges[j].ToID
			}
			return edges[i].Kind < edges[j].Kind
		})
		out := map[string]any{
			"root":       prefixedID(r.Repo, target),
			"repo":       r.Repo,
			"depth":      depth,
			"nodes":      nodes,
			"edges":      edges,
			"node_count": len(nodes),
			"edge_count": len(edges),
			"truncated":  nodesTruncated,
		}
		if nodesTruncated {
			out["truncation_note"] = fmt.Sprintf(
				"node expansion capped at max_nodes=%d to bound a high-degree subgraph; "+
					"some nodes beyond the cap (and their edges) are omitted — narrow with a smaller "+
					"depth, or pass a larger max_nodes for an explicit deeper pull",
				maxNodes)
		}
		return jsonResult(out), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// subgraphMarkdown returns an LLM-friendly Markdown summary of entity_id's
// local neighbourhood. Extracted from the former handleSummarizeSubgraph.
func (s *Server) subgraphMarkdown(entityID string, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 4 {
		depth = 4
	}

	repoHint, local := splitPrefixed(entityID)
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
			target = entityID
		}
		byID := r.getByID()
		root, ok := byID[target]
		if !ok {
			continue
		}

		adj := r.getAdjacency()

		// Gather inbound callers (depth hops).
		inVisited := map[string]int{}
		inFront := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range inFront {
				for _, e := range adj.in[n] {
					if _, seen := inVisited[e.target]; !seen {
						inVisited[e.target] = d + 1
						next = append(next, e.target)
					}
				}
			}
			inFront = next
		}

		// Gather outbound callees (depth hops).
		outVisited := map[string]int{}
		outFront := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range outFront {
				for _, e := range adj.out[n] {
					if _, seen := outVisited[e.target]; !seen {
						outVisited[e.target] = d + 1
						next = append(next, e.target)
					}
				}
			}
			outFront = next
		}

		// Build callers list (sorted by hop then name).
		type neighbor struct {
			name string
			kind string
			file string
			hop  int
		}
		var callers []neighbor
		for id, d := range inVisited {
			if e := byID[id]; e != nil {
				callers = append(callers, neighbor{name: e.Name, kind: stripScopePrefix(e.Kind), file: e.SourceFile, hop: d})
			}
		}
		sort.Slice(callers, func(i, j int) bool {
			if callers[i].hop != callers[j].hop {
				return callers[i].hop < callers[j].hop
			}
			return callers[i].name < callers[j].name
		})

		var callees []neighbor
		for id, d := range outVisited {
			if e := byID[id]; e != nil {
				callees = append(callees, neighbor{name: e.Name, kind: stripScopePrefix(e.Kind), file: e.SourceFile, hop: d})
			}
		}
		sort.Slice(callees, func(i, j int) bool {
			if callees[i].hop != callees[j].hop {
				return callees[i].hop < callees[j].hop
			}
			return callees[i].name < callees[j].name
		})

		// Render markdown.
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s\n\n", root.Name))
		b.WriteString(fmt.Sprintf("**Kind:** %s  \n", stripScopePrefix(root.Kind)))
		b.WriteString(fmt.Sprintf("**Repo:** %s  \n", r.Repo))
		if root.SourceFile != "" {
			b.WriteString(fmt.Sprintf("**File:** `%s`", root.SourceFile))
			if root.StartLine > 0 {
				b.WriteString(fmt.Sprintf(":%d", root.StartLine))
			}
			b.WriteString("  \n")
		}
		if root.QualifiedName != "" && root.QualifiedName != root.Name {
			b.WriteString(fmt.Sprintf("**Qualified name:** `%s`  \n", root.QualifiedName))
		}
		b.WriteString("\n")

		if len(callers) > 0 {
			b.WriteString(fmt.Sprintf("## Called by (%d entities within %d hop(s))\n\n", len(callers), depth))
			for _, c := range callers {
				b.WriteString(fmt.Sprintf("- **%s** (%s)", c.name, c.kind))
				if c.file != "" {
					b.WriteString(fmt.Sprintf(" — `%s`", c.file))
				}
				if c.hop > 1 {
					b.WriteString(fmt.Sprintf(" _(hop %d)_", c.hop))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		} else {
			b.WriteString("## Called by\n\n_No callers within the graph (entry point or unreferenced)._\n\n")
		}

		if len(callees) > 0 {
			b.WriteString(fmt.Sprintf("## Calls (%d entities within %d hop(s))\n\n", len(callees), depth))
			for _, c := range callees {
				b.WriteString(fmt.Sprintf("- **%s** (%s)", c.name, c.kind))
				if c.file != "" {
					b.WriteString(fmt.Sprintf(" — `%s`", c.file))
				}
				if c.hop > 1 {
					b.WriteString(fmt.Sprintf(" _(hop %d)_", c.hop))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		} else {
			b.WriteString("## Calls\n\n_No callees within the graph (leaf node or all edges unresolved)._\n\n")
		}

		return mcpapi.NewToolResultText(b.String()), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// ---------------------------------------------------------------------------
// grafel_find_dead_code
// ---------------------------------------------------------------------------

// stdlibKindPrefixes is the set of entity kind prefixes that represent
// stdlib/external references — we skip these when counting non-stdlib edges.
var stdlibKindPrefixes = []string{
	"stdlib", "external", "builtin", "vendor", "third_party", "foreign",
}

// isStdlibEntity returns true if the entity's kind or properties indicate it is
// a stdlib/external symbol (not project code).
func isStdlibEntity(e *graph.Entity) bool {
	k := strings.ToLower(e.Kind)
	for _, p := range stdlibKindPrefixes {
		if strings.HasPrefix(k, p) || strings.Contains(k, p) {
			return true
		}
	}
	if e.Properties["is_external"] == "true" ||
		e.Properties["is_stdlib"] == "true" ||
		e.Properties["external"] == "true" {
		return true
	}
	return false
}

// isModuleFileEntity returns true when an entity is a file or module container
// node rather than a named callable operation. These nodes (SCOPE.Component,
// SCOPE.Module, File, Module, Package, Namespace, Directory) appear as inbound
// edge sources in the graph but do not represent actual callers of a function;
// they are structural containers that inflate the raw in-degree count.
func isModuleFileEntity(e *graph.Entity) bool {
	k := e.Kind
	// SCOPE.* prefix always indicates a container/file node.
	if strings.HasPrefix(k, "SCOPE.") {
		return true
	}
	lower := strings.ToLower(k)
	// Bare kind names that represent container/file concepts.
	switch lower {
	case "file", "module", "package", "namespace", "directory", "folder":
		return true
	}
	return false
}

// inboundRefKinds are the edge kinds that count as a real reference to an
// entity for dead-code purposes. An entity that is the target of any of these
// is "used" and cannot be dead. CONTAINS is intentionally excluded — every
// entity is contained by its module, so it carries no usage signal.
var inboundRefKinds = map[string]bool{
	"CALLS":           true,
	"REFERENCES":      true,
	"IMPORTS":         true, // #2039: file/module → symbol or re-exported module
	"TESTS":           true,
	"ROUTES_TO":       true,
	"IMPLEMENTS":      true,
	"HANDLES":         true,
	"ENTRY_POINT_OF":  true,
	"RENDERS":         true,
	"FETCHES":         true,
	"STEP_IN_PROCESS": true,
	"PRODUCES":        true,
	"CONSUMES":        true,
}

// inboundNeighborStructuralKinds are edge kinds that carry NO predecessor /
// caller signal and must stay excluded from find_callers regardless of the
// broadened semantic-edge acceptance (#4242). CONTAINS is the canonical case:
// every entity is contained by its module/file, so a CONTAINS predecessor is
// the structural parent, not a caller (preserves #1915 / the
// TestFindCallers_ExcludesContainsEdges contract). DECLARES/DEFINES are the
// same structural-ownership shape.
var inboundNeighborStructuralKinds = map[string]bool{
	"CONTAINS": true,
	"DECLARES": true,
	"DEFINES":  true,
}

// isInboundNeighborKind reports whether an inbound edge of the given kind
// represents a real predecessor (caller / in-neighbor) of an entity for the
// grafel_neighbors(direction=in) / find_callers walk.
//
// #4242: the pre-fix walk used the inboundRefKinds allow-list only, which
// covered CALLS/REFERENCES/IMPORTS/TESTS/… but DROPPED every non-CALLS semantic
// predecessor — INJECTED_INTO, THROWS, CATCHES, JOINS_COLLECTION, DEPENDS_ON,
// EXTENDS, INHERITS, … — even though the forward (callees/out) side already
// surfaced all of them. That asymmetry made the rewrite oracle conclude those
// edges were "unmodeled" when they exist on the graph. We now accept any edge
// kind that is a known reference kind OR a projected semantic kind OR a
// type/dependency relation, excluding only the pure-structural ownership edges
// (CONTAINS/DECLARES/DEFINES). Mirrors the all-kinds out side.
func isInboundNeighborKind(kind string) bool {
	if inboundNeighborStructuralKinds[strings.ToUpper(kind)] {
		return false
	}
	if inboundRefKinds[kind] {
		return true
	}
	if isSemanticEdgeKind(kind) {
		return true
	}
	switch strings.ToUpper(kind) {
	case "EXTENDS", "INHERITS", "DEPENDS_ON", "IMPLEMENTS":
		return true
	}
	return false
}

// deadMarkerRe matches conventional dead-code / deprecation name markers. A
// public symbol that is structurally orphaned AND carries one of these markers
// is high-confidence dead code (vs. a legitimate-but-currently-unused public
// API export, which we deliberately do NOT flag). Matching is case-insensitive
// against the symbol's leaf name.
var deadMarkerRe = regexp.MustCompile(`(?i)(legacy|deprecated|obsolete|dead|unused|_old$|^old[_A-Z])`)

// nonCodeLanguages are languages whose "operations" are config/markup
// directives, not real callable code (Dockerfile CMD/COPY, SQL DDL, etc.).
var nonCodeLanguages = map[string]bool{
	"dockerfile": true,
	"markdown":   true,
	"sql":        true,
	"hcl":        true,
	"yaml":       true,
	"toml":       true,
	"json":       true,
}

// routeNameRe matches synthetic HTTP-endpoint operation names like
// "GET /products/{sku}" or "http:POST:/charges". These are route handlers
// reachable via the web framework, never dead code.
var routeNameRe = regexp.MustCompile(`(?i)^(get|post|put|delete|patch|head|options|http)[\s:]`)

// frameworkLifecycleNames are method names that are invoked by a framework,
// runtime, or DI container rather than by an explicit in-graph call edge.
// Flagging these would be a false positive (they are entry points / hooks).
var frameworkLifecycleNames = map[string]bool{
	"main": true, "init": true, "__init__": true, "setup": true, "run": true,
	"bootstrap": true, "configure": true, "mount": true, "application": true,
	"project": true, "start": true, "startup": true, "shutdown": true,
	"connect": true, "render": true, "health": true, "show": true,
	"controller": true, "live_view": true, "channel": true, "socket": true,
	"up": true, "down": true, "use": true, "join": true, "leave": true,
	"new": true, "create": true, "build": true, "default": true,
	"config_change": true,
}

// isConstructorName reports whether name looks like a constructor (Java/C#/Go
// style "ClassName.ClassName") — instantiated reflectively / by the framework.
func isConstructorName(name string) bool {
	if !strings.Contains(name, ".") {
		return false
	}
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return false
	}
	return parts[len(parts)-1] == parts[len(parts)-2]
}

// isFrameworkOrHandler reports whether an operation is a framework-managed
// entry point (route handler, lifecycle hook, event listener, constructor)
// that is reachable without an explicit in-graph call edge.
func isFrameworkOrHandler(e *graph.Entity) bool {
	name := e.Name
	leaf := name
	if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
		leaf = leaf[i+1:]
	}
	low := strings.ToLower(leaf)
	if routeNameRe.MatchString(name) {
		return true
	}
	if frameworkLifecycleNames[low] {
		return true
	}
	if isConstructorName(name) {
		return true
	}
	// Event-listener / framework-callback naming conventions:
	// onOrderPlaced, handle_in, handle_event, handle_info, handleMessage…
	if strings.HasPrefix(low, "on") && len(low) > 2 && (low[2] >= 'a' && low[2] <= 'z' || low[2] == '_') {
		// crude: onX with capital next char in original
		if len(leaf) > 2 && leaf[2] >= 'A' && leaf[2] <= 'Z' {
			return true
		}
	}
	if strings.HasPrefix(low, "handle") {
		return true
	}
	// HTTP-endpoint kinds are routes, never dead.
	if isHTTPEndpointKind(e.Kind) {
		return true
	}
	return false
}

// buildImportedNameSet collects every symbol name that appears as an
// `imported_name` (or `local_name`) on an IMPORTS edge across all repos in the
// group. A library export whose name is imported somewhere is consumed and so
// is NOT dead, even with zero in-repo call edges.
func buildImportedNameSet(repos []*LoadedRepo) map[string]bool {
	set := map[string]bool{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind != "IMPORTS" {
				continue
			}
			if n := rel.Properties["imported_name"]; n != "" {
				set[n] = true
			}
			if n := rel.Properties["local_name"]; n != "" {
				set[n] = true
			}
		}
	}
	return set
}

// isExternallyConsumed reports whether an entity is referenced cross-repo via a
// symbol import. We check the entity's own name, its leaf name, every dotted
// segment of its qualified name, and its source-file module basename — because
// Python/JS consumers may import the module (`from pkg import vault`) and then
// call `vault.read()`, so the module name being imported keeps `read` alive.
func isExternallyConsumed(e *graph.Entity, imported map[string]bool) bool {
	if imported[e.Name] {
		return true
	}
	if i := strings.LastIndexByte(e.Name, '.'); i >= 0 {
		if imported[e.Name[i+1:]] {
			return true
		}
		if imported[e.Name[:i]] {
			return true
		}
	}
	if e.QualifiedName != "" {
		for _, seg := range strings.Split(e.QualifiedName, ".") {
			if imported[seg] {
				return true
			}
		}
	}
	if e.SourceFile != "" {
		base := e.SourceFile
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		if i := strings.LastIndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if imported[base] {
			return true
		}
	}
	return false
}

// isOperationKind reports whether an entity is a callable operation
// (function/method) — the only kind dead-code analysis applies to.
func isOperationKind(e *graph.Entity) bool {
	k := strings.ToLower(stripScopePrefix(e.Kind))
	return k == "operation" || k == "function" || k == "method"
}

// handleFindDeadCode returns dead-code candidates. Two classes are reported:
//
//  1. Fully isolated entities — zero inbound AND zero outbound edges to other
//     project entities (no relationship at all beyond CONTAINS). These are
//     unambiguously orphaned regardless of visibility.
//
//  2. Unreferenced public operations — an operation (function/method) with zero
//     inbound reference edges (CALLS/REFERENCES/TESTS/ROUTES_TO/…), that is not
//     imported anywhere cross-repo, is not a route handler / framework
//     lifecycle hook / constructor / entry point, and carries a conventional
//     dead-code marker in its name (legacy/deprecated/obsolete/dead/unused).
//     The marker requirement is what separates genuine dead code from a
//     legitimate-but-currently-unused public API export (e.g. a shared library
//     helper that other repos may call) — we deliberately do NOT flag the
//     latter to keep precision high.
//
//  3. test_only_referenced (#3657) — a production operation whose ONLY inbound
//     reference edges originate in test files (or are TESTS edges). It is
//     reachable in the full graph (so the classic caller-count check thinks it
//     is live) yet UNREACHABLE in a production-only pass that drops test-source
//     callers. This is the recurring "unwired code" class — orphaned enrichers,
//     test-only extractors, framework patterns wired only from *_test.go — that
//     a plain reachability BFS misses because it counts test callers as
//     references. The same honest exclusions as class 2 apply (route handlers,
//     framework lifecycle hooks, constructors, cross-repo imports) so we do not
//     false-positive on reflectively-invoked or externally-consumed symbols.
//
// Supports optional filters: kind_filter, repo_filter, limit.
func (s *Server) handleFindDeadCode(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	// imported-name set is built across the WHOLE group so cross-repo library
	// consumption is visible even when repo_filter narrows the report.
	allRepos := reposToConsider(lg, nil)
	imported := buildImportedNameSet(allRepos)

	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	kindFilter := strings.ToLower(argString(req, "kind_filter", ""))
	limit := argInt(req, "limit", 100)
	minConfidence := argMinConfidence(req) // #2769 Phase 1C

	type item struct {
		EntityID   string  `json:"entity_id"`
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		Repo       string  `json:"repo"`
		SourceFile string  `json:"source_file,omitempty"`
		StartLine  int     `json:"start_line,omitempty"`
		Reason     string  `json:"reason"`
		Confidence float64 `json:"confidence"`
	}

	out := []item{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}

		// Build set of entities that are project (non-stdlib) code, plus the
		// per-entity test-file predicate (#3657). testEntity[id] is true when
		// the entity's source file matches a recognised test-file convention —
		// used to drop test-file callers from the production-only reachability
		// pass.
		projectEntities := map[string]bool{}
		testEntity := map[string]bool{}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isStdlibEntity(e) {
				projectEntities[e.ID] = true
			}
			if isTestFileMCP(e.SourceFile) {
				testEntity[e.ID] = true
			}
		}

		// Count inbound *reference* edges (the usage signal) per entity. An
		// operation with any inbound CALLS/REFERENCES/TESTS/ROUTES_TO/etc. is
		// live. CONTAINS is excluded (every entity is contained by its module,
		// so it carries no usage signal). Cross-repo references are handled
		// separately via the imported-name set.
		//
		// #3657: we keep TWO counts. inRef is the full count (any caller,
		// including tests) — an entity with inRef>0 is NOT classic dead code.
		// inRefProd drops edges whose source is a test entity (FromID in a test
		// file) AND drops the TESTS edge kind itself (a test→target relation is
		// by definition a test caller). An operation that is reachable in the
		// full graph (inRef>0) but UNREACHABLE production-only (inRefProd==0) is
		// production-dead-but-test-covered: the `test_only_referenced` class.
		inRef := map[string]int{}
		inRefProd := map[string]int{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if !projectEntities[rel.FromID] || !projectEntities[rel.ToID] {
				continue
			}
			if rel.Kind == "CONTAINS" {
				continue
			}
			if inboundRefKinds[rel.Kind] {
				inRef[rel.ToID]++
				// Production-only edge: source is not a test entity, and the
				// edge is not a TESTS relation. These are the edges that keep a
				// symbol alive in production.
				if rel.Kind != "TESTS" && !testEntity[rel.FromID] {
					inRefProd[rel.ToID]++
				}
			}
		}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if isStdlibEntity(e) {
				continue
			}
			if !matchesKindFilter(e, kindFilter) {
				continue
			}
			// #2769 Phase 1C: drop entities below the caller's confidence floor.
			if !entityPassesConfidence(e, minConfidence) {
				continue
			}

			// Dead-code analysis applies ONLY to callable operations
			// (functions/methods). Schemas, columns, topics, endpoints,
			// config directives, etc. are skipped — an "isolated" data node
			// is an extraction artefact, not dead code, and flagging them
			// destroys precision on real per-repo graphs (where cross-repo
			// usage lives in the group links file, not in per-repo edges).
			if !isOperationKind(e) {
				continue
			}
			// Exclude non-code "operation" entities (Dockerfile CMD, SQL DDL).
			if nonCodeLanguages[strings.ToLower(e.Language)] {
				continue
			}
			// The TARGET being a test entity is never dead production code — a
			// test helper called only by other tests is wired-as-intended. We
			// only flag PRODUCTION symbols. Both the legacy class and the new
			// test_only_referenced class require the target to live in
			// production code.
			if testEntity[e.ID] || strings.Contains(strings.ToLower(e.SourceFile), "test") {
				continue
			}
			// Route handlers, framework lifecycle hooks, event listeners, and
			// constructors are reachable without an explicit call edge. These
			// are honest exclusions shared by BOTH classes — a symbol invoked
			// reflectively / by the framework / as an entry point is not dead
			// even with zero in-graph production callers.
			if isFrameworkOrHandler(e) {
				continue
			}
			// Imported by another repo → live public API surface.
			if isExternallyConsumed(e, imported) {
				continue
			}

			leaf := e.Name
			if j := strings.LastIndexByte(leaf, '.'); j >= 0 {
				leaf = leaf[j+1:]
			}

			switch {
			case inRef[e.ID] > 0:
				// Has callers in the full graph. Classic dead-code detection
				// treats this as live. BUT #3657: if EVERY caller is a test
				// (inRefProd == 0), the symbol is production-unreachable while
				// test-covered — the recurring "unwired code" class (orphaned
				// enrichers, test-only extractors, Java patterns wired only from
				// *_test.go). Flag it as test_only_referenced. Otherwise it has
				// at least one real production caller and is genuinely live.
				if inRefProd[e.ID] == 0 {
					out = append(out, item{
						EntityID:   prefixedID(r.Repo, e.ID),
						Name:       e.Name,
						Kind:       stripScopePrefix(e.Kind),
						Repo:       r.Repo,
						SourceFile: e.SourceFile,
						StartLine:  e.StartLine,
						Reason:     "test_only_referenced: reachable only from test files (0 production callers, not imported, not a route/handler/entrypoint) — production-dead but test-covered",
						Confidence: 0.8,
					})
				}
				// else: real production caller → live, not flagged.
			default:
				// Zero callers of any kind. Flagged as dead code only when it
				// carries a conventional dead-code marker (legacy/deprecated/
				// obsolete/dead/unused/old) in its name. The marker is the
				// precision gate that separates genuine dead code from a
				// legitimate-but-currently-unused public API export or a symbol
				// reachable only via reflection/config. Without it, a merely
				// zero-caller operation (extremely common on real per-repo
				// graphs, where cross-repo usage lives in the group links file
				// rather than in per-repo edges) is NOT flagged, keeping false
				// positives near zero.
				if !deadMarkerRe.MatchString(leaf) {
					continue
				}
				out = append(out, item{
					EntityID:   prefixedID(r.Repo, e.ID),
					Name:       e.Name,
					Kind:       stripScopePrefix(e.Kind),
					Repo:       r.Repo,
					SourceFile: e.SourceFile,
					StartLine:  e.StartLine,
					Reason:     "unreferenced operation with dead-code marker (0 callers, not imported, not a route/handler/entrypoint)",
					Confidence: 0.85,
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Name < out[j].Name
	})

	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"dead_code": out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
		"note":      "Dead code candidates. Class 1 (confidence 0.6): isolated entities with no edges — may be an extraction gap. Class 2 (confidence 0.85): unreferenced public operations carrying a dead-code marker (reason='unreferenced operation…'). Class 3 (confidence 0.8): test_only_referenced — operations reachable ONLY from test files (0 production callers; reason='test_only_referenced…'). These are production-dead but test-covered: the recurring unwired-code class (orphaned helpers, test-only extractors/enrichers) the classic caller-count BFS misses because it counts test callers as references. Verify before deletion — some entry points are invoked via reflection or config.",
	}), nil
}

// ---------------------------------------------------------------------------
// #2665 — route-literal resolution for grafel_find_callers
// ---------------------------------------------------------------------------

// entityExistsAnywhere reports whether the given id matches an entity ID or
// Name across any repo in the group. Used by findCallersStructured to decide
// whether to attempt route-literal resolution.
func entityExistsAnywhere(lg *LoadedGroup, id string) bool {
	if lg == nil {
		return false
	}
	_, local := splitPrefixed(id)
	probe := id
	if local != "" {
		probe = local
	}
	for _, r := range lg.Repos {
		if r == nil || r.Doc == nil {
			continue
		}
		if _, ok := r.getByID()[probe]; ok {
			return true
		}
		for i := range r.Doc.Entities {
			if r.Doc.Entities[i].Name == probe {
				return true
			}
		}
	}
	return false
}

// tryFindCallersByRoute resolves a route literal (e.g. "/users/[id]") by
// walking NAVIGATES_TO edges in reverse and returning their source entities
// as callers. Returns nil if no matching edges exist so the caller can fall
// through to the standard not-found path.
//
// Match rules (in order of preference):
//  1. rel.ToID == "route:" + literal
//  2. Properties["route"] == literal
//  3. Properties["route"] case-insensitive equals literal (final fallback)
//
// Each returned caller carries file:line + the NAVIGATES_TO params_keys so
// the agent can immediately answer "which call sites pass param X?".
func (s *Server) tryFindCallersByRoute(req mcpapi.CallToolRequest, lg *LoadedGroup, routeLit string) map[string]any {
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	type routeCaller struct {
		EntityID   string `json:"id"`
		Name       string `json:"name"`
		Repo       string `json:"repo,omitempty"`
		SourceFile string `json:"file,omitempty"`
		StartLine  int    `json:"line,omitempty"`
		Route      string `json:"route,omitempty"`
		ParamsKeys string `json:"params_keys,omitempty"`
		HopCount   int    `json:"hop_count"`
	}
	var callers []routeCaller
	wantToID := "route:" + routeLit
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		byID := r.getByID()
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind != "NAVIGATES_TO" {
				continue
			}
			match := rel.ToID == wantToID
			if !match && rel.Properties != nil {
				if rel.Properties["route"] == routeLit {
					match = true
				} else if strings.EqualFold(rel.Properties["route"], routeLit) {
					match = true
				}
			}
			if !match {
				continue
			}
			line := 0
			route := routeLit
			paramsKeys := ""
			if rel.Properties != nil {
				if ls := rel.Properties["line"]; ls != "" {
					if n, perr := strconv.Atoi(ls); perr == nil {
						line = n
					}
				}
				if rt := rel.Properties["route"]; rt != "" {
					route = rt
				}
				paramsKeys = rel.Properties["params_keys"]
			}
			rc := routeCaller{
				EntityID:   prefixedID(r.Repo, rel.FromID),
				Repo:       r.Repo,
				StartLine:  line,
				Route:      route,
				ParamsKeys: paramsKeys,
				HopCount:   1,
			}
			if e := byID[rel.FromID]; e != nil {
				rc.Name = e.Name
				rc.SourceFile = e.SourceFile
			}
			callers = append(callers, rc)
		}
	}
	if len(callers) == 0 {
		return nil
	}
	sort.Slice(callers, func(i, j int) bool {
		if callers[i].Repo != callers[j].Repo {
			return callers[i].Repo < callers[j].Repo
		}
		if callers[i].SourceFile != callers[j].SourceFile {
			return callers[i].SourceFile < callers[j].SourceFile
		}
		return callers[i].StartLine < callers[j].StartLine
	})
	return map[string]any{
		"entity_id":       routeLit,
		"entity_name":     routeLit,
		"resolved_as":     "navigation_route",
		"navigation_edge": "NAVIGATES_TO",
		"depth":           1,
		"callers":         callers,
		"count":           len(callers),
		"note":            "Route literal resolved via NAVIGATES_TO incoming edges (#2665). params_keys (JSON array) on each caller indicates which navigation params are statically passed.",
	}
}
