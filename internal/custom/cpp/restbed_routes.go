package cpp

// restbed_routes.go — Restbed C++ HTTP framework route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. auto resource = make_shared<Resource>();
//     resource->set_path("/path");
//     resource->set_method_handler("GET", handler);
//
//  2. resource.set_path("/path");
//     resource.set_method_handler("POST", handler);
//
// The extractor tracks set_path() → set_method_handler() associations within
// the same file when the same Resource variable is used.
//
// Status: partial (regex/heuristic; no AST; same-file variable correlation).

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
	extractor.Register("custom_cpp_restbed", &restbedExtractor{})
}

type restbedExtractor struct{}

func (e *restbedExtractor) Language() string { return "custom_cpp_restbed" }

var (
	// resource->set_path("/path") or resource.set_path("/path")
	// capture: (1) variable name, (2) path
	reRestbedSetPath = regexp.MustCompile(
		`(?m)(\w+)\s*(?:->|\.)\s*set_path\s*\(\s*"([^"]+)"`,
	)

	// resource->set_method_handler("GET", handler)
	// capture: (1) variable name, (2) verb, (3) handler
	reRestbedSetMethodHandler = regexp.MustCompile(
		`(?m)(\w+)\s*(?:->|\.)\s*set_method_handler\s*\(\s*"([A-Z]+)"\s*,\s*([^)]+)\)`,
	)
)

func (e *restbedExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.restbed_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "restbed"),
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
	if !strings.Contains(src, "set_path") && !strings.Contains(src, "set_method_handler") {
		return nil, nil
	}

	// Build var → path map from set_path() calls.
	varPaths := make(map[string]string)
	for _, m := range reRestbedSetPath.FindAllStringSubmatchIndex(src, -1) {
		varName := strings.TrimSpace(src[m[2]:m[3]])
		path := strings.TrimSpace(src[m[4]:m[5]])
		varPaths[varName] = path
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	for _, m := range reRestbedSetMethodHandler.FindAllStringSubmatchIndex(src, -1) {
		varName := strings.TrimSpace(src[m[2]:m[3]])
		verb := strings.ToUpper(strings.TrimSpace(src[m[4]:m[5]]))
		handler := strings.TrimSpace(src[m[6]:m[7]])
		// Trim trailing noise from handler
		if idx := strings.IndexAny(handler, " \t\r\n,)"); idx > 0 {
			handler = handler[:idx]
		}

		path := varPaths[varName]
		if path == "" {
			path = "<" + varName + ">"
		} else {
			path = cppNormalizeRoutePath(path)
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
			"provenance", "INFERRED_FROM_RESTBED_ROUTE",
			"framework", "restbed",
		)
		entities = append(entities, ent)
	}

	return entities, nil
}
