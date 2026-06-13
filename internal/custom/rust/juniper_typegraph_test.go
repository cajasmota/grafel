package rust_test

// juniper_typegraph_test.go — value-asserting tests for the
// custom_rust_juniper_typegraph extractor (#5007, follow-up from #4964).
//
// Asserts the GRAPH_RELATES object-type→type edges for juniper carry the exact
// FromID/ToID structural refs + cardinality the SDL / async-graphql / py / jsts
// passes emit, that the owning type node is reused (no duplicate), that resolver
// return types produce root→type edges, and that scalar / unresolved /
// non-GraphQL / wrong-language inputs make NO edge.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/rust"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runJunTG(t *testing.T, path, lang, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get("custom_rust_juniper_typegraph")
	if !ok {
		t.Fatal("custom_rust_juniper_typegraph not registered")
	}
	ents, err := e.Extract(context.Background(),
		extractor.FileInput{Path: path, Language: lang, Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func junTGRef(path, name string) string {
	return "scope:operation:method:graphql:" + path + ":" + name
}

func findJunTG(ents []types.EntityRecord, fromID, toID, field string) *types.RelationshipRecord {
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

func countJunTGNodes(ents []types.EntityRecord, name string) int {
	n := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type" && e.Name == name {
			n++
		}
	}
	return n
}

func countJunTGEdges(ents []types.EntityRecord) int {
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

// TestJunTG_GraphQLObject_FieldGraph (happy path) asserts the GraphQLObject
// struct field graph: scalar fields make no edge, Vec<Order> is a to_many edge,
// Option<Account> is a nullable to_one edge, and a self-reference is flagged.
func TestJunTG_GraphQLObject_FieldGraph(t *testing.T) {
	path := "schema.rs"
	src := `
use juniper::*;

#[derive(GraphQLObject)]
struct User {
    id: i32,
    name: String,
    orders: Vec<Order>,
    account: Option<Account>,
    manager: Option<User>,
}

#[derive(GraphQLObject)]
struct Order { id: i32 }

#[derive(GraphQLObject)]
struct Account { id: i32 }

#[derive(GraphQLInputObject)]
struct NewUser { name: String, account: Option<Account> }
`
	ents := runJunTG(t, path, "rust", src)

	// User node exists exactly once.
	if n := countJunTGNodes(ents, "User"); n != 1 {
		t.Fatalf("expected exactly 1 User type node, got %d", n)
	}
	// GraphQLInputObject NewUser is NOT an owner / type node.
	if n := countJunTGNodes(ents, "NewUser"); n != 0 {
		t.Errorf("GraphQLInputObject NewUser must not be a type node, got %d", n)
	}

	userRef := junTGRef(path, "User")

	// orders: Vec<Order> -> to_many edge, framework=juniper.
	o := findJunTG(ents, userRef, junTGRef(path, "Order"), "orders")
	if o == nil {
		t.Fatal("missing User.orders -> Order GRAPH_RELATES edge")
	}
	if o.Properties["cardinality"] != "to_many" || o.Properties["list"] != "true" {
		t.Errorf("User.orders want list=true/to_many, got list=%q card=%q",
			o.Properties["list"], o.Properties["cardinality"])
	}
	if o.Properties["nullable"] != "false" {
		t.Errorf("User.orders want nullable=false, got %q", o.Properties["nullable"])
	}
	if o.Properties["framework"] != "juniper" {
		t.Errorf("want framework=juniper, got %q", o.Properties["framework"])
	}
	if o.Properties["graphql_field"] != "User.orders" {
		t.Errorf("want graphql_field=User.orders, got %q", o.Properties["graphql_field"])
	}

	// account: Option<Account> -> nullable to_one edge.
	a := findJunTG(ents, userRef, junTGRef(path, "Account"), "account")
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
	m := findJunTG(ents, userRef, userRef, "manager")
	if m == nil {
		t.Fatal("missing User.manager -> User self-reference edge")
	}
	if m.Properties["self_ref"] != "true" {
		t.Errorf("User.manager want self_ref=true, got %q", m.Properties["self_ref"])
	}

	// Scalar fields id/name make NO edge. NewUser input fields make NO edge.
	for i := range ents {
		for _, r := range ents[i].Relationships {
			switch r.Properties["field_name"] {
			case "id", "name":
				t.Errorf("scalar field %q must not produce an edge", r.Properties["field_name"])
			}
			if r.Properties["graphql_field"] == "NewUser.account" {
				t.Error("GraphQLInputObject field must not produce an edge")
			}
		}
	}
}

// TestJunTG_ResolverReturnType asserts a #[graphql_object] impl Query resolver's
// return type produces a Query->Type edge with the right cardinality, including
// FieldResult<T> / Vec<T> unwrapping; scalar returns make no edge.
func TestJunTG_ResolverReturnType(t *testing.T) {
	path := "query.rs"
	src := `
use juniper::*;

#[derive(GraphQLObject)]
struct User { id: i32 }

#[derive(GraphQLObject)]
struct Order { id: i32 }

struct Query;

#[graphql_object]
impl Query {
    fn user(&self, id: i32) -> FieldResult<User> {
        todo!()
    }
    fn orders(&self) -> Vec<Order> {
        todo!()
    }
    fn count(&self) -> i32 {
        0
    }
}
`
	ents := runJunTG(t, path, "rust", src)
	queryRef := junTGRef(path, "Query")

	// user(...) -> FieldResult<User> unwrapped to a to_one User edge.
	u := findJunTG(ents, queryRef, junTGRef(path, "User"), "user")
	if u == nil {
		t.Fatal("missing Query.user -> User resolver edge (FieldResult<T> unwrap)")
	}
	if u.Properties["cardinality"] != "to_one" {
		t.Errorf("Query.user want to_one, got %q", u.Properties["cardinality"])
	}

	// orders(...) -> Vec<Order> to_many edge.
	o := findJunTG(ents, queryRef, junTGRef(path, "Order"), "orders")
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

// TestJunTG_WrongLanguageNoOp (wrong-language no-op) asserts a non-rust file with
// juniper-looking content emits nothing.
func TestJunTG_WrongLanguageNoOp(t *testing.T) {
	path := "schema.go"
	src := `
#[derive(GraphQLObject)]
struct User { orders: Vec<Order> }
#[derive(GraphQLObject)]
struct Order { id: i32 }
`
	ents := runJunTG(t, path, "go", src)
	if len(ents) != 0 {
		t.Fatalf("non-rust language must emit nothing, got %d entities", len(ents))
	}
}

// TestJunTG_NoMatchPlainStruct (no-match no-op) asserts a plain Rust struct with
// no juniper derive (and no other juniper signal) produces NO node and NO edge.
func TestJunTG_NoMatchPlainStruct(t *testing.T) {
	path := "plain.rs"
	src := `
struct User {
    id: u64,
    orders: Vec<Order>,
}
struct Order { id: u64 }
`
	ents := runJunTG(t, path, "rust", src)
	if len(ents) != 0 {
		t.Fatalf("plain struct (no juniper signal) must emit nothing, got %d entities", len(ents))
	}
}

// TestJunTG_NegativeNonGraphQLImpl asserts a plain `impl` block (no
// #[graphql_object]) within an otherwise-juniper file produces NO resolver edge
// and the impl root is not a type node.
func TestJunTG_NegativeNonGraphQLImpl(t *testing.T) {
	path := "mixed.rs"
	src := `
use juniper::*;

#[derive(GraphQLObject)]
struct User { id: i32 }

struct Helper;
impl Helper {
    fn user(&self) -> User { todo!() }
}
`
	ents := runJunTG(t, path, "rust", src)
	if countJunTGNodes(ents, "Helper") != 0 {
		t.Error("non-#[graphql_object] impl Helper must not become a GraphQL type node")
	}
	if countJunTGEdges(ents) != 0 {
		t.Errorf("plain impl (no #[graphql_object]) must produce no resolver edge, got %d edges", countJunTGEdges(ents))
	}
}

// TestJunTG_CrossFileEdge (#5109 axis b) asserts a field referencing a
// capitalized object type NOT declared in the same file now emits a by-name
// cross-file GRAPH_RELATES edge (cross_file=true) that the resolver binds via
// its global by-name index to the target file's own type node.
func TestJunTG_CrossFileEdge(t *testing.T) {
	path := "partial.rs"
	src := `
use juniper::*;

#[derive(GraphQLObject)]
struct User {
    id: i32,
    external: Vec<RemoteThing>,
    lower: Vec<notatype>,
}
`
	ents := runJunTG(t, path, "rust", src)
	userRef := junTGRef(path, "User")
	// External capitalized type -> by-name cross-file edge.
	e := findJunTG(ents, userRef, "Kind:SCOPE.Schema:RemoteThing", "external")
	if e == nil {
		t.Fatal("missing cross-file User.external -> RemoteThing GRAPH_RELATES edge")
	}
	if e.Properties["cross_file"] != "true" {
		t.Errorf("cross-file edge want cross_file=true, got %q", e.Properties["cross_file"])
	}
	if e.Properties["cardinality"] != "to_many" {
		t.Errorf("cross-file edge want to_many, got %q", e.Properties["cardinality"])
	}
	// A lowercase same-file-unknown token is NOT a type -> no edge.
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Properties["field_name"] == "lower" {
				t.Error("lowercase non-type token must not produce a cross-file edge")
			}
		}
	}
	if countJunTGNodes(ents, "User") != 1 {
		t.Errorf("User node should still be emitted, got %d", countJunTGNodes(ents, "User"))
	}
}

// TestJunTG_FieldAndTypeRename (#5109 axis a) asserts #[graphql(name = "...")]
// on a type and on a field overrides the Rust ident in the recorded GraphQL
// names and in the node/edge structural identity.
func TestJunTG_FieldAndTypeRename(t *testing.T) {
	path := "rename.rs"
	src := `
use juniper::*;

#[derive(GraphQLObject)]
#[graphql(name = "Account")]
struct UserAccount {
    id: i32,
    #[graphql(name = "primaryOrders")]
    orders: Vec<Order>,
}

#[derive(GraphQLObject)]
struct Order { id: i32 }
`
	ents := runJunTG(t, path, "rust", src)
	// Type renamed UserAccount -> Account.
	if countJunTGNodes(ents, "Account") != 1 {
		t.Fatalf("renamed type node Account expected once, got %d", countJunTGNodes(ents, "Account"))
	}
	if countJunTGNodes(ents, "UserAccount") != 0 {
		t.Errorf("Rust ident UserAccount must not be the node name, got %d", countJunTGNodes(ents, "UserAccount"))
	}
	// Field renamed orders -> primaryOrders; edge keyed on GraphQL names.
	accRef := junTGRef(path, "Account")
	e := findJunTG(ents, accRef, junTGRef(path, "Order"), "primaryOrders")
	if e == nil {
		t.Fatal("missing renamed Account.primaryOrders -> Order edge")
	}
	if e.Properties["graphql_field"] != "Account.primaryOrders" {
		t.Errorf("want graphql_field=Account.primaryOrders, got %q", e.Properties["graphql_field"])
	}
	// The Rust field name must not survive as the field_name.
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Properties["graphql_field"] == "Account.orders" {
				t.Error("un-renamed Account.orders edge must not exist")
			}
		}
	}
}

// TestJunTG_Interface (#5109 axis c) asserts a #[graphql_interface(for=[...])]
// trait becomes an interface type node, implementing objects get
// relation=implements GRAPH_RELATES edges, and a field typed as the interface
// is a recognized edge target.
func TestJunTG_Interface(t *testing.T) {
	path := "iface.rs"
	src := `
use juniper::*;

#[graphql_interface(for = [Human, Droid])]
trait Character {
    fn id(&self) -> i32;
    fn friends(&self) -> Vec<Character>;
}

#[derive(GraphQLObject)]
struct Human { id: i32 }

#[derive(GraphQLObject)]
struct Droid { id: i32 }

#[derive(GraphQLObject)]
struct Crew {
    captain: Character,
}
`
	ents := runJunTG(t, path, "rust", src)
	// Interface node exists and is marked.
	if countJunTGNodes(ents, "Character") != 1 {
		t.Fatalf("interface node Character expected once, got %d", countJunTGNodes(ents, "Character"))
	}
	var ifaceMarked bool
	for _, e := range ents {
		if e.Name == "Character" && e.Properties["graphql_kind"] == "interface" {
			ifaceMarked = true
		}
	}
	if !ifaceMarked {
		t.Error("Character node must carry graphql_kind=interface")
	}
	charRef := junTGRef(path, "Character")
	// Implements edges Human->Character and Droid->Character.
	for _, obj := range []string{"Human", "Droid"} {
		found := false
		for i := range ents {
			for _, r := range ents[i].Relationships {
				if r.ToID == charRef && r.FromID == junTGRef(path, obj) &&
					r.Properties["relation"] == "implements" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("missing %s -> Character implements edge", obj)
		}
	}
	// A field typed as the interface (Crew.captain: Character) is a target.
	if findJunTG(ents, junTGRef(path, "Crew"), charRef, "captain") == nil {
		t.Error("missing Crew.captain -> Character interface-target edge")
	}
}
