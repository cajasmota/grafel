package elixir_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractRaw runs the named extractor and returns the raw EntityRecords (with
// Relationships intact, which the entitySummary helper drops).
func extractRaw(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// findType returns the SCOPE.Schema/type node named `name`, or nil.
func findType(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "type" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// findGraphRelates returns the GRAPH_RELATES edge from the owner node for the
// given field_name, or nil.
func findGraphRelates(owner *types.EntityRecord, fieldName string) *types.RelationshipRecord {
	if owner == nil {
		return nil
	}
	for i := range owner.Relationships {
		r := owner.Relationships[i]
		if r.Kind == string(types.RelationshipKindGraphRelates) && r.Properties["field_name"] == fieldName {
			return &owner.Relationships[i]
		}
	}
	return nil
}

// TestAbsintheTypeGraph_ObjectFieldEdges asserts the core value: object type
// nodes plus field→type GRAPH_RELATES edges with the correct cardinality, and
// that scalar fields produce NO edge.
func TestAbsintheTypeGraph_ObjectFieldEdges(t *testing.T) {
	src := `
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  object :user do
    field :id, :id
    field :name, non_null(:string)
    field :orders, list_of(:order)
    field :account, :account
    field :manager, :user
  end

  object :order do
    field :total, :decimal
  end

  object :account do
    field :balance, :decimal
  end
end
`
	ents := extractRaw(t, "custom_elixir_absinthe_typegraph", fi("schema.ex", "elixir", src))

	user := findType(ents, "user")
	if user == nil {
		t.Fatal("expected SCOPE.Schema/type node :user")
	}
	if got := user.Properties["framework"]; got != "absinthe" {
		t.Errorf("expected framework absinthe, got %q", got)
	}
	if got := user.Properties["graphql_type"]; got != "user" {
		t.Errorf("expected graphql_type user, got %q", got)
	}
	if findType(ents, "order") == nil {
		t.Fatal("expected SCOPE.Schema/type node :order")
	}
	if findType(ents, "account") == nil {
		t.Fatal("expected SCOPE.Schema/type node :account")
	}

	// scalar fields -> NO edge.
	if e := findGraphRelates(user, "id"); e != nil {
		t.Errorf("scalar :id field must not emit an edge, got %+v", e.Properties)
	}
	if e := findGraphRelates(user, "name"); e != nil {
		t.Errorf("scalar non_null(:string) field must not emit an edge, got %+v", e.Properties)
	}

	// list_of(:order) -> to_many list edge user->order.
	orders := findGraphRelates(user, "orders")
	if orders == nil {
		t.Fatal("expected GRAPH_RELATES user.orders -> order")
	}
	if orders.ToID != extreg.BuildOperationStructuralRef("graphql", "schema.ex", "order") {
		t.Errorf("orders edge ToID = %q, want ref to :order", orders.ToID)
	}
	if orders.Properties["list"] != "true" {
		t.Errorf("orders list = %q, want true", orders.Properties["list"])
	}
	if orders.Properties["cardinality"] != "to_many" {
		t.Errorf("orders cardinality = %q, want to_many", orders.Properties["cardinality"])
	}
	if orders.Properties["self_ref"] != "false" {
		t.Errorf("orders self_ref = %q, want false", orders.Properties["self_ref"])
	}
	if orders.Properties["graphql_field"] != "user.orders" {
		t.Errorf("orders graphql_field = %q", orders.Properties["graphql_field"])
	}

	// :account -> to_one nullable edge.
	account := findGraphRelates(user, "account")
	if account == nil {
		t.Fatal("expected GRAPH_RELATES user.account -> account")
	}
	if account.Properties["list"] != "false" {
		t.Errorf("account list = %q, want false", account.Properties["list"])
	}
	if account.Properties["cardinality"] != "to_one" {
		t.Errorf("account cardinality = %q, want to_one", account.Properties["cardinality"])
	}
	if account.Properties["nullable"] != "true" {
		t.Errorf("account nullable = %q, want true (bare Absinthe field)", account.Properties["nullable"])
	}

	// self-ref :user -> :user.
	manager := findGraphRelates(user, "manager")
	if manager == nil {
		t.Fatal("expected GRAPH_RELATES user.manager -> user (self-ref)")
	}
	if manager.Properties["self_ref"] != "true" {
		t.Errorf("manager self_ref = %q, want true", manager.Properties["self_ref"])
	}
}

// TestAbsintheTypeGraph_NonNullNullability asserts non_null/1 wrappers flip the
// nullable flag (and item_nullable inside lists) per Absinthe semantics.
func TestAbsintheTypeGraph_NonNullNullability(t *testing.T) {
	src := `
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  object :post do
    field :author, non_null(:user)
    field :tags, list_of(non_null(:tag))
    field :reviewers, non_null(list_of(:user))
  end

  object :user do
    field :id, :id
  end

  object :tag do
    field :name, :string
  end
end
`
	ents := extractRaw(t, "custom_elixir_absinthe_typegraph", fi("schema.ex", "elixir", src))
	post := findType(ents, "post")
	if post == nil {
		t.Fatal("expected :post type node")
	}

	// non_null(:user) -> nullable=false, to_one.
	author := findGraphRelates(post, "author")
	if author == nil {
		t.Fatal("expected GRAPH_RELATES post.author -> user")
	}
	if author.Properties["nullable"] != "false" {
		t.Errorf("author nullable = %q, want false", author.Properties["nullable"])
	}
	if author.Properties["list"] != "false" {
		t.Errorf("author list = %q, want false", author.Properties["list"])
	}

	// list_of(non_null(:tag)) -> list=true, item_nullable=false, list itself nullable.
	tags := findGraphRelates(post, "tags")
	if tags == nil {
		t.Fatal("expected GRAPH_RELATES post.tags -> tag")
	}
	if tags.Properties["list"] != "true" {
		t.Errorf("tags list = %q, want true", tags.Properties["list"])
	}
	if tags.Properties["item_nullable"] != "false" {
		t.Errorf("tags item_nullable = %q, want false", tags.Properties["item_nullable"])
	}
	if tags.Properties["nullable"] != "true" {
		t.Errorf("tags nullable = %q, want true (bare list_of is nullable)", tags.Properties["nullable"])
	}

	// non_null(list_of(:user)) -> list=true, nullable=false (the list), item nullable.
	reviewers := findGraphRelates(post, "reviewers")
	if reviewers == nil {
		t.Fatal("expected GRAPH_RELATES post.reviewers -> user")
	}
	if reviewers.Properties["list"] != "true" {
		t.Errorf("reviewers list = %q, want true", reviewers.Properties["list"])
	}
	if reviewers.Properties["nullable"] != "false" {
		t.Errorf("reviewers nullable = %q, want false (non_null list)", reviewers.Properties["nullable"])
	}
	if reviewers.Properties["item_nullable"] != "true" {
		t.Errorf("reviewers item_nullable = %q, want true", reviewers.Properties["item_nullable"])
	}
}

// TestAbsintheTypeGraph_RootAndUnknownTargets asserts operation roots own
// resolver-field edges to declared types, an interface/union node is emitted and
// is a valid field target, and a field referencing an UNKNOWN (cross-file /
// scalar) atom produces no edge.
func TestAbsintheTypeGraph_RootAndUnknownTargets(t *testing.T) {
	src := `
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  query do
    field :current_user, :user
    field :external, :remote_thing
  end

  interface :node do
    field :id, non_null(:id)
  end

  union :search_result do
    types [:user]
  end

  object :user do
    field :id, :id
    field :kind, :search_result
  end
end
`
	ents := extractRaw(t, "custom_elixir_absinthe_typegraph", fi("schema.ex", "elixir", src))

	// interface + union emit type nodes.
	if findType(ents, "node") == nil {
		t.Error("expected interface :node type node")
	}
	if findType(ents, "search_result") == nil {
		t.Error("expected union :search_result type node")
	}

	// query root owns a resolver-field edge to a declared type.
	root := findType(ents, "query")
	if root == nil {
		t.Fatal("expected query root node")
	}
	cu := findGraphRelates(root, "current_user")
	if cu == nil {
		t.Fatal("expected GRAPH_RELATES query.current_user -> user")
	}
	if cu.ToID != extreg.BuildOperationStructuralRef("graphql", "schema.ex", "user") {
		t.Errorf("current_user ToID = %q", cu.ToID)
	}
	// :remote_thing is not declared in this file -> no edge.
	if e := findGraphRelates(root, "external"); e != nil {
		t.Errorf("field to unknown :remote_thing must not emit an edge, got %+v", e.Properties)
	}

	// union is a valid field target.
	user := findType(ents, "user")
	if findGraphRelates(user, "kind") == nil {
		t.Error("expected GRAPH_RELATES user.kind -> search_result (union is a target)")
	}
}

// TestAbsintheTypeGraph_InputAndEnumNotOwners asserts input_object and enum are
// neither type nodes nor edge targets in the output type graph.
func TestAbsintheTypeGraph_InputAndEnumNotOwners(t *testing.T) {
	src := `
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  input_object :user_filter do
    field :name, :string
  end

  enum :role do
    value :admin
    value :user
  end

  object :user do
    field :role, :role
    field :filter, :user_filter
  end
end
`
	ents := extractRaw(t, "custom_elixir_absinthe_typegraph", fi("schema.ex", "elixir", src))

	if findType(ents, "user_filter") != nil {
		t.Error("input_object :user_filter must NOT be a type node")
	}
	if findType(ents, "role") != nil {
		t.Error("enum :role must NOT be a type node")
	}
	user := findType(ents, "user")
	if user == nil {
		t.Fatal("expected :user type node")
	}
	// field referencing an enum or an input_object -> no edge (not output objects).
	if e := findGraphRelates(user, "role"); e != nil {
		t.Errorf("field to enum :role must not emit an edge, got %+v", e.Properties)
	}
	if e := findGraphRelates(user, "filter"); e != nil {
		t.Errorf("field to input_object :user_filter must not emit an edge, got %+v", e.Properties)
	}
}

// TestAbsintheTypeGraph_NoSignalNoOutput asserts a non-Absinthe / non-schema
// file emits nothing.
func TestAbsintheTypeGraph_NoSignalNoOutput(t *testing.T) {
	src := `
defmodule MyApp.Plain do
  def hello, do: :world
end
`
	ents := extractRaw(t, "custom_elixir_absinthe_typegraph", fi("plain.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-Absinthe file, got %d", len(ents))
	}
}
