package literalparity

import (
	"reflect"
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"PAGE_SLUG":       "page_slug",
		"page-slug":       "page_slug",
		"page.slug":       "page_slug",
		"  Page Slug  ":   "page_slug",
		"CORE_ADMIN":      "core_admin",
		"core-admin":      "core_admin",
		"a__b--c":         "a_b_c",
		"trailing_":       "trailing",
		"witnessing/comp": "witnessing_comp",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// equivalent: identical key+value sets (modulo key separator/case) → no diff.
func TestDiff_Equivalent(t *testing.T) {
	oracle := []Member{
		{Key: "DASHBOARD", Value: "dashboard"},
		{Key: "SETTINGS", Value: "settings"},
	}
	v3 := []Member{
		{Key: "dashboard", Value: "dashboard"},
		{Key: "settings", Value: "settings"},
	}
	res := Diff("page_slugs", oracle, v3)
	if res.Verdict != VerdictEquivalent {
		t.Fatalf("verdict = %q, want equivalent; result=%+v", res.Verdict, res)
	}
	if len(res.OnlyInOracle) != 0 || len(res.OnlyInV3) != 0 ||
		len(res.ValueMismatches) != 0 || len(res.IntraV3Inconsistencies) != 0 {
		t.Fatalf("expected clean equivalent, got %+v", res)
	}
}

// value_mismatch: same aligned key, different literal value (the _ vs - class).
func TestDiff_ValueMismatch(t *testing.T) {
	oracle := []Member{{Key: "ADMIN", Value: "core_admin"}}
	v3 := []Member{{Key: "ADMIN", Value: "core-admin"}}
	res := Diff("action_codenames", oracle, v3)
	if res.Verdict != VerdictDrift {
		t.Fatalf("verdict = %q, want drift", res.Verdict)
	}
	want := []ValueMismatch{{Key: "ADMIN", Oracle: "core_admin", V3: "core-admin"}}
	if !reflect.DeepEqual(res.ValueMismatches, want) {
		t.Fatalf("value_mismatches = %+v, want %+v", res.ValueMismatches, want)
	}
	if len(res.OnlyInOracle) != 0 || len(res.OnlyInV3) != 0 {
		t.Fatalf("unexpected membership diff: %+v", res)
	}
}

// only_in: keys present on only one side.
func TestDiff_OnlyIn(t *testing.T) {
	oracle := []Member{
		{Key: "KEEP", Value: "keep"},
		{Key: "DROPPED", Value: "dropped"},
	}
	v3 := []Member{
		{Key: "KEEP", Value: "keep"},
		{Key: "ADDED", Value: "added"},
	}
	res := Diff("status_strings", oracle, v3)
	if res.Verdict != VerdictDrift {
		t.Fatalf("verdict = %q, want drift", res.Verdict)
	}
	if !reflect.DeepEqual(res.OnlyInOracle, []string{"DROPPED"}) {
		t.Errorf("only_in_oracle = %v, want [DROPPED]", res.OnlyInOracle)
	}
	if !reflect.DeepEqual(res.OnlyInV3, []string{"ADDED"}) {
		t.Errorf("only_in_v3 = %v, want [ADDED]", res.OnlyInV3)
	}
}

// intra-v3 separator inconsistency: v3 mixes underscore + hyphen value
// conventions within one set.
func TestDiff_IntraV3Inconsistency(t *testing.T) {
	oracle := []Member{
		{Key: "EMAIL", Value: "email_templates"},
		{Key: "WITNESS", Value: "witnessing_companies"},
	}
	v3 := []Member{
		{Key: "EMAIL", Value: "email_templates"},      // snake
		{Key: "WITNESS", Value: "witnessing-companies"}, // kebab — the outlier
	}
	res := Diff("page_slugs", oracle, v3)
	if res.Verdict != VerdictDrift {
		t.Fatalf("verdict = %q, want drift", res.Verdict)
	}
	if len(res.IntraV3Inconsistencies) != 1 {
		t.Fatalf("expected 1 intra-v3 inconsistency, got %+v", res.IntraV3Inconsistencies)
	}
	ic := res.IntraV3Inconsistencies[0]
	if ic.Convention != "snake" {
		t.Errorf("dominant convention = %q, want snake", ic.Convention)
	}
	if !reflect.DeepEqual(ic.Outliers, []string{"WITNESS"}) {
		t.Errorf("outliers = %v, want [WITNESS]", ic.Outliers)
	}
	// This case ALSO trips a value_mismatch on the aligned WITNESS key.
	if len(res.ValueMismatches) != 1 || res.ValueMismatches[0].Key != "WITNESS" {
		t.Errorf("expected value_mismatch on WITNESS, got %+v", res.ValueMismatches)
	}
}

// A clean v3 set with one consistent convention does NOT trip intra-v3.
func TestDiff_IntraV3_ConsistentSnake(t *testing.T) {
	v3 := []Member{
		{Key: "A", Value: "alpha_one"},
		{Key: "B", Value: "beta_two"},
		{Key: "C", Value: "single"}, // no separator — convention-neutral
	}
	ic := detectIntraInconsistency(v3)
	if len(ic) != 0 {
		t.Fatalf("expected no intra inconsistency for consistent snake set, got %+v", ic)
	}
}

// A single value carrying BOTH separators is flagged "mixed".
func TestDiff_IntraV3_MixedSeparator(t *testing.T) {
	v3 := []Member{
		{Key: "A", Value: "weird_mixed-token"},
		{Key: "B", Value: "plain_snake"},
	}
	ic := detectIntraInconsistency(v3)
	if len(ic) != 1 {
		t.Fatalf("expected 1 inconsistency for mixed-separator value, got %+v", ic)
	}
	found := false
	for _, o := range ic[0].Outliers {
		if o == "A" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected A flagged as outlier, got %+v", ic[0].Outliers)
	}
}

// Value-less members (enum constants without literals) fall back to KEY
// convention and still participate in alignment / membership.
func TestDiff_ValuelessMembers(t *testing.T) {
	oracle := []Member{{Key: "ACTIVE"}, {Key: "ARCHIVED"}}
	v3 := []Member{{Key: "active"}, {Key: "archived"}}
	res := Diff("enum:Status", oracle, v3)
	if res.Verdict != VerdictEquivalent {
		t.Fatalf("verdict = %q, want equivalent; %+v", res.Verdict, res)
	}
}
