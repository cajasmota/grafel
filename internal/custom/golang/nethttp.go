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
	extractor.Register("custom_go_nethttp", &netHTTPExtractor{})
}

// netHTTPExtractor recovers routes registered against the Go standard-library
// HTTP router. Three registration surfaces are covered:
//
//	http.HandleFunc("/path", fn)       // package-level DefaultServeMux
//	http.Handle("/path", handler)      // package-level DefaultServeMux
//	mux := http.NewServeMux()          // explicit mux -> SCOPE.Service
//	mux.HandleFunc("/path", fn)        // mux-scoped registration
//	mux.Handle("/path", handler)
//
// Since Go 1.22 the stdlib router accepts method-prefixed patterns and host
// prefixes, e.g. `mux.HandleFunc("GET /users/{id}", h)` or
// `http.HandleFunc("POST example.com/orders", h)`. We split the leading method
// token (if it is a recognised HTTP verb) off the pattern so the synthesized
// endpoint carries the real verb; patterns without a method token default to
// "ANY" (the stdlib matches any method).
//
// Handler-method attribution (rewriting `Controller:<recvVar>` ROUTES_TO edges
// into `Controller:<Type>.<Method>`) is handled by the shared AST pass in
// internal/engine/go_routes.go, whose goHTTPVerbs set already includes
// HandleFunc/Handle — so `mux.HandleFunc("/p", h.List)` resolves for free.
type netHTTPExtractor struct{}

func (e *netHTTPExtractor) Language() string { return "custom_go_nethttp" }

var (
	// mux := http.NewServeMux()  /  var mux = http.NewServeMux()
	reNetHTTPMux = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*http\.NewServeMux\s*\(\s*\)`,
	)
	// http.HandleFunc("/p", fn) / http.Handle("/p", h) on DefaultServeMux,
	// and <mux>.HandleFunc/Handle("/p", …) on an explicit mux. The receiver
	// capture lets us distinguish the package-level form (receiver == "http")
	// from a mux variable.
	reNetHTTPRoute = regexp.MustCompile(
		`(?m)(\w+)\.(HandleFunc|Handle)\s*\(\s*` + "[\"`]" + `([^"` + "`" + `]+)` + "[\"`]",
	)
)

// netHTTPMethodToken splits a Go 1.22+ method-prefixed pattern into (method,
// rest). Returns ("ANY", pattern) when no recognised leading verb is present.
// Per the stdlib grammar the method token is followed by a single space:
// `METHOD [host]/[path]`.
func netHTTPMethodToken(pattern string) (string, string) {
	sp := strings.IndexByte(pattern, ' ')
	if sp <= 0 {
		return "ANY", pattern
	}
	tok := pattern[:sp]
	rest := strings.TrimSpace(pattern[sp+1:])
	switch tok {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE":
		return tok, rest
	}
	return "ANY", pattern
}

func (e *netHTTPExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.nethttp_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "net/http"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	// Cheap pre-filter: only do work on files that touch the stdlib router.
	if !strings.Contains(src, "HandleFunc") && !strings.Contains(src, ".Handle(") &&
		!strings.Contains(src, "http.NewServeMux") {
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

	// 1. http.NewServeMux() -> SCOPE.Service
	for _, m := range reNetHTTPMux.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "net/http", "provenance", "INFERRED_FROM_NETHTTP_MUX",
			"constructor", "http.NewServeMux")
		add(ent)
	}

	// 2. HandleFunc/Handle registrations -> SCOPE.Operation/endpoint.
	for _, m := range reNetHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		register := src[m[4]:m[5]]
		pattern := src[m[6]:m[7]]
		method, path := netHTTPMethodToken(pattern)
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "net/http", "provenance", "INFERRED_FROM_NETHTTP_ROUTE",
			"http_method", method, "route_path", path, "router_var", recv,
			"register_func", register)
		if recv == "http" {
			ent.Properties["default_serve_mux"] = "true"
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
