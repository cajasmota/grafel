package python_test

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	"github.com/cajasmota/archigraph/internal/extractor"
)

// findFieldChild locates a SCOPE.Schema/field child entity by qualified name.
func findFieldChild(rs []extractResult, name string) *extractResult {
	for i := range rs {
		if rs[i].Name == name && rs[i].Kind == "SCOPE.Schema" && rs[i].Subtype == "field" {
			return &rs[i]
		}
	}
	return nil
}

// hasContainsTo reports whether any emitted entity carries a CONTAINS edge to
// the given qualified field name.
func hasContainsTo(rs []extractResult, fieldName string) bool {
	for _, r := range rs {
		for _, rel := range r.Rels {
			if rel.Kind == "CONTAINS" && rel.ToID == fieldName {
				return true
			}
		}
	}
	return false
}

// ── Pydantic model field membership (#4613) ──────────────────────────────────

func TestPydantic_FieldMembers(t *testing.T) {
	src := `from pydantic import BaseModel, Field
from typing import Optional

class CreateUser(BaseModel):
    name: str
    age: int = 0
    nickname: Optional[str] = None
    score: float = Field(..., gt=0, le=100)
`
	rs := extract(t, "python_pydantic", src)

	name := findFieldChild(rs, "CreateUser.name")
	if name == nil {
		t.Fatal("expected field sub-entity CreateUser.name")
	}
	if name.Props["field_type"] != "string" {
		t.Errorf("name type = %q, want string", name.Props["field_type"])
	}
	if name.Props["optional"] == "true" {
		t.Errorf("name (no default, not Optional) should be required, props=%v", name.Props)
	}

	age := findFieldChild(rs, "CreateUser.age")
	if age == nil || age.Props["field_type"] != "integer" {
		t.Fatalf("expected CreateUser.age:integer, got %+v", age)
	}
	if age.Props["optional"] != "true" {
		t.Errorf("age (has default 0) should be optional, props=%v", age.Props)
	}

	nick := findFieldChild(rs, "CreateUser.nickname")
	if nick == nil {
		t.Fatal("expected CreateUser.nickname")
	}
	if nick.Props["optional"] != "true" {
		t.Errorf("nickname (Optional) should be optional, props=%v", nick.Props)
	}
	if nick.Props["field_type"] != "string" {
		t.Errorf("nickname type = %q, want string", nick.Props["field_type"])
	}

	score := findFieldChild(rs, "CreateUser.score")
	if score == nil {
		t.Fatal("expected CreateUser.score")
	}
	if score.Props["validators"] == "" {
		t.Errorf("score (Field gt/le) should carry validators, props=%v", score.Props)
	}
	// Field(...) ellipsis = required.
	if score.Props["optional"] == "true" {
		t.Errorf("score (Field(...)) should be required, props=%v", score.Props)
	}

	// CONTAINS membership edges.
	if !hasContainsTo(rs, "CreateUser.name") {
		t.Error("expected CONTAINS edge to CreateUser.name")
	}
	if !hasContainsTo(rs, "CreateUser.score") {
		t.Error("expected CONTAINS edge to CreateUser.score")
	}
}

// Signature must be present so the shape resolver can parse it. Verified via the
// raw extractor (the shared `extract` helper drops Signature).
func TestPydantic_FieldSignature(t *testing.T) {
	src := `from pydantic import BaseModel
class P(BaseModel):
    title: str
`
	ext, ok := extractor.Get("python_pydantic")
	if !ok {
		t.Fatal("python_pydantic not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "p.py", Content: []byte(src), Language: "python",
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range ents {
		if e.Name == "P.title" && e.Subtype == "field" {
			found = true
			if e.Signature == "" {
				t.Error("P.title must carry a Signature for the shape resolver")
			}
		}
	}
	if !found {
		t.Fatal("expected P.title field member")
	}
}

// ── DRF serializer field membership (#4613) ──────────────────────────────────

func TestDRF_ExplicitSerializerFieldMembers(t *testing.T) {
	src := `from rest_framework import serializers

class UserSerializer(serializers.Serializer):
    name = serializers.CharField(max_length=100)
    email = serializers.EmailField(required=False)
    age = serializers.IntegerField(allow_null=True)
`
	rs := extract(t, "python_django", src)

	name := findFieldChild(rs, "UserSerializer.name")
	if name == nil || name.Props["field_type"] != "string" {
		t.Fatalf("expected UserSerializer.name:string, got %+v", name)
	}
	if name.Props["optional"] == "true" {
		t.Errorf("name (required default) should not be optional, props=%v", name.Props)
	}
	email := findFieldChild(rs, "UserSerializer.email")
	if email == nil || email.Props["optional"] != "true" {
		t.Fatalf("email (required=False) should be optional, got %+v", email)
	}
	age := findFieldChild(rs, "UserSerializer.age")
	if age == nil || age.Props["field_type"] != "integer" || age.Props["optional"] != "true" {
		t.Fatalf("age (IntegerField, allow_null) wrong: %+v", age)
	}

	if !hasContainsTo(rs, "UserSerializer.name") {
		t.Error("expected CONTAINS edge to UserSerializer.name")
	}
}

func TestDRF_ModelSerializerMetaFieldsList(t *testing.T) {
	src := `from rest_framework import serializers

class ProductSerializer(serializers.ModelSerializer):
    class Meta:
        model = Product
        fields = ["id", "title", "price"]
`
	rs := extract(t, "python_django", src)

	for _, fn := range []string{"id", "title", "price"} {
		if findFieldChild(rs, "ProductSerializer."+fn) == nil {
			t.Errorf("expected Meta field member ProductSerializer.%s", fn)
		}
		if !hasContainsTo(rs, "ProductSerializer."+fn) {
			t.Errorf("expected CONTAINS edge to ProductSerializer.%s", fn)
		}
	}
	// Serializer node should be flagged as meta-list sourced.
	var ser *extractResult
	for i := range rs {
		if rs[i].Name == "ProductSerializer" && rs[i].Props["pattern_type"] == "serializer" {
			ser = &rs[i]
		}
	}
	if ser == nil {
		t.Fatal("expected ProductSerializer component")
	}
	if ser.Props["fields_source"] != "meta_list" {
		t.Errorf("fields_source = %q, want meta_list", ser.Props["fields_source"])
	}
}

func TestDRF_ModelSerializerAllFieldsFlagged(t *testing.T) {
	src := `from rest_framework import serializers

class OrderSerializer(serializers.ModelSerializer):
    class Meta:
        model = Order
        fields = "__all__"
`
	rs := extract(t, "python_django", src)
	var ser *extractResult
	for i := range rs {
		if rs[i].Name == "OrderSerializer" && rs[i].Props["pattern_type"] == "serializer" {
			ser = &rs[i]
		}
	}
	if ser == nil {
		t.Fatal("expected OrderSerializer component")
	}
	// __all__ must be FLAGGED, not silently empty.
	if ser.Props["fields_source"] != "model_all" {
		t.Errorf("fields_source = %q, want model_all", ser.Props["fields_source"])
	}
	if ser.Props["fields_unenumerated"] != "true" {
		t.Errorf("__all__ serializer must be flagged unenumerated, props=%v", ser.Props)
	}
}
