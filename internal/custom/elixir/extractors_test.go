package elixir_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/elixir"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Phoenix
// ---------------------------------------------------------------------------

func TestPhoenixRoute(t *testing.T) {
	src := `
scope "/api", MyAppWeb do
  get "/users", UserController, :index
  post "/users", UserController, :create
  delete "/users/:id", UserController, :delete
end
`
	ents := extract(t, "custom_elixir_phoenix", fi("router.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestPhoenixResources(t *testing.T) {
	src := `
scope "/", MyAppWeb do
  resources "/articles", ArticleController
end
`
	ents := extract(t, "custom_elixir_phoenix", fi("router.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /articles") {
		t.Error("expected GET /articles from resources")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /articles") {
		t.Error("expected POST /articles from resources")
	}
}

func TestPhoenixLiveView(t *testing.T) {
	// Phoenix LiveView extractor looks for "use Phoenix.LiveView"
	src := `
defmodule MyAppWeb.CounterLive do
  use Phoenix.LiveView

  def mount(_params, _session, socket), do: {:ok, socket}
  def handle_event("increment", _, socket), do: {:noreply, socket}
end
`
	ents := extract(t, "custom_elixir_phoenix", fi("counter_live.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "MyAppWeb.CounterLive") {
		t.Error("expected CounterLive UIComponent")
	}
	if !containsEntity(ents, "SCOPE.Operation", "handle_event") {
		t.Error("expected handle_event operation")
	}
}

func TestPhoenixPipeline(t *testing.T) {
	src := `
pipeline :api do
  plug :accepts, ["json"]
  plug :fetch_session
end
`
	ents := extract(t, "custom_elixir_phoenix", fi("router.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Pattern", "pipeline:api") {
		t.Error("expected api pipeline pattern")
	}
}

func TestPhoenixNoMatch(t *testing.T) {
	src := `defmodule MyApp.Helper do\n  def greet, do: "hello"\nend`
	ents := extract(t, "custom_elixir_phoenix", fi("helper.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Ecto
// ---------------------------------------------------------------------------

func TestEctoSchema(t *testing.T) {
	src := `
defmodule MyApp.Post do
  use Ecto.Schema

  schema "posts" do
    field :title, :string
    field :body, :text
    belongs_to :user, MyApp.User
    has_many :comments, MyApp.Comment
  end
end
`
	ents := extract(t, "custom_elixir_ecto", fi("post.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Schema", "posts") {
		t.Error("expected posts schema")
	}
	if !containsEntity(ents, "SCOPE.Component", "belongs_to:user") {
		t.Error("expected belongs_to:user component")
	}
	if !containsEntity(ents, "SCOPE.Component", "has_many:comments") {
		t.Error("expected has_many:comments component")
	}
}

func TestEctoChangeset(t *testing.T) {
	src := `
defmodule MyApp.Post do
  def changeset(post, attrs) do
    post |> cast(attrs, [:title, :body]) |> validate_required([:title])
  end
end
`
	ents := extract(t, "custom_elixir_ecto", fi("post.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "changeset") {
		t.Error("expected changeset operation")
	}
}

func TestEctoRepo(t *testing.T) {
	src := `
defmodule MyApp.Repo do
  use Ecto.Repo, otp_app: :my_app, adapter: Ecto.Adapters.Postgres
end
`
	ents := extract(t, "custom_elixir_ecto", fi("repo.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Service", "MyApp.Repo") {
		t.Error("expected MyApp.Repo SCOPE.Service")
	}
}

func TestEctoRepoQuery(t *testing.T) {
	src := `
Repo.get(Post, id)
Repo.all(Post)
Repo.insert(%Post{title: "Hello"})
`
	ents := extract(t, "custom_elixir_ecto", fi("controller.ex", "elixir", src))
	if !containsEntity(ents, "SCOPE.Operation", "Repo.get") {
		t.Error("expected Repo.get operation")
	}
	if !containsEntity(ents, "SCOPE.Operation", "Repo.insert") {
		t.Error("expected Repo.insert operation")
	}
}

func TestEctoNoMatch(t *testing.T) {
	src := `defmodule MyApp.Helper do\n  def add(a, b), do: a + b\nend`
	ents := extract(t, "custom_elixir_ecto", fi("helper.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
