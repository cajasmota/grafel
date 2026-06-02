package rust_test

// graphql_codefirst_typegraph_test.go — value-asserting tests for the
// rust_graphql_codefirst_typegraph extractor (epic #3872, Rust audit #3884,
// completes #3804 for Rust).
//
// Asserts the GRAPH_RELATES object-type→type edges for async-graphql carry the
// exact FromID/ToID structural refs + cardinality the SDL / py / jsts passes
// emit, that the owning type node is reused (no duplicate), that resolver return
// types produce root→type edges, and that scalar / unresolved / non-GraphQL
// fields make NO edge.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/rust"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runGqlTG(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get("rust_graphql_codefirst_typegraph")
	if !ok {
		t.Fatal("rust_graphql_codefirst_typegraph not registered")
	}
	ents, err := e.Extract(context.Background(),
		extractor.FileInput{Path: path, Language: "rust", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func gtgRef(path, name string) string {
	return "scope:operation:method:graphql:" + path + ":" + name
}

func findGTG(ents []types.EntityRecord, fromID, toID, field string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) &&
				r.FromID == fromID && r.ToID == toID && r.Properties["field_name"] == field {
				return r
			}
		}
	}
	return nil
}

func countTGNodes(ents []types.EntityRecord, name string) int {
	n := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type" && e.Name == name {
			n++
		}
	}
	return n
}

func countGTGEdges(ents []types.EntityRecord) int {
	n := 0
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				n++
			}
		}
	}
	return n
}

// TestGqlTG_SimpleObject_FieldGraph asserts the SimpleObject struct field graph:
// scalar fields make no edge, Vec<Order> is a to_many edge, Option<Account> is a
// nullable to_one edge, and a self-reference is flagged.
func TestGqlTG_SimpleObject_FieldGraph(t *testing.T) {
	path := "schema.rs"
	src := `
use async_graphql::*;

#[derive(SimpleObject)]
struct User {
    id: ID,
    name: String,
    orders: Vec<Order>,
    account: Option<Account>,
    manager: Option<User>,
}

#[derive(SimpleObject)]
struct Order { id: ID }

#[derive(SimpleObject)]
struct Account { id: ID }
`
	ents := runGqlTG(t, path, src)

	// User node exists exactly once.
	if n := countTGNodes(ents, "User"); n != 1 {
		t.Fatalf("expected exactly 1 User type node, got %d", n)
	}

	userRef := gtgRef(path, "User")

	// orders: Vec<Order> -> to_many edge.
	e := findGTG(ents, userRef, gtgRef(path, "Order"), "orders")
	if e == nil {
		t.Fatal("missing User.orders -> Order GRAPH_RELATES edge")
	}
	if e.Properties["cardinality"] != "to_many" || e.Properties["list"] != "true" {
		t.Errorf("User.orders want list=true cardinality=to_many, got list=%q card=%q",
			e.Properties["list"], e.Properties["cardinality"])
	}
	if e.Properties["nullable"] != "false" {
		t.Errorf("User.orders want nullable=false, got %q", e.Properties["nullable"])
	}

	// account: Option<Account> -> nullable to_one edge.
	a := findGTG(ents, userRef, gtgRef(path, "Account"), "account")
	if a == nil {
		t.Fatal("missing User.account -> Account GRAPH_RELATES edge")
	}
	if a.Properties["cardinality"] != "to_one" || a.Properties["list"] != "false" {
		t.Errorf("User.account want to_one/list=false, got card=%q list=%q",
			a.Properties["cardinality"], a.Properties["list"])
	}
	if a.Properties["nullable"] != "true" {
		t.Errorf("User.account want nullable=true, got %q", a.Properties["nullable"])
	}

	// manager: Option<User> -> self_ref.
	m := findGTG(ents, userRef, userRef, "manager")
	if m == nil {
		t.Fatal("missing User.manager -> User self-reference edge")
	}
	if m.Properties["self_ref"] != "true" {
		t.Errorf("User.manager want self_ref=true, got %q", m.Properties["self_ref"])
	}

	// Scalar fields id/name make NO edge.
	for _, f := range []string{"id", "name"} {
		for i := range ents {
			for _, r := range ents[i].Relationships {
				if r.Properties["field_name"] == f {
					t.Errorf("scalar field %q must not produce an edge", f)
				}
			}
		}
	}
}

// TestGqlTG_ResolverReturnType asserts an #[Object] impl Query resolver's return
// type produces a Query->Type edge with the right cardinality, including
// Result<T> / Vec<T> unwrapping.
func TestGqlTG_ResolverReturnType(t *testing.T) {
	path := "query.rs"
	src := `
use async_graphql::*;

#[derive(SimpleObject)]
struct User { id: ID }

#[derive(SimpleObject)]
struct Order { id: ID }

struct Query;

#[Object]
impl Query {
    async fn user(&self, ctx: &Context<'_>, id: ID) -> Result<User> {
        todo!()
    }
    async fn orders(&self) -> Vec<Order> {
        todo!()
    }
    async fn count(&self) -> i32 {
        0
    }
}
`
	ents := runGqlTG(t, path, src)
	queryRef := gtgRef(path, "Query")

	// user(...) -> Result<User> unwrapped to a to_one User edge.
	u := findGTG(ents, queryRef, gtgRef(path, "User"), "user")
	if u == nil {
		t.Fatal("missing Query.user -> User resolver edge (Result<T> unwrap)")
	}
	if u.Properties["cardinality"] != "to_one" {
		t.Errorf("Query.user want to_one, got %q", u.Properties["cardinality"])
	}

	// orders(...) -> Vec<Order> to_many edge.
	o := findGTG(ents, queryRef, gtgRef(path, "Order"), "orders")
	if o == nil {
		t.Fatal("missing Query.orders -> Order resolver edge")
	}
	if o.Properties["cardinality"] != "to_many" {
		t.Errorf("Query.orders want to_many, got %q", o.Properties["cardinality"])
	}

	// count(...) -> i32 scalar return makes NO edge.
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Properties["field_name"] == "count" {
				t.Error("scalar resolver return i32 must not produce an edge")
			}
		}
	}
}

// TestGqlTG_NegativePlainStruct asserts a plain Rust struct with no async-graphql
// derive (and no other GraphQL signal) produces NO type node and NO edge.
func TestGqlTG_NegativePlainStruct(t *testing.T) {
	path := "plain.rs"
	src := `
struct User {
    id: u64,
    orders: Vec<Order>,
}
struct Order { id: u64 }
`
	ents := runGqlTG(t, path, src)
	if len(ents) != 0 {
		t.Fatalf("plain struct (no GraphQL signal) must emit nothing, got %d entities", len(ents))
	}
}

// TestGqlTG_NegativeNonGraphQLImpl asserts a plain `impl` block (no #[Object])
// within an otherwise-GraphQL file produces NO resolver edge.
func TestGqlTG_NegativeNonGraphQLImpl(t *testing.T) {
	path := "mixed.rs"
	src := `
use async_graphql::*;

#[derive(SimpleObject)]
struct User { id: ID }

struct Helper;
impl Helper {
    fn user(&self) -> User { todo!() }
}
`
	ents := runGqlTG(t, path, src)
	// User node should exist (SimpleObject) but NO Helper->User resolver edge,
	// and Helper must not be a type node.
	if countTGNodes(ents, "Helper") != 0 {
		t.Error("non-#[Object] impl Helper must not become a GraphQL type node")
	}
	if countGTGEdges(ents) != 0 {
		t.Errorf("plain impl (no #[Object]) must produce no resolver edge, got %d edges", countGTGEdges(ents))
	}
}

// TestGqlTG_UnresolvedFieldNoEdge asserts a field referencing a type not declared
// in the same file makes NO edge (honest same-file limit).
func TestGqlTG_UnresolvedFieldNoEdge(t *testing.T) {
	path := "partial.rs"
	src := `
use async_graphql::*;

#[derive(SimpleObject)]
struct User {
    id: ID,
    external: Vec<RemoteThing>,
}
`
	ents := runGqlTG(t, path, src)
	if countGTGEdges(ents) != 0 {
		t.Errorf("unresolved cross-file type must produce no edge, got %d", countGTGEdges(ents))
	}
	if countTGNodes(ents, "User") != 1 {
		t.Errorf("User node should still be emitted, got %d", countTGNodes(ents, "User"))
	}
}
