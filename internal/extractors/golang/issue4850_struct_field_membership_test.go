// Package golang — issue #4850 struct field-membership (CONTAINS) tests.
//
// Root cause: Go struct fields were only consumed for DEPENDS_ON edges and
// call-dispatch stamping; they were never emitted as graph entities, and the
// attachClassContains loop linked SCOPE.Operation methods only. So a Go DTO
// struct resolved to a SCOPE.Component with ZERO field children, the dashboard
// shape endpoint returned rows:[] and classHasFieldChildren was false — the
// same gap #4845/#4851 closed for JS/TS DTOs.
//
// After #4850 every named struct field gets a SCOPE.Schema/field entity AND a
// struct→field CONTAINS edge (via BuildSchemaFieldStructuralRef), and embedded
// fields emit an EXTENDS edge to the in-file base struct so the shape walker
// recurses into promoted fields.
package golang_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// goFieldEntityExists reports whether a SCOPE.Schema/field entity named
// "<struct>.<field>" was emitted.
func goFieldEntityExists(ents []types.EntityRecord, structName, field string) bool {
	want := structName + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

// goHasContainsField reports whether the struct Component named structName
// carries a CONTAINS edge to the SCOPE.Schema/field structural-ref for
// "<struct>.<field>".
func goHasContainsField(ents []types.EntityRecord, path, structName, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("go", path, structName+"."+field)
	for _, e := range ents {
		if e.Name != structName || e.Kind != "SCOPE.Component" {
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

// goHasExtends reports whether the struct Component named structName carries an
// EXTENDS edge to base.
func goHasExtends(ents []types.EntityRecord, structName, base string) bool {
	for _, e := range ents {
		if e.Name != structName || e.Kind != "SCOPE.Component" {
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

// TestStructFieldsAreContained proves a Go DTO struct with ≥2 named fields
// (including grouped `A, B int` and tagged fields) emits one SCOPE.Schema/field
// entity per field AND a struct→field CONTAINS edge for each.
func TestStructFieldsAreContained(t *testing.T) {
	path := "dto/create_user.go"
	src := `package dto

type CreateUserRequest struct {
	Name     string ` + "`json:\"name\"`" + `
	Email    string ` + "`json:\"email\"`" + `
	Age      int
	Verified bool
	internal string ` + "`json:\"-\"`" + `
}
`
	ents := extractRecords(t, src, path)

	// json-tagged fields use the wire name; untagged fields keep the Go name.
	for _, f := range []string{"name", "email", "Age", "Verified"} {
		if !goFieldEntityExists(ents, "CreateUserRequest", f) {
			t.Errorf("expected SCOPE.Schema/field entity CreateUserRequest.%s", f)
		}
		if !goHasContainsField(ents, path, "CreateUserRequest", f) {
			t.Errorf("expected CONTAINS edge from struct to field %q", f)
		}
	}
	// json:"-" fields are excluded from the wire shape (parity with custom DTO
	// field members), so no entity for the bare Go name either.
	if goFieldEntityExists(ents, "CreateUserRequest", "internal") {
		t.Errorf("json:\"-\" field must be excluded from field membership")
	}
}

// TestStructGroupedFieldsAreContained proves grouped same-type fields
// (`X, Y int`) each get their own field entity + CONTAINS edge.
func TestStructGroupedFieldsAreContained(t *testing.T) {
	path := "geo/point.go"
	src := `package geo

type Point struct {
	X, Y int
}
`
	ents := extractRecords(t, src, path)
	for _, f := range []string{"X", "Y"} {
		if !goFieldEntityExists(ents, "Point", f) {
			t.Errorf("expected SCOPE.Schema/field entity Point.%s", f)
		}
		if !goHasContainsField(ents, path, "Point", f) {
			t.Errorf("expected CONTAINS edge from Point to field %q", f)
		}
	}
}

// TestStructNestedAnonymousNotMisattributed proves a field whose type is a
// nested anonymous struct contributes only the OUTER field, not the inner
// struct's fields (membership must not leak across nesting).
func TestStructNestedAnonymousNotMisattributed(t *testing.T) {
	path := "model/envelope.go"
	src := `package model

type Envelope struct {
	Meta struct {
		Total int
	}
	OK bool
}
`
	ents := extractRecords(t, src, path)
	if !goHasContainsField(ents, path, "Envelope", "Meta") {
		t.Errorf("expected CONTAINS edge from Envelope to field Meta")
	}
	if !goHasContainsField(ents, path, "Envelope", "OK") {
		t.Errorf("expected CONTAINS edge from Envelope to field OK")
	}
	// The inner anonymous struct's "Total" must NOT be a field of Envelope.
	if goFieldEntityExists(ents, "Envelope", "Total") {
		t.Errorf("nested anonymous struct field Total must not be an Envelope member")
	}
}

// TestStructEmbeddedFieldEmitsExtends proves an embedded (anonymous) field of
// an in-file base struct emits an EXTENDS edge to the base (so the shape walker
// recurses into promoted fields) and NOT a bogus field entity.
func TestStructEmbeddedFieldEmitsExtends(t *testing.T) {
	path := "model/user.go"
	src := `package model

type Base struct {
	ID        string
	CreatedAt int64
}

type User struct {
	Base
	Username string
}
`
	ents := extractRecords(t, src, path)

	// Base's own fields are contained.
	for _, f := range []string{"ID", "CreatedAt"} {
		if !goFieldEntityExists(ents, "Base", f) {
			t.Errorf("expected SCOPE.Schema/field entity Base.%s", f)
		}
		if !goHasContainsField(ents, path, "Base", f) {
			t.Errorf("expected CONTAINS edge from Base to field %q", f)
		}
	}
	// User's own field is contained.
	if !goHasContainsField(ents, path, "User", "Username") {
		t.Errorf("expected CONTAINS edge from User to field Username")
	}
	// User embeds Base → EXTENDS, and Base must NOT be a field entity of User.
	if !goHasExtends(ents, "User", "Base") {
		t.Errorf("expected EXTENDS edge from User to embedded Base")
	}
	if goFieldEntityExists(ents, "User", "Base") {
		t.Errorf("embedded Base must not be emitted as a User field entity")
	}
}
