package dashboard

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestBuildV2GraphWithNodeCapMatchesLegacyThinning(t *testing.T) {
	grp := makeCompactLODFixture(120, 900)
	server := &Server{}
	repos := sortedRepos(grp)
	full := server.buildV2Graph(repos, grp, "", false, false)
	const cap = 30
	wantNodes := thinByPagerankConnected(full.Nodes, full.Edges, cap)
	kept := make(map[string]bool, len(wantNodes))
	for _, node := range wantNodes {
		kept[node.ID] = true
	}
	wantEdges := make([]v2GraphEdge, 0)
	for _, edge := range full.Edges {
		if kept[edge.Source] && kept[edge.Target] {
			wantEdges = append(wantEdges, edge)
		}
	}
	recomputeServedDegree(wantNodes, wantEdges)

	got := server.buildV2GraphWithNodeCap(repos, grp, "", false, false, cap)
	if got.TotalNodeCount != full.TotalNodeCount || len(got.Nodes) != len(wantNodes) || len(got.Edges) != len(wantEdges) {
		t.Fatalf("compact LoD counts = total:%d nodes:%d edges:%d, want %d/%d/%d",
			got.TotalNodeCount, len(got.Nodes), len(got.Edges), full.TotalNodeCount, len(wantNodes), len(wantEdges))
	}
	for i := range wantNodes {
		if got.Nodes[i] != wantNodes[i] {
			t.Fatalf("node %d differs: got %+v want %+v", i, got.Nodes[i], wantNodes[i])
		}
	}
	for i := range wantEdges {
		if got.Edges[i] != wantEdges[i] {
			t.Fatalf("edge %d differs: got %+v want %+v", i, got.Edges[i], wantEdges[i])
		}
	}
}

var benchmarkV2Response v2GraphResponse

func BenchmarkBuildV2GraphLOD20K300K(b *testing.B) {
	grp := makeCompactLODFixture(20_000, 300_000)
	server := &Server{}
	repos := sortedRepos(grp)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkV2Response = server.buildV2GraphWithNodeCap(repos, grp, "", false, false, 3_000)
	}
}

func BenchmarkBuildV2GraphLegacy20K300K(b *testing.B) {
	grp := makeCompactLODFixture(20_000, 300_000)
	server := &Server{}
	repos := sortedRepos(grp)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := server.buildV2Graph(repos, grp, "", false, false)
		resp.Nodes = thinByPagerankConnected(resp.Nodes, resp.Edges, 3_000)
		kept := make(map[string]bool, len(resp.Nodes))
		for _, node := range resp.Nodes {
			kept[node.ID] = true
		}
		edges := resp.Edges[:0]
		for _, edge := range resp.Edges {
			if kept[edge.Source] && kept[edge.Target] {
				edges = append(edges, edge)
			}
		}
		resp.Edges = edges
		recomputeServedDegree(resp.Nodes, resp.Edges)
		benchmarkV2Response = resp
	}
}

func makeCompactLODFixture(entityCount, relationshipCount int) *DashGroup {
	entities := make([]graph.Entity, entityCount)
	for i := range entities {
		rank := float64(entityCount - i)
		entities[i] = graph.Entity{ID: fmt.Sprintf("entity-%05d", i), Name: fmt.Sprintf("Entity %d", i), Kind: "function", PageRank: &rank}
	}
	relationships := make([]graph.Relationship, relationshipCount)
	for i := range relationships {
		from := i % entityCount
		to := (i*17 + 1) % entityCount
		if from == to {
			to = (to + 1) % entityCount
		}
		relationships[i] = graph.Relationship{FromID: entities[from].ID, ToID: entities[to].ID, Kind: "CALLS"}
	}
	return &DashGroup{
		Name: "lod-benchmark",
		Repos: map[string]*DashRepo{
			"repo": {Slug: "repo", Doc: &graph.Document{Entities: entities, Relationships: relationships}},
		},
	}
}
