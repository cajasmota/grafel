package ruby_test

// redis_test.go — value-asserting tests for the custom_ruby_redis extractor
// (redis-rb key/channel/stream topology; #3625 epic).

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runRubyRedis(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_ruby_redis")
	if !ok {
		t.Fatal("custom_ruby_redis not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "cache.rb", Language: "ruby", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findRubyRedisEdge(ents []types.EntityRecord, kind, targetRef string) *types.RelationshipRecord {
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

func findRubyKeyspace(ents []types.EntityRecord, ref string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == ref && ents[i].Kind == "SCOPE.Datastore" {
			return &ents[i]
		}
	}
	return nil
}

func TestRubyRedis_Get_ReadsFromLiteralKey(t *testing.T) {
	ents := runRubyRedis(t, `v = redis.get("session:abc")`)
	ref := "Datastore:redis:session:abc"
	if findRubyKeyspace(ents, ref) == nil {
		t.Fatalf("expected keyspace entity %q", ref)
	}
	rel := findRubyRedisEdge(ents, "READS_FROM", ref)
	if rel == nil {
		t.Fatalf("expected READS_FROM edge to %q", ref)
	}
	if rel.Properties["keyspace"] != "session:abc" {
		t.Errorf("keyspace prop = %q, want session:abc", rel.Properties["keyspace"])
	}
}

func TestRubyRedis_Set_WritesToLiteralKey(t *testing.T) {
	ents := runRubyRedis(t, `redis.set("user:42", payload)`)
	if findRubyRedisEdge(ents, "WRITES_TO", "Datastore:redis:user:42") == nil {
		t.Fatal("expected WRITES_TO edge to user:42")
	}
}

func TestRubyRedis_Set_ConcatPrefixGlob(t *testing.T) {
	ents := runRubyRedis(t, `redis.set("user:" + id, payload)`)
	ref := "Datastore:redis:user:*"
	if findRubyRedisEdge(ents, "WRITES_TO", ref) == nil {
		t.Fatalf("expected WRITES_TO edge to prefix glob %q", ref)
	}
	ks := findRubyKeyspace(ents, ref)
	if ks == nil || ks.Subtype != "key_prefix" {
		t.Fatalf("expected key_prefix keyspace %q, got %+v", ref, ks)
	}
}

func TestRubyRedis_Set_InterpolatedPrefixGlob(t *testing.T) {
	ents := runRubyRedis(t, `redis.set("user:#{id}", payload)`)
	if findRubyRedisEdge(ents, "WRITES_TO", "Datastore:redis:user:*") == nil {
		t.Fatal("expected WRITES_TO edge to user:* (interpolated)")
	}
}

func TestRubyRedis_SingleQuoteNoInterp(t *testing.T) {
	// Single-quoted strings do not interpolate in Ruby; '#{id}' is a literal key.
	ents := runRubyRedis(t, `redis.get('user:#{id}')`)
	if findRubyRedisEdge(ents, "READS_FROM", "Datastore:redis:user:#{id}") == nil {
		t.Fatal("expected single-quoted literal key user:#{id}")
	}
}

func TestRubyRedis_Publish_PublishesToChannel(t *testing.T) {
	ents := runRubyRedis(t, `redis.publish("events", payload)`)
	ref := "Datastore:redis:events"
	rel := findRubyRedisEdge(ents, "PUBLISHES_TO", ref)
	if rel == nil {
		t.Fatalf("expected PUBLISHES_TO edge to channel %q", ref)
	}
	if rel.Properties["channel"] != "events" {
		t.Errorf("channel prop = %q, want events", rel.Properties["channel"])
	}
}

func TestRubyRedis_Subscribe_SubscribesToChannel(t *testing.T) {
	ents := runRubyRedis(t, `redis.subscribe("events") do |on| end`)
	if findRubyRedisEdge(ents, "SUBSCRIBES_TO", "Datastore:redis:events") == nil {
		t.Fatal("expected SUBSCRIBES_TO edge to channel events")
	}
}

func TestRubyRedis_Xadd_WritesToStream(t *testing.T) {
	ents := runRubyRedis(t, `redis.xadd("orders", { f: 1 })`)
	ref := "Datastore:redis:orders"
	rel := findRubyRedisEdge(ents, "WRITES_TO", ref)
	if rel == nil {
		t.Fatalf("expected WRITES_TO edge to stream %q", ref)
	}
	if rel.Properties["stream"] != "orders" {
		t.Errorf("stream prop = %q, want orders", rel.Properties["stream"])
	}
}

func TestRubyRedis_DynamicKey_NoFabricatedKey(t *testing.T) {
	ents := runRubyRedis(t, `v = redis.get(key)`)
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
