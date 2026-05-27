package main

import (
	"reflect"
	"testing"
)

func TestPrettyKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"auth_coverage", "Auth Coverage"},
		{"endpoint_synthesis", "Endpoint Synthesis"},
		{"jsx_template", "JSX Template"},
		{"dto_extraction", "DTO Extraction"},
		{"ipc_extraction", "IPC Extraction"},
		{"http_backend", "HTTP Backend"},
		{"sdk", "SDK"},
		{"single", "Single"},
	}
	for _, c := range cases {
		if got := prettyKey(c.in); got != c.want {
			t.Errorf("prettyKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidSubcategory(t *testing.T) {
	if !validSubcategory("http_framework", "ui_frontend") {
		t.Errorf("ui_frontend should be valid for http_framework")
	}
	if validSubcategory("http_framework", "bogus") {
		t.Errorf("bogus should not be valid")
	}
	if validSubcategory("orm", "ui_frontend") {
		t.Errorf("orm has no subcategories — ui_frontend should not validate")
	}
}

func TestValidCapabilityKeyForSubcategory(t *testing.T) {
	// Subcategory-specific key.
	if !validCapabilityKeyForSubcategory("http_framework", "ui_frontend", "component_extraction") {
		t.Errorf("component_extraction should be valid under ui_frontend")
	}
	// Category-wide key still acceptable under any subcategory.
	if !validCapabilityKeyForSubcategory("http_framework", "ui_frontend", "auth_coverage") {
		t.Errorf("auth_coverage (category key) should remain valid under ui_frontend")
	}
	// Unknown key.
	if validCapabilityKeyForSubcategory("http_framework", "ui_frontend", "bogus_key") {
		t.Errorf("bogus_key should not validate")
	}
	// Cross-subcategory key should NOT leak.
	if validCapabilityKeyForSubcategory("http_framework", "ui_frontend", "ipc_extraction") {
		t.Errorf("ipc_extraction (desktop key) must not validate under ui_frontend")
	}
}

func TestSubcategoryRenderKeysExcludesCategoryUnion(t *testing.T) {
	// Render columns should be only the subcategory's keys — no auth_coverage
	// or middleware_coverage leaking into the UI Frontend table.
	got := subcategoryRenderKeys("http_framework", "ui_frontend")
	want := []string{
		"component_extraction",
		"data_fetching",
		"hook_recognition",
		"jsx_template",
		"prop_extraction",
		"router_pattern",
		"state_management",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("render keys = %v, want %v", got, want)
	}
}

func TestSubcategoryCapabilityKeysSorted(t *testing.T) {
	keys := subcategoryCapabilityKeys("http_framework", "ui_frontend")
	// Must include all ui_frontend keys + category-wide keys, sorted, deduped.
	want := []string{
		"auth_coverage",
		"component_extraction",
		"data_fetching",
		"endpoint_synthesis",
		"handler_attribution",
		"hook_recognition",
		"jsx_template",
		"middleware_coverage",
		"prop_extraction",
		"router_pattern",
		"state_management",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("subcategoryCapabilityKeys ui_frontend = %v, want %v", keys, want)
	}
}

func TestOrderedSubcategoriesCanonicalFirst(t *testing.T) {
	subs := map[string]bool{
		"ui_frontend":  true,
		"http_backend": true,
		"mobile":       true,
	}
	got := orderedSubcategories("http_framework", subs)
	want := []string{"http_backend", "ui_frontend", "mobile"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ordered = %v, want %v", got, want)
	}
}

func TestValidateRegistrySubcategory(t *testing.T) {
	reg := &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			{
				ID:          "lang.jsts.framework.react",
				Category:    "http_framework",
				Subcategory: "ui_frontend",
				Language:    "jsts",
				Label:       "React",
				Capabilities: map[string]Capability{
					"router_pattern":       {Status: StatusFull},
					"component_extraction": {Status: StatusPartial, Issue: "x"},
				},
			},
			{
				ID:          "bad.subcat",
				Category:    "http_framework",
				Subcategory: "no_such_subcategory",
				Language:    "jsts",
				Label:       "Bad",
				Capabilities: map[string]Capability{
					"auth_coverage": {Status: StatusMissing, Issue: "x"},
				},
			},
			{
				ID:          "bad.key",
				Category:    "http_framework",
				Subcategory: "ui_frontend",
				Language:    "jsts",
				Label:       "BadKey",
				Capabilities: map[string]Capability{
					"ipc_extraction": {Status: StatusMissing, Issue: "x"},
				},
			},
		},
	}
	res := validateRegistry(reg, ".")
	// React record should be clean (no errors involving it).
	hasError := func(needle string) bool {
		for _, e := range res.Errors {
			if containsStr(e, needle) {
				return true
			}
		}
		return false
	}
	if hasError("lang.jsts.framework.react") {
		t.Errorf("react record should validate; got errors: %v", res.Errors)
	}
	if !hasError("no_such_subcategory") {
		t.Errorf("expected unknown-subcategory error; got: %v", res.Errors)
	}
	if !hasError("ipc_extraction") {
		t.Errorf("expected cross-subcategory key rejection; got: %v", res.Errors)
	}
}

func TestBuildBucketSectionGroupsBySubcategory(t *testing.T) {
	recs := []recordView{
		{ID: "lang.jsts.framework.express", Category: "http_framework", Subcategory: "http_backend", Label: "Express",
			CapByKey: map[string]Capability{"endpoint_synthesis": {Status: StatusFull}}},
		{ID: "lang.jsts.framework.react", Category: "http_framework", Subcategory: "ui_frontend", Label: "React",
			CapByKey: map[string]Capability{"router_pattern": {Status: StatusFull}}},
		{ID: "lang.jsts.framework.legacy", Category: "http_framework", Label: "Legacy",
			CapByKey: map[string]Capability{}},
	}
	sec := buildBucketSection(BucketFrameworks, recs)
	if len(sec.Subsections) != 2 {
		t.Fatalf("want 2 subsections, got %d", len(sec.Subsections))
	}
	if sec.Subsections[0].Subcategory != "http_backend" {
		t.Errorf("first sub should be http_backend, got %s", sec.Subsections[0].Subcategory)
	}
	if sec.Subsections[1].Subcategory != "ui_frontend" {
		t.Errorf("second sub should be ui_frontend, got %s", sec.Subsections[1].Subcategory)
	}
	if len(sec.Records) != 1 || sec.Records[0].ID != "lang.jsts.framework.legacy" {
		t.Errorf("legacy record should fall through to flat Records, got %+v", sec.Records)
	}
	// Render keys must be subcategory-scoped, not the bucket union.
	if got := sec.Subsections[1].CapabilityKeys; len(got) == 0 || got[0] == "auth_coverage" {
		t.Errorf("ui_frontend columns should not include auth_coverage, got %v", got)
	}
}

func TestBuildBucketSectionNoSubcategoriesUsesLegacy(t *testing.T) {
	recs := []recordView{
		{ID: "x", Category: "http_framework", Label: "X",
			CapByKey: map[string]Capability{}},
	}
	sec := buildBucketSection(BucketFrameworks, recs)
	if len(sec.Subsections) != 0 {
		t.Errorf("want no subsections, got %d", len(sec.Subsections))
	}
	if len(sec.Records) != 1 {
		t.Errorf("want 1 flat record, got %d", len(sec.Records))
	}
}

// containsStr is a tiny wrapper to avoid importing strings just for this.
func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
