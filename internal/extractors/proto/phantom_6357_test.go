package proto_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6357 — no relationship may point at a target this extractor never emits.
//
// #6356 turned this extractor on for every .proto file in every indexed repo.
// Two of its edge families pointed at IDs that no entity ever carried, so
// internal/resolve/refs.go materialised them as phantom grey nodes — one per
// enum value, and one per cross-file field type:
//
//   - F2: buildEnum emitted `CONTAINS → scope:schema:column:proto:<f>:E#VALUE`
//     for every enum value while emitting no entity for any enum value. The
//     package doc claimed "enum → enum value" and buildField did exactly this
//     for message fields, so the omission was in the code, not the design.
//   - F4: namedTypeRefs strips the package qualifier off a field type and
//     buildMessage then builds the ref against file.Path, so
//     `etcdserverpb.LeaseTimeToLiveRequest x = 1;` in etcd's lease.proto
//     emitted a REFERENCES edge into lease.proto for a message defined in
//     etcd/api/etcdserverpb/rpc.proto.
//
// The assertion below is deliberately an invariant over ALL emitted edges
// rather than three spot checks, because the next construct added to this
// extractor is subject to exactly the same failure mode.
// ---------------------------------------------------------------------------

// selfRefIDs returns the set of structural-ref IDs the given entities are
// addressable by, built with the same two builders the extractor's edges use:
// the operation ref for named definitions (service/message/enum/rpc) and the
// Format-B member ref for dotted members (field/enum_value).
func selfRefIDs(entities []types.EntityRecord) map[string]bool {
	ids := make(map[string]bool, len(entities)*2)
	for _, e := range entities {
		if e.Name == "" {
			continue
		}
		switch e.Subtype {
		case "field", "enum_value":
			parent, member, ok := strings.Cut(e.Name, ".")
			if !ok {
				continue
			}
			ids["scope:schema:column:proto:"+e.SourceFile+":"+parent+"#"+member] = true
		case "service", "message", "enum", "endpoint":
			ids["scope:operation:method:proto:"+e.SourceFile+":"+e.Name] = true
		}
	}
	return ids
}

// phantomFixtures deliberately spans the constructs whose edges are built by
// different code paths: enum values (F2), a package-qualified cross-file field
// type (F4), a same-file named field type (the REFERENCES case that MUST
// survive the F4 fix), a map value type, and a nested message.
var phantomFixtures = map[string]string{
	"enum_values": `syntax = "proto3";
enum Status {
  STATUS_UNSPECIFIED = 0;
  STATUS_PAID = 1;
  STATUS_REFUNDED = 2;
}
`,
	"cross_file_qualified_type": `syntax = "proto3";
package leasepb;
import "etcd/api/etcdserverpb/rpc.proto";
message LeaseInternalRequest {
  etcdserverpb.LeaseTimeToLiveRequest LeaseTimeToLiveRequest = 1;
}
`,
	"same_file_named_type": `syntax = "proto3";
message Order { string id = 1; }
message User {
  repeated Order orders = 3;
  map<string, Order> by_id = 4;
}
`,
	"nested_and_service": `syntax = "proto3";
message Outer {
  message Inner { string v = 1; }
  Inner inner = 1;
  Kind kind = 2;
  enum Kind { KIND_UNSPECIFIED = 0; KIND_A = 1; }
}
message Ack { bool ok = 1; }
service S {
  rpc Do(Outer) returns (Ack);
}
`,
}

// importTargetIDs returns the set of IMPORTS ToIDs that ARE backed by an
// emitted entity: buildImportEntities mints one SCOPE.Component/import stub per
// file-level `import "..."`, addressed by its raw path, which is the ToID the
// paired IMPORTS edge carries.
func importTargetIDs(entities []types.EntityRecord) map[string]bool {
	ids := make(map[string]bool, len(entities))
	for _, e := range entities {
		if e.Subtype == "import" && e.Name != "" {
			ids[e.Name] = true
		}
	}
	return ids
}

// #6359 removed this file's LAST exemption. It used to carry
// `allowedBareIMPORTS`, pinning the one edge family the invariant knowingly
// let through: buildRPC's `IMPORTS file → <ReqType>` / `→ <RespType>` with the
// bare, unqualified type name — no import path, no structural ref, no backing
// entity. Those edges now exist as rpc → type REFERENCES against
// BuildOperationStructuralRef, so they are checked by the CONTAINS/REFERENCES
// branch like every other type ref and need no exemption. The allow-list is
// deleted rather than emptied: an empty map would leave the exemption
// machinery in place for the next edge that wants to slip through.

func TestProto_NoRelationshipTargetsAPhantomNode(t *testing.T) {
	for name, src := range phantomFixtures {
		t.Run(name, func(t *testing.T) {
			path := name + ".proto"
			entities := extract(t, path, src)
			if len(entities) == 0 {
				t.Fatalf("fixture %s produced no entities — fixture is vacuous", name)
			}
			ids := selfRefIDs(entities)
			imports := importTargetIDs(entities)

			// Every CONTAINS / REFERENCES / IMPORTS edge is checked — no
			// prefix filter, no skip, and since #6359 no exemption.
			// CONTAINS/REFERENCES must land on a structural ref this extractor
			// also emits an entity for; IMPORTS must land on an emitted import
			// stub, which after #6359 is the only thing IMPORTS ever means
			// here.
			checked := 0
			for _, e := range entities {
				for _, r := range e.Relationships {
					switch r.Kind {
					case "CONTAINS", "REFERENCES":
						checked++
						if !ids[r.ToID] {
							t.Errorf("%s edge ToID=%q from entity %q has NO backing entity — "+
								"resolve/refs.go will materialise it as a phantom node",
								r.Kind, r.ToID, e.Name)
						}
					case "IMPORTS":
						checked++
						if imports[r.ToID] {
							continue
						}
						t.Errorf("IMPORTS edge ToID=%q from entity %q has NO backing import "+
							"stub — resolve/refs.go will materialise it as a phantom node",
							r.ToID, e.Name)
					}
				}
			}
			if checked == 0 {
				t.Fatalf("fixture %s emitted no CONTAINS/REFERENCES/IMPORTS edges — "+
					"the assertion never ran", name)
			}
			t.Logf("%s: %d entities, %d edges checked (no exemptions)",
				name, len(entities), checked)
		})
	}
}

// TestProto_EnumValuesAreEmittedAsEntities pins the F2 direction chosen
// (emit the entity, keep the edge) rather than the alternative (drop the edge),
// so a later change that silences the phantom by deleting the enum→value
// CONTAINS edge fails here instead of passing the invariant above vacuously.
func TestProto_EnumValuesAreEmittedAsEntities(t *testing.T) {
	entities := extract(t, "e.proto", phantomFixtures["enum_values"])
	want := map[string]bool{
		"Status.STATUS_UNSPECIFIED": false,
		"Status.STATUS_PAID":        false,
		"Status.STATUS_REFUNDED":    false,
	}
	for _, e := range entities {
		if e.Subtype != "enum_value" {
			continue
		}
		if _, ok := want[e.Name]; !ok {
			t.Errorf("unexpected enum_value entity %q", e.Name)
			continue
		}
		want[e.Name] = true
		if e.Language != "protobuf" {
			t.Errorf("enum_value %q Language=%q, want protobuf", e.Name, e.Language)
		}
		if e.SourceFile != "e.proto" {
			t.Errorf("enum_value %q SourceFile=%q, want e.proto", e.Name, e.SourceFile)
		}
		if e.StartLine <= 0 {
			t.Errorf("enum_value %q StartLine=%d, want > 0", e.Name, e.StartLine)
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("no enum_value entity emitted for %q", n)
		}
	}
}

// TestProto_SameFileReferencesSurvive guards the F4 fix against over-reach:
// suppressing unresolvable cross-file refs must not suppress the same-file
// `repeated Order orders = 3;` case the REFERENCES edge was written for.
func TestProto_SameFileReferencesSurvive(t *testing.T) {
	entities := extract(t, "u.proto", phantomFixtures["same_file_named_type"])
	refs := relsByKind(entities, "REFERENCES")
	if len(refs) == 0 {
		t.Fatal("no REFERENCES edges emitted for a same-file named field type — " +
			"the #6357 cross-file filter is over-broad")
	}
	wantTo := "scope:operation:method:proto:u.proto:Order"
	for _, r := range refs {
		if r.ToID != wantTo {
			t.Errorf("REFERENCES ToID=%q, want %q", r.ToID, wantTo)
		}
	}
	t.Logf("same-file REFERENCES edges kept: %d", len(refs))
}

// TestProto_EnumValueNumberProperty pins Properties["number"], the value
// #6357 started persisting on every enum_value entity.
//
// It was shipped with zero assertions anywhere in the package: replacing
// `props["number"] = number` in buildEnumValue with the constant "0" left the
// whole package green. It also read the wrong thing — the proto grammar emits
// the sign as a token SIBLING of int_lit, so `E_NEG = -1;` persisted as "1",
// indistinguishable from a genuine 1. Negative enum values are legal proto3
// (the field is int32).
//
// The four cases below are chosen to be jointly un-fakeable by any constant:
// zero, a positive, int32 max, and a negative.
func TestProto_EnumValueNumberProperty(t *testing.T) {
	src := `syntax = "proto3";
enum Signed {
  SIGNED_ZERO = 0;
  SIGNED_ONE = 1;
  SIGNED_MAX = 2147483647;
  SIGNED_NEG = -1;
}
`
	entities := extract(t, "signed.proto", src)

	want := map[string]string{
		"Signed.SIGNED_ZERO": "0",
		"Signed.SIGNED_ONE":  "1",
		"Signed.SIGNED_MAX":  "2147483647",
		"Signed.SIGNED_NEG":  "-1",
	}
	got := make(map[string]string, len(want))
	for _, e := range entities {
		if e.Subtype != "enum_value" {
			continue
		}
		got[e.Name] = e.Properties["number"]
	}
	if len(got) != len(want) {
		t.Fatalf("emitted %d enum_value entities (%v), want %d — fixture is not exercising all four cases",
			len(got), got, len(want))
	}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("%s Properties[\"number\"] = %q, want %q", n, got[n], w)
		}
	}

	// The number is also rendered into the Signature, so a fix that only
	// patched one of the two readers is caught here too.
	for _, e := range entities {
		if e.Subtype != "enum_value" || e.Name != "Signed.SIGNED_NEG" {
			continue
		}
		if e.Signature != "Signed.SIGNED_NEG = -1" {
			t.Errorf("SIGNED_NEG Signature = %q, want %q", e.Signature, "Signed.SIGNED_NEG = -1")
		}
	}
}
