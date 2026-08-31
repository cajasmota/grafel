// kind_split_6451_test.go — issue #6451, producer B half of the kind split.
//
// The k8s Ingress pass mints one entity per spec.rules[].host. Its Name is a
// bare HOSTNAME with no path — a different address dialect from the URL paths
// internal/extractors/cross/httpclient emits, which shared the same
// SCOPE.ExternalAPI kind until this split. Ingress hosts are cluster topology,
// so they get their own kind: SCOPE.IngressHost.
package yaml_test

import "testing"

func TestKindSplit6451_IngressHostsAreIngressHostKind(t *testing.T) {
	entities, err := extractYAML(k8sIngressFixture, "k8s/ingress.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hosts := findEntitiesBySubtype(entities, "ingress_host")
	if len(hosts) < 2 {
		t.Fatalf("expected ≥2 ingress_host entities, got %d", len(hosts))
	}
	for _, e := range hosts {
		if e.Kind != "SCOPE.IngressHost" {
			t.Errorf("ingress_host %q kind=%q, want SCOPE.IngressHost", e.Name, e.Kind)
		}
	}
	// Permissive-direction guards: neither the retired ambiguous kind nor the
	// HTTP-client half's kind may appear anywhere in this document.
	for _, e := range entities {
		if e.Kind == "SCOPE.ExternalAPI" {
			t.Errorf("yaml still emits SCOPE.ExternalAPI for %q (#6451)", e.Name)
		}
		if e.Kind == "SCOPE.ExternalEndpoint" {
			t.Errorf("yaml emitted SCOPE.ExternalEndpoint for %q — that kind belongs to the HTTP-client extractor", e.Name)
		}
	}
}
