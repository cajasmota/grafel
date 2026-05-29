package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

func genRecords(t *testing.T, name string) (sums []entitySummary, props map[string]map[string]string) {
	t.Helper()
	e, ok := extreg.Get("custom_go_gen")
	if !ok {
		t.Fatal("custom_go_gen not registered")
	}
	ents, err := e.Extract(context.Background(), fixtureInput(t, name, "go"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	props = map[string]map[string]string{}
	for _, ent := range ents {
		sums = append(sums, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
		props[ent.Name] = ent.Properties
	}
	return sums, props
}

// ---------------------------------------------------------------------------
// Models / config (partial): generator program + ApplyBasic model wiring.
// ---------------------------------------------------------------------------

func TestGenGeneratorAndModels(t *testing.T) {
	sums, props := genRecords(t, "gen_generator.go")

	if !hasSubtype(sums, "SCOPE.Schema", "config", "gen_generator") {
		t.Error("expected gen_generator config entity")
	}
	for _, m := range []string{"User", "Post"} {
		nm := "model:" + m
		if !containsEntity(sums, "SCOPE.Schema", nm) {
			t.Errorf("expected applied model %s", nm)
		}
		if props[nm]["model_name"] != m {
			t.Errorf("%s model_name = %q, want %q", nm, props[nm]["model_name"], m)
		}
	}
	// Introspection-driven model generation.
	if !hasSubtype(sums, "SCOPE.Schema", "config", "gen_introspect:GenerateModel") {
		t.Error("expected gen_introspect:GenerateModel")
	}
	if !hasSubtype(sums, "SCOPE.Schema", "config", "gen_introspect:GenerateModelAs") {
		t.Error("expected gen_introspect:GenerateModelAs")
	}
}

// ---------------------------------------------------------------------------
// Queries (full): typed q.<Model>.<Op> attributed to the model.
// ---------------------------------------------------------------------------

func TestGenTypedQueries(t *testing.T) {
	sums, props := genRecords(t, "gen_queries.go")

	if !hasSubtype(sums, "SCOPE.Schema", "query_api", "gen_query_api") {
		t.Error("expected gen_query_api (generated header)")
	}
	// Only the head of a chain is `q.<Model>.<Op>`; chainers after the first
	// operate on the returned builder, so they are not model-prefixed. We assert
	// the head ops + the standalone calls that are genuinely attributable.
	for _, q := range []string{"User.Where", "User.Count", "Post.Where", "Post.Delete"} {
		nm := "query:" + q
		if !hasSubtype(sums, "SCOPE.Operation", "query", nm) {
			t.Errorf("expected typed query %s", nm)
		}
	}
	if got := props["query:User.Where"]["model_name"]; got != "User" {
		t.Errorf("query:User.Where model_name = %q, want User", got)
	}
	// Field-level predicate helpers q.User.Active.Eq(...).
	if !hasSubtype(sums, "SCOPE.Operation", "query", "predicate:User.Active.Eq") {
		t.Error("expected predicate:User.Active.Eq")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "predicate:Post.Title.Like") {
		t.Error("expected predicate:Post.Title.Like")
	}
}

func TestGenNoMatch(t *testing.T) {
	e, _ := extreg.Get("custom_go_gen")
	ents, err := e.Extract(context.Background(), extreg.FileInput{Path: "main.go", Language: "go", Content: []byte("package main")})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
