package dashboard

import (
	"fmt"
	"reflect"
	"testing"
)

func TestFilterRepositoryTopologyRemovesDisconnectedNodes(t *testing.T) {
	index := repositoryFilterFixture()
	got := filterRepositoryTopology(index, repositoryTopologyQuery{
		Channels: []repositoryChannel{channelDubbo}, Evidence: []repositoryEvidence{evidenceConfirmed},
		Direction: directionBoth, Depth: 1, MinCount: 1,
	})
	assertRepositoryNodeIDs(t, got.Nodes, "a", "b")
	assertRepositoryEdgeIDs(t, got.Edges, "a->b:dubbo")
	if got.Nodes[0].OutboundRelationships != 1 || got.Nodes[1].InboundRelationships != 1 {
		t.Fatalf("filtered metrics not applied: %#v", got.Nodes)
	}
}

func TestFilterRepositoryTopologyAppliesEvidenceRepositorySearchAndMinimumCount(t *testing.T) {
	index := repositoryFilterFixture()
	query := repositoryTopologyQuery{
		Channels: []repositoryChannel{channelHTTP}, Repos: []string{"b", "c"},
		Evidence: []repositoryEvidence{evidenceInferred}, Direction: directionBoth, Depth: 1,
		MinCount: 2, Search: "orders",
	}
	got := filterRepositoryTopology(index, query)
	assertRepositoryNodeIDs(t, got.Nodes, "b", "c")
	assertRepositoryEdgeIDs(t, got.Edges, "b->c:http")
	if got.Edges[0].RelationshipCount != 2 {
		t.Fatalf("relationship count = %d", got.Edges[0].RelationshipCount)
	}
}

func TestFilterRepositoryTopologyFocusTraversalRespectsDirectionAndDepth(t *testing.T) {
	index := repositoryChainFixture()
	tests := []struct {
		name      string
		direction repositoryDirection
		depth     int
		wantNodes []string
	}{
		{name: "outbound depth two", direction: directionOutbound, depth: 2, wantNodes: []string{"b", "c", "d"}},
		{name: "inbound depth one", direction: directionInbound, depth: 1, wantNodes: []string{"a", "b"}},
		{name: "both depth one", direction: directionBoth, depth: 1, wantNodes: []string{"a", "b", "c"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := filterRepositoryTopology(index, repositoryTopologyQuery{
				Channels: []repositoryChannel{channelDubbo}, Evidence: []repositoryEvidence{evidenceConfirmed},
				Focus: "b", Direction: test.direction, Depth: test.depth, MinCount: 1,
			})
			assertRepositoryNodeIDs(t, got.Nodes, test.wantNodes...)
			for _, edge := range got.Edges {
				if !containsString(test.wantNodes, edge.Source) || !containsString(test.wantNodes, edge.Target) {
					t.Fatalf("edge outside traversal: %#v", edge)
				}
			}
		})
	}
}

func TestFilterRepositoryTopologyChoosesDeterministicShortestPath(t *testing.T) {
	grp := repositoryTopologyTestGroup("a", "b", "c", "d")
	grp.Links = []CrossRepoLink{
		{Source: "a::1", Target: "c::1", Channel: "http"},
		{Source: "c::2", Target: "d::1", Channel: "http"},
		{Source: "a::2", Target: "b::1", Channel: "dubbo"},
		{Source: "b::2", Target: "d::2", Channel: "dubbo"},
	}
	got := filterRepositoryTopology(buildRepositoryTopologyIndex(grp), repositoryTopologyQuery{
		Channels: []repositoryChannel{channelDubbo, channelHTTP}, Evidence: []repositoryEvidence{evidenceConfirmed},
		Source: "a", Target: "d", Direction: directionBoth, Depth: 3, MinCount: 1,
	})
	if !got.PathFound || !reflect.DeepEqual(got.Path, []string{"a", "b", "d"}) {
		t.Fatalf("path = %#v found=%v", got.Path, got.PathFound)
	}
	assertRepositoryEdgeIDs(t, got.Edges, "a->b:dubbo", "b->d:dubbo")
}

func TestFilterRepositoryTopologyLeavesGraphVisibleDuringIncompletePathSelection(t *testing.T) {
	index := repositoryChainFixture()
	for _, query := range []repositoryTopologyQuery{
		{Source: "a"},
		{Target: "d"},
	} {
		query.Channels = []repositoryChannel{channelDubbo}
		query.Evidence = []repositoryEvidence{evidenceConfirmed}
		query.Direction = directionBoth
		query.Depth = 3
		query.MinCount = 1
		got := filterRepositoryTopology(index, query)
		if got.PathFound || len(got.Path) != 0 {
			t.Fatalf("incomplete path unexpectedly resolved: %#v", got)
		}
		assertRepositoryNodeIDs(t, got.Nodes, "a", "b", "c", "d")
		assertRepositoryEdgeIDs(t, got.Edges, "a->b:dubbo", "b->c:dubbo", "c->d:dubbo")
	}
}

func TestFilterRepositoryTopologyHandlesSameNodeAndMissingPath(t *testing.T) {
	index := repositoryChainFixture()
	same := filterRepositoryTopology(index, repositoryTopologyQuery{
		Channels: []repositoryChannel{channelDubbo}, Evidence: []repositoryEvidence{evidenceConfirmed},
		Source: "b", Target: "b", Direction: directionBoth, Depth: 3, MinCount: 1,
	})
	if !same.PathFound || !reflect.DeepEqual(same.Path, []string{"b"}) || len(same.Edges) != 0 || len(same.Nodes) != 1 {
		t.Fatalf("same-node path = %#v", same)
	}
	missing := filterRepositoryTopology(index, repositoryTopologyQuery{
		Channels: []repositoryChannel{channelDubbo}, Evidence: []repositoryEvidence{evidenceConfirmed},
		Source: "d", Target: "a", Direction: directionBoth, Depth: 3, MinCount: 1,
	})
	if missing.PathFound || len(missing.Nodes) != 0 || len(missing.Edges) != 0 {
		t.Fatalf("missing path = %#v", missing)
	}
}

func TestFilterRepositoryTopologyCapsRepositoryNodes(t *testing.T) {
	repositories := make([]string, repositoryTopologyMaxNodes+1)
	for index := range repositories {
		repositories[index] = fmt.Sprintf("repo-%04d", index)
	}
	grp := repositoryTopologyTestGroup(repositories...)
	for index := 0; index < len(repositories)-1; index++ {
		grp.Links = append(grp.Links, CrossRepoLink{
			Source:     fmt.Sprintf("%s::client", repositories[index]),
			Target:     fmt.Sprintf("%s::server", repositories[index+1]),
			Channel:    "dubbo",
			Identifier: fmt.Sprintf("dubbo:Contract%04d", index),
			Confidence: 1,
		})
	}

	got := filterRepositoryTopology(buildRepositoryTopologyIndex(grp), repositoryTopologyQuery{})
	if got.Summary.PreLimitNodes != repositoryTopologyMaxNodes+1 {
		t.Fatalf("pre-limit nodes = %d, want %d", got.Summary.PreLimitNodes, repositoryTopologyMaxNodes+1)
	}
	if len(got.Nodes) != repositoryTopologyMaxNodes {
		t.Fatalf("nodes = %d, want %d", len(got.Nodes), repositoryTopologyMaxNodes)
	}
	if len(got.Edges) != repositoryTopologyMaxNodes-1 {
		t.Fatalf("edges = %d, want %d after removing the edge to the omitted repository", len(got.Edges), repositoryTopologyMaxNodes-1)
	}
	if !got.Truncated || got.Limits.MaxNodes != repositoryTopologyMaxNodes {
		t.Fatalf("truncation metadata = %#v", got)
	}
}

func TestFilterRepositoryTopologyCapsAggregatedEdges(t *testing.T) {
	const repositoryCount = 64
	repositories := make([]string, repositoryCount)
	for index := range repositories {
		repositories[index] = fmt.Sprintf("repo-%04d", index)
	}
	grp := repositoryTopologyTestGroup(repositories...)
	for source := 0; source < repositoryCount && len(grp.Links) < repositoryTopologyMaxEdges+1; source++ {
		for target := 0; target < repositoryCount && len(grp.Links) < repositoryTopologyMaxEdges+1; target++ {
			if source == target {
				continue
			}
			grp.Links = append(grp.Links, CrossRepoLink{
				Source:     fmt.Sprintf("%s::client", repositories[source]),
				Target:     fmt.Sprintf("%s::server", repositories[target]),
				Channel:    "dubbo",
				Identifier: fmt.Sprintf("dubbo:Contract%04d", len(grp.Links)),
				Confidence: 1,
			})
		}
	}

	got := filterRepositoryTopology(buildRepositoryTopologyIndex(grp), repositoryTopologyQuery{})
	if got.Summary.PreLimitEdges != repositoryTopologyMaxEdges+1 {
		t.Fatalf("pre-limit edges = %d, want %d", got.Summary.PreLimitEdges, repositoryTopologyMaxEdges+1)
	}
	if len(got.Edges) != repositoryTopologyMaxEdges {
		t.Fatalf("edges = %d, want %d", len(got.Edges), repositoryTopologyMaxEdges)
	}
	if len(got.Nodes) > repositoryCount {
		t.Fatalf("nodes = %d, want at most %d", len(got.Nodes), repositoryCount)
	}
	if !got.Truncated || got.Limits.MaxEdges != repositoryTopologyMaxEdges {
		t.Fatalf("truncation metadata = %#v", got)
	}
}

func repositoryFilterFixture() repositoryTopologyIndex {
	grp := repositoryTopologyTestGroup("a", "b", "c", "d", "e")
	grp.Links = []CrossRepoLink{
		{Source: "a::1", Target: "b::1", Channel: "dubbo", Identifier: "dubbo:Rules", Properties: map[string]string{"confidence": "resolved"}},
		{Source: "b::1", Target: "c::1", Method: "http", Identifier: "http:GET:/orders", Properties: map[string]string{"confidence": "inferred"}},
		{Source: "b::2", Target: "c::2", Method: "http", Identifier: "http:POST:/orders", Properties: map[string]string{"confidence": "heuristic"}},
		{Source: "c::1", Target: "d::1", Method: "kafka_topic", Identifier: "kafka:events"},
		{Source: "e::1", Target: "b::1", Identifier: "rabbitmq:payments", Properties: map[string]string{"routing_key": "payments"}},
	}
	return buildRepositoryTopologyIndex(grp)
}

func repositoryChainFixture() repositoryTopologyIndex {
	grp := repositoryTopologyTestGroup("a", "b", "c", "d")
	grp.Links = []CrossRepoLink{
		{Source: "a::1", Target: "b::1", Channel: "dubbo"},
		{Source: "b::1", Target: "c::1", Channel: "dubbo"},
		{Source: "c::1", Target: "d::1", Channel: "dubbo"},
	}
	return buildRepositoryTopologyIndex(grp)
}

func assertRepositoryNodeIDs(t *testing.T, nodes []repositoryTopologyNode, want ...string) {
	t.Helper()
	got := make([]string, 0, len(nodes))
	for _, node := range nodes {
		got = append(got, node.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("node ids = %#v, want %#v", got, want)
	}
}

func assertRepositoryEdgeIDs(t *testing.T, edges []repositoryTopologyEdge, want ...string) {
	t.Helper()
	got := make([]string, 0, len(edges))
	for _, edge := range edges {
		got = append(got, edge.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edge ids = %#v, want %#v", got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
