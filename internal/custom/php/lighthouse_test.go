package php_test

import "testing"

// lighthouse_test.go — value-asserting tests for the Lighthouse (Laravel
// GraphQL SDL) extractor. Record lang.php.framework.lighthouse.
// Issue #3556 (epic #3505).

const lighthouseSchemaSrc = `
type Query {
    users: [User!]! @paginate
    user(id: ID! @eq): User @find
    me: User @field(resolver: "App\\GraphQL\\Queries\\Me")
}

type Mutation {
    createUser(name: String!): User @create
    deleteUser(id: ID!): User @delete
}

type User {
    id: ID!
    name: String!
    posts: [Post!]! @hasMany
}
`

// TestLighthouse_ResolverFields asserts each root field carrying a Lighthouse
// server-side directive becomes an addressable GRAPHQL endpoint.
func TestLighthouse_ResolverFields(t *testing.T) {
	ents := extract(t, "custom_php_lighthouse", fi("schema.graphql", "graphql", lighthouseSchemaSrc))
	if len(ents) == 0 {
		t.Fatal("[lighthouse] expected entities, got none")
	}

	for _, want := range []string{
		"GRAPHQL /graphql/Query/users",
		"GRAPHQL /graphql/Query/user",
		"GRAPHQL /graphql/Query/me",
		"GRAPHQL /graphql/Mutation/createUser",
		"GRAPHQL /graphql/Mutation/deleteUser",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("expected operation %q", want)
		}
	}

	// User-type data fields (id, name) must NOT become endpoints; `posts` has a
	// relation directive but lives on a non-root type, so it is a DTO field, not
	// an addressable root operation.
	for _, notWant := range []string{
		"GRAPHQL /graphql/Query/id",
		"GRAPHQL /graphql/User/posts",
		"GRAPHQL /graphql/Mutation/id",
	} {
		if containsEntity(ents, "SCOPE.Operation", notWant) {
			t.Errorf("non-root / data field leaked as endpoint: %q", notWant)
		}
	}
}

// TestLighthouse_DTO asserts a non-root SDL type becomes a SCOPE.Schema DTO,
// while the Query/Mutation roots do NOT.
func TestLighthouse_DTO(t *testing.T) {
	ents := extract(t, "custom_php_lighthouse", fi("schema.graphql", "graphql", lighthouseSchemaSrc))
	if !containsEntity(ents, "SCOPE.Schema", "lighthouse_dto:User") {
		t.Error("expected lighthouse_dto:User schema DTO")
	}
	for _, notWant := range []string{"lighthouse_dto:Query", "lighthouse_dto:Mutation"} {
		if containsEntity(ents, "SCOPE.Schema", notWant) {
			t.Errorf("operation root wrongly emitted as DTO: %q", notWant)
		}
	}
}

// TestLighthouse_NoMatch verifies the extractor is a no-op on plain GraphQL SDL
// that carries no Lighthouse server-side directive.
func TestLighthouse_NoMatch(t *testing.T) {
	src := `
type Query {
    users: [User!]!
}
type User { id: ID! name: String! }
`
	ents := extract(t, "custom_php_lighthouse", fi("plain.graphql", "graphql", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on directive-free SDL, got %d", len(ents))
	}
}

// TestLighthouse_WrongLanguage verifies the language gate (graphql only).
func TestLighthouse_WrongLanguage(t *testing.T) {
	ents := extract(t, "custom_php_lighthouse", fi("schema.php", "php", lighthouseSchemaSrc))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-graphql language, got %d", len(ents))
	}
}
