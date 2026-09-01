// kind_split_6451_test.go — issue #6451, the process-flow consumer of the split.
//
// chainCrossesExternalLib labels a process chain that reaches a third-party
// boundary; the dashboard renders it as an "external lib" badge. The only half
// of the old SCOPE.ExternalAPI that can appear in a code CALLS chain is the
// HTTP-client half (the _cross_httpclient extractor embeds a CALLS edge from
// scope:component:http_caller:<file> onto the entity). k8s Ingress hosts come
// from YAML, carry no inbound CALLS edge, and describe traffic coming IN — the
// opposite of "this process calls out to a third party". So the gate takes
// SCOPE.ExternalEndpoint and NOT SCOPE.IngressHost.
package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestKindSplit6451_ChainCrossesExternalLib(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"SCOPE.ExternalEndpoint", true},
		{"SCOPE.IngressHost", false},
		// The retired ambiguous kind must stop firing the gate — otherwise a
		// stale producer keeps the badge alive and the split is cosmetic.
		{"SCOPE.ExternalAPI", false},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			e := graph.Entity{ID: "x1", Kind: tc.kind, SourceFile: "svc.ts"}
			byID := map[string]*graph.Entity{e.ID: &e}
			if got := chainCrossesExternalLib([]string{e.ID}, byID, map[string]bool{}); got != tc.want {
				t.Fatalf("chainCrossesExternalLib(kind=%q) = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
	// Case-insensitivity of the surviving arm must be preserved.
	t.Run("uppercase SCOPE.EXTERNALENDPOINT", func(t *testing.T) {
		e := graph.Entity{ID: "x2", Kind: "SCOPE.EXTERNALENDPOINT"}
		byID := map[string]*graph.Entity{e.ID: &e}
		if !chainCrossesExternalLib([]string{e.ID}, byID, map[string]bool{}) {
			t.Fatal("uppercase SCOPE.ExternalEndpoint must still cross an external lib")
		}
	})
}
