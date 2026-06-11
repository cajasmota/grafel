package dashboard

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// marshalNormalized is a tiny helper mirroring what writeJSON/writeReportJSON
// do on the wire: normalize nil slices, then JSON-encode.
func marshalNormalized(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(normalizeNilSlices(v)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Reflection helper unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestNormalizeNilSlices_TopLevelNilSlice(t *testing.T) {
	type T struct {
		Items []string `json:"items"`
	}
	got := marshalNormalized(t, T{}) // Items is nil
	if strings.Contains(got, "null") {
		t.Fatalf("nil slice marshaled to null: %s", got)
	}
	if !strings.Contains(got, `"items":[]`) {
		t.Fatalf("expected items:[] got %s", got)
	}
}

func TestNormalizeNilSlices_PopulatedSliceUnchanged(t *testing.T) {
	type T struct {
		Items []string `json:"items"`
	}
	got := marshalNormalized(t, T{Items: []string{"a", "b"}})
	if !strings.Contains(got, `"items":["a","b"]`) {
		t.Fatalf("populated slice altered: %s", got)
	}
}

func TestNormalizeNilSlices_NestedStructsAndSlices(t *testing.T) {
	type Inner struct {
		Tags []string `json:"tags"`
	}
	type Outer struct {
		Rows  []Inner `json:"rows"`  // nil at top level
		One   Inner   `json:"one"`   // nested struct with nil slice
		Ptr   *Inner  `json:"ptr"`   // non-nil pointer with nil slice
		NilP  *Inner  `json:"nil_p"` // nil pointer — stays null
		Items []Inner `json:"items"` // populated; element has nil slice
	}
	v := Outer{
		Ptr:   &Inner{},
		Items: []Inner{{Tags: []string{"x"}}, {}},
	}
	got := marshalNormalized(t, v)

	// Top-level nil slice -> []
	if !strings.Contains(got, `"rows":[]`) {
		t.Errorf("rows not normalized: %s", got)
	}
	// Nested struct's nil slice -> []
	if !strings.Contains(got, `"one":{"tags":[]}`) {
		t.Errorf("nested struct slice not normalized: %s", got)
	}
	// Non-nil pointer's nil slice -> []
	if !strings.Contains(got, `"ptr":{"tags":[]}`) {
		t.Errorf("pointer-target slice not normalized: %s", got)
	}
	// Nil pointer stays null (semantically meaningful optional object).
	if !strings.Contains(got, `"nil_p":null`) {
		t.Errorf("nil pointer should stay null: %s", got)
	}
	// Populated element keeps data; empty element's slice -> [].
	if !strings.Contains(got, `"items":[{"tags":["x"]},{"tags":[]}]`) {
		t.Errorf("slice-of-struct normalization wrong: %s", got)
	}
}

func TestNormalizeNilSlices_MapsAndPointersUntouchedButValuesWalked(t *testing.T) {
	type Inner struct {
		Tags []string `json:"tags"`
	}
	type T struct {
		NilMap  map[string]int    `json:"nil_map"`   // stays null
		Counts  map[string]int    `json:"counts"`    // populated, untouched
		ByName  map[string]Inner  `json:"by_name"`   // values have nil slices -> []
		ByNameP map[string]*Inner `json:"by_name_p"` // pointer values walked
	}
	v := T{
		Counts:  map[string]int{"a": 1},
		ByName:  map[string]Inner{"k": {}},
		ByNameP: map[string]*Inner{"p": {}},
	}
	got := marshalNormalized(t, v)

	if !strings.Contains(got, `"nil_map":null`) {
		t.Errorf("nil map should stay null: %s", got)
	}
	if !strings.Contains(got, `"counts":{"a":1}`) {
		t.Errorf("populated map altered: %s", got)
	}
	// Map VALUE's nil slice should be normalized to [].
	if !strings.Contains(got, `"by_name":{"k":{"tags":[]}}`) {
		t.Errorf("map value slice not normalized: %s", got)
	}
	if !strings.Contains(got, `"by_name_p":{"p":{"tags":[]}}`) {
		t.Errorf("map pointer value slice not normalized: %s", got)
	}
}

func TestNormalizeNilSlices_DoesNotMutateInput(t *testing.T) {
	type T struct {
		Items []string `json:"items"`
	}
	in := T{}
	_ = normalizeNilSlices(in)
	if in.Items != nil {
		t.Fatalf("input was mutated: Items=%v", in.Items)
	}
}

func TestNormalizeNilSlices_NilInput(t *testing.T) {
	if got := normalizeNilSlices(nil); got != nil {
		t.Fatalf("nil input should return nil, got %v", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Representative dashboard report payloads — every array-typed field the
// frontend iterates must serialize as [] not null when empty.
// ─────────────────────────────────────────────────────────────────────────────

func TestDashboardReports_EmptySlicesSerializeAsArray(t *testing.T) {
	// Each payload is constructed with ALL slice fields left nil (the empty /
	// not-yet-indexed case that triggered the null.length crashes #4516).
	payloads := []struct {
		name    string
		payload any
		// jsonFields are the JSON keys whose value must be [] (never null).
		jsonFields []string
	}{
		{
			name:       "coverage",
			payload:    GroupCoverageReport{Group: "g"},
			jsonFields: []string{"uncovered_entities", "by_directory", "by_file", "by_module"},
		},
		{
			name:       "security_auth",
			payload:    GroupAuthCoverageReport{Group: "g"},
			jsonFields: []string{"findings"},
		},
		{
			name:       "security_secrets",
			payload:    GroupSecretsReport{Group: "g"},
			jsonFields: []string{"findings"},
		},
		{
			name:       "security_cycles",
			payload:    GroupCyclesReport{Group: "g"},
			jsonFields: []string{"findings"},
		},
		{
			name:       "nplus1",
			payload:    GroupNPlusOneReport{Group: "g"},
			jsonFields: []string{"findings"},
		},
	}

	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			got := marshalNormalized(t, p.payload)
			for _, f := range p.jsonFields {
				nullPat := `"` + f + `":null`
				arrPat := `"` + f + `":[]`
				if strings.Contains(got, nullPat) {
					t.Errorf("%s: field %q serialized as null (frontend crashes on .length): %s", p.name, f, got)
				}
				if !strings.Contains(got, arrPat) {
					t.Errorf("%s: field %q not emitted as []: %s", p.name, f, got)
				}
			}
		})
	}
}

// TestDashboardReports_NestedCycleEdges proves nested slice fields (CycleFinding
// has its own Members/Edges []) are normalized when a finding is present.
func TestDashboardReports_NestedCycleEdges(t *testing.T) {
	v := GroupCyclesReport{
		Group: "g",
		Findings: []CycleFinding{
			{Repo: "r"}, // Members and Edges are nil
		},
	}
	got := marshalNormalized(t, v)
	if strings.Contains(got, `"members":null`) || strings.Contains(got, `"edges":null`) {
		t.Fatalf("nested cycle slices serialized as null: %s", got)
	}
	if !strings.Contains(got, `"members":[]`) || !strings.Contains(got, `"edges":[]`) {
		t.Fatalf("nested cycle slices not emitted as []: %s", got)
	}
}
