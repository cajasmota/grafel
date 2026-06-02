package csharp_test

// redis_test.go — value-asserting tests for the custom_csharp_redis extractor
// (StackExchange.Redis key/channel/stream topology; #3625 epic).

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runCSharpRedis(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_csharp_redis")
	if !ok {
		t.Fatal("custom_csharp_redis not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "Cache.cs", Language: "csharp", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findRedisEdge(ents []types.EntityRecord, kind, targetRef string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == kind && r.ToID == targetRef {
				return r
			}
		}
	}
	return nil
}

func findKeyspaceEntity(ents []types.EntityRecord, ref string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == ref && ents[i].Kind == "SCOPE.Datastore" {
			return &ents[i]
		}
	}
	return nil
}

func TestCSharpRedis_StringGet_ReadsFromLiteralKey(t *testing.T) {
	ents := runCSharpRedis(t, `var v = db.StringGet("session:abc");`)
	ref := "Datastore:redis:session:abc"
	if findKeyspaceEntity(ents, ref) == nil {
		t.Fatalf("expected keyspace entity %q", ref)
	}
	rel := findRedisEdge(ents, "READS_FROM", ref)
	if rel == nil {
		t.Fatalf("expected READS_FROM edge to %q", ref)
	}
	if rel.Properties["keyspace"] != "session:abc" {
		t.Errorf("keyspace prop = %q, want session:abc", rel.Properties["keyspace"])
	}
}

func TestCSharpRedis_StringSet_WritesToLiteralKey(t *testing.T) {
	ents := runCSharpRedis(t, `db.StringSet("user:42", payload);`)
	ref := "Datastore:redis:user:42"
	if findRedisEdge(ents, "WRITES_TO", ref) == nil {
		t.Fatalf("expected WRITES_TO edge to %q", ref)
	}
}

func TestCSharpRedis_StringSet_ConcatPrefixGlob(t *testing.T) {
	ents := runCSharpRedis(t, `db.StringSet("user:" + id, payload);`)
	ref := "Datastore:redis:user:*"
	if findRedisEdge(ents, "WRITES_TO", ref) == nil {
		t.Fatalf("expected WRITES_TO edge to prefix glob %q", ref)
	}
	ks := findKeyspaceEntity(ents, ref)
	if ks == nil || ks.Subtype != "key_prefix" {
		t.Fatalf("expected key_prefix keyspace %q, got %+v", ref, ks)
	}
}

func TestCSharpRedis_InterpolatedPrefixGlob(t *testing.T) {
	ents := runCSharpRedis(t, `var v = db.StringGet($"session:{sid}");`)
	ref := "Datastore:redis:session:*"
	if findRedisEdge(ents, "READS_FROM", ref) == nil {
		t.Fatalf("expected READS_FROM edge to prefix glob %q", ref)
	}
}

func TestCSharpRedis_Publish_PublishesToChannel(t *testing.T) {
	ents := runCSharpRedis(t, `sub.Publish("events", payload);`)
	ref := "Datastore:redis:events"
	rel := findRedisEdge(ents, "PUBLISHES_TO", ref)
	if rel == nil {
		t.Fatalf("expected PUBLISHES_TO edge to channel %q", ref)
	}
	if rel.Properties["channel"] != "events" {
		t.Errorf("channel prop = %q, want events", rel.Properties["channel"])
	}
}

func TestCSharpRedis_Subscribe_SubscribesToChannel(t *testing.T) {
	ents := runCSharpRedis(t, `sub.Subscribe("events", handler);`)
	if findRedisEdge(ents, "SUBSCRIBES_TO", "Datastore:redis:events") == nil {
		t.Fatal("expected SUBSCRIBES_TO edge to channel events")
	}
}

func TestCSharpRedis_StreamAdd_WritesToStream(t *testing.T) {
	ents := runCSharpRedis(t, `db.StreamAdd("orders", values);`)
	ref := "Datastore:redis:orders"
	rel := findRedisEdge(ents, "WRITES_TO", ref)
	if rel == nil {
		t.Fatalf("expected WRITES_TO edge to stream %q", ref)
	}
	if rel.Properties["stream"] != "orders" {
		t.Errorf("stream prop = %q, want orders", rel.Properties["stream"])
	}
}

func TestCSharpRedis_StreamRead_ReadsFromStream(t *testing.T) {
	ents := runCSharpRedis(t, `var r = db.StreamRead("orders", "0");`)
	if findRedisEdge(ents, "READS_FROM", "Datastore:redis:orders") == nil {
		t.Fatal("expected READS_FROM edge to stream orders")
	}
}

func TestCSharpRedis_DynamicKey_NoFabricatedKey(t *testing.T) {
	ents := runCSharpRedis(t, `var v = db.StringGet(dynamicKey);`)
	for i := range ents {
		if ents[i].Kind == "SCOPE.Datastore" {
			t.Fatalf("expected NO keyspace entity for dynamic key, got %q", ents[i].Name)
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "READS_FROM" || r.Kind == "WRITES_TO" {
				t.Fatalf("expected NO access edge for dynamic key, got %s -> %s", r.Kind, r.ToID)
			}
		}
	}
	// The op site itself is still emitted (honest-partial), flagged dynamic.
	var found bool
	for i := range ents {
		if ents[i].Subtype == "cache_op" && ents[i].Properties["dynamic"] == "true" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a dynamic-flagged cache_op site")
	}
}
