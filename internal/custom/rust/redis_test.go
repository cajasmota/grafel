package rust_test

// redis_test.go — value-asserting tests for the custom_rust_redis extractor
// (redis-rs key/channel/stream topology; #3625 epic).

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runRustRedis(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_rust_redis")
	if !ok {
		t.Fatal("custom_rust_redis not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "cache.rs", Language: "rust", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findRustRedisEdge(ents []types.EntityRecord, kind, targetRef string) *types.RelationshipRecord {
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

func findRustKeyspace(ents []types.EntityRecord, ref string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == ref && ents[i].Kind == "SCOPE.Datastore" {
			return &ents[i]
		}
	}
	return nil
}

func TestRustRedis_Get_ReadsFromLiteralKey(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
let v: String = con.get("session:abc")?;`)
	ref := "Datastore:redis:session:abc"
	if findRustKeyspace(ents, ref) == nil {
		t.Fatalf("expected keyspace entity %q", ref)
	}
	rel := findRustRedisEdge(ents, "READS_FROM", ref)
	if rel == nil {
		t.Fatalf("expected READS_FROM edge to %q", ref)
	}
	if rel.Properties["keyspace"] != "session:abc" {
		t.Errorf("keyspace prop = %q, want session:abc", rel.Properties["keyspace"])
	}
}

func TestRustRedis_Get_Turbofish(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
let v = con.get::<_, String>("cfg:flags")?;`)
	if findRustRedisEdge(ents, "READS_FROM", "Datastore:redis:cfg:flags") == nil {
		t.Fatal("expected READS_FROM edge to cfg:flags (turbofish)")
	}
}

func TestRustRedis_Set_WritesToLiteralKey(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
con.set("user:42", payload)?;`)
	if findRustRedisEdge(ents, "WRITES_TO", "Datastore:redis:user:42") == nil {
		t.Fatal("expected WRITES_TO edge to user:42")
	}
}

func TestRustRedis_Publish_PublishesToChannel(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
con.publish("events", payload)?;`)
	ref := "Datastore:redis:events"
	rel := findRustRedisEdge(ents, "PUBLISHES_TO", ref)
	if rel == nil {
		t.Fatalf("expected PUBLISHES_TO edge to channel %q", ref)
	}
	if rel.Properties["channel"] != "events" {
		t.Errorf("channel prop = %q, want events", rel.Properties["channel"])
	}
}

func TestRustRedis_Xadd_WritesToStream(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
con.xadd("orders", "*", &items)?;`)
	ref := "Datastore:redis:orders"
	rel := findRustRedisEdge(ents, "WRITES_TO", ref)
	if rel == nil {
		t.Fatalf("expected WRITES_TO edge to stream %q", ref)
	}
	if rel.Properties["stream"] != "orders" {
		t.Errorf("stream prop = %q, want orders", rel.Properties["stream"])
	}
}

func TestRustRedis_CmdBuilder_Get_ReadsFromKey(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
let v: String = cmd("GET").arg("session:abc").query(&mut con)?;`)
	ref := "Datastore:redis:session:abc"
	rel := findRustRedisEdge(ents, "READS_FROM", ref)
	if rel == nil {
		t.Fatalf("expected READS_FROM edge to %q via cmd builder", ref)
	}
	if rel.Properties["op"] != "GET" {
		t.Errorf("op prop = %q, want GET", rel.Properties["op"])
	}
}

func TestRustRedis_CmdBuilder_Set_WritesToKey(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
cmd("SET").arg("user:42").arg(v).execute(&mut con);`)
	if findRustRedisEdge(ents, "WRITES_TO", "Datastore:redis:user:42") == nil {
		t.Fatal("expected WRITES_TO edge to user:42 via cmd builder")
	}
}

func TestRustRedis_CmdBuilder_Publish_PublishesToChannel(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
cmd("PUBLISH").arg("events").arg(p).execute(&mut con);`)
	if findRustRedisEdge(ents, "PUBLISHES_TO", "Datastore:redis:events") == nil {
		t.Fatal("expected PUBLISHES_TO edge to events via cmd builder")
	}
}

func TestRustRedis_DynamicKey_NoFabricatedKey(t *testing.T) {
	ents := runRustRedis(t, `use redis::Commands;
let v: String = con.get(format!("user:{}", id))?;`)
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
