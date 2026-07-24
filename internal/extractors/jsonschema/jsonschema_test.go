package jsonschema_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/jsonschema"
	"github.com/cajasmota/grafel/internal/types"
)

func extract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("jsonschema")
	if !ok {
		t.Fatal("jsonschema extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "jsonschema",
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func find(ents []types.EntityRecord, subtype, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// TestJSONSchema_PropertyType asserts a schema's property becomes a typed field.
func TestJSONSchema_PropertyType(t *testing.T) {
	src := `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "User",
  "type": "object",
  "properties": {
    "id": {"type": "integer"},
    "name": {"type": "string"}
  }
}`
	ents := extract(t, "user.schema.json", src)

	sc := find(ents, "object", "User")
	if sc == nil {
		t.Fatal("expected object schema User")
	}
	if sc.Kind != "SCOPE.Schema" {
		t.Errorf("User kind = %q, want SCOPE.Schema", sc.Kind)
	}
	idField := find(ents, "field", "User.id")
	if idField == nil {
		t.Fatal("expected field User.id")
	}
	if got := idField.Properties["type"]; got != "integer" {
		t.Errorf("User.id type = %q, want integer", got)
	}

	wantRef := "scope:schema:column:jsonschema:user.schema.json:User#id"
	found := false
	for _, r := range sc.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == wantRef {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CONTAINS edge User → id (%q)", wantRef)
	}
}

// TestJSONSchema_RefEdge asserts a $ref to another schema yields a REFERENCES
// edge, and the $defs subschema is emitted as its own entity.
func TestJSONSchema_RefEdge(t *testing.T) {
	src := `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "User",
  "type": "object",
  "properties": {
    "address": {"$ref": "#/$defs/Address"}
  },
  "$defs": {
    "Address": {
      "type": "object",
      "properties": {"city": {"type": "string"}}
    }
  }
}`
	ents := extract(t, "user.schema.json", src)

	user := find(ents, "object", "User")
	if user == nil {
		t.Fatal("expected schema User")
	}
	wantRef := extractor.BuildOperationStructuralRef("jsonschema", "user.schema.json", "Address")
	found := false
	for _, r := range user.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == wantRef {
			found = true
			if r.Properties.Get("via_field") != "address" {
				t.Errorf("REFERENCES via_field = %q, want address", r.Properties.Get("via_field"))
			}
		}
	}
	if !found {
		t.Errorf("expected REFERENCES edge User → Address (%q)", wantRef)
	}

	// $defs.Address emitted as its own schema with a city field.
	addr := find(ents, "object", "Address")
	if addr == nil {
		t.Fatal("expected $defs schema Address")
	}
	if find(ents, "field", "Address.city") == nil {
		t.Error("expected field Address.city")
	}
}

// TestJSONSchema_ArrayRef asserts array-of-$ref yields a REFERENCES edge and an
// array<T> field type.
func TestJSONSchema_ArrayRef(t *testing.T) {
	src := `{
  "title": "Cart",
  "properties": {
    "orders": {"type": "array", "items": {"$ref": "#/$defs/Order"}}
  }
}`
	ents := extract(t, "cart.schema.json", src)
	cart := find(ents, "object", "Cart")
	if cart == nil {
		t.Fatal("expected schema Cart")
	}
	wantRef := extractor.BuildOperationStructuralRef("jsonschema", "cart.schema.json", "Order")
	found := false
	for _, r := range cart.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == wantRef {
			found = true
		}
	}
	if !found {
		t.Error("expected REFERENCES edge Cart → Order (array items $ref)")
	}
	orders := find(ents, "field", "Cart.orders")
	if orders == nil || orders.Properties["type"] != "array<Order>" {
		t.Errorf("Cart.orders type = %v, want array<Order>", orders)
	}
}

// TestJSONSchema_NonSchemaNoOp asserts a JSON file without schema markers emits
// nothing.
func TestJSONSchema_NonSchemaNoOp(t *testing.T) {
	ents := extract(t, "data.schema.json", `{"foo": 1, "bar": [2,3]}`)
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-schema JSON, got %d", len(ents))
	}
}

// TestJSONSchema_TitleFallback asserts the file basename is used when no title.
func TestJSONSchema_TitleFallback(t *testing.T) {
	src := `{"$schema":"x","properties":{"a":{"type":"string"}}}`
	ents := extract(t, "path/to/order.schema.json", src)
	if find(ents, "object", "order") == nil {
		t.Error("expected schema named 'order' from basename fallback")
	}
}
