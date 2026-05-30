package cpp

// oatpp_routes.go — Oat++ C++ HTTP framework route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. ENDPOINT("GET", "/path", handlerName)
//     — inline controller endpoint macro
//  2. ENDPOINT_ASYNC("POST", "/path", HandlerClass)
//     — async endpoint macro
//  3. ADD_CORS(endpoint, ...)
//     — cross-origin wrapper (path extracted from inner endpoint)
//
// Each matched route emits one SCOPE.Operation/endpoint entity with
// provenance INFERRED_FROM_OATPP_ROUTE.  Handler names are stamped in
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
	extractor.Register("custom_cpp_oatpp", &oatppExtractor{})
}

type oatppExtractor struct{}

func (e *oatppExtractor) Language() string { return "custom_cpp_oatpp" }

var (
	// ENDPOINT("GET", "/path", handlerName)
	// ENDPOINT_ASYNC("POST", "/path", HandlerClass)
	// capture groups: (1) verb, (2) path, (3) handler
	reOatppEndpoint = regexp.MustCompile(
		`(?m)\bENDPOINT(?:_ASYNC)?\s*\(\s*"([A-Z]+)"\s*,\s*"([^"]+)"\s*,\s*([A-Za-z_]\w*)`,
	)
)

func (e *oatppExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.oatpp_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "oatpp"),
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
	if !strings.Contains(src, "ENDPOINT") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	for _, m := range reOatppEndpoint.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(strings.TrimSpace(src[m[2]:m[3]]))
		path := cppNormalizeRoutePath(strings.TrimSpace(src[m[4]:m[5]]))
		handler := strings.TrimSpace(src[m[6]:m[7]])
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
			"provenance", "INFERRED_FROM_OATPP_ROUTE",
			"framework", "oatpp",
		)
		entities = append(entities, ent)
	}

	return entities, nil
}
