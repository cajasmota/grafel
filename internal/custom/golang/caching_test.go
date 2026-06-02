package golang_test

// caching_test.go — value-asserting tests for the custom_go_caching extractor
// ([cache] breadth, epic #3628). Asserts the actual cache region + CACHES /
// INVALIDATES edge (region id + edge kind), not len>0.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/golang"
	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runGoCaching(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_go_caching")
	if !ok {
		t.Fatal("custom_go_caching not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "cache.go", Language: "go", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func goEdge(ents []types.EntityRecord, kind, wantRef string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == kind && r.ToID == wantRef {
				return r
			}
		}
	}
	return nil
}

func goHasRegion(ents []types.EntityRecord, ref string) bool {
	for _, e := range ents {
		if e.Kind == "SCOPE.Datastore" && e.Subtype == "cache_region" {
			if "cache:"+e.Properties["framework"]+":"+e.Properties["region"] == ref {
				return true
			}
		}
	}
	return false
}

// ristretto Set + Del on the SAME literal key converge: CACHES(Set→region) AND
// INVALIDATES(Del→region) on the one "cache:ristretto:user:1" node.
func TestGoCaching_Ristretto_SetDel_Converge(t *testing.T) {
	src := `
import "github.com/dgraph-io/ristretto"

func warm(cache *ristretto.Cache, v User) { cache.Set("user:1", v, 1) }
func evict(cache *ristretto.Cache)        { cache.Del("user:1") }
`
	ents := runGoCaching(t, src)
	ref := "cache:ristretto:user:1"
	if !goHasRegion(ents, ref) {
		t.Fatalf("expected ristretto cache region %q", ref)
	}
	c := goEdge(ents, "CACHES", ref)
	if c == nil {
		t.Fatalf("expected cache.Set CACHES region user:1")
	}
	if c.Properties["mode"] != "write" {
		t.Errorf("Set mode = %q, want write", c.Properties["mode"])
	}
	i := goEdge(ents, "INVALIDATES", ref)
	if i == nil {
		t.Fatalf("expected cache.Del INVALIDATES region user:1 (converging on same node)")
	}
	if i.Properties["mode"] != "evict" {
		t.Errorf("Del mode = %q, want evict", i.Properties["mode"])
	}
}

func TestGoCaching_Ristretto_Get_Read(t *testing.T) {
	src := `
import "github.com/dgraph-io/ristretto"
func read(cache *ristretto.Cache) { cache.Get("profile") }
`
	ents := runGoCaching(t, src)
	e := goEdge(ents, "CACHES", "cache:ristretto:profile")
	if e == nil {
		t.Fatalf("expected cache.Get CACHES region profile")
	}
	if e.Properties["mode"] != "read" {
		t.Errorf("Get mode = %q, want read", e.Properties["mode"])
	}
}

func TestGoCaching_Groupcache_NewGroupAndGet(t *testing.T) {
	src := `
import "github.com/golang/groupcache"

var users = groupcache.NewGroup("users", 64<<20, getter)

func read(ctx context.Context) { users.Get(ctx, "user:1", dest) }
`
	ents := runGoCaching(t, src)
	if !goHasRegion(ents, "cache:groupcache:users") {
		t.Fatalf("expected declared groupcache region users")
	}
	if goEdge(ents, "CACHES", "cache:groupcache:user:1") == nil {
		t.Fatalf("expected group.Get CACHES region user:1")
	}
}

// Honest-partial: a variable (dynamic) key yields a <dynamic> region, not a
// concrete one.
func TestGoCaching_Ristretto_DynamicKey_Partial(t *testing.T) {
	src := `
import "github.com/dgraph-io/ristretto"
func warm(cache *ristretto.Cache, k string, v User) { cache.Set(k, v, 1) }
`
	ents := runGoCaching(t, src)
	if goEdge(ents, "CACHES", "cache:ristretto:<dynamic>") == nil {
		t.Fatalf("expected honest-partial <dynamic> CACHES edge for variable key")
	}
	for _, e := range ents {
		if e.Kind == "SCOPE.Datastore" && e.Subtype == "cache_region" &&
			e.Properties["region"] == "<dynamic>" && e.Properties["dynamic"] != "true" {
			t.Fatalf("dynamic region must be marked dynamic")
		}
	}
}

// Negative: a non-cache `.Set`/`.Get` on an unrelated value in a file with NO
// cache import must emit no edges (import gate).
func TestGoCaching_NoCacheImport_NoEdge(t *testing.T) {
	src := `
func f(m map[string]int) { m["x"] = 1; _ = config.Get("y") }
`
	ents := runGoCaching(t, src)
	if len(ents) != 0 {
		t.Fatalf("non-cache file should emit no entities, got %d", len(ents))
	}
}
