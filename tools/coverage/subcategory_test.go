package main

import (
	"os"
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
		"branch_conditions",
		"component_extraction",
		// #2769 substrate confidence overlay.
		"confidence_overlay",
		// #2761 substrate cross-cutting keys.
		"constant_propagation",
		"context_extraction",
		"data_fetching",
		// #2764 Phase 1A substrate effect-classification keys.
		"db_effect",
		// #2766 substrate Phase 1B reachability + dead-code keys.
		"dead_code_detection",
		// #2774 Phase 3C def-use chains.
		"def_use_chain_extraction",
		"enum_extraction",
		"env_fallback_recognition",
		"fs_effect",
		// #2938 jsx_template + hoc_wrapper_recognition removed from the shared
		// ui_frontend Structure group; re-homed to React framework_specific.
		"hook_recognition",
		"http_effect",
		"import_resolution_quality",
		"interface_extraction",
		// #2774 Phase 3B module cycles.
		"module_cycle_detection",
		"mutation_effect",
		"prop_extraction",
		// #2774 Phase 3A pure-function tagging.
		"pure_function_tagging",
		"reachability_analysis",
		// #2770 Phase 2A substrate payload-shape + drift detection.
		"request_shape_extraction",
		"response_shape_extraction",
		"router_pattern",
		// #2772 Phase 2B substrate taint-flow keys.
		"sanitizer_recognition",
		"schema_drift_detection",
		"state_management",
		"state_setter_emission",
		"taint_sink_detection",
		"taint_source_detection",
		// #2774 Phase 3D template-pattern catalog.
		"template_pattern_catalog",
		"tests_linkage",
		"type_alias_extraction",
		"vulnerability_finding",
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
		"branch_conditions",
		"component_extraction",
		// #2769 substrate confidence overlay.
		"confidence_overlay",
		// #2761 substrate keys live under the Substrate group across every
		// subcategory that imports an HTTP / RPC client surface.
		"constant_propagation",
		"context_extraction",
		"data_fetching",
		// #2764 Phase 1A substrate effect-classification keys.
		"db_effect",
		// #2766 substrate Phase 1B reachability + dead-code keys.
		"dead_code_detection",
		// #2774 Phase 3C def-use chains.
		"def_use_chain_extraction",
		"endpoint_synthesis",
		"enum_extraction",
		"env_fallback_recognition",
		"fs_effect",
		"handler_attribution",
		// #2938 jsx_template + hoc_wrapper_recognition removed from ui_frontend.
		"hook_recognition",
		"http_effect",
		"import_resolution_quality",
		"interface_extraction",
		"middleware_coverage",
		// #2774 Phase 3B module cycles.
		"module_cycle_detection",
		"mutation_effect",
		"prop_extraction",
		// #2774 Phase 3A pure-function tagging.
		"pure_function_tagging",
		"reachability_analysis",
		// #2770 Phase 2A substrate payload-shape + drift detection.
		"request_shape_extraction",
		"response_shape_extraction",
		"router_pattern",
		// #2772 Phase 2B substrate taint-flow keys.
		"sanitizer_recognition",
		"schema_drift_detection",
		"state_management",
		"state_setter_emission",
		"taint_sink_detection",
		"taint_source_detection",
		// #2774 Phase 3D template-pattern catalog.
		"template_pattern_catalog",
		"tests_linkage",
		"type_alias_extraction",
		"vulnerability_finding",
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
				Groups: map[string]map[string]Capability{
					"Navigation": {"router_pattern": {Status: StatusFull}},
					"Structure":  {"component_extraction": {Status: StatusPartial, Issue: "x"}},
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
				Groups: map[string]map[string]Capability{
					"Structure": {"ipc_extraction": {Status: StatusMissing, Issue: "x"}},
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
	// The ui_frontend record populates every canonical group so the
	// don't-strand guard (#2902) keeps all columns; this isolates the
	// subcategory-splitting behaviour from the column-hiding behaviour
	// (covered by TestBuildBucketSectionHidesStrandedGroups).
	recs := []recordView{
		recordToView(Record{ID: "lang.jsts.framework.express", Category: "http_framework", Subcategory: "http_backend", Label: "Express",
			Capabilities: map[string]Capability{"endpoint_synthesis": {Status: StatusFull}}}),
		recordToView(Record{ID: "lang.jsts.framework.react", Category: "http_framework", Subcategory: "ui_frontend", Label: "React",
			Groups: map[string]map[string]Capability{
				"Structure":   {"component_extraction": {Status: StatusFull}},
				"Data Flow":   {"prop_flow": {Status: StatusFull}},
				"Navigation":  {"router_pattern": {Status: StatusFull}},
				"Type System": {"prop_types": {Status: StatusFull}},
				"Lifecycle":   {"lifecycle_hooks": {Status: StatusFull}},
				"Testing":     {"test_recognition": {Status: StatusFull}},
				"Substrate":   {"def_use": {Status: StatusFull}},
			}}),
		recordToView(Record{ID: "lang.jsts.framework.legacy", Category: "http_framework", Label: "Legacy",
			Capabilities: map[string]Capability{}}),
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
	// ui_frontend has a declared group taxonomy (#2737), so the
	// subsection now renders group-digest columns rather than per-
	// capability columns. CapabilityKeys is unset; GroupNames carries
	// the canonical group render order.
	uiSec := sec.Subsections[1]
	if len(uiSec.CapabilityKeys) != 0 {
		t.Errorf("ui_frontend should use group columns, got CapabilityKeys=%v", uiSec.CapabilityKeys)
	}
	wantGroups := []string{"Structure", "Data Flow", "Navigation", "Type System", "Lifecycle", "Testing", "Substrate"}
	if !reflect.DeepEqual(uiSec.GroupNames, wantGroups) {
		t.Errorf("ui_frontend GroupNames = %v, want %v", uiSec.GroupNames, wantGroups)
	}
}

// TestNonStrandedGroupNames covers the don't-strand render guard (#2902)
// in isolation: a column whose digest is "—" for every record is dropped,
// a column with at least one non-"—" cell (including a tracked-but-missing
// "❌ 0/n" digest) is kept, and canonical order is preserved.
func TestNonStrandedGroupNames(t *testing.T) {
	candidates := []string{"A", "B", "C", "D"}
	digests := []map[string]string{
		// rec0: carries A (full) and C (tracked-but-missing).
		{"A": "✅ 1/1", "B": "—", "C": "❌ 0/1", "D": "—"},
		// rec1: carries nothing extra; B and D still all-"—".
		{"A": "—", "B": "—", "C": "—", "D": "—"},
	}
	got := nonStrandedGroupNames(candidates, len(digests), func(r int, g string) string {
		return groupCell(digests[r], g)
	})
	want := []string{"A", "C"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nonStrandedGroupNames = %v, want %v", got, want)
	}
	// All-stranded → empty slice (caller falls back to a Name/Notes table).
	allDash := []map[string]string{{"A": "—", "B": "—"}}
	if g := nonStrandedGroupNames([]string{"A", "B"}, 1, func(r int, name string) string {
		return groupCell(allDash[r], name)
	}); len(g) != 0 {
		t.Errorf("all-stranded should yield empty, got %v", g)
	}
	// Zero records / nil candidates → passthrough (no panic, no filtering).
	if g := nonStrandedGroupNames(candidates, 0, nil); !reflect.DeepEqual(g, candidates) {
		t.Errorf("zero records should pass candidates through, got %v", g)
	}
}

// TestBuildBucketSectionHidesStrandedGroups proves the guard wired into
// buildBucketSection drops an all-"—" group column from the rendered
// subsection while keeping populated columns in canonical order.
func TestBuildBucketSectionHidesStrandedGroups(t *testing.T) {
	// Two ui_frontend records, neither carrying Navigation / Type System /
	// Lifecycle / Testing / Substrate cells, so those columns are all-"—".
	recs := []recordView{
		recordToView(Record{ID: "lang.jsts.framework.vue", Category: "http_framework", Subcategory: "ui_frontend", Label: "Vue",
			Groups: map[string]map[string]Capability{
				"Structure": {"component_extraction": {Status: StatusFull}},
				"Data Flow": {"prop_flow": {Status: StatusPartial, Issue: "x"}},
			}}),
		recordToView(Record{ID: "lang.jsts.framework.svelte", Category: "http_framework", Subcategory: "ui_frontend", Label: "Svelte",
			Groups: map[string]map[string]Capability{
				"Structure": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
			}}),
	}
	sec := buildBucketSection(BucketFrameworks, recs)
	if len(sec.Subsections) != 1 {
		t.Fatalf("want 1 subsection, got %d", len(sec.Subsections))
	}
	got := sec.Subsections[0].GroupNames
	want := []string{"Structure", "Data Flow"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stranded groups not hidden: GroupNames = %v, want %v", got, want)
	}
}

// TestGroupForCapability checks the canonical taxonomy lookups.
func TestGroupForCapability(t *testing.T) {
	if g := groupForCapability("ui_frontend", "router_pattern"); g != "Navigation" {
		t.Errorf("router_pattern should resolve to Navigation, got %q", g)
	}
	if g := groupForCapability("ui_frontend", "component_extraction"); g != "Structure" {
		t.Errorf("component_extraction should resolve to Structure, got %q", g)
	}
	if g := groupForCapability("ui_frontend", "nonexistent_key"); g != "" {
		t.Errorf("nonexistent_key should return empty, got %q", g)
	}
}

// TestGroupDigest checks the worst-glyph + full-count/total digest.
func TestGroupDigest(t *testing.T) {
	caps := map[string]Capability{
		"a": {Status: StatusFull},
		"b": {Status: StatusPartial},
		"c": {Status: StatusMissing},
	}
	if got := groupDigest(caps); got != "❌ 1/3" {
		t.Errorf("groupDigest = %q, want ❌ 1/3", got)
	}
	if got := groupDigest(map[string]Capability{}); got != "—" {
		t.Errorf("empty groupDigest = %q, want —", got)
	}
}

// TestValidateGroupedRecord exercises the new #2737 validation rules:
// canonical group names, capability-belongs-to-group, and
// uniqueness-within-record.
func TestValidateGroupedRecord(t *testing.T) {
	reg := &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			{
				ID: "lang.jsts.framework.react", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "React",
				Groups: map[string]map[string]Capability{
					"Structure":  {"component_extraction": {Status: StatusPartial, Issue: "x"}},
					"Navigation": {"router_pattern": {Status: StatusFull}},
				},
			},
			{
				ID: "lang.jsts.framework.bad-group", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "BadGroup",
				Groups: map[string]map[string]Capability{
					"Strcture": {"component_extraction": {Status: StatusFull}},
				},
			},
			{
				ID: "lang.jsts.framework.bad-place", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "BadPlace",
				Groups: map[string]map[string]Capability{
					"Structure": {"router_pattern": {Status: StatusFull}},
				},
			},
			{
				ID: "lang.jsts.framework.dup-key", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "DupKey",
				Groups: map[string]map[string]Capability{
					"Structure":  {"component_extraction": {Status: StatusFull}},
					"Navigation": {"component_extraction": {Status: StatusFull}},
				},
			},
		},
	}
	res := validateRegistry(reg, ".")
	hasError := func(needle string) bool {
		for _, e := range res.Errors {
			if containsStr(e, needle) {
				return true
			}
		}
		return false
	}
	if hasError("lang.jsts.framework.react") {
		t.Errorf("react should validate; got: %v", res.Errors)
	}
	if !hasError(`unknown group "Strcture"`) {
		t.Errorf("expected unknown group error; got: %v", res.Errors)
	}
	if !hasError("does not belong to group") {
		t.Errorf("expected capability-belongs-to-group error; got: %v", res.Errors)
	}
	if !hasError("already declared under group") {
		t.Errorf("expected duplicate-key error; got: %v", res.Errors)
	}
}

// TestFlatShapeForbiddenForGroupedSubcategory pins the #2758 regression
// guard: a record carrying a subcategory whose taxonomy declares groups
// MUST use the nested shape. The flat shape — even with valid
// capability keys — is rejected so by-language pivots never render
// empty group columns for that record again.
func TestFlatShapeForbiddenForGroupedSubcategory(t *testing.T) {
	reg := &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			{
				ID:          "lang.java.framework.quarkus",
				Category:    "http_framework",
				Subcategory: "http_backend",
				Language:    "java",
				Label:       "Quarkus",
				Capabilities: map[string]Capability{
					"endpoint_synthesis": {Status: StatusFull},
				},
			},
			{
				ID:          "static.example",
				Category:    "http_framework",
				Subcategory: "static_site",
				Language:    "multi",
				Label:       "Static",
				Capabilities: map[string]Capability{
					"build_extraction": {Status: StatusMissing, Issue: "x"},
				},
			},
		},
	}
	res := validateRegistry(reg, ".")
	hasError := func(needle string) bool {
		for _, e := range res.Errors {
			if containsStr(e, needle) {
				return true
			}
		}
		return false
	}
	if !hasError("flat capability shape forbidden") {
		t.Errorf("expected flat-shape-forbidden error for http_backend record; got: %v", res.Errors)
	}
	// static_site has no group taxonomy declared — the flat shape is
	// still permitted there.
	for _, e := range res.Errors {
		if containsStr(e, "static.example") && containsStr(e, "flat capability shape forbidden") {
			t.Errorf("static_site record should accept flat shape (no group taxonomy); got: %v", res.Errors)
		}
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

// TestFrameworkSpecificRoundTrip exercises load + write of a record
// that carries framework_specific entries, both for the flat and
// grouped capability shapes (#2739).
func TestFrameworkSpecificRoundTrip(t *testing.T) {
	src := `{
  "$schema_version": 1,
  "records": [
    {
      "id": "lang.jsts.framework.angular",
      "category": "http_framework",
      "subcategory": "ui_frontend",
      "language": "jsts",
      "label": "Angular",
      "capabilities": {
        "Structure": {
          "component_extraction": {"status": "missing"}
        }
      },
      "framework_specific": {
        "Angular Internals": {
          "dependency_injection": {"status": "missing"}
        }
      }
    }
  ]
}`
	tmp := t.TempDir() + "/reg.json"
	if err := writeFile(tmp, []byte(src)); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistry(tmp)
	if err != nil {
		t.Fatal(err)
	}
	rec := reg.Records[0]
	if !rec.HasFrameworkSpecific() {
		t.Fatalf("expected framework_specific to be present")
	}
	if len(rec.FrameworkSpecific["Angular Internals"]) != 1 {
		t.Errorf("expected 1 cap in Angular Internals; got %v", rec.FrameworkSpecific)
	}
	if err := saveRegistry(tmp, reg); err != nil {
		t.Fatal(err)
	}
	reg2, err := loadRegistry(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !reg2.Records[0].HasFrameworkSpecific() {
		t.Errorf("framework_specific lost across save/load")
	}
	if reg2.Records[0].FrameworkSpecific["Angular Internals"]["dependency_injection"].Status != StatusMissing {
		t.Errorf("framework_specific status not preserved")
	}
}

// writeFile is a tiny helper used by round-trip tests.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// TestValidateFrameworkSpecific exercises the #2739 rules for the
// framework_specific field: empty group names rejected, malformed
// capability keys rejected, cross-group key collisions rejected, and
// canonical capability collisions rejected.
func TestValidateFrameworkSpecific(t *testing.T) {
	reg := &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			{
				ID: "lang.jsts.framework.angular", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "Angular",
				Groups: map[string]map[string]Capability{
					"Structure": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
				},
				FrameworkSpecific: map[string]map[string]Capability{
					"Angular Internals": {"dependency_injection": {Status: StatusMissing, Issue: "x"}},
				},
			},
			{
				ID: "lang.jsts.framework.fs-empty", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "EmptyGroup",
				Groups: map[string]map[string]Capability{
					"Structure": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
				},
				FrameworkSpecific: map[string]map[string]Capability{
					"   ": {"foo_bar": {Status: StatusMissing, Issue: "x"}},
				},
			},
			{
				ID: "lang.jsts.framework.fs-badkey", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "BadKey",
				Groups: map[string]map[string]Capability{
					"Structure": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
				},
				FrameworkSpecific: map[string]map[string]Capability{
					"BadKey Internals": {"Bad Key With Spaces": {Status: StatusMissing, Issue: "x"}},
				},
			},
			{
				ID: "lang.jsts.framework.fs-dupkey", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "DupKey",
				Groups: map[string]map[string]Capability{
					"Structure": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
				},
				FrameworkSpecific: map[string]map[string]Capability{
					"DupKey A": {"foo_bar": {Status: StatusMissing, Issue: "x"}},
					"DupKey B": {"foo_bar": {Status: StatusMissing, Issue: "x"}},
				},
			},
			{
				ID: "lang.jsts.framework.fs-clash", Category: "http_framework",
				Subcategory: "ui_frontend", Language: "jsts", Label: "Clash",
				Groups: map[string]map[string]Capability{
					"Structure": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
				},
				FrameworkSpecific: map[string]map[string]Capability{
					"Clash Internals": {"component_extraction": {Status: StatusMissing, Issue: "x"}},
				},
			},
		},
	}
	res := validateRegistry(reg, ".")
	hasError := func(needle string) bool {
		for _, e := range res.Errors {
			if containsStr(e, needle) {
				return true
			}
		}
		return false
	}
	hasErrorFor := func(id, needle string) bool {
		for _, e := range res.Errors {
			if containsStr(e, id) && containsStr(e, needle) {
				return true
			}
		}
		return false
	}
	if hasErrorFor("lang.jsts.framework.angular", "framework_specific") {
		t.Errorf("angular framework_specific should validate; got: %v", res.Errors)
	}
	if !hasError("group name is empty or whitespace-only") {
		t.Errorf("expected empty-group-name error; got: %v", res.Errors)
	}
	if !hasError("must match ^[a-z]") {
		t.Errorf("expected capability-key shape error; got: %v", res.Errors)
	}
	if !hasError("already declared under framework_specific group") {
		t.Errorf("expected duplicate-key error; got: %v", res.Errors)
	}
	if !hasError("also appears in canonical capabilities") {
		t.Errorf("expected canonical-clash error; got: %v", res.Errors)
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
