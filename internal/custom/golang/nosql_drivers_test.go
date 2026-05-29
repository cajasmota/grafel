package golang_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// ---------------------------------------------------------------------------
// gocql (Cassandra): CQL CREATE TABLE schema + columns + Query() verbs.
// ---------------------------------------------------------------------------

func TestGocqlSchemaAndQueries(t *testing.T) {
	ents := extractWith(t, "custom_go_gocql", fixtureInput(t, "gocql_store.go", "go"))

	// Schema: CREATE TABLE literals => tables (keyspace prefix stripped).
	if !containsEntity(ents, "SCOPE.Schema", "table:users") {
		t.Error("expected users table schema from CREATE TABLE")
	}
	if !containsEntity(ents, "SCOPE.Schema", "table:orders") {
		t.Error("expected orders table schema from CREATE TABLE")
	}

	// Columns enumerated as fields; PRIMARY KEY clause not a field.
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:users.name") {
		t.Error("expected users.name field component")
	}
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:users.email") {
		t.Error("expected users.email field component")
	}
	if hasSubtype(ents, "SCOPE.Component", "field", "field:users.PRIMARY") {
		t.Error("did not expect a PRIMARY KEY pseudo-field")
	}

	// Queries: at least one CQL query operation.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one gocql query operation")
	}
}

func TestGocqlImportGate(t *testing.T) {
	src := `package x

const ddl = "CREATE TABLE users (id uuid, PRIMARY KEY (id))"
`
	file := extreg.FileInput{Path: "no_gocql.go", Language: "go", Content: []byte(src)}
	ents := extractWith(t, "custom_go_gocql", file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without gocql import, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// dynamodb: dynamodbav item structs + TableName tables + client verbs.
// ---------------------------------------------------------------------------

func TestDynamoModelsTablesAndQueries(t *testing.T) {
	ents := extractWith(t, "custom_go_dynamodb", fixtureInput(t, "dynamodb_store.go", "go"))

	// Models: structs with dynamodbav tags are item shapes.
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User item schema from dynamodbav tags")
	}
	if !containsEntity(ents, "SCOPE.Schema", "Order") {
		t.Error("expected Order item schema from dynamodbav tags")
	}
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Email") {
		t.Error("expected User.Email field component")
	}
	// dynamodbav:"-" must NOT produce a field.
	if hasSubtype(ents, "SCOPE.Component", "field", "field:User.Skip") {
		t.Error("did not expect User.Skip field (dynamodbav:\"-\")")
	}

	// Schema: TableName literals => tables.
	if !containsEntity(ents, "SCOPE.Schema", "table:users") {
		t.Error("expected users table from TableName")
	}
	if !containsEntity(ents, "SCOPE.Schema", "table:orders") {
		t.Error("expected orders table from TableName")
	}

	// Queries.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one dynamodb query operation")
	}
}

func TestDynamoImportGate(t *testing.T) {
	src := `package x

type Doc struct {
	ID string ` + "`dynamodbav:\"id\"`" + `
}
`
	file := extreg.FileInput{Path: "no_dynamo.go", Language: "go", Content: []byte(src)}
	ents := extractWith(t, "custom_go_dynamodb", file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without dynamodb import, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// elasticsearch: index schema + json doc structs + create-index migration +
// query verbs.
// ---------------------------------------------------------------------------

func TestElasticIndicesModelsMigrationsAndQueries(t *testing.T) {
	ents := extractWith(t, "custom_go_elasticsearch", fixtureInput(t, "elasticsearch_store.go", "go"))

	// Schema: index name call sites => indices.
	if !containsEntity(ents, "SCOPE.Schema", "index:products") {
		t.Error("expected products index schema")
	}

	// Models: json-tagged struct => document shape.
	if !containsEntity(ents, "SCOPE.Schema", "Product") {
		t.Error("expected Product doc schema from json tags")
	}
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:Product.Name") {
		t.Error("expected Product.Name field component")
	}

	// Migrations (partial): index-creation operation.
	if !hasKindSubtype(ents, "SCOPE.Operation", "migration") {
		t.Error("expected an index-creation migration operation")
	}
	if !containsEntity(ents, "SCOPE.Operation", "create_index:products") {
		t.Error("expected create_index:products migration")
	}

	// Queries.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one elasticsearch query operation")
	}
}

func TestElasticImportGate(t *testing.T) {
	src := `package x

type Doc struct {
	ID string ` + "`json:\"id\"`" + `
}
`
	file := extreg.FileInput{Path: "no_es.go", Language: "go", Content: []byte(src)}
	ents := extractWith(t, "custom_go_elasticsearch", file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without elasticsearch import, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// neo4j: Cypher node labels (schema) + relationship types (relationships,
// first-class) + query verbs.
// ---------------------------------------------------------------------------

func TestNeo4jSchemaRelationshipsAndQueries(t *testing.T) {
	ents := extractWith(t, "custom_go_neo4j", fixtureInput(t, "neo4j_store.go", "go"))

	// Schema: node labels in Cypher patterns => nodes.
	if !containsEntity(ents, "SCOPE.Schema", "node:Person") {
		t.Error("expected Person node schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "node:Movie") {
		t.Error("expected Movie node schema")
	}

	// Relationships: relationship types => SCOPE.Schema subtype=relationship.
	if !hasSubtype(ents, "SCOPE.Schema", "relationship", "rel:ACTED_IN") {
		t.Error("expected ACTED_IN relationship")
	}
	if !hasSubtype(ents, "SCOPE.Schema", "relationship", "rel:KNOWS") {
		t.Error("expected KNOWS relationship")
	}

	// Queries.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one neo4j query operation")
	}
}

func TestNeo4jImportGate(t *testing.T) {
	src := `package x

const q = "MATCH (p:Person)-[:KNOWS]->(o) RETURN p"
`
	file := extreg.FileInput{Path: "no_neo4j.go", Language: "go", Content: []byte(src)}
	ents := extractWith(t, "custom_go_neo4j", file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without neo4j import, got %d", len(ents))
	}
}
