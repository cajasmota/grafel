package javascript

// caching.go — JS/TS caching-topology extraction (#3692, epic #3628, area #18).
//
// Cross-language consistent with the Spring cache-region node
// (internal/custom/java/caching.go), the Python cache-region node, and the
// Rails cache-region node. Two dominant JS/TS cache abstractions:
//
//	NestJS (@nestjs/cache-manager):
//	  @CacheKey('users')   on a handler  → method CACHES key "users"
//	                                       (read-through via CacheInterceptor)
//
//	cache-manager (the library NestJS wraps, also used standalone):
//	  cache.wrap('user:1', fn)   → call-site CACHES key "user:1" (read-through)
//	  cache.set('user:1', v)     → CACHES key "user:1"          (write)
//	  cache.del('user:1')        → INVALIDATES key "user:1"     (evict)
//	  cacheManager.del(...)      → INVALIDATES                  (evict)
//
// Entity/edge shape:
//
//	target  : SCOPE.Datastore  subtype "cache_region"
//	          Ref  "cache:<framework>:<key>"  (sites converge on one node)
//	carrier : SCOPE.Operation  the decorated method / cache call-site
//	edge    : CACHES | INVALIDATES   carrier → key
//
// Dynamic keys (`cache.wrap(key, fn)` / template literal `` `user:${id}` ``) are
// honest-partial: the edge is emitted with dynamic="true" against a key-prefix
// (template head) or "<dynamic>" target.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extreg.Register("custom_js_caching", &jsCachingExtractor{})
}

type jsCachingExtractor struct{}

func (e *jsCachingExtractor) Language() string { return "custom_js_caching" }

var (
	// reNestCacheKey matches @CacheKey('users') on a method. Group 1 = key
	// literal body; Group 2 = decorated method name. Intervening decorators
	// (@CacheTTL, @UseInterceptors) are skipped.
	reNestCacheKey = regexp.MustCompile(
		`@CacheKey\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]*)['"` + "`" + `]\s*\)\s*` +
			`(?:@\w+(?:\([^)]*\))?\s*)*` +
			`(?:public\s+|private\s+|protected\s+)?(?:async\s+)?(\w+)\s*\(`)

	// reCacheManagerOp matches a cache-manager op. Group 1 = receiver
	// (cache|cacheManager|cacheStore); Group 2 = verb; Group 3 = the first-arg
	// key — either a quoted/backtick literal body, or empty when dynamic.
	reCacheManagerOp = regexp.MustCompile(
		`\b(cache|cacheManager|cacheStore)\.(wrap|set|get|del|delete|reset|mget|mset)\s*\(\s*(['"` + "`" + `][^'"` + "`" + `$]*)?`)
)

func jsCacheRef(framework, key string) string {
	return fmt.Sprintf("cache:%s:%s", framework, key)
}

// cacheManagerEdge classifies a cache-manager verb into (relType, mode).
func cacheManagerEdge(verb string) (string, string) {
	switch verb {
	case "del", "delete", "reset":
		return "INVALIDATES", "evict"
	case "set", "mset":
		return "CACHES", "write"
	default: // wrap, get, mget
		return "CACHES", "read_through"
	}
}

func (e *jsCachingExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.js_caching")
	_, span := tracer.Start(ctx, "custom.js_caching")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "@CacheKey") && !strings.Contains(src, "cache") {
		return nil, nil
	}

	var out []types.EntityRecord
	seenRegion := make(map[string]bool)
	add := func(e types.EntityRecord) { out = append(out, e) }

	emitRegion := func(framework, key string, dynamic bool, line int) string {
		ref := jsCacheRef(framework, key)
		if !seenRegion[ref] {
			seenRegion[ref] = true
			ent := makeEntity(key, "SCOPE.Datastore", "cache_region", file.Path, file.Language, line)
			setProps(&ent, "framework", framework, "region", key,
				"cache_kind", "region", "provenance", "INFERRED_FROM_JS_CACHE")
			if dynamic {
				setProps(&ent, "dynamic", "true")
			}
			add(ent)
		}
		return ref
	}

	// 1. NestJS @CacheKey decorators (always read-through via CacheInterceptor).
	for _, m := range reNestCacheKey.FindAllStringSubmatchIndex(src, -1) {
		key := src[m[2]:m[3]]
		method := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		if strings.TrimSpace(key) == "" {
			continue
		}
		ent := makeEntity(method, "SCOPE.Operation", "cache_method", file.Path, file.Language, line)
		setProps(&ent, "framework", "nestjs", "cache_mode", "read_through",
			"cache_decorator", "CacheKey", "provenance", "INFERRED_FROM_CACHE_KEY")
		ref := emitRegion("nestjs", key, false, line)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: ref, Kind: "CACHES",
			Properties: types.Props{
				{K: "framework", V: "nestjs"},
				{K: "language", V: file.Language},
				{K: "mode", V: "read_through"},
				{K: "region", V: key},
			},
		})
		add(ent)
	}

	// 2. cache-manager call-sites.
	for _, m := range reCacheManagerOp.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[m[2]:m[3]]
		verb := src[m[4]:m[5]]
		line := lineOf(src, m[0])

		rawKey := ""
		if m[6] >= 0 {
			rawKey = src[m[6]:m[7]]
		}
		key, dynamic := normaliseJSCacheKey(rawKey)

		relType, mode := cacheManagerEdge(verb)
		ent := makeEntity(receiver+"."+verb, "SCOPE.Operation", "cache_method", file.Path, file.Language, line)
		setProps(&ent, "framework", "cache_manager", "cache_verb", verb, "cache_mode", mode,
			"provenance", "INFERRED_FROM_CACHE_MANAGER")
		if dynamic {
			setProps(&ent, "dynamic", "true")
		}
		ref := emitRegion("cache_manager", key, dynamic, line)
		edgeProps := types.Props{
			{K: "framework", V: "cache_manager"},
			{K: "language", V: file.Language},
			{K: "mode", V: mode},
			{K: "region", V: key},
		}
		if dynamic {
			edgeProps.Set("dynamic", "true")
		}
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: ref, Kind: relType, Properties: edgeProps,
		})
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// normaliseJSCacheKey resolves the captured first-argument literal:
//
//	'home'        -> ("home", false)
//	"user:1"      -> ("user:1", false)
//	`user:${...}  -> the regex stops the literal at `$`, so a backtick literal
//	                 with interpolation yields its static head -> key-prefix.
//	""            -> ("<dynamic>", true)   (variable key)
func normaliseJSCacheKey(raw string) (string, bool) {
	if raw == "" {
		return "<dynamic>", true
	}
	// Strip the leading quote/backtick.
	body := strings.TrimLeft(raw, "'\"`")
	if body == "" {
		// Was a bare opening backtick before an interpolation -> fully dynamic.
		return "<dynamic>", true
	}
	// A backtick literal whose interpolation was cut leaves a static head; mark
	// it as a prefix (dynamic). A plain-quote literal that reached here is a
	// concrete key. We distinguish by the original opening delimiter.
	if strings.HasPrefix(raw, "`") {
		return body + "*", true
	}
	return body, false
}
