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

	"github.com/cajasmota/archigraph/internal/graph"
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
	if doc == nil {
		return processStats{}
	}
	cfg = clampConfig(cfg)

	// Index entities by ID for fast lookup of kind / source-file metadata.
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		byID[e.ID] = e
	}

	// Build the same HTTP-boundary set used by entry ranking. Any chain
	// whose entry or step is on this set traverses an HTTP handler — that
	// makes the resulting Process cross-stack relevant even when the
	// http_endpoint entity itself sits at the end of an IMPLEMENTS edge
	// rather than the CALLS chain.
	httpBoundary := buildHTTPBoundarySet(doc)

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

	// Build CALLS adjacency. Edges with explicit `confidence < 0.5` are
	// excluded so fuzzy global-fallback matches don't dominate traces.
	adj := buildCallsAdjacency(doc)

	// Score candidate entry points.
	candidates := rankEntryPoints(doc, byID, adj, cfg)
	stats := processStats{EntryCandidates: len(candidates)}
	if len(candidates) == 0 {
		return stats
	}
	if len(candidates) > cfg.MaxEntryPoints {
		candidates = candidates[:cfg.MaxEntryPoints]
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

	// BFS from each entry point.
	best := make(map[traceKey][]string) // key -> longest chain
	for _, c := range candidates {
		traces, depthTrunc, fanTrunc := bfsTraces(c.id, adj, cfg)
		stats.TruncatedDepth += depthTrunc
		stats.TruncatedFanout += fanTrunc
		for _, t := range traces {
			// #754 — short chains are allowed when the chain traverses a
			// FETCHES edge (i.e. crosses a repo boundary). Cross-repo
			// bridges typically have a tiny intra-repo depth (caller →
			// consumer endpoint = 2 steps) but the cross-stack signal is
			// the load-bearing semantic, so keep them.
			minSteps := cfg.MinSteps
			if chainHasFetchesEdge(t, adj) || chainCrossesRepo(t, adj) {
				// #769 — phantom cross-repo CALLS chains are typically
				// shallow (caller → phantom terminal = 2 steps). Relax the
				// MinSteps gate so they survive emission.
				minSteps = 2
			}
			if len(t) < minSteps {
				continue
			}
			term := t[len(t)-1]
			k := traceKey{c.id, term}
			if prev, ok := best[k]; !ok || len(t) > len(prev) {
				best[k] = t
			}
		}
	}

	// Stable, scored ordering of the surviving traces.
	type emit struct {
		chain     []string
		entryName string
		entryFile string
	}
	keys := make([]traceKey, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		// Longest chains first, then by entry id, then terminal id for
		// determinism.
		li := len(best[keys[i]])
		lj := len(best[keys[j]])
		if li != lj {
			return li > lj
		}
		if keys[i].entry != keys[j].entry {
			return keys[i].entry < keys[j].entry
		}
		return keys[i].terminal < keys[j].terminal
	})
	if len(keys) > cfg.MaxProcesses {
		keys = keys[:cfg.MaxProcesses]
	}

	// Drop chains that are a strict prefix of a longer chain emitted from
	// the same entry. This collapses sub-trace redundancy without losing
	// the longest representation of any branch.
	keys = dropPrefixSubtraces(keys, best)

	// Emit Process entities + edges.
	for _, k := range keys {
		chain := best[k]
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
				if tgt := phantomEdge.Properties["target_repo"]; tgt != "" {
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

		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         processID,
			Name:       label,
			Kind:       string(EntityKindProcess),
			SourceFile: entry.SourceFile,
			StartLine:  entry.StartLine,
			EndLine:    entry.EndLine,
			Language:   entry.Language,
			Properties: props,
		})
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
				Properties: map[string]string{
					"step_index": strconv.Itoa(i),
				},
			}
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
}

// edgeKey is a directed (from,to) edge identity.
type edgeKey struct{ from, to string }

// buildCallsAdjacency filters the document's edges down to CALLS + FETCHES
// (#754) and produces a deterministic adjacency list. Edges with
// confidence < 0.5 (as set by the resolver) are excluded — they're
// typically global-fallback matches and inflate trace counts with false
// branches. FETCHES edges are unconditionally included: they're emitted
// at extraction/resolve time with no confidence property and represent
// definite cross-repo fetch points, not fuzzy resolution candidates.
func buildCallsAdjacency(doc *graph.Document) *callsAdjacency {
	a := &callsAdjacency{
		out:     make(map[string][]string),
		in:      make(map[string]int),
		fetches: make(map[edgeKey]bool),
		phantom: make(map[edgeKey]bool),
	}
	seen := make(map[string]map[string]bool)
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
	for k := range a.out {
		sort.Strings(a.out[k])
	}
	return a
}

// isPhantomCallsEdge reports whether a CALLS relationship is a phantom
// cross-repo edge injected by the phantom-edge pass (#769). Phantom
// edges carry cross_repo="true" in their Properties.
func isPhantomCallsEdge(r *graph.Relationship) bool {
	if r.Kind != string(RelationshipKindCalls) {
		return false
	}
	if r.Properties == nil {
		return false
	}
	return r.Properties["cross_repo"] == "true"
}

// confidenceOK returns true when the relationship has either no
// confidence property or a parseable confidence ≥ 0.5.
func confidenceOK(r *graph.Relationship) bool {
	if r.Properties == nil {
		return true
	}
	v, ok := r.Properties["confidence"]
	if !ok {
		return true
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return true
	}
	return f >= 0.5
}

// bfsTraces runs forward BFS from entry, emitting one chain per reachable
// terminal node within the configured depth + branching bounds. A "terminal"
// is any node with no outgoing CALLS or the node at MaxDepth.
//
// Returns the slice of chains plus the count of depth- and fanout-truncated
// branches (useful for stats).
func bfsTraces(entry string, adj *callsAdjacency, cfg ProcessFlowConfig) ([][]string, int, int) {
	type frame struct {
		chain []string
		seen  map[string]bool
	}
	initSeen := map[string]bool{entry: true}
	work := []frame{{chain: []string{entry}, seen: initSeen}}
	var out [][]string
	depthTrunc, fanTrunc := 0, 0

	for len(work) > 0 {
		// Pop last (DFS-ish iterative — order doesn't matter for the
		// emitted set since we dedupe by (entry,terminal)).
		f := work[len(work)-1]
		work = work[:len(work)-1]

		current := f.chain[len(f.chain)-1]
		neighbors := adj.out[current]
		if len(neighbors) == 0 || len(f.chain) > cfg.MaxDepth {
			if len(f.chain) > cfg.MaxDepth {
				depthTrunc++
			}
			// Emit a copy — f.chain may alias slices we will mutate later.
			out = append(out, append([]string(nil), f.chain...))
			continue
		}
		// Sort + cap to branching factor for determinism.
		sortedN := append([]string(nil), neighbors...)
		sort.Strings(sortedN)
		if len(sortedN) > cfg.BranchingFactor {
			fanTrunc += len(sortedN) - cfg.BranchingFactor
			sortedN = sortedN[:cfg.BranchingFactor]
		}
		extended := false
		for _, n := range sortedN {
			if f.seen[n] {
				continue
			}
			extended = true
			newSeen := make(map[string]bool, len(f.seen)+1)
			for k := range f.seen {
				newSeen[k] = true
			}
			newSeen[n] = true
			newChain := append(append([]string(nil), f.chain...), n)
			work = append(work, frame{chain: newChain, seen: newSeen})
		}
		if !extended {
			// All neighbors already visited → terminal cycle stop.
			out = append(out, append([]string(nil), f.chain...))
		}
	}
	return out, depthTrunc, fanTrunc
}

// dropPrefixSubtraces removes chains that are strict prefixes of another
// chain emitted from the same entry id. The longer chain is kept.
func dropPrefixSubtraces(keys []traceKey, best map[traceKey][]string) []traceKey {
	// Bucket by entry id.
	byEntry := make(map[string][]traceKey)
	for _, k := range keys {
		byEntry[k.entry] = append(byEntry[k.entry], k)
	}
	keep := make(map[traceKey]bool, len(keys))
	for _, ks := range byEntry {
		// Longest first so we can short-circuit prefix checks.
		sort.Slice(ks, func(i, j int) bool {
			return len(best[ks[i]]) > len(best[ks[j]])
		})
		for i, k := range ks {
			isPrefix := false
			for j := 0; j < i; j++ {
				if isStrictPrefix(best[k], best[ks[j]]) {
					isPrefix = true
					break
				}
			}
			if !isPrefix {
				keep[k] = true
			}
		}
	}
	out := make([]traceKey, 0, len(keep))
	for _, k := range keys {
		if keep[k] {
			out = append(out, k)
		}
	}
	return out
}

func isStrictPrefix(short, long []string) bool {
	if len(short) >= len(long) {
		return false
	}
	for i := range short {
		if short[i] != long[i] {
			return false
		}
	}
	return true
}

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
	out := make(map[string]bool)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		switch r.Kind {
		case "IMPLEMENTS", "ROUTES_TO", "SERVES":
			out[r.FromID] = true
			out[r.ToID] = true
		}
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
		if e.Properties == nil {
			continue
		}
		if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
			out[e.SourceFile] = true
			continue
		}
		// Fallback recognition for older synthetics that lost their
		// pattern_type stamp: any http_endpoint stamped with a
		// source_caller property is consumer-side by construction.
		if _, hasCaller := e.Properties["source_caller"]; hasCaller {
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
	// Walk pairwise — every transition has an originating edge.
	for i := 1; i < len(chain); i++ {
		from, to := chain[i-1], chain[i]
		if adj != nil && adj.fetches[edgeKey{from, to}] {
			return true, fmt.Sprintf("FETCHES edge at step %d (%s → %s)", i, from, to)
		}
		// #769 — phantom CALLS edge: cross-repo link promoted by the
		// phantom-edge pass. The target entity lives in another repo; the
		// BFS terminates there (no outgoing edges), making this the final
		// step. Separate label from crosses_external_lib.
		if adj != nil && adj.phantom[edgeKey{from, to}] {
			return true, fmt.Sprintf("phantom cross-repo CALLS edge at step %d (%s → %s)", i, from, to)
		}
		if isConsumerHTTPEndpoint(byID[to]) {
			return true, fmt.Sprintf("consumer http_endpoint at step %d (%s)", i, to)
		}
	}
	// Also check the entry itself — a Process whose entry IS a consumer
	// synthetic (unusual but possible) still crosses repos.
	if len(chain) > 0 && isConsumerHTTPEndpoint(byID[chain[0]]) {
		return true, fmt.Sprintf("consumer http_endpoint at step 0 (%s)", chain[0])
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
		if r.Properties != nil && r.Properties["cross_repo"] == "true" {
			return r
		}
	}
	return nil
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
	if e.Properties == nil {
		return false
	}
	if pt, ok := e.Properties["pattern_type"]; ok {
		return pt == "http_endpoint_client_synthesis"
	}
	// Fallback: the consumer side stamps `source_caller` on every entity.
	_, hasCaller := e.Properties["source_caller"]
	return hasCaller
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

// traceKey is a chain identity (defined here at file scope so the
// dropPrefixSubtraces helper can take it as a parameter).
type traceKey struct {
	entry    string
	terminal string
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
