package engine

import (
	"testing"
)

// TestSynth_Gqlgen_Query covers a gqlgen schema.resolvers.go Query resolver.
// Asserts the EXACT operation-endpoint shape shared with the JS/TS GraphQL
// server (synthesizeGraphQLResolvers) and the Python Strawberry server, plus
// the framework label and the resolver-method handler attribution that the
// Phase-2 resolver rebinds into a HANDLES edge. #3613.
func TestSynth_Gqlgen_Query(t *testing.T) {
	src := `package graph

import "context"

// github.com/99designs/gqlgen generated resolver glue.

func (r *queryResolver) Users(ctx context.Context) ([]*User, error) {
	return nil, nil
}

func (r *queryResolver) User(ctx context.Context, id string) (*User, error) {
	return nil, nil
}
`
	got, res := runDetect(t, "go", "graph/schema.resolvers.go", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/users",
		"http:GRAPHQL:/graphql/Query/user",
	}
	requireContains(t, got, want, "gqlgen Query")

	// EXACT shape + framework + handler attribution → HANDLES edge.
	e := findSynthDef(res, "http:GRAPHQL:/graphql/Query/users")
	if e == nil {
		t.Fatalf("gqlgen Query: missing http:GRAPHQL:/graphql/Query/users")
	}
	if e.Properties["framework"] != "gqlgen" {
		t.Errorf("gqlgen Query: framework = %q, want gqlgen", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:queryResolver.Users" {
		t.Errorf("gqlgen Query: source_handler = %q, want SCOPE.Operation:queryResolver.Users",
			e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("gqlgen Query: StartLine not stamped on Users resolver")
	}
}

// TestSynth_Gqlgen_Mutation covers a gqlgen Mutation resolver and asserts the
// CreateUser → createUser field-name mapping (gqlgen lower-cases the leading
// capital). #3613.
func TestSynth_Gqlgen_Mutation(t *testing.T) {
	src := `package graph

import "context"

func (r *mutationResolver) CreateUser(ctx context.Context, input NewUser) (*User, error) {
	return nil, nil
}

func (r *mutationResolver) DeleteUser(ctx context.Context, id string) (bool, error) {
	return true, nil
}
`
	got, res := runDetect(t, "go", "graph/schema.resolvers.go", src)
	want := []string{
		"http:GRAPHQL:/graphql/Mutation/createUser",
		"http:GRAPHQL:/graphql/Mutation/deleteUser",
	}
	requireContains(t, got, want, "gqlgen Mutation")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Mutation/createUser")
	if e == nil {
		t.Fatalf("gqlgen Mutation: missing http:GRAPHQL:/graphql/Mutation/createUser")
	}
	if e.Properties["framework"] != "gqlgen" {
		t.Errorf("gqlgen Mutation: framework = %q, want gqlgen", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:mutationResolver.CreateUser" {
		t.Errorf("gqlgen Mutation: source_handler = %q, want SCOPE.Operation:mutationResolver.CreateUser",
			e.Properties["source_handler"])
	}
}

// TestSynth_Gqlgen_Subscription covers a gqlgen Subscription resolver returning
// a channel. #3613.
func TestSynth_Gqlgen_Subscription(t *testing.T) {
	src := `package graph

import "context"

func (r *subscriptionResolver) UserAdded(ctx context.Context) (<-chan *User, error) {
	return nil, nil
}
`
	got, res := runDetect(t, "go", "graph/schema.resolvers.go", src)
	want := []string{"http:GRAPHQL:/graphql/Subscription/userAdded"}
	requireContains(t, got, want, "gqlgen Subscription")

	e := findSynthDef(res, "http:GRAPHQL:/graphql/Subscription/userAdded")
	if e == nil {
		t.Fatalf("gqlgen Subscription: missing http:GRAPHQL:/graphql/Subscription/userAdded")
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:subscriptionResolver.UserAdded" {
		t.Errorf("gqlgen Subscription: source_handler = %q, want SCOPE.Operation:subscriptionResolver.UserAdded",
			e.Properties["source_handler"])
	}
}

// TestSynth_Gqlgen_InitialismFieldName asserts gqlgen's initialism name-mapping:
// a leading run of capitals where the last begins a new word keeps that capital
// (HTTPServer → httpServer), and an all-caps identifier lower-cases fully
// (URL → url). #3613.
func TestSynth_Gqlgen_InitialismFieldName(t *testing.T) {
	src := `package graph

import "context"

func (r *queryResolver) HTTPServer(ctx context.Context) (string, error) {
	return "", nil
}

func (r *queryResolver) URL(ctx context.Context) (string, error) {
	return "", nil
}
`
	got, _ := runDetect(t, "go", "graph/schema.resolvers.go", src)
	want := []string{
		"http:GRAPHQL:/graphql/Query/httpServer",
		"http:GRAPHQL:/graphql/Query/url",
	}
	requireContains(t, got, want, "gqlgen initialism field name")
}

// TestSynth_Gqlgen_NoOpOnPlainGo asserts the gqlgen synthesizer does not fire on
// a plain net/http Go file (no gqlgen signal). #3613.
func TestSynth_Gqlgen_NoOpOnPlainGo(t *testing.T) {
	src := `package main

import "net/http"

func main() {
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {})
	http.ListenAndServe(":8080", nil)
}
`
	_, res := runDetect(t, "go", "main.go", src)
	for _, ent := range res.Entities {
		if ent.Properties["framework"] == "gqlgen" {
			t.Errorf("gqlgen synthesizer fired on plain Go file, emitted: %s", ent.ID)
		}
	}
}

// TestSynth_Gqlgen_IgnoresFieldResolvers asserts that resolver methods on
// per-type field-resolver receivers (e.g. *userResolver) are NOT mapped to root
// operation endpoints — only the three GraphQL root types are operations. #3613.
func TestSynth_Gqlgen_IgnoresFieldResolvers(t *testing.T) {
	src := `package graph

import "context"

// Field resolver for User.friends — NOT a root operation.
func (r *userResolver) Friends(ctx context.Context, obj *User) ([]*User, error) {
	return nil, nil
}

func (r *queryResolver) Users(ctx context.Context) ([]*User, error) {
	return nil, nil
}
`
	got, _ := runDetect(t, "go", "graph/schema.resolvers.go", src)
	for _, id := range got {
		if id == "http:GRAPHQL:/graphql/Query/friends" ||
			id == "http:GRAPHQL:/graphql/User/friends" {
			t.Errorf("gqlgen: field resolver should not be emitted as a root operation, got %q", id)
		}
	}
	requireContains(t, got, []string{"http:GRAPHQL:/graphql/Query/users"}, "gqlgen field-resolver skip")
}
