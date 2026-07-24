package python_test

// graphql_codefirst_typegraph_test.go — value-asserting tests for the
// python_graphql_codefirst_typegraph extractor (epic #3628, completes #3804).
//
// Asserts the GRAPH_RELATES object-type→type edges for the two Python code-first
// GraphQL frameworks (Strawberry / Graphene) carry the exact FromID/ToID
// structural refs + cardinality the SDL pass (#3805) emits, that the owning type
// node is reused (no duplicate), and that scalar / unresolved fields make NO edge.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/grafel/internal/custom/python"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func runGqlCFPy(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get("python_graphql_codefirst_typegraph")
	if !ok {
		t.Fatal("python_graphql_codefirst_typegraph not registered")
	}
	ents, err := e.Extract(context.Background(),
		extractor.FileInput{Path: path, Language: "python", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func refPy(path, name string) string {
	return "scope:operation:method:graphql:" + path + ":" + name
}

func findGR(ents []types.EntityRecord, fromID, toID, field string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) &&
				r.FromID == fromID && r.ToID == toID && r.Properties.Get("field_name") == field {
				return r
			}
		}
	}
	return nil
}

func countTypeNodesPy(ents []types.EntityRecord, name string) int {
	n := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "type" && e.Name == name {
			n++
		}
	}
	return n
}

// --- Strawberry ---

func TestGqlCFPy_Strawberry_ListAndToOne(t *testing.T) {
	path := "schema.py"
	src := `
import strawberry

@strawberry.type
class Order:
    id: int

@strawberry.type
class User:
    name: str
    orders: list["Order"]
    account: "Account"
`
	src += "\n@strawberry.type\nclass Account:\n    id: int\n"
	ents := runGqlCFPy(t, path, src)

	// User → Order list edge.
	e := findGR(ents, refPy(path, "User"), refPy(path, "Order"), "orders")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES User→Order for orders")
	}
	if e.Properties.Get("list") != "true" || e.Properties.Get("cardinality") != "to_many" {
		t.Errorf("orders edge: want list=true to_many, got %v", e.Properties)
	}

	// User → Account to_one edge (forward ref).
	a := findGR(ents, refPy(path, "User"), refPy(path, "Account"), "account")
	if a == nil {
		t.Fatal("expected GRAPH_RELATES User→Account for account")
	}
	if a.Properties.Get("cardinality") != "to_one" || a.Properties.Get("list") != "false" {
		t.Errorf("account edge: want to_one list=false, got %v", a.Properties)
	}

	// scalar field name: str → NO edge.
	for i := range ents {
		for j := range ents[i].Relationships {
			if ents[i].Relationships[j].Properties.Get("field_name") == "name" {
				t.Error("scalar `name` must not produce an edge")
			}
		}
	}

	// Convergence: one User node, one Order node.
	if c := countTypeNodesPy(ents, "User"); c != 1 {
		t.Errorf("expected 1 User node, got %d", c)
	}
	if c := countTypeNodesPy(ents, "Order"); c != 1 {
		t.Errorf("expected 1 Order node, got %d", c)
	}
}

func TestGqlCFPy_Strawberry_UnresolvedNoEdge(t *testing.T) {
	path := "s.py"
	src := `
import strawberry
@strawberry.type
class User:
    home: "Address"   # Address declared elsewhere
`
	ents := runGqlCFPy(t, path, src)
	if findGR(ents, refPy(path, "User"), refPy(path, "Address"), "home") != nil {
		t.Error("unresolved cross-file target must not produce an edge")
	}
}

// --- Graphene ---

func TestGqlCFPy_Graphene_ListLambdaAndField(t *testing.T) {
	path := "g.py"
	src := `
import graphene

class Order(graphene.ObjectType):
    id = graphene.ID()

class User(graphene.ObjectType):
    name = graphene.String()
    orders = graphene.List(lambda: Order)
    account = graphene.Field(Account)

class Account(graphene.ObjectType):
    id = graphene.ID()
`
	ents := runGqlCFPy(t, path, src)

	e := findGR(ents, refPy(path, "User"), refPy(path, "Order"), "orders")
	if e == nil {
		t.Fatal("expected GRAPH_RELATES User→Order for orders (graphene List(lambda: Order))")
	}
	if e.Properties.Get("list") != "true" || e.Properties.Get("cardinality") != "to_many" {
		t.Errorf("orders edge: want list=true to_many, got %v", e.Properties)
	}

	a := findGR(ents, refPy(path, "User"), refPy(path, "Account"), "account")
	if a == nil {
		t.Fatal("expected GRAPH_RELATES User→Account for account (graphene Field)")
	}
	if a.Properties.Get("cardinality") != "to_one" {
		t.Errorf("account edge: want to_one, got %v", a.Properties)
	}

	// scalar graphene.String() → no edge
	for i := range ents {
		for j := range ents[i].Relationships {
			if ents[i].Relationships[j].Properties.Get("field_name") == "name" {
				t.Error("graphene scalar `name` must not produce an edge")
			}
		}
	}
}

// Graphene root Query/Mutation are NOT owners (operation roots, not data types).
func TestGqlCFPy_Graphene_RootTypeNotOwner(t *testing.T) {
	path := "q.py"
	src := `
import graphene

class User(graphene.ObjectType):
    id = graphene.ID()

class Query(graphene.ObjectType):
    user = graphene.Field(User)
`
	ents := runGqlCFPy(t, path, src)
	// Query is excluded as a type node and as an edge owner.
	if countTypeNodesPy(ents, "Query") != 0 {
		t.Error("Query root must not be emitted as a data type node")
	}
	if findGR(ents, refPy(path, "Query"), refPy(path, "User"), "user") != nil {
		t.Error("Query root must not own a GRAPH_RELATES data edge")
	}
}

func TestGqlCFPy_NoMarkers_NoOp(t *testing.T) {
	ents := runGqlCFPy(t, "plain.py", "class Foo:\n    bar: int\n")
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
