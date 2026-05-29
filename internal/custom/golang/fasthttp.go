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
	extractor.Register("custom_go_fasthttp", &fasthttpExtractor{})
}

type fasthttpExtractor struct{}

func (e *fasthttpExtractor) Language() string { return "custom_go_fasthttp" }

var (
	// r := router.New() — fasthttp/router router constructor.
	reFastRouterNew = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*router\.New\s*\(\s*\)`,
	)
	// r.GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS("/path", handler)
	reFastRoute = regexp.MustCompile(
		`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"\s*,\s*([\w.]+)`,
	)
	// r.Handle("GET", "/path", handler) — explicit-method registration.
	reFastHandle = regexp.MustCompile(
		`(?m)(\w+)\.Handle\s*\(\s*"([^"]+)"\s*,\s*"([^"]+)"\s*,\s*([\w.]+)`,
	)
	// g := r.Group("/api") — fasthttp/router group.
	reFastGroup = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]+)"`,
	)
	// fasthttp.ListenAndServe(addr, handler) — raw-handler server entry.
	reFastListen = regexp.MustCompile(
		`(?m)fasthttp\.ListenAndServe(?:TLS)?\s*\(\s*([^,]+),\s*([\w.]+)`,
	)
	// func name(ctx *fasthttp.RequestCtx) — raw RequestHandler declaration.
	reFastHandlerDecl = regexp.MustCompile(
		`(?m)func\s+(\w+)\s*\(\s*\w+\s+\*fasthttp\.RequestCtx\s*\)`,
	)
)

func (e *fasthttpExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.fasthttp_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "fasthttp"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
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

	// 1. router.New() -> SCOPE.Service (the fasthttp/router instance).
	for _, m := range reFastRouterNew.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fasthttp", "provenance", "INFERRED_FROM_FASTHTTP_ROUTER")
		add(ent)
	}

	// 2. fasthttp.ListenAndServe(addr, handler) -> SCOPE.Service (raw-handler
	//    server entry; the 2nd arg is the top-level RequestHandler).
	for _, m := range reFastListen.FindAllStringSubmatchIndex(src, -1) {
		handler := submatch(src, m, 4)
		ent := makeEntity("fasthttp_server:"+handler, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fasthttp", "provenance", "INFERRED_FROM_FASTHTTP_LISTEN",
			"handler", handler)
		add(ent)
	}

	// 3. Group prefixes -> SCOPE.Component, tracked for verb-route resolution.
	groupPaths := make(map[string]string) // varName -> full prefix
	for _, m := range reFastGroup.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		parent := submatch(src, m, 4)
		path := submatch(src, m, 6)
		if pp, ok := groupPaths[parent]; ok {
			path = pp + path
		}
		groupPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fasthttp", "provenance", "INFERRED_FROM_FASTHTTP_GROUP",
			"group_path", path)
		add(ent)
	}

	// 4. Verb routes -> SCOPE.Operation/endpoint with handler attribution.
	for _, m := range reFastRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		method := strings.ToUpper(submatch(src, m, 4))
		path := submatch(src, m, 6)
		handler := submatch(src, m, 8)
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fasthttp", "provenance", "INFERRED_FROM_FASTHTTP_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		if handler != "" {
			ent.Properties["handler"] = handler
		}
		add(ent)
	}

	// 5. r.Handle("METHOD", "/path", h) -> SCOPE.Operation/endpoint.
	for _, m := range reFastHandle.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		method := strings.ToUpper(submatch(src, m, 4))
		path := submatch(src, m, 6)
		handler := submatch(src, m, 8)
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fasthttp", "provenance", "INFERRED_FROM_FASTHTTP_HANDLE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		if handler != "" {
			ent.Properties["handler"] = handler
		}
		add(ent)
	}

	// 6. Raw RequestHandler declarations (func(ctx *fasthttp.RequestCtx)) ->
	//    SCOPE.Pattern. Without a router these handlers are dispatched manually
	//    (e.g. a switch on string(ctx.Path())), so we cannot synthesize a
	//    method+path endpoint — we record the handler for attribution only.
	for _, m := range reFastHandlerDecl.FindAllStringSubmatchIndex(src, -1) {
		fn := submatch(src, m, 2)
		ent := makeEntity("handler:"+fn, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "fasthttp", "provenance", "INFERRED_FROM_FASTHTTP_HANDLER",
			"pattern_kind", "request_handler", "handler", fn)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
