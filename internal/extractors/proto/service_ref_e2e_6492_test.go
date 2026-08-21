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
// the record, not the field. Since #6518 the file-level edges follow the same
// convention, on the per-file SCOPE.Component/file record; countFileContains
// below reads those.
func countChildContains(parent *types.EntityRecord, toID string) int {
	n := 0
	for _, r := range parent.Relationships {
		if r.Kind == "CONTAINS" && r.FromID == "" && r.ToID == toID {
			n++
		}
	}
	return n
}

// countFileContains counts the file-level CONTAINS edges to toID, i.e. those
// carried by the SCOPE.Component/file entity for file (#6518).
func countFileContains(t *testing.T, recs []types.EntityRecord, file, toID string) int {
	t.Helper()
	n := 0
	for _, r := range fileLevelContains(t, recs, file) {
		if r.ToID == toID {
			n++
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

	if n := countFileContains(t, recs, "c6459.proto", svc.ID); n != 1 {
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
	if n := countFileContains(t, recs, "c6459.proto", msg.ID); n != 1 {
		t.Fatalf("file → message Foo CONTAINS edges = %d, want 1 — the schema arm is "+
			"addressed in the schema address space and must be untouched (#6459)", n)
	}
}

// selfNamedRpcSrc is the RESIDUAL shape #6459 does NOT close: the service and
// one of its OWN rpcs carry the same name, so `message Bar` is only there to
// keep the file valid.
const selfNamedRpcSrc = `syntax = "proto3";

message Bar { string id = 1; }

service Foo {
  rpc Foo(Bar) returns (Bar);
}
`

// TestSelfNamedRpcLeavesTheServiceOrphaned6459Residual is a CHARACTERISATION
// test: it records, in the graph, the one #6459-titled shape this PR does not
// fix, so the claim in proto.go and in the proto-mini NOTICE can be read
// against something executable.
//
// `service Foo { rpc Foo(...) }` addresses the service (fileContainsOperationRel)
// and the rpc (buildService's child edge) with the BYTE-IDENTICAL ref
// scope:operation:method:proto:<file>:Foo. The ordered tier cannot help: its
// precondition sees the rpc in the operation family and bails, which is
// correct — the alternative is a service silently outranking a real rpc. Two
// distinct entities are simply not distinguishable by the ref that names them,
// so the file → service edge lands on the rpc instead.
//
// The observable result is #6459's title symptom at this head: the service ends
// with ZERO inbound CONTAINS, and the rpc carries TWO (its own parent edge plus
// the mis-bound file edge). Closing it needs the extractor to stop addressing a
// service and its rpc identically — a change to the ref format, not to the
// resolver — and belongs in its own issue.
//
// If a later change makes this pass differently, that is the fix landing, not a
// regression: update the numbers and the two doc claims together.
func TestSelfNamedRpcLeavesTheServiceOrphaned6459Residual(t *testing.T) {
	recs := resolveProto6422(t, "residual.proto", selfNamedRpcSrc)

	svc := findRec6422(t, recs, "SCOPE.Service", "service", "Foo")
	rpc := findRec6422(t, recs, "SCOPE.Operation", "endpoint", "Foo")
	if svc.ID == rpc.ID {
		t.Fatalf("service Foo and rpc Foo share id %q — the fixture cannot "+
			"distinguish them", svc.ID)
	}

	inbound := func(toID string) int {
		n := 0
		for i := range recs {
			for _, r := range recs[i].Relationships {
				if r.Kind == "CONTAINS" && r.ToID == toID {
					n++
				}
			}
		}
		return n
	}

	if got := inbound(svc.ID); got != 0 {
		t.Fatalf("inbound CONTAINS on service Foo = %d, want 0 (RESIDUAL, not a "+
			"target). If this is now 1, the self-named-rpc residual has been "+
			"CLOSED — update this test and the scoped claims in "+
			"internal/extractors/proto/proto.go and "+
			"internal/quality/golden/proto-mini/NOTICE.md in the same change (#6459)", got)
	}
	if got := inbound(rpc.ID); got != 2 {
		t.Fatalf("inbound CONTAINS on rpc Foo = %d, want 2 — its own service → rpc "+
			"edge plus the file → service edge mis-bound onto it, because both refs "+
			"are the byte-identical string %q (#6459 residual)", got,
			"scope:operation:method:proto:residual.proto:Foo")
	}

	// The residual is a MIS-BINDING, not a dangle: nothing is left unresolved,
	// which is exactly why no existing assertion in this file catches it.
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "CONTAINS" && strings.HasPrefix(r.ToID, "scope:") {
				t.Fatalf("CONTAINS edge from %q left an UNRESOLVED ToID %q — the "+
					"residual is a mis-binding, not a dangling ref (#6459)", r.FromID, r.ToID)
			}
		}
	}
}

// selfNamedImportSrc puts a SCOPE.Component at the service's own (file, name).
//
// buildImportEntities mints one entity per import with Name set to the
// VERBATIM quoted string and SourceFile set to the IMPORTING file. That string
// is not a path in any enforced sense: the tree-sitter grammar accepts any
// string literal and grafel never runs protoc. So `import "Foo";` beside
// `service Foo` is valid input that lands a SCOPE.Component named "Foo" in the
// same file as the service — no extractor change, no hypothetical kind.
const selfNamedImportSrc = `syntax = "proto3";

import "Foo";

service Foo {
  rpc Go(Bar) returns (Bar);
}

message Bar { string id = 1; }
`

// TestSelfNamedImportDoesNotBlockTheServiceTier6492 pins the #6459 tier's
// precondition against WIDENING, end to end.
//
// The precondition scans operationKindFamily. Swapping it for
// componentOrOperationKindFamily — or merely appending
// scopeKindPrefix+"Component" to it — survives every other test in this PR,
// and on this input it re-opens #6459 exactly: the import's SCOPE.Component
// sits at (this file, "Foo"), the widened scan sees it, the tier bails, and
// the file → service Foo CONTAINS edge dangles. Measured: 1 edge at the fixed
// head, 0 under either mutation.
//
// The direction makes this milder than a #6492 regression — a widened
// precondition loses a binding, it never lets a SCOPE.Service outrank a real
// rpc — but the bail set is a behavioural boundary and both of its sides need
// pinning. The synthetic-table twin is
// TestProtoServiceTierPreconditionScansTheWholeFamilyAndBase6492's
// scope-component / bare-component rows.
func TestSelfNamedImportDoesNotBlockTheServiceTier6492(t *testing.T) {
	const file = "selfimport.proto"
	recs := resolveProto6422(t, file, selfNamedImportSrc)

	svc := findRec6422(t, recs, "SCOPE.Service", "service", "Foo")
	imp := findRec6422(t, recs, "SCOPE.Component", "import", "Foo")
	if svc.ID == imp.ID {
		t.Fatalf("service Foo and import \"Foo\" share id %q — the fixture cannot "+
			"distinguish them", svc.ID)
	}
	if imp.SourceFile != file {
		t.Fatalf("import entity SourceFile = %q, want %q — the fixture only creates "+
			"the collision if the import entity lives in the IMPORTING file (#6492)",
			imp.SourceFile, file)
	}

	if n := countFileContains(t, recs, file, svc.ID); n != 1 {
		inbound := 0
		for i := range recs {
			for _, r := range recs[i].Relationships {
				if r.Kind == "CONTAINS" && r.ToID == svc.ID {
					inbound++
				}
			}
		}
		t.Fatalf("file → service Foo CONTAINS edges = %d (total inbound CONTAINS on "+
			"the service = %d), want 1. An `import \"Foo\";` in the file that declares "+
			"`service Foo` puts a SCOPE.Component at the service's own (file, name). "+
			"That kind is NOT in operationKindFamily, so the #6459 tier's precondition "+
			"must not see it; a precondition scanning any superset of that family "+
			"bails here and re-opens #6459 on valid proto (#6492)", n, inbound)
	}

	// The rpc arm is the control: the tier must still be inert where the
	// operation family really does have a candidate.
	rpc := findRec6422(t, recs, "SCOPE.Operation", "endpoint", "Go")
	if n := countChildContains(svc, rpc.ID); n != 1 {
		t.Fatalf("control: service Foo → rpc Go CONTAINS edges = %d, want 1", n)
	}

	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "CONTAINS" && strings.HasPrefix(r.ToID, "scope:") {
				t.Fatalf("CONTAINS edge from %q left an UNRESOLVED ToID %q (#6492)",
					r.FromID, r.ToID)
			}
		}
	}
}
