package graphql_test

// Issue #385 (PORT-RELS-GRAPHQL) — relationship-emission tests.
//
// The GraphQL extractor emits two relationship kinds:
//
//   - CONTAINS: file → each top-level schema/operation/fragment definition,
//     and type/interface/input → each declared field. CONTAINS ToIDs use the
//     canonical Format-A structural-ref via BuildOperationStructuralRef.
//
//   - IMPORTS: every `extend type Foo` and every `...FragmentName` spread
//     inside an operation/fragment body. Properties carry
//     {source_module, import_kind} matching the contract used by the
//     other ported extractors.

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/graphql"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func extractGQL(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("graphql")
	if !ok {
		t.Fatal("graphql extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "graphql",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return got
}

func gqlRelsByKind(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

func hasGQLRel(entities []types.EntityRecord, kind, toContains string) bool {
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind && strings.Contains(r.ToID, toContains) {
				return true
			}
		}
	}
	return false
}

// ---- CONTAINS: file → top-level definitions --------------------------------

func TestRelationships_Contains_FileToType(t *testing.T) {
	src := `type User {
  id: ID!
  name: String!
}
`
	entities := extractGQL(t, "schema.graphql", src)
	wantRef := extractor.BuildOperationStructuralRef("graphql", "schema.graphql", "User")
	if !hasGQLRel(entities, "CONTAINS", wantRef) {
		t.Errorf("expected CONTAINS to %q", wantRef)
	}
}

func TestRelationships_Contains_FileToMultipleDefinitions(t *testing.T) {
	src := `type User { id: ID! }
interface Node { id: ID! }
enum Status { ACTIVE INACTIVE }
input CreateUserInput { name: String! }
scalar DateTime
`
	entities := extractGQL(t, "schema.graphql", src)
	for _, name := range []string{"User", "Node", "Status", "CreateUserInput", "DateTime"} {
		ref := extractor.BuildOperationStructuralRef("graphql", "schema.graphql", name)
		if !hasGQLRel(entities, "CONTAINS", ref) {
			t.Errorf("expected file CONTAINS %q (ref %q)", name, ref)
		}
	}
}

func TestRelationships_Contains_FileToOperationsAndFragments(t *testing.T) {
	src := `query GetUser($id: ID!) { user(id: $id) { id } }
mutation CreateUser($input: CreateUserInput!) { createUser(input: $input) { id } }
subscription OnUserCreated { userCreated { id } }
fragment UserFields on User { id name }
`
	entities := extractGQL(t, "ops.graphql", src)
	for _, name := range []string{"GetUser", "CreateUser", "OnUserCreated", "UserFields"} {
		ref := extractor.BuildOperationStructuralRef("graphql", "ops.graphql", name)
		if !hasGQLRel(entities, "CONTAINS", ref) {
			t.Errorf("expected file CONTAINS %q (ref %q)", name, ref)
		}
	}
}

// ---- CONTAINS: type → field -------------------------------------------------

func TestRelationships_Contains_TypeToField(t *testing.T) {
	src := `type User {
  id: ID!
  name: String!
  email: String!
}
`
	entities := extractGQL(t, "schema.graphql", src)
	for _, fname := range []string{"id", "name", "email"} {
		ref := extractor.BuildOperationStructuralRef("graphql", "schema.graphql", "User."+fname)
		if !hasGQLRel(entities, "CONTAINS", ref) {
			t.Errorf("expected type CONTAINS field %q (ref %q)", fname, ref)
		}
	}
}

func TestRelationships_Contains_InterfaceToField(t *testing.T) {
	src := `interface Node {
  id: ID!
}
`
	entities := extractGQL(t, "schema.graphql", src)
	ref := extractor.BuildOperationStructuralRef("graphql", "schema.graphql", "Node.id")
	if !hasGQLRel(entities, "CONTAINS", ref) {
		t.Errorf("expected interface CONTAINS field id (ref %q)", ref)
	}
}

func TestRelationships_Contains_InputToField(t *testing.T) {
	src := `input CreateUserInput {
  name: String!
  email: String!
}
`
	entities := extractGQL(t, "schema.graphql", src)
	for _, fname := range []string{"name", "email"} {
		ref := extractor.BuildOperationStructuralRef("graphql", "schema.graphql", "CreateUserInput."+fname)
		if !hasGQLRel(entities, "CONTAINS", ref) {
			t.Errorf("expected input CONTAINS field %q", fname)
		}
	}
}

// ---- IMPORTS: extend type (federation) -------------------------------------

func TestRelationships_Imports_ExtendType(t *testing.T) {
	src := `extend type User @key(fields: "id") {
  id: ID! @external
  reviews: [Review!]!
}
`
	entities := extractGQL(t, "reviews.graphql", src)
	rels := gqlRelsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "User" {
		t.Errorf("IMPORTS ToID = %q, want User", rels[0].ToID)
	}
	if rels[0].FromID != "reviews.graphql" {
		t.Errorf("IMPORTS FromID = %q, want reviews.graphql", rels[0].FromID)
	}
	if rels[0].Properties["import_kind"] != "extend" {
		t.Errorf("import_kind = %q, want extend", rels[0].Properties["import_kind"])
	}
	if rels[0].Properties["language"] != "graphql" {
		t.Errorf("language = %q, want graphql", rels[0].Properties["language"])
	}
}

func TestRelationships_Imports_ExtendMultiple(t *testing.T) {
	src := `extend type User { reviews: [Review!]! }
extend type Product { reviews: [Review!]! }
`
	entities := extractGQL(t, "x.graphql", src)
	rels := gqlRelsByKind(entities, "IMPORTS")
	if len(rels) != 2 {
		t.Fatalf("IMPORTS count = %d, want 2", len(rels))
	}
	wants := map[string]bool{"User": false, "Product": false}
	for _, r := range rels {
		if _, ok := wants[r.ToID]; ok {
			wants[r.ToID] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing IMPORTS to %q", k)
		}
	}
}

// ---- IMPORTS: fragment spreads ---------------------------------------------

func TestRelationships_Imports_FragmentSpread(t *testing.T) {
	src := `query GetUser($id: ID!) {
  user(id: $id) {
    ...UserFields
  }
}
`
	entities := extractGQL(t, "ops.graphql", src)
	rels := gqlRelsByKind(entities, "IMPORTS")
	if len(rels) < 1 {
		t.Fatalf("IMPORTS count = %d, want >= 1", len(rels))
	}
	found := false
	for _, r := range rels {
		if r.ToID == "UserFields" && r.Properties["import_kind"] == "fragment_spread" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fragment_spread IMPORTS to UserFields, got %+v", rels)
	}
}

func TestRelationships_Imports_NoneWhenAbsent(t *testing.T) {
	src := `type User { id: ID! }
`
	entities := extractGQL(t, "x.graphql", src)
	rels := gqlRelsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Errorf("IMPORTS count = %d, want 0", len(rels))
	}
}

func TestRelationships_Imports_DedupesIdentical(t *testing.T) {
	src := `extend type User { a: Int }
extend type User { b: Int }
`
	entities := extractGQL(t, "x.graphql", src)
	rels := gqlRelsByKind(entities, "IMPORTS")
	count := 0
	for _, r := range rels {
		if r.ToID == "User" && r.Properties["import_kind"] == "extend" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("IMPORTS dedup count = %d, want 1", count)
	}
}

// ---- Combined --------------------------------------------------------------

func TestRelationships_Combined_BothKinds(t *testing.T) {
	src := `extend type User @key(fields: "id") {
  id: ID! @external
  reviews: [Review!]!
}

type Review {
  id: ID!
  body: String!
}

fragment ReviewFields on Review {
  id
  body
}

query GetReviews {
  reviews {
    ...ReviewFields
  }
}
`
	entities := extractGQL(t, "reviews.graphql", src)
	if len(gqlRelsByKind(entities, "CONTAINS")) < 1 {
		t.Errorf("expected >= 1 CONTAINS")
	}
	if len(gqlRelsByKind(entities, "IMPORTS")) < 1 {
		t.Errorf("expected >= 1 IMPORTS")
	}
}

// ---- Language tag ----------------------------------------------------------

func TestRelationships_LanguageTagged(t *testing.T) {
	src := `extend type User { reviews: [Review!]! }

type User {
  id: ID!
}
`
	entities := extractGQL(t, "x.graphql", src)
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Properties["language"] != "graphql" {
				t.Errorf("relationship %s→%s missing language=graphql, got %q",
					r.FromID, r.ToID, r.Properties["language"])
			}
		}
	}
}
