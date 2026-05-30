package rust

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
	extractor.Register("custom_rust_actix_web", &actixWebExtractor{})
}

type actixWebExtractor struct{}

func (e *actixWebExtractor) Language() string { return "custom_rust_actix_web" }

var (
	reActixRouteMacro = regexp.MustCompile(
		`#\[(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"\s*\)\][\s\S]*?(?:async\s+)?fn\s+(\w+)\s*\(`,
	)
	reActixScope = regexp.MustCompile(
		`web::scope\s*\(\s*"([^"]+)"\s*\)`,
	)
	reActixWrap = regexp.MustCompile(
		`\.wrap\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
	reActixExtractor = regexp.MustCompile(
		`web::(Json|Path|Query|Form|Data)<([A-Za-z_]\w*)>`,
	)
	reActixServer = regexp.MustCompile(
		`HttpServer\s*::\s*new\s*\(`,
	)
	reActixWebSocket = regexp.MustCompile(
		`(?m)ws::WebsocketContext\s*<\s*(\w+)\s*>|WebsocketContext<(\w+)>`,
	)
	reActixRouteManual = regexp.MustCompile(
		`\.route\s*\(\s*"([^"]+)"\s*,\s*web::(get|post|put|delete|patch|head|options)\s*\(\s*\)\.to\s*\(\s*(\w+)\s*\)`,
	)
)

// actixScopePrefix returns the web::scope("/prefix") that lexically encloses a
// manual .route(...) found at routeOff. actix chains routes onto a scope:
//
//	web::scope("/api").route("/users", web::get().to(h))
//
// We take the nearest preceding web::scope on the same statement (no `;`
// between the scope and the route). Returns "" when the route is not scoped.
func actixScopePrefix(src string, routeOff int) string {
	locs := reActixScope.FindAllStringSubmatchIndex(src, -1)
	best := -1
	var prefix string
	for _, sm := range locs {
		if sm[0] > routeOff {
			break
		}
		// Reject if a statement terminator separates this scope from the route,
		// which would mean they are not on the same chain.
		if strings.ContainsAny(src[sm[1]:routeOff], ";") {
			continue
		}
		best = sm[0]
		prefix = rustNormalizePath(src[sm[2]:sm[3]])
	}
	if best < 0 {
		return ""
	}
	return prefix
}

func (e *actixWebExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.actix_web_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "actix_web"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
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

	// 1. Macro-annotated HTTP routes -> SCOPE.Operation/endpoint
	// Attribute macros (#[get("/users/{id}")]) carry the full path; actix
	// already uses {param} syntax so normalisation is a no-op but applied for
	// consistency. Scopes do not prefix attribute-macro paths in actix.
	for _, m := range reActixRouteMacro.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := rustNormalizePath(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_ROUTE",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		add(ent)
	}

	// 2. Manual .route() registrations -> SCOPE.Operation/endpoint
	// These can be nested inside a web::scope("/prefix"); compose the prefix.
	for _, m := range reActixRouteManual.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		method := strings.ToUpper(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		prefix := actixScopePrefix(src, m[0])
		fullPath := rustJoinPaths(prefix, path)
		name := method + " " + fullPath
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_ROUTE",
			"http_method", method, "route_pattern", fullPath, "handler_name", handler)
		if prefix != "" {
			setProps(&ent, "scope_prefix", prefix)
		}
		add(ent)
	}

	// 3. web::scope -> SCOPE.Component
	for _, m := range reActixScope.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		ent := makeEntity(prefix, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_SCOPE",
			"scope_prefix", prefix)
		add(ent)
	}

	// 4. .wrap(Middleware) -> SCOPE.Pattern
	for _, m := range reActixWrap.FindAllStringSubmatchIndex(src, -1) {
		mwType := src[m[2]:m[3]]
		ent := makeEntity("middleware:"+mwType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_MIDDLEWARE",
			"middleware_type", mwType)
		add(ent)
	}

	// 5. web::Json<T>/Path<T>/Query<T>/Form<T>/Data<T> extractors -> SCOPE.Schema
	for _, m := range reActixExtractor.FindAllStringSubmatchIndex(src, -1) {
		extKind := src[m[2]:m[3]]
		typeParam := src[m[4]:m[5]]
		name := extKind + "<" + typeParam + ">"
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_EXTRACTOR",
			"extractor_kind", extKind, "type_param", typeParam)
		add(ent)
	}

	// 6. HttpServer::new() -> SCOPE.Service
	for _, m := range reActixServer.FindAllStringIndex(src, -1) {
		ent := makeEntity("HttpServer", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_SERVER")
		add(ent)
	}

	// 7. WebSocket handlers -> SCOPE.Operation/websocket
	for _, m := range reActixWebSocket.FindAllStringSubmatchIndex(src, -1) {
		handler := ""
		if m[2] >= 0 {
			handler = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			handler = src[m[4]:m[5]]
		}
		if handler == "" {
			continue
		}
		ent := makeEntity(handler, "SCOPE.Operation", "websocket", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "actix_web", "provenance", "INFERRED_FROM_ACTIX_WEBSOCKET",
			"handler_name", handler)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
