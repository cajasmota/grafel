package proto_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// End-to-end guards for #6459 / #6492, run through the SAME production pipeline
// as the #6422 tests (real extractor, real graph.EntityID, real BuildIndex,
// real ReferencesEmbedded) so the assertions are about what lands in the graph.
//
// Two rounds of #6459 each shipped a fix that widened a kind family, and each
// widening destroyed an edge that resolved before. Unit tests on the resolver
// index alone did not catch it; these do, because the collision they need is
// created by the extractor's OWN addressing choices — buildService addresses an
// rpc child and fileContainsOperationRel addresses a service through the SAME
// BuildOperationStructuralRef, i.e. the same address space.

// twoServiceSrc is B1: entirely ordinary proto in which an rpc's name equals a
// SIBLING service's name. No API author would blink at it.
const twoServiceSrc = `syntax = "proto3";

message Foo { string id = 1; }

service User {
  rpc Get(Foo) returns (Foo);
}

service Admin {
  rpc User(Foo) returns (Foo);
}
`

// serviceNameCollisionSrc is the #6459 shape: a message and a service sharing a
// name, with NO rpc of that name. This is the case where the operation family
// has no candidate at all and the ordered tier is allowed to run.
const serviceNameCollisionSrc = `syntax = "proto3";

message Foo { string id = 1; }

service Foo {
  rpc Go(Foo) returns (Foo);
}
`

// countChildContains counts the CONTAINS edges the PARENT record carries to
// toID. Parent → child edges are appended to the parent's own record with an
// EMPTY FromID — assembly stamps the owning entity's id — so the from side is
// the record, not the field. (fileContainsRel is the exception: it sets FromID
// to the raw file path; countFileContains below reads those.)
func countChildContains(parent *types.EntityRecord, toID string) int {
	n := 0
	for _, r := range parent.Relationships {
		if r.Kind == "CONTAINS" && r.FromID == "" && r.ToID == toID {
			n++
		}
	}
	return n
}

// countFileContains counts file-anchored CONTAINS edges to toID across the
// whole extraction.
func countFileContains(recs []types.EntityRecord, file, toID string) int {
	n := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "CONTAINS" && r.FromID == file && r.ToID == toID {
				n++
			}
		}
	}
	return n
}

// TestServiceSiblingRpcCollisionKeepsItsContains6492 is the end-to-end B1
// guard: `service Admin { rpc User(...) }` beside `service User`.
//
// Reproduced at the round-2 head, where SCOPE.Service was admitted to a
// proto-scoped operation family: the service Admin → rpc User CONTAINS edge
// went DANGLING (both the rpc and the service User matched the family, so
// uniqueMatchInFamily returned no match), and the file → service User edge
// dangled with it. One correct edge destroyed, zero gained. The ordered tier
// keeps the operation family untouched, so this edge is unaffected.
func TestServiceSiblingRpcCollisionKeepsItsContains6492(t *testing.T) {
	recs := resolveProto6422(t, "b1.proto", twoServiceSrc)

	svcAdmin := findRec6422(t, recs, "SCOPE.Service", "service", "Admin")
	svcUser := findRec6422(t, recs, "SCOPE.Service", "service", "User")
	rpcUser := findRec6422(t, recs, "SCOPE.Operation", "endpoint", "User")
	if svcUser.ID == rpcUser.ID {
		t.Fatalf("service User and rpc User share id %q — the fixture cannot "+
			"distinguish them", svcUser.ID)
	}

	if n := countChildContains(svcAdmin, rpcUser.ID); n != 1 {
		t.Fatalf("service Admin → rpc User CONTAINS edges = %d, want 1. The rpc's name "+
			"equals a SIBLING service's name, which is ordinary proto; a SCOPE.Service "+
			"member in the family this ref is filtered by makes the rpc ref match two "+
			"members and the edge dangles (#6492 B1)", n)
	}

	// The non-colliding arm is the control.
	rpcGet := findRec6422(t, recs, "SCOPE.Operation", "endpoint", "Get")
	if n := countChildContains(svcUser, rpcGet.ID); n != 1 {
		t.Fatalf("control: service User → rpc Get CONTAINS edges = %d, want 1", n)
	}

	// No CONTAINS endpoint anywhere may still be an unresolved structural ref.
	// This is the shape the review measured as DANGLING, stated directly.
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "CONTAINS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "scope:") {
				t.Fatalf("CONTAINS edge from %q left an UNRESOLVED ToID %q — the "+
					"resolver could not bind it (#6492 B1)", r.FromID, r.ToID)
			}
		}
	}
}

// TestServiceNameCollisionGetsInboundContains6492 is the end-to-end #6459
// guard: the defect the whole PR exists to fix must actually be fixed.
//
// `message Foo` + `service Foo` in one file makes the kind-agnostic byLocation
// fallback ambiguous, so the file → service CONTAINS ToID was dropped and the
// service ended with zero inbound CONTAINS. The tier binds it because no
// operation-family entity is named Foo in that file.
func TestServiceNameCollisionGetsInboundContains6492(t *testing.T) {
	recs := resolveProto6422(t, "c6459.proto", serviceNameCollisionSrc)

	svc := findRec6422(t, recs, "SCOPE.Service", "service", "Foo")
	msg := findRec6422(t, recs, "SCOPE.Schema", "message", "Foo")
	if svc.ID == msg.ID {
		t.Fatalf("service Foo and message Foo share id %q — the fixture cannot "+
			"distinguish them", svc.ID)
	}

	if n := countFileContains(recs, "c6459.proto", svc.ID); n != 1 {
		inbound := 0
		for i := range recs {
			for _, r := range recs[i].Relationships {
				if r.Kind == "CONTAINS" && r.ToID == svc.ID {
					inbound++
				}
			}
		}
		t.Fatalf("file → service Foo CONTAINS edges = %d (total inbound CONTAINS on "+
			"the service = %d), want 1. `message Foo` beside `service Foo` makes the "+
			"kind-agnostic byLocation fallback ambiguous, so without the ordered "+
			"SCOPE.Service tier the edge is dropped and the service has no parent (#6459)",
			n, inbound)
	}

	// The tier must not have stolen the message's own edge on the way.
	if n := countFileContains(recs, "c6459.proto", msg.ID); n != 1 {
		t.Fatalf("file → message Foo CONTAINS edges = %d, want 1 — the schema arm is "+
			"addressed in the schema address space and must be untouched (#6459)", n)
	}
}
