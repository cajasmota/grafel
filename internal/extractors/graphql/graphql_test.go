package graphql_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/graphql"
)

func TestGraphQLExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("graphql")
	if !ok {
		t.Fatal("graphql extractor not registered")
	}
}

func TestGraphQLExtractor_TypeDefinitions(t *testing.T) {
	src := `type User {
  id: ID!
  name: String!
  email: String!
}

interface Node {
  id: ID!
}

enum Status {
  ACTIVE
  INACTIVE
}

input CreateUserInput {
  name: String!
  email: String!
}

scalar DateTime
`
	ext, _ := extractor.Get("graphql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "schema.graphql",
		Content:  []byte(src),
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subtypes := make(map[string]string)
	for _, e := range entities {
		// Only schema-definition entities are checked here; the extractor
		// also emits SCOPE.Component file/field/import stubs that carry
		// CONTAINS/IMPORTS relationships (Issue #385).
		if e.Kind != "SCOPE.Schema" {
			continue
		}
		subtypes[e.Name] = e.Subtype
	}

	expected := map[string]string{
		"User":            "type",
		"Node":            "interface",
		"Status":          "enum",
		"CreateUserInput": "input",
		"DateTime":        "scalar",
	}
	for name, subtype := range expected {
		if subtypes[name] != subtype {
			t.Errorf("entity %q: expected Subtype=%q, got %q", name, subtype, subtypes[name])
		}
	}
}

func TestGraphQLExtractor_Operations(t *testing.T) {
	src := `query GetUser($id: ID!) {
  user(id: $id) {
    id
    name
  }
}

mutation CreateUser($input: CreateUserInput!) {
  createUser(input: $input) {
    id
  }
}

subscription OnUserCreated {
  userCreated {
    id
  }
}
`
	ext, _ := extractor.Get("graphql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "operations.graphql",
		Content:  []byte(src),
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ops := make(map[string]string)
	for _, e := range entities {
		ops[e.Name] = e.Subtype
	}
	if ops["GetUser"] != "query" {
		t.Errorf("expected GetUser to be query, got %q", ops["GetUser"])
	}
	if ops["CreateUser"] != "mutation" {
		t.Errorf("expected CreateUser to be mutation, got %q", ops["CreateUser"])
	}
	if ops["OnUserCreated"] != "subscription" {
		t.Errorf("expected OnUserCreated to be subscription, got %q", ops["OnUserCreated"])
	}
}

func TestGraphQLExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("graphql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.graphql",
		Content:  []byte{},
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestGraphQLExtractor_Signatures(t *testing.T) {
	src := `type Product {
  id: ID!
  name: String!
}
`
	ext, _ := extractor.Get("graphql")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "product.graphql",
		Content:  []byte(src),
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Name == "Product" {
			if e.Signature == "" {
				t.Error("expected non-empty Signature for Product type")
			}
			return
		}
	}
	t.Error("entity 'Product' not found")
}
