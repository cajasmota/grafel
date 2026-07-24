package javascript_test

// caching_test.go — value-asserting tests for the custom_js_caching extractor
// (#3692, epic #3628, area #18).

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func runJSCaching(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_js_caching")
	if !ok {
		t.Fatal("custom_js_caching not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "typescript", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findJSCacheRel(ents []types.EntityRecord, kind, targetRef string) *types.RelationshipRecord {
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

func TestJSCaching_NestCacheKey_ReadThrough(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @CacheKey('users')
  @Get()
  findAll() { return this.svc.all(); }
}
`
	ents := runJSCaching(t, "users.controller.ts", src)
	r := findJSCacheRel(ents, "CACHES", "cache:nestjs:users")
	if r == nil {
		t.Fatalf("expected @CacheKey('users') to CACHES key users (read-through)")
	}
	if r.Properties.Get("mode") != "read_through" {
		t.Errorf("mode = %q, want read_through", r.Properties.Get("mode"))
	}
}

func TestJSCaching_CacheManagerWrap_ReadThrough(t *testing.T) {
	src := `
async function getUser(id) {
  return cache.wrap('user:1', () => db.find(id));
}
`
	ents := runJSCaching(t, "svc.ts", src)
	r := findJSCacheRel(ents, "CACHES", "cache:cache_manager:user:1")
	if r == nil {
		t.Fatalf("expected cache.wrap('user:1', fn) to CACHES key user:1")
	}
	if r.Properties.Get("mode") != "read_through" {
		t.Errorf("mode = %q, want read_through", r.Properties.Get("mode"))
	}
}

func TestJSCaching_CacheManagerDel_Invalidates(t *testing.T) {
	src := `
async function evict() {
  await cacheManager.del('user:1');
}
`
	ents := runJSCaching(t, "svc.ts", src)
	r := findJSCacheRel(ents, "INVALIDATES", "cache:cache_manager:user:1")
	if r == nil {
		t.Fatalf("expected cacheManager.del('user:1') to INVALIDATE key user:1")
	}
	if r.Properties.Get("mode") != "evict" {
		t.Errorf("mode = %q, want evict", r.Properties.Get("mode"))
	}
}

func TestJSCaching_CacheManagerSet_Write(t *testing.T) {
	src := `await cache.set('config', value);`
	ents := runJSCaching(t, "svc.ts", src)
	r := findJSCacheRel(ents, "CACHES", "cache:cache_manager:config")
	if r == nil || r.Properties.Get("mode") != "write" {
		t.Fatalf("expected write mode on config, got %+v", r)
	}
}

func TestJSCaching_TemplateLiteral_Dynamic(t *testing.T) {
	src := "async function f(id) { return cache.wrap(`user:${id}`, fn); }"
	ents := runJSCaching(t, "svc.ts", src)
	r := findJSCacheRel(ents, "CACHES", "cache:cache_manager:user:*")
	if r == nil {
		t.Fatalf("expected template-literal key to CACHES prefix user:*")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("template-literal key should be dynamic")
	}
}

// Negative: a plain method with no cache call/decorator emits no cache edge.
func TestJSCaching_Plain_NoEdge(t *testing.T) {
	src := `
export class UsersController {
  findAll() { return this.svc.all(); }
}
`
	ents := runJSCaching(t, "users.controller.ts", src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CACHES" || r.Kind == "INVALIDATES" {
				t.Fatalf("plain method should emit no cache edge, got %+v", r)
			}
		}
	}
}
