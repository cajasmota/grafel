// gqlgen_typegraph_auth_4006_test.go — #4006 (epic #3872) Go GraphQL parity:
// asserts the SDL type→type graph (GRAPH_RELATES) and the @hasRole/@auth
// field-directive auth_coverage fire on gqlgen's canonical schema.graphqls.
// gqlgen is SDL-first / code-generated, so the SDL is the source of truth for
// both the type-graph and the most statically-recoverable auth signal.
package graphql

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// gqlgenSDL is a representative gqlgen schema.graphqls: code-first models are
// generated FROM this SDL, so the SDL is the source of truth for both the
// type→type graph and the @hasRole field auth directive.
const gqlgenSDL = `
directive @hasRole(role: Role!) on FIELD_DEFINITION
directive @auth on FIELD_DEFINITION

enum Role { ADMIN USER }

type User {
  id: ID!
  name: String!
  account: Account
  orders: [Order!]!
}

type Account { id: ID! }
type Order { id: ID! }

type Query {
  me: User! @auth
  adminUsers: [User!]! @hasRole(role: ADMIN)
  publicStats: Int!
}
`

// PROBE 1 (positive, type-graph): the SDL pass must emit User→Order to_many and
// User→Account to_one nullable GRAPH_RELATES edges. This already works once the
// extractor runs on the content.
func TestGqlgen_TypeGraph_4006(t *testing.T) {
	ents := extractGraphQL(gqlgenSDL, "graph/schema.graphqls")
	var userToOrder, userToAccount bool
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "GRAPH_RELATES" {
				continue
			}
			if r.Properties["graphql_field"] == "User.orders" {
				userToOrder = true
				if r.Properties["cardinality"] != "to_many" {
					t.Errorf("User.orders cardinality=%q want to_many", r.Properties["cardinality"])
				}
				if r.Properties["list"] != "true" {
					t.Errorf("User.orders list=%q want true", r.Properties["list"])
				}
			}
			if r.Properties["graphql_field"] == "User.account" {
				userToAccount = true
				if r.Properties["cardinality"] != "to_one" {
					t.Errorf("User.account cardinality=%q want to_one", r.Properties["cardinality"])
				}
				if r.Properties["nullable"] != "true" {
					t.Errorf("User.account nullable=%q want true", r.Properties["nullable"])
				}
			}
		}
	}
	if !userToOrder {
		t.Error("PROBE FAIL: missing User->Order GRAPH_RELATES to_many edge")
	}
	if !userToAccount {
		t.Error("PROBE FAIL: missing User->Account GRAPH_RELATES to_one nullable edge")
	}
}

// PROBE 2 (value-asserting, auth): @hasRole(role: ADMIN) → auth_required +
// auth_roles=ADMIN on Query.adminUsers; bare @auth → auth_required, no roles on
// Query.me; a directive-free field (Query.publicStats) → NO auth.
func TestGqlgen_DirectiveAuth_4006(t *testing.T) {
	ext := &Extractor{}
	ents, _ := ext.Extract(context.Background(), extractor.FileInput{
		Path:    "graph/schema.graphqls",
		Content: []byte(gqlgenSDL),
	})
	byName := map[string]map[string]string{}
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "field" {
			byName[e.Name] = e.Properties
		}
	}

	admin, ok := byName["Query.adminUsers"]
	if !ok {
		t.Fatal("Query.adminUsers field entity not emitted")
	}
	if admin["auth_required"] != "true" {
		t.Errorf("Query.adminUsers auth_required=%q want true", admin["auth_required"])
	}
	if admin["auth_roles"] != "ADMIN" {
		t.Errorf("Query.adminUsers auth_roles=%q want ADMIN", admin["auth_roles"])
	}
	if admin["auth_method"] != "graphql_directive" {
		t.Errorf("Query.adminUsers auth_method=%q want graphql_directive", admin["auth_method"])
	}

	me, ok := byName["Query.me"]
	if !ok {
		t.Fatal("Query.me field entity not emitted")
	}
	if me["auth_required"] != "true" {
		t.Errorf("Query.me auth_required=%q want true", me["auth_required"])
	}
	if me["auth_roles"] != "" {
		t.Errorf("Query.me auth_roles=%q want empty (bare @auth)", me["auth_roles"])
	}

	// Negative: a directive-free field carries no auth.
	if stats := byName["Query.publicStats"]; stats["auth_required"] != "" {
		t.Errorf("Query.publicStats should have no auth, got %v", stats)
	}
	// Negative: a plain object field (User.id, no directive) carries no auth.
	if id := byName["User.id"]; id["auth_required"] != "" {
		t.Errorf("User.id should have no auth, got %v", id)
	}
}

// PROBE 3 (negative, non-auth directive): @deprecated / @goField must NOT be
// mistaken for an auth directive.
func TestGqlgen_NonAuthDirective_NoAuth_4006(t *testing.T) {
	const sdl = `
type T {
  legacy: String @deprecated(reason: "use new")
  renamed: String @goField(name: "Renamed")
}
`
	ents := extractGraphQL(sdl, "graph/schema.graphqls")
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "field" {
			if e.Properties["auth_required"] != "" {
				t.Errorf("%s wrongly flagged auth from non-auth directive: %v", e.Name, e.Properties)
			}
		}
	}
}
