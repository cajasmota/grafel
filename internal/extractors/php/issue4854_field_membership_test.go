// Package php — issue #4854 general class field-membership tests.
//
// Root cause: PHP class properties were only emitted as field entities by the
// framework/ORM-bound custom emitters (internal/custom/php, #4613). A plain PHP
// data class resolved to a SCOPE.Component with ZERO field children, so the
// dashboard shape endpoint returned rows:[] — the same gap #4850/#4855 closed
// for Go and #4845/#4851 for JS/TS.
//
// After #4854 every typed property + promoted constructor parameter gets a
// SCOPE.Schema/field entity AND a class→field CONTAINS edge, and an in-file
// parent class emits an EXTENDS edge.
package php_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func phpExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extreg.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "php",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

func phpFieldEntityExists(ents []types.EntityRecord, owner, field string) bool {
	want := owner + "." + field
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == want {
			return true
		}
	}
	return false
}

func phpHasContainsField(ents []types.EntityRecord, path, owner, field string) bool {
	want := extreg.BuildSchemaFieldStructuralRef("php", path, owner+"."+field)
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

func phpHasExtends(ents []types.EntityRecord, owner, base string) bool {
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

// TestPhpTypedPropertiesAndPromotedParamsAreContained proves a plain data class
// with typed properties AND constructor-promoted params emits one
// SCOPE.Schema/field entity per member AND a class→field CONTAINS edge.
func TestPhpTypedPropertiesAndPromotedParamsAreContained(t *testing.T) {
	path := "src/Model/User.php"
	src := `<?php
namespace App\Model;

class User {
    public int $id;
    protected string $name = "x";
    public ?float $balance;

    public function __construct(public readonly string $email, private int $age) {}

    public function greet(): void {}
}
`
	ents := phpExtract(t, src, path)
	for _, f := range []string{"id", "name", "balance", "email", "age"} {
		if !phpFieldEntityExists(ents, "User", f) {
			t.Errorf("expected SCOPE.Schema/field entity User.%s", f)
		}
		if !phpHasContainsField(ents, path, "User", f) {
			t.Errorf("expected CONTAINS edge from User to field %q", f)
		}
	}
	if phpFieldEntityExists(ents, "User", "greet") {
		t.Errorf("method greet must not be a field entity")
	}
}

// TestPhpBaseClassEmitsExtends proves an in-file parent class emits EXTENDS
// while an implemented interface does not.
func TestPhpBaseClassEmitsExtends(t *testing.T) {
	path := "src/Model/Account.php"
	src := `<?php
namespace App\Model;

interface Stringable {}

class BaseEntity {
    public int $id;
}

class Account extends BaseEntity implements Stringable {
    public string $owner;
}
`
	ents := phpExtract(t, src, path)
	if !phpFieldEntityExists(ents, "BaseEntity", "id") {
		t.Errorf("expected SCOPE.Schema/field entity BaseEntity.id")
	}
	if !phpHasContainsField(ents, path, "Account", "owner") {
		t.Errorf("expected CONTAINS edge from Account to field owner")
	}
	if !phpHasExtends(ents, "Account", "BaseEntity") {
		t.Errorf("expected EXTENDS edge from Account to in-file base BaseEntity")
	}
	if phpHasExtends(ents, "Account", "Stringable") {
		t.Errorf("implemented interface Stringable must not be an EXTENDS target")
	}
}
