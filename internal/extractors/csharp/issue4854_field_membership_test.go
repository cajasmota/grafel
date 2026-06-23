// Package csharp — issue #4854 general class/record/struct field-membership.
//
// Root cause: only the endpoint/DTO-bound subset of C# members emitted field
// entities (internal/custom/csharp/dto_field_members.go, #4715). A plain data
// class resolved to a SCOPE.Component with ZERO field children, so the
// dashboard shape endpoint returned rows:[] — the same gap #4850/#4855 closed
// for Go and #4845/#4851 for JS/TS.
//
// After #4854 every property / public field / record positional parameter gets
// a SCOPE.Schema/field entity AND a class→field CONTAINS edge, and an in-file
// base class emits an EXTENDS edge.
package csharp_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func csExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extreg.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func csFieldEntityExists(ents []types.EntityRecord, owner, field string) bool {
	want := owner + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

func csHasContainsField(ents []types.EntityRecord, path, owner, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("csharp", path, owner+"."+field)
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

func csHasExtends(ents []types.EntityRecord, owner, base string) bool {
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

// TestCsharpDataClassFieldsAreContained proves a plain (non-endpoint) C# data
// class with auto-properties and a public field emits one SCOPE.Schema/field
// entity per member AND a class→field CONTAINS edge for each.
func TestCsharpDataClassFieldsAreContained(t *testing.T) {
	path := "Models/User.cs"
	src := `
namespace App.Models
{
    public class User
    {
        public int Id { get; set; }
        public string Name { get; set; }
        public bool Active;
        public void Touch() { }
    }
}
`
	ents := csExtract(t, src, path)
	for _, f := range []string{"Id", "Name", "Active"} {
		if !csFieldEntityExists(ents, "User", f) {
			t.Errorf("expected SCOPE.Schema/field entity User.%s", f)
		}
		if !csHasContainsField(ents, path, "User", f) {
			t.Errorf("expected CONTAINS edge from User to field %q", f)
		}
	}
	if csFieldEntityExists(ents, "User", "Touch") {
		t.Errorf("method Touch must not be a field entity")
	}
}

// TestCsharpRecordPositionalParamsAreContained proves a positional record's
// parameters become field members.
func TestCsharpRecordPositionalParamsAreContained(t *testing.T) {
	path := "Models/Point.cs"
	src := `
namespace App.Models
{
    public record Point(int X, int Y);
}
`
	ents := csExtract(t, src, path)
	for _, f := range []string{"X", "Y"} {
		if !csFieldEntityExists(ents, "Point", f) {
			t.Errorf("expected SCOPE.Schema/field entity Point.%s", f)
		}
		if !csHasContainsField(ents, path, "Point", f) {
			t.Errorf("expected CONTAINS edge from Point to field %q", f)
		}
	}
}

// TestCsharpBaseClassEmitsExtends proves an in-file base class emits an EXTENDS
// edge while a merely-implemented interface does not.
func TestCsharpBaseClassEmitsExtends(t *testing.T) {
	path := "Models/Account.cs"
	src := `
namespace App.Models
{
    public class BaseEntity
    {
        public int Id { get; set; }
    }

    public class Account : BaseEntity
    {
        public string Owner { get; set; }
    }
}
`
	ents := csExtract(t, src, path)
	if !csFieldEntityExists(ents, "BaseEntity", "Id") {
		t.Errorf("expected SCOPE.Schema/field entity BaseEntity.Id")
	}
	if !csHasContainsField(ents, path, "Account", "Owner") {
		t.Errorf("expected CONTAINS edge from Account to field Owner")
	}
	if !csHasExtends(ents, "Account", "BaseEntity") {
		t.Errorf("expected EXTENDS edge from Account to in-file base BaseEntity")
	}
}
