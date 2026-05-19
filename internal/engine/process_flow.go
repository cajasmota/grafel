// Process-flow BFS pass (#724).
//
// Walks the CALLS graph forward from heuristically-detected entry points
// and emits Process entities + STEP_IN_PROCESS / ENTRY_POINT_OF edges.
// Each Process is a linearized call chain. The pass is language-agnostic —
// it consumes the CALLS edges that the per-language extractors already
// produce and never inspects source code directly.
//
// Algorithm (per ADR-0018 / issue #724):
//   1. Score every Function/Method/Operation/Component candidate by
//      fan-out, name pattern, exported flag, framework signal, and HTTP
//      boundary signal.
//   2. Keep the top entry points (capped at MaxEntryPoints).
//   3. For each entry point, run forward BFS over CALLS edges with depth
//      bounded by MaxDepth (≤10) and branching bounded by BranchingFactor
//      (≤4). Each traversal stops on a leaf (no outgoing CALLS) or when
//      bounds are hit.
//   4. Dedupe traces by (entry_id, terminal_id) — keep the longest chain
//      and drop strict prefixes of longer chains.
//   5. Emit one Process entity per surviving trace plus STEP_IN_PROCESS
//      edges (step_index ordered) and ENTRY_POINT_OF edges from the
//      entry function to the Process.
//
// Cross-stack detection: a Process is marked cross_stack=true when its
// chain traverses an HTTP boundary — i.e. one of its steps is an HTTP
// endpoint / route entity, or any edge target switches to a "SCOPE.Endpoint",
// "SCOPE.Route", or "http_endpoint" kind. Once #726 (FETCHES) lands the
// detection here will also include that edge kind.
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

	// BFS from each entry point.
	best := make(map[traceKey][]string) // key -> longest chain
	for _, c := range candidates {
		traces, depthTrunc, fanTrunc := bfsTraces(c.id, adj, cfg)
		stats.TruncatedDepth += depthTrunc
		stats.TruncatedFanout += fanTrunc
		for _, t := range traces {
			if len(t) < cfg.MinSteps {
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
		if entry == nil || terminal == nil {
			continue
		}
		crossStack := chainCrossesStack(chain, byID) || chainTouchesHTTP(chain, httpBoundary)
		processID := computeProcessID(doc.Repo, chain)
		label := fmt.Sprintf("%s → %s", entry.Name, terminal.Name)

		props := map[string]string{
			"entry_id":     entry.ID,
			"entry_name":   entry.Name,
			"terminal_id":  terminal.ID,
			"step_count":   strconv.Itoa(len(chain)),
			"cross_stack":  strconv.FormatBool(crossStack),
			"chain":        strings.Join(chain, ","),
			"chain_labels": strings.Join(chainLabels(chain, byID), " → "),
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

// callsAdjacency stores out / in degree per node id over CALLS edges.
type callsAdjacency struct {
	out map[string][]string
	in  map[string]int
}

// buildCallsAdjacency filters the document's edges down to CALLS only and
// produces a deterministic adjacency list. Edges with confidence < 0.5
// (as set by the resolver) are excluded — they're typically global-fallback
// matches and inflate trace counts with false branches.
func buildCallsAdjacency(doc *graph.Document) *callsAdjacency {
	a := &callsAdjacency{
		out: make(map[string][]string),
		in:  make(map[string]int),
	}
	seen := make(map[string]map[string]bool)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != string(RelationshipKindCalls) {
			continue
		}
		if !confidenceOK(r) {
			continue
		}
		if r.FromID == r.ToID {
			continue // skip self-loops
		}
		if seen[r.FromID] == nil {
			seen[r.FromID] = make(map[string]bool)
		}
		if seen[r.FromID][r.ToID] {
			continue
		}
		seen[r.FromID][r.ToID] = true
		a.out[r.FromID] = append(a.out[r.FromID], r.ToID)
		a.in[r.ToID]++
	}
	for k := range a.out {
		sort.Strings(a.out[k])
	}
	return a
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

// chainTouchesHTTP returns true when any step in the chain is on the
// HTTP-boundary set (i.e. is the source or target of an IMPLEMENTS /
// ROUTES_TO / SERVES edge).
func chainTouchesHTTP(chain []string, boundary map[string]bool) bool {
	for _, id := range chain {
		if boundary[id] {
			return true
		}
	}
	return false
}

// chainCrossesStack reports whether any step in the chain points at an
// entity whose kind represents an HTTP / service boundary. Once the
// FETCHES edge kind lands (#726) the detection is extended in
// buildCallsAdjacency to flag the transition there.
func chainCrossesStack(chain []string, byID map[string]*graph.Entity) bool {
	for _, id := range chain {
		e, ok := byID[id]
		if !ok {
			continue
		}
		switch strings.ToLower(e.Kind) {
		case "http_endpoint",
			strings.ToLower(string(EntityKindEndpoint)),
			strings.ToLower(string(EntityKindRoute)),
			strings.ToLower(string(EntityKindExternalAPI)):
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
