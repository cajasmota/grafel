package cpp

// pistache_routes.go — Pistache C++ HTTP framework route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. Routes::Get(router, "/path", Routes::bind(&Handler::method))
//  2. Routes::Post(router, "/path", Routes::bind(&Handler::method))
//     … and all other HTTP verbs (Put, Delete, Patch, Head, Options, Any)
//  3. router.get("/path", Routes::bind(&Handler::method))
//     … method-shorthand form on a Rest::Router instance
//
// Each matched route emits one SCOPE.Operation/endpoint entity with
// provenance INFERRED_FROM_PISTACHE_ROUTE.  Handler names are extracted from
// Routes::bind() and stamped in handler_name.
//
// Status: partial (regex/heuristic; no AST).

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_pistache", &pistacheExtractor{})
}

type pistacheExtractor struct{}

func (e *pistacheExtractor) Language() string { return "custom_cpp_pistache" }

// Pistache HTTP verbs as seen in Routes:: static calls.
var pistacheVerbSet = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Delete": true,
	"Patch": true, "Head": true, "Options": true, "Any": true,
}

var (
	// Routes::Get(router, "/path", Routes::bind(&Handler::method))
	// Routes::Post(router, "/path", handler)
	//
	// capture groups: (1) verb, (2) path, (3) handler expression
	rePistacheStaticRoute = regexp.MustCompile(
		`(?m)\bRoutes\s*::\s*(Get|Post|Put|Delete|Patch|Head|Options|Any)\s*\(` +
			`\s*\w+\s*,\s*"([^"]+)"\s*,\s*([^)]+)\)`,
	)

	// router.get("/path", Routes::bind(&Handler::method))
	// Only match method names that map to known verbs (case-insensitive first char).
	rePistacheInstanceRoute = regexp.MustCompile(
		`(?m)\.(?i)(get|post|put|delete|patch|head|options|any)\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
	)

	// Routes::bind(&Handler::method) — extract the handler pointer
	rePistacheBind = regexp.MustCompile(
		`Routes\s*::\s*bind\s*\(\s*&?\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
)

// pistacheHandler extracts the handler name from an expression like
// "Routes::bind(&Handler::method)" or a plain identifier "handler".
func pistacheHandler(expr string) string {
	expr = strings.TrimSpace(expr)
	if m := rePistacheBind.FindStringSubmatch(expr); m != nil {
		return m[1]
	}
	// Strip leading & and trailing noise
	expr = strings.TrimLeft(expr, "&")
	if idx := strings.IndexAny(expr, " \t\r\n,)"); idx > 0 {
		expr = expr[:idx]
	}
	return expr
}

func (e *pistacheExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.pistache_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "pistache"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "Routes::") && !strings.Contains(src, "Rest::Router") &&
		!strings.Contains(src, "Pistache") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. Routes::Get(router, "/path", handler)
	for _, m := range rePistacheStaticRoute.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[m[2]:m[3]])
		path := cppNormalizeRoutePath(src[m[4]:m[5]])
		handlerExpr := src[m[6]:m[7]]
		handler := pistacheHandler(handlerExpr)

		name := verb + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "pistache",
			"provenance", "INFERRED_FROM_PISTACHE_ROUTE",
			"http_method", verb,
			"route_path", path,
			"handler_name", handler,
			"dsl", "Routes::"+src[m[2]:m[3]],
		)
		add(ent)
	}

	// 2. router.get("/path", handler)
	for _, m := range rePistacheInstanceRoute.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[m[2]:m[3]])
		path := cppNormalizeRoutePath(src[m[4]:m[5]])
		handlerExpr := src[m[6]:m[7]]
		handler := pistacheHandler(handlerExpr)

		name := verb + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "pistache",
			"provenance", "INFERRED_FROM_PISTACHE_ROUTE",
			"http_method", verb,
			"route_path", path,
			"handler_name", handler,
			"dsl", "router.method",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
