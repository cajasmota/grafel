package elixir_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractFull returns full EntityRecords (with properties) so deep-grind tests
// can assert specific table / column / association / query / migration values.
func extractFull(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
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

func findRecord(ents []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func assertProp(t *testing.T, e *types.EntityRecord, key, want string) {
	t.Helper()
	if e == nil {
		t.Fatalf("entity not found while asserting %s=%s", key, want)
	}
	if got := e.Properties[key]; got != want {
		t.Errorf("%s/%s prop %q = %q, want %q", e.Kind, e.Name, key, got, want)
	}
}

// ---------------------------------------------------------------------------
// Ecto schema — fields with column names + types
// ---------------------------------------------------------------------------

func TestEctoSchemaFields(t *testing.T) {
	src := `
defmodule MyApp.User do
  use Ecto.Schema

  schema "users" do
    field :name, :string
    field :age, :integer
    field :active, :boolean, default: true
    timestamps()
  end
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("user.ex", "elixir", src))

	if findRecord(ents, "SCOPE.Schema", "users") == nil {
		t.Fatal("expected users schema entity")
	}

	name := findRecord(ents, "SCOPE.Schema", "users.name")
	assertProp(t, name, "subtype", "column")
	assertProp(t, name, "column_name", "name")
	assertProp(t, name, "field_type", "string")
	assertProp(t, name, "table_name", "users")

	age := findRecord(ents, "SCOPE.Schema", "users.age")
	assertProp(t, age, "field_type", "integer")

	active := findRecord(ents, "SCOPE.Schema", "users.active")
	assertProp(t, active, "field_type", "boolean")

	// timestamps() expands to inserted_at / updated_at columns.
	if findRecord(ents, "SCOPE.Schema", "users.inserted_at") == nil {
		t.Error("expected inserted_at column from timestamps()")
	}
	ua := findRecord(ents, "SCOPE.Schema", "users.updated_at")
	assertProp(t, ua, "field_type", "naive_datetime")
}

// ---------------------------------------------------------------------------
// Ecto associations — target schema + foreign key + join_through
// ---------------------------------------------------------------------------

func TestEctoAssociationsTargets(t *testing.T) {
	src := `
defmodule MyApp.Post do
  use Ecto.Schema

  schema "posts" do
    belongs_to :user, MyApp.User
    belongs_to :org, MyApp.Org, foreign_key: :organization_id
    has_many :comments, MyApp.Comment
    has_one :featured_image, MyApp.Image
    many_to_many :tags, MyApp.Tag, join_through: "posts_tags"
  end
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("post.ex", "elixir", src))

	bu := findRecord(ents, "SCOPE.Component", "belongs_to:user")
	assertProp(t, bu, "target_schema", "MyApp.User")
	assertProp(t, bu, "foreign_key", "user_id")
	assertProp(t, bu, "owns_fk", "true")

	bo := findRecord(ents, "SCOPE.Component", "belongs_to:org")
	assertProp(t, bo, "target_schema", "MyApp.Org")
	assertProp(t, bo, "foreign_key", "organization_id")

	hm := findRecord(ents, "SCOPE.Component", "has_many:comments")
	assertProp(t, hm, "target_schema", "MyApp.Comment")
	assertProp(t, hm, "association_type", "has_many")

	ho := findRecord(ents, "SCOPE.Component", "has_one:featured_image")
	assertProp(t, ho, "target_schema", "MyApp.Image")

	mtm := findRecord(ents, "SCOPE.Component", "many_to_many:tags")
	assertProp(t, mtm, "target_schema", "MyApp.Tag")
	assertProp(t, mtm, "join_through", "posts_tags")
}

// ---------------------------------------------------------------------------
// Ecto query DSL — queried schema + clauses
// ---------------------------------------------------------------------------

func TestEctoQueryClauses(t *testing.T) {
	src := `
def adults(repo) do
  query =
    from u in User,
      join: o in Org, on: o.id == u.org_id,
      where: u.age > 18,
      order_by: [desc: u.inserted_at],
      select: u.name

  repo.all(query)
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("queries.ex", "elixir", src))

	q := findRecord(ents, "SCOPE.Operation", "query:User[u]")
	if q == nil {
		t.Fatal("expected query:User[u] operation")
	}
	assertProp(t, q, "queried_schema", "User")
	assertProp(t, q, "source_binding", "u")
	clauses := q.Properties["query_clauses"]
	for _, want := range []string{"join", "where", "order_by", "select"} {
		if !containsSub(clauses, want) {
			t.Errorf("query_clauses %q missing %q", clauses, want)
		}
	}
}

func containsSub(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Ecto migration — columns + references FK
// ---------------------------------------------------------------------------

func TestEctoMigrationColumns(t *testing.T) {
	src := `
defmodule MyApp.Repo.Migrations.CreatePosts do
  use Ecto.Migration

  def change do
    create table(:posts) do
      add :title, :string
      add :views, :integer
      add :org_id, references(:orgs)
      timestamps()
    end
  end
end
`
	ents := extractFull(t, "custom_elixir_ecto", fi("create_posts.exs", "elixir", src))

	if findRecord(ents, "SCOPE.Schema", "migration:posts") == nil {
		t.Fatal("expected migration:posts schema entity")
	}

	title := findRecord(ents, "SCOPE.Schema", "migration:posts.title")
	assertProp(t, title, "subtype", "column")
	assertProp(t, title, "field_type", "string")
	assertProp(t, title, "pattern_type", "column")

	views := findRecord(ents, "SCOPE.Schema", "migration:posts.views")
	assertProp(t, views, "field_type", "integer")

	fk := findRecord(ents, "SCOPE.Schema", "migration:posts.org_id")
	assertProp(t, fk, "pattern_type", "foreign_key")
	assertProp(t, fk, "references_table", "orgs")
}
