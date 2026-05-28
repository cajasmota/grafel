package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadCapabilityDictionaryFromDisk exercises LoadCapabilityDictionary
// against the canonical file shipped with the tool.
func TestLoadCapabilityDictionaryFromDisk(t *testing.T) {
	d, err := LoadCapabilityDictionary(filepath.Join(repoRoot(t), defaultDictionaryPath))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if d.SchemaVersion != 1 {
		t.Errorf("schema version = %d, want 1", d.SchemaVersion)
	}
	if got := d.BucketForCategory("http_framework"); got != BucketFrameworks {
		t.Errorf("BucketForCategory(http_framework) = %q, want %q", got, BucketFrameworks)
	}
	if got := d.BucketForCategory("unknown_category"); got != BucketOther {
		t.Errorf("BucketForCategory(unknown) = %q, want %q (fallback)", got, BucketOther)
	}
	if got := d.SubcategoryDisplay("ui_frontend"); got != "UI Frontend" {
		t.Errorf("SubcategoryDisplay = %q, want %q", got, "UI Frontend")
	}
	wantUI := []string{"Structure", "Data Flow", "Navigation", "Type System", "Lifecycle", "Testing", "Substrate"}
	if got := d.GroupNames("ui_frontend"); !reflect.DeepEqual(got, wantUI) {
		t.Errorf("GroupNames(ui_frontend) = %v, want %v", got, wantUI)
	}
	if got := d.GroupForCapability("ui_frontend", "router_pattern"); got != "Navigation" {
		t.Errorf("GroupForCapability = %q, want %q", got, "Navigation")
	}
	if got := d.GroupForCapability("ui_frontend", "nonexistent"); got != "" {
		t.Errorf("GroupForCapability(nonexistent) = %q, want empty", got)
	}
	if !d.HasSubcategory("http_framework", "ui_frontend") {
		t.Errorf("HasSubcategory(http_framework, ui_frontend) = false")
	}
	if d.HasSubcategory("http_framework", "bogus") {
		t.Errorf("HasSubcategory(http_framework, bogus) = true")
	}
	if d.HasGroup("ui_frontend", "Navigation") != true {
		t.Errorf("HasGroup(ui_frontend, Navigation) = false")
	}
	if d.HasGroup("ui_frontend", "BogusGroup") {
		t.Errorf("HasGroup(ui_frontend, BogusGroup) = true")
	}
	// Subcategory render order — http_framework subcategories must
	// follow the explicit subcategory_order from the dictionary.
	wantOrder := []string{"http_backend", "jvm_backend", "ui_frontend", "meta_framework", "mobile", "desktop", "rpc_framework", "static_site", "ai_integration", "task_queue"}
	if got := d.SubcategoriesByCategory("http_framework"); !reflect.DeepEqual(got, wantOrder) {
		t.Errorf("SubcategoriesByCategory(http_framework) = %v, want %v", got, wantOrder)
	}
}

// TestLoadCapabilityDictionaryMissingFile checks the error path for a
// non-existent dictionary file.
func TestLoadCapabilityDictionaryMissingFile(t *testing.T) {
	if _, err := LoadCapabilityDictionary(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestLoadCapabilityDictionaryRejectsWrongVersion ensures the loader
// refuses dictionaries from an unknown schema version.
func TestLoadCapabilityDictionaryRejectsWrongVersion(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "dict.yaml")
	if err := os.WriteFile(tmp, []byte("$schema_version: 99\nbuckets: {}\ncategories: {}\nsubcategories: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCapabilityDictionary(tmp); err == nil {
		t.Fatalf("expected version error, got nil")
	}
}

// TestEmbeddedDictionaryMatchesDisk ensures the YAML embedded into the
// binary parses cleanly and reports the same bucket order as the on-
// disk file. Guards against the build skipping the embed directive.
func TestEmbeddedDictionaryMatchesDisk(t *testing.T) {
	emb, err := loadEmbeddedDictionary()
	if err != nil {
		t.Fatalf("embedded: %v", err)
	}
	disk, err := LoadCapabilityDictionary(filepath.Join(repoRoot(t), defaultDictionaryPath))
	if err != nil {
		t.Fatalf("disk: %v", err)
	}
	if !reflect.DeepEqual(emb.BucketOrder(), disk.BucketOrder()) {
		t.Errorf("bucket order mismatch: emb=%v disk=%v", emb.BucketOrder(), disk.BucketOrder())
	}
}

// TestDictionarySingletonStable ensures dict() returns the same pointer
// across calls so the load happens exactly once.
func TestDictionarySingletonStable(t *testing.T) {
	a := dict()
	b := dict()
	if a != b {
		t.Errorf("dict() returned distinct pointers across calls: a=%p b=%p", a, b)
	}
}

// TestUniversalCoreOrder pins the #2940 universal-core lane ordering and
// membership query against the shipped dictionary.
func TestUniversalCoreOrder(t *testing.T) {
	d, err := LoadCapabilityDictionary(filepath.Join(repoRoot(t), defaultDictionaryPath))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"Routing", "Auth", "Type System", "Testing", "Substrate"}
	if got := d.UniversalCoreOrder(); !reflect.DeepEqual(got, want) {
		t.Errorf("UniversalCoreOrder = %v, want %v", got, want)
	}
	for _, name := range want {
		if !d.IsUniversalCore(name) {
			t.Errorf("IsUniversalCore(%q) = false, want true", name)
		}
	}
	// "Security" was renamed to "Auth" (#2940): the http_backend auth lane
	// must now be spelled canonically so the universal column picks it up.
	if d.IsUniversalCore("Security") {
		t.Errorf("IsUniversalCore(Security) = true; lane should be renamed to Auth")
	}
	if g := d.GroupForCapability("http_backend", "auth_coverage"); g != "Auth" {
		t.Errorf("http_backend auth_coverage group = %q, want Auth", g)
	}
}

// TestUniversalCoreConsistencyRejectsDivergentSpelling proves the load-
// time guard: a subcategory group whose name case-insensitively matches a
// universal_core lane but is spelled differently (here "auth" vs the
// canonical "Auth") fails to load. This is what keeps the pivot's exact-
// string `universal_core ∩ groups` intersection from silently dropping a
// lane because of a casing divergence.
func TestUniversalCoreConsistencyRejectsDivergentSpelling(t *testing.T) {
	src := `$schema_version: 1
universal_core:
  order: [Auth]
buckets:
  Frameworks: {order: 1, categories: [http_framework]}
categories:
  http_framework: {capabilities: [auth_coverage], subcategory_order: [http_backend]}
subcategories:
  http_backend:
    display: Backend
    parent_category: http_framework
    capabilities: [auth_coverage]
    groups:
      - {name: auth, keys: [auth_coverage]}
`
	tmp := filepath.Join(t.TempDir(), "dict.yaml")
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCapabilityDictionary(tmp)
	if err == nil {
		t.Fatalf("expected consistency error for divergent universal-core spelling, got nil")
	}
	if !containsStr(err.Error(), `must be spelled "Auth"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestOtherCapabilitiesDigest verifies the #2940 merged digest: non-
// universal canonical group cells UNION all framework_specific cells,
// excluding universal-core lanes.
func TestOtherCapabilitiesDigest(t *testing.T) {
	rec := Record{
		ID: "lang.jsts.framework.nestjs", Category: "http_framework",
		Subcategory: "http_backend", Language: "jsts", Label: "NestJS",
		Groups: map[string]map[string]Capability{
			"Routing":    {"endpoint_synthesis": {Status: StatusFull}}, // universal — excluded
			"Validation": {"request_validation": {Status: StatusMissing}},
		},
		FrameworkSpecific: map[string]map[string]Capability{
			"NestJS Internals": {
				"dependency_injection": {Status: StatusMissing},
				"module_graph":         {Status: StatusPartial},
			},
		},
	}
	cells := otherCapabilitiesCells(rec)
	if _, ok := cells["endpoint_synthesis"]; ok {
		t.Errorf("universal-core cell leaked into Other digest set")
	}
	want := []string{"dependency_injection", "module_graph", "request_validation"}
	got := sortedCapKeys(cells)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Other cells = %v, want %v", got, want)
	}
	// 3 cells, 0 full, worst = missing → ❌ 0/3.
	view := recordToView(rec)
	if d := view.GroupDigestByName[OtherCapabilitiesColumn]; d != "❌ 0/3" {
		t.Errorf("Other digest = %q, want ❌ 0/3", d)
	}
}
