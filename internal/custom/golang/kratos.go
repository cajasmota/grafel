package golang

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
	extractor.Register("custom_go_kratos", &kratosExtractor{})
}

// kratosExtractor extracts routing structure from go-kratos
// (github.com/go-kratos/kratos/v2) services. Kratos is proto/codegen-driven:
// the protoc-gen-go-http plugin generates a `*_http.pb.go` file per service
// containing a `RegisterXxxHTTPServer(s, srv)` function whose body wires the
// transport verb calls against a router obtained from `s.Route(...)` —
//
//	func RegisterGreeterHTTPServer(s *http.Server, srv GreeterHTTPServer) {
//		r := s.Route("/")
//		r.GET("/helloworld/{name}", _Greeter_SayHello0_HTTP_Handler(srv))
//	}
//
// Each `r.GET/POST/...("/path", _Svc_Method0_HTTP_Handler(srv))` registration
// yields an endpoint. The handler is the generated `_Svc_Method_HTTP_Handler`
// wrapper, from which the underlying service method name is recovered for
// handler attribution. The `RegisterXxxHTTPServer` function itself is recorded
// as the service-registration scope.
//
// Honesty note: this targets the *generated* `*_http.pb.go` output. When that
// file is present in the repo (the common committed-codegen case) routes and
// handlers resolve fully from a single statically-analysable AST shape — the
// proving fixture exercises exactly this. When only the `.proto` source is
// present and the generated file is absent, no registration sites exist to
// detect; that is an inherent limit of the proto-only layout, not a heuristic
// gap.
type kratosExtractor struct{}

func (e *kratosExtractor) Language() string { return "custom_go_kratos" }

var (
	// func RegisterGreeterHTTPServer(s *http.Server, srv GreeterHTTPServer) {
	// Captures the service token (e.g. "Greeter") from the generated
	// registration entry point.
	reKratosRegister = regexp.MustCompile(
		`(?m)func\s+Register(\w+?)HTTPServer\s*\(`,
	)
	// r := s.Route("/") — router handle obtained from the *http.Server inside a
	// RegisterXxxHTTPServer body. The optional prefix becomes a route prefix.
	reKratosRoute = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Route\s*\(\s*"([^"]*)"`,
	)
	// r.GET("/helloworld/{name}", _Greeter_SayHello0_HTTP_Handler(srv))
	// verb registration with a generated `_Svc_Method<idx>_HTTP_Handler`
	// handler. The handler identifier is captured whole for attribution.
	reKratosVerb = regexp.MustCompile(
		`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_]\w*)`,
	)
	// _Greeter_SayHello0_HTTP_Handler -> service "Greeter", method "SayHello".
	// The trailing numeric suffix is protoc-gen-go-http's per-binding index.
	reKratosHandler = regexp.MustCompile(
		`^_(\w+?)_(\w+?)(\d+)_HTTP_Handler$`,
	)
)

// kratosMethodFromHandler recovers the underlying service method name from a
// generated `_Svc_Method<idx>_HTTP_Handler` wrapper identifier. Returns "" when
// the identifier is not a generated kratos handler wrapper.
func kratosMethodFromHandler(handler string) string {
	m := reKratosHandler.FindStringSubmatch(handler)
	if m == nil {
		return ""
	}
	return m[2]
}

func (e *kratosExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.kratos_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "kratos"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	// Gate on the generated-HTTP-transport signature: the registration entry
	// point + the generated handler wrapper suffix. This keeps the extractor
	// inert on hand-written kratos service/biz/data code (which carries no
	// route registrations) and on unrelated Go files.
	if !strings.Contains(src, "HTTPServer") || !strings.Contains(src, "_HTTP_Handler") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. RegisterXxxHTTPServer entry points -> SCOPE.Service (one per service).
	for _, m := range reKratosRegister.FindAllStringSubmatchIndex(src, -1) {
		svc := submatch(src, m, 2)
		ent := makeEntity(svc, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kratos", "provenance", "INFERRED_FROM_KRATOS_HTTP_REGISTER",
			"service", svc)
		add(ent)
	}

	// 2. r := s.Route("/prefix") -> router-var prefix map (+ SCOPE.Component
	//    when a non-empty prefix is declared).
	routePrefix := make(map[string]string) // router var -> prefix
	for _, m := range reKratosRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		prefix := submatch(src, m, 6)
		if prefix == "/" {
			prefix = "" // root mount adds no path segment
		}
		routePrefix[routerVar] = prefix
		if prefix != "" {
			ent := makeEntity(prefix, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "kratos", "provenance", "INFERRED_FROM_KRATOS_ROUTE",
				"group_path", prefix)
			add(ent)
		}
	}

	// 3. r.GET/POST/...("/path", _Svc_Method0_HTTP_Handler(srv)) verb routes ->
	//    SCOPE.Operation/endpoint, with the underlying service method recovered
	//    from the generated handler wrapper for handler attribution.
	for _, m := range reKratosVerb.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		method := strings.ToUpper(submatch(src, m, 4))
		path := submatch(src, m, 6)
		handler := submatch(src, m, 8)
		if p, ok := routePrefix[routerVar]; ok && p != "" {
			path = p + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kratos", "provenance", "INFERRED_FROM_KRATOS_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		ent.Properties["handler"] = handler
		if svcMethod := kratosMethodFromHandler(handler); svcMethod != "" {
			ent.Properties["service_method"] = svcMethod
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
