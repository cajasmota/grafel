package graphql_test

// type_graph_test.go — value-asserting tests for the SDL schema type→type graph
// (#3628, child of the #3607 GraphQL family). The extractor emits GRAPH_RELATES
// edges between the *existing* SCOPE.Schema type nodes for object-typed fields,
// carrying list/nullable cardinality. Scalar fields make no edge.
//
// These tests assert FROM type id + TO type id + cardinality props — not len>0.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// findGraphRelates returns the first GRAPH_RELATES edge from `owner` whose ToID
// resolves to type `target` in `file`, plus its field_name match.
func findGraphRelates(entities []types.EntityRecord, file, owner, target, field string) *types.RelationshipRecord {
	fromRef := extractor.BuildOperationStructuralRef("graphql", file, owner)
	toRef := extractor.BuildOperationStructuralRef("graphql", file, target)
	for _, e := range entities {
		for i := range e.Relationships {
			r := e.Relationships[i]
			if r.Kind != string(types.RelationshipKindGraphRelates) {
				continue
			}
			if r.FromID == fromRef && r.ToID == toRef &&
				(field == "" || r.Properties["field_name"] == field) {
				return &e.Relationships[i]
			}
		}
	}
	return nil
}

func graphRelatesCount(entities []types.EntityRecord) int {
	n := 0
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				n++
			}
		}
	}
	return n
}

// type User { orders: [Order!]! } → User→Order, list=true, non-null.
func TestTypeGraph_ListNonNull(t *testing.T) {
	const file = "schema.graphql"
	src := `type User {
  id: ID!
  name: String!
  orders: [Order!]!
}
type Order {
  id: ID!
}
`
	ents := extractGQL(t, file, src)
	e := findGraphRelates(ents, file, "User", "Order", "orders")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES User → Order on field orders")
	}
	// Convergence: the FromID is the SAME structural ref the base extractor
	// uses for the User type node (BuildOperationStructuralRef), proving we
	// reuse the existing node rather than minting a duplicate.
	wantFrom := extractor.BuildOperationStructuralRef("graphql", file, "User")
	if e.FromID != wantFrom {
		t.Errorf("FromID = %q, want canonical type node ref %q", e.FromID, wantFrom)
	}
	if e.Properties["list"] != "true" {
		t.Errorf("list = %q, want true", e.Properties["list"])
	}
	if e.Properties["nullable"] != "false" {
		t.Errorf("nullable = %q, want false ([Order!]! list is non-null)", e.Properties["nullable"])
	}
	if e.Properties["item_nullable"] != "false" {
		t.Errorf("item_nullable = %q, want false (Order! items non-null)", e.Properties["item_nullable"])
	}
	if e.Properties["cardinality"] != "to_many" {
		t.Errorf("cardinality = %q, want to_many", e.Properties["cardinality"])
	}
}

// type Order { user: User! } → Order→User, non-null singular (to_one).
func TestTypeGraph_SingularNonNull(t *testing.T) {
	const file = "schema.graphql"
	src := `type Order {
  user: User!
}
type User {
  id: ID!
}
`
	ents := extractGQL(t, file, src)
	e := findGraphRelates(ents, file, "Order", "User", "user")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES Order → User on field user")
	}
	if e.Properties["list"] != "false" {
		t.Errorf("list = %q, want false", e.Properties["list"])
	}
	if e.Properties["nullable"] != "false" {
		t.Errorf("nullable = %q, want false (User!)", e.Properties["nullable"])
	}
	if e.Properties["cardinality"] != "to_one" {
		t.Errorf("cardinality = %q, want to_one", e.Properties["cardinality"])
	}
}

// Nullable singular: type A { b: B } → list=false, nullable=true.
func TestTypeGraph_SingularNullable(t *testing.T) {
	const file = "s.graphql"
	src := `type A { b: B }
type B { id: ID! }
`
	ents := extractGQL(t, file, src)
	e := findGraphRelates(ents, file, "A", "B", "b")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES A → B")
	}
	if e.Properties["nullable"] != "true" {
		t.Errorf("nullable = %q, want true (B has no !)", e.Properties["nullable"])
	}
}

// Nullable list items: type A { bs: [B] } → list=true, nullable=true, item_nullable=true.
func TestTypeGraph_ListNullable(t *testing.T) {
	const file = "s.graphql"
	src := `type A { bs: [B] }
type B { id: ID! }
`
	ents := extractGQL(t, file, src)
	e := findGraphRelates(ents, file, "A", "B", "bs")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES A → B (list)")
	}
	if e.Properties["list"] != "true" || e.Properties["nullable"] != "true" || e.Properties["item_nullable"] != "true" {
		t.Errorf("cardinality props = %+v, want list=true nullable=true item_nullable=true", e.Properties)
	}
}

// NEGATIVE: a type whose fields are all scalars makes NO type→type edge.
func TestTypeGraph_ScalarOnly_NoEdge(t *testing.T) {
	const file = "s.graphql"
	src := `type User {
  id: ID!
  name: String!
  age: Int
  active: Boolean
  score: Float!
}
`
	ents := extractGQL(t, file, src)
	if n := graphRelatesCount(ents); n != 0 {
		t.Errorf("scalar-only type produced %d GRAPH_RELATES edges, want 0", n)
	}
}

// NEGATIVE: a field referencing an undeclared custom type → no edge (unresolved).
func TestTypeGraph_UnresolvedType_NoEdge(t *testing.T) {
	const file = "s.graphql"
	src := `type User {
  widget: Widget
}
`
	ents := extractGQL(t, file, src)
	if n := graphRelatesCount(ents); n != 0 {
		t.Errorf("unresolved custom type produced %d edges, want 0", n)
	}
}

// NEGATIVE: enum-typed field makes no type→type edge (enum is not an object).
func TestTypeGraph_EnumTarget_NoEdge(t *testing.T) {
	const file = "s.graphql"
	src := `enum Role { ADMIN USER }
type User { role: Role! }
`
	ents := extractGQL(t, file, src)
	if e := findGraphRelates(ents, file, "User", "Role", "role"); e != nil {
		t.Errorf("enum-typed field must not produce a type→type edge, got %+v", e)
	}
}

// Interface target: a field typed as an interface is a valid edge target.
func TestTypeGraph_InterfaceTarget(t *testing.T) {
	const file = "s.graphql"
	src := `interface Node { id: ID! }
type User { node: Node! }
`
	ents := extractGQL(t, file, src)
	if e := findGraphRelates(ents, file, "User", "Node", "node"); e == nil {
		t.Error("expected GRAPH_RELATES User → Node (interface target)")
	}
}

// Self-reference: type Employee { manager: Employee } → self_ref=true.
func TestTypeGraph_SelfRef(t *testing.T) {
	const file = "s.graphql"
	src := `type Employee {
  manager: Employee
  reports: [Employee!]
}
`
	ents := extractGQL(t, file, src)
	e := findGraphRelates(ents, file, "Employee", "Employee", "manager")
	if e == nil {
		t.Fatal("expected self-referential GRAPH_RELATES Employee → Employee")
	}
	if e.Properties["self_ref"] != "true" {
		t.Errorf("self_ref = %q, want true", e.Properties["self_ref"])
	}
}

// Union expansion (honest-partial): a union-typed field links to each concrete
// member declared in the same file.
func TestTypeGraph_UnionExpansion(t *testing.T) {
	const file = "s.graphql"
	src := `union SearchResult = User | Post
type User { id: ID! }
type Post { id: ID! }
type Query2 { results: [SearchResult!]! }
`
	ents := extractGQL(t, file, src)
	toUser := findGraphRelates(ents, file, "Query2", "User", "results")
	toPost := findGraphRelates(ents, file, "Query2", "Post", "results")
	if toUser == nil || toPost == nil {
		t.Fatalf("expected union expansion Query2 → {User, Post}, got user=%v post=%v", toUser, toPost)
	}
	if toUser.Properties["via_union"] != "SearchResult" {
		t.Errorf("via_union = %q, want SearchResult", toUser.Properties["via_union"])
	}
	if toUser.Properties["list"] != "true" {
		t.Errorf("union member edge should carry the field's list cardinality, got %+v", toUser.Properties)
	}
}

// Field with arguments: posts(first: Int): [Post!]! still resolves the return
// type, not an argument type.
func TestTypeGraph_FieldWithArgs(t *testing.T) {
	const file = "s.graphql"
	src := `type User {
  posts(first: Int, after: String): [Post!]!
}
type Post { id: ID! }
`
	ents := extractGQL(t, file, src)
	e := findGraphRelates(ents, file, "User", "Post", "posts")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES User → Post for field with args")
	}
	if e.Properties["list"] != "true" {
		t.Errorf("list = %q, want true", e.Properties["list"])
	}
}

// input objects are not edge roots: an input type's object-typed field does not
// create a GRAPH_RELATES edge (inputs are argument shapes, not entity nodes).
func TestTypeGraph_InputNotRoot(t *testing.T) {
	const file = "s.graphql"
	src := `input CreateOrder { user: User! }
type User { id: ID! }
`
	ents := extractGQL(t, file, src)
	if n := graphRelatesCount(ents); n != 0 {
		t.Errorf("input object produced %d edges, want 0", n)
	}
}
