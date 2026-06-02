package engine

import (
	"testing"
)

// TestSynth_GraphQLRuby_Query covers a graphql-ruby QueryType. Asserts the
// EXACT operation-endpoint shape shared with the JS/TS Apollo server, the
// Python Strawberry server, the Go gqlgen server and the C# HotChocolate server,
// plus the framework label and the same-file resolver-method handler
// attribution that the resolver post-pass rebinds into a HANDLES edge. #3621.
func TestSynth_GraphQLRuby_Query(t *testing.T) {
	src := `module Types
  class QueryType < Types::BaseObject
    field :users, [Types::UserType], null: false
    def users
      User.all
    end

    field :user, Types::UserType, null: true do
      argument :id, ID, required: true
    end
    def user(id:)
      User.find(id)
    end
  end
end
`
	got, res := runDetect(t, "ruby", "app/graphql/types/query_type.rb", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/users",
		"http:GRAPHQL:/graphql/Query/user",
	}
	requireContains(t, got, want, "graphql-ruby Query")

	// EXACT shape + framework + handler attribution → HANDLES edge.
	e := findSynthDef(res, "http:GRAPHQL:/graphql/Query/users")
	if e == nil {
		t.Fatalf("graphql-ruby Query: missing http:GRAPHQL:/graphql/Query/users")
	}
	if e.Properties["framework"] != "graphql-ruby" {
		t.Errorf("graphql-ruby Query: framework = %q, want graphql-ruby", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:users" {
		t.Errorf("graphql-ruby Query: source_handler = %q, want SCOPE.Operation:users",
			e.Properties["source_handler"])
	}
	if e.Properties["handler_file"] != "app/graphql/types/query_type.rb" {
		t.Errorf("graphql-ruby Query: handler_file = %q, want app/graphql/types/query_type.rb",
			e.Properties["handler_file"])
	}
	if e.Properties["verb"] != "GRAPHQL" {
		t.Errorf("graphql-ruby Query: verb = %q, want GRAPHQL", e.Properties["verb"])
	}
	if e.StartLine == 0 {
		t.Errorf("graphql-ruby Query: StartLine not stamped on users field")
	}
}

// TestSynth_GraphQLRuby_Mutation covers a graphql-ruby MutationType and asserts
// the snake_case field name maps verbatim (create_user → createUser is NOT
// applied; graphql-ruby keeps the Ruby snake_case symbol as the GraphQL field
// name on the wire). #3621.
func TestSynth_GraphQLRuby_Mutation(t *testing.T) {
	src := `module Types
  class MutationType < Types::BaseObject
    field :create_user, Types::UserType, null: false
    def create_user(name:)
      User.create(name: name)
    end

    field :delete_user, Boolean, null: false
    def delete_user(id:)
      User.find(id).destroy
    end
  end
end
`
	got, res := runDetect(t, "ruby", "app/graphql/types/mutation_type.rb", src)
	want := []string{
		"http:GRAPHQL:/graphql/Mutation/create_user",
		"http:GRAPHQL:/graphql/Mutation/delete_user",
	}
	requireContains(t, got, want, "graphql-ruby Mutation")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Mutation/create_user")
	if e == nil {
		t.Fatalf("graphql-ruby Mutation: missing http:GRAPHQL:/graphql/Mutation/create_user")
	}
	if e.Properties["framework"] != "graphql-ruby" {
		t.Errorf("graphql-ruby Mutation: framework = %q, want graphql-ruby", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:create_user" {
		t.Errorf("graphql-ruby Mutation: source_handler = %q, want SCOPE.Operation:create_user",
			e.Properties["source_handler"])
	}
}

// TestSynth_GraphQLRuby_Subscription covers a graphql-ruby SubscriptionType,
// including a namespaced class declaration (`class Types::SubscriptionType`). #3621.
func TestSynth_GraphQLRuby_Subscription(t *testing.T) {
	src := `class Types::SubscriptionType < Types::BaseObject
  field :user_added, Types::UserType, null: false
  def user_added
    # ...
  end
end
`
	got, res := runDetect(t, "ruby", "app/graphql/types/subscription_type.rb", src)
	want := []string{"http:GRAPHQL:/graphql/Subscription/user_added"}
	requireContains(t, got, want, "graphql-ruby Subscription")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Subscription/user_added")
	if e == nil {
		t.Fatalf("graphql-ruby Subscription: missing http:GRAPHQL:/graphql/Subscription/user_added")
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:user_added" {
		t.Errorf("graphql-ruby Subscription: source_handler = %q, want SCOPE.Operation:user_added",
			e.Properties["source_handler"])
	}
}

// TestSynth_GraphQLRuby_DirectSchemaObjectBase covers a root type that
// subclasses GraphQL::Schema::Object directly (no project BaseObject alias). #3621.
func TestSynth_GraphQLRuby_DirectSchemaObjectBase(t *testing.T) {
	src := `class QueryType < GraphQL::Schema::Object
  field :health, String, null: false
  def health
    "ok"
  end
end
`
	got, _ := runDetect(t, "ruby", "app/graphql/query_type.rb", src)
	requireContains(t, got, []string{"http:GRAPHQL:/graphql/Query/health"},
		"graphql-ruby direct GraphQL::Schema::Object base")
}

// TestSynth_GraphQLRuby_IgnoresNonRootTypes asserts that `field :` declarations
// on a NON-root object type (e.g. UserType) are NOT emitted as root operations.
// Only Query / Mutation / Subscription root types carry operation endpoints. #3621.
func TestSynth_GraphQLRuby_IgnoresNonRootTypes(t *testing.T) {
	src := `module Types
  class UserType < Types::BaseObject
    field :id, ID, null: false
    field :name, String, null: false
    field :friends, [Types::UserType], null: false
    def friends
      object.friends
    end
  end

  class QueryType < Types::BaseObject
    field :users, [Types::UserType], null: false
    def users
      User.all
    end
  end
end
`
	got, _ := runDetect(t, "ruby", "app/graphql/types.rb", src)
	for _, id := range got {
		if id == "http:GRAPHQL:/graphql/User/friends" ||
			id == "http:GRAPHQL:/graphql/Query/friends" ||
			id == "http:GRAPHQL:/graphql/Query/id" ||
			id == "http:GRAPHQL:/graphql/Query/name" {
			t.Errorf("graphql-ruby: non-root type field leaked as an operation endpoint, got %q", id)
		}
	}
	requireContains(t, got, []string{"http:GRAPHQL:/graphql/Query/users"},
		"graphql-ruby non-root field skip")
}

// TestSynth_GraphQLRuby_NoOpOnPlainRuby asserts the synthesizer does not fire on
// a plain Ruby / Rails routes file (no graphql-ruby signal). #3621.
func TestSynth_GraphQLRuby_NoOpOnPlainRuby(t *testing.T) {
	src := `Rails.application.routes.draw do
  resources :users
  get '/health', to: 'health#index'
end
`
	_, res := runDetect(t, "ruby", "config/routes.rb", src)
	for _, ent := range res.Entities {
		if ent.Properties["framework"] == "graphql-ruby" {
			t.Errorf("graphql-ruby synthesizer fired on plain Rails routes file, emitted: %s", ent.ID)
		}
	}
}

// TestSynth_GraphQLRuby_DedupesRepeatedField asserts a field name is emitted
// once per root type even if the regex sees an accidental repeat. #3621.
func TestSynth_GraphQLRuby_DedupesRepeatedField(t *testing.T) {
	src := `class QueryType < Types::BaseObject
  field :users, [Types::UserType], null: false
  def users
    User.all
  end
end
`
	got, _ := runDetect(t, "ruby", "app/graphql/query_type.rb", src)
	count := 0
	for _, id := range got {
		if id == "http:GRAPHQL:/graphql/Query/users" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("graphql-ruby: expected exactly 1 Query/users endpoint, got %d", count)
	}
}
