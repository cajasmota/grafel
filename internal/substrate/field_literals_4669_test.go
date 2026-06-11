package substrate

import (
	"reflect"
	"sort"
	"testing"
)

// field_literals_4669_test.go — #4669 field-level partial-stub analyzer.
//
// Validates the literal-vs-derived classification + the PartialStubFields
// roll-up + the envelope heuristic, across the two flagship languages
// (Python/DRF, JS/TS/NestJS), with the issue's concrete cases:
//   - A: {count: qs.count(), tbd: 0, all: 5} → flags tbd, all NOT count.
//   - B: {part_id: null, name: item.name}    → flags part_id.
//   - negative: {success: true, data: <derived>} → does NOT flag data;
//     success is recorded but excluded as an envelope flag.

func flaggedFieldNames(t *testing.T, facets []FieldFacet) []string {
	t.Helper()
	flagged := PartialStubFields(facets)
	names := make([]string, 0, len(flagged))
	for _, f := range flagged {
		names = append(names, f.Field)
	}
	sort.Strings(names)
	return names
}

func TestFieldLiteralsPython_CaseA_countDerived(t *testing.T) {
	// DRF Response dict: count derived, tbd/all hardcoded.
	src := `def get_summary(self, request):
    qs = Thing.objects.filter(active=True)
    return Response({
        "count": qs.count(),
        "tbd": 0,
        "all": 5,
    }, status=200)
`
	facets := analyzeFieldLiteralsPython(src, 1)
	got := flaggedFieldNames(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsPython_CaseB_partIdNull(t *testing.T) {
	src := `def get_item(self, request):
    item = self.get_object()
    return Response({"part_id": None, "name": item.name})
`
	facets := analyzeFieldLiteralsPython(src, 1)
	got := flaggedFieldNames(t, facets)
	want := []string{"part_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Case B: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
	// literal_value must be the verbatim None.
	for _, f := range PartialStubFields(facets) {
		if f.Field == "part_id" && f.LiteralValue != "None" {
			t.Fatalf("Case B: part_id literal_value=%q want None", f.LiteralValue)
		}
	}
}

func TestFieldLiteralsPython_Negative_envelope(t *testing.T) {
	src := `def list_things(self, request):
    qs = Thing.objects.all()
    return Response({"success": True, "data": ThingSerializer(qs, many=True).data})
`
	facets := analyzeFieldLiteralsPython(src, 1)
	got := flaggedFieldNames(t, facets)
	if len(got) != 0 {
		t.Fatalf("negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
	// success must be RECORDED as envelope (not flagged); data must be derived.
	var sawSuccessEnvelope, sawDataDerived bool
	for _, f := range facets {
		if f.Field == "success" {
			if f.Binding != BindingLiteral || !f.Envelope {
				t.Fatalf("success: binding=%v envelope=%v want literal+envelope", f.Binding, f.Envelope)
			}
			sawSuccessEnvelope = true
		}
		if f.Field == "data" {
			if f.Binding != BindingDerived {
				t.Fatalf("data: binding=%v want derived", f.Binding)
			}
			sawDataDerived = true
		}
	}
	if !sawSuccessEnvelope || !sawDataDerived {
		t.Fatalf("negative: success-envelope=%v data-derived=%v", sawSuccessEnvelope, sawDataDerived)
	}
}

func TestFieldLiteralsPython_conditionalFieldNotFlagged(t *testing.T) {
	// part_id literal in one return, derived in another → NOT unconditionally
	// literal → must NOT be flagged.
	src := `def maybe(self, request):
    if request.GET.get("x"):
        return Response({"part_id": None})
    return Response({"part_id": item.part_id})
`
	facets := analyzeFieldLiteralsPython(src, 1)
	got := flaggedFieldNames(t, facets)
	if len(got) != 0 {
		t.Fatalf("conditional: expected no flags (part_id derived in one branch), got %v", got)
	}
}

func TestFieldLiteralsJSTS_CaseA_and_shorthand(t *testing.T) {
	// NestJS response object: count derived (call), tbd/all hardcoded,
	// shorthand name derived.
	src := `getSummary() {
  const qs = this.repo.find();
  return {
    count: qs.length,
    tbd: 0,
    all: 5,
    name,
  };
}`
	facets := analyzeFieldLiteralsJSTS(src, 1)
	got := flaggedFieldNames(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSTS Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsJSTS_CaseB_partIdNull(t *testing.T) {
	src := `getItem() {
  const item = this.svc.get();
  return { part_id: null, name: item.name };
}`
	facets := analyzeFieldLiteralsJSTS(src, 1)
	got := flaggedFieldNames(t, facets)
	want := []string{"part_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSTS Case B: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsJSTS_Negative_envelope(t *testing.T) {
	src := `list() {
  const data = this.svc.all();
  return { success: true, data };
}`
	facets := analyzeFieldLiteralsJSTS(src, 1)
	got := flaggedFieldNames(t, facets)
	if len(got) != 0 {
		t.Fatalf("JSTS negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
}

func TestFieldLiteralsJSTS_templateInterpolationDerived(t *testing.T) {
	src := "build() {\n  return { label: `id-${item.id}`, tag: `static` };\n}"
	facets := analyzeFieldLiteralsJSTS(src, 1)
	var label, tag *FieldFacet
	for i := range facets {
		switch facets[i].Field {
		case "label":
			label = &facets[i]
		case "tag":
			tag = &facets[i]
		}
	}
	if label == nil || label.Binding != BindingDerived {
		t.Fatalf("label (interpolated template) should be derived: %+v", label)
	}
	if tag == nil || tag.Binding != BindingLiteral {
		t.Fatalf("tag (static template) should be literal: %+v", tag)
	}
}

func TestFieldLiteralRegistry_flagshipsRegistered(t *testing.T) {
	for _, lang := range []string{"python", "jsts"} {
		if FieldLiteralAnalyzerFor(lang) == nil {
			t.Fatalf("expected field-literal analyzer registered for %q", lang)
		}
	}
}
