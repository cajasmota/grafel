package cpp

// crow_routes.go — Crow C++ HTTP framework route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. CROW_ROUTE(app, "/path")(handler)
//     — basic route, verb defaults to GET
//  2. CROW_ROUTE(app, "/path").methods("GET"_method, "POST"_method)(handler)
//     — explicit method list
//  3. CROW_BP_ROUTE(bp, "/path")(handler)      — blueprint route (same shape)
//  4. CROW_CATCHALL_ROUTE(app)(handler)         — catch-all, path "*"
//
// Each matched route emits one SCOPE.Operation/endpoint entity with
// provenance INFERRED_FROM_CROW_ROUTE.  Handler names are stamped in
// handler_name to support handler_attribution.
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
	extractor.Register("custom_cpp_crow", &crowExtractor{})
}

type crowExtractor struct{}

func (e *crowExtractor) Language() string { return "custom_cpp_crow" }

var (
	// CROW_ROUTE(app, "/path")          optional .methods("GET"_method)(handler)
	// CROW_BP_ROUTE(bp, "/path")        optional .methods("GET"_method)(handler)
	//
	// We capture:
	//   group 1: path string
	//   group 2: optional methods list (content inside .methods(...))
	//   group 3: handler identifier (first token inside the trailing call parens)
	reCrowRoute = regexp.MustCompile(
		`(?m)\bCROW(?:_BP)?_ROUTE\s*\(\s*\w+\s*,\s*"([^"]+)"\s*\)` +
			`(?:\s*\.methods\s*\(([^)]+)\))?` +
			`\s*\(\s*([A-Za-z_&][A-Za-z0-9_:&\s]*)`,
	)
	// CROW_CATCHALL_ROUTE(app)(handler)
	reCrowCatchAll = regexp.MustCompile(
		`(?m)\bCROW_CATCHALL_ROUTE\s*\(\s*\w+\s*\)\s*\(\s*([A-Za-z_&][A-Za-z0-9_:&\s]*)`,
	)

	// "_method" suffix used in Crow method literals — "GET"_method → GET
	reCrowMethodLiteral = regexp.MustCompile(`"([A-Z]+)"_method`)
)

// crowVerbs parses the methods(...) argument list, e.g.:
//
//	"GET"_method,"POST"_method  →  "GET,POST"
//	(empty)                     →  "GET"  (Crow default)
func crowVerbs(methodsRaw string) string {
	if strings.TrimSpace(methodsRaw) == "" {
		return "GET"
	}
	var verbs []string
	for _, m := range reCrowMethodLiteral.FindAllStringSubmatch(methodsRaw, -1) {
		verbs = append(verbs, m[1])
	}
	if len(verbs) == 0 {
		return "ANY"
	}
	return strings.Join(verbs, ",")
}

func (e *crowExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.crow_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "crow"),
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
	if !strings.Contains(src, "CROW_ROUTE") && !strings.Contains(src, "CROW_BP_ROUTE") &&
		!strings.Contains(src, "CROW_CATCHALL_ROUTE") {
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

	// 1. CROW_ROUTE / CROW_BP_ROUTE
	for _, m := range reCrowRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		methodsRaw := ""
		if m[4] >= 0 {
			methodsRaw = src[m[4]:m[5]]
		}
		handlerRaw := strings.TrimSpace(src[m[6]:m[7]])
		// strip trailing whitespace/newlines captured by the lazy group
		if idx := strings.IndexAny(handlerRaw, " \t\r\n)"); idx > 0 {
			handlerRaw = handlerRaw[:idx]
		}
		handler := strings.TrimLeft(handlerRaw, "&")

		methods := crowVerbs(methodsRaw)
		path = cppNormalizeRoutePath(path)
		name := methods + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "crow",
			"provenance", "INFERRED_FROM_CROW_ROUTE",
			"http_method", methods,
			"route_path", path,
			"handler_name", handler,
			"dsl", "CROW_ROUTE",
		)
		add(ent)
	}

	// 2. CROW_CATCHALL_ROUTE
	for _, m := range reCrowCatchAll.FindAllStringSubmatchIndex(src, -1) {
		handlerRaw := strings.TrimSpace(src[m[2]:m[3]])
		if idx := strings.IndexAny(handlerRaw, " \t\r\n)"); idx > 0 {
			handlerRaw = handlerRaw[:idx]
		}
		handler := strings.TrimLeft(handlerRaw, "&")

		name := "ANY *"
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "crow",
			"provenance", "INFERRED_FROM_CROW_ROUTE",
			"http_method", "ANY",
			"route_path", "*",
			"handler_name", handler,
			"dsl", "CROW_CATCHALL_ROUTE",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
