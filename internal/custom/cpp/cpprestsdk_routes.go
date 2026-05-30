package cpp

// cpprestsdk_routes.go — cpprestsdk (Casablanca) C++ HTTP route/handler extractor.
//
// Covered DSL surfaces:
//
//  1. listener.support(methods::GET, handler)
//     — direct method→handler registration on an http_listener
//  2. listener.support(handler)
//     — catch-all registration (verb ANY)
//  3. router.support(methods::GET, handler)  (same API on http_listener aliases)
//
// Each matched route emits one SCOPE.Operation/endpoint entity with
// provenance INFERRED_FROM_CPPRESTSDK_ROUTE.  Handler identifiers are stamped
// in handler_name to support handler_attribution.
//
// cpprestsdk uses listener construction to declare the base URL (path), but
// the path is typically a runtime variable, not a string literal at the
// .support() call site.  We therefore record path as "<listener>" to indicate
// that the full URL is bound at listener construction time.  When the listener
// variable is declared nearby with a string literal we capture it via
// reCppRestListenerInit.
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
	extractor.Register("custom_cpp_cpprestsdk", &cppRestSDKExtractor{})
}

type cppRestSDKExtractor struct{}

func (e *cppRestSDKExtractor) Language() string { return "custom_cpp_cpprestsdk" }

var (
	// listener.support(methods::GET, handler)
	// Capture: (1) receiver var, (2) verb (methods::GET → GET), (3) handler
	reCppRestSupport = regexp.MustCompile(
		`(?m)(\w+)\s*\.\s*support\s*\(\s*methods\s*::\s*([A-Z_]+)\s*,\s*([^)]+)\)`,
	)

	// listener.support(handler)  — no verb argument → ANY
	reCppRestSupportAny = regexp.MustCompile(
		`(?m)(\w+)\s*\.\s*support\s*\(\s*([A-Za-z_&][A-Za-z0-9_:&\s]*)\)`,
	)

	// http_listener listener("http://0.0.0.0:8080/api/v1");
	// web::http::experimental::listener::http_listener listener(U("/path"));
	// Capture: (1) variable name, (2) URL/path string
	reCppRestListenerInit = regexp.MustCompile(
		`(?m)http_listener\s+(\w+)\s*\(\s*(?:U\s*\(\s*)?['""]([^'"")]+)['""]`,
	)
)

func (e *cppRestSDKExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpprestsdk_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "cpprestsdk"),
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
	if !strings.Contains(src, ".support(") && !strings.Contains(src, "http_listener") {
		return nil, nil
	}

	// Build a listener-var → path map from declarations in this file.
	listenerPaths := make(map[string]string)
	for _, m := range reCppRestListenerInit.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		urlPath := cppNormalizeRoutePath(src[m[4]:m[5]])
		listenerPaths[varName] = urlPath
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

	// Track which receiver vars matched the verb form so we don't double-emit.
	verbMatchedReceivers := make(map[int]bool)

	// 1. listener.support(methods::GET, handler)
	for _, m := range reCppRestSupport.FindAllStringSubmatchIndex(src, -1) {
		verbMatchedReceivers[m[0]] = true
		receiverVar := src[m[2]:m[3]]
		verb := strings.ToUpper(src[m[4]:m[5]])
		handlerExpr := strings.TrimSpace(src[m[6]:m[7]])
		if idx := strings.IndexAny(handlerExpr, " \t\r\n)"); idx > 0 {
			handlerExpr = handlerExpr[:idx]
		}
		handler := strings.TrimLeft(handlerExpr, "&")

		path := listenerPaths[receiverVar]
		if path == "" {
			path = "<" + receiverVar + ">"
		}

		name := verb + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "cpprestsdk",
			"provenance", "INFERRED_FROM_CPPRESTSDK_ROUTE",
			"http_method", verb,
			"route_path", path,
			"handler_name", handler,
			"listener_var", receiverVar,
			"dsl", "support",
		)
		add(ent)
	}

	// 2. listener.support(handler) — catch-all, but only when no verb form matched
	//    at this position (to avoid re-processing the verb form's receiver).
	for _, m := range reCppRestSupportAny.FindAllStringSubmatchIndex(src, -1) {
		if verbMatchedReceivers[m[0]] {
			continue
		}
		receiverVar := src[m[2]:m[3]]
		handlerExpr := strings.TrimSpace(src[m[4]:m[5]])
		// If the first argument looks like methods::XXX this was already matched.
		if strings.Contains(handlerExpr, "methods::") {
			continue
		}
		if idx := strings.IndexAny(handlerExpr, " \t\r\n)"); idx > 0 {
			handlerExpr = handlerExpr[:idx]
		}
		handler := strings.TrimLeft(handlerExpr, "&")

		path := listenerPaths[receiverVar]
		if path == "" {
			path = "<" + receiverVar + ">"
		}

		name := "ANY " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "cpprestsdk",
			"provenance", "INFERRED_FROM_CPPRESTSDK_ROUTE",
			"http_method", "ANY",
			"route_path", path,
			"handler_name", handler,
			"listener_var", receiverVar,
			"dsl", "support",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
