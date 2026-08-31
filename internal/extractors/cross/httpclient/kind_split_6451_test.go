// kind_split_6451_test.go — issue #6451, producer A half of the kind split.
//
// This extractor's entity Name is a URL PATH (usually relative, no host). The
// k8s Ingress pass in internal/extractors/yaml minted the same
// SCOPE.ExternalAPI kind with a bare HOSTNAME as the Name. The kind is split so
// the two dialects stop being indistinguishable; this producer now owns
// SCOPE.ExternalEndpoint and must never emit the retired ambiguous kind.
package httpclient

import "testing"

func TestKindSplit6451_HTTPClientEmitsExternalEndpoint(t *testing.T) {
	src := `fetch('/api/things')`
	records := runExtract(t, "javascript", src)

	var endpoints, ambiguous, ingress int
	for _, r := range records {
		switch r.Kind {
		case "SCOPE.ExternalEndpoint":
			endpoints++
			if r.Name != "/api/things" {
				t.Errorf("SCOPE.ExternalEndpoint name = %q, want %q", r.Name, "/api/things")
			}
		case "SCOPE.ExternalAPI":
			ambiguous++
		case "SCOPE.IngressHost":
			ingress++
		}
	}
	if endpoints != 1 {
		t.Errorf("SCOPE.ExternalEndpoint entities = %d, want 1", endpoints)
	}
	// Permissive-direction guards: emitting the retired kind, or the OTHER
	// half's kind, both defeat the split.
	if ambiguous != 0 {
		t.Errorf("httpclient still emits %d SCOPE.ExternalAPI entities (#6451)", ambiguous)
	}
	if ingress != 0 {
		t.Errorf("httpclient emitted %d SCOPE.IngressHost entities — that kind belongs to the k8s Ingress pass", ingress)
	}
}
