package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// TestProtoServiceRefResolvesUnderNameCollision6459 closes #6459.
//
// internal/extractors/proto emits the file → service CONTAINS edge through
// fileContainsOperationRel → extractor.BuildOperationStructuralRef, i.e. the
// ToID lands in the OPERATION address space:
//
//	scope:operation:method:proto:<file>:<ServiceName>
//
// but the service entity itself carries Kind "SCOPE.Service", which was absent
// from operationKindFamily. lookupStructural therefore could not bind the ref
// through the kind-filtered lookupLocationKind tier and had to fall through to
// the kind-agnostic byLocation index — which DROPS any (file, name) that is not
// unique. The moment anything else in the same .proto shares the service's name
// the edge dangles and the service ends with zero inbound CONTAINS.
//
// `message Foo` + `service Foo` in one file is legal proto3 and is exactly that
// collision. It is the mirror image of the #6422 message/rpc collision, and the
// shape the corrected doc comment at internal/extractors/proto/proto.go:261-284
// records as still-unfixed.
func TestProtoServiceRefResolvesUnderNameCollision6459(t *testing.T) {
	const protoFile = "api/orders/v1/orders.proto"

	message := types.EntityRecord{
		ID: "a1a1a1a1a1a1a1a1", Kind: "SCOPE.Schema", Subtype: "message",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	service := types.EntityRecord{
		ID: "b2b2b2b2b2b2b2b2", Kind: "SCOPE.Service", Subtype: "service",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	rpc := types.EntityRecord{
		ID: "c3c3c3c3c3c3c3c3", Kind: "SCOPE.Operation", Subtype: "rpc",
		Name: "Go", SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{message, service, rpc})

	ref := extractor.BuildOperationStructuralRef("proto", protoFile, "Foo")

	id, _, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id != service.ID {
		t.Fatalf("lookupStructural(%q) = %q, want the SCOPE.Service entity ID %q — "+
			"SCOPE.Service must be a member of operationKindFamily, otherwise the "+
			"file → service CONTAINS edge dangles whenever the service's name "+
			"collides with a sibling in the same .proto (#6459)",
			ref, id, service.ID)
	}

	// Negative half: the same ref in a file whose ONLY Foo is the message must
	// still bind to the message, not to a service that is not there. Without
	// this, the assertion above could pass because the operation family had been
	// widened into something that swallows schema entities too.
	idxNoService := BuildIndex([]types.EntityRecord{message, rpc})
	if id, _, _ := idxNoService.lookupStructural(ref); id == service.ID {
		t.Fatalf("lookupStructural(%q) bound to the service entity ID %q in an index "+
			"that contains no service", ref, id)
	}
}

// TestProtoServiceIsNotAComponent6459 pins the mirror hazard of the #6459 fix.
//
// SCOPE.Service belongs in operationKindFamily because the proto extractor
// ADDRESSES services in the operation address space. It must NOT also be added
// to componentKindFamily: that family is what EXTENDS / IMPLEMENTS and the
// component-shaped semantic edges (INJECTED_INTO, BINDS, REGISTERS, …) bias
// toward, and admitting a proto service there would let a bare type reference
// bind to an IDL service definition. Widening both families also destroys the
// discrimination the operation entry exists to provide: a
// scope:component:class:* ref and a scope:operation:method:* ref naming the
// same (file, name) would become indistinguishable.
func TestProtoServiceIsNotAComponent6459(t *testing.T) {
	for _, k := range componentKindFamily {
		if k == scopeKindPrefix+"Service" {
			t.Fatalf("componentKindFamily contains %q; SCOPE.Service is addressed in "+
				"the OPERATION address space only (#6459)", k)
		}
	}
	for _, k := range componentOrOperationKindFamily {
		if k == scopeKindPrefix+"Service" {
			t.Fatalf("componentOrOperationKindFamily contains %q; the union is for "+
				"RENDERS / USES_TRANSLATION endpoints, which are never proto services (#6459)", k)
		}
	}
	var found bool
	for _, k := range operationKindFamily {
		if k == scopeKindPrefix+"Service" {
			found = true
		}
	}
	if !found {
		t.Fatalf("operationKindFamily = %v, want it to contain %q (#6459)",
			operationKindFamily, scopeKindPrefix+"Service")
	}
}

// TestProtoServiceRefStillHonoursComponentAddressSpace6459 is the widening
// guard for the fix: a scope:component:class:* ref naming the same (file, name)
// as a proto service must NOT bind to the service. Admitting SCOPE.Service to
// operationKindFamily is a widening of one family only; if it leaked into the
// component family (directly, or via the componentOrOperation union feeding
// structuralKindFamilies), this ref would start resolving to the service.
//
// The colliding `message Foo` is load-bearing, not decoration: it is what keeps
// the kind-agnostic byLocation fallback ambiguous. Without it that fallback
// binds ANY same-(file, name) ref to the lone entity regardless of kind, and the
// assertion would be red on clean code — an unsatisfiable row rather than a
// guard (see internal/quality/golden/proto-mini/NOTICE.md).
func TestProtoServiceRefStillHonoursComponentAddressSpace6459(t *testing.T) {
	const protoFile = "api/orders/v1/orders.proto"
	message := types.EntityRecord{
		ID: "a1a1a1a1a1a1a1a1", Kind: "SCOPE.Schema", Subtype: "message",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	service := types.EntityRecord{
		ID: "b2b2b2b2b2b2b2b2", Kind: "SCOPE.Service", Subtype: "service",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{message, service})

	ref := extractor.BuildComponentStructuralRef("proto", protoFile, "Foo")
	if id, _, _ := idx.lookupStructural(ref); id == service.ID {
		t.Fatalf("lookupStructural(%q) = %q — a component-address-space ref bound to "+
			"a SCOPE.Service; SCOPE.Service must stay out of componentKindFamily (#6459)",
			ref, id)
	}
}

// TestIsOperationKindStillExcludesService6459 pins the deliberate divergence
// between operationKindFamily and isOperationKind.
//
// isOperationKind is NOT a fast mirror of operationKindFamily — it is already
// strictly narrower (it excludes "Function" and "Method"), and it gates two
// LANGUAGE-SPECIFIC package indexes in BuildIndex: byKotlinPkgMember /
// byKotlinPkgFunc (keyed off the kotlin_package property) and
// byPackageOperation, the Go same-package callee index keyed by directory.
// Admitting SCOPE.Service there would let a Go CALLS edge to a bare name bind
// to a proto `service` that merely happens to sit in the same directory — a
// cross-language mis-binding with no corresponding address-space claim, since
// nothing routes a Go call through the proto service's structural ref. The
// #6459 fix is scoped to the family the proto extractor actually addresses.
func TestIsOperationKindStillExcludesService6459(t *testing.T) {
	if isOperationKind(scopeKindPrefix + "Service") {
		t.Fatalf("isOperationKind(%q) = true; the Kotlin/Go package indexes it gates "+
			"must not admit proto services (#6459)", scopeKindPrefix+"Service")
	}
	// Guard the premise: the predicate is narrower than the family by design,
	// so this test is not simply restating operationKindFamily.
	if isOperationKind("Method") || isOperationKind("Function") {
		t.Fatal("isOperationKind now accepts Method/Function; it is no longer the " +
			"narrow predicate this test's reasoning assumes — re-derive the exclusion")
	}
}
