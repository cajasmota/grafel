// adjacency_csr_5852_bench_test.go — latency benchmarks for the CSR
// (compressed-sparse-row) adjacency refactor (#5852). The CSR path
// materializes []edge on demand inside Outgoing/Incoming (a small per-call
// alloc + copy from the CSR row into an []edge slice), replacing a direct
// map[string][]edge lookup that returned the backing slice with zero
// allocation. This benchmark quantifies that per-call cost, both for a
// single accessor call and for a realistic multi-hop bounded BFS traversal
// (the hot path every expand/related/neighbors/traces call goes through),
// so a memory-vs-latency tradeoff decision can be made with numbers instead
// of guesswork.
//
// RUN (compare before/after by stashing the CSR change and re-running):
//
//	go test ./internal/mcp/ -run '^$' -bench 'BenchmarkCSR5852' -benchmem -count=3
package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// csrBenchDoc builds a synthetic graph with a high-degree hub (fan-in from
// hubDeg distinct sources) plus a linear chain hanging off one of those
// sources, so a bounded BFS from the chain head walks several hops through
// varying-degree nodes — representative of the expand/traces hot path.
func csrBenchDoc(hubDeg, chainLen int) *graph.Document {
	ents := make([]graph.Entity, 0, hubDeg+chainLen+1)
	rels := make([]graph.Relationship, 0, hubDeg+chainLen)

	ents = append(ents, graph.Entity{ID: "hub", Name: "Hub", Kind: "Function", SourceFile: "h.go", StartLine: 1})
	for i := 0; i < hubDeg; i++ {
		id := "src" + itoa(i)
		ents = append(ents, graph.Entity{ID: id, Name: id, Kind: "Function", SourceFile: "s.go", StartLine: 1})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "hub", Kind: "CALLS"})
	}
	// Chain: chain0 -> chain1 -> ... -> chain(chainLen-1) -> hub.
	prev := "hub"
	for i := 0; i < chainLen; i++ {
		id := "chain" + itoa(i)
		ents = append(ents, graph.Entity{ID: id, Name: id, Kind: "Function", SourceFile: "c.go", StartLine: 1})
		rels = append(rels, graph.Relationship{FromID: id, ToID: prev, Kind: "CALLS"})
		prev = id
	}
	return &graph.Document{Entities: ents, Relationships: rels}
}

// BenchmarkCSR5852_Outgoing measures a single Outgoing() call on a low-degree
// node (chain link) — the common case for most call sites (a specific
// entity's direct callees/callers).
func BenchmarkCSR5852_Outgoing(b *testing.B) {
	doc := csrBenchDoc(50000, 20)
	a := buildAdjacency(doc, "repo1")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.Outgoing("chain5")
	}
}

// BenchmarkCSR5852_Incoming_HighDegree measures a single Incoming() call on
// the hub node with 50,000 in-edges — the pathological high-fan-in case
// where materializing a full []edge row is most expensive.
func BenchmarkCSR5852_Incoming_HighDegree(b *testing.B) {
	doc := csrBenchDoc(50000, 20)
	a := buildAdjacency(doc, "repo1")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.Incoming("hub")
	}
}

// BenchmarkCSR5852_BoundedBFS_Chain exercises bfsBounded over the chain
// (low-degree hops only, never touching the hub's high fan-in), matching the
// common expand/related traversal shape: several hops of low-degree nodes.
func BenchmarkCSR5852_BoundedBFS_Chain(b *testing.B) {
	doc := csrBenchDoc(50000, 20)
	a := buildAdjacency(doc, "repo1")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = bfsBounded(a, "chain0", 6, nil, 0)
	}
}

// BenchmarkCSR5852_BoundedBFS_HitsHub exercises bfsBounded from the hub
// itself, so the walk must materialize the full 50,000-edge Incoming row —
// the worst case for per-call CSR materialization cost inside a traversal.
func BenchmarkCSR5852_BoundedBFS_HitsHub(b *testing.B) {
	doc := csrBenchDoc(50000, 20)
	a := buildAdjacency(doc, "repo1")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = bfsBounded(a, "hub", 2, nil, 0)
	}
}
