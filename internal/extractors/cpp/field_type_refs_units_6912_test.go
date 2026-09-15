package cpp

// #6912 arm I — direct-call grading for the rules the fixture suite cannot
// separate.
//
// Three shapes live here rather than in the black-box file:
//
//   - Guards that are MUTUALLY MASKED through Extract. Two guards that only ever
//     fire together grade neither, and the fixture suite cannot construct the
//     distinguishing input because no cpp producer emits it. Each such guard is
//     driven here with a record set that isolates it.
//   - The SAME-FILE conjuncts, which need a second file's records in the slice
//     and must fail in OPPOSITE directions.
//   - The ambiguity rule, which is unreachable from cpp source today (section 4
//     of field_type_refs.go) and is kept as a mirror of the resolver.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// comp builds a same-file type DEFINITION record (the admitted shape).
func comp(name, file, subtype string) types.EntityRecord {
	return types.EntityRecord{
		Name: name, Kind: "SCOPE.Component", Subtype: subtype, SourceFile: file,
		Metadata: map[string]interface{}{"subtype": subtype, "definition": true},
	}
}

// fieldRec builds a SCOPE.Schema/field record with a declared type.
func fieldRec(owner, name, file, typ string) types.EntityRecord {
	return types.EntityRecord{
		Name: owner + "." + name, Kind: "SCOPE.Schema", Subtype: "field", SourceFile: file,
		Properties: map[string]string{
			"field_name": name, "field_type": typ, "parent_class": owner,
		},
	}
}

// targetsOf returns the target_type values of the field-type edges on a record.
func targetsOf(recs []types.EntityRecord, name string) []string {
	var out []string
	for i := range recs {
		if recs[i].Name != name {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Properties.Get("ref_kind") == cppFieldTargetRefKind {
				out = append(out, r.Properties.Get("target_type"))
			}
		}
	}
	return out
}

func wantTargets(t *testing.T, got, want []string, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

// TestCppFieldTypeRefs_Unit_OnlyComponentsAreTargets isolates the KIND test from
// the subtype allow-list.
//
// Through Extract the two are mutually masked: no cpp producer mints a record
// with Subtype ∈ {class, struct, union} under any Kind but SCOPE.Component, so
// the allow-list alone carries every refusal the fixture suite observes and a
// deletion of the Kind test would read ALIVE. The distinguishing input is
// {Kind: "SCOPE.Model", Subtype: "struct"} — the shape a FUTURE producer
// (a detector re-kinding a C++ class into a domain model) would mint, and the
// same shape the ambiguity rule below is kept for.
func TestCppFieldTypeRefs_Unit_OnlyComponentsAreTargets(t *testing.T) {
	const f = "a.cpp"
	for _, kind := range []string{
		"SCOPE.Model", "SCOPE.View", "SCOPE.Schema", "SCOPE.Operation",
		"SCOPE.Enum", "SCOPE.Pattern", "Class",
	} {
		recs := []types.EntityRecord{
			{Name: "Order", Kind: kind, Subtype: "struct", SourceFile: f,
				Metadata: map[string]interface{}{"definition": true}},
			fieldRec("Holder", "o", f, "Order"),
		}
		got := attachCppFieldTypeRefs(recs, f, "cpp")
		wantTargets(t, targetsOf(got, "Holder.o"), nil,
			"a field whose only same-file candidate is kinded "+kind)
	}
	// Positive control: the identical record set with the admitted Kind DOES
	// produce the edge, so the loop above cannot pass by the pass being broken.
	recs := []types.EntityRecord{comp("Order", "a.cpp", "struct"), fieldRec("Holder", "o", "a.cpp", "Order")}
	got := attachCppFieldTypeRefs(recs, "a.cpp", "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"}, "the SCOPE.Component control")
}

// TestCppFieldTypeRefs_Unit_OnlyTypeSubtypesAreTargets isolates the SUBTYPE
// allow-list from the definition marker.
//
// Through Extract those two are ALSO mutually masked in one direction: an
// `#include` / `using` placeholder and a namespace carry a refused subtype AND
// no definition marker, so deleting the allow-list would still see them refused
// by the marker. The distinguishing input is a refused subtype that DOES carry
// the marker.
//
// The other direction is not masked and is graded through Extract instead: a
// forward declaration carries an ADMITTED subtype and no marker
// (TestCppFieldTypeRefs_ForwardDeclarationIsNeverATarget).
func TestCppFieldTypeRefs_Unit_OnlyTypeSubtypesAreTargets(t *testing.T) {
	const f = "a.cpp"
	for _, sub := range []string{"import", "namespace", "file", ""} {
		recs := []types.EntityRecord{
			comp("Order", f, sub),               // carries definition:true, so only the
			fieldRec("Holder", "o", f, "Order"), // allow-list can refuse it
		}
		got := attachCppFieldTypeRefs(recs, f, "cpp")
		wantTargets(t, targetsOf(got, "Holder.o"), nil,
			"a field whose only same-file candidate has subtype "+sub)
	}
	for _, sub := range []string{"class", "struct", "union"} {
		recs := []types.EntityRecord{comp("Order", f, sub), fieldRec("Holder", "o", f, "Order")}
		got := attachCppFieldTypeRefs(recs, f, "cpp")
		wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"},
			"a field whose same-file candidate has subtype "+sub)
	}
}

// TestCppFieldTypeRefs_Unit_ADefinitionMarkerIsRequired isolates the #7047
// refusal from the allow-list, in the direction the fixture suite cannot reach:
// an ADMITTED subtype with no marker.
func TestCppFieldTypeRefs_Unit_ADefinitionMarkerIsRequired(t *testing.T) {
	const f = "a.cpp"
	for _, meta := range []map[string]interface{}{
		nil,
		{},
		{"subtype": "class"},
		{"definition": false},
	} {
		recs := []types.EntityRecord{
			{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: f, Metadata: meta},
			fieldRec("Holder", "o", f, "Order"),
		}
		got := attachCppFieldTypeRefs(recs, f, "cpp")
		wantTargets(t, targetsOf(got, "Holder.o"), nil,
			"a field whose only same-file candidate is not a definition")
	}
	recs := []types.EntityRecord{comp("Order", f, "class"), fieldRec("Holder", "o", f, "Order")}
	got := attachCppFieldTypeRefs(recs, f, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"}, "the definition control")
}

// TestCppFieldTypeRefs_Unit_FunctionShapedMarkerIsGradedOnBothValues closes an
// ASYMMETRY rather than a row.
//
// This pass reads three markers off a record — `definition`, `function_shaped`
// and `template_params`. The first was graded on both of its values from the
// start (..._Unit_ADefinitionMarkerIsRequired drives nil, empty, a foreign key
// and an explicit `false`), and `function_shaped` was graded only on `true`: a
// reader that treated ANY presence of the key as "refuse" — `_, ok :=
// r.Metadata["function_shaped"]` — would pass every test. Two markers of the
// same shape, one graded on both values and one on one.
//
// The distinguishing input is an explicit `false`, which is what a future
// producer stamping the key unconditionally would write.
func TestCppFieldTypeRefs_Unit_FunctionShapedMarkerIsGradedOnBothValues(t *testing.T) {
	const f = "a.cpp"
	build := func(meta map[string]interface{}) []types.EntityRecord {
		fld := fieldRec("Holder", "o", f, "Order")
		fld.Metadata = meta
		return []types.EntityRecord{comp("Order", f, "class"), fld}
	}
	// EMITS: the marker is absent, or present and false.
	for _, meta := range []map[string]interface{}{
		nil,
		{},
		{"subtype": "field", "owner": "Holder"},
		{"function_shaped": false},
		{"function_shaped": "true"}, // a non-bool value is not a refusal
	} {
		got := attachCppFieldTypeRefs(build(meta), f, "cpp")
		wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"},
			"a field whose function_shaped marker is absent or false")
	}
	// REFUSES: only an explicit true.
	got := attachCppFieldTypeRefs(build(map[string]interface{}{"function_shaped": true}), f, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), nil,
		"a field marked function_shaped")
}

// TestCppFieldTypeRefs_Unit_InFamilyRivalSuppressesTheTarget grades rule 3, which
// is VACUOUS AS SHIPPED: no cpp producer emits SCOPE.Model / SCOPE.View /
// SCOPE.Class / a bare Component, so len(nameKinds[name]) can never exceed 1
// from C or C++ source today. It is kept as a mirror of the resolver for the
// future producer the test above names, and is therefore graded by direct call
// rather than claimed to be covered by the fixture suite.
func TestCppFieldTypeRefs_Unit_InFamilyRivalSuppressesTheTarget(t *testing.T) {
	const f = "a.cpp"
	rivalInFamily := types.EntityRecord{
		Name: "Order", Kind: "SCOPE.Model", Subtype: "model", SourceFile: f,
	}
	recs := []types.EntityRecord{
		comp("Order", f, "class"), rivalInFamily, fieldRec("Holder", "o", f, "Order"),
	}
	got := attachCppFieldTypeRefs(recs, f, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), nil,
		"a field whose target name is carried by TWO component-family kinds")

	// The opposite direction, and the reason the count is scoped to the family:
	// an OUT-of-family rival never enters the tier that resolves this address,
	// so it must NOT suppress the edge. Arm D's all-kinds rule would drop this.
	rivalOutOfFamily := types.EntityRecord{
		Name: "Order", Kind: "SCOPE.Schema", Subtype: "enum", SourceFile: f,
	}
	recs = []types.EntityRecord{
		comp("Order", f, "class"), rivalOutOfFamily, fieldRec("Holder", "o", f, "Order"),
	}
	got = attachCppFieldTypeRefs(recs, f, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"},
		"a field whose target name is also carried by an OUT-of-family kind")

	// And the reason the unit is the KIND and not the RECORD: two records
	// sharing (Kind, Name, SourceFile) are ONE graph node — a forward
	// declaration beside its definition, a re-opened namespace, an elaborated
	// type specifier. Counting records would delete a legitimate edge.
	recs = []types.EntityRecord{
		comp("Order", f, "class"),
		{Name: "Order", Kind: "SCOPE.Component", Subtype: "class", SourceFile: f},
		fieldRec("Holder", "o", f, "Order"),
	}
	got = attachCppFieldTypeRefs(recs, f, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"},
		"a field whose target name is carried by two records of ONE kind")
}

// TestCppFieldTypeRefs_Unit_EveryAddressFamilyEntryIsGraded closes a hole an
// independent review found: the first revision's rival test sampled ONE kind
// (SCOPE.Model), so six of the eight cppComponentAddressFamily entries could be
// deleted with the whole suite still green — in the very table whose comment
// promises not to re-introduce arm F's missing-SCOPE.Class hole.
//
// Every entry is now driven individually: a rival kinded K must suppress the
// target, for each K in the table. Removing any single entry turns exactly this
// test red. The NEGATIVE half is enumerated too, because a test that only
// asserts suppression is satisfied by a table containing every kind there is.
func TestCppFieldTypeRefs_Unit_EveryAddressFamilyEntryIsGraded(t *testing.T) {
	const f = "a.cpp"
	suppresses := func(kind string) bool {
		recs := []types.EntityRecord{
			comp("Order", f, "class"),
			{Name: "Order", Kind: kind, Subtype: "whatever", SourceFile: f},
			fieldRec("Holder", "o", f, "Order"),
		}
		return len(targetsOf(attachCppFieldTypeRefs(recs, f, "cpp"), "Holder.o")) == 0
	}

	// The table's own entries, listed here as an INDEPENDENT literal rather than
	// ranged over from the map under test — ranging over it would make the test
	// agree with the table by construction and grade nothing.
	//
	// SCOPE.Component is graded DIFFERENTLY from the other seven and the reason
	// is structural, not a gap: the TARGET is itself a SCOPE.Component, so a
	// rival of that kind is the same (Kind, Name, SourceFile) — ONE graph node,
	// which is exactly what the kind-not-record rule says must not suppress. Its
	// entry is instead what makes the target's OWN kind countable, so every one
	// of the seven rows below needs it and all seven fail without it. That is
	// stated rather than left implicit, and it is verified by mutation.
	rivalGraded := []string{
		"Component", "Class", "View", "Model",
		"SCOPE.Class", "SCOPE.View", "SCOPE.Model",
	}
	if len(cppComponentAddressFamily) != len(rivalGraded)+1 {
		t.Fatalf("the address family has %d entries, this test grades %d + "+
			"SCOPE.Component — add the new kind here rather than leaving it "+
			"ungraded", len(cppComponentAddressFamily), len(rivalGraded))
	}
	if !cppComponentAddressFamily["SCOPE.Component"] {
		t.Error("SCOPE.Component is absent from cppComponentAddressFamily; the " +
			"target's own kind is then uncountable and NO rival can ever " +
			"suppress anything")
	}
	for _, k := range rivalGraded {
		if !cppComponentAddressFamily[k] {
			t.Errorf("%q is graded here but absent from cppComponentAddressFamily", k)
			continue
		}
		if !suppresses(k) {
			t.Errorf("a same-file rival kinded %q did NOT suppress the target; "+
				"that entry of the address family is not doing its job", k)
		}
	}
	// The kind-not-record rule, restated as the reason SCOPE.Component is not in
	// the loop: a rival of the TARGET'S OWN kind is one node and must not
	// suppress. (Also graded from cpp source by
	// TestCppFieldTypeRefs_ForwardDeclarationBesideItsDefinitionStillBinds.)
	if suppresses("SCOPE.Component") {
		t.Error("a same-KIND rival suppressed the target; the ambiguity rule is " +
			"counting records rather than kinds (#7038)")
	}

	// Kinds OUTSIDE the family must NOT suppress — this is what keeps the table
	// from degenerating into "every kind", which would be arm D's rule wearing
	// this table's name.
	for _, k := range []string{
		"SCOPE.Schema", "SCOPE.Operation", "SCOPE.Enum", "SCOPE.Pattern",
		"SCOPE.Config", "SCOPE.ExceptionType", "Schema", "Operation",
	} {
		if suppresses(k) {
			t.Errorf("a same-file rival kinded %q suppressed the target, but that "+
				"kind never enters the tier which resolves a component-space ref", k)
		}
	}
}

// TestCppFieldTypeRefs_Unit_TrimAliasEntriesAreWeighedByTheResolver proves the
// half of the table that is easiest to get wrong and hardest to see: the
// SCOPE-trimmed aliases. BuildIndex writes every entity under its raw Kind AND
// its SCOPE-trimmed alias, so a `SCOPE.Class`-kinded rival is keyed under
// "Class" — which IS in componentKindFamily even though "SCOPE.Class" is not.
// Arm F shipped this table with "SCOPE.Class" missing.
//
// Driven through the REAL resolver rather than argued: with the rival present
// the component-space ref must NOT resolve to the class, which is exactly why
// the pass refuses to emit it.
func TestCppFieldTypeRefs_Unit_TrimAliasEntriesAreWeighedByTheResolver(t *testing.T) {
	const f = "a.cpp"
	for _, kind := range []string{"SCOPE.Class", "Class"} {
		recs := []types.EntityRecord{
			comp("Order", f, "class"),
			{Name: "Order", Kind: kind, Subtype: "class", SourceFile: f},
		}
		for i := range recs {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
		// Premise: the two records really are two distinct graph nodes, or the
		// rival could not make anything ambiguous and the row is vacuous.
		if recs[0].ID == recs[1].ID {
			t.Fatalf("a %q rival collapses onto the class — this row is vacuous", kind)
		}
		probe := []types.EntityRecord{{
			Name: "probe", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: f,
			ID: "probe-id",
			Relationships: []types.RelationshipRecord{{
				ToID: extractor.BuildComponentStructuralRef("cpp", f, "Order"),
				Kind: "REFERENCES",
			}},
		}}
		all := append(append([]types.EntityRecord{}, recs...), probe...)
		idx := resolve.BuildIndex(all)
		resolve.ReferencesEmbedded(all, idx)
		got := all[len(all)-1].Relationships[0].ToID
		if got == recs[0].ID {
			t.Errorf("with a %q rival present the component-space ref still bound "+
				"to the class; the suppression this table encodes is unnecessary "+
				"for that kind and the comment overclaims", kind)
		}
	}
}

// TestCppFieldTypeRefs_Unit_TargetsAreScopedToTheRequestedFile grades the
// SAME-FILE promise, which is the constraint every arm rests on and none may
// break (#6976 / #6369). There are THREE independent same-file conjuncts and
// grading one is exactly what makes its siblings look covered, so each is driven
// with its own record set and they fail in OPPOSITE directions.
func TestCppFieldTypeRefs_Unit_TargetsAreScopedToTheRequestedFile(t *testing.T) {
	const this, other = "this.cpp", "other.cpp"

	// (b) TARGET scan — a record in ANOTHER file must never BECOME a target.
	recs := []types.EntityRecord{
		comp("Order", other, "class"),
		fieldRec("Holder", "o", this, "Order"),
	}
	got := attachCppFieldTypeRefs(recs, this, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), nil,
		"a field whose type is declared in ANOTHER file")

	// (a) RIVAL scan — an in-family rival in ANOTHER file must never SUPPRESS a
	// perfectly good same-file target. This is the opposite failure direction:
	// dropping the file test in pass 1 makes the edge DISAPPEAR rather than
	// appear, so the assertion above could not see it.
	recs = []types.EntityRecord{
		comp("Order", this, "class"),
		{Name: "Order", Kind: "SCOPE.Model", Subtype: "model", SourceFile: other},
		fieldRec("Holder", "o", this, "Order"),
	}
	got = attachCppFieldTypeRefs(recs, this, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"},
		"a field whose target name has an in-family rival in ANOTHER file")

	// (c) FIELD scan — a field record belonging to another file must not be
	// walked at all, even when this file declares the type it names.
	recs = []types.EntityRecord{
		comp("Order", this, "class"),
		fieldRec("Holder", "o", other, "Order"),
	}
	got = attachCppFieldTypeRefs(recs, this, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), nil,
		"a field belonging to ANOTHER file")

	// The ToID carries THIS file, not the field's or the rival's — a same-file
	// rule that emitted a ref naming a different file would bind elsewhere.
	recs = []types.EntityRecord{comp("Order", this, "class"), fieldRec("Holder", "o", this, "Order")}
	got = attachCppFieldTypeRefs(recs, this, "cpp")
	want := extractor.BuildComponentStructuralRef("cpp", this, "Order")
	n := 0
	for i := range got {
		for _, r := range got[i].Relationships {
			if r.Properties.Get("ref_kind") == cppFieldTargetRefKind {
				n++
				if r.ToID != want {
					t.Fatalf("ToID = %q, want %q", r.ToID, want)
				}
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d field-type edges, want 1 — the ToID assertion is vacuous", n)
	}
}

// TestCppFieldTypeRefs_CandidateScannerEnumeratesTheShapeSpace ENUMERATES the
// type-expression shape space rather than sampling it. Arm E's strongest test is
// this one, and its reason holds here: three rounds of hand-picked attacks on a
// sibling arm missed two holes, whereas an enumeration makes an unlisted shape a
// visible gap.
//
// It grades the SCANNER in isolation, which the fixture suite cannot: a bare
// identifier the file does not declare produces no edge either way, so through
// Extract a scanner that returned junk tokens and one that returned none are
// indistinguishable on every row but the few that bind.
func TestCppFieldTypeRefs_CandidateScannerEnumeratesTheShapeSpace(t *testing.T) {
	cases := []struct {
		typ  string
		want []string
	}{
		// --- bare and composed ---
		{"Order", []string{"Order"}},
		{"unsigned int", []string{"unsigned", "int"}},
		{"const int", []string{"const", "int"}},
		{"struct Item", []string{"struct", "Item"}},
		{"enum Color", []string{"enum", "Color"}},
		{"unsigned long long", []string{"unsigned", "long", "long"}},
		// --- template arguments: every argument is its own candidate ---
		{"std::vector<Order>", []string{"Order"}},
		{"std::map<Item, Order>", []string{"Item", "Order"}},
		{"std::pair<Order, Item>", []string{"Order", "Item"}},
		{"std::unique_ptr<Order>", []string{"Order"}},
		{"Box<Order>", []string{"Box", "Order"}},
		{"Box<Box<Order>>", []string{"Box", "Box", "Order"}},
		{"std::array<Order, 4>", []string{"Order"}},
		{"std::map<std::string, Order>", []string{"Order"}},
		// --- qualified names are consumed WHOLE and discarded ---
		{"Ns::Order", nil},
		{"A::B::Order", nil},
		{"::Order", nil},
		{"::Ns::Order", nil},
		{"Ns::Box<Order>", []string{"Order"}},
		{"Ns::Order*", nil},
		// --- declarator decoration, which the grammar mostly keeps out of the
		//     type field but which a hand-written property could carry ---
		{"Order*", []string{"Order"}},
		{"Order&", []string{"Order"}},
		{"Order&&", []string{"Order"}},
		{"const Order&", []string{"const", "Order"}},
		{"Order[4]", []string{"Order"}},
		{"Order *const", []string{"Order", "const"}},
		// --- brace: an inline definition is refused WHOLE, member names and all
		{"struct { int inner; }", nil},
		{"struct { Order inner; }", nil},
		{"union { int a; }", nil},
		// --- degenerate ---
		{"", nil},
		{"  ", nil},
		{"::", nil},
		{"<>", nil},
		{"123", nil},
		{"_Order", []string{"_Order"}},
		{"Order2", []string{"Order2"}},
	}
	for _, c := range cases {
		got := cppFieldTypeCandidates(c.typ)
		if len(got) != len(c.want) {
			t.Errorf("cppFieldTypeCandidates(%q) = %v, want %v", c.typ, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("cppFieldTypeCandidates(%q) = %v, want %v", c.typ, got, c.want)
				break
			}
		}
	}
}

// TestCppFieldTypeRefs_Unit_OneEdgePerTargetPerField — a type expression naming
// the same type twice (`std::pair<Order, Order>`) must produce ONE edge, not
// two. Without the dedup the graph would carry duplicate parallel edges that no
// consumer distinguishes.
func TestCppFieldTypeRefs_Unit_OneEdgePerTargetPerField(t *testing.T) {
	const f = "a.cpp"
	recs := []types.EntityRecord{
		comp("Order", f, "class"),
		fieldRec("Holder", "o", f, "std::pair<Order, Order>"),
	}
	got := attachCppFieldTypeRefs(recs, f, "cpp")
	wantTargets(t, targetsOf(got, "Holder.o"), []string{"Order"},
		"a type expression naming one target twice")
}

// TestCppFieldTypeRefs_Unit_ExistingRelationshipsAreNotClobbered — the field
// record carries no relationships today, but Relationships must be APPENDED to
// so a later producer's edge is not silently destroyed.
func TestCppFieldTypeRefs_Unit_ExistingRelationshipsAreNotClobbered(t *testing.T) {
	const f = "a.cpp"
	fld := fieldRec("Holder", "o", f, "Order")
	fld.Relationships = []types.RelationshipRecord{{ToID: "pre-existing", Kind: "USES"}}
	recs := []types.EntityRecord{comp("Order", f, "class"), fld}
	got := attachCppFieldTypeRefs(recs, f, "cpp")
	for i := range got {
		if got[i].Name != "Holder.o" {
			continue
		}
		if len(got[i].Relationships) != 2 || got[i].Relationships[0].ToID != "pre-existing" {
			t.Fatalf("relationships = %+v, want the pre-existing edge kept and the "+
				"field-type edge appended", got[i].Relationships)
		}
		return
	}
	t.Fatal("no field record survived — this test is vacuous")
}
