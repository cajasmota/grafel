package proto_test

import (
	"context"
	"testing"

	_ "github.com/cajasmota/grafel/internal/extractors/proto"
	"github.com/cajasmota/grafel/internal/types"
)

// TestFields_TypedFieldEntities asserts each message field becomes a
// SCOPE.Schema/field entity carrying its resolved type, and that a field whose
// type is another message produces a REFERENCES edge to that message.
func TestFields_TypedFieldEntities(t *testing.T) {
	src := `syntax = "proto3";

message Order { string id = 1; }

message User {
  string name = 1;
  int32 id = 2;
  repeated Order orders = 3;
}
`
	entities := extract(t, "u.proto", src)

	// Field entity name:string must exist with type=string.
	var nameField *types.EntityRecord
	for i := range entities {
		e := &entities[i]
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == "User.name" {
			nameField = e
		}
	}
	if nameField == nil {
		t.Fatal("expected SCOPE.Schema/field entity User.name")
	}
	if got := nameField.Properties["type"]; got != "string" {
		t.Errorf("User.name type = %q, want string", got)
	}

	// repeated Order orders → field with type containing Order + label repeated.
	var ordersField *types.EntityRecord
	for i := range entities {
		e := &entities[i]
		if e.Subtype == "field" && e.Name == "User.orders" {
			ordersField = e
		}
	}
	if ordersField == nil {
		t.Fatal("expected field entity User.orders")
	}
	if ordersField.Properties["type"] != "Order" {
		t.Errorf("User.orders type = %q, want Order", ordersField.Properties["type"])
	}
	if ordersField.Properties["label"] != "repeated" {
		t.Errorf("User.orders label = %q, want repeated", ordersField.Properties["label"])
	}

	// REFERENCES edge User → Order via the orders field.
	var userMsg *types.EntityRecord
	for i := range entities {
		if entities[i].Subtype == "message" && entities[i].Name == "User" {
			userMsg = &entities[i]
		}
	}
	if userMsg == nil {
		t.Fatal("User message entity not found")
	}
	wantRef := msgTypeRef("u.proto", "Order")
	found := false
	for _, r := range userMsg.Relationships {
		if r.Kind == "REFERENCES" && r.ToID == wantRef {
			found = true
			if r.Properties.Get("via_field") != "orders" {
				t.Errorf("REFERENCES via_field = %q, want orders", r.Properties.Get("via_field"))
			}
		}
	}
	if !found {
		t.Errorf("expected REFERENCES edge User → Order (%q)", wantRef)
	}
}

// TestFields_ScalarsNoReference asserts scalar-typed fields emit NO REFERENCES
// edge — only named message/enum types do.
func TestFields_ScalarsNoReference(t *testing.T) {
	src := `syntax = "proto3";
message Plain {
  string a = 1;
  int64 b = 2;
  bool c = 3;
}
`
	entities := extract(t, "p.proto", src)
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				t.Errorf("unexpected REFERENCES edge on scalar-only message: %+v", r)
			}
		}
	}
}

// TestFields_MapValueReference asserts map<string, Order> references Order.
func TestFields_MapValueReference(t *testing.T) {
	src := `syntax = "proto3";
message Order { string id = 1; }
message Cart {
  map<string, Order> items = 1;
}
`
	entities := extract(t, "c.proto", src)
	wantRef := msgTypeRef("c.proto", "Order")
	found := false
	for _, e := range entities {
		if e.Subtype != "message" || e.Name != "Cart" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == wantRef {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected REFERENCES edge Cart → Order for map value type")
	}
	_ = context.Background
}
