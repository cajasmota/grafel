package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseCapabilityMap_FlatAndGrouped exercises both record shapes in
// one document. The loader must place flat keys under .Capabilities and
// grouped keys under .Groups, and never both on the same record.
func TestParseCapabilityMap_FlatAndGrouped(t *testing.T) {
	const src = `
records:
  lang.python.framework.flask:
    capabilities:
      endpoint_synthesis:
        status: full
        symbols:
          - file: internal/engine/http_endpoint_synthesis.go
            functions: [synthesizeFlask]
        tests:
          - file: testdata/fixture.json
        issues_implemented: ["2681"]
        verified_at: "2026-05-28"
  lang.jsts.framework.react:
    capabilities:
      Data Flow:
        state_management:
          status: partial
          symbols:
            - file: internal/extractors/javascript/zustand_store.go
              functions: [emitStoreActionEntities]
`
	cm, err := parseCapabilityMap([]byte(src), "test.yaml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	flask, ok := cm.Records["lang.python.framework.flask"]
	if !ok {
		t.Fatal("flask record missing")
	}
	if flask.IsGrouped() {
		t.Fatal("flask should be flat")
	}
	entry, ok := flask.Lookup("", "endpoint_synthesis")
	if !ok {
		t.Fatal("flask endpoint_synthesis missing")
	}
	if entry.Status != "full" || len(entry.Symbols) != 1 || entry.Symbols[0].File != "internal/engine/http_endpoint_synthesis.go" {
		t.Fatalf("flask entry decoded wrong: %+v", entry)
	}
	if !reflect.DeepEqual(entry.IssuesImplemented, []string{"2681"}) {
		t.Fatalf("issues_implemented: %v", entry.IssuesImplemented)
	}

	react, ok := cm.Records["lang.jsts.framework.react"]
	if !ok {
		t.Fatal("react record missing")
	}
	if !react.IsGrouped() {
		t.Fatal("react should be grouped")
	}
	sm, ok := react.Lookup("Data Flow", "state_management")
	if !ok {
		t.Fatal("react Data Flow/state_management missing")
	}
	if sm.Status != "partial" || len(sm.Symbols[0].Functions) != 1 {
		t.Fatalf("react state_management entry decoded wrong: %+v", sm)
	}
}

// TestParseCapabilityMap_MixedShapeRejected ensures a single record
// carrying both a flat capability and a group is reported as an error
// rather than silently coerced.
func TestParseCapabilityMap_MixedShapeRejected(t *testing.T) {
	const src = `
records:
  lang.foo.bar:
    capabilities:
      direct_flat:
        status: full
      "Some Group":
        nested_key:
          status: partial
`
	_, err := parseCapabilityMap([]byte(src), "test.yaml")
	if err == nil {
		t.Fatal("expected mixed-shape error")
	}
	if !strings.Contains(err.Error(), "mixes flat and grouped") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadCapabilityMap_Missing returns (nil, nil) when the file does
// not exist — callers treat the mapping as optional.
func TestLoadCapabilityMap_Missing(t *testing.T) {
	tmp := t.TempDir()
	cm, err := LoadCapabilityMap(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cm != nil {
		t.Fatalf("expected nil CapabilityMap, got %+v", cm)
	}
}

// TestLoadCapabilityMap_RoundTrip writes a fixture to disk and reads
// it back, exercising the filesystem-bound entry point and confirming
// the sorted iteration helpers produce stable ordering.
func TestLoadCapabilityMap_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "tools", "coverage"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const src = `
records:
  z.record:
    capabilities:
      key_b:
        status: full
      key_a:
        status: partial
  a.record:
    capabilities:
      "Group B":
        key_x:
          status: full
      "Group A":
        key_y:
          status: missing
`
	path := filepath.Join(tmp, "tools", "coverage", "capability-map.yaml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cm, err := LoadCapabilityMap(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ids := cm.SortedRecordIDs()
	want := []string{"a.record", "z.record"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("SortedRecordIDs got %v want %v", ids, want)
	}
	zRec := cm.Records["z.record"]
	flatKeys := zRec.SortedFlatKeys()
	if !reflect.DeepEqual(flatKeys, []string{"key_a", "key_b"}) {
		t.Fatalf("SortedFlatKeys got %v", flatKeys)
	}
	aRec := cm.Records["a.record"]
	groupNames := aRec.SortedGroupNames()
	if !reflect.DeepEqual(groupNames, []string{"Group A", "Group B"}) {
		t.Fatalf("SortedGroupNames got %v", groupNames)
	}
	keys := aRec.SortedKeysInGroup("Group A")
	if !reflect.DeepEqual(keys, []string{"key_y"}) {
		t.Fatalf("SortedKeysInGroup got %v", keys)
	}
}

// TestIsLeafEntry covers the disambiguator at the heart of the loader.
// Any node that contains a known leaf key (status/symbols/tests/...) is
// a leaf; nodes that only contain non-leaf keys are groups.
func TestIsLeafEntry(t *testing.T) {
	leaf, err := parseCapabilityMap([]byte(`
records:
  r.id:
    capabilities:
      cap_a:
        status: full
`), "t.yaml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rec := leaf.Records["r.id"]
	if rec.IsGrouped() {
		t.Fatal("status-only entry should be flat leaf")
	}

	grp, err := parseCapabilityMap([]byte(`
records:
  r.id:
    capabilities:
      "Group X":
        nested:
          status: partial
`), "t.yaml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rec2 := grp.Records["r.id"]
	if !rec2.IsGrouped() {
		t.Fatal("nested-only entry should be group")
	}
}
