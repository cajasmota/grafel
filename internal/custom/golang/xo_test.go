package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

func xoRecords(t *testing.T, name string) (sums []entitySummary, props map[string]map[string]string) {
	t.Helper()
	e, ok := extreg.Get("custom_go_xo")
	if !ok {
		t.Fatal("custom_go_xo not registered")
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
// Models (partial): generated db-tagged structs + fields.
// ---------------------------------------------------------------------------

func TestXoModelsAndFields(t *testing.T) {
	sums, props := xoRecords(t, "xo_user.go")

	if !containsEntity(sums, "SCOPE.Schema", "User") {
		t.Error("expected User model")
	}
	if !containsEntity(sums, "SCOPE.Schema", "Author") {
		t.Error("expected Author model")
	}
	for _, f := range []string{"User.ID", "User.Email", "User.AuthorID", "User.CreatedAt"} {
		nm := "field:" + f
		if !hasSubtype(sums, "SCOPE.Component", "field", nm) {
			t.Errorf("expected field %s", nm)
		}
	}
	if got := props["field:User.CreatedAt"]["column_name"]; got != "created_at" {
		t.Errorf("CreatedAt column_name = %q, want created_at (from db tag)", got)
	}
	if got := props["field:User.AuthorID"]["column_name"]; got != "author_id" {
		t.Errorf("AuthorID column_name = %q, want author_id", got)
	}
}

// ---------------------------------------------------------------------------
// Relationships (partial): generated FK accessor methods.
// ---------------------------------------------------------------------------

func TestXoForeignKeyAccessor(t *testing.T) {
	sums, props := xoRecords(t, "xo_user.go")

	if !hasSubtype(sums, "SCOPE.Component", "relation", "rel:User.Author") {
		t.Error("expected rel:User.Author FK accessor")
	}
	if got := props["rel:User.Author"]["target_model"]; got != "Author" {
		t.Errorf("rel:User.Author target_model = %q, want Author", got)
	}
	if got := props["rel:User.Author"]["relationship"]; got != "belongs_to" {
		t.Errorf("rel:User.Author relationship = %q, want belongs_to", got)
	}
}

// ---------------------------------------------------------------------------
// Queries (partial): CRUD methods + <Type>By<Index> lookup funcs.
// ---------------------------------------------------------------------------

func TestXoQueries(t *testing.T) {
	sums, props := xoRecords(t, "xo_user.go")

	for _, q := range []string{"User.Insert", "User.Update", "User.Delete"} {
		nm := "query:" + q
		if !hasSubtype(sums, "SCOPE.Operation", "query", nm) {
			t.Errorf("expected CRUD query %s", nm)
		}
	}
	if got := props["query:User.Insert"]["model_name"]; got != "User" {
		t.Errorf("query:User.Insert model_name = %q, want User", got)
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "lookup:UserByID") {
		t.Error("expected lookup:UserByID")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "lookup:UserByEmail") {
		t.Error("expected lookup:UserByEmail")
	}
	if !hasSubtype(sums, "SCOPE.Operation", "query", "lookup:AuthorByID") {
		t.Error("expected lookup:AuthorByID")
	}
}

// A db-tagged struct without the xo header must not be claimed by xo.
func TestXoRequiresGeneratedHeader(t *testing.T) {
	e, _ := extreg.Get("custom_go_xo")
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: "plain.go", Language: "go",
		Content: []byte("package p\ntype User struct {\n\tID int `db:\"id\"`\n}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities without xo header, got %d", len(ents))
	}
}

func TestXoNoMatch(t *testing.T) {
	e, _ := extreg.Get("custom_go_xo")
	ents, err := e.Extract(context.Background(), extreg.FileInput{Path: "main.go", Language: "go", Content: []byte("package main")})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
