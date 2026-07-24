package javascript_test

// graphql_codefirst_typegraph_test.go — value-asserting tests for the
// custom_js_graphql_codefirst_typegraph extractor (epic #3628, completes #3804).
//
// Asserts the GRAPH_RELATES object-type→type edges for the three TS code-first
// GraphQL builders (TypeGraphQL / Nexus / Pothos) carry the exact FromID/ToID
// structural refs + cardinality the SDL pass (#3805) emits, that the owning type
// node is reused (no duplicate), and that scalar / unresolved fields make NO edge.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/grafel/internal/custom/javascript"
	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func runGqlCF(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_graphql_codefirst_typegraph")
	if !ok {
		t.Fatal("custom_js_graphql_codefirst_typegraph not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "typescript", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// findGraphRelates returns the GRAPH_RELATES edge with the given FromID/ToID, or nil.
func findGraphRelates(ents []types.EntityRecord, fromID, toID string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) &&
				r.FromID == fromID && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

func ref(path, name string) string {
	return "scope:operation:method:graphql:" + path + ":" + name
}

func countTypeNodes(ents []types.EntityRecord, name string) int {
	n := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type" && e.Name == name {
			n++
		}
	}
	return n
}

// --- TypeGraphQL ---

func TestGqlCF_TypeGraphQL_ListAndToOne(t *testing.T) {
	path := "user.ts"
	src := `
import { ObjectType, Field, ID } from "type-graphql";

@ObjectType()
class Order {
  @Field(() => ID) id: string;
}

@ObjectType()
class User {
  @Field() name: string;
  @Field(() => [Order]) orders: Order[];
  @Field(() => Order) latest: Order;
}
`
	ents := runGqlCF(t, path, src)

	// User → Order list edge.
	e := findGraphRelates(ents, ref(path, "User"), ref(path, "Order"))
	if e == nil {
		t.Fatal("expected GRAPH_RELATES User→Order")
	}
	if e.Properties.Get("list") != "true" || e.Properties.Get("cardinality") != "to_many" {
		t.Errorf("orders edge: want list=true to_many, got %v", e.Properties)
	}
	if e.Properties.Get("field_name") != "orders" {
		t.Errorf("want field_name=orders, got %q", e.Properties.Get("field_name"))
	}

	// There are two User→Order edges (orders=list, latest=to_one); confirm a
	// to_one one exists too.
	var sawToOne bool
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.FromID == ref(path, "User") && r.ToID == ref(path, "Order") &&
				r.Properties.Get("field_name") == "latest" {
				sawToOne = true
				if r.Properties.Get("list") != "false" || r.Properties.Get("cardinality") != "to_one" {
					t.Errorf("latest edge: want to_one, got %v", r.Properties)
				}
			}
		}
	}
	if !sawToOne {
		t.Error("expected to_one User→Order edge for `latest`")
	}

	// Scalar field `name` → NO edge (no self/scalar target).
	if findGraphRelates(ents, ref(path, "User"), ref(path, "string")) != nil {
		t.Error("scalar field name must not produce an edge")
	}

	// Convergence: exactly one User node, one Order node (node reuse).
	if c := countTypeNodes(ents, "User"); c != 1 {
		t.Errorf("expected 1 User type node, got %d", c)
	}
	if c := countTypeNodes(ents, "Order"); c != 1 {
		t.Errorf("expected 1 Order type node, got %d", c)
	}
}

func TestGqlCF_TypeGraphQL_UnresolvedTargetNoEdge(t *testing.T) {
	path := "u.ts"
	src := `
import { ObjectType, Field } from "type-graphql";
@ObjectType()
class User {
  @Field(() => Address) home: Address;  // Address declared in another file
}
`
	ents := runGqlCF(t, path, src)
	if findGraphRelates(ents, ref(path, "User"), ref(path, "Address")) != nil {
		t.Error("cross-file unresolved target must not produce an edge")
	}
}

// --- Nexus ---

func TestGqlCF_Nexus_ListField(t *testing.T) {
	path := "schema.ts"
	src := `
import { objectType } from "nexus";

export const Order = objectType({
  name: "Order",
  definition(t) { t.id("id"); },
});

export const User = objectType({
  name: "User",
  definition(t) {
    t.string("name");
    t.list.field("orders", { type: "Order" });
    t.nonNull.field("primary", { type: "Order" });
  },
});
`
	ents := runGqlCF(t, path, src)

	e := findGraphRelates(ents, ref(path, "User"), ref(path, "Order"))
	if e == nil {
		t.Fatal("expected GRAPH_RELATES User→Order (nexus)")
	}
	// the list field
	var list, nonNull *types.RelationshipRecord
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.FromID == ref(path, "User") && r.ToID == ref(path, "Order") {
				switch r.Properties.Get("field_name") {
				case "orders":
					list = r
				case "primary":
					nonNull = r
				}
			}
		}
	}
	if list == nil || list.Properties.Get("list") != "true" || list.Properties.Get("cardinality") != "to_many" {
		t.Errorf("nexus orders: want list=true to_many, got %v", list)
	}
	if nonNull == nil || nonNull.Properties.Get("nullable") != "false" || nonNull.Properties.Get("cardinality") != "to_one" {
		t.Errorf("nexus primary: want nullable=false to_one, got %v", nonNull)
	}
	// scalar t.string("name") → no edge
	for i := range ents {
		for j := range ents[i].Relationships {
			if ents[i].Relationships[j].Properties.Get("field_name") == "name" {
				t.Error("nexus scalar `name` must not produce an edge")
			}
		}
	}
}

// --- Pothos ---

func TestGqlCF_Pothos_ListField(t *testing.T) {
	path := "builder.ts"
	src := `
builder.objectType("Order", {
  fields: (t) => ({ id: t.exposeID("id") }),
});

builder.objectType("User", {
  fields: (t) => ({
    name: t.exposeString("name"),
    orders: t.field({ type: ["Order"] }),
    primary: t.field({ type: "Order" }),
  }),
});
`
	ents := runGqlCF(t, path, src)

	var list, single *types.RelationshipRecord
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.FromID == ref(path, "User") && r.ToID == ref(path, "Order") {
				switch r.Properties.Get("field_name") {
				case "orders":
					list = r
				case "primary":
					single = r
				}
			}
		}
	}
	if list == nil || list.Properties.Get("list") != "true" || list.Properties.Get("cardinality") != "to_many" {
		t.Errorf("pothos orders: want list=true to_many, got %v", list)
	}
	if single == nil || single.Properties.Get("cardinality") != "to_one" {
		t.Errorf("pothos primary: want to_one, got %v", single)
	}
}

// Negative: a file with no code-first GraphQL markers yields nothing.
func TestGqlCF_NoMarkers_NoOp(t *testing.T) {
	ents := runGqlCF(t, "plain.ts", `export const x = 1; class Foo { bar: number; }`)
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-graphql file, got %d", len(ents))
	}
}
