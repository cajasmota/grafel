// Package swift — issue #4854 general struct/class field-membership tests.
//
// Root cause: Swift had no general field-membership pass at all (the custom
// Swift emitters cover SwiftUI/Vapor routes, not Codable data model fields). A
// plain Swift data struct resolved to a SCOPE.Component with ZERO field
// children, so the dashboard shape endpoint returned rows:[] — the same gap
// #4850/#4855 closed for Go and #4845/#4851 for JS/TS.
//
// After #4854 every stored property (let/var) gets a SCOPE.Schema/field entity
// AND a type→field CONTAINS edge; an in-file superclass emits an EXTENDS edge;
// computed properties are excluded.
package swift_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func swExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extreg.Get("swift")
	if !ok {
		t.Fatal("swift extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func swFieldEntityExists(ents []types.EntityRecord, owner, field string) bool {
	want := owner + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

func swHasContainsField(ents []types.EntityRecord, path, owner, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("swift", path, owner+"."+field)
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CONTAINS" && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func swHasExtends(ents []types.EntityRecord, owner, base string) bool {
	for _, e := range ents {
		if e.Name != owner || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" && r.ToID == base {
				return true
			}
		}
	}
	return false
}

// TestSwiftStoredPropertiesAreContained proves a plain Codable struct emits one
// SCOPE.Schema/field entity per stored property AND a struct→field CONTAINS
// edge, while a computed property is excluded.
func TestSwiftStoredPropertiesAreContained(t *testing.T) {
	path := "Models/User.swift"
	src := `
struct User: Codable {
    let id: Int
    var name: String
    var balance: Double = 0
    var full: String { return name }
}
`
	ents := swExtract(t, src, path)
	for _, f := range []string{"id", "name", "balance"} {
		if !swFieldEntityExists(ents, "User", f) {
			t.Errorf("expected SCOPE.Schema/field entity User.%s", f)
		}
		if !swHasContainsField(ents, path, "User", f) {
			t.Errorf("expected CONTAINS edge from User to field %q", f)
		}
	}
	if swFieldEntityExists(ents, "User", "full") {
		t.Errorf("computed property full must not be a field entity")
	}
	// A struct adopting only a protocol (Codable) must NOT get an EXTENDS edge.
	if swHasExtends(ents, "User", "Codable") {
		t.Errorf("adopted protocol Codable must not be an in-file EXTENDS target")
	}
}

// TestSwiftSuperclassEmitsExtends proves an in-file superclass emits an EXTENDS
// edge so the shape walker recurses into inherited stored properties.
func TestSwiftSuperclassEmitsExtends(t *testing.T) {
	path := "Models/Account.swift"
	src := `
class BaseEntity {
    let id: Int
    var createdAt: Double
}

class Account: BaseEntity {
    let owner: String
}
`
	ents := swExtract(t, src, path)
	if !swFieldEntityExists(ents, "BaseEntity", "id") {
		t.Errorf("expected SCOPE.Schema/field entity BaseEntity.id")
	}
	if !swHasContainsField(ents, path, "Account", "owner") {
		t.Errorf("expected CONTAINS edge from Account to field owner")
	}
	if !swHasExtends(ents, "Account", "BaseEntity") {
		t.Errorf("expected EXTENDS edge from Account to in-file base BaseEntity")
	}
}
