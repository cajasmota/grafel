// kind_split_6451_test.go — issue #6451, the enrichment consumer of the split.
//
// qualifyHighArchKinds listed SCOPE.ExternalAPI back when that kind meant one
// thing: "calls into third-party HTTP / SDK surfaces" (internal/mcp/SCHEMA.md).
// That is the HTTP-client half. The split keeps enrichment pointed at the half
// the entry was written for — SCOPE.ExternalEndpoint, whose node aggregates
// real call sites an LLM can read — and does NOT extend it to
// SCOPE.IngressHost, a one-line YAML hostname with no body to summarise.
package enrichment

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestKindSplit6451_EnrichmentTakesExternalEndpointOnly(t *testing.T) {
	emitters := []CandidateEmitter{&describeEntityEmitter{}}

	cases := []struct {
		kind string
		name string
		want int
	}{
		{"SCOPE.ExternalEndpoint", "/api/things", 1},
		// Narrowed deliberately: cluster topology, not a described role.
		{"SCOPE.IngressHost", "api.example.com", 0},
		// The retired ambiguous kind must not keep qualifying.
		{"SCOPE.ExternalAPI", "/api/things", 0},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			doc := mkDoc(graph.Entity{ID: "e1", Name: tc.name, Kind: tc.kind})
			got := CollectCandidates(doc, emitters, nil)
			if len(got) != tc.want {
				t.Fatalf("kind %q name %q: got %d candidates, want %d", tc.kind, tc.name, len(got), tc.want)
			}
		})
	}
}
