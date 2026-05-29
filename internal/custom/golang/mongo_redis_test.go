package golang_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

func extractWith(t *testing.T, lang string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(lang)
	if !ok {
		t.Fatalf("%s not registered", lang)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	out := make([]entitySummary, 0, len(ents))
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

// ---------------------------------------------------------------------------
// mongodb: collection schema + bson struct fields + query DSL verbs.
// ---------------------------------------------------------------------------

func TestMongoCollectionsModelsAndQueries(t *testing.T) {
	ents := extractWith(t, "custom_go_mongo_driver", fixtureInput(t, "mongo_store.go", "go"))

	// Schema: .Collection("x") call sites => collections.
	if !containsEntity(ents, "SCOPE.Schema", "collection:users") {
		t.Error("expected users collection schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "collection:orders") {
		t.Error("expected orders collection schema")
	}

	// Models: structs with bson tags are document shapes.
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema from bson tags")
	}
	if !containsEntity(ents, "SCOPE.Schema", "Order") {
		t.Error("expected Order schema from bson tags")
	}
	// Schema fields.
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Name") {
		t.Error("expected User.Name field component")
	}
	// bson:"email,omitempty" -> field email (option stripped, still emitted).
	if !hasSubtype(ents, "SCOPE.Component", "field", "field:User.Email") {
		t.Error("expected User.Email field component")
	}
	// bson:"-" must NOT produce a field.
	if hasSubtype(ents, "SCOPE.Component", "field", "field:User.Skip") {
		t.Error("did not expect User.Skip field (bson:\"-\")")
	}

	// Queries: at least one query operation from the collection method calls.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one mongo query operation")
	}
}

// A Go file without the mongo-driver import yields nothing (import gate).
func TestMongoImportGate(t *testing.T) {
	src := `package x

type Doc struct {
	ID string ` + "`bson:\"_id\"`" + `
}

func run() { _ = ".Collection(\"users\")" }
`
	file := extreg.FileInput{Path: "no_mongo.go", Language: "go", Content: []byte(src)}
	ents := extractWith(t, "custom_go_mongo_driver", file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without mongo import, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// redis: coarse key-pattern keyspaces + command DSL verbs.
// ---------------------------------------------------------------------------

func TestRedisKeyspacesAndCommands(t *testing.T) {
	ents := extractWith(t, "custom_go_redis_driver", fixtureInput(t, "redis_cache.go", "go"))

	// Coarse schema: namespaced key patterns => keyspaces by prefix.
	if !containsEntity(ents, "SCOPE.Schema", "keyspace:user") {
		t.Error("expected user keyspace from key pattern")
	}
	if !containsEntity(ents, "SCOPE.Schema", "keyspace:session") {
		t.Error("expected session keyspace from key pattern")
	}
	if !containsEntity(ents, "SCOPE.Schema", "keyspace:cache") {
		t.Error("expected cache keyspace from key pattern")
	}

	// Queries: command call sites => query operations.
	if !hasKindSubtype(ents, "SCOPE.Operation", "query") {
		t.Error("expected at least one redis query operation")
	}
}

// A Go file without the go-redis import yields nothing (import gate).
func TestRedisImportGate(t *testing.T) {
	src := `package x

func run() { _ = "user:1" }
`
	file := extreg.FileInput{Path: "no_redis.go", Language: "go", Content: []byte(src)}
	ents := extractWith(t, "custom_go_redis_driver", file)
	if len(ents) != 0 {
		t.Errorf("expected no entities without redis import, got %d", len(ents))
	}
}
