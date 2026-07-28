package graph

import (
	"fmt"
	"runtime"
	"testing"
)

// csrBenchFixture is the shape every CSR benchmark in this file runs on.
//
// buildHeavyTailedGraph is preferential-attachment (Barabasi-Albert): heavy
// in-degree tail, one cluster holding ~26% of entities. It is the closest
// synthetic shape to the reference corpus that this package already has. It
// does NOT contain parallel edges, self-loops or dangling endpoints (the
// generator de-dups); those three BuildGraph behaviours are pinned by the
// equivalence tests in algorithms_csr_test.go instead, not by this benchmark.
//
// n=60000 / degree 4 gives ~240k relationships. The reference corpus is 427k
// entities / 1.33M collapsed edges, i.e. ~5.5x larger; numbers here are a
// per-edge proxy, not an absolute prediction of corpus RSS.
//
// IMPORTANT: buildHeavyTailedGraph emits relationships with NO PROPERTIES, so
// PropsSnapshot() returns nil and edgeWeight short-circuits. Corpus
// relationships carry callsite_count/confidence on essentially every edge, and
// a version of this change that read those properties twice cost 31.6% of
// BuildGraph's wall time while being completely invisible on this fixture.
// Anything that touches edge-weight derivation must be benchmarked on
// csrBenchFixtureWithProps as well — see BenchmarkBuildGraph_WithProps.
func csrBenchFixture() ([]Entity, []Relationship) {
	return buildHeavyTailedGraph(60000, 4, 5)
}

// csrBenchFixtureWithProps is csrBenchFixture with callsite_count/confidence
// attached to every relationship, which is what the real corpus looks like.
// Same topology, so the two are directly comparable and the difference between
// them is exactly the cost of reading relationship properties.
func csrBenchFixtureWithProps() ([]Entity, []Relationship) {
	ents, rels := csrBenchFixture()
	withProps := make([]Relationship, len(rels))
	for i := range rels {
		withProps[i] = rels[i].WithProperties(map[string]string{
			"callsite_count": fmt.Sprintf("%d", 1+i%5),
			"confidence":     fmt.Sprintf("0.%02d", 11+i%80),
		})
	}
	return ents, withProps
}

func BenchmarkBuildGraph_HeavyTailed(b *testing.B) {
	ents, rels := csrBenchFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g, idx := BuildGraph(ents, rels)
		if g == nil || idx == nil {
			b.Fatal("nil graph")
		}
	}
}

// BenchmarkBuildGraph_WithProps is the same topology with corpus-shaped
// relationship properties. It exists because Relationship.PropsSnapshot()
// materialises a fresh map per call, so any implementation that reads a
// relationship's properties more than once shows up HERE and nowhere else.
func BenchmarkBuildGraph_WithProps(b *testing.B) {
	ents, rels := csrBenchFixtureWithProps()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g, idx := BuildGraph(ents, rels)
		if g == nil || idx == nil {
			b.Fatal("nil graph")
		}
	}
}

// BenchmarkSampledBetweenness_SharedCSR isolates the betweenness consumer.
// Before #5954 S5/S6 this function derived its own adjacency by walking g's
// interface-returning edge iterator; now it aliases the shared CSR.
func BenchmarkSampledBetweenness_SharedCSR(b *testing.B) {
	ents, rels := csrBenchFixture()
	g, idx := BuildGraph(ents, rels)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sampledBetweenness(g, idx.csr, betweennessSampleSize, betweennessSampleSeed)
	}
}

// BenchmarkComputeCommunities_SharedCSR isolates the Louvain consumer. Before
// #5954 S5/S6 this built a whole simple.WeightedUndirectedGraph and then walked
// it back out twice; now it projects the shared CSR in one pass.
func BenchmarkComputeCommunities_SharedCSR(b *testing.B) {
	ents, rels := csrBenchFixture()
	_, idx := BuildGraph(ents, rels)
	names := nodeNames(ents, idx)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = ComputeCommunities(idx, names, DefaultCommunityOptions())
	}
}

// BenchmarkPass4_HeavyTailed measures the whole pass-4 sweep through the public
// entry point, and reports peak HeapInuse over baseline alongside the standard
// churn numbers — peak heap, not TotalAlloc, is what decides whether the
// group-algo child process swaps.
func BenchmarkPass4_HeavyTailed(b *testing.B) {
	b.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "100")
	ents, rels := csrBenchFixture()

	var peakSum, iters uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var res *AlgorithmResults
		p, _ := betweennessMemProbe(func() {
			res = RunAlgorithmsWithOptions(ents, rels, DefaultCommunityOptions())
		})
		if res == nil {
			b.Fatal("nil result")
		}
		runtime.KeepAlive(res)
		peakSum += p
		iters++
	}
	b.StopTimer()
	if iters > 0 {
		b.ReportMetric(float64(peakSum/iters)/(1<<20), "peakHeapInuseMB")
	}
}
