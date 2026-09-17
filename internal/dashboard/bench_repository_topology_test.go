package dashboard

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func benchmarkRepositoryTopologyGroup(repositoryCount, linkCount, relationshipCount int) *DashGroup {
	repositories := make([]string, repositoryCount)
	for index := range repositories {
		repositories[index] = fmt.Sprintf("repo-%03d", index)
	}
	group := repositoryTopologyTestGroup(repositories...)
	group.Links = make([]CrossRepoLink, linkCount)
	for index := range group.Links {
		source := repositories[index%repositoryCount]
		target := repositories[(index*17+1)%repositoryCount]
		group.Links[index] = CrossRepoLink{
			Source: source + "::client", Target: target + "::service",
			Channel:    []string{"dubbo", "http", "kafka", "rabbitmq"}[index%4],
			Identifier: fmt.Sprintf("contract-%06d", index),
			Properties: map[string]string{"confidence": "resolved"},
		}
	}
	if relationshipCount > 0 {
		document := group.Repos[repositories[0]].Doc
		document.Relationships = make([]graph.Relationship, relationshipCount)
		for index := range document.Relationships {
			document.Relationships[index] = graph.Relationship{FromID: "module", ToID: "module", Kind: "CALLS"}
		}
	}
	return group
}

func BenchmarkRepositoryTopology10kLinks(b *testing.B) {
	benchmarkRepositoryTopology(b, 100, 10_000, 0)
}

func BenchmarkRepositoryTopology100kLinks(b *testing.B) {
	benchmarkRepositoryTopology(b, 500, 100_000, 0)
}

func BenchmarkRepositoryTopology100kLinksMillionInternalRelationships(b *testing.B) {
	benchmarkRepositoryTopology(b, 500, 100_000, 1_000_000)
}

func benchmarkRepositoryTopology(b *testing.B, repositories, links, relationships int) {
	b.Helper()
	group := benchmarkRepositoryTopologyGroup(repositories, links, relationships)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = buildRepositoryTopologyIndex(group)
	}
}
