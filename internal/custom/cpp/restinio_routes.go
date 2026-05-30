package cpp

// restinio_routes.go — RESTinio C++ HTTP framework route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. router->http_get("/path", handler)
//     router->http_post("/path", handler)
//     ... (http_put, http_delete, http_patch, http_head, http_options)
//
//  2. router->add_handler(restinio::http_connection_header_t::GET, "/path", handler)
//     — explicit verb form
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
	extractor.Register("custom_cpp_restinio", &restinioExtractor{})
}

type restinioExtractor struct{}

func (e *restinioExtractor) Language() string { return "custom_cpp_restinio" }

// restinioVerbMap maps http_<verb> method names to HTTP verbs.
var restinioVerbMap = map[string]string{
	"http_get":     "GET",
	"http_post":    "POST",
	"http_put":     "PUT",
	"http_delete":  "DELETE",
	"http_patch":   "PATCH",
	"http_head":    "HEAD",
	"http_options": "OPTIONS",
}

var (
	// router->http_get("/path", handler) or router.http_get("/path", handler)
	// capture: (1) method name (http_get etc.), (2) path, (3) handler
	reRestinioHTTPMethod = regexp.MustCompile(
		`(?m)\w+\s*(?:->|\.)\s*(http_(?:get|post|put|delete|patch|head|options))\s*\(\s*"([^"]+)"\s*,\s*([^)]+)\)`,
	)

	// router->add_handler(restinio::http_connection_header_t::GET, "/path", handler)
	// capture: (1) verb, (2) path, (3) handler
	reRestinioAddHandler = regexp.MustCompile(
		`(?m)\w+\s*(?:->|\.)\s*add_handler\s*\(\s*(?:\w+::)*([A-Z]+)\s*,\s*"([^"]+)"\s*,\s*([^)]+)\)`,
	)
)

func (e *restinioExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.restinio_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "restinio"),
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
	if !strings.Contains(src, "http_get") && !strings.Contains(src, "http_post") &&
		!strings.Contains(src, "add_handler") && !strings.Contains(src, "http_put") &&
		!strings.Contains(src, "http_delete") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// http_<verb>(path, handler) style
	for _, m := range reRestinioHTTPMethod.FindAllStringSubmatchIndex(src, -1) {
		methodName := strings.TrimSpace(src[m[2]:m[3]])
		path := cppNormalizeRoutePath(strings.TrimSpace(src[m[4]:m[5]]))
		handler := strings.TrimSpace(src[m[6]:m[7]])
		if idx := strings.IndexAny(handler, " \t\r\n,)"); idx > 0 {
			handler = handler[:idx]
		}
		verb := restinioVerbMap[methodName]
		if verb == "" {
			verb = strings.ToUpper(strings.TrimPrefix(methodName, "http_"))
		}
		name := verb + " " + path
		if seen[name] {
			continue
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"http_method", verb,
			"route_path", path,
			"handler_name", handler,
			"provenance", "INFERRED_FROM_RESTINIO_ROUTE",
			"framework", "restinio",
		)
		entities = append(entities, ent)
	}

	// add_handler(VERB, path, handler) style
	for _, m := range reRestinioAddHandler.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(strings.TrimSpace(src[m[2]:m[3]]))
		path := cppNormalizeRoutePath(strings.TrimSpace(src[m[4]:m[5]]))
		handler := strings.TrimSpace(src[m[6]:m[7]])
		if idx := strings.IndexAny(handler, " \t\r\n,)"); idx > 0 {
			handler = handler[:idx]
		}
		name := verb + " " + path
		if seen[name] {
			continue
		}
		seen[name] = true
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"http_method", verb,
			"route_path", path,
			"handler_name", handler,
			"provenance", "INFERRED_FROM_RESTINIO_ROUTE",
			"framework", "restinio",
		)
		entities = append(entities, ent)
	}

	return entities, nil
}
