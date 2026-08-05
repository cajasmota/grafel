package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// cfRows renders a record set as sorted "kind|name|subtype|file" content
// tuples. Assertions below compare these sets WHOLE, in both directions: a
// "the typed row is present" check passes while the generic row also survives
// (two nodes for one class), and a "the generic row is gone" check passes on an
// empty set.
func cfRows(recs []types.EntityRecord) []string {
	out := make([]string, 0, len(recs))
	for i := range recs {
		r := &recs[i]
		out = append(out, r.Kind+"|"+r.Name+"|"+r.Subtype+"|"+r.SourceFile)
	}
	sort.Strings(out)
	return out
}

func cfAssertRows(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("record rows:\n  want %v\n  got  %v", want, got)
	}
}

func cfSource(name, file string) types.EntityRecord {
	return types.EntityRecord{
		Kind: "SCOPE.Component", Subtype: "class", Name: name, SourceFile: file,
		QualifiedName: "m." + name, Language: "python", StartLine: 3, EndLine: 9,
		Properties: map[string]string{"role": "class", "provenance": "INFERRED_FROM_CLASS_HIERARCHY"},
	}
}

func cfFramework(kind, name, file string) types.EntityRecord {
	return types.EntityRecord{
		Kind: kind, Name: name, SourceFile: file,
		QualifiedName: "m." + name, Language: "python", StartLine: 3,
		Properties: map[string]string{"framework": "python", "pattern_type": "yaml_driven"},
	}
}

// TestFoldFrameworkClassKinds_TypedSurvivorReplacesGenericNode is the #6148
// core: the generic class record becomes the framework-typed record.
func TestFoldFrameworkClassKinds_TypedSurvivorReplacesGenericNode(t *testing.T) {
	recs := []types.EntityRecord{
		cfSource("Probe", "a.py"),
		{Kind: "SCOPE.Operation", Subtype: "method", Name: "Probe.m", SourceFile: "a.py"},
	}
	fw := []types.EntityRecord{cfFramework("Controller", "Probe", "a.py")}

	if n := FoldFrameworkClassKinds(recs, fw); n != 1 {
		t.Errorf("folded %d records, want 1", n)
	}
	cfAssertRows(t, cfRows(recs), []string{
		"Controller|Probe||a.py",
		"SCOPE.Operation|Probe.m|method|a.py",
	})

	// The survivor is the framework record — including its EMPTY subtype, which
	// the content-keyed parity comparator compares — carrying the properties the
	// source had and it lacked, minus the source-describing ones.
	sv := recs[0]
	if sv.Properties["framework"] != "python" || sv.Properties["pattern_type"] != "yaml_driven" {
		t.Errorf("survivor lost its own properties: %v", sv.Properties)
	}
	if got := sv.Properties["role"]; got != "class" {
		t.Errorf("survivor should inherit the source property it lacks (role): %v", sv.Properties)
	}
	if _, ok := sv.Properties["provenance"]; ok {
		t.Errorf("provenance describes the folded-away source and must not travel: %v", sv.Properties)
	}
}

// TestFoldFrameworkClassKinds_NoCandidateLeavesGenericNode pins the other half:
// a class no rule matches must stay generic. Without this, a fix that typed
// every class would pass the test above and corrupt every unmatched class.
func TestFoldFrameworkClassKinds_NoCandidateLeavesGenericNode(t *testing.T) {
	recs := []types.EntityRecord{cfSource("Probe", "a.py")}
	before := cfRows(recs)

	// Framework records that must NOT match: right name wrong file, right file
	// wrong name, and a kind that is not class-declaration-strength at all.
	fw := []types.EntityRecord{
		cfFramework("Controller", "Probe", "other.py"),
		cfFramework("Controller", "Other", "a.py"),
		cfFramework("Route", "Probe", "a.py"),
	}
	if n := FoldFrameworkClassKinds(recs, fw); n != 0 {
		t.Errorf("folded %d records, want 0", n)
	}
	cfAssertRows(t, cfRows(recs), before)
}

// TestFoldFrameworkClassKinds_NonClassRecordsUntouched: an operation, a file
// component and an import placeholder are not class representations and must
// survive verbatim even when a framework record shares their name.
func TestFoldFrameworkClassKinds_NonClassRecordsUntouched(t *testing.T) {
	recs := []types.EntityRecord{
		{Kind: "SCOPE.Operation", Name: "Probe", SourceFile: "a.py"},
		{Kind: "SCOPE.Component", Subtype: "file", Name: "Probe", SourceFile: "a.py"},
		{Kind: "SCOPE.Component", Subtype: "import", Name: "Probe", SourceFile: "a.py"},
	}
	before := cfRows(recs)
	if n := FoldFrameworkClassKinds(recs, []types.EntityRecord{cfFramework("Controller", "Probe", "a.py")}); n != 0 {
		t.Errorf("folded %d records, want 0", n)
	}
	cfAssertRows(t, cfRows(recs), before)
}

// TestFoldFrameworkClassKinds_CanonRankBreaksEqualPriority reproduces the
// #3172/#3195 double-emission: one class surfacing as BOTH Model and
// Controller, equal priority. The structural-declaration kind must win, and
// exactly ONE node must remain for the class.
func TestFoldFrameworkClassKinds_CanonRankBreaksEqualPriority(t *testing.T) {
	for _, order := range [][]types.EntityRecord{
		{cfFramework("Controller", "Probe", "a.py"), cfFramework("Model", "Probe", "a.py")},
		{cfFramework("Model", "Probe", "a.py"), cfFramework("Controller", "Probe", "a.py")},
	} {
		recs := []types.EntityRecord{cfSource("Probe", "a.py")}
		FoldFrameworkClassKinds(recs, order)
		cfAssertRows(t, cfRows(recs), []string{"Model|Probe||a.py"})
	}
}

// TestFoldFrameworkClassKinds_PriorityBeatsRank guards the ordering of the two
// tiebreakers: priority is primary, canonical rank only breaks an exact tie.
// "TestClass" (priority 90, rank 0) must lose to "Controller" (priority 100,
// rank 0) — and "Task" (priority 70) to "Schema" (priority 80, rank 3).
func TestFoldFrameworkClassKinds_PriorityBeatsRank(t *testing.T) {
	recs := []types.EntityRecord{cfSource("Probe", "a.py")}
	FoldFrameworkClassKinds(recs, []types.EntityRecord{
		cfFramework("TestClass", "Probe", "a.py"),
		cfFramework("Controller", "Probe", "a.py"),
	})
	cfAssertRows(t, cfRows(recs), []string{"Controller|Probe||a.py"})

	recs = []types.EntityRecord{cfSource("Probe", "a.py")}
	FoldFrameworkClassKinds(recs, []types.EntityRecord{
		cfFramework("Task", "Probe", "a.py"),
		cfFramework("Schema", "Probe", "a.py"),
	})
	cfAssertRows(t, cfRows(recs), []string{"Schema|Probe||a.py"})
}

// TestFoldFrameworkClassKinds_EdgesTheSourceOwnsMoveToSurvivor: the fold must
// never drop an edge. The source's owned edges keep an empty FromID, which the
// record→graph seam resolves to the OWNING record's id — so carrying them onto
// the survivor is what re-homes them onto the typed node.
func TestFoldFrameworkClassKinds_EdgesTheSourceOwnsMoveToSurvivor(t *testing.T) {
	src := cfSource("Probe", "a.py")
	src.Relationships = []types.RelationshipRecord{{ToID: "Probe.m", Kind: "CONTAINS"}}
	fwRec := cfFramework("Controller", "Probe", "a.py")
	fwRec.Relationships = []types.RelationshipRecord{{ToID: "Other", Kind: "MOUNTS"}}

	recs := []types.EntityRecord{src}
	FoldFrameworkClassKinds(recs, []types.EntityRecord{fwRec})

	var kinds []string
	for _, r := range recs[0].Relationships {
		if r.FromID != "" {
			t.Errorf("owned edge gained an explicit FromID %q — it would no longer bind to the survivor", r.FromID)
		}
		kinds = append(kinds, r.Kind+"→"+r.ToID)
	}
	sort.Strings(kinds)
	cfAssertRows(t, kinds, []string{"CONTAINS→Probe.m", "MOUNTS→Other"})
}

// TestFoldFrameworkClassKinds_NoInputsIsANoOp covers the guard clauses: no
// records, or no framework records, must not panic and must not fold.
func TestFoldFrameworkClassKinds_NoInputsIsANoOp(t *testing.T) {
	if n := FoldFrameworkClassKinds(nil, []types.EntityRecord{cfFramework("Controller", "Probe", "a.py")}); n != 0 {
		t.Errorf("folded %d with no records", n)
	}
	recs := []types.EntityRecord{cfSource("Probe", "a.py")}
	if n := FoldFrameworkClassKinds(recs, nil); n != 0 {
		t.Errorf("folded %d with no framework records", n)
	}
	cfAssertRows(t, cfRows(recs), []string{"SCOPE.Component|Probe|class|a.py"})
}
