package cpp

// poco_routes.go — POCO C++ Libraries HTTP server route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. addHandler<HandlerClass>("/path")
//     — registers an HTTPRequestHandler subclass at a path
//  2. router.add(HTTPRequest::HTTP_GET, "/path", new HandlerFactory())
//     — explicit verb + path registration on HTTPRouter
//  3. server.addHandler("/path", handler)
//     — path-based registration shorthand
//
// Each matched route emits one SCOPE.Operation/endpoint entity with
// provenance INFERRED_FROM_POCO_ROUTE.
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
	extractor.Register("custom_cpp_poco", &pocoExtractor{})
}

type pocoExtractor struct{}

func (e *pocoExtractor) Language() string { return "custom_cpp_poco" }

var (
	// addHandler<HandlerClass>("/path")
	// capture: (1) handler class, (2) path
	rePocoAddHandlerTemplate = regexp.MustCompile(
		`(?m)\baddHandler\s*<\s*([A-Za-z_]\w*)\s*>\s*\(\s*"([^"]+)"`,
	)

	// router.add(HTTPRequest::HTTP_GET, "/path", ...)
	// capture: (1) verb constant (HTTP_GET → GET), (2) path
	rePocoRouterAdd = regexp.MustCompile(
		`(?m)\brouter\s*\.\s*add\s*\(\s*HTTPRequest\s*::\s*HTTP_([A-Z]+)\s*,\s*"([^"]+)"`,
	)

	// server.addHandler("/path", handler) — catch-all style
	// capture: (1) path, (2) handler
	rePocoServerAddHandler = regexp.MustCompile(
		`(?m)\bserver\s*\.\s*addHandler\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_*][A-Za-z0-9_:*\s]*)`,
	)
)

func (e *pocoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.poco_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "poco"),
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
	if !strings.Contains(src, "addHandler") && !strings.Contains(src, "router") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// Template-style handler registration
	for _, m := range rePocoAddHandlerTemplate.FindAllStringSubmatchIndex(src, -1) {
		handler := strings.TrimSpace(src[m[2]:m[3]])
		path := cppNormalizeRoutePath(strings.TrimSpace(src[m[4]:m[5]]))
		name := "ANY " + path
		if seen[name] {
			continue
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"http_method", "ANY",
			"route_path", path,
			"handler_name", handler,
			"provenance", "INFERRED_FROM_POCO_ROUTE",
			"framework", "poco",
		)
		entities = append(entities, ent)
	}

	// Router.add() with explicit verb
	for _, m := range rePocoRouterAdd.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(strings.TrimSpace(src[m[2]:m[3]]))
		path := cppNormalizeRoutePath(strings.TrimSpace(src[m[4]:m[5]]))
		name := verb + " " + path
		if seen[name] {
			continue
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"http_method", verb,
			"route_path", path,
			"handler_name", "<factory>",
			"provenance", "INFERRED_FROM_POCO_ROUTE",
			"framework", "poco",
		)
		entities = append(entities, ent)
	}

	// server.addHandler(path, handler)
	for _, m := range rePocoServerAddHandler.FindAllStringSubmatchIndex(src, -1) {
		path := cppNormalizeRoutePath(strings.TrimSpace(src[m[2]:m[3]]))
		handler := strings.TrimSpace(src[m[4]:m[5]])
		// Trim trailing noise (e.g. trailing ")") from handler
		if idx := strings.IndexAny(handler, " \t\r\n,)"); idx > 0 {
			handler = handler[:idx]
		}
		name := "ANY " + path
		if seen[name] {
			continue
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"http_method", "ANY",
			"route_path", path,
			"handler_name", handler,
			"provenance", "INFERRED_FROM_POCO_ROUTE",
			"framework", "poco",
		)
		entities = append(entities, ent)
	}

	return entities, nil
}
