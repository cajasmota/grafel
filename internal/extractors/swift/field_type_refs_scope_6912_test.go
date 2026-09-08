package swift

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm G — an INTERNAL test, and it exists for one reason: the
// `r.SourceFile != filePath` conjuncts in swiftInFileTypeTargets cannot be
// reached through Extract.
//
// Extract is called once per file and every record it builds carries that
// file's path, so from the outside neither conjunct ever fires and a mutant
// that deletes either survives the entire external suite. That is the "alive
// but unreachable" verdict — and the cheap answer to it is to call the function
// directly with the input the guard exists for, rather than to leave a conjunct
// ungraded and call it too expensive. The whole cost is this file.
//
// The two conjuncts fail in OPPOSITE directions and each needs its own row:
//
//	PASS 1 (the collision scans) — a foreign-file record sharing a same-file
//	  type's name must not SUPPRESS an edge that is perfectly unambiguous here.
//	PASS 2 (the target scan) — a foreign-file type declaration must not BECOME
//	  a target for this file.
//
// Grading one is exactly what makes the other look covered: the pass-2 rows
// below share no name with Models.swift, so a leak in pass 1 changes nothing
// about them; the pass-1 rows share a name but are not themselves admissible
// targets, so a leak in pass 2 changes nothing about them.
//
// The hazard is not hypothetical: attachSwiftExtends next door already takes a
// records slice and — unlike this pass — does NOT filter by file, so a future
// caller that batches files would silently start binding a field in one file to
// a same-named type in another. That is the cross-file hazard #6976/#6369 are
// about, and the same-file rule is this arm's central promise.
func TestSwiftFieldTypeRefs_TargetsAreScopedToTheRequestedFile(t *testing.T) {
	const a = "Sources/App/Models.swift"
	const b = "Sources/App/Other.swift"

	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: a},
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: a},
		{Name: "Tier", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: a},

		// --- pass 2 rows: same shape, other file. Neither may become a target
		// for `a` merely because some other file declares it.
		{Name: "Shipper", Kind: "SCOPE.Component", Subtype: "protocol", SourceFile: b},
		{Name: "Meters", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: b},

		// --- pass 1 rows: DIFFERENT-KINDED records in `b` sharing a name with
		// an `a` declaration. These grade the collision scans, which the rows
		// above cannot. With the conjunct removed:
		//   famKinds["Customer"] gains SCOPE.View — a component-family kind —
		//   and wrongly suppresses `a`'s unambiguous Customer;
		//   allKinds["Tier"] gains SCOPE.Operation and wrongly suppresses `a`'s
		//   unambiguous alias.
		// The two scans are separate maps with separate conjuncts, so both are
		// graded rather than one being paid for and its twin left open.
		{Name: "Customer", Kind: "SCOPE.View", Subtype: "class", SourceFile: b},
		{Name: "Tier", Kind: "SCOPE.Operation", Subtype: "function", SourceFile: b},
	}

	got := swiftInFileTypeTargets(records, a)

	want := map[string]string{
		"Customer": "scope:component:class:swift:" + a + ":Customer",
		"Order":    "scope:component:class:swift:" + a + ":Order",
		"Tier":     "scope:component:class:swift:" + a + ":Tier",
	}
	if len(got) != len(want) {
		t.Fatalf("target set has %d entries %v, want %d %v",
			len(got), swKeysOf(got), len(want), want)
	}
	for name, toID := range want {
		tgt, ok := got[name]
		if !ok {
			t.Fatalf("%q missing from %s's target set %v", name, a, swKeysOf(got))
		}
		if tgt.toID != toID {
			t.Errorf("%q toID = %q, want %q", name, tgt.toID, toID)
		}
	}
	for _, foreign := range []string{"Shipper", "Meters"} {
		if _, ok := got[foreign]; ok {
			t.Errorf("%q is declared in %s and must NOT be a target for %s — "+
				"the same-file rule is this arm's central promise", foreign, b, a)
		}
	}

	// The mirror direction, so the assertions above are not simply "b's records
	// were ignored entirely": asking for b returns b's declarations and none of
	// a's.
	gotB := swiftInFileTypeTargets(records, b)
	if _, ok := gotB["Shipper"]; !ok {
		t.Fatalf("%s's own target set %v is missing Shipper — the assertions "+
			"above would pass even if the function returned nothing", b, swKeysOf(gotB))
	}
	if _, ok := gotB["Customer"]; ok {
		t.Errorf("Customer must not be a target for %s: a's is cross-file and "+
			"b's own is a SCOPE.View, which is not an admitted type declaration", b)
	}
}

// TestSwiftFieldTypeRefs_TheTwoCollisionScansAreIndependent — the positive
// controls for the suppression direction. Each collider is placed in the SAME
// file so the conjunct is out of the way, and each is asserted to suppress ONLY
// the tier it belongs to. Without this pair, TestSwiftFieldTypeRefs_...Scoped
// above is also satisfied by collision scans that never suppress anything.
func TestSwiftFieldTypeRefs_TheTwoCollisionScansAreIndependent(t *testing.T) {
	const a = "Sources/App/Models.swift"

	// A component-family collider suppresses the Component and leaves the alias.
	comp := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: a},
		{Name: "Customer", Kind: "SCOPE.View", Subtype: "class", SourceFile: a},
		{Name: "Tier", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: a},
	}
	got := swiftInFileTypeTargets(comp, a)
	if _, ok := got["Customer"]; ok {
		t.Error("a same-file SCOPE.View did not suppress the SCOPE.Component target — " +
			"the component-family collision rule is not firing")
	}
	if _, ok := got["Tier"]; !ok {
		t.Error("the alias was suppressed by a collider that does not touch it")
	}

	// A NON-family collider suppresses the alias and leaves the Component. This
	// is the whole reason the two scans are separate: SCOPE.Enum and
	// SCOPE.Operation are invisible to the component family, and an alias is
	// invisible to it too, so an alias must be counted against every kind.
	alias := []types.EntityRecord{
		{Name: "Status", Kind: "SCOPE.Component", Subtype: "enum", SourceFile: a},
		{Name: "Status", Kind: "SCOPE.Enum", Subtype: "enum", SourceFile: a},
		{Name: "Tier", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: a},
		{Name: "Tier", Kind: "SCOPE.Operation", Subtype: "function", SourceFile: a},
	}
	got = swiftInFileTypeTargets(alias, a)
	if _, ok := got["Status"]; !ok {
		t.Error("a SCOPE.Enum value-set suppressed its own SCOPE.Component — that is " +
			"arm D's all-kinds rule leaking into the component tier, and it would " +
			"refuse EVERY Swift enum target")
	}
	if _, ok := got["Tier"]; ok {
		t.Error("a same-file SCOPE.Operation did not suppress the alias target — that " +
			"is arm F's component-scoped rule leaking into the alias tier, and the " +
			"edge it admits dangles at the resolver")
	}
}

// TestSwiftFieldTypeRefs_TwoRecordsOneKindAreOneNodeUnit — the unit of the count is
// the KIND, not the record, because graph.EntityID hashes
// (repo, Kind, Name, SourceFile) with Subtype EXCLUDED and buildSymbolIndex
// marks a collision only on `existing != e.ID` (internal/resolve/refs.go:1308).
// Two same-file records sharing a Kind are ONE graph node and must not suppress
// each other. Arm C counts records and is wrong for exactly this reason
// (#7038); its rule is not copied here, and this row is what would fail if it
// were.
func TestSwiftFieldTypeRefs_TwoRecordsOneKindAreOneNodeUnit(t *testing.T) {
	const a = "Sources/App/Models.swift"
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: a},
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: a},
	}
	if _, ok := swiftInFileTypeTargets(records, a)["Customer"]; !ok {
		t.Error("two SCOPE.Component records with one name were counted as two nodes — " +
			"they share an entity ID, so the resolver sees one and this refusal " +
			"deletes a legitimate edge")
	}

	// The SAME contract on the ALIAS branch, which counts a different map. The
	// component branch's version above is reachable from Swift source
	// (TestSwiftFieldTypeRefs_ExtensionBesideItsTypeIsStillOneNode); this one is
	// not — the only Swift that produces two SCOPE.Schema records under one name
	// in one file is a duplicate `typealias`, which swiftc rejects. It is graded
	// anyway because the two maps are two pieces of code and "the component one
	// is right" says nothing about the alias one: a record-counting alias scan
	// survives every other test in this package.
	alias := []types.EntityRecord{
		{Name: "Tier", Kind: "SCOPE.Schema", Subtype: "type_alias", SourceFile: a},
		{Name: "Tier", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: a},
	}
	if _, ok := swiftInFileTypeTargets(alias, a)["Tier"]; !ok {
		t.Error("two SCOPE.Schema records with one name were counted as two nodes on " +
			"the alias branch — EntityID excludes Subtype, so the resolver sees one")
	}
}

// TestSwiftFieldTypeRefs_OnlyFieldRecordsCarryTheEdge — the attach pass anchors
// on SCOPE.Schema/field records and on nothing else. That is the whole point of
// the arm: #6726 counts the FIELD as the orphan, and an edge hung off the owner
// (arm B found protobuf already doing exactly that) leaves the orphan rate
// where it was.
//
// Not reachable from Swift source today — only emitSwiftFieldMembers writes the
// stash, and only onto field records — so it is stated as a function-level
// contract, which is also what makes the guard graded rather than argued.
func TestSwiftFieldTypeRefs_OnlyFieldRecordsCarryTheEdge(t *testing.T) {
	const a = "Sources/App/Models.swift"
	stash := func() map[string]interface{} {
		return map[string]interface{}{swiftFieldTypeRefsMetaKey: []string{"Customer"}}
	}
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: a},
		{Name: "Impostor", Kind: "SCOPE.Component", Subtype: "class", SourceFile: a, Metadata: stash()},
		{Name: "Order.buyer", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: a, Metadata: stash()},
	}
	out := attachSwiftFieldTypeRefs(records, a)
	count := func(name string) int {
		n := 0
		for i := range out {
			if out[i].Name != name {
				continue
			}
			for _, r := range out[i].Relationships {
				if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == swiftFieldTargetRefKind {
					n++
				}
			}
		}
		return n
	}
	if got := count("Order.buyer"); got != 1 {
		t.Fatalf("the field record got %d field-type edges, want 1 — the positive "+
			"control is broken and the assertion below is vacuous", got)
	}
	if got := count("Impostor"); got != 0 {
		t.Errorf("a SCOPE.Component carrying the stash got %d field-type edges; this "+
			"pass must anchor on the field record only", got)
	}
}

// TestSwiftFieldTypeRefs_AllowListRefusesNonTypeComponents grades the SUBTYPE
// allow-list rather than arguing it is belt-and-braces — the mistake arm D
// recorded and then corrected.
//
// The honest status of each row, because they are not equal:
//
//   - `module` (the import carrier) — this is Go's import-placeholder case, the
//     one record whose bare name genuinely reaches the target set there. In
//     Swift it is ALSO blocked upstream: buildImport namespaces the carrier as
//     `<file>::import::<module>` (#492), so today's extractor cannot hand this
//     function a bare-named module record. The row is written with a BARE name
//     anyway, because that is the input the allow-list exists for and because
//     the upstream namespacing is a separate decision that could be revisited
//     (TestSwiftFieldTypeRefs_ImportCarrierNameCannotCollide watches it).
//   - `file` (#577) — its Name is the file path, so it is additionally
//     unreachable by name shape. Claimed as nothing more than a refusal.
//   - `swiftpm_target` (package.go) — emitted by the SEPARATE swift_package
//     extractor, so it is never in this extractor's record slice. Refused here
//     as well, so a future merge of the two record streams cannot silently make
//     a SwiftPM target name a field-type target.
//
// A positive control shares the fixture so a passing run cannot mean the
// function returns nothing.
func TestSwiftFieldTypeRefs_AllowListRefusesNonTypeComponents(t *testing.T) {
	const a = "Sources/App/Models.swift"
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "struct", SourceFile: a},
		{Name: "Logging", Kind: "SCOPE.Component", Subtype: "module", SourceFile: a},
		{Name: "AppTarget", Kind: "SCOPE.Component", Subtype: "swiftpm_target", SourceFile: a},
		{Name: a, Kind: "SCOPE.Component", Subtype: "file", SourceFile: a},
	}
	got := swiftInFileTypeTargets(records, a)
	if _, ok := got["Customer"]; !ok {
		t.Fatalf("the positive control is missing from %v — every assertion below "+
			"would pass on a function that returns nothing", swKeysOf(got))
	}
	for _, n := range []string{"Logging", "AppTarget", a} {
		if _, ok := got[n]; ok {
			t.Errorf("%q was admitted as a field-type target; the subtype allow-list "+
				"is not filtering", n)
		}
	}
}

func swKeysOf(m map[string]swiftFieldTypeTarget) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
