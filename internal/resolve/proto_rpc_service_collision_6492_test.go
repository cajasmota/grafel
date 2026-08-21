package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// B1 (#6492) — the regression a widened proto operation family introduces on
// ORDINARY proto.
//
// internal/extractors/proto/proto.go's buildService addresses every rpc child
// through extractor.BuildOperationStructuralRef("proto", file, rpcName) — the
// SAME builder, and therefore the same address space, that fileContainsOperationRel
// uses for the file → service CONTAINS edge. Both an rpc and a service in a
// .proto file are addressed as scope:operation:method:proto:<file>:<Name>.
//
// So the moment SCOPE.Service joins the family that operation-space refs are
// filtered by, an rpc whose name equals ANY service name in the same file
// matches TWO family members and uniqueMatchInFamily returns ("", false): the
// service → rpc CONTAINS edge, which resolved cleanly before the fix, dangles.
//
//	service User  { rpc Get(Foo)  returns (Foo); }
//	service Admin { rpc User(Foo) returns (Foo); }
//
// is entirely ordinary proto and reproduces it. The golden fixture proto-mini
// cannot: no rpc there shares a service name.
//
// The correct mechanism is an ORDERED TIER, not a widened family: try the
// unmodified operation family first (so this collision resolves to the rpc
// exactly as it does pre-fix), and consult the service kind ONLY when the
// operation family yields nothing at all. #6459's shape — `message Foo` +
// `service Foo`, with no rpc named Foo — is exactly the "nothing at all" case.
func TestProtoRpcNamedAfterASiblingServiceStillBinds6492(t *testing.T) {
	const protoFile = "api/orders/v1/orders.proto"

	msg := types.EntityRecord{
		ID: "f0f0f0f0f0f0f0f0", Kind: "SCOPE.Schema", Subtype: "message",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	svcUser := types.EntityRecord{
		ID: "6544153a6544153a", Kind: "SCOPE.Service", Subtype: "service",
		Name: "User", SourceFile: protoFile, Language: "protobuf",
	}
	svcAdmin := types.EntityRecord{
		ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Service", Subtype: "service",
		Name: "Admin", SourceFile: protoFile, Language: "protobuf",
	}
	rpcGet := types.EntityRecord{
		ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Operation", Subtype: "rpc",
		Name: "Get", SourceFile: protoFile, Language: "protobuf",
	}
	// The collision: service User (above) and Admin's rpc User share a name.
	rpcUser := types.EntityRecord{
		ID: "c55020864eaf05ae", Kind: "SCOPE.Operation", Subtype: "rpc",
		Name: "User", SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{msg, svcUser, svcAdmin, rpcGet, rpcUser})

	ref := extractor.BuildOperationStructuralRef("proto", protoFile, "User")
	id, status, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id != rpcUser.ID {
		t.Fatalf("lookupStructural(%q) = %q (status=%d), want the rpc %q — "+
			"`service Admin { rpc User(...) }` beside `service User` is ordinary "+
			"proto, and buildService addresses that rpc with this exact ref. A "+
			"SCOPE.Service member in the family this ref is filtered by makes the "+
			"service User entity %q a second match, uniqueMatchInFamily returns "+
			"no match, and the service Admin → rpc User CONTAINS edge DANGLES. "+
			"One correct edge destroyed, zero gained (#6492 B1)",
			ref, id, status, rpcUser.ID, svcUser.ID)
	}
	if status != statusRewritten {
		t.Fatalf("lookupStructural(%q) status = %d, want statusRewritten (%d) (#6492 B1)",
			ref, status, statusRewritten)
	}

	// The non-colliding rpc in the same file is the control: if it too broke,
	// the failure above would be about something other than the collision.
	refGet := extractor.BuildOperationStructuralRef("proto", protoFile, "Get")
	if got, _, _ := idx.lookupStructural(refGet); got != rpcGet.ID {
		t.Fatalf("control: lookupStructural(%q) = %q, want %q — the non-colliding "+
			"rpc must bind regardless (#6492 B1)", refGet, got, rpcGet.ID)
	}
	// The non-colliding SERVICE in the same file is the other control: it must
	// keep its inbound file → service CONTAINS binding.
	refAdmin := extractor.BuildOperationStructuralRef("proto", protoFile, "Admin")
	if got, _, _ := idx.lookupStructural(refAdmin); got != svcAdmin.ID {
		t.Fatalf("control: lookupStructural(%q) = %q, want the service %q (#6492 B1)",
			refAdmin, got, svcAdmin.ID)
	}
}

// TestProtoDegenerateSelfNamedRpcStillBinds6492 is the same defect at its
// smallest: `service User { rpc User(...) }` — one service, one rpc, same name,
// same file. The rpc is what the operation-space ref means (buildService mints
// it for the CONTAINS child edge); the service's own inbound edge is addressed
// identically and was already indistinguishable from it BEFORE any fix, so the
// tier must not trade the rpc away trying to disambiguate a tie it cannot win.
func TestProtoDegenerateSelfNamedRpcStillBinds6492(t *testing.T) {
	const protoFile = "api/user/v1/user.proto"

	svc := types.EntityRecord{
		ID: "1111111111111111", Kind: "SCOPE.Service", Subtype: "service",
		Name: "User", SourceFile: protoFile, Language: "protobuf",
	}
	rpc := types.EntityRecord{
		ID: "2222222222222222", Kind: "SCOPE.Operation", Subtype: "rpc",
		Name: "User", SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{svc, rpc})

	ref := extractor.BuildOperationStructuralRef("proto", protoFile, "User")
	id, _, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id != rpc.ID {
		t.Fatalf("lookupStructural(%q) = %q, want the rpc %q — the operation family "+
			"matches the rpc uniquely and must be consulted FIRST; the service %q "+
			"may only be reached when the operation family matches NOTHING (#6492 B1)",
			ref, id, rpc.ID, svc.ID)
	}
}

// TestProtoServiceTierDoesNotBreakAnRpcAmbiguityTie6492 pins the "nothing at
// all, not merely ambiguous" half of the tier's precondition.
//
// Two services in one .proto declaring an rpc of the same name is ordinary:
//
//	service A  { rpc Do(Foo) returns (Foo); }
//	service B  { rpc Do(Foo) returns (Foo); }
//	service Do { }
//
// Both rpcs land at the same (file, "Do"), so the SCOPE.Operation bucket carries
// the ambiguous-within-kind blank sentinel and the operation family yields no
// unique match. That is NOT the #6459 shape: there ARE operation entities here
// and the resolver simply cannot pick one. A tier that fired on "the operation
// family returned no id" rather than "the operation family has no member here"
// would hand the tie to `service Do` — silently answering an rpc reference with
// a service. The precondition scans for PRESENCE, blank sentinel included.
func TestProtoServiceTierDoesNotBreakAnRpcAmbiguityTie6492(t *testing.T) {
	const protoFile = "api/tie/v1/tie.proto"

	svc := types.EntityRecord{
		ID: "5555555555555555", Kind: "SCOPE.Service", Subtype: "service",
		Name: "Do", SourceFile: protoFile, Language: "protobuf",
	}
	rpcA := types.EntityRecord{
		ID: "6666666666666666", Kind: "SCOPE.Operation", Subtype: "rpc",
		Name: "Do", SourceFile: protoFile, Language: "protobuf",
	}
	rpcB := types.EntityRecord{
		ID: "7777777777777777", Kind: "SCOPE.Operation", Subtype: "rpc",
		Name: "Do", SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{svc, rpcA, rpcB})

	// Premise: the two rpcs really did collide into the blank sentinel, so the
	// operation family is present-but-unresolvable rather than absent.
	if id, ok := idx.lookupLocationKind(protoFile, "Do", operationKindFamily); ok {
		t.Fatalf("premise: lookupLocationKind picked %q out of two same-named rpcs; "+
			"the ambiguity this test needs does not exist (#6492)", id)
	}

	ref := extractor.BuildOperationStructuralRef("proto", protoFile, "Do")
	id, _, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id == svc.ID {
		t.Fatalf("lookupStructural(%q) = the service %q; the #6459 tier must fire only "+
			"when the operation family has NO member at this (file, name), never to "+
			"break a tie between two rpcs (#6492)", ref, svc.ID)
	}
	if id != "" {
		t.Fatalf("lookupStructural(%q) = %q, want no binding — two same-named rpcs are "+
			"genuinely ambiguous (#6492)", ref, id)
	}
}

// TestProtoServiceTierRequiresTheScopeKind6492 pins the tier's kind boundary in
// the permissive direction.
//
// The tier's family is exactly [SCOPE.Service] — one member, deliberately. A
// bare `Service` Kind is NOT the proto extractor's spelling (proto.go emits
// "SCOPE.Service"), and BuildIndex never synthesises the SCOPE. prefix: it
// dual-indexes a SCOPE.* kind under its trimmed alias, not the other way
// round. So a bare-`Service` entity must stay unreachable from the tier —
// otherwise the ~60 non-proto emitters that use the bare spelling, plus any
// real `Service` class in a proto-adjacent file, become bindable by an
// operation-space ref.
func TestProtoServiceTierRequiresTheScopeKind6492(t *testing.T) {
	const protoFile = "api/kind/v1/kind.proto"
	msg := types.EntityRecord{
		ID: "8888888888888888", Kind: "SCOPE.Schema", Subtype: "message",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	bare := types.EntityRecord{
		ID: "9999999999999999", Kind: "Service", Subtype: "service",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	idx := BuildIndex([]types.EntityRecord{msg, bare})

	ref := extractor.BuildOperationStructuralRef("proto", protoFile, "Foo")
	if id, _, _ := idx.lookupStructural(ref); id == bare.ID {
		t.Fatalf("lookupStructural(%q) = the bare-`Service`-kinded entity %q; the "+
			"#6459 tier's family is exactly [%q] (#6492)",
			ref, bare.ID, scopeKindPrefix+"Service")
	}

	// Positive control in the SAME shape: swap the kind to the SCOPE. spelling
	// the proto extractor actually emits and the tier must fire. Without this
	// the assertion above could pass because the tier is broken outright.
	scoped := bare
	scoped.Kind = scopeKindPrefix + "Service"
	idxScoped := BuildIndex([]types.EntityRecord{msg, scoped})
	if id, _, _ := idxScoped.lookupStructural(ref); id != scoped.ID {
		t.Fatalf("control: lookupStructural(%q) = %q, want the SCOPE.Service %q — "+
			"the tier is not firing at all, so the bare-kind assertion is vacuous (#6492)",
			ref, id, scoped.ID)
	}
}

// TestProtoServiceTierPreconditionScansTheWholeFamilyAndBase6492 pins the two
// halves of the tier's precondition that a narrower scan would silently drop.
//
// The precondition is `for _, k := range operationKindFamily { if present in
// locEnt.base -> bail }`. Two mutations of it survived the rest of the suite,
// and NEITHER is equivalent:
//
//	S2: drop the `.base` half, scan only `.real`.
//	S3: scan `[]string{scopeKindPrefix + "Operation"}` instead of the whole
//	    operationKindFamily.
//
// Both go blind to the same asymmetry. BuildIndex writes `.base` under the raw
// Kind AND its SCOPE-trimmed alias, but `.real` under the raw Kind alone. So an
// entity kinded "SCOPE.Method" occupies `.base["Method"]` — a member of
// operationKindFamily — and `.real["SCOPE.Method"]`, which is NOT a member.
//
// The precondition is only LOAD-BEARING when the operation family is
// present-but-ambiguous: a unique operation match is bound by lookupLocationKind
// one tier earlier and the service tier never runs at all. So each fixture below
// puts TWO same-named operation entities in the file, which blanks the family's
// bucket to the ambiguous-within-kind sentinel. That is
// TestProtoServiceTierDoesNotBreakAnRpcAmbiguityTie6492's shape, generalised off
// the single kind spelling that test happens to use: under S2 or S3 the tier
// goes blind to the sentinel, fires, and answers an operation reference with a
// SCOPE.Service.
//
// Nothing emits SCOPE.Method under a .proto path today — internal/extractors/
// proto/proto.go mints exactly SCOPE.{Service,Operation,Schema,Component}. That
// is an accident of the current extractor, not an invariant, and it is the
// identical shape that produced this PR's two earlier regressions. This test
// pins the precondition against the shape rather than against the accident.
func TestProtoServiceTierPreconditionScansTheWholeFamilyAndBase6492(t *testing.T) {
	const protoFile = "api/pre/v1/pre.proto"
	ref := extractor.BuildOperationStructuralRef("proto", protoFile, "Foo")
	svc := types.EntityRecord{
		ID: "aaaa0000aaaa0000", Kind: "SCOPE.Service", Subtype: "service",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}

	for _, tc := range []struct {
		name string
		kind string
		// fires inverts the expectation: the row's kind is OUTSIDE
		// operationKindFamily, so the precondition must NOT see it and the
		// tier must still bind the service. These rows pin the bail set from
		// GROWING, which the rows above cannot do — every mutation that
		// WIDENS the scanned family passes all of them.
		fires bool
	}{
		// Trimmed-alias-only members: the sentinel lands in .base under the
		// ALIAS ("Method"/"Function"), while .real carries only the raw
		// "SCOPE.Method"/"SCOPE.Function", which is in no family. Kills S2
		// (and S3, since neither alias is "SCOPE.Operation").
		{"scope-method", scopeKindPrefix + "Method", false},
		{"scope-function", scopeKindPrefix + "Function", false},
		// Bare spellings: same key in .base and .real, so S2 still bails —
		// but the key is not the single one S3 narrows to. Kills S3.
		{"bare-method", "Method", false},
		{"bare-function", "Function", false},
		{"bare-operation", "Operation", false},
		// The other direction: the precondition must scan
		// operationKindFamily and NOT a superset of it. Swapping it for
		// componentOrOperationKindFamily — the union that literally contains
		// operationKindFamily — survived every row above, and the widening
		// localises to exactly these two keys, since a SCOPE.Component
		// entity occupies .base under BOTH "SCOPE.Component" and the trimmed
		// alias "Component".
		//
		// This is not a hypothetical kind under a .proto path.
		// buildImportEntities (internal/extractors/proto/proto.go) mints a
		// SCOPE.Component whose Name is the VERBATIM quoted import string,
		// in the importing file — the grammar accepts any string and grafel
		// never runs protoc — so `import "Foo";` next to `service Foo` puts a
		// SCOPE.Component at the service's own (file, name). Under the
		// widened scan the tier bails there and the file → service CONTAINS
		// edge dangles: #6459's exact symptom, on valid proto, with no
		// extractor change required. See
		// TestSelfNamedImportDoesNotBlockTheServiceTier6492 in
		// internal/extractors/proto for the end-to-end shape.
		{"scope-component", scopeKindPrefix + "Component", true},
		{"bare-component", "Component", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opA := types.EntityRecord{
				ID: "bbbb0000bbbb0000", Kind: tc.kind, Subtype: "rpc",
				Name: "Foo", SourceFile: protoFile, Language: "protobuf",
			}
			opB := opA
			opB.ID = "cccc0000cccc0000"
			idx := BuildIndex([]types.EntityRecord{svc, opA, opB})

			id, status, handled := idx.lookupStructural(ref)
			if !handled {
				t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
			}
			if tc.fires {
				if id != svc.ID {
					t.Fatalf("lookupStructural(%q) = (%q, status=%d), want the service "+
						"%q. Two %q-kinded entities share this (file, name), but that "+
						"kind is NOT in operationKindFamily, so the tier's precondition "+
						"must not see them and the service must still bind. A "+
						"precondition scanning any SUPERSET of operationKindFamily "+
						"(componentOrOperationKindFamily, or the family plus "+
						"%q) bails here instead and re-opens #6459 — the proto "+
						"extractor mints exactly this kind for `import \"Foo\";` in the "+
						"file that declares `service Foo` (#6492)",
						ref, id, status, svc.ID, tc.kind, scopeKindPrefix+"Component")
				}
				if status != statusRewritten {
					t.Fatalf("lookupStructural(%q) status = %d, want statusRewritten=%d (#6492)",
						ref, status, statusRewritten)
				}
				return
			}
			if id == svc.ID {
				t.Fatalf("lookupStructural(%q) = the SCOPE.Service %q, but two %q-kinded "+
					"operation entities share this (file, name). The tier's precondition "+
					"must scan the WHOLE operationKindFamily against locEnt.base — a "+
					"SCOPE-prefixed kind lands in .base under its TRIMMED alias and in "+
					".real under the raw kind, so a .real-only or single-key scan is "+
					"blind to the ambiguity sentinel and lets a service silently win a "+
					"tie between real operations (#6492)", ref, id, tc.kind)
			}
			if id != "" || status != statusAmbiguous {
				t.Fatalf("lookupStructural(%q) = (%q, status=%d), want (\"\", "+
					"statusAmbiguous=%d) — two operation entities tie at this (file, "+
					"name) and the tier must bail, leaving the pre-#6459 disposition (#6492)",
					ref, id, status, statusAmbiguous)
			}
		})
	}

	// The tier's scope-kind gate is case-insensitive (strings.EqualFold), and
	// so is structuralKindFamilies one line above it. Pin BOTH at the same
	// spelling: an `EqualFold -> ==` mutation on the gate otherwise survives,
	// because the sibling structuralKindFamilies test pins an "OPERATION" row
	// but nothing pinned it here (#6492 S5).
	upperRef := "scope:OPERATION:method:proto:" + protoFile + ":Foo"
	msgForUpper := types.EntityRecord{
		ID: "eeee0000eeee0000", Kind: "SCOPE.Schema", Subtype: "message",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	if id, _, _ := BuildIndex([]types.EntityRecord{svc, msgForUpper}).lookupStructural(upperRef); id != svc.ID {
		t.Fatalf("lookupStructural(%q) = %q, want the service %q — the scope-kind "+
			"segment is matched case-insensitively everywhere else in this "+
			"function, and the #6459 gate must not be the one exception (#6492 S5)",
			upperRef, id, svc.ID)
	}

	// Non-vacuity control: with the operation entities REMOVED, the very same
	// ref must bind the service. Otherwise every assertion above could pass
	// because the tier is broken outright.
	msg := types.EntityRecord{
		ID: "dddd0000dddd0000", Kind: "SCOPE.Schema", Subtype: "message",
		Name: "Foo", SourceFile: protoFile, Language: "protobuf",
	}
	if id, _, _ := BuildIndex([]types.EntityRecord{svc, msg}).lookupStructural(ref); id != svc.ID {
		t.Fatalf("control: lookupStructural(%q) = %q, want the service %q — the tier "+
			"is not firing at all, so the assertions above are vacuous (#6492)",
			ref, id, svc.ID)
	}
}
