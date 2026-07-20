// Process-flow BFS pass (#724).
//
// Walks the CALLS graph forward from heuristically-detected entry points
// and emits Process entities + STEP_IN_PROCESS / ENTRY_POINT_OF edges.
// Each Process is a linearized call chain. The pass is language-agnostic —
// it consumes the CALLS edges that the per-language extractors already
// produce and never inspects source code directly.
//
// Algorithm (per ADR-0018 / issue #724):
//  1. Score every Function/Method/Operation/Component candidate by
//     fan-out, name pattern, exported flag, framework signal, and HTTP
//     boundary signal.
//  2. Keep the top entry points (capped at MaxEntryPoints).
//  3. For each entry point, run forward BFS over CALLS edges with depth
//     bounded by MaxDepth (≤10) and branching bounded by BranchingFactor
//     (≤4). Each traversal stops on a leaf (no outgoing CALLS) or when
//     bounds are hit.
//  4. Dedupe traces by (entry_id, terminal_id) — keep the longest chain
//     and drop strict prefixes of longer chains.
//  5. Emit one Process entity per surviving trace plus STEP_IN_PROCESS
//     edges (step_index ordered) and ENTRY_POINT_OF edges from the
//     entry function to the Process.
//
// Cross-stack detection (#754): a Process is marked cross_stack=true only
// when its chain traverses a real cross-repo boundary, signalled by one of:
//
//   - At least one step in the chain is reached via a FETCHES edge (the
//     consumer-side HTTP fetch primitive: caller function → synthetic
//     consumer http_endpoint). FETCHES is emitted by the http_endpoint
//     resolver (`http_endpoint_resolve.go`) for any consumer synthetic
//     whose `source_caller` resolves in-file, and directly by the
//     per-language wave-1 client extractors (#721+).
//   - The chain contains a CONSUMER-side synthetic http_endpoint
//     (pattern_type="http_endpoint_client_synthesis"). The cross-repo
//     HTTP linker pairs these with a producer entity in another repo
//     during the link pass, so any chain that lands on one is by
//     construction a chain that crosses a repo boundary.
//
// The previous heuristic (`chainTouchesHTTP` — any step appears on an
// IMPLEMENTS / ROUTES_TO / SERVES edge boundary) is preserved as a separate
// `crosses_external_lib` property whenever the chain ALSO terminates in
// an external-library SCOPE.External / SCOPE.ExternalAPI node. That
// property captures the original intent ("this process bottoms out in a
// third-party dep") without conflating it with cross-repo traversal.
//
// Internal HTTP handlers (a controller method that implements a same-repo
// route synthetic) are intentionally NOT cross_stack: the BFS never
// leaves the source repo. They appear in the graph as plain CALLS chains.
//
// The pass is deterministic: entries are sorted by descending score then
// by canonical ID; outgoing edges at each BFS step are sorted by callee ID
// for stable top-k selection.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// ProcessFlowConfig controls the BFS pass.
type ProcessFlowConfig struct {
	// MaxDepth caps the chain length (number of hops past the entry). ≤10.
	MaxDepth int
	// BranchingFactor caps the number of outgoing CALLS expanded at each
	// step. ≤4 keeps the trace count tractable.
	BranchingFactor int
	// MaxEntryPoints is the global cap on entry candidates considered.
	MaxEntryPoints int
	// MaxProcesses is the global cap on Process entities emitted.
	MaxProcesses int
	// MinSteps is the minimum chain length for a Process to be emitted.
	// Trivial 1-hop processes are discarded.
	MinSteps int
}

// DefaultProcessFlowConfig returns the v1.0 tuning per #724.
func DefaultProcessFlowConfig() ProcessFlowConfig {
	return ProcessFlowConfig{
		MaxDepth:        10,
		BranchingFactor: 4,
		MaxEntryPoints:  200,
		MaxProcesses:    300,
		MinSteps:        3,
	}
}

// processStats summarises the outcome of one pass for stderr / tests.
type processStats struct {
	EntryCandidates int
	EntriesUsed     int
	Processes       int
	StepEdges       int
	EntryEdges      int
	CrossStack      int
	TruncatedDepth  int
	TruncatedFanout int
}

// RunProcessFlow executes the BFS pass against doc and appends the
// emitted Process entities + STEP_IN_PROCESS / ENTRY_POINT_OF edges to
// the document in place. Returns a stats summary. Safe to call on a
// document with no CALLS edges (returns an empty stats record).
func RunProcessFlow(doc *graph.Document, cfg ProcessFlowConfig) processStats {
	return RunProcessFlowWithCompanions(doc, nil, cfg)
}

// RunProcessFlowWithCompanions is the cross-repo-aware variant of
// RunProcessFlow (#1893). When companions is non-empty, the BFS extends past
// phantom cross-repo CALLS edges into the target repo's handler chain by
// unifying adjacency + entity index across all provided docs.
//
// Semantics:
//   - Process entities + edges are still appended only to `doc` (the source
//     repo). companions are read-only — never mutated.
//   - The BFS traverses CALLS / FETCHES / phantom edges from `doc` AND from
//     every companion. With phantom-edge targets indexed across docs, a chain
//     that begins in the frontend and traverses
//     caller → http_endpoint_call → http_endpoint_definition → backend handler
//     → ... is one continuous walk rather than a frontend-only chain that
//     dead-ends at the HTTP boundary.
//   - chainCrossesRepoBoundary still uses the phantom edge as the
//     authoritative cross-repo marker. The resulting Process is tagged
//     cross_stack=true and gets a new `cross_stack_bridge_at_step` property
//     recording the index of the first phantom step in the chain (useful for
//     dashboards to highlight the boundary visually).
//   - When companions is nil/empty the behavior is byte-identical to the
//     pre-#1893 single-doc pass.
func RunProcessFlowWithCompanions(doc *graph.Document, companions []*graph.Document, cfg ProcessFlowConfig) processStats {
	if doc == nil {
		return processStats{}
	}
	cfg = clampConfig(cfg)

	// Index entities by ID across doc + companions for fast lookup of kind /
	// source-file metadata. Entity IDs include the repo as a hash salt (see
	// graph.EntityID), so cross-doc collisions are impossible by construction.
	totalEntities := len(doc.Entities)
	for _, c := range companions {
		if c != nil {
			totalEntities += len(c.Entities)
		}
	}
	byID := make(map[string]*graph.Entity, totalEntities)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		byID[e.ID] = e
	}
	for _, c := range companions {
		if c == nil {
			continue
		}
		for i := range c.Entities {
			e := &c.Entities[i]
			if _, exists := byID[e.ID]; !exists {
				byID[e.ID] = e
			}
		}
	}

	// Build the same HTTP-boundary set used by entry ranking. Any chain
	// whose entry or step is on this set traverses an HTTP handler — that
	// makes the resulting Process cross-stack relevant even when the
	// http_endpoint entity itself sits at the end of an IMPLEMENTS edge
	// rather than the CALLS chain.
	httpBoundary := buildHTTPBoundarySetMulti(doc, companions)

	// #754 — file-level consumer endpoint index. Maps a source file path
	// to whether it contains a CONSUMER-side synthetic http_endpoint.
	// Class-field arrow methods (`byId = (id) => $http.get(...)`) and
	// other shapes the per-language extractors don't surface as discrete
	// function entities still produce one consumer synthetic per call
	// site at synthesis time. Treating any same-file Process chain as
	// cross_stack lets the cross-stack semantic survive that extraction
	// gap. The signal is necessarily file-coarse — when the per-language
	// extractors catch up (eventual JS/TS class-field support) the BFS
	// will reach the endpoint structurally and this fallback becomes a
	// no-op overlay on top of the precise FETCHES-edge signal.
	consumerEndpointFiles := buildConsumerEndpointFileSet(doc)

	// Build CALLS adjacency across doc + companions. Edges with explicit
	// `confidence < 0.5` are excluded so fuzzy global-fallback matches don't
	// dominate traces. Companion edges extend the adjacency past phantom
	// cross-repo edge targets (#1893): the phantom edge in doc points at an
	// entity that lives in a companion's Entities slice; without companion
	// adjacency the BFS would dead-end there.
	adj := buildCallsAdjacencyMulti(doc, companions)

	// Score candidate entry points.
	candidates := rankEntryPoints(doc, byID, adj, cfg)
	stats := processStats{EntryCandidates: len(candidates)}
	if len(candidates) == 0 {
		return stats
	}
	// #4344 — the MaxEntryPoints cap applies only to NON-endpoint candidates.
	// Every http_endpoint_definition root is retained so the flow count can
	// approach the endpoint count: an endpoint root must never be starved by
	// brokers / UI components competing for the 200 slots. Candidates are in
	// descending-score order, and all endpoint roots carry the dominating
	// httpEndpointEntryScore, so they already cluster at the front — but we
	// cap explicitly on the non-endpoint suffix to keep the guarantee
	// independent of the exact score constants.
	if len(candidates) > cfg.MaxEntryPoints {
		capped := make([]entryCandidate, 0, len(candidates))
		nonEndpoint := 0
		for _, c := range candidates {
			if isHTTPEndpointDefinition(byID[c.id]) {
				capped = append(capped, c)
				continue
			}
			if nonEndpoint < cfg.MaxEntryPoints {
				capped = append(capped, c)
				nonEndpoint++
			}
		}
		candidates = capped
	}
	// Drop entries that are reachable from a higher-ranked entry — those
	// are mid-chain functions, not true entry points. This collapses the
	// "every node with fan-out claims to be an entry" problem on linear
	// chains while preserving genuinely-independent entries in DAGs.
	candidates = pruneReachableEntries(candidates, adj, cfg.MaxDepth)
	stats.EntriesUsed = len(candidates)

	// #797 — build a lookup from entry entity ID → entryKind so we can
	// stamp the entry_kind property on emitted Process entities.
	entryKindByID := make(map[string]string, len(candidates))
	for _, c := range candidates {
		if c.entryKind != "" {
			entryKindByID[c.id] = c.entryKind
		}
	}

	// #1945 Phase 1 — DAG walker. The pre-#1945 path projected branched
	// call trees into N linear chains sharing a prefix, inflating Process
	// counts 3-5×. Now we build ONE DAG per entry and persist it via a
	// backward-compatible "primary linear chain" + a JSON-serialised DAG
	// blob on the Process Properties map. Existing consumers (dashboard,
	// MCP traces, JARVIS comet) continue to walk just the linear chain.
	type dagEmit struct {
		entry    string
		terminal string
		chain    []string
		dag      *ChainStep
		stats    dagBuildResult
	}
	emits := make([]dagEmit, 0, len(candidates))
	dagCfg := dagWalkConfig{
		MaxDepth:        cfg.MaxDepth,
		BranchingFactor: cfg.BranchingFactor,
		MaxNodes:        defaultDAGMaxNodes,
	}
	for _, c := range candidates {
		res := buildFlowDAG(c.id, adj, byID, dagCfg)
		stats.TruncatedDepth += res.DepthTruncated
		stats.TruncatedFanout += res.FanoutTruncated
		if res.Root == nil {
			continue
		}
		// #4316 — choose the canonical linear chain with cross-repo awareness:
		// when a fan-out offers both a dead-end consumer http_endpoint
		// synthetic (FETCHES) and a phantom cross-repo continuation into the
		// backend handler, follow the continuation so the persisted chain goes
		// end-to-end (caller → handler → service) instead of dead-ending at the
		// HTTP-call node. Falls back to leftmost primaryPath for pure intra-repo
		// flows, so UI-state setter chains stay terminal (no over-chaining).
		chain := primaryPathCrossRepo(res.Root, adj, byID)

		// #754 — short chains are allowed when the chain traverses a
		// FETCHES edge (cross-repo bridge) or a phantom cross-repo edge.
		// Cross-repo bridges typically have a tiny intra-repo depth
		// (caller → consumer endpoint = 2 steps); the cross-stack signal
		// is load-bearing so we keep them.
		minSteps := cfg.MinSteps
		if chainHasFetchesEdge(chain, adj) || chainCrossesRepo(chain, adj) {
			minSteps = 2
		}
		// #4344 — endpoint-rooted flows are exempt from the MinSteps cutoff.
		// Every HTTP endpoint with a handler should yield a flow labeled by
		// route, even a short one (endpoint → handler → service is only 3
		// steps; endpoint → handler is 2). Dropping these for being short is
		// exactly the bug that starved the flow count below the endpoint
		// count. The root resolving to an http_endpoint_definition is the
		// authoritative signal; 2 is the floor so a bare endpoint with no
		// reachable handler step still can't emit a 1-node flow.
		if isHTTPEndpointDefinition(byID[chain[0]]) {
			minSteps = 2
		}
		if len(chain) < minSteps {
			continue
		}
		emits = append(emits, dagEmit{
			entry:    c.id,
			terminal: chain[len(chain)-1],
			chain:    chain,
			dag:      res.Root,
			stats:    res,
		})
	}

	// Stable ordering: longest primary chain first, then by entry id, then
	// terminal id. Matches the prior tie-break so determinism tests pass.
	sort.Slice(emits, func(i, j int) bool {
		li := len(emits[i].chain)
		lj := len(emits[j].chain)
		if li != lj {
			return li > lj
		}
		if emits[i].entry != emits[j].entry {
			return emits[i].entry < emits[j].entry
		}
		return emits[i].terminal < emits[j].terminal
	})
	if len(emits) > cfg.MaxProcesses {
		emits = emits[:cfg.MaxProcesses]
	}

	// Emit Process entities + edges. One Process per surviving entry —
	// branches are captured inside the DAG attached as `branches_dag`.
	for _, em := range emits {
		chain := em.chain
		entry := byID[chain[0]]
		terminal := byID[chain[len(chain)-1]]
		// #769 — phantom cross-repo terminals live in another repo's
		// Entities slice and are intentionally absent from this doc's byID
		// map. Allow chains that end on a phantom edge even when terminal
		// is nil: the phantom edge Properties carry the target_repo and
		// link_method so we can still emit a meaningful Process label.
		terminalIsPhantom := terminal == nil && len(chain) >= 2 &&
			adj != nil && adj.phantom[edgeKey{chain[len(chain)-2], chain[len(chain)-1]}]
		if entry == nil || (terminal == nil && !terminalIsPhantom) {
			continue
		}
		crossStack, crossReason := chainCrossesRepoBoundary(chain, byID, adj)
		if !crossStack {
			// Fallback (#754): any chain whose entry file contains a
			// consumer http_endpoint entity is treated as cross_stack
			// even when the BFS couldn't physically reach the endpoint.
			// This captures the cross-repo semantic for JS/TS class-
			// field arrow methods (fixture-e shape) where the per-
			// language extractor doesn't surface the method as a
			// discrete entity. Producer-side handler processes are NOT
			// caught by this rule — their file contains producer-only
			// synthetics (pattern_type=http_endpoint_synthesis) which
			// buildConsumerEndpointFileSet excludes.
			if entry != nil && consumerEndpointFiles[entry.SourceFile] {
				crossStack = true
				crossReason = "entry file contains consumer http_endpoint synthetics (BFS chain didn't reach the bridge structurally — see #754 fallback)"
			}
		}
		crossesExternalLib := chainCrossesExternalLib(chain, byID, httpBoundary)
		processID := computeProcessID(doc.Repo, chain)

		// Resolve terminal name/ID — phantom terminals are not in byID.
		terminalID := chain[len(chain)-1]
		terminalName := terminalID // fallback for phantom targets
		if terminal != nil {
			terminalID = terminal.ID
			terminalName = terminal.Name
		} else if terminalIsPhantom {
			// Try to derive a human-readable label from the phantom edge
			// Properties. This makes the Process label less cryptic.
			phantomEdge := phantomEdgeForStep(chain[len(chain)-2], chain[len(chain)-1], doc)
			if phantomEdge != nil {
				if tgt := phantomEdge.PropGet("target_repo"); tgt != "" {
					terminalName = tgt + ":<cross-repo>"
				}
			}
		}
		// entryLabel is the human-readable name for the entry entity.
		// entry.Name is the canonical source; fall back through QualifiedName
		// and then the last path component of the ID so the Process entity
		// always carries a non-empty, non-hash Name field.
		entryLabel := entry.Name
		if entryLabel == "" {
			entryLabel = entry.QualifiedName
		}
		if entryLabel == "" {
			// Strip any "<repo>::<kind>:" prefix from the ID.
			if idx := strings.LastIndex(entry.ID, ":"); idx >= 0 && idx < len(entry.ID)-1 {
				entryLabel = entry.ID[idx+1:]
			} else {
				entryLabel = entry.ID
			}
		}
		// terminalLabel: prefer the human name, fall back to last ID segment.
		terminalLabel := terminalName
		if terminalLabel == terminalID && terminalID != "" {
			// terminalName still equals the raw ID (phantom fallback or missing name).
			// Try stripping the ID prefix for a shorter display value.
			if idx := strings.LastIndex(terminalID, ":"); idx >= 0 && idx < len(terminalID)-1 {
				terminalLabel = terminalID[idx+1:]
			}
		}
		label := fmt.Sprintf("%s → %s", entryLabel, terminalLabel)

		props := map[string]string{
			"entry_id":             entry.ID,
			"entry_name":           entry.Name,
			"terminal_id":          terminalID,
			"step_count":           strconv.Itoa(len(chain)),
			"cross_stack":          strconv.FormatBool(crossStack),
			"crosses_external_lib": strconv.FormatBool(crossesExternalLib),
			"chain":                strings.Join(chain, ","),
			"chain_labels":         strings.Join(chainLabels(chain, byID), " → "),
		}
		// #797 — stamp entry_kind so consumers can filter by entry type.
		if ek, ok := entryKindByID[entry.ID]; ok && ek != "" {
			props["entry_kind"] = ek
		} else if entry != nil && httpBoundary[entry.ID] {
			props["entry_kind"] = "http"
		}
		if terminalIsPhantom {
			props["terminal_is_phantom"] = "true"
		}
		if crossStack && crossReason != "" {
			props["cross_stack_reason"] = crossReason
		}
		// #1893 — record the chain index of the first phantom cross-repo edge
		// so dashboards / docs can visually mark the boundary step. -1 means no
		// phantom edge was traversed (the chain may still be cross_stack via
		// an unresolved consumer http_endpoint signal, in which case the
		// boundary is the endpoint step itself; see chainCrossesRepoBoundary).
		if bridgeIdx := firstPhantomStepIndex(chain, adj); bridgeIdx >= 0 {
			props["cross_stack_bridge_at_step"] = strconv.Itoa(bridgeIdx)
		}

		// #1945 Phase 1 — DAG metadata. `chain` above remains the primary
		// linear path (leftmost depth-first) for backward compatibility;
		// these new properties expose the full branched DAG that the
		// walker produced. Phase 2 will populate per-step Reason values
		// from per-language control-flow extraction; Phase 1 leaves
		// Reason empty on every step.
		props["dag_node_count"] = strconv.Itoa(em.stats.NodeCount)
		props["branch_count"] = strconv.Itoa(em.stats.BranchCount)
		props["is_dag"] = strconv.FormatBool(em.stats.BranchCount > 0)
		if em.stats.NodeCapHit {
			props["dag_node_cap_hit"] = "true"
		}
		if dagJSON := encodeDAGJSON(em.dag); dagJSON != "" {
			props["branches_dag"] = dagJSON
		}

		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         processID,
			Name:       label,
			Kind:       string(EntityKindProcess),
			SourceFile: entry.SourceFile,
			StartLine:  entry.StartLine,
			EndLine:    entry.EndLine,
			Language:   entry.Language,
		}.WithProperties(props))
		stats.Processes++
		if crossStack {
			stats.CrossStack++
		}

		// ENTRY_POINT_OF: entry function → Process.
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(entry.ID, processID, string(RelationshipKindEntryPointOf)),
			FromID: entry.ID,
			ToID:   processID,
			Kind:   string(RelationshipKindEntryPointOf),
		})
		stats.EntryEdges++

		// STEP_IN_PROCESS edges: Process → step entity, step_index 0-based.
		for i, stepID := range chain {
			rel := graph.Relationship{
				ID:     graph.RelationshipID(processID, stepID, string(RelationshipKindStepInProcess)+":"+strconv.Itoa(i)),
				FromID: processID,
				ToID:   stepID,
				Kind:   string(RelationshipKindStepInProcess),
			}.WithProperties(map[string]string{
				"step_index": strconv.Itoa(i),
			},
			)
			doc.Relationships = append(doc.Relationships, rel)
			stats.StepEdges++
		}
	}
	return stats
}

// clampConfig enforces the algorithmic bounds in #724.
func clampConfig(cfg ProcessFlowConfig) ProcessFlowConfig {
	if cfg.MaxDepth <= 0 || cfg.MaxDepth > 10 {
		cfg.MaxDepth = 10
	}
	if cfg.BranchingFactor <= 0 || cfg.BranchingFactor > 4 {
		cfg.BranchingFactor = 4
	}
	if cfg.MaxEntryPoints <= 0 {
		cfg.MaxEntryPoints = 200
	}
	if cfg.MaxProcesses <= 0 {
		cfg.MaxProcesses = 300
	}
	if cfg.MinSteps < 2 {
		cfg.MinSteps = 3
	}
	return cfg
}

// callsAdjacency stores out / in degree per node id over the BFS-traversable
// edge kinds (CALLS + FETCHES). The `fetches` side-set records (from,to)
// pairs that originated from a FETCHES edge so the cross-stack detector
// can distinguish "this step was reached by traversing a fetch boundary"
// from "this step was reached by an ordinary intra-repo CALLS edge".
// The `phantom` side-set records (from,to) pairs that originated from a
// phantom CALLS edge (cross_repo="true" property) injected by the phantom-
// edge pass (#769). chainCrossesRepo uses this to mark Process entities
// cross_stack=true when a chain traverses a phantom edge.
type callsAdjacency struct {
	out     map[string][]string
	in      map[string]int
	fetches map[edgeKey]bool
	phantom map[edgeKey]bool
	// handlerCont records (from,to) pairs that are synthetic "handler
	// continuation" edges injected by buildCallsAdjacency: an
	// http_endpoint_definition → its backend handler, derived by reversing
	// the handler IMPLEMENTS http_endpoint_definition edge (#1639). These
	// let the BFS continue a flow PAST the HTTP boundary into the resolved
	// backend handler instead of dead-ending at the endpoint synthetic.
	handlerCont map[edgeKey]bool
}

// edgeKey is a directed (from,to) edge identity.
type edgeKey struct{ from, to string }

// buildCallsAdjacency is the single-doc adjacency builder. Equivalent to
// buildCallsAdjacencyMulti(doc, nil); preserved for tests that exercise the
// single-doc shape directly.
func buildCallsAdjacency(doc *graph.Document) *callsAdjacency {
	return buildCallsAdjacencyMulti(doc, nil)
}

// buildCallsAdjacencyMulti filters CALLS + FETCHES edges (#754) across `doc`
// AND every doc in `companions`, producing a single deterministic adjacency
// list. The unified adjacency lets the BFS continue past phantom cross-repo
// CALLS edges (whose target ID lives in a companion's Entities slice) into
// the target repo's CALLS chain — the #1893 cross-repo flow extension.
//
// Edge filtering rules (per-relationship, identical across docs):
//   - CALLS with confidence < 0.5 dropped (fuzzy global fallback noise).
//   - FETCHES always kept (no confidence property; structural primitive).
//   - Phantom CALLS (cross_repo="true") always kept; recorded in adj.phantom.
//   - IMPLEMENTS edges to http_endpoint_definition reversed as handler-
//     continuation edges (#1639). Reversal runs for every doc, so a frontend
//     phantom edge can land on the backend's http_endpoint_definition and
//     continue into the backend handler in one BFS step.
//
// Per-doc passes are merged into the same maps. Companion docs are read-only.
func buildCallsAdjacencyMulti(doc *graph.Document, companions []*graph.Document) *callsAdjacency {
	a := &callsAdjacency{
		out:         make(map[string][]string),
		in:          make(map[string]int),
		fetches:     make(map[edgeKey]bool),
		phantom:     make(map[edgeKey]bool),
		handlerCont: make(map[edgeKey]bool),
	}
	seen := make(map[string]map[string]bool)
	docs := make([]*graph.Document, 0, 1+len(companions))
	docs = append(docs, doc)
	for _, c := range companions {
		if c != nil {
			docs = append(docs, c)
		}
	}
	for _, d := range docs {
		ingestCallsAdjacency(a, seen, d)
	}
	for k := range a.out {
		sort.Strings(a.out[k])
	}
	return a
}

// ingestCallsAdjacency merges one document's edges into the shared adjacency.
// Extracted so the multi-doc path can iterate without duplicating logic.
// Sorting of out-lists is deferred to the caller.
func ingestCallsAdjacency(a *callsAdjacency, seen map[string]map[string]bool, doc *graph.Document) {
	if doc == nil {
		return
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		isFetches := r.Kind == RelationshipKindFetches
		isPhantom := isPhantomCallsEdge(r)
		if r.Kind != string(RelationshipKindCalls) && !isFetches {
			continue
		}
		// Only CALLS edges are confidence-gated; FETCHES and phantom edges are
		// structural primitives always trusted.
		if !isFetches && !isPhantom && !confidenceOK(r) {
			continue
		}
		if r.FromID == r.ToID {
			continue // skip self-loops
		}
		if seen[r.FromID] == nil {
			seen[r.FromID] = make(map[string]bool)
		}
		if seen[r.FromID][r.ToID] {
			// Already added under a different edge kind. Still want to
			// record the fetches/phantom flag if this iteration provides it —
			// both cross-stack signals are preserved.
			if isFetches {
				a.fetches[edgeKey{r.FromID, r.ToID}] = true
			}
			if isPhantom {
				a.phantom[edgeKey{r.FromID, r.ToID}] = true
			}
			continue
		}
		seen[r.FromID][r.ToID] = true
		a.out[r.FromID] = append(a.out[r.FromID], r.ToID)
		a.in[r.ToID]++
		if isFetches {
			a.fetches[edgeKey{r.FromID, r.ToID}] = true
		}
		if isPhantom {
			a.phantom[edgeKey{r.FromID, r.ToID}] = true
		}
	}
	// #1639 — handler continuation edges. The HTTP resolve pass (#1615/#1217)
	// links a caller to an http_endpoint_definition (via the retargeted FETCHES
	// edge) and links the backend handler to that same definition via an
	// IMPLEMENTS edge (handler → http_endpoint_definition). The BFS can reach
	// the definition (forward over CALLS/FETCHES) but then dead-ends because
	// the definition has no outgoing edge — the handler points INTO it. We
	// reverse the IMPLEMENTS edge so the definition gains an outgoing
	// continuation edge to the handler, letting a flow track deeper across the
	// HTTP boundary into the backend handler (and onward through the handler's
	// own CALLS chain). Only IMPLEMENTS edges whose ToID is an
	// http_endpoint_definition (producer-side, the resolved backend route) are
	// reversed; consumer synthetics and other IMPLEMENTS shapes are untouched.
	defKinds := make(map[string]bool, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if strings.EqualFold(e.Kind, "http_endpoint_definition") {
			defKinds[e.ID] = true
		}
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "IMPLEMENTS" {
			continue
		}
		// handler --IMPLEMENTS--> http_endpoint_definition. Reverse it so the
		// definition continues into the handler.
		if !defKinds[r.ToID] {
			continue
		}
		from, to := r.ToID, r.FromID // definition → handler
		if from == to {
			continue
		}
		if seen[from] == nil {
			seen[from] = make(map[string]bool)
		}
		if seen[from][to] {
			a.handlerCont[edgeKey{from, to}] = true
			continue
		}
		seen[from][to] = true
		a.out[from] = append(a.out[from], to)
		a.in[to]++
		a.handlerCont[edgeKey{from, to}] = true
	}
}

// isPhantomCallsEdge reports whether a CALLS relationship is a phantom
// cross-repo edge injected by the phantom-edge pass (#769). Phantom
// edges carry cross_repo="true" in their Properties.
func isPhantomCallsEdge(r *graph.Relationship) bool {
	if r.Kind != string(RelationshipKindCalls) {
		return false
	}
	if r.PropLen() == 0 {
		return false
	}
	return r.PropGet("cross_repo") == "true"
}

// confidenceOK returns true when the relationship has either no
// confidence property or a parseable confidence ≥ 0.5.
func confidenceOK(r *graph.Relationship) bool {
	if r.PropLen() == 0 {
		return true
	}
	v, ok := r.PropLookup("confidence")
	if !ok {
		return true
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return true
	}
	return f >= 0.5
}

// NOTE: the original `bfsTraces` (linear chain projection) was removed in
// #1945 Phase 1. The DAG walker in process_flow_dag.go replaces it. The
// query-time MCP traces walker (`internal/mcp/traces.go::followCallsBFS`)
// is a separate read-side walker and remains untouched.

// chainLabels returns the human-readable names of each step (or its ID
// when the entity is missing).
func chainLabels(chain []string, byID map[string]*graph.Entity) []string {
	out := make([]string, len(chain))
	for i, id := range chain {
		if e, ok := byID[id]; ok && e.Name != "" {
			out[i] = e.Name
		} else {
			out[i] = id
		}
	}
	return out
}

// buildHTTPBoundarySet returns the set of entity ids on either side of
// an IMPLEMENTS / ROUTES_TO / SERVES edge — i.e. functions/methods that
// implement an HTTP endpoint, plus the endpoint entities themselves.
// Used by both rankEntryPoints (boost candidate score) and the cross-
// stack detector (mark Process as cross_stack=true).
func buildHTTPBoundarySet(doc *graph.Document) map[string]bool {
	return buildHTTPBoundarySetMulti(doc, nil)
}

// buildHTTPBoundarySetMulti is the cross-repo-aware variant. Companion docs'
// IMPLEMENTS / ROUTES_TO / SERVES endpoints (in particular the backend
// handlers + http_endpoint_definition pairs) are included so cross-repo flow
// extension can score backend handlers as legitimate entry-points / boundary
// nodes (#1893).
func buildHTTPBoundarySetMulti(doc *graph.Document, companions []*graph.Document) map[string]bool {
	out := make(map[string]bool)
	add := func(d *graph.Document) {
		if d == nil {
			return
		}
		for i := range d.Relationships {
			r := &d.Relationships[i]
			switch r.Kind {
			case "IMPLEMENTS", "ROUTES_TO", "SERVES":
				out[r.FromID] = true
				out[r.ToID] = true
			}
		}
	}
	add(doc)
	for _, c := range companions {
		add(c)
	}
	return out
}

// buildConsumerEndpointFileSet returns a set of source-file paths that
// contain at least one CONSUMER-side synthetic http_endpoint entity
// (pattern_type="http_endpoint_client_synthesis"). Files where the only
// http_endpoint entities are PRODUCER-side synthetics (handlers /
// routes) are EXCLUDED — producer synthetics represent the handler's
// own HTTP surface, not a cross-repo fetch call site.
//
// Used by RunProcessFlow as a file-coarse fallback for cross_stack when
// a chain's entry lives in a file with consumer endpoints that the BFS
// couldn't structurally reach (typically because the per-language
// extractor doesn't surface class-field arrow methods as entities).
func buildConsumerEndpointFileSet(doc *graph.Document) map[string]bool {
	out := map[string]bool{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if strings.ToLower(e.Kind) != "http_endpoint" {
			continue
		}
		if e.PropLen() == 0 {
			continue
		}
		if e.PropGet("pattern_type") == "http_endpoint_client_synthesis" {
			out[e.SourceFile] = true
			continue
		}
		// Fallback recognition for older synthetics that lost their
		// pattern_type stamp: any http_endpoint stamped with a
		// source_caller property is consumer-side by construction.
		if _, hasCaller := e.PropLookup("source_caller"); hasCaller {
			out[e.SourceFile] = true
		}
	}
	return out
}

// chainHasFetchesEdge returns true when any consecutive pair in the chain
// is connected by a FETCHES edge (recorded in adj.fetches at adjacency-
// build time). Used to relax the MinSteps gate for cross-repo chains so
// the 2-step caller → consumer-endpoint bridge survives.
func chainHasFetchesEdge(chain []string, adj *callsAdjacency) bool {
	if adj == nil {
		return false
	}
	for i := 1; i < len(chain); i++ {
		if adj.fetches[edgeKey{chain[i-1], chain[i]}] {
			return true
		}
	}
	return false
}

// chainCrossesRepoBoundary reports whether the BFS chain actually crosses
// a repo boundary (#754). The two recognised signals are:
//
//  1. An edge in the chain is a FETCHES edge: the BFS stepped from a
//     calling function into a consumer-side synthetic http_endpoint,
//     which the cross-repo HTTP linker pairs with a producer in another
//     repo. This is the canonical "definitely cross-stack" marker.
//  2. A step in the chain is a CONSUMER-side synthetic http_endpoint
//     entity (pattern_type="http_endpoint_client_synthesis"). Even
//     without a direct FETCHES edge, landing on a consumer synthetic
//     means the chain reached a cross-repo bridge node — the linker
//     joins the same Name to a producer entity in another repo.
//
// The second argument returns a short human-readable reason that the
// caller stamps onto `cross_stack_reason` for the emitted Process so
// docs can say "this process enters another repo at step N via FETCHES".
//
// PRODUCER-side synthetics (pattern_type="http_endpoint_synthesis") are
// deliberately NOT a cross-stack signal: they're the local handler for
// an HTTP route that lives in this repo. A controller method calling
// its same-repo route synthetic is intra-repo by definition.
func chainCrossesRepoBoundary(
	chain []string,
	byID map[string]*graph.Entity,
	adj *callsAdjacency,
) (bool, string) {
	// #1639 — repo-aware cross-repo flag. A chain is cross-repo ONLY when it
	// genuinely leaves the source repo. With the handler-continuation fix, an
	// HTTP call that resolves to a SAME-repo backend now continues caller →
	// http_endpoint_call → (FETCHES) → http_endpoint_definition →
	// (continuation) → backend handler — all inside this repo. That chain
	// must NOT be tagged cross-repo even though it traverses FETCHES edges and
	// touches http_endpoint synthetics. So the FETCHES edge alone is no longer
	// a cross-repo signal; we require an authoritative boundary marker:
	//
	//   1. A phantom cross-repo CALLS edge (#769): target_repo != this repo.
	//   2. A consumer http_endpoint synthetic that the chain does NOT resolve
	//      INTO a same-repo handler (no handler-continuation edge leaves it).
	//      An unresolved consumer synthetic is, by construction, a call whose
	//      backend lives in another repo (the cross-repo HTTP linker pairs it
	//      with a producer elsewhere). When the chain DID continue into a
	//      same-repo handler, the call resolved locally and is intra-repo.
	hasContinuation := false
	for i := 1; i < len(chain); i++ {
		if adj != nil && adj.handlerCont[edgeKey{chain[i-1], chain[i]}] {
			hasContinuation = true
			break
		}
	}

	// Walk pairwise — every transition has an originating edge.
	for i := 1; i < len(chain); i++ {
		from, to := chain[i-1], chain[i]
		// #769 — phantom CALLS edge: cross-repo link promoted by the
		// phantom-edge pass. The target entity lives in another repo; the
		// BFS terminates there (no outgoing edges), making this the final
		// step. This is the authoritative cross-repo signal.
		if adj != nil && adj.phantom[edgeKey{from, to}] {
			return true, fmt.Sprintf("phantom cross-repo CALLS edge at step %d (%s → %s)", i, from, to)
		}
		// A consumer synthetic that did NOT resolve into a same-repo handler
		// is a call whose backend lives in another repo.
		if !hasContinuation && isConsumerHTTPEndpoint(byID[to]) {
			return true, fmt.Sprintf("unresolved consumer http_endpoint at step %d (%s) — backend in another repo", i, to)
		}
	}
	// Also check the entry itself — a Process whose entry IS a consumer
	// synthetic (unusual but possible) still crosses repos.
	if !hasContinuation && len(chain) > 0 && isConsumerHTTPEndpoint(byID[chain[0]]) {
		return true, fmt.Sprintf("unresolved consumer http_endpoint at step 0 (%s)", chain[0])
	}
	return false, ""
}

// phantomEdgeForStep returns the first phantom CALLS relationship between
// fromID and toID in the document, or nil when no such edge exists.
// Used to extract Properties (target_repo, link_method) for label generation
// when the terminal entity is a phantom target absent from byID.
func phantomEdgeForStep(fromID, toID string, doc *graph.Document) *graph.Relationship {
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.FromID != fromID || r.ToID != toID {
			continue
		}
		if r.Kind != string(RelationshipKindCalls) {
			continue
		}
		if r.PropLen() > 0 && r.PropGet("cross_repo") == "true" {
			return r
		}
	}
	return nil
}

// firstPhantomStepIndex returns the (1-based-into-chain) index of the first
// step reached via a phantom cross-repo CALLS edge, or -1 when no such edge
// is traversed. The returned index points at the TARGET step of the phantom
// edge — i.e. the first step that lives in the other repo. Used to stamp
// cross_stack_bridge_at_step (#1893) so consumers can highlight the boundary.
func firstPhantomStepIndex(chain []string, adj *callsAdjacency) int {
	if adj == nil {
		return -1
	}
	for i := 1; i < len(chain); i++ {
		if adj.phantom[edgeKey{chain[i-1], chain[i]}] {
			return i
		}
	}
	return -1
}

// chainCrossesRepo is a convenience predicate that returns true iff any
// step in the chain is connected to the next by a phantom cross-repo CALLS
// edge (#769). Used by tests and by RunProcessFlow's MinSteps relaxation.
// Distinct from chainCrossesRepoBoundary which also fires on FETCHES edges
// and consumer http_endpoint kinds — this one is narrowly scoped to
// phantom edges only, which is what the phantom-edge pass emits.
func chainCrossesRepo(chain []string, adj *callsAdjacency) bool {
	if adj == nil {
		return false
	}
	for i := 1; i < len(chain); i++ {
		if adj.phantom[edgeKey{chain[i-1], chain[i]}] {
			return true
		}
	}
	return false
}

// isConsumerHTTPEndpoint returns true when the entity is an http_endpoint
// synthetic emitted by the consumer-side (client) synthesizer rather than
// the producer-side route synthesizer. We discriminate on the
// `pattern_type` property the synthesisers stamp on every entity:
//
//   - producer: pattern_type="http_endpoint_synthesis"
//   - consumer: pattern_type="http_endpoint_client_synthesis"
//
// When pattern_type is missing (older docs) we fall back to a presence
// check on `source_caller`, the property the consumer side always sets.
func isConsumerHTTPEndpoint(e *graph.Entity) bool {
	if e == nil {
		return false
	}
	if strings.ToLower(e.Kind) != "http_endpoint" {
		return false
	}
	if e.PropLen() == 0 {
		return false
	}
	if pt, ok := e.PropLookup("pattern_type"); ok {
		return pt == "http_endpoint_client_synthesis"
	}
	// Fallback: the consumer side stamps `source_caller` on every entity.
	_, hasCaller := e.PropLookup("source_caller")
	return hasCaller
}

// isHTTPEndpointDefinition reports whether an entity is a producer-side
// http_endpoint_definition — the route node that #4344 roots process-flows
// at. Used to exempt endpoint-rooted flows from the MinSteps cutoff and to
// derive the route label for the Process.
func isHTTPEndpointDefinition(e *graph.Entity) bool {
	if e == nil {
		return false
	}
	return strings.EqualFold(e.Kind, "http_endpoint_definition")
}

// chainCrossesExternalLib captures the OLD `chainCrossesStack ||
// chainTouchesHTTP` heuristic (#754): the chain either includes a step
// whose entity kind is HTTP/route-like, or touches the HTTP-boundary set
// (IMPLEMENTS / ROUTES_TO / SERVES). It is preserved as a separate
// boolean property so consumers that previously relied on the inflated
// `cross_stack` flag for "this process touches an HTTP handler" can
// switch to this label without losing that signal. The check is also
// useful for documentation generators that want to highlight processes
// terminating in third-party libraries (SCOPE.External / SCOPE.ExternalAPI).
func chainCrossesExternalLib(chain []string, byID map[string]*graph.Entity, boundary map[string]bool) bool {
	for _, id := range chain {
		if boundary[id] {
			return true
		}
		e, ok := byID[id]
		if !ok {
			continue
		}
		switch strings.ToLower(e.Kind) {
		case "http_endpoint",
			strings.ToLower(string(EntityKindEndpoint)),
			strings.ToLower(string(EntityKindRoute)),
			strings.ToLower(string(EntityKindExternalAPI)),
			"scope.external":
			return true
		}
	}
	return false
}

// computeProcessID derives a stable Process entity ID from the repo tag
// and the full chain. The hash is collision-resistant: two chains with
// the same entry + terminal but distinct intermediates get distinct IDs.
func computeProcessID(repo string, chain []string) string {
	h := sha256.New()
	h.Write([]byte(repo))
	h.Write([]byte{0})
	h.Write([]byte("Process"))
	h.Write([]byte{0})
	for _, c := range chain {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	return "proc:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// pruneReachableEntries removes candidates that are reachable from any
// higher-ranked candidate. Candidates is consumed in score order so we
// only need to track which IDs have been "claimed" by an earlier entry's
// forward-reachable set. Reachability is bounded by maxDepth to mirror the
// later BFS — a candidate that only becomes reachable past the BFS depth
// limit is still a valid independent entry.
func pruneReachableEntries(candidates []entryCandidate, adj *callsAdjacency, maxDepth int) []entryCandidate {
	claimed := make(map[string]bool)
	out := make([]entryCandidate, 0, len(candidates))
	for _, c := range candidates {
		if claimed[c.id] {
			continue
		}
		out = append(out, c)
		// Mark everything reachable from c (within maxDepth) as claimed.
		// We don't need separate frame state here — a simple level-BFS is
		// enough since we only care about set membership.
		frontier := []string{c.id}
		seen := map[string]bool{c.id: true}
		for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
			var next []string
			for _, n := range frontier {
				for _, nb := range adj.out[n] {
					if seen[nb] {
						continue
					}
					seen[nb] = true
					claimed[nb] = true
					next = append(next, nb)
				}
			}
			frontier = next
		}
	}
	return out
}
