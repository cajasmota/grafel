package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6141 — the leaf-name tiers (lookupMemberByLeafName /
// lookupPackageMemberByLeafName) bind a bare CALLS stub to ANY entity in
// the caller's file / package directory whose dotted Name ends in the
// stub, regardless of the entity's Kind. A call target must be an
// operation; without a kind filter the tier binds calls to FIELDS.
//
// The defect costs precision AND recall at once, in opposite directions:
//
//   - PRECISION: a bare `owner()` call binds cross-scope, cross-file to a
//     sibling contract's `address public owner` state variable.
//   - RECALL: a CORRECT operation binding is LOST when an unrelated
//     same-leaf-named field joins the candidate set and trips the
//     ambiguity guard.
//
// Every assertion below is on what the edge binds TO, by content
// (kind / name / source_file) — never on an aggregate dangling count,
// which is blind here because the two symptoms cancel.

// callerWithCall builds an entity record carrying one embedded bare CALLS
// edge, which is the shape ReferencesEmbedded resolves with caller
// (file, pkgDir) context — the only path that reaches the leaf-name tiers.
func callerWithCall(id, kind, name, file, target string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: kind, Name: name, SourceFile: file,
		Relationships: []types.RelationshipRecord{{
			FromID: id, ToID: target, Kind: "CALLS",
		}},
	}
}

// boundTo describes what an edge actually bound to, resolved back through
// the entity set by ID. A nil result means the edge is still a stub.
type boundTo struct {
	id, kind, name, file string
}

// resolveEdge runs ReferencesEmbedded over records and returns the content
// of whatever records[callerIdx]'s first embedded edge bound to. Returns
// ("", ...) with stub set when the edge was left verbatim.
func resolveEdge(t *testing.T, records []types.EntityRecord, callerIdx int) (got boundTo, stub string) {
	t.Helper()
	idx := BuildIndex(records)
	ReferencesEmbedded(records, idx)
	to := records[callerIdx].Relationships[0].ToID
	for i := range records {
		if records[i].ID == to {
			return boundTo{records[i].ID, records[i].Kind, records[i].Name, records[i].SourceFile}, ""
		}
	}
	return boundTo{}, to
}

// --- PRECISION: the wrong bind --------------------------------------------

// TestLeafNameTier_PackageScope_MustNotBindCallToField_6141 is the wrong-bind
// half, package-scoped. contracts/Vault.sol calls bare `owner()`; a SIBLING
// FILE declares the state variable `Registry.owner`. Nothing in the package
// declares an OPERATION named `owner`, so the correct outcome is an
// unresolved stub — not a cross-contract, cross-file bind to a field.
func TestLeafNameTier_PackageScope_MustNotBindCallToField_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("2222222222222222", "SCOPE.Schema", "Registry.owner", "contracts/Registry.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "owner" {
		t.Fatalf("bare CALLS `owner` bound to a non-operation: id=%s kind=%s name=%s file=%s; "+
			"want the stub %q left verbatim (no operation named `owner` exists in the package)",
			got.id, got.kind, got.name, got.file, "owner")
	}
}

// TestLeafNameTier_FileScope_MustNotBindCallToField_6141 is the wrong-bind
// half, file-scoped (lookupMemberByLeafName). Same file, different scope:
// `Vault.deposit` calls bare `owner()` and only `Other.owner` — a field —
// declares that leaf name.
func TestLeafNameTier_FileScope_MustNotBindCallToField_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("3333333333333333", "SCOPE.Schema", "Other.owner", "contracts/Vault.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "owner" {
		t.Fatalf("same-file bare CALLS `owner` bound to a non-operation: id=%s kind=%s name=%s file=%s; "+
			"want the stub %q left verbatim", got.id, got.kind, got.name, got.file, "owner")
	}
}

// --- RECALL: the lost edge -------------------------------------------------

// TestLeafNameTier_PackageScope_FieldMustNotShadowOperation_6141 is the
// recall half, package-scoped. `Owned.owner()` is a real operation in a
// sibling file and is the ONLY legitimate target. Adding an unrelated
// `Other.owner` FIELD to the same package directory trips the cross-scope
// ambiguity guard and the correct edge is lost. The field is not a call
// target and must never enter the candidate set.
func TestLeafNameTier_PackageScope_FieldMustNotShadowOperation_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("4444444444444444", "SCOPE.Operation", "Owned.owner", "contracts/Owned.sol"),
		entAt("5555555555555555", "SCOPE.Schema", "Other.owner", "contracts/Other.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `owner` left as stub %q; want it bound to the operation "+
			"Owned.owner [SCOPE.Operation] @ contracts/Owned.sol — the sibling FIELD "+
			"Other.owner must not enter the candidate set", stub)
	}
	if got.id != "4444444444444444" || got.kind != "SCOPE.Operation" ||
		got.name != "Owned.owner" || got.file != "contracts/Owned.sol" {
		t.Fatalf("bound to id=%s kind=%s name=%s file=%s; want id=4444444444444444 "+
			"kind=SCOPE.Operation name=Owned.owner file=contracts/Owned.sol",
			got.id, got.kind, got.name, got.file)
	}
}

// TestLeafNameTier_FileScope_FieldMustNotShadowOperation_6141 is the recall
// half, file-scoped. Two scopes in ONE file: `Owned.owner` (operation, the
// correct target) and `Other.owner` (field). The file-scoped tier's
// cross-scope ambiguity guard drops the correct edge.
func TestLeafNameTier_FileScope_FieldMustNotShadowOperation_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("6666666666666666", "SCOPE.Operation", "Owned.owner", "contracts/Vault.sol"),
		entAt("7777777777777777", "SCOPE.Schema", "Other.owner", "contracts/Vault.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("same-file bare CALLS `owner` left as stub %q; want it bound to the "+
			"operation Owned.owner [SCOPE.Operation]", stub)
	}
	if got.id != "6666666666666666" || got.kind != "SCOPE.Operation" || got.name != "Owned.owner" {
		t.Fatalf("bound to id=%s kind=%s name=%s file=%s; want id=6666666666666666 "+
			"kind=SCOPE.Operation name=Owned.owner", got.id, got.kind, got.name, got.file)
	}
}

// --- POSITIVE CONTROLS: what must keep binding ----------------------------

// TestLeafNameTier_JavaCrossFileMethodStillBinds_6141 is the #778 contract
// the tiers exist for: a bare Java CALLS stub binding to a same-package,
// cross-file method. It must be untouched by the kind filter. Java methods
// are emitted with Kind "Operation".
func TestLeafNameTier_JavaCrossFileMethodStillBinds_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "Operation", "OrderService.place", "src/svc/OrderService.java", "merge"),
		entAt("8888888888888888", "Operation", "InventoryService.merge", "src/svc/InventoryService.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("#778 Java cross-file bare CALLS regressed: stub %q left verbatim", stub)
	}
	if got.id != "8888888888888888" || got.name != "InventoryService.merge" {
		t.Fatalf("bound to id=%s kind=%s name=%s; want InventoryService.merge", got.id, got.kind, got.name)
	}
}

// TestLeafNameTier_EveryOperationKindStillBinds_6141 pins the whole
// operationKindFamily through the leaf-name tiers. If any language emits
// its callables under one of these kinds, the filter must not drop it.
func TestLeafNameTier_EveryOperationKindStillBinds_6141(t *testing.T) {
	for _, kind := range []string{"Operation", "Function", "Method", "SCOPE.Operation"} {
		t.Run(kind, func(t *testing.T) {
			records := []types.EntityRecord{
				callerWithCall("1111111111111111", "Operation", "Caller.run", "src/pkg/Caller.java", "helper"),
				entAt("9999999999999999", kind, "Util.helper", "src/pkg/Util.java"),
			}
			got, stub := resolveEdge(t, records, 0)
			if stub != "" {
				t.Fatalf("callable of kind %q no longer binds through the leaf-name tier: stub %q", kind, stub)
			}
			if got.id != "9999999999999999" {
				t.Fatalf("kind %q bound to id=%s name=%s; want 9999999999999999 / Util.helper",
					kind, got.id, got.name)
			}
		})
	}
}

// TestLeafNameTier_TwoOperationsStillAmbiguous_6141 is the false-negative
// control on the ambiguity guard itself: the kind filter must NARROW the
// candidate set, never DISABLE the guard. Two genuine operations sharing a
// leaf name in one package remain unresolvable.
func TestLeafNameTier_TwoOperationsStillAmbiguous_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "Operation", "Caller.run", "src/pkg/Caller.java", "find"),
		entAt("aaaaaaaaaaaaaaaa", "Operation", "RepoA.find", "src/pkg/RepoA.java"),
		entAt("bbbbbbbbbbbbbbbb", "Operation", "RepoB.find", "src/pkg/RepoB.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "find" {
		t.Fatalf("two same-named operations bound anyway: id=%s name=%s file=%s; "+
			"want the ambiguity guard to leave the stub verbatim", got.id, got.name, got.file)
	}
}

// TestLeafNameTier_JavaFieldHintStubStillBinds_6141 is the scope guard on
// the OTHER caller of lookupPackageMemberByLeafName: the #667 Java
// `scope:schema:ref:java:<file>:<Class>.<field>` inherited-field hint. That
// call site is FIELD-shaped and must NOT be operation-filtered.
//
// TestJavaExtendsFieldRef already covers the shape, but it CANNOT protect
// this call site: with a single global `*.parentField` entity the resolver
// would fall through to lookupUniqueSchemaFieldByName and bind anyway, so
// filtering the package tier would not fail it. A DECOY field of the same
// leaf name in a DIFFERENT package directory makes the global-unique tier
// ambiguous, leaving lookupPackageMemberByLeafName as the only tier that
// can bind — which is what makes this a real guard.
func TestLeafNameTier_JavaFieldHintStubStillBinds_6141(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID: "1111111111111111", Kind: "Operation", Name: "Child.use", SourceFile: "src/Child.java",
			Relationships: []types.RelationshipRecord{{
				FromID: "1111111111111111",
				ToID:   "scope:schema:ref:java:src/Child.java:Child.parentField",
				Kind:   "REFERENCES",
			}},
		},
		entAt("cccccccccccccccc", "SCOPE.Schema", "Parent.parentField", "src/Parent.java"),
		// Decoy in another package — defeats lookupUniqueSchemaFieldByName.
		entAt("dddddddddddddddd", "SCOPE.Schema", "Elsewhere.parentField", "other/Elsewhere.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("#667 Java inherited-field hint stub regressed: %q left verbatim", stub)
	}
	if got.id != "cccccccccccccccc" || got.kind != "SCOPE.Schema" {
		t.Fatalf("bound to id=%s kind=%s name=%s; want cccccccccccccccc / SCOPE.Schema / Parent.parentField",
			got.id, got.kind, got.name)
	}
}
