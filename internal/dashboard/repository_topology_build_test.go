package dashboard

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestBuildRepositoryTopologyIndexAggregatesPairAndChannel(t *testing.T) {
	grp := repositoryTopologyTestGroup("client", "rule")
	grp.Links = []CrossRepoLink{
		{Source: "client::c1", Target: "rule::p1", Channel: "dubbo", Identifier: "dubbo:A", Confidence: 1},
		{Source: "client::c2", Target: "rule::p2", Channel: "dubbo", Identifier: "dubbo:B", Confidence: 1},
		{Source: "client::h1", Target: "rule::h2", Method: "http", Identifier: "http:GET:/rules", Confidence: 1},
	}
	got := buildRepositoryTopologyIndex(grp)
	if len(got.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(got.Edges))
	}
	key := repositoryEdgeKey{Source: "client", Target: "rule", Channel: channelDubbo}
	edge, ok := got.EdgeByKey[key]
	if !ok {
		t.Fatalf("missing edge %v", key)
	}
	if edge.RelationshipCount != 2 || edge.ContractCount != 2 {
		t.Fatalf("edge = %#v", edge)
	}
	if ids := got.RelationshipIDs[key]; len(ids) != 2 {
		t.Fatalf("relationship ids = %#v", ids)
	}
}

func TestBuildRepositoryTopologyIndexDoesNotTraverseInternalRelationships(t *testing.T) {
	grp := repositoryTopologyTestGroup("client")
	grp.Repos["client"].Doc.Relationships = make([]graph.Relationship, 100_000)
	got := buildRepositoryTopologyIndex(grp)
	if len(got.Edges) != 0 || len(got.Relationships) != 0 {
		t.Fatalf("unexpected topology from internal relationships: edges=%d relationships=%d", len(got.Edges), len(got.Relationships))
	}
}

func TestBuildRepositoryTopologyIndexOrdersEdgesAndBoundsSamples(t *testing.T) {
	grp := repositoryTopologyTestGroup("zeta", "alpha", "beta")
	for i := 7; i >= 1; i-- {
		grp.Links = append(grp.Links, CrossRepoLink{
			Source: "zeta::c" + fmt.Sprint(i), Target: "alpha::p" + fmt.Sprint(i),
			Channel: "dubbo", Identifier: fmt.Sprintf("dubbo:%02d", i),
		})
	}
	grp.Links = append(grp.Links, CrossRepoLink{Source: "alpha::h", Target: "beta::h", Method: "http", Identifier: "http:GET:/b"})
	got := buildRepositoryTopologyIndex(grp)
	if got.Edges[0].Source != "alpha" || got.Edges[1].Source != "zeta" {
		t.Fatalf("edges not sorted: %#v", got.Edges)
	}
	dubbo := got.EdgeByKey[repositoryEdgeKey{Source: "zeta", Target: "alpha", Channel: channelDubbo}]
	if len(dubbo.Samples) != repositoryTopologyInlineSampleLimit || !dubbo.HasMore {
		t.Fatalf("sample bound not applied: %#v", dubbo)
	}
	if dubbo.Samples[0].Label != "dubbo:01" || dubbo.Samples[4].Label != "dubbo:05" {
		t.Fatalf("samples not sorted: %#v", dubbo.Samples)
	}
}

func repositoryTopologyTestGroup(repos ...string) *DashGroup {
	grp := &DashGroup{Name: "test", Repos: make(map[string]*DashRepo)}
	for index, repo := range repos {
		grp.Repos[repo] = &DashRepo{
			Slug: repo,
			Doc: &graph.Document{
				Repo:     repo,
				Stats:    graph.Stats{Entities: 10 + index},
				Entities: []graph.Entity{{ID: "module", Kind: "module", Language: "java"}},
			},
		}
	}
	return grp
}
