package proto_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6359 — rpc request/response edges.
//
// buildRPC emitted, unconditionally and with no guard:
//
//	{FromID: file.Path, ToID: reqType,  Kind: "IMPORTS"}
//	{FromID: file.Path, ToID: respType, Kind: "IMPORTS"}
//
// where reqType/respType default to the literal string "?". Four defects:
// the verb is wrong (an rpc's message types are not a file import, and the
// collision with the real `import "x.proto"` edges made IMPORTS ambiguous),
// the source is wrong (the FILE, not the rpc, so every rpc in a service
// collapsed onto one origin), the target is unresolvable (a bare unqualified
// type name — a guaranteed phantom node), and an unparsed rpc emitted an edge
// to "?".
//
// The fix routes rpc type refs through exactly the machinery message field
// types already use: REFERENCES + messageTypeRef + dropUnresolvableTypeRefs.
// One convention, not two.
// ---------------------------------------------------------------------------

const rpcSrc = `syntax = "proto3";

message GetUserRequest { string id = 1; }
message User { string id = 1; }
message Empty {}

service UserService {
  rpc GetUser(GetUserRequest) returns (User);
  rpc Ping(Empty) returns (Empty);
  rpc External(otherpkg.Thing) returns (User);
}
`

func rpcEntity(t *testing.T, entities []types.EntityRecord, name string) *types.EntityRecord {
	t.Helper()
	for i := range entities {
		if entities[i].Kind == "SCOPE.Operation" && entities[i].Name == name {
			return &entities[i]
		}
	}
	t.Fatalf("rpc entity %q not found", name)
	return nil
}

// TestRPC_TypeEdgesAreResolvableReferences is the core assertion: the two
// request/response edges are REFERENCES against the same structural ref a
// message→field-type edge uses, carry the direction, and are anchored on the
// rpc — not on the file.
func TestRPC_TypeEdgesAreResolvableReferences(t *testing.T) {
	entities := extract(t, "svc.proto", rpcSrc)
	get := rpcEntity(t, entities, "GetUser")

	wantReq := msgTypeRef("svc.proto", "GetUserRequest")
	wantResp := msgTypeRef("svc.proto", "User")

	got := make(map[string]string)
	for _, r := range get.Relationships {
		if r.Kind != "REFERENCES" {
			t.Errorf("rpc GetUser emitted a %q edge; only REFERENCES is expected (#6359): %+v", r.Kind, r)
			continue
		}
		// Entity-anchored, NOT file-anchored. A file-path FromID here is the
		// exact shape internal/extractors/file_anchored_rels_guard_test.go
		// exists to catch, and it passed only because the old edge was
		// mis-kinded IMPORTS (the #120 exemption).
		if r.FromID != "" {
			t.Errorf("rpc GetUser REFERENCES FromID = %q, want empty (anchored on the rpc entity)", r.FromID)
		}
		if r.Properties.Get("via_rpc") != "GetUser" {
			t.Errorf("REFERENCES via_rpc = %q, want GetUser", r.Properties.Get("via_rpc"))
		}
		got[r.ToID] = r.Properties.Get("direction")
	}
	if got[wantReq] != "request" {
		t.Errorf("request edge to %q: direction = %q, want \"request\" (got map %v)", wantReq, got[wantReq], got)
	}
	if got[wantResp] != "response" {
		t.Errorf("response edge to %q: direction = %q, want \"response\" (got map %v)", wantResp, got[wantResp], got)
	}
	if len(got) != 2 {
		t.Errorf("rpc GetUser emitted %d REFERENCES edges (%v), want 2", len(got), got)
	}
}

// TestRPC_EmitsNoIMPORTS pins the verb split. Before #6359 IMPORTS meant both
// "this file imports x.proto" and "this rpc takes type T", so no consumer
// could distinguish them. After it, the only IMPORTS in a proto extraction
// come from buildImportEntities and every one of them targets an emitted
// import stub.
func TestRPC_EmitsNoIMPORTS(t *testing.T) {
	entities := extract(t, "svc.proto", rpcSrc)
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				t.Errorf("rpc-only fixture emitted an IMPORTS edge %+v from entity %q — "+
					"buildRPC must not use the import verb", r, e.Name)
			}
		}
	}

	// And with a real import present, the file-level edge still works and is
	// still the ONLY IMPORTS, even though the file also has an rpc.
	withImport := `syntax = "proto3";
import "google/protobuf/empty.proto";
message Req { string id = 1; }
message Resp { string id = 1; }
service S { rpc Do(Req) returns (Resp); }
`
	ents := extract(t, "i.proto", withImport)
	imports := relsByKind(ents, "IMPORTS")
	if len(imports) != 1 {
		t.Fatalf("IMPORTS count = %d, want exactly 1 (the file-level import); got %+v", len(imports), imports)
	}
	if imports[0].FromID != "i.proto" || imports[0].ToID != "google/protobuf/empty.proto" {
		t.Errorf("IMPORTS edge = %+v, want i.proto → google/protobuf/empty.proto", imports[0])
	}
}

// TestRPC_UnresolvableTypesEmitNoEdge pins the convention shared with
// dropUnresolvableTypeRefs (#6357): a type not defined in THIS file yields no
// edge, rather than a dangling one. `otherpkg.Thing` lives in another file, so
// the request arm of External is simply absent; its response arm (User, local)
// survives, proving the filter is not a blanket.
func TestRPC_UnresolvableTypesEmitNoEdge(t *testing.T) {
	entities := extract(t, "svc.proto", rpcSrc)
	ext := rpcEntity(t, entities, "External")

	wantResp := msgTypeRef("svc.proto", "User")
	if len(ext.Relationships) != 1 {
		t.Fatalf("rpc External emitted %d edges (%+v), want exactly 1 — the cross-file "+
			"request type must be dropped, not dangled", len(ext.Relationships), ext.Relationships)
	}
	r := ext.Relationships[0]
	if r.ToID != wantResp || r.Properties.Get("direction") != "response" {
		t.Errorf("surviving edge = %+v, want REFERENCES → %q direction=response", r, wantResp)
	}
	if strings.Contains(r.ToID, "Thing") {
		t.Errorf("cross-file type otherpkg.Thing leaked into a ref: %q", r.ToID)
	}
}

// TestRPC_SameTypeBothArmsIsOneEdge pins the duplicate fix. The old code
// emitted two identical `file → Empty` edges for `rpc Ping(Empty) returns
// (Empty)`; the new one emits a single edge recording both roles.
func TestRPC_SameTypeBothArmsIsOneEdge(t *testing.T) {
	entities := extract(t, "svc.proto", rpcSrc)
	ping := rpcEntity(t, entities, "Ping")

	want := msgTypeRef("svc.proto", "Empty")
	if len(ping.Relationships) != 1 {
		t.Fatalf("rpc Ping emitted %d edges (%+v), want 1 de-duplicated edge",
			len(ping.Relationships), ping.Relationships)
	}
	r := ping.Relationships[0]
	if r.ToID != want {
		t.Errorf("Ping edge ToID = %q, want %q", r.ToID, want)
	}
	if d := r.Properties.Get("direction"); d != "request,response" {
		t.Errorf("Ping edge direction = %q, want \"request,response\" — collapsing the "+
			"duplicate must not lose the second role", d)
	}
}

// TestRPC_NeverEmitsQuestionMarkTarget pins the missing guard. reqType and
// respType initialise to the literal "?" and were emitted with no check, so an
// rpc whose types the grammar did not yield shipped an edge to the string "?".
//
// Both arms assert something that can fail (#6419 review — the Weird arm used
// to assert only "no ? escaped" over an entity with ZERO relationships, which
// is true of any empty set):
//
//   - Stream: the edge set is NON-EMPTY, so the no-"?" scan runs over real
//     edges, and the one edge points at message M.
//   - Weird: the entity IS emitted (the rpc is not silently skipped) and its
//     edge set is empty for a reason that was verified, not assumed. The
//     grammar yields EMPTY request/response type text here (the Signature is
//     "rpc Weird() returns ()", so reqType/respType never reach their "?"
//     initialiser), and two independent guards then keep it out of the graph:
//     namedTypeRefs returns nothing for empty/scalar type text, and
//     dropUnresolvableTypeRefs removes any REFERENCES no local message backs.
//     Measured: removing EITHER guard alone still leaves 0 edges; removing
//     BOTH mints `REFERENCES → scope:schema:message:proto:q.proto:` and this
//     arm fails. So the assertion can fail — it is not an empty-set tautology
//     — but it is double-guarded, which is why the Stream arm above carries
//     the load-bearing positive assertion.
func TestRPC_NeverEmitsQuestionMarkTarget(t *testing.T) {
	src := `syntax = "proto3";
message M { string id = 1; }
service S {
  rpc Stream(stream M) returns (stream M);
  rpc Weird() returns ();
}
`
	entities := extract(t, "q.proto", src)

	stream := rpcEntity(t, entities, "Stream")
	if len(stream.Relationships) == 0 {
		t.Fatal("rpc Stream emitted no edges — the no-\"?\" scan below would be vacuous")
	}
	for _, r := range stream.Relationships {
		if r.Kind != "REFERENCES" || r.ToID != msgTypeRef("q.proto", "M") {
			t.Errorf("rpc Stream edge = %+v, want REFERENCES → %q", r, msgTypeRef("q.proto", "M"))
		}
	}

	weird := rpcEntity(t, entities, "Weird")
	if len(weird.Relationships) != 0 {
		t.Errorf("rpc Weird emitted %d edges (%+v), want 0 — its request/response types "+
			"parse to empty text, so there is nothing resolvable to point at",
			len(weird.Relationships), weird.Relationships)
	}

	checked := 0
	for _, e := range entities {
		for _, r := range e.Relationships {
			checked++
			if strings.Contains(r.ToID, "?") || strings.Contains(r.FromID, "?") {
				t.Errorf("edge with an unparsed \"?\" placeholder escaped into the graph: "+
					"%+v (from entity %q)", r, e.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no edges scanned — the \"?\" assertion never ran")
	}
}

// msgTypeRef spells out, literally, the structural ref this extractor uses to
// ADDRESS a message/enum as the target of a type reference. Kept as a literal
// (not a call to the production builder) so a change to the wire format has to
// be made here too, deliberately.
func msgTypeRef(filePath, name string) string {
	return "scope:schema:message:proto:" + filePath + ":" + name
}

// TestRPC_RPCAndMessageOfTheSameNameDoNotSelfLoop pins the collision the
// #6359 fix introduced.
//
// rpc entities are SCOPE.Operation and message entities are SCOPE.Schema, but
// both were addressed by BuildOperationStructuralRef("proto", file, name) —
// ONE address space per file. For `rpc User(User) returns (User)` the emitted
// REFERENCES ToID was therefore the rpc's OWN address: assembly stamps FromID
// with the rpc's id, FromID == ToID, and the edge is discarded as a self-loop
// (internal/graph/orientation.go:206, algorithms.go:287, pr_impact.go:273).
// The intended rpc → message link vanished silently.
//
// The message type ref now lives in the schema address space, which
// internal/resolve/refs.go resolves through schemaKindFamily (refs.go:1807,
// structuralKindFamilies) — so it binds to the SCOPE.Schema message and NOT to
// the same-named SCOPE.Operation rpc. Both halves are asserted: no self-loop
// AND the link still exists.
func TestRPC_RPCAndMessageOfTheSameNameDoNotSelfLoop(t *testing.T) {
	src := `syntax = "proto3";
message User { string id = 1; }
service S {
  rpc User(User) returns (User);
}
`
	entities := extract(t, "f.proto", src)
	rpc := rpcEntity(t, entities, "User")

	own := extractor.BuildOperationStructuralRef("proto", "f.proto", "User")
	for _, r := range rpc.Relationships {
		if r.ToID == own {
			t.Errorf("rpc User emitted an edge to its OWN address %q — assembly stamps "+
				"FromID == ToID and the edge is dropped as a self-loop, losing the "+
				"rpc → message link: %+v", own, r)
		}
	}

	if len(rpc.Relationships) != 1 {
		t.Fatalf("rpc User emitted %d edges (%+v), want exactly 1 rpc → message REFERENCES",
			len(rpc.Relationships), rpc.Relationships)
	}
	r := rpc.Relationships[0]
	if r.Kind != "REFERENCES" || r.ToID != msgTypeRef("f.proto", "User") {
		t.Errorf("rpc User edge = %+v, want REFERENCES → %q", r, msgTypeRef("f.proto", "User"))
	}
	if d := r.Properties.Get("direction"); d != "request,response" {
		t.Errorf("rpc User edge direction = %q, want \"request,response\"", d)
	}
}
