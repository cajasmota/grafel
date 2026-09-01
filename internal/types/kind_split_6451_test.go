// kind_split_6451_test.go — issue #6451.
//
// SCOPE.ExternalAPI was minted by two unrelated producers under incompatible
// name dialects: internal/extractors/cross/httpclient (Name = a URL *path*,
// usually relative, no host) and internal/extractors/yaml's k8s Ingress pass
// (Name = a bare *hostname*, no path). One kind, two meanings — the #6472
// pattern. The kind is split so each half can be reasoned about (and, later,
// joined or declared terminal) on its own terms.
//
// This file pins the taxonomy half of the split: the two new kinds exist and
// are valid, and the ambiguous kind is no longer part of the closed enum.
package types

import "testing"

func TestKindSplit6451_NewKindsAreValid(t *testing.T) {
	if got := string(EntityKindExternalEndpoint); got != "SCOPE.ExternalEndpoint" {
		t.Errorf("EntityKindExternalEndpoint = %q, want %q", got, "SCOPE.ExternalEndpoint")
	}
	if got := string(EntityKindIngressHost); got != "SCOPE.IngressHost" {
		t.Errorf("EntityKindIngressHost = %q, want %q", got, "SCOPE.IngressHost")
	}
	for _, k := range []EntityKind{EntityKindExternalEndpoint, EntityKindIngressHost} {
		if !IsValidEntityKind(string(k)) {
			t.Errorf("%s must be a valid entity kind", k)
		}
		found := false
		for _, all := range AllEntityKinds() {
			if all == k {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllEntityKinds missing %s", k)
		}
	}
}

// TestKindSplit6451_AmbiguousKindRetired is the permissive-direction guard: if
// SCOPE.ExternalAPI survives in the enum, a producer can keep emitting the
// ambiguous kind and every consumer gate below stays undecidable.
func TestKindSplit6451_AmbiguousKindRetired(t *testing.T) {
	if IsValidEntityKind("SCOPE.ExternalAPI") {
		t.Error("SCOPE.ExternalAPI must no longer be a valid entity kind (#6451 split)")
	}
	for _, k := range AllEntityKinds() {
		if string(k) == "SCOPE.ExternalAPI" {
			t.Error("AllEntityKinds still contains SCOPE.ExternalAPI (#6451 split)")
		}
	}
}
