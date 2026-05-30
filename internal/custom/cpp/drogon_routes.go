package cpp

// drogon_routes.go — Drogon C++ HTTP framework route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. ADD_METHOD_TO(Controller::method, "/path", Get[, Post, ...])
//  2. METHOD_ADD(Controller, "/path", method[, method...])
//  3. app().registerHandler("/path", handler, {Get[, Post, ...]})
//
// Each matched route emits one SCOPE.Operation/endpoint entity with
// provenance INFERRED_FROM_DROGON_ROUTE.  Handler names are stamped in the
// handler_name property to support handler_attribution.
//
// Status: partial (regex/heuristic; no AST).  A proving fixture per pattern
// lives in extractors_test.go.

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
	extractor.Register("custom_cpp_drogon", &drogonExtractor{})
}

type drogonExtractor struct{}

func (e *drogonExtractor) Language() string { return "custom_cpp_drogon" }

var (
	// ADD_METHOD_TO(Controller::method, "/path", Get)
	// ADD_METHOD_TO(Controller::method, "/path", Get, Post)
	reDrogonAddMethodTo = regexp.MustCompile(
		`(?m)\bADD_METHOD_TO\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)+)\s*,\s*"([^"]+)"\s*,\s*([^)]+)\)`,
	)
	// METHOD_ADD(Controller, "/path", Get)
	reDrogonMethodAdd = regexp.MustCompile(
		`(?m)\bMETHOD_ADD\s*\(\s*([A-Za-z_]\w*)\s*,\s*"([^"]+)"\s*,\s*([^)]+)\)`,
	)
	// app().registerHandler("/path", handler, {Get})
	// app().registerHandler("/path", handler, {Get, Post})
	reDrogonRegisterHandler = regexp.MustCompile(
		`(?m)\bregisterHandler\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)\s*,\s*\{([^}]+)\}`,
	)
)

// drogonVerbs normalises the method list string from the macro to a
// comma-separated, upper-cased verb list (e.g. "Get,Post" → "GET,POST").
func drogonVerbs(raw string) string {
	parts := strings.Split(raw, ",")
	var verbs []string
	for _, p := range parts {
		v := strings.TrimSpace(p)
		// Strip drogon:: prefix if present
		if idx := strings.LastIndex(v, "::"); idx >= 0 {
			v = v[idx+2:]
		}
		v = strings.ToUpper(v)
		if v == "" {
			continue
		}
		verbs = append(verbs, v)
	}
	if len(verbs) == 0 {
		return "ANY"
	}
	return strings.Join(verbs, ",")
}

func (e *drogonExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.drogon_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "drogon"),
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
	// Cheap pre-filter.
	if !strings.Contains(src, "ADD_METHOD_TO") &&
		!strings.Contains(src, "METHOD_ADD") &&
		!strings.Contains(src, "registerHandler") {
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

	// 1. ADD_METHOD_TO(Controller::method, "/path", Get[, Post...])
	for _, m := range reDrogonAddMethodTo.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		path := cppNormalizeRoutePath(src[m[4]:m[5]])
		methods := drogonVerbs(src[m[6]:m[7]])
		name := methods + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_ROUTE",
			"http_method", methods,
			"route_path", path,
			"handler_name", handler,
			"dsl", "ADD_METHOD_TO",
		)
		add(ent)
	}

	// 2. METHOD_ADD(Controller, "/path", Get)
	for _, m := range reDrogonMethodAdd.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		path := cppNormalizeRoutePath(src[m[4]:m[5]])
		methods := drogonVerbs(src[m[6]:m[7]])
		name := methods + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_ROUTE",
			"http_method", methods,
			"route_path", path,
			"handler_name", handler,
			"dsl", "METHOD_ADD",
		)
		add(ent)
	}

	// 3. app().registerHandler("/path", handler, {Get, Post})
	for _, m := range reDrogonRegisterHandler.FindAllStringSubmatchIndex(src, -1) {
		path := cppNormalizeRoutePath(src[m[2]:m[3]])
		handler := src[m[4]:m[5]]
		methods := drogonVerbs(src[m[6]:m[7]])
		name := methods + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "drogon",
			"provenance", "INFERRED_FROM_DROGON_ROUTE",
			"http_method", methods,
			"route_path", path,
			"handler_name", handler,
			"dsl", "registerHandler",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
