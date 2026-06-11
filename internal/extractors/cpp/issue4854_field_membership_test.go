// Package cpp — issue #4854 general struct/class field-membership tests.
//
// Root cause: C/C++ data members were only stashed in the owner Component's
// Metadata; they were never emitted as graph entities, so a plain C++ data
// class resolved to a SCOPE.Component with ZERO field children and the
// dashboard shape endpoint returned rows:[] — the same gap #4850/#4855 closed
// for Go and #4845/#4851 for JS/TS.
//
// After #4854 every data member gets a SCOPE.Schema/field entity AND a
// class→field CONTAINS edge (via BuildSchemaFieldStructuralRef), and a C++
// base class declared in the same file emits an EXTENDS edge so the shape
// walker recurses into inherited members.
package cpp_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func cppFieldEntityExists(ents []types.EntityRecord, owner, field string) bool {
	want := owner + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

func cppHasContainsField(ents []types.EntityRecord, lang, path, owner, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef(lang, path, owner+"."+field)
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

func cppHasExtends(ents []types.EntityRecord, owner, base string) bool {
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

// TestCppDataClassFieldsAreContained proves a plain (non-ORM, non-endpoint)
// C++ data class with several members — including pointer / array decorated
// declarators and a multi-name declaration — emits one SCOPE.Schema/field
// entity per member AND a class→field CONTAINS edge for each. Member functions
// must NOT be emitted as fields.
func TestCppDataClassFieldsAreContained(t *testing.T) {
	path := "model/user.hpp"
	src := `
struct User {
	int id;
	std::string name;
	double balance;
	char* token;
	int scores[8];
	bool a, b;
	void greet() { return; }
};
`
	ents, err := extractCPP(src, path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, f := range []string{"id", "name", "balance", "token", "scores", "a", "b"} {
		if !cppFieldEntityExists(ents, "User", f) {
			t.Errorf("expected SCOPE.Schema/field entity User.%s", f)
		}
		if !cppHasContainsField(ents, "cpp", path, "User", f) {
			t.Errorf("expected CONTAINS edge from User to field %q", f)
		}
	}
	if cppFieldEntityExists(ents, "User", "greet") {
		t.Errorf("member function greet must not be a field entity")
	}
}

// TestCppBaseClassEmitsExtends proves a derived class whose base is declared
// in the same file emits an EXTENDS edge (so the shape walker recurses into
// inherited fields) and that both classes carry their own field members.
func TestCppBaseClassEmitsExtends(t *testing.T) {
	path := "model/account.hpp"
	src := `
class Base {
public:
	int id;
	long createdAt;
};

class Account : public Base {
public:
	std::string owner;
};
`
	ents, err := extractCPP(src, path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, f := range []string{"id", "createdAt"} {
		if !cppFieldEntityExists(ents, "Base", f) {
			t.Errorf("expected SCOPE.Schema/field entity Base.%s", f)
		}
	}
	if !cppFieldEntityExists(ents, "Account", "owner") {
		t.Errorf("expected SCOPE.Schema/field entity Account.owner")
	}
	if !cppHasContainsField(ents, "cpp", path, "Account", "owner") {
		t.Errorf("expected CONTAINS edge from Account to field owner")
	}
	if !cppHasExtends(ents, "Account", "Base") {
		t.Errorf("expected EXTENDS edge from Account to in-file base Base")
	}
}

// TestCFieldsAreContained proves the same field-membership works for a plain C
// struct (language key "c").
func TestCFieldsAreContained(t *testing.T) {
	path := "model/point.h"
	src := `
struct Point {
	int x;
	int y;
};
`
	ents, err := extractC(src, path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, f := range []string{"x", "y"} {
		if !cppFieldEntityExists(ents, "Point", f) {
			t.Errorf("expected SCOPE.Schema/field entity Point.%s", f)
		}
		if !cppHasContainsField(ents, "c", path, "Point", f) {
			t.Errorf("expected CONTAINS edge from Point to field %q", f)
		}
	}
}
