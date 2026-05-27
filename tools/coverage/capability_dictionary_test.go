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
	wantUI := []string{"Structure", "Data Flow", "Navigation", "Type System", "Lifecycle", "Testing"}
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
	wantOrder := []string{"http_backend", "ui_frontend", "meta_framework", "mobile", "desktop", "rpc_framework", "static_site", "ai_integration"}
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
