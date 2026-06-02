package python

// caching.go — Python caching-decorator topology extraction (#3692, epic
// #3628, area #18).
//
// Sits ON TOP of the raw redis key work (redis.go): instead of low-level
// GET/SET it records the read-through caching INTENT a decorated function
// declares. Three idioms are covered:
//
//	@lru_cache / @cache / @functools.lru_cache  (stdlib, in-process memoisation)
//	    → function CACHES an in-process region keyed by the function's qualname.
//	      mode=in_process. No external key — the cache key is the call args.
//
//	@cache.cached(timeout=60, key_prefix='view/%s')   (Flask-Caching)
//	    → function CACHES region <key_prefix>. mode=read_through.
//	      Missing key_prefix → Flask derives it from the request path → dynamic.
//
//	@cached(cache, key=...)  /  @cachetools.cached(...)   (cachetools)
//	    → function CACHES an in-process region keyed by the function qualname.
//	      A `key=` callable is dynamic; recorded honest-partial.
//
// Entity/edge shape (cross-language consistent with the redis keyspace node and
// the Spring cache-region node):
//
//	target  : SCOPE.Datastore  subtype "cache_region"
//	          Ref  "cache:<framework>:<region>"  (converges sites on one node)
//	carrier : the decorated function operation (owner of the edge)
//	edge    : CACHES   owner → region
//
// Django cache framework (django.core.cache) is also covered here:
//
//	cache.get('k') / cache.set('k', v) / caches['x'].get('k')
//	    → call-site CACHES region 'k'   (low-level cache API)
//	cache.delete('k')                   → INVALIDATES region 'k'
//	@cache_page(60)  on a view          → view CACHES region '<request_path>'
//	                                      (per-URL page cache; dynamic region)
//
// So unlike the decorator-only idioms above, Django DOES emit INVALIDATES
// (cache.delete), converging Set/Get/Delete of the same literal key on one
// region node — matching the Spring @Cacheable/@CacheEvict convergence.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_caching", &CachingExtractor{})
}

// CachingExtractor extracts Python caching decorators and the regions they
// populate.
type CachingExtractor struct{}

func (e *CachingExtractor) Language() string { return "python_caching" }

var (
	// cacheLruRe matches @lru_cache / @cache / @functools.lru_cache /
	// @functools.cache (with or without parens) immediately above a def.
	// Group 1 = decorated function name.
	cacheLruRe = regexp.MustCompile(
		`(?m)@(?:functools\.)?(?:lru_cache|cache)\b\s*(?:\([^)]*\))?\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// cacheFlaskRe matches @cache.cached(...) / @<something>.cached(...)
	// (Flask-Caching). Group 1 = the argument body; Group 2 = function name.
	cacheFlaskRe = regexp.MustCompile(
		`(?m)@\w+\.cached\s*\(([^)]*)\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// cacheToolsRe matches @cached(...) or @cachetools.cached(...) (cachetools).
	// Group 1 = argument body; Group 2 = function name. The leading `\w+\.`
	// alternation lets it match both the bare and module-qualified forms while
	// NOT overlapping the `\w+.cached` Flask form (handled above) — we
	// post-filter so a `.cached` match is not double-counted.
	cacheToolsRe = regexp.MustCompile(
		`(?m)@(?:cachetools\.)?cached\s*\(([^)]*)\)\s*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// flaskKeyPrefixRe pulls key_prefix='view/%s' out of a Flask-Caching body.
	// Group 1 = the key-prefix literal body.
	flaskKeyPrefixRe = regexp.MustCompile(`key_prefix\s*=\s*["']([^"']*)["']`)

	// djangoCacheOpRe matches a Django low-level cache op. Group 1 = receiver
	// (`cache` or a `caches['x']` subscript); Group 2 = verb; Group 3 = the
	// OPTIONAL first-argument string-literal key (absent → variable key →
	// dynamic). The receiver alternation accepts both the default `cache` handle
	// and an aliased `caches['default']` handle.
	djangoCacheOpRe = regexp.MustCompile(
		`(?m)\b(cache|caches\[[^\]]*\])\.(get|set|add|delete|get_or_set|incr|decr)\s*\(\s*(?:["']([^"']*)["'])?`)

	// djangoCachePageRe matches @cache_page(60) on a view. Group 1 = decorated
	// function name. The per-URL page cache region is the request path → dynamic.
	djangoCachePageRe = regexp.MustCompile(
		`(?m)@(?:\w+\.)?cache_page\s*\([^)]*\)\s*\n(?:\s*@[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)
)

// djangoCacheEdge classifies a Django low-level cache verb into (relType, mode).
func djangoCacheEdge(verb string) (relType, mode string) {
	switch verb {
	case "delete":
		return string(types.RelationshipKindInvalidates), "evict"
	case "set", "add", "incr", "decr":
		return string(types.RelationshipKindCaches), "write"
	default: // get, get_or_set
		return string(types.RelationshipKindCaches), "read_through"
	}
}

// cacheRegionRef builds the stable target ref so multiple decorated functions
// that share a region converge on one node (mirrors redisKeyspaceRef).
func cacheRegionRef(framework, region string) string {
	return fmt.Sprintf("cache:%s:%s", framework, region)
}

func (e *CachingExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_caching")
	_, span := tracer.Start(ctx, "custom.python_caching")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	source := string(file.Content)
	if !strings.Contains(source, "cache") && !strings.Contains(source, "cached") {
		return nil, nil
	}

	var out []types.EntityRecord
	seenRegion := make(map[string]bool)
	seenOwner := make(map[string]bool)

	// emitRegion adds one SCOPE.Datastore cache-region node per distinct region
	// (deduplicated across the file). Returns the target ref for the edge.
	emitRegion := func(framework, region string, dynamic bool, line int) string {
		ref := cacheRegionRef(framework, region)
		if !seenRegion[ref] {
			seenRegion[ref] = true
			props := map[string]string{
				"framework":  framework,
				"region":     region,
				"cache_kind": "region",
				"language":   "python",
			}
			if dynamic {
				props["dynamic"] = "true"
			}
			out = append(out, entity(ref, "SCOPE.Datastore", "cache_region", file.Path, line, props))
		}
		return ref
	}

	// emitCache appends the cache-region target (if new) and the decorated-
	// function carrier with its CACHES edge already attached. Region is appended
	// BEFORE the owner so the owner is the last element — no live pointer is held
	// across a slice append (which would reallocate and silently drop the edge).
	// emitEdge is the general form: it appends the region target (if new) and a
	// carrier operation with a relType (CACHES|INVALIDATES) edge to that region.
	// patternType distinguishes the decorator idioms from the imperative
	// low-level Django call-sites. ownerSuffix disambiguates multiple ops that
	// land on the same line (e.g. get then set).
	emitEdge := func(framework, fn, region, mode, relType, patternType, ownerSuffix string, dynamic bool, line int) {
		ownerRef := fmt.Sprintf("cache_fn:%s:%s:%d:%s", framework, file.Path, line, ownerSuffix)
		if seenOwner[ownerRef] {
			return
		}
		seenOwner[ownerRef] = true

		targetRef := emitRegion(framework, region, dynamic, line)

		edgeProps := map[string]string{
			"framework": framework,
			"region":    region,
			"mode":      mode,
			"language":  "python",
		}
		if dynamic {
			edgeProps["dynamic"] = "true"
		}
		owner := entity(ownerRef, "SCOPE.Operation", "cache_method", file.Path, line,
			map[string]string{
				"framework":    framework,
				"cache_mode":   mode,
				"cached_fn":    fn,
				"language":     "python",
				"pattern_type": patternType,
			})
		owner.Relationships = append(owner.Relationships, types.RelationshipRecord{
			ToID:       targetRef,
			Kind:       relType,
			Properties: edgeProps,
		})
		out = append(out, owner)
	}

	// emitCache is the decorator-idiom shorthand: always a CACHES edge.
	emitCache := func(framework, fn, region, mode string, dynamic bool, line int) {
		emitEdge(framework, fn, region, mode, string(types.RelationshipKindCaches),
			"cache_decorator", "", dynamic, line)
	}

	// 1. @lru_cache / @cache — in-process memoisation. Region = function qualname.
	for _, m := range cacheLruRe.FindAllStringSubmatchIndex(source, -1) {
		fn := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		emitCache("lru_cache", fn, "fn:"+fn, "in_process", false, line)
	}

	// 2. @cache.cached(...) — Flask-Caching. Region = key_prefix or dynamic.
	flaskLines := make(map[int]bool)
	for _, m := range cacheFlaskRe.FindAllStringSubmatchIndex(source, -1) {
		body := source[m[2]:m[3]]
		fn := source[m[4]:m[5]]
		line := lineOf(source, m[0])
		flaskLines[line] = true
		region := ""
		dynamic := false
		if km := flaskKeyPrefixRe.FindStringSubmatch(body); km != nil && strings.TrimSpace(km[1]) != "" {
			region = km[1]
		} else {
			region = "<request_path>"
			dynamic = true
		}
		emitCache("flask_caching", fn, region, "read_through", dynamic, line)
	}

	// 3. @cached(...) / @cachetools.cached(...) — cachetools. Region = fn qualname.
	for _, m := range cacheToolsRe.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		if flaskLines[line] {
			continue // already claimed by the Flask `.cached` form.
		}
		fn := source[m[4]:m[5]]
		emitCache("cachetools", fn, "fn:"+fn, "in_process", false, line)
	}

	// 4. Django low-level cache API — cache.get/set/delete/... with a literal
	// key. Same literal key on get/set/delete converges on one region node, so
	// INVALIDATES (delete) maps onto the same node a CACHES (set/get) populates.
	for _, m := range djangoCacheOpRe.FindAllStringSubmatchIndex(source, -1) {
		recv := source[m[2]:m[3]]
		verb := source[m[4]:m[5]]
		line := lineOf(source, m[0])
		region := ""
		dynamic := false
		if m[6] >= 0 && strings.TrimSpace(source[m[6]:m[7]]) != "" {
			region = source[m[6]:m[7]]
		} else {
			region = "<dynamic>"
			dynamic = true
		}
		relType, mode := djangoCacheEdge(verb)
		emitEdge("django", recv+"."+verb, region, mode, relType,
			"cache_call", verb, dynamic, line)
	}

	// 5. @cache_page(...) per-URL page cache. Region is the request path → dynamic.
	for _, m := range djangoCachePageRe.FindAllStringSubmatchIndex(source, -1) {
		fn := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		emitEdge("django", fn, "<request_path>", "read_through",
			string(types.RelationshipKindCaches), "cache_decorator", "cache_page", true, line)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
