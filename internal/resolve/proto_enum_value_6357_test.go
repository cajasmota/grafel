package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestProtoEnumValueRefResolvesToItsEntity6357 closes the loop on #6357 F2 at
// the layer that decides whether an edge becomes a real node or a phantom.
//
// internal/extractors/proto emits, for `enum Status { STATUS_PAID = 1; }`, a
// CONTAINS edge whose ToID is the Format-B member ref
// scope:schema:column:proto:<file>:Status#STATUS_PAID. Before #6357 no entity
// carried that address, so lookupStructural returned unmatched and refs.go
// materialised a grey stub — one per enum value in every indexed .proto file.
//
// The proto-side test (internal/extractors/proto/phantom_6357_test.go) proves
// the ENTITY is emitted with the dotted Name the member index is built from.
// This test proves the other half: that such an entity is what this resolver
// actually binds the ref to. Neither test alone shows the phantom is gone.
func TestProtoEnumValueRefResolvesToItsEntity6357(t *testing.T) {
	const protoFile = "api/orders/v1/orders.proto"
	enum := types.EntityRecord{
		ID: "a1a1a1a1a1a1a1a1", Kind: "SCOPE.Schema", Subtype: "enum",
		Name: "Status", SourceFile: protoFile, Language: "protobuf",
	}
	// Exactly the shape internal/extractors/proto.buildEnumValue emits.
	value := types.EntityRecord{
		ID: "b2b2b2b2b2b2b2b2", Kind: "SCOPE.Schema", Subtype: "enum_value",
		Name: "Status.STATUS_PAID", QualifiedName: "Status.STATUS_PAID",
		SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{enum, value})

	// Exactly the shape internal/extractors/proto.fieldMemberRef mints.
	const ref = "scope:schema:column:proto:" + protoFile + ":Status#STATUS_PAID"

	id, _, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id != value.ID {
		t.Fatalf("lookupStructural(%q) = %q, want the enum_value entity ID %q — "+
			"an unbound enum-value ref is materialised as a phantom node (#6357)",
			ref, id, value.ID)
	}

	// Negative half: with the value entity absent — the pre-#6357 state — the
	// same ref must NOT resolve. Without this, the assertion above could pass
	// for a reason unrelated to the entity being present.
	idxNoValue := BuildIndex([]types.EntityRecord{enum})
	if id, _, _ := idxNoValue.lookupStructural(ref); id != "" {
		t.Fatalf("lookupStructural(%q) = %q with no enum_value entity in the index; "+
			"want \"\" — the test is not measuring the entity's presence", ref, id)
	}
}
