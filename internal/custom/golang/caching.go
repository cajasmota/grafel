package golang

// caching.go — Go in-process cache-topology extraction ([cache] breadth child
// of epic #3628). Cross-language consistent with the Spring cache-region node
// (internal/custom/java/caching.go), the Python cache-region node
// (internal/custom/python/caching.go), the Rails cache-region node
// (internal/custom/ruby/caching.go), and the JS cache-region node
// (internal/custom/javascript/caching.go). Two dominant Go cache abstractions:
//
//	ristretto (dgraph-io/ristretto):
//	  cache.Set("user:1", v, 1)   → call-site CACHES region "user:1"  (write)
//	  cache.Get("user:1")         → CACHES region "user:1"            (read)
//	  cache.Del("user:1")         → INVALIDATES region "user:1"       (evict)
//
//	groupcache (golang/groupcache):
//	  groupcache.NewGroup("users", ...) → declares region "users"
//	  group.Get(ctx, "user:1", dest)    → CACHES region "user:1"      (read)
//
// Entity/edge shape (identical to the other languages so a Set and a Del on the
// same literal key converge on ONE region node — the whole value of the model):
//
//	target  : SCOPE.Datastore  subtype "cache_region"
//	          Ref  "cache:<framework>:<region>"   (sites converge on one node)
//	carrier : SCOPE.Operation  the cache call-site
//	edge    : CACHES | INVALIDATES   carrier → region
//
// Honest-partial: a non-literal first argument (a variable / interpolated key)
// is dynamic — the edge is emitted against a "<dynamic>" region with
// dynamic="true" so the intent stays traversable, mirroring the existing
// JS/Rails convention. A non-cache `.Get`/`.Set` on an unrelated map/struct is
// NOT matched: the file must import a known cache library (ristretto or
// groupcache) for any call-site to count.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_go_caching", &goCachingExtractor{})
}

type goCachingExtractor struct{}

func (e *goCachingExtractor) Language() string { return "custom_go_caching" }

var (
	// reGoCacheOp matches a ristretto-style cache op. Group 1 = receiver
	// (any identifier — gated on a cache import); Group 2 = verb; Group 3 = the
	// first-argument string literal body, which is OPTIONAL: a variable key
	// (`cache.Set(k, v, 1)`) matches with group 3 absent → honest-partial. The
	// receiver is intentionally broad because ristretto caches are plain values
	// (`cache.Set(...)`); the import gate below prevents map/struct false hits.
	reGoCacheOp = regexp.MustCompile(
		`\b(\w+)\.(Set|Get|Del|Delete)\s*\(\s*(?:"([^"]*)")?`)

	// reGroupGet matches a groupcache `group.Get(ctx, "key", dest)` read — the
	// key is the SECOND argument (the first is a context). Group 1 = receiver;
	// Group 2 = the optional key literal body (absent when the key is dynamic).
	reGroupGet = regexp.MustCompile(
		`\b(\w+)\.Get\s*\(\s*[A-Za-z_]\w*\s*,\s*(?:"([^"]*)")?`)

	// reGroupNew matches groupcache.NewGroup("users", ...) — declares a region.
	// Group 1 = the region name literal body.
	reGroupNew = regexp.MustCompile(
		`groupcache\.NewGroup\s*\(\s*"([^"]*)"`)
)

func goCacheRef(framework, region string) string {
	return "cache:" + framework + ":" + region
}

// goCacheEdge classifies a ristretto verb into (relType, mode).
func goCacheEdge(verb string) (relType, mode string) {
	switch verb {
	case "Del", "Delete":
		return "INVALIDATES", "evict"
	case "Set":
		return "CACHES", "write"
	default: // Get
		return "CACHES", "read"
	}
}

func (e *goCachingExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.go_caching")
	_, span := tracer.Start(ctx, "custom.go_caching")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Import gate: a Go file only participates if it actually pulls in a known
	// in-process cache library. This is what stops `someMap.Get("x")` /
	// `cfg.Set("x")` on unrelated values from emitting phantom cache edges.
	hasRistretto := strings.Contains(src, "ristretto")
	hasGroupcache := strings.Contains(src, "groupcache")
	if !hasRistretto && !hasGroupcache {
		return nil, nil
	}

	var out []types.EntityRecord
	seenRegion := make(map[string]bool)
	seenOwner := make(map[string]bool)

	emitRegion := func(framework, region string, dynamic bool, line int) string {
		ref := goCacheRef(framework, region)
		if !seenRegion[ref] {
			seenRegion[ref] = true
			ent := makeEntity(region, "SCOPE.Datastore", "cache_region", file.Path, file.Language, line)
			setProps(&ent, "framework", framework, "region", region,
				"cache_kind", "region", "language", "go",
				"provenance", "INFERRED_FROM_GO_CACHE")
			if dynamic {
				setProps(&ent, "dynamic", "true")
			}
			out = append(out, ent)
		}
		return ref
	}

	// emitOp appends the carrier call-site with its CACHES/INVALIDATES edge.
	emitOp := func(framework, carrier, region, relType, mode string, dynamic bool, line int) {
		ownerKey := framework + ":" + carrier + ":" + region + ":" + itoa(line)
		if seenOwner[ownerKey] {
			return
		}
		seenOwner[ownerKey] = true

		ref := emitRegion(framework, region, dynamic, line)
		ent := makeEntity(carrier, "SCOPE.Operation", "cache_method", file.Path, file.Language, line)
		setProps(&ent, "framework", framework, "cache_mode", mode,
			"provenance", "INFERRED_FROM_GO_CACHE")
		if dynamic {
			setProps(&ent, "dynamic", "true")
		}
		edgeProps := map[string]string{
			"framework": framework, "region": region, "mode": mode, "language": "go",
		}
		if dynamic {
			edgeProps["dynamic"] = "true"
		}
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: ref, Kind: relType, Properties: edgeProps,
		})
		out = append(out, ent)
	}

	// 0. groupcache.NewGroup("users", ...) — declares a region up front so a
	// later group.Get against a dynamic key still converges on the named region
	// (best-effort: the read key, not the group name, is the convergence unit,
	// but a declared region is worth recording as a node).
	if hasGroupcache {
		for _, m := range reGroupNew.FindAllStringSubmatchIndex(src, -1) {
			name := submatch(src, m, 2)
			if strings.TrimSpace(name) == "" {
				continue
			}
			emitRegion("groupcache", name, false, lineOf(src, m[0]))
		}

		// groupcache reads: group.Get(ctx, "key", dest).
		for _, m := range reGroupGet.FindAllStringSubmatchIndex(src, -1) {
			recv := submatch(src, m, 2)
			rawKey := submatch(src, m, 4)
			region, dynamic := normaliseGoCacheKey(rawKey, m[4] >= 0)
			emitOp("groupcache", recv+".Get", region, "CACHES", "read", dynamic, lineOf(src, m[0]))
		}
	}

	// 1. ristretto-style Set/Get/Del with a literal first-argument key.
	if hasRistretto {
		for _, m := range reGoCacheOp.FindAllStringSubmatchIndex(src, -1) {
			recv := submatch(src, m, 2)
			verb := submatch(src, m, 4)
			rawKey := submatch(src, m, 6)
			region, dynamic := normaliseGoCacheKey(rawKey, m[6] >= 0)
			relType, mode := goCacheEdge(verb)
			emitOp("ristretto", recv+"."+verb, region, relType, mode, dynamic, lineOf(src, m[0]))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// normaliseGoCacheKey resolves the captured first-argument key. matched reports
// whether a string-literal group actually participated (vs. a variable key,
// where the literal group did not match at all). A literal "user:1" is a
// concrete region; a non-literal key is honest-partial "<dynamic>".
func normaliseGoCacheKey(raw string, matched bool) (region string, dynamic bool) {
	if !matched || strings.TrimSpace(raw) == "" {
		return "<dynamic>", true
	}
	return raw, false
}
