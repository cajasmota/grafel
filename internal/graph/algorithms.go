// Package graph — algorithms.go implements Pass 4: graph algorithm
// computation over the merged entity/relationship set.
//
// Six attributes are pre-baked into every entity:
//   - community_id              (Louvain modularity-maximising community)
//   - centrality                (betweenness centrality, weighted)
//   - pagerank                  (PageRank, damping=0.85, tol=1e-6)
//   - is_god_node               (top 5% by combined betweenness+pagerank rank)
//   - is_surprise_endpoint      (endpoint of one of the top-K cross-community
//     "surprise" edges)
//   - is_articulation_point     (cut vertex in the undirected graph)
//
// Top-level corpus aggregates are exposed via AlgorithmResults: per-community
// stats, surprise-edge list, and timing.
//
// # Community detection algorithm
//
// We use Louvain modularity maximisation. The implementation is in-repo
// (louvain.go) rather than gonum's community.Modularize: gonum evaluates a
// candidate move by iterating every member of the target community, making one
// local-moving sweep cost Σ_c |c|². On the production corpus that was 3h15m,
// 99.5% of the whole indexing pipeline. The in-repo version is the SAME
// algorithm computed the standard way (incremental sigma_tot, neighbour-side
// k_{i,C}, CSR adjacency) at O(E) per sweep. See louvain.go.
//
// It carries no PRNG at all: node ordering, adjacency ordering and move
// tie-breaks are fixed, so results are byte-identical across re-runs of the
// same graph by construction.
//
// Leiden was evaluated for this release (#1382) and deferred because no
// production-quality Go Leiden library exists: github.com/vsuryav/leiden-go and
// github.com/k8nstantin/go-leiden are both pre-v1, un-tagged, and lack the
// weighted-graph + deterministic-seeding APIs required. An in-house Leiden port
// would require porting the full CPM refinement phase (~500 LOC) and is
// out-of-scope for this PR. The gonum Louvain implementation already produces
// stable, well-connected communities with a fixed seed; the main noise problem
// is addressed by the min-size denoise filter (see CommunityOptions.MinSize).
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"sort"
	"time"

	gonumgraph "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

// CommunityResult summarises one Louvain community for the on-disk output.
//
// AutoName is the deterministic Layer-1 label produced by AssignCommunityNames
// (TF-IDF over member entity names). It is always populated when communities
// are computed; consumers that previously fell back to "community_<id>" can
// now display AutoName directly.
//
// AgentName is the Layer-2 label resolved by an MCP agent via
// submit_enrichment(kind="name_community"). It takes precedence over AutoName
// when present (issue #426).
type CommunityResult struct {
	ID          int      `json:"id"`
	Size        int      `json:"size"`
	Modularity  float64  `json:"modularity"`
	TopEntities []string `json:"top_entities"`
	AutoName    string   `json:"auto_name,omitempty"`
	AgentName   string   `json:"agent_name,omitempty"`
}

// SurpriseEdge is a cross-community edge whose pair frequency is rare.
type SurpriseEdge struct {
	FromID string  `json:"from_id"`
	ToID   string  `json:"to_id"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// CommunityOptions controls community-detection behaviour. It is passed to
// RunAlgorithmsWithOptions; RunAlgorithms uses DefaultCommunityOptions.
type CommunityOptions struct {
	// MinSize is the minimum number of nodes a community must contain to be
	// emitted as a named community. Communities smaller than MinSize have their
	// members remapped to community -1 ("ungrouped") and are dropped from the
	// CommunityResult slice. This eliminates singleton and micro-community
	// noise without affecting the graph structure or any other algorithm pass.
	//
	// Default: 5  (configurable via ~/.grafel/algorithms.json or
	// the GRAFEL_COMMUNITY_MIN_SIZE environment variable).
	//
	// Set to 1 to disable denoising (all communities emitted, matching the
	// pre-#1382 behaviour).
	MinSize int `json:"min_size"`
}

// DefaultCommunityOptions returns the production defaults for community
// detection. MinSize=5 removes singletons and micro-communities that
// contribute noise without structural signal.
func DefaultCommunityOptions() CommunityOptions {
	return CommunityOptions{MinSize: 5}
}

// AlgorithmStats are the corpus-level metrics exposed both inside graph.json
// and inside the .grafel/graph-stats.json sidecar.
type AlgorithmStats struct {
	LouvainModularity  float64 `json:"louvain_modularity"`
	NumCommunities     int     `json:"num_communities"`
	NumGodNodes        int     `json:"num_god_nodes"`
	NumArticulationPts int     `json:"num_articulation_points"`
	NumSurpriseEdges   int     `json:"num_surprise_edges"`
	RuntimeMS          int64   `json:"runtime_ms"`
	// DenoisedCommunities is the number of raw Louvain communities that were
	// collapsed into the "ungrouped" bucket (community_id=-1) because they
	// fell below CommunityOptions.MinSize. Zero when MinSize <= 1.
	DenoisedCommunities int `json:"denoised_communities,omitempty"`
}

// AlgorithmResults bundles the per-entity and corpus-level outputs of Pass 4.
type AlgorithmResults struct {
	CommunityID        map[string]int     // entity id -> community id
	Centrality         map[string]float64 // entity id -> betweenness
	PageRank           map[string]float64 // entity id -> pagerank
	GodNodes           map[string]bool
	ArticulationPoints map[string]bool
	SurpriseEndpoints  map[string]bool
	Communities        []CommunityResult
	SurpriseEdges      []SurpriseEdge
	Stats              AlgorithmStats
}

// nodeIndex maps stable string entity IDs onto contiguous int64 node IDs
// (gonum/graph addresses nodes by int64 only).
type nodeIndex struct {
	toInt   map[string]int64
	fromInt map[int64]string
	next    int64

	// csr is the directed adjacency of the graph BuildGraph just built, in
	// compact CSR form. It is derived ONCE, from the relationship slice, and
	// then shared by every consumer that needs adjacency (sampled betweenness,
	// the Louvain undirected projection). See directedCSR.
	csr *directedCSR
	// csrBuilds counts how many times a directed CSR was derived for this
	// index. It must be exactly 1 after BuildGraph and must not grow when the
	// downstream algorithms run — that invariant is the entire point of #5954
	// S5/S6 and is pinned by TestDirectedCSRIsBuiltExactlyOncePerBuildGraph.
	csrBuilds int
	// undirectedDerivations counts how many undirected projections have been
	// derived from csr. One full Pass-4 run must derive exactly one; a second
	// would mean the Louvain path is walking the structure twice again.
	//
	// It lives here rather than on directedCSR so that directedCSR itself has
	// NO writer once BuildGraph returns: newBCScratch ALIASES its arrays, and
	// "read-only apart from one counter" is a weaker invariant than that
	// aliasing deserves.
	undirectedDerivations int
}

func newNodeIndex() *nodeIndex {
	return &nodeIndex{
		toInt:   make(map[string]int64),
		fromInt: make(map[int64]string),
	}
}

func (n *nodeIndex) get(id string) int64 {
	if v, ok := n.toInt[id]; ok {
		return v
	}
	v := n.next
	n.next++
	n.toInt[id] = v
	n.fromInt[v] = id
	return v
}

// directedCSR is the directed adjacency of the graph BuildGraph constructs,
// in compressed-sparse-row form: `off` holds n+1 row offsets and `adj`/`w` are
// the flat, row-major neighbour and weight arrays. Node i of the CSR is gonum
// node id i (BuildGraph hands out ids 0..n-1 in entity order), so no id
// translation is needed by any consumer.
//
// # Why this exists (#5954 S5/S6)
//
// The directed gonum graph stays — ComputeCentrality's PageRank and
// IdentifyArticulationPoints both consume it. But the adjacency STRUCTURE was
// previously re-derived from it up to four separate times per Pass-4 run, each
// time through gonum's interface-returning edge iterator, which materialises
// one boxed graph.WeightedEdge per edge into a slice sized by the whole edge
// set. Profiling after the S4 scratch-reuse fix attributed 105.1 MB (62% of
// what was left) to exactly that boxing. Deriving the adjacency once, from the
// relationship slice, and sharing it removes those walks outright.
//
// # int32 sizing
//
// The reference corpus is ~427k entities and ~1.33M collapsed edges, both three
// orders of magnitude below math.MaxInt32, so int32 indices are safe and halve
// the footprint of `off`/`adj` relative to int64. buildDirectedCSR ENFORCES the
// bound on both the node count and the pre-collapse edge count — see
// csrIndexLimit for why it enforces by panicking.
//
// # Mutability
//
// Every field is written once, during construction, and is READ-ONLY
// thereafter. newBCScratch relies on that: it aliases off/adj rather than
// copying them. Do not add mutable state here — the two derivation counters
// this change needs live on nodeIndex precisely so this object has no writer
// after BuildGraph returns.
type directedCSR struct {
	n   int
	off []int32   // len n+1
	adj []int32   // len off[n], target node ids, ASCENDING within a row
	w   []float64 // len off[n], parallel to adj
}

// csrIndexLimit is the largest node count and edge count directedCSR's int32
// indices can address.
//
// Exceeding it PANICS rather than degrading. The alternative — returning a
// truncated or all-zero CSR — is a silent wrong answer: newBCScratch would
// accept an all-zero offset array and produce all-zero betweenness, and the
// Louvain projection would produce n singleton communities, both of which are
// then PERSISTED as if they were real scores. A graph two thousand times larger
// than the reference corpus is a situation that needs a human, not a fallback.
//
// It is a var only so TestDirectedCSRIndexLimitIsEnforced can lower it and drive
// the real BuildGraph path: a bound that can only be exercised by calling a
// helper directly is a bound nothing proves is wired into the code that runs.
// Nothing in production writes it.
var csrIndexLimit int64 = math.MaxInt32

// row returns the [lo, hi) slice bounds of u's adjacency row.
func (c *directedCSR) row(u int32) (int32, int32) { return c.off[u], c.off[u+1] }

// hasEdge reports whether the directed edge u->v is present. Rows are sorted
// ascending, so this is a binary search.
func (c *directedCSR) hasEdge(u, v int32) bool {
	lo, hi := c.row(u)
	for lo < hi {
		mid := lo + (hi-lo)/2
		switch {
		case c.adj[mid] < v:
			lo = mid + 1
		case c.adj[mid] > v:
			hi = mid
		default:
			return true
		}
	}
	return false
}

// weightOf returns the weight of u->v and whether it exists.
func (c *directedCSR) weightOf(u, v int32) (float64, bool) {
	lo, hi := c.row(u)
	for lo < hi {
		mid := lo + (hi-lo)/2
		switch {
		case c.adj[mid] < v:
			lo = mid + 1
		case c.adj[mid] > v:
			hi = mid
		default:
			return c.w[mid], true
		}
	}
	return 0, false
}

// csrEndpoints applies BuildGraph's edge-admission rules to one relationship
// and returns the dense endpoint pair. It is the SINGLE definition of "which
// relationships become edges", shared with BuildGraph's gonum insertion loop so
// the two cannot drift:
//
//   - blank endpoint id      -> rejected
//   - endpoint not an entity -> rejected (bare stdlib names etc.)
//   - self-loop              -> rejected (gonum rejects them on simple graphs)
//
// Parallel edges are ADMITTED here and collapsed downstream, mirroring
// BuildGraph's weight accumulation.
func csrEndpoints(idx *nodeIndex, r *Relationship) (from, to int32, ok bool) {
	if r.FromID == "" || r.ToID == "" {
		return 0, 0, false
	}
	f, ok := idx.toInt[r.FromID]
	if !ok {
		return 0, 0, false
	}
	t, ok := idx.toInt[r.ToID]
	if !ok {
		return 0, 0, false
	}
	if f == t {
		return 0, 0, false
	}
	return int32(f), int32(t), true
}

// buildDirectedCSR derives the compact directed adjacency straight from the
// relationship slice — it never touches the gonum graph, so it costs no edge
// boxing at all.
//
// It reproduces BuildGraph's three structural behaviours exactly:
//
//  1. every entity is a node, including isolated ones (n = idx.next, so a node
//     with no incident relationship still gets an — empty — row);
//  2. self-loops are dropped (csrEndpoints);
//  3. parallel edges are collapsed with their weights ACCUMULATED, in
//     relationship order, so the resulting float is bit-identical to the
//     `w += existing.Weight()` chain BuildGraph runs.
//
// Allocation is count-then-fill, never append-from-zero: pass 1 counts raw
// out-degrees, pass 2 fills exactly-sized raw arrays, then parallel edges are
// collapsed in place and the result copied into exactly-sized final arrays.
func buildDirectedCSR(idx *nodeIndex, rels []Relationship) *directedCSR {
	idx.csrBuilds++
	n := int(idx.next)
	if int64(n) > csrIndexLimit {
		panic(fmt.Sprintf("graph: %d entities exceeds the int32 CSR index limit %d", n, csrIndexLimit))
	}
	c := &directedCSR{n: n, off: make([]int32, n+1)}
	if n == 0 {
		return c
	}

	// Pass 1 — raw out-degree per source. Parallel edges are still counted
	// separately at this point; they collapse below. The running total is int64
	// so the bound check below cannot itself be the thing that overflows.
	rawOff := make([]int32, n+1)
	var maxRow int32
	var admitted int64
	for i := range rels {
		u, _, ok := csrEndpoints(idx, &rels[i])
		if !ok {
			continue
		}
		rawOff[u+1]++
		admitted++
	}
	if admitted > csrIndexLimit {
		panic(fmt.Sprintf("graph: %d admitted relationships exceeds the int32 CSR index limit %d", admitted, csrIndexLimit))
	}
	for i := 0; i < n; i++ {
		if d := rawOff[i+1]; d > maxRow {
			maxRow = d
		}
		rawOff[i+1] += rawOff[i]
	}
	rawTotal := rawOff[n]

	// Pass 2 — fill. Within a row, entries land in relationship order, which
	// is what makes the collapse below reproduce BuildGraph's accumulation
	// order.
	adj := make([]int32, rawTotal)
	w := make([]float64, rawTotal)
	cursor := make([]int32, n)
	copy(cursor, rawOff[:n])
	for i := range rels {
		u, v, ok := csrEndpoints(idx, &rels[i])
		if !ok {
			continue
		}
		p := cursor[u]
		adj[p] = v
		w[p] = edgeWeight(rels[i].PropsSnapshot())
		cursor[u] = p + 1
	}

	// Collapse parallel edges in place. Rows are processed in ascending order
	// and the write cursor never overtakes the row start, so the compacted
	// prefix is always behind the row being read. `rowW` shadows the row's
	// weights because the write can land on the row's own first slot.
	//
	// The sort key packs (target, arrival index within the row) into one int64
	// so slices.Sort — no closure, no reflect swapper — gives a target-ordered,
	// arrival-stable permutation. Arrival stability is what preserves the
	// float accumulation order.
	keys := make([]int64, 0, maxRow)
	rowW := make([]float64, maxRow)
	var write int32
	for u := 0; u < n; u++ {
		lo, hi := rawOff[u], rawOff[u+1]
		c.off[u] = write
		if lo == hi {
			continue
		}
		keys = keys[:0]
		for p := lo; p < hi; p++ {
			keys = append(keys, int64(adj[p])<<32|int64(p-lo))
		}
		copy(rowW, w[lo:hi])
		slices.Sort(keys)
		for j := 0; j < len(keys); {
			target := int32(keys[j] >> 32)
			// BuildGraph computes `w := edgeWeight(...)` then `w += existing`,
			// i.e. new + accumulated. IEEE-754 addition is commutative, so the
			// left-to-right accumulation here is bit-identical.
			sum := rowW[keys[j]&0xffffffff]
			j++
			for j < len(keys) && int32(keys[j]>>32) == target {
				sum += rowW[keys[j]&0xffffffff]
				j++
			}
			adj[write] = target
			w[write] = sum
			write++
		}
	}
	c.off[n] = write

	if write == rawTotal {
		c.adj, c.w = adj, w
		return c
	}
	c.adj = make([]int32, write)
	c.w = make([]float64, write)
	copy(c.adj, adj[:write])
	copy(c.w, w[:write])
	return c
}

// BuildGraph constructs a weighted directed graph plus an index mapping
// string entity IDs to gonum int64 node IDs. Edge weight follows the spec:
//
//	weight = max(1, callsite_count) * confidence
//
// with both properties drawn from Relationship.Properties (string-typed).
//
// The CSR is built FIRST and the gonum edge set is then materialised from it
// (#5954 S5/S6). The obvious ordering — build g by walking rels, then derive the
// CSR by walking rels again — reads every relationship's properties twice, and
// Relationship.PropsSnapshot() materialises a fresh map[string]string on every
// call. On a 60k-entity fixture whose relationships actually carry
// callsite_count/confidence (as corpus relationships do) that cost 31.6% of
// BuildGraph's wall time; it is invisible on a fixture with no properties,
// which is how it initially escaped review. Building in this order reads them
// exactly once, and additionally spares gonum the remove-and-reinsert churn
// that parallel edges used to cause, because the CSR arrives pre-collapsed.
//
// The two representations are therefore no longer independent derivations.
// legacyBuildGraph in algorithms_csr_legacy_test.go preserves the original
// walk-rels-into-gonum implementation verbatim, and
// TestDirectedCSRMatchesGonumAdjacencyBitForBit checks BOTH the CSR and g
// against it, so the equivalence claim still rests on an independent oracle.
func BuildGraph(entities []Entity, rels []Relationship) (*simple.WeightedDirectedGraph, *nodeIndex) {
	g := simple.NewWeightedDirectedGraph(0, 0)
	idx := newNodeIndex()

	// Insert every entity as a node so isolated nodes still receive scores.
	for _, e := range entities {
		nid := idx.get(e.ID)
		if g.Node(nid) == nil {
			g.AddNode(simple.Node(nid))
		}
	}

	// Derive the shared directed adjacency ONCE. Every downstream consumer of
	// adjacency reads this instead of re-walking g's interface-returning edge
	// iterator. See directedCSR.
	idx.csr = buildDirectedCSR(idx, rels)

	// Materialise the gonum edge set from the CSR. Self-loops are already gone
	// and parallel edges already collapsed, so this is one SetWeightedEdge per
	// surviving edge with no lookup, no RemoveEdge and no re-accumulation.
	for u := int32(0); u < int32(idx.csr.n); u++ {
		lo, hi := idx.csr.row(u)
		for p := lo; p < hi; p++ {
			g.SetWeightedEdge(g.NewWeightedEdge(
				simple.Node(int64(u)), simple.Node(int64(idx.csr.adj[p])), idx.csr.w[p]))
		}
	}
	return g, idx
}

// CommunityInputHash returns a stable content hash over the COMMUNITY-RELEVANT
// input graph that BuildGraph would construct from (entities, rels): the set of
// node ids (every entity, including isolated ones — they still receive scores)
// plus the accumulated directed weighted edge set (endpoints both in the node
// set, self-loops dropped, parallel edges weight-accumulated — exactly the
// transformation BuildGraph applies). The hash is the COMPLETE determinant of
// the deterministic Pass-4 output (community partition + integer labels +
// PageRank + betweenness): the gonum node-id assignment is a pure function of
// entity insertion order, and louvainPartition is a deterministic function of
// the node/edge set with no PRNG at all, so two unions with the same
// CommunityInputHash produce byte-identical AlgorithmResults.
//
// Scope note: "byte-identical" is a claim about two runs of the SAME grafel
// build. It is not a claim across builds — a change to the community algorithm
// itself (as in the #5954 replacement of gonum's community.Modularize with the
// in-repo Louvain in louvain.go) re-partitions the graph without changing this
// hash. That is by design: the hash covers the INPUT, not the algorithm
// version. Any such change therefore requires a full recompute of stored
// overlays even where the hash matches, and is gated on partition quality
// (modularity), not on equality with the previously stored partition.
//
// This is the gate for incremental community detection (#5309 layer 4): when a
// reindex leaves this hash unchanged (docs/comment/config edits, or any change
// that touches neither a node nor a community-graph edge), the prior overlay is
// exactly what a full recompute would yield, so the recompute can be SKIPPED
// while maintaining strict parity with a full rebuild. When the hash changes, a
// full deterministic recompute runs (CPU-bounded by the daemon-wide ceiling,
// #5602).
//
// The hash is order-independent (node ids and accumulated edges are both
// sorted before hashing) so it depends only on the graph CONTENT, not on the
// order entities/rels happen to arrive in — a re-sort of the same union yields
// the same hash. Edge weights are rendered at the same determinism rounding
// the algorithm layer uses so float jitter never spuriously invalidates.
func CommunityInputHash(entities []Entity, rels []Relationship) string {
	// Node set: mirror BuildGraph — every entity id is a node.
	nodes := make(map[string]struct{}, len(entities))
	nodeIDs := make([]string, 0, len(entities))
	for i := range entities {
		id := entities[i].ID
		if _, ok := nodes[id]; ok {
			continue // BuildGraph de-dups via AddNode-if-absent
		}
		nodes[id] = struct{}{}
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	// Edge set: accumulate weights for parallel (from,to) edges exactly as
	// BuildGraph does, keyed by the directed (from,to) pair. Endpoints must both
	// be nodes; self-loops are dropped.
	type edgeKey struct{ from, to string }
	weights := make(map[edgeKey]float64, len(rels))
	for i := range rels {
		r := &rels[i]
		if r.FromID == "" || r.ToID == "" {
			continue
		}
		if _, ok := nodes[r.FromID]; !ok {
			continue
		}
		if _, ok := nodes[r.ToID]; !ok {
			continue
		}
		if r.FromID == r.ToID {
			continue // self-loops rejected by the simple graph
		}
		weights[edgeKey{r.FromID, r.ToID}] += edgeWeight(r.PropsSnapshot())
	}
	edges := make([]string, 0, len(weights))
	for k, w := range weights {
		// Round to the algorithm layer's determinism precision so float jitter
		// in confidence weights never spuriously flips the hash.
		edges = append(edges, fmt.Sprintf("%s\x1f%s\x1f%.6f", k.from, k.to, roundForDeterminism(sanitizeFloat(w))))
	}
	sort.Strings(edges)

	h := sha256.New()
	// Length-prefix each section so a node id can never alias an edge string.
	fmt.Fprintf(h, "nodes:%d\n", len(nodeIDs))
	for _, id := range nodeIDs {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	fmt.Fprintf(h, "edges:%d\n", len(edges))
	for _, e := range edges {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func edgeWeight(props map[string]string) float64 {
	calls := 1
	if v, ok := props["callsite_count"]; ok {
		if n := atoiSafe(v); n > 1 {
			calls = n
		}
	}
	conf := 1.0
	if v, ok := props["confidence"]; ok {
		if f := atofSafe(v); f > 0 {
			conf = f
		}
	}
	return float64(calls) * conf
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func atofSafe(s string) float64 {
	// Tiny non-locale-aware parser to avoid pulling strconv panics on
	// adversarial input. Returns 0 on parse failure (caller falls back to 1).
	if s == "" {
		return 0
	}
	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	} else if s[0] == '+' {
		i = 1
	}
	whole, frac, fracDiv := 0.0, 0.0, 1.0
	seenDot := false
	for ; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if seenDot {
				return 0
			}
			seenDot = true
			continue
		}
		if c < '0' || c > '9' {
			return 0
		}
		if seenDot {
			frac = frac*10 + float64(c-'0')
			fracDiv *= 10
		} else {
			whole = whole*10 + float64(c-'0')
		}
	}
	v := whole + frac/fracDiv
	if neg {
		v = -v
	}
	return v
}

// ComputeCommunities runs Louvain modularity maximisation on the undirected
// projection of g. Returns:
//   - per-community summary (size, modularity contribution, top entity names)
//   - mapping from entity ID -> community id (community_id=-1 for ungrouped)
//   - overall modularity score
//   - number of raw communities that were denoised (below opts.MinSize)
//
// Denoise: communities with fewer than opts.MinSize nodes are removed from the
// result slice and their members are assigned community_id=-1 ("ungrouped").
// This prevents singleton/micro-community noise from reaching the MCP surface
// and the dashboard. Set opts.MinSize=1 to disable denoising.
func ComputeCommunities(idx *nodeIndex, entityNames []string, opts CommunityOptions) ([]CommunityResult, map[string]int, float64, int) {
	// Undirected projection, derived ONCE from the shared directed CSR
	// BuildGraph already built (#5954 S5/S6). This replaced a
	// simple.WeightedUndirectedGraph that existed only to be walked straight
	// back out again by buildCSRFromUndirected: two boxed edge iterations plus
	// a full map-of-maps copy of the edge set, for a projection that is one
	// pass over flat arrays.
	ucsr, ids := buildCSRFromDirected(idx)

	// In-repo Louvain (louvain.go). No PRNG: node order, adjacency order and
	// tie-breaks are all fixed, so the partition is deterministic by
	// construction rather than by seeding. See #5954 / louvain.go for why
	// gonum's community.Modularize was replaced.
	const resolution = 1.0
	groups, _ := louvainPartitionFromCSR(ucsr, ids, resolution)

	// Issue #633 phase-2 — pprof showed `community.Q` accounted for ~90% of
	// indexing allocations (21.6 GB on client-fixture-b: 9,549 communities ×
	// per-call O(|V|+|E|) iteration over the undirected graph). Each Q call
	// re-builds the weighted-degree table `k[]` and scans every node. We
	// replace ALL gonum Q calls with a single pre-computed pass:
	//
	//   1. Compute k[uid] (weighted degree) and m2 = Σ k[u] in one sweep.
	//   2. For each community, accumulate internal edge weight and ΣK in
	//      O(|E_C|) using a node→community map (built below).
	//   3. Per-community contribution: q_c = (2*internal_w - K_C^2/m2) / m2
	//      (BuildGraph drops self-loops, so the diagonal term collapses to 0
	//      and gonum's "2*w_uv for u<v" off-diagonal sum becomes 2*internal_w).
	//   4. Overall Q = Σ q_c — matches the textbook modularity of `groups`
	//      to the rounding tolerance enforced by roundForDeterminism().
	//
	// The per-node stats are three flat slices indexed by node id rather than a
	// map[int64]*nodeStat: node ids are dense 0..n-1 (BuildGraph assigns them),
	// so the map was ~430k entries of pointer-to-heap-struct for data that fits
	// in three contiguous arrays.
	n := ucsr.n
	nodeK := make([]float64, n)
	nodeDeg := make([]int32, n)
	nodeCID := make([]int32, n)
	for i := range nodeCID {
		nodeCID[i] = -1
	}
	for cid, gg := range groups {
		for _, nid := range gg {
			nodeCID[nid] = int32(cid)
		}
	}
	// Walk every undirected edge once: contribute weight to each endpoint's
	// `k` (weighted degree) and, when both endpoints share a community, to
	// that community's internal-weight accumulator.
	//
	// Each unordered pair appears in both endpoints' CSR rows; the `v > u`
	// filter visits it exactly once, in ascending (u, v) order. The previous
	// version walked gonum's edge iterator, whose order is map-derived and
	// therefore differed run to run — so this loop is strictly MORE
	// deterministic than what it replaces, at the cost that a float sum here
	// may land on a different last ulp than any one prior run happened to
	// produce. roundForDeterminism() quantises the published value well above
	// that.
	internalW := make([]float64, len(groups))
	var m2 float64
	for u := int32(0); u < int32(n); u++ {
		for p := ucsr.off[u]; p < ucsr.off[u+1]; p++ {
			v := ucsr.adj[p]
			if v <= u {
				continue
			}
			w := ucsr.w[p]
			nodeK[u] += w
			nodeK[v] += w
			nodeDeg[u]++
			nodeDeg[v]++
			m2 += 2 * w // undirected: each edge contributes 2 to Σ k.
			if nodeCID[u] >= 0 && nodeCID[u] == nodeCID[v] {
				internalW[nodeCID[u]] += w
			}
		}
	}

	// Per-community K_C = Σ k[u] for u in c.
	K := make([]float64, len(groups))
	for cid, gg := range groups {
		var k float64
		for _, nid := range gg {
			k += nodeK[nid]
		}
		K[cid] = k
	}

	// Compute per-community q_c and overall Q analytically.
	var overallQRaw float64
	communityQ := make([]float64, len(groups))
	if m2 > 0 {
		for cid := range groups {
			// q_c = (2*internal_w_c - resolution * K_c^2 / m2) / m2
			q := (2*internalW[cid] - resolution*K[cid]*K[cid]/m2) / m2
			communityQ[cid] = q
			overallQRaw += q
		}
	}
	overallQ := roundForDeterminism(sanitizeFloat(overallQRaw))

	communityOf := make(map[string]int, idx.next)
	// Default every node into community -1; the partition lists only nodes
	// present in the undirected projection, but we want a value for entities
	// that never made it into the graph too.
	for sid := range idx.toInt {
		communityOf[sid] = -1
	}
	results := make([]CommunityResult, 0, len(groups))

	for cid, g := range groups {
		// Sort member nodes by degree (proxy for "top entity") — degree of an
		// undirected weighted graph is best approximated by edge count.
		type member struct {
			id     int64
			degree int
		}
		members := make([]member, 0, len(g))
		for _, nid := range g {
			communityOf[idx.fromInt[nid]] = cid
			members = append(members, member{nid, int(nodeDeg[nid])})
		}
		// Issue #481 — degree ties were resolved by map-iteration order
		// (g.Nodes / und.From); tiebreak on the gonum int64 node id so
		// TopEntities is reproducible.
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].degree != members[j].degree {
				return members[i].degree > members[j].degree
			}
			return members[i].id < members[j].id
		})

		topN := 5
		if topN > len(members) {
			topN = len(members)
		}
		top := make([]string, 0, topN)
		for k := 0; k < topN; k++ {
			eid := idx.fromInt[members[k].id]
			name := nameOf(entityNames, members[k].id)
			if name == "" {
				name = eid
			}
			top = append(top, name)
		}

		cQ := roundForDeterminism(sanitizeFloat(communityQ[cid]))

		results = append(results, CommunityResult{
			ID:          cid,
			Size:        len(g),
			Modularity:  cQ,
			TopEntities: top,
		})
	}
	// Issue #481 — tiebreak Size-equal communities on the integer community
	// id assigned by the partition (ascending lowest-member node index) so
	// result ordering is stable across runs.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Size != results[j].Size {
			return results[i].Size > results[j].Size
		}
		return results[i].ID < results[j].ID
	})

	// Issue #1382 — denoise: drop communities below MinSize into the
	// "ungrouped" bucket (community_id = -1). This eliminates singleton and
	// micro-community noise that inflates community counts and pollutes the MCP
	// and dashboard surfaces. The graph topology (edges, centrality, PageRank)
	// is unaffected; only the community membership label changes.
	minSize := opts.MinSize
	if minSize < 1 {
		minSize = 1 // safety: never discard everything
	}
	var denoised int
	if minSize > 1 {
		kept := results[:0]
		for _, r := range results {
			if r.Size >= minSize {
				kept = append(kept, r)
			} else {
				denoised++
				// Remap members to ungrouped (-1).
				for eid, cid := range communityOf {
					if cid == r.ID {
						communityOf[eid] = -1
					}
				}
			}
		}
		results = kept
	}

	return results, communityOf, overallQ, denoised
}

// ComputeCentrality returns betweenness centrality and PageRank, both keyed by
// the original string entity IDs.
//
// Betweenness uses gonum's BetweennessWeighted for graphs with at most
// betweennessExactCutoff nodes. Above that threshold we fall back to the
// unweighted Brandes implementation (much cheaper) and document the trade-off.
const betweennessExactCutoff = 3000

// betweennessSampleThreshold is the node count above which betweenness switches
// from exact (full Brandes, O(V·E)) to a sampled-pivot approximation
// (O(K·E), K = betweennessSampleSize). On 28k+-entity group unions (#5349 A4,
// plan §4 risk 1) exact betweenness is minutes-scary; sampling preserves the
// important nodes (god-node tier) at a fraction of the cost. PageRank and
// community detection stay EXACT every pass (decision Q1); only betweenness
// samples above this threshold.
//
// Override at runtime with GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD (a positive
// integer). A value of 0 or a parse failure falls back to the default.
const betweennessSampleThreshold = 8000

// betweennessSampleSize is the number of random pivot sources K used by the
// sampled Brandes approximation. The estimator sums single-source dependencies
// from K pivots and scales by V/K (standard Brandes-sampling, Bader et al.).
// 512 pivots gives a tight top-K ranking estimate on sparse code graphs.
const betweennessSampleSize = 512

// betweennessSampleSeed is the fixed PCG seed for pivot selection so the
// sampled approximation is byte-reproducible across runs of the same graph
// (mirrors the fixed-seed determinism elsewhere in this file).
const betweennessSampleSeed = 0x5349

// betweennessSampleThresholdValue returns the node-count threshold above which
// betweenness is sampled, honouring the GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD
// override. Factored out so tests can assert the gate and the env override.
func betweennessSampleThresholdValue() int {
	if v := os.Getenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD"); v != "" {
		if n := atoiSafe(v); n > 0 {
			return n
		}
	}
	return betweennessSampleThreshold
}

// BetweennessSampleThreshold exports the effective betweenness-sampling node
// threshold (honouring GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD) so out-of-package
// consumers (e.g. the group-algo differential validator) can report whether a
// group was large enough to trigger the sampled approximation.
func BetweennessSampleThreshold() int { return betweennessSampleThresholdValue() }

// betweennessPath names the betweenness computation strategy ComputeCentrality
// selects for a graph of a given size. It exists as an explicit value so the
// choice is unit-testable and loggable without timing the run.
type betweennessPath int

const (
	// betweennessPathExactWeighted — FloydWarshall + weighted Betweenness.
	// Most accurate, O(V^3); only viable on tiny graphs (<= betweennessExactCutoff).
	betweennessPathExactWeighted betweennessPath = iota
	// betweennessPathExactBrandes — gonum's unweighted Brandes, O(V·E). Exact
	// but the enrichment-bound cost on large graphs; used for mid-size graphs
	// (between the FloydWarshall cutoff and the sampling threshold) and whenever
	// exact computation is forced via GRAFEL_BETWEENNESS_FORCE_EXACT.
	betweennessPathExactBrandes
	// betweennessPathSampled — deterministic K-source sampled approximation
	// (sampledBetweenness); used above the sampling threshold to bound cost on
	// very large graphs (#5692).
	betweennessPathSampled
)

func (p betweennessPath) String() string {
	switch p {
	case betweennessPathExactWeighted:
		return "exact-weighted"
	case betweennessPathExactBrandes:
		return "exact-brandes"
	case betweennessPathSampled:
		return "sampled"
	default:
		return "unknown"
	}
}

// chooseBetweennessPath selects the betweenness strategy purely from sizes and
// the force-exact flag, so the gate is testable in isolation (#5692).
//
//   - forceExact=true  -> never sample; pick exact-weighted (<= exactCutoff) or
//     exact-brandes. This is the operator opt-out for large graphs that need
//     exact centrality and accept the O(V·E) cost.
//   - nodes > sampleThreshold (>0) -> sampled approximation.
//   - nodes <= exactCutoff         -> exact-weighted (FloydWarshall).
//   - otherwise                    -> exact-brandes.
//
// For nodes <= sampleThreshold the result is IDENTICAL to the pre-#5692 code,
// preserving the hard "small graphs unchanged" constraint.
func chooseBetweennessPath(nodes, exactCutoff, sampleThreshold int, forceExact bool) betweennessPath {
	if !forceExact && sampleThreshold > 0 && nodes > sampleThreshold {
		return betweennessPathSampled
	}
	if nodes <= exactCutoff {
		return betweennessPathExactWeighted
	}
	return betweennessPathExactBrandes
}

// betweennessForceExact reports whether GRAFEL_BETWEENNESS_FORCE_EXACT requests
// that betweenness always be computed exactly (the pre-sampling behaviour),
// bypassing the node-count sampling gate (#5692 opt-out). Any of the usual
// truthy spellings (1/true/yes/on) enable it.
func betweennessForceExact() bool {
	return envTruthy(os.Getenv("GRAFEL_BETWEENNESS_FORCE_EXACT"))
}

// envTruthy interprets an env-var value as a boolean without pulling strconv.
func envTruthy(v string) bool {
	switch v {
	case "1", "t", "T", "true", "TRUE", "True", "yes", "YES", "Yes", "on", "ON", "On":
		return true
	}
	return false
}

// logBetweennessPath emits a single stderr line recording which betweenness path
// ran, so operators can confirm on large graphs whether the sampled
// approximation (or a forced-exact override) was taken (#5692). It does not
// affect on-disk output bytes — reproducible-build mode governs artifact
// content, not process logs.
func logBetweennessPath(p betweennessPath, nodes int) {
	fmt.Fprintf(os.Stderr,
		"grafel: betweenness path=%s nodes=%d sample_threshold=%d force_exact=%v\n",
		p, nodes, betweennessSampleThresholdValue(), betweennessForceExact())
}

func ComputeCentrality(g *simple.WeightedDirectedGraph, idx *nodeIndex) (map[string]float64, map[string]float64) {
	// Betweenness — choose exact-weighted vs unweighted vs sampled by size.
	// The selection is factored into chooseBetweennessPath so it is unit-testable
	// and so operators can force exact computation via GRAFEL_BETWEENNESS_FORCE_EXACT
	// (#5692 opt-out). Below the sampling threshold the behaviour is IDENTICAL to
	// the pre-#5692 code; only very large graphs take the sampled path.
	nodes := int(idx.next)
	bpath := chooseBetweennessPath(nodes, betwennessNodeCount(idx), betweennessSampleThresholdValue(), betweennessForceExact())
	logBetweennessPath(bpath, nodes)

	var raw map[int64]float64
	switch bpath {
	case betweennessPathSampled:
		// Large group union (#5349 A4 / #5692): exact Brandes is O(V·E) and the
		// enrichment-bound cost (~240s on a 291k-node graph). Use the
		// deterministic sampled-pivot approximation.
		raw = sampledBetweenness(g, idx.csr, betweennessSampleSize, betweennessSampleSeed)
	case betweennessPathExactWeighted:
		// FloydWarshall is O(V^3) and precomputes all shortest paths; on
		// graphs <= cutoff this is the most accurate option.
		shortest, ok := path.FloydWarshall(g)
		if ok {
			raw = network.BetweennessWeighted(g, shortest)
		}
	}
	// betweennessPathExactBrandes (and any FloydWarshall failure above) falls
	// through to the unweighted Brandes exact computation.
	if raw == nil {
		raw = network.Betweenness(g)
	}
	// #5954 — betweenness is SPARSE: gonum (and sampledBetweenness) only report
	// nodes with a non-zero score, so this map is sized to the reported set, not
	// to the entity count. Callers must read an absent key as 0 (Go's zero value
	// for a map read), which is what every consumer does — see
	// identifyGodNodesFrom below, groupalgo.BuildOverlay, and the two
	// entity-attachment loops (cmd/grafel/index.go, dashboard.applyAlgorithmResults)
	// that gate on PageRank membership.
	betw := make(map[string]float64, len(raw))
	for nid, v := range raw {
		// Zero scores are dropped, not stored: that makes "absent" the single
		// representation of a zero betweenness, so a reconstituted result (see
		// groupalgo.overlayToResults) is comparable to a freshly computed one.
		if rv := roundForDeterminism(sanitizeFloat(v)); rv != 0 {
			betw[idx.fromInt[nid]] = rv
		}
	}

	// PageRank requires a directed graph — use g directly. damping=0.85,
	// tolerance=1e-6 per spec.
	//
	// Issue #633 phase-2 — pprof showed `network.PageRank` allocates a dense
	// N×N transition matrix via `mat.NewDense` (~1.74 GB live for 15k nodes
	// on client-fixture-b). `network.PageRankSparse` solves the SAME fixed
	// point with identical damping/tolerance using a sparse row-compressed
	// matrix proportional to |E|. Both gonum variants use the same un-seeded
	// init vector and converge to the same scores; roundForDeterminism()
	// rounds to 1e-4 (well above the 1e-6 solver tolerance) so the on-disk
	// bytes stay stable. Always use sparse — code graphs are sparse by nature.
	//
	// PageRank is DENSE: PageRankSparse returns a score for every node in g, and
	// BuildGraph inserts a node per entity, so `pr` still has one entry per
	// entity. Consumers that need the full entity set (the overlay writer's
	// fold-in loop, the entity-attachment loops) key off this map.
	prRaw := network.PageRankSparse(g, 0.85, 1e-6)
	pr := make(map[string]float64, len(prRaw))
	for nid, v := range prRaw {
		pr[idx.fromInt[nid]] = roundForDeterminism(sanitizeFloat(v))
	}
	return betw, pr
}

// sampledBetweenness approximates betweenness centrality on a directed graph
// using K random pivot sources (Brandes-sampling, Bader/Brandes-Pich). For each
// pivot s it runs a single-source unweighted BFS, builds the shortest-path DAG
// (sigma counts + predecessor lists in non-decreasing distance order), then
// back-accumulates dependencies delta exactly as in Brandes' algorithm. The sum
// of single-source dependencies over K pivots is an unbiased estimator of the
// full betweenness scaled by K/V; we rescale by V/K so the magnitudes match the
// exact directed Brandes output and the god-node tier (top-K ranking) is
// preserved (acceptance: top-50 overlap >= 0.9 vs exact on mid-size graphs).
//
// Pivot selection uses a fixed PCG seed so the approximation is byte-stable
// across runs of the same graph (the on-disk determinism contract, #481).
// Unweighted shortest paths are used (matching network.Betweenness, the exact
// fallback above the FloydWarshall cutoff) so the comparison is apples-to-apples.
// csr is the shared directed adjacency from BuildGraph; when it covers this
// graph the BFS reads it directly and no edge iteration happens at all. Pass
// nil for a hand-built graph (tests), which falls back to deriving adjacency
// from g's edge iterator.
func sampledBetweenness(g *simple.WeightedDirectedGraph, csr *directedCSR, k int, seed uint64) map[int64]float64 {
	nodes := gonumgraph.NodesOf(g.Nodes())
	v := len(nodes)
	cb := make(map[int64]float64, v)
	if v == 0 {
		return cb
	}
	ids := make([]int64, v)
	for i, n := range nodes {
		ids[i] = n.ID()
	}
	// Deterministic order so the seeded pivot draw is reproducible regardless
	// of gonum's internal node-map iteration order.
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	if k > v {
		k = v
	}
	// Deterministic pivot sample without replacement via a seeded Fisher-Yates
	// partial shuffle over a copy of the sorted ids.
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // reproducibility, not security
	perm := make([]int64, v)
	copy(perm, ids)
	for i := 0; i < k; i++ {
		j := i + int(rng.Uint64N(uint64(v-i)))
		perm[i], perm[j] = perm[j], perm[i]
	}
	pivots := perm[:k]

	// Dense successor adjacency, built once. Node i is the i-th entry of the
	// ascending-id `ids` slice, so adjacency sorted by dense index is the same
	// order as the previous sort-by-node-id — every float summation below
	// therefore happens in exactly the order the map-based version used.
	sc := newBCScratch(g, ids, csr)

	for _, s := range pivots {
		sc.accumulatePivot(sc.dense.of(s), cb, ids)
	}

	// Rescale so sampled magnitudes match full Brandes (V/K estimator scale).
	scale := float64(v) / float64(k)
	for id := range cb {
		cb[id] *= scale
	}
	return cb
}

// denseIDMap maps a gonum node id to its index in an ascending-sorted id slice.
//
// BuildGraph hands out node ids sequentially from 0, so in production ids[i]==i
// and the lookup is a subtraction. The map fallback covers any caller that
// supplies a sparse id space (e.g. a graph built by hand in a test).
type denseIDMap struct {
	contiguous bool
	base       int64
	byID       map[int64]int32
}

func newDenseIDMap(ids []int64) *denseIDMap {
	d := &denseIDMap{contiguous: true}
	if len(ids) == 0 {
		return d
	}
	d.base = ids[0]
	for i, id := range ids {
		if id != d.base+int64(i) {
			d.contiguous = false
			break
		}
	}
	if !d.contiguous {
		d.byID = make(map[int64]int32, len(ids))
		for i, id := range ids {
			d.byID[id] = int32(i)
		}
	}
	return d
}

func (d *denseIDMap) of(id int64) int32 {
	if d.contiguous {
		return int32(id - d.base)
	}
	return d.byID[id]
}

// bcScratch is the reusable working set for sampled Brandes. Every field is
// allocated ONCE for the whole K-pivot run; a pivot mutates it and then restores
// only the entries it touched. This is the #5954 fix: the previous version
// allocated four V-hinted maps plus a V-capacity stack PER PIVOT, which on the
// reference corpus (V=427k, K=512) churned ~25 GB to produce a 9 MB result.
//
// Representation choices, all of which preserve the exact arithmetic order of
// the map-based original:
//
//   - dist is generation-stamped (`gen`/`curGen`) so it is never cleared: a
//     dist entry is meaningful only when gen[i] == curGen, which makes "has this
//     pivot's BFS seen node i?" an O(1) test with no per-pivot O(V) reset.
//   - sigma/delta are flat float64 slices indexed by dense node index instead of
//     map[int64]float64.
//   - predecessors are a linked list over the CSR edge slots (predHead/predNext/
//     predNode) instead of a map of append-grown slices.
//   - the BFS queue and the Brandes stack are the same sequence (dequeue order
//     == the order nodes are pushed), so one slice with a read cursor serves as
//     both.
//
// The two EXACT betweenness paths (betweennessPathExactWeighted and
// betweennessPathExactBrandes) are gonum's own implementations and deliberately
// do NOT share this scratch: they run only below betweennessSampleThreshold
// (8000 nodes) where the per-source allocation is not the bottleneck, and
// rewriting them would put their output — which this change keeps byte-identical
// — at risk for no memory benefit.
type bcScratch struct {
	n   int
	off []int32 // len n+1, CSR row offsets into adj
	adj []int32 // len off[n], successor dense indices, ascending within a row

	dist   []int32  // valid only where gen[i] == curGen
	gen    []uint32 // generation stamp per node
	curGen uint32

	sigma []float64 // path counts; reset to 0 for touched nodes after each pivot
	delta []float64 // dependencies; reset to 0 for touched nodes after each pivot

	stack []int32 // BFS visit order; doubles as the back-accumulation stack

	predHead []int32 // len n, -1 == empty; head slot of the predecessor list
	predNext []int32 // len off[n], next slot in the predecessor list, -1 == end
	predNode []int32 // len off[n], the predecessor's dense index
	predCnt  int32   // slots used by the current pivot

	// dense maps gonum node ids to dense indices; retained so callers can
	// translate a pivot id without rebuilding the mapping.
	dense *denseIDMap

	// sharedCSR records whether off/adj alias the directedCSR BuildGraph
	// derived (true) or were re-derived here from g's edge iterator (false).
	// Production must always be true; the false branch exists only for
	// hand-built test graphs. Asserted by
	// TestSampledBetweennessUsesSharedCSROnTheBuildGraphPath.
	sharedCSR bool
}

// newBCScratch builds the dense successor CSR and allocates all per-run scratch.
// ids must be ascending; dense index i corresponds to gonum node ids[i].
// csr, when non-nil and dimensionally compatible with ids, is used DIRECTLY as
// the successor adjacency: it already has exactly the shape this needs (row
// offsets plus ascending-sorted targets, dense index == gonum node id), so the
// slices are aliased rather than copied and g's edge iterator is never touched.
// That is the #5954 S5/S6 win — the 1.33M-edge boxing walk this function used
// to perform is gone on the production path.
func newBCScratch(g *simple.WeightedDirectedGraph, ids []int64, csr *directedCSR) *bcScratch {
	n := len(ids)
	sc := &bcScratch{
		n:        n,
		dense:    newDenseIDMap(ids),
		dist:     make([]int32, n),
		gen:      make([]uint32, n),
		sigma:    make([]float64, n),
		delta:    make([]float64, n),
		stack:    make([]int32, 0, n),
		predHead: make([]int32, n),
	}
	if n == 0 {
		sc.off = make([]int32, 1)
		return sc
	}

	// The shared CSR is addressed by gonum node id, so it is usable only when
	// this graph's id space is exactly 0..n-1 — which is what BuildGraph hands
	// out. Any other id space (hand-built test graphs) takes the fallback.
	if csr != nil && csr.n == n && sc.dense.contiguous && sc.dense.base == 0 {
		sc.sharedCSR = true
		sc.off = csr.off
		sc.adj = csr.adj
		total := csr.off[n]
		sc.predNext = make([]int32, total)
		sc.predNode = make([]int32, total)
		for i := range sc.predHead {
			sc.predHead[i] = -1
		}
		return sc
	}

	sc.off = make([]int32, n+1)
	// Fallback: materialise the edge list ONCE via a single global iterator.
	// Calling g.From(id) per node instead would allocate one gonum iterator per
	// node per pass — 2V allocations and hundreds of MB of churn at corpus
	// scale.
	dense := sc.dense
	type edge struct{ u, v int32 }
	var edges []edge
	if it := g.Edges(); it != nil {
		edges = make([]edge, 0, it.Len())
		for it.Next() {
			e := it.Edge()
			edges = append(edges, edge{dense.of(e.From().ID()), dense.of(e.To().ID())})
		}
	}

	for i := range edges {
		sc.off[edges[i].u+1]++
	}
	for i := 0; i < n; i++ {
		sc.off[i+1] += sc.off[i]
	}
	total := sc.off[n]
	sc.adj = make([]int32, total)
	sc.predNext = make([]int32, total)
	sc.predNode = make([]int32, total)

	cursor := make([]int32, n)
	copy(cursor, sc.off[:n])
	for i := range edges {
		e := edges[i]
		sc.adj[cursor[e.u]] = e.v
		cursor[e.u]++
	}
	// gonum's edge iteration order is map-derived and unstable, so each row is
	// sorted by dense index (== ascending node id). slices.Sort, not sort.Slice:
	// the latter allocates a closure and a reflect-based swapper per row, which
	// at corpus scale is V extra allocations.
	for i := 0; i < n; i++ {
		slices.Sort(sc.adj[sc.off[i]:sc.off[i+1]])
	}

	for i := range sc.predHead {
		sc.predHead[i] = -1
	}
	return sc
}

// accumulatePivot runs one single-source Brandes pass from pivot s (a dense
// index), adding its dependency contributions into cb (keyed by gonum node id),
// then restores the scratch so the next pivot starts from a clean state.
//
// The float arithmetic is identical to the map-based original:
//   - sigma[w] accumulates over predecessors in BFS-discovery order, which is
//     fixed by (queue order, ascending-successor order) — unchanged here.
//   - in the back-accumulation, a given w contributes to delta[p] for each of
//     its predecessors p; the graph is simple, so p appears at most ONCE in
//     pred[w] and each delta[p] receives exactly one += per w. Predecessor
//     iteration order therefore cannot change any sum, only which distinct
//     accumulator is touched. The order of w itself (reverse stack) is
//     unchanged, so delta[p] sees its addends in the same sequence as before.
func (sc *bcScratch) accumulatePivot(s int32, cb map[int64]float64, ids []int64) {
	if sc.n == 0 || int(s) >= sc.n {
		return
	}
	sc.curGen++
	if sc.curGen == 0 {
		// Generation counter wrapped: clear the stamps so no stale entry can
		// alias generation 0. Happens at most once per 4 billion pivots.
		for i := range sc.gen {
			sc.gen[i] = 0
		}
		sc.curGen = 1
	}
	gen := sc.curGen
	sc.predCnt = 0
	sc.stack = sc.stack[:0]

	sc.dist[s] = 0
	sc.gen[s] = gen
	sc.sigma[s] = 1
	sc.stack = append(sc.stack, s)

	// BFS. `head` is the queue read cursor into stack; stack grows as nodes are
	// enqueued, so stack[head:] is exactly the pending queue.
	for head := 0; head < len(sc.stack); head++ {
		cur := sc.stack[head]
		dcur := sc.dist[cur]
		scur := sc.sigma[cur]
		for p := sc.off[cur]; p < sc.off[cur+1]; p++ {
			w := sc.adj[p]
			if sc.gen[w] != gen {
				sc.gen[w] = gen
				sc.dist[w] = dcur + 1
				sc.stack = append(sc.stack, w)
			}
			if sc.dist[w] == dcur+1 {
				sc.sigma[w] += scur
				// Prepend cur onto w's predecessor list. Order within the list
				// is irrelevant to the sums (see the doc comment above).
				slot := sc.predCnt
				sc.predCnt++
				sc.predNode[slot] = cur
				sc.predNext[slot] = sc.predHead[w]
				sc.predHead[w] = slot
			}
		}
	}

	// Back-accumulation, then per-node cleanup in the same sweep.
	for i := len(sc.stack) - 1; i >= 0; i-- {
		w := sc.stack[i]
		sw := sc.sigma[w]
		// Kept in the ORIGINAL (sigma[p]/sigma[w]) * (1+delta[w]) form. Hoisting
		// the division out as (1+delta[w])/sigma[w] would be algebraically equal
		// but NOT bit-identical, and this feeds a persisted ranking.
		// delta[w] cannot change inside this loop: every p has dist[p] < dist[w],
		// so p != w.
		factor := 1 + sc.delta[w]
		for slot := sc.predHead[w]; slot >= 0; slot = sc.predNext[slot] {
			p := sc.predNode[slot]
			sc.delta[p] += (sc.sigma[p] / sw) * factor
		}
		if w != s {
			cb[ids[w]] += sc.delta[w]
		}
	}
	// Reset only what this pivot touched. dist/gen need no reset (stamped).
	for _, w := range sc.stack {
		sc.sigma[w] = 0
		sc.delta[w] = 0
		sc.predHead[w] = -1
	}
}

// runtimeMSFor returns wall-clock milliseconds elapsed since start, or 0
// when SOURCE_DATE_EPOCH is set so reproducible-builds mode (#481) emits a
// stable byte stream.
func runtimeMSFor(start time.Time) int64 {
	if os.Getenv("SOURCE_DATE_EPOCH") != "" {
		return 0
	}
	return time.Since(start).Milliseconds()
}

// roundForDeterminism rounds a gonum-derived score so the on-disk bytes stay
// stable across runs of the same input, WITHOUT collapsing small scores to 0.
//
// Issue #481 — gonum's PageRank and Betweenness implementations iterate
// over node maps internally, so tiny floating-point reorderings accumulate
// to differences of ~1e-8 across runs of the same input. The PageRank
// solver converges to a tolerance of 1e-6 (see the call site below).
//
// Issue #489 — on larger graphs (gin ~6.4k entities, spdlog ~1.8k entities)
// the accumulated float drift crosses the 1e-5 boundary occasionally,
// causing 2/10 runs to produce different byte output even though the logical
// PageRank ranking is identical. The original fix rounded to a fixed 1e-4
// ABSOLUTE bucket (4 decimal places), whose ~1e-4 quantum sits far above the
// ~1e-6 drift, so it is byte-stable.
//
// Flaw 4 — that absolute 1e-4 bucket is wrong for LARGE GROUP UNIONS (#5349,
// 28k+ entities): PageRank mass sums to 1 across all nodes, so the average
// score is ~1/28000 ≈ 3.6e-5 and even a top-5% god-node's score can be well
// below 1e-4. math.Round(v*1e4)/1e4 then collapses those values to 0,
// producing the contradiction "flagged god-node, pagerank 0".
//
// Fix — a HYBRID quantum:
//   - |v| >= 1e-3: keep the proven 1e-4 ABSOLUTE bucket. These mid/large
//     scores carry drift up to ~1e-6, and the 1e-4 quantum (100× the drift)
//     keeps them byte-stable exactly as before (issue #489 determinism).
//   - |v| < 1e-3: round to 4 SIGNIFICANT figures instead. The quantum then
//     scales DOWN with the value, so a 4e-5 god-node pagerank keeps a ~1e-7
//     quantum — non-zero and well-ordered — while still being far coarser than
//     the proportionally-tiny drift on such small scores (so byte output stays
//     deterministic). This is the only regime large unions exercise.
func roundForDeterminism(v float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	const absoluteFloor = 1e-3 // below this, switch to significant-figure rounding
	if math.Abs(v) >= absoluteFloor {
		const scale = 1e4 // 4 decimal places (the proven #489 determinism bucket)
		return math.Round(v*scale) / scale
	}
	// Significant-figure rounding for small scores: scale so the most-significant
	// digit sits just left of the decimal point, round to (sigFigs-1) fractional
	// digits, then scale back. Relative precision => never zeroes a non-zero value.
	const sigFigs = 4
	exp := math.Floor(math.Log10(math.Abs(v)))
	scale := math.Pow(10, float64(sigFigs-1)-exp)
	return math.Round(v*scale) / scale
}

// betwennessNodeCount is a tiny indirection so we can stub the cutoff in
// tests without relying on a global mutable variable.
func betwennessNodeCount(idx *nodeIndex) int { return betweennessExactCutoff }

// sanitizeFloat scrubs NaN/+Inf/-Inf values to 0 so the JSON encoder doesn't
// reject them. Gonum's modularity computation can produce NaN on degenerate
// inputs (single-node communities, empty edge sets); 0 is the right neutral
// value to surface in the on-disk schema.
func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// identifyGodNodesFrom returns the union of the top-5% entities by betweenness
// AND the top-5% by PageRank, with the candidate universe taken from the node
// index (every entity) rather than from each map's key set.
//
// It replaces the exported IdentifyGodNodes, deleted in #5954: that function
// ranked each map over its own keys, which was the entity universe only while
// both maps were pre-seeded with a zero per entity. The betweenness map is
// sparse now, so its key set is no longer the right denominator and an exported
// entry point taking just the two maps cannot compute the cut correctly. It had
// no callers outside this file.
//
// This reproduces the selection made before #5954, when both maps were
// pre-seeded with a 0 for every entity: the 5% cut is over the entity count,
// and entities with no score rank last, ordered by id — exactly where an
// explicit 0 sorted them. TestGodNodesParityWithZeroPreSeed checks it against a
// verbatim copy of that implementation, including a fixture where the cut is
// filled entirely from the zero tail.
func identifyGodNodesFrom(betw, pr map[string]float64, idx *nodeIndex) map[string]bool {
	universeLen := int(idx.next)
	universe := func() []string { return mapKeys(idx.toInt) }
	out := make(map[string]bool)
	for _, id := range pickTopFraction(betw, universeLen, universe) {
		out[id] = true
	}
	for _, id := range pickTopFraction(pr, universeLen, universe) {
		out[id] = true
	}
	return out
}

// mapKeys collects the keys of m in unspecified order. Callers sort.
func mapKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// pickTopFraction returns the top universeLen/20 (5%, minimum 1) ids ranked by
// score descending with an id-ascending tiebreak, where every id in the universe
// that is absent from m scores 0.
//
// Issue #481 — ties on score were resolved by map-iteration order, so the top-5%
// set flipped between runs; the id tiebreaker pins the membership.
//
// The universe is passed as a size plus a lazy accessor because on the
// RunAlgorithms path it is the full entity set (~430k ids): the id slice is only
// materialised when the positively-scored entries do not already fill the cut,
// which is the small/sparse-graph case. Scores are non-negative in practice, but
// the split is written as ">0 first, then the rest ranked the same way" so it
// stays order-equivalent to ranking one combined slice regardless.
func pickTopFraction(m map[string]float64, universeLen int, universe func() []string) []string {
	k := universeLen / 20 // 5%
	if k == 0 && universeLen > 0 {
		k = 1
	}
	if k == 0 {
		return nil
	}
	type pair struct {
		id string
		v  float64
	}
	byScore := func(ps []pair) func(i, j int) bool {
		return func(i, j int) bool {
			if ps[i].v != ps[j].v {
				return ps[i].v > ps[j].v
			}
			return ps[i].id < ps[j].id
		}
	}

	ps := make([]pair, 0, len(m))
	for id, v := range m {
		if v > 0 {
			ps = append(ps, pair{id, v})
		}
	}
	sort.SliceStable(ps, byScore(ps))
	if len(ps) > k {
		ps = ps[:k]
	}
	out := make([]string, 0, k)
	for _, p := range ps {
		out = append(out, p.id)
	}
	if len(out) == k {
		return out
	}

	// The positive scores did not fill the cut — rank the zero/negative tail.
	ids := universe()
	tail := make([]pair, 0, len(ids)-len(out))
	for _, id := range ids {
		if v := m[id]; v <= 0 {
			tail = append(tail, pair{id, v})
		}
	}
	sort.SliceStable(tail, byScore(tail))
	for i := 0; i < len(tail) && len(out) < k; i++ {
		out = append(out, tail[i].id)
	}
	return out
}

// ComputeSurpriseEdges scans every relationship; an edge whose endpoints sit
// in different communities is a "cross-community" edge. We score surprise as
// 1/frequency of the (commA, commB) pair: a once-only cross is maximally
// surprising. Top 20 by score are returned.
func ComputeSurpriseEdges(rels []Relationship, communityOf map[string]int) []SurpriseEdge {
	type pair struct{ a, b int }
	freq := make(map[pair]int)
	type candidate struct {
		from, to string
		p        pair
	}
	candidates := make([]candidate, 0)

	for _, r := range rels {
		ca, oka := communityOf[r.FromID]
		cb, okb := communityOf[r.ToID]
		if !oka || !okb || ca == cb {
			continue
		}
		// Order pair canonically so direction doesn't fragment frequency.
		p := pair{ca, cb}
		if p.a > p.b {
			p.a, p.b = p.b, p.a
		}
		freq[p]++
		candidates = append(candidates, candidate{r.FromID, r.ToID, p})
	}

	scored := make([]SurpriseEdge, 0, len(candidates))
	for _, c := range candidates {
		f := freq[c.p]
		score := 1.0 / float64(f)
		scored = append(scored, SurpriseEdge{
			FromID: c.from,
			ToID:   c.to,
			Score:  score,
			Reason: "rare_cross_community_pair",
		})
	}
	// Issue #481 — score ties were tiebroken by candidates-slice order,
	// which inherits goroutine-scheduling order through rels. Tiebreak on
	// (FromID, ToID) so the top-20 surface is reproducible.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].FromID != scored[j].FromID {
			return scored[i].FromID < scored[j].FromID
		}
		return scored[i].ToID < scored[j].ToID
	})
	if len(scored) > 20 {
		scored = scored[:20]
	}
	return scored
}

// IdentifyArticulationPoints implements Tarjan's articulation-point algorithm
// on the undirected projection of g. A node u is an articulation point if:
//
//   - u is the root of the DFS tree and has at least two children; OR
//   - u has a child v such that no descendant of v has a back-edge to a
//     proper ancestor of u — i.e. low[v] >= disc[u].
//
// Returns a set of original entity IDs.
func IdentifyArticulationPoints(g *simple.WeightedDirectedGraph, idx *nodeIndex) map[string]bool {
	// Build undirected adjacency from the directed graph.
	adj := make(map[int64][]int64, idx.next)
	nodes := g.Nodes()
	for nodes.Next() {
		adj[nodes.Node().ID()] = nil
	}
	edges := g.Edges()
	seen := make(map[[2]int64]bool)
	for edges.Next() {
		e := edges.Edge()
		u, v := e.From().ID(), e.To().ID()
		if u == v {
			continue
		}
		key := [2]int64{u, v}
		if u > v {
			key = [2]int64{v, u}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		adj[u] = append(adj[u], v)
		adj[v] = append(adj[v], u)
	}

	disc := make(map[int64]int, len(adj))
	low := make(map[int64]int, len(adj))
	parent := make(map[int64]int64, len(adj))
	visited := make(map[int64]bool, len(adj))
	ap := make(map[int64]bool)
	timer := 0

	// Iterative DFS to avoid stack overflow on very large graphs.
	type frame struct {
		u    int64
		i    int
		root bool
	}
	// Issue #481 — DFS root choice is observable in the articulation-point
	// set (the root is an articulation point iff it has >= 2 DFS children,
	// and the children we discover depend on neighbour ordering). Walk the
	// adjacency map in deterministic order: keys sorted ascending, and each
	// neighbour list sorted ascending so the DFS itself is reproducible.
	keys := make([]int64, 0, len(adj))
	for k := range adj {
		sort.Slice(adj[k], func(a, b int) bool { return adj[k][a] < adj[k][b] })
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, start := range keys {
		if visited[start] {
			continue
		}
		stack := []frame{{u: start, i: 0, root: true}}
		visited[start] = true
		disc[start] = timer
		low[start] = timer
		timer++
		parent[start] = -1
		children := 0

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.i < len(adj[top.u]) {
				v := adj[top.u][top.i]
				top.i++
				if !visited[v] {
					parent[v] = top.u
					visited[v] = true
					disc[v] = timer
					low[v] = timer
					timer++
					if top.root {
						children++
					}
					stack = append(stack, frame{u: v, i: 0})
				} else if v != parent[top.u] {
					if disc[v] < low[top.u] {
						low[top.u] = disc[v]
					}
				}
				continue
			}
			// All neighbours of top.u processed — propagate low to parent.
			u := top.u
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				// Root: articulation iff it had >= 2 DFS children.
				if children >= 2 {
					ap[u] = true
				}
				break
			}
			pu := stack[len(stack)-1].u
			if low[u] < low[pu] {
				low[pu] = low[u]
			}
			if low[u] >= disc[pu] && parent[pu] != -1 {
				ap[pu] = true
			}
		}
	}

	out := make(map[string]bool, len(ap))
	for nid := range ap {
		out[idx.fromInt[nid]] = true
	}
	return out
}

// nodeNames returns entity display names indexed by the gonum node id that
// BuildGraph assigned to each entity id — the lookup table ComputeCommunities
// takes. Entities sharing an id collapse onto one slot, last write winning,
// which is how the map[string]string it replaced resolved them too. An entity
// absent from the index (BuildGraph indexes every entity it is given, so only a
// mismatched pair reaches this) is skipped rather than panicking.
func nodeNames(entities []Entity, idx *nodeIndex) []string {
	names := make([]string, idx.next)
	for _, e := range entities {
		if nid, ok := idx.toInt[e.ID]; ok {
			names[nid] = e.Name
		}
	}
	return names
}

// nameOf returns the display name recorded for a gonum node id, or "" when the
// slice does not cover it.
func nameOf(names []string, nid int64) string {
	if nid < 0 || nid >= int64(len(names)) {
		return ""
	}
	return names[nid]
}

// RunAlgorithms executes the full Pass 4 sweep with default options (community
// MinSize=5). It is a convenience wrapper over RunAlgorithmsWithOptions.
func RunAlgorithms(entities []Entity, rels []Relationship) *AlgorithmResults {
	return RunAlgorithmsWithOptions(entities, rels, DefaultCommunityOptions())
}

// RunAlgorithmsWithOptions executes the full Pass 4 sweep and bundles every
// result into AlgorithmResults. opts controls community-detection behaviour
// (e.g. MinSize for denoising). The caller decides how to attach the
// per-entity attributes onto the on-disk Document and where to emit the corpus
// aggregate.
func RunAlgorithmsWithOptions(entities []Entity, rels []Relationship, opts CommunityOptions) *AlgorithmResults {
	// Guard: gonum's PageRankSparse (via ComputeCentrality) calls
	// mat.NewVecDense(0, ...) when the graph has zero nodes, which panics with
	// "mat: zero length in matrix dimension" (gonum/mat vector.go:103).
	// Return an empty-but-valid result immediately so callers get a safe no-op
	// rather than a crash. Tracked in #937 / #1795.
	if len(entities) == 0 {
		return &AlgorithmResults{} //nolint:exhaustruct // zero-entity fast path; all fields intentionally zero
	}

	start := time.Now()

	g, idx := BuildGraph(entities, rels)

	// Community naming needs at most 5 entity names per community, so #5954
	// replaced the map[string]string over every entity with a []string indexed
	// by the gonum node id BuildGraph already assigned: the same lookups off a
	// flat slice instead of a ~430k-entry string-keyed map (measured 21.5 MB ->
	// 6.6 MB at 433k entities).
	names := nodeNames(entities, idx)

	commResults, commOf, overallQ, denoised := ComputeCommunities(idx, names, opts)
	// Layer-1 deterministic naming (TF-IDF over member entity names +
	// qualified names + source-file basenames). Mutates commResults in place.
	AssignCommunityNames(commResults, entities, commOf)
	betw, pr := ComputeCentrality(g, idx)
	gods := identifyGodNodesFrom(betw, pr, idx)
	arts := IdentifyArticulationPoints(g, idx)
	surprises := ComputeSurpriseEdges(rels, commOf)

	endpoints := make(map[string]bool, len(surprises)*2)
	for _, s := range surprises {
		endpoints[s.FromID] = true
		endpoints[s.ToID] = true
	}

	return &AlgorithmResults{
		CommunityID:        commOf,
		Centrality:         betw,
		PageRank:           pr,
		GodNodes:           gods,
		ArticulationPoints: arts,
		SurpriseEndpoints:  endpoints,
		Communities:        commResults,
		SurpriseEdges:      surprises,
		Stats: AlgorithmStats{
			LouvainModularity:  overallQ,
			NumCommunities:     len(commResults),
			NumGodNodes:        len(gods),
			NumArticulationPts: len(arts),
			NumSurpriseEdges:   len(surprises),
			// Issue #481 — RuntimeMS is wall-clock and therefore varies run to
			// run. When SOURCE_DATE_EPOCH is set (reproducible-builds mode)
			// emit 0 so graph.json stays byte-stable.
			RuntimeMS:           runtimeMSFor(start),
			DenoisedCommunities: denoised,
		},
	}
}
