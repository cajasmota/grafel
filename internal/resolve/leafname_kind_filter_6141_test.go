package resolve

import (
	"strings"
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
// ONLY THE RECALL HALF IS FIXED, and deliberately so. The issue prescribes
// hard-filtering these tiers to operationKindFamily. That was implemented,
// then MEASURED against the real extractors and reverted: Ruby models every
// attr_accessor as `Class.attr` / SCOPE.Schema and JS/TS models
// `handler = function(){}` the same way, while both languages emit their
// real methods under BARE names — so for those languages the leaf tier is
// the ONLY binding route to what is genuinely a method. A hard filter turns
// those correct edges into dangles. See scanLeafMembersPreferring in refs.go
// and TestRubyAttrAccessorCallStillBinds_6141 in internal/extractors/ruby,
// which measures it end-to-end through the actual Ruby extractor.
//
// The tiers therefore PREFER operations rather than requiring them. The
// precision half is pinned below as a known gap rather than silently left
// untested.
//
// #6177 UPDATE — the Solidity slice of that gap is now closed by ELIGIBILITY
// (see Index.uncallableMember), pinned in
// solidity_nonpublic_field_calls_6177_test.go. The gap fixtures below still
// describe live behaviour: they stamp no Language and no Signature, so the new
// rule cannot reach them, and the gap itself is still open for Java and Go.
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

// --- PRECISION: the wrong bind, PINNED AS A KNOWN GAP ----------------------

// TestLeafNameTier_PrecisionGap_CallStillBindsToField_6141 is a
// CHARACTERISATION test: it asserts behaviour that is still WRONG for
// Solidity/Java/Go, so that the tradeoff is visible rather than forgotten.
//
// contracts/Vault.sol calls bare `owner()`; a SIBLING FILE declares the
// state variable `Registry.owner`. No operation of that leaf name exists,
// so the operation-preferring pass finds nothing and the unrestricted pass
// binds the call cross-contract, cross-file to the FIELD — exactly the
// mis-bind issue #6141 reports.
//
// It is left unfixed because the only resolver-side remedy (refuse
// non-operation candidates outright) was measured to destroy real Ruby and
// JS/TS bindings; see the file header. Closing it properly needs the
// extractors to distinguish a data field from an invocable member modelled
// as a field.
//
// If a future change makes this bind correctly refuse, this test SHOULD
// fail — update it, and check TestRubyAttrAccessorCallStillBinds_6141 still
// passes, because that is the constraint that put the gap here.
func TestLeafNameTier_PrecisionGap_CallStillBindsToField_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("2222222222222222", "SCOPE.Schema", "Registry.owner", "contracts/Registry.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `owner` now refuses to bind (stub %q). If that was intentional, "+
			"confirm the Ruby attr_accessor end-to-end test still passes before updating this test", stub)
	}
	if got.id != "2222222222222222" || got.kind != "SCOPE.Schema" || got.name != "Registry.owner" {
		t.Fatalf("known precision gap changed shape: bound to id=%s kind=%s name=%s file=%s",
			got.id, got.kind, got.name, got.file)
	}
}

// TestLeafNameTier_PrecisionGap_FileScope_6141 is the same known gap on the
// file-scoped tier. Same file, different scope: `Vault.deposit` calls bare
// `owner()` and only the field `Other.owner` declares that leaf name.
func TestLeafNameTier_PrecisionGap_FileScope_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("3333333333333333", "SCOPE.Schema", "Other.owner", "contracts/Vault.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("same-file bare CALLS `owner` now refuses to bind (stub %q); see the sibling "+
			"package-scoped gap test before updating", stub)
	}
	if got.id != "3333333333333333" || got.kind != "SCOPE.Schema" {
		t.Fatalf("known precision gap changed shape: bound to id=%s kind=%s name=%s",
			got.id, got.kind, got.name)
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

// --- DIRECT unit tests of the new mechanism -------------------------------
//
// These exist because the end-to-end tests above cannot reach every arm.
// memberFamilyMask's SCOPE.-trim branch in particular has no fixture: no
// extractor emits "SCOPE.Method" today, so the branch would survive
// mutation while only being protected by a language nobody ships. Testing
// the helper directly is what makes it a guard rather than decoration.

func TestMemberFamilyMask_6141(t *testing.T) {
	cases := []struct {
		kind string
		want uint8
	}{
		// Operation family, raw.
		{"Operation", famOperation},
		{"Function", famOperation},
		{"Method", famOperation},
		{"SCOPE.Operation", famOperation},
		// Operation family reached ONLY via the SCOPE.-trim branch.
		{"SCOPE.Method", famOperation},
		{"SCOPE.Function", famOperation},
		// Schema family — the kinds a call must never bind to.
		{"Schema", famSchema},
		{"Field", famSchema},
		{"Property", famSchema},
		{"SCOPE.Schema", famSchema},
		// Component family.
		{"Component", famComponent},
		{"Class", famComponent},
		{"SCOPE.Component", famComponent},
		// Outside every family — must classify as 0 so a family-filtered
		// lookup rejects it. These are real kinds seen on dotted-name
		// entities (SCOPE.Pattern from internal/custom, Module from the
		// python package-module emitter, SCOPE.Datastore from dbt sources).
		{"SCOPE.Pattern", 0},
		{"Module", 0},
		{"SCOPE.Datastore", 0},
		{"Handler", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := memberFamilyMask(c.kind); got != c.want {
			t.Errorf("memberFamilyMask(%q) = %d, want %d", c.kind, got, c.want)
		}
	}
}

// TestMemberFamilyMask_FamiliesAreDisjoint_6141 pins the assumption the
// bitmask encoding rests on: no entity kind sits in two families at once.
// If a future kind is added to two of the slices the mask silently becomes
// a union and a call could bind to a field again.
//
// #6492 — this iterates memberFamilyMask over the UNION of the family
// members, not the raw familyMaskByKind map. The raw map cannot see the
// hazard it is supposed to guard: memberFamilyMask ORs a kind's own mask
// with the mask of its SCOPE.-trimmed form, so a bare "Service" in one
// family and a "SCOPE.Service" in another produce two separate single-bit
// map entries — a raw scan finds both clean — while memberFamilyMask
// ("SCOPE.Service") returns a two-bit mask and the filter is no longer
// discriminating. Classifying through the real function closes that gap.
func TestMemberFamilyMask_FamiliesAreDisjoint_6141(t *testing.T) {
	seen := make(map[string]bool)
	for _, fam := range [][]string{
		operationKindFamily, componentKindFamily, schemaKindFamily,
		componentOrOperationKindFamily, protoOperationKindFamily,
	} {
		for _, kind := range fam {
			if seen[kind] {
				continue
			}
			seen[kind] = true
			// Classify through the same entry point the leaf-name filter
			// uses, and through both spellings BuildIndex dual-indexes.
			for _, probe := range []string{kind, scopeKindPrefix + strings.TrimPrefix(kind, scopeKindPrefix)} {
				mask := memberFamilyMask(probe)
				if mask&(mask-1) != 0 {
					t.Errorf("kind %q (probed as %q) belongs to more than one kind family "+
						"(mask=%b); the leaf-name filter assumes the families are disjoint",
						kind, probe, mask)
				}
			}
		}
	}
	// Guard the premise: the scan must actually have classified something,
	// otherwise an empty family set would make this test vacuously green.
	if len(seen) < len(operationKindFamily) {
		t.Fatalf("disjointness scan covered only %d kinds; the family slices are "+
			"empty or unreachable", len(seen))
	}
}

// TestInFamily_ZeroMaskDisablesFilter_6141 pins the escape hatch the #667
// Java field call site depends on.
func TestInFamily_ZeroMaskDisablesFilter_6141(t *testing.T) {
	idx := BuildIndex([]types.EntityRecord{
		entAt("1111111111111111", "SCOPE.Schema", "Parent.field", "src/Parent.java"),
	})
	if !idx.inFamily("1111111111111111", 0) {
		t.Error("want == 0 must disable the filter entirely")
	}
	if idx.inFamily("1111111111111111", famOperation) {
		t.Error("a SCOPE.Schema member must not pass the operation filter")
	}
	if !idx.inFamily("1111111111111111", famSchema) {
		t.Error("a SCOPE.Schema member must pass the schema filter")
	}
	if idx.inFamily("no-such-id", famOperation) {
		t.Error("an id absent from memberFamily must not pass a non-zero filter")
	}
}

// TestMemberFamily_OnlyDottedNamesIndexed_6141 pins the side-table's size
// contract: it exists to serve the member indexes, so an entity that never
// reaches byMember must not occupy a slot in it.
func TestMemberFamily_OnlyDottedNamesIndexed_6141(t *testing.T) {
	idx := BuildIndex([]types.EntityRecord{
		entAt("1111111111111111", "SCOPE.Operation", "Bare", "pkg/a.go"),          // undotted
		entAt("2222222222222222", "SCOPE.Operation", "Scoped.member", "pkg/b.go"), // dotted
		entAt("3333333333333333", "SCOPE.Pattern", "Other.member", "pkg/c.go"),    // dotted, no family
	})
	if _, ok := idx.memberFamily["1111111111111111"]; ok {
		t.Error("undotted entity must not enter memberFamily — it never reaches byMember")
	}
	if idx.memberFamily["2222222222222222"] != famOperation {
		t.Errorf("dotted operation missing from memberFamily: %d", idx.memberFamily["2222222222222222"])
	}
	if _, ok := idx.memberFamily["3333333333333333"]; ok {
		t.Error("zero-mask kind must be OMITTED, not stored — the entry would cost memory for nothing")
	}
}

// TestLeafNameTier_SameScopeDuplicateStaysAmbiguous_6141 documents the
// mechanism's blind spot, so the limitation is pinned rather than assumed.
// When a field and an operation collide inside ONE scope — the Java
// `Cell.borderTop` field + setter shape, and the internal/custom pattern of
// re-emitting `Class.method` under SCOPE.Pattern — byMember stores a BLANK
// sentinel and both entity IDs are erased. The kind filter cannot see
// through that, so the edge is ambiguous before AND after the fix.
func TestLeafNameTier_SameScopeDuplicateStaysAmbiguous_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "Operation", "Caller.run", "src/pkg/Caller.java", "borderTop"),
		entAt("2222222222222222", "SCOPE.Operation", "Cell.borderTop", "src/pkg/Cell.java"),
		entAt("3333333333333333", "SCOPE.Schema", "Cell.borderTop", "src/pkg/Cell.java"),
	}
	_, stub := resolveEdge(t, records, 0)
	if stub != "borderTop" {
		t.Fatalf("same-scope field/operation collision resolved to %q; the blank sentinel erases "+
			"both IDs before the kind filter can see them, so this must stay unresolved", stub)
	}
}

// TestLeafNameTier_FieldStubMustNotPreferOperation_6141 is the guard that
// makes the `0` mask at the #667 Java inherited-field call site actually
// load-bearing.
//
// It exists because the obvious guard is NOT one. With the two-pass design,
// passing famOperation at a field-shaped call site is invisible whenever
// only ONE candidate exists: the operation pass misses and the kind-blind
// fallback binds the field anyway. Mutating 0 -> famOperation there
// survived the whole suite. The mutation only becomes observable when the
// package holds BOTH a field and an operation of that leaf name — then an
// operation preference actively picks the wrong one.
//
// So: a field-shaped REFERENCES stub must never bind to Helper.parentField
// [SCOPE.Operation]. Leaving it unresolved is fine; binding to the
// operation is the failure.
func TestLeafNameTier_FieldStubMustNotPreferOperation_6141(t *testing.T) {
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
		// An OPERATION sharing the leaf name, in the same package.
		entAt("eeeeeeeeeeeeeeee", "SCOPE.Operation", "Helper.parentField", "src/Helper.java"),
		// Cross-package decoy — defeats lookupUniqueSchemaFieldByName so the
		// package leaf tier is the only thing that could bind.
		entAt("dddddddddddddddd", "SCOPE.Schema", "Elsewhere.parentField", "other/Elsewhere.java"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub == "" && got.id == "eeeeeeeeeeeeeeee" {
		t.Fatalf("a field-shaped REFERENCES stub bound to the OPERATION Helper.parentField "+
			"[%s] @ %s — the #667 call site must pass mask 0, never famOperation",
			got.kind, got.file)
	}
}

// TestLeafNameTier_PreferenceIsWithinTierNotAcrossTiers_6141 pins the tier
// ORDER itself, which nothing else in this file could distinguish.
//
// The operation preference is applied INSIDE each tier — (fileOp, fileAny)
// then (pkgOp, pkgAny) — so locality beats kind. The alternative structure,
// applying it ACROSS tiers (fileOp, pkgOp, fileAny, pkgAny), reaches into a
// sibling file for an operation rather than binding the caller's own file.
// Every other fixture here puts the competing member in a different file
// from the caller, so the two orderings are indistinguishable to them: a
// mutant swapping refs.go to the across-tiers order passed the entire
// internal/resolve, ruby and javascript suites.
//
//	caller  Vault.deposit  contracts/Vault.sol
//	field   Other.owner    contracts/Vault.sol   <- caller's OWN file
//	op      Owned.owner    contracts/Owned.sol
//
// Locality wins: the caller's own file is the better evidence about what a
// bare name means, and it is the rule the surrounding tiers already follow
// (lookupBareWithLocality tries byLocation[callerFile] before any
// package-scoped bucket). internal/extractors/sresolver.lookupLeaf MUST
// order its four passes the same way — see the twin test there. If these
// two drift apart, a full rebuild and an incremental build bind the same
// source differently.
func TestLeafNameTier_PreferenceIsWithinTierNotAcrossTiers_6141(t *testing.T) {
	records := []types.EntityRecord{
		callerWithCall("1111111111111111", "SCOPE.Operation", "Vault.deposit", "contracts/Vault.sol", "owner"),
		entAt("2222222222222222", "SCOPE.Schema", "Other.owner", "contracts/Vault.sol"),
		entAt("3333333333333333", "SCOPE.Operation", "Owned.owner", "contracts/Owned.sol"),
	}
	got, stub := resolveEdge(t, records, 0)
	if stub != "" {
		t.Fatalf("bare CALLS `owner` left as stub %q; want the caller's own file to win", stub)
	}
	if got.id != "2222222222222222" {
		t.Fatalf("the operation preference must apply WITHIN a tier, not ACROSS tiers: bound to "+
			"id=%s kind=%s name=%s file=%s; want the caller-file member Other.owner "+
			"(2222222222222222), not the sibling-file operation Owned.owner",
			got.id, got.kind, got.name, got.file)
	}
}
