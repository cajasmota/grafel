package php_test

import "testing"

// graphql_test.go — value-asserting tests for the webonyx/graphql-php
// extractor. Record lang.php.framework.graphql-php; cells endpoint_synthesis /
// handler_attribution / route_extraction / dto_extraction → full.
// Issue #3544 (epic #3505).

const gqlpSchemaSrc = `<?php
use GraphQL\Type\Definition\ObjectType;
use GraphQL\Type\Definition\Type;
use GraphQL\Type\Schema;

$userType = new ObjectType([
    'name' => 'User',
    'fields' => [
        'id'   => Type::nonNull(Type::id()),
        'name' => Type::string(),
    ],
]);

$queryType = new ObjectType([
    'name' => 'Query',
    'fields' => [
        'user' => [
            'type' => Type::nonNull($userType),
            'args' => [
                'id' => Type::nonNull(Type::id()),
            ],
            'resolve' => fn ($root, $args, $ctx) => $repo->find($args['id']),
        ],
        'users' => [
            'type' => Type::listOf($userType),
            'resolve' => function ($root, $args) {
                return $repo->all();
            },
        ],
    ],
]);

$mutationType = new ObjectType([
    'name' => 'Mutation',
    'fields' => [
        'createUser' => [
            'type' => Type::nonNull($userType),
            'resolve' => fn ($root, $args) => $repo->save($args),
        ],
    ],
]);

$schema = new Schema([
    'query'    => $queryType,
    'mutation' => $mutationType,
]);
`

// TestGraphQLPHP_ResolverFields asserts each field of the Query/Mutation
// ObjectType becomes an addressable GRAPHQL endpoint /graphql/<Type>/<field>.
func TestGraphQLPHP_ResolverFields(t *testing.T) {
	ents := extract(t, "custom_php_graphql_php", fi("schema.php", "php", gqlpSchemaSrc))
	if len(ents) == 0 {
		t.Fatal("[graphql-php] expected entities, got none")
	}

	for _, want := range []string{
		"GRAPHQL /graphql/Query/user",
		"GRAPHQL /graphql/Query/users",
		"GRAPHQL /graphql/Mutation/createUser",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("expected operation %q", want)
		}
	}

	// `args`, `type`, `id`, `name` are nested config keys, NOT field names —
	// they must not leak out as endpoints.
	for _, notWant := range []string{
		"GRAPHQL /graphql/Query/type",
		"GRAPHQL /graphql/Query/args",
		"GRAPHQL /graphql/Query/resolve",
		"GRAPHQL /graphql/Query/id",
	} {
		if containsEntity(ents, "SCOPE.Operation", notWant) {
			t.Errorf("nested config key leaked as endpoint: %q", notWant)
		}
	}
}

// TestGraphQLPHP_DTO asserts an ordinary (non-root) ObjectType becomes a
// SCOPE.Schema DTO, while the Query/Mutation roots do NOT become DTOs.
func TestGraphQLPHP_DTO(t *testing.T) {
	ents := extract(t, "custom_php_graphql_php", fi("schema.php", "php", gqlpSchemaSrc))

	if !containsEntity(ents, "SCOPE.Schema", "graphql_dto:User") {
		t.Error("expected graphql_dto:User schema DTO")
	}
	for _, notWant := range []string{"graphql_dto:Query", "graphql_dto:Mutation"} {
		if containsEntity(ents, "SCOPE.Schema", notWant) {
			t.Errorf("operation root wrongly emitted as DTO: %q", notWant)
		}
	}
}

// TestGraphQLPHP_Schema asserts new Schema([...]) yields a SCOPE.Service root.
func TestGraphQLPHP_Schema(t *testing.T) {
	ents := extract(t, "custom_php_graphql_php", fi("schema.php", "php", gqlpSchemaSrc))
	if !containsEntity(ents, "SCOPE.Service", "graphql_schema:schema.php") {
		t.Error("expected graphql_schema:schema.php service root")
	}
}

// TestGraphQLPHP_ArraySyntax verifies the legacy `array( ... )` literal form is
// handled identically to short `[ ... ]` arrays.
func TestGraphQLPHP_ArraySyntax(t *testing.T) {
	src := `<?php
$queryType = new ObjectType(array(
    'name' => 'Query',
    'fields' => array(
        'ping' => array(
            'type' => Type::string(),
            'resolve' => function () { return 'pong'; },
        ),
    ),
));
`
	ents := extract(t, "custom_php_graphql_php", fi("legacy.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GRAPHQL /graphql/Query/ping") {
		t.Error("expected GRAPHQL /graphql/Query/ping from array() syntax")
	}
}

// TestGraphQLPHP_NoMatch verifies the extractor is a no-op on plain PHP.
func TestGraphQLPHP_NoMatch(t *testing.T) {
	src := `<?php class Foo { public function bar() { return 1; } }`
	ents := extract(t, "custom_php_graphql_php", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on plain PHP, got %d", len(ents))
	}
}

// TestGraphQLPHP_WrongLanguage verifies the language gate.
func TestGraphQLPHP_WrongLanguage(t *testing.T) {
	ents := extract(t, "custom_php_graphql_php", fi("schema.js", "javascript", gqlpSchemaSrc))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-php language, got %d", len(ents))
	}
}
