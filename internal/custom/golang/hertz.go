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
	extractor.Register("custom_go_hertz", &hertzExtractor{})
}

// hertzExtractor extracts routing structure from CloudWeGo Hertz
// (github.com/cloudwego/hertz) servers. Hertz exposes a gin-style routing
// surface: a `*server.Hertz` engine created via `server.Default()` /
// `server.New(...)`, verb methods `h.GET/POST/...`, route groups via
// `h.Group("/prefix")`, middleware via `.Use(...)`, and static file mounts
// via `.Static/.StaticFS/.StaticFile`. Handlers are attributed from the
// final argument of each verb call.
type hertzExtractor struct{}

func (e *hertzExtractor) Language() string { return "custom_go_hertz" }

var (
	// h := server.Default() / h = server.New(opts...) — Hertz engine.
	reHertzEngine = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*server\.(?:Default|New)\s*\(`,
	)
	// g := h.Group("/api") — route-prefix group (returns *route.RouterGroup).
	reHertzGroup = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]+)"`,
	)
	// h.GET/POST/...("/path", handler) — verb route with handler attribution.
	// Intermediate middleware args (anything that is not the final identifier)
	// are skipped via a non-greedy run constrained to a single statement
	// ([^,()\n] keeps each arg on one line and inside the call), so the final
	// [\w.]+ binds to the trailing handler identifier.
	reHertzRoute = regexp.MustCompile(
		`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any)\s*\(\s*"([^"]+)"\s*,\s*(?:[^\n,]*?,\s*)*?([\w.]+)\s*\)`,
	)
	// h.Static("/assets", "./root") / h.StaticFS / h.StaticFile — static mounts.
	reHertzStatic = regexp.MustCompile(
		`(?m)(\w+)\.(?:Static|StaticFS|StaticFile)\s*\(\s*"([^"]+)"`,
	)
	// h.NoRoute / h.NoMethod error handlers.
	reHertzNoRoute = regexp.MustCompile(
		`(?m)(\w+)\.(NoRoute|NoMethod)\s*\(`,
	)
)

func (e *hertzExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.hertz_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "hertz"),
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

	// 1. server.Default()/server.New() engine -> SCOPE.Service.
	for _, m := range reHertzEngine.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hertz", "provenance", "INFERRED_FROM_HERTZ_ENGINE")
		add(ent)
	}

	// 2. h.Group(...) -> SCOPE.Component, tracking nested group prefixes so
	//    verb routes registered on a group resolve to their full path.
	groupPaths := make(map[string]string) // varName -> full prefix
	for _, m := range reHertzGroup.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		parent := submatch(src, m, 4)
		path := submatch(src, m, 6)
		if pp, ok := groupPaths[parent]; ok {
			path = pp + path
		}
		groupPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hertz", "provenance", "INFERRED_FROM_HERTZ_GROUP",
			"group_path", path)
		add(ent)
	}

	// 3. Verb routes -> SCOPE.Operation/endpoint with group-prefix resolution
	//    and handler attribution from the final argument.
	for _, m := range reHertzRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		method := strings.ToUpper(submatch(src, m, 4))
		path := submatch(src, m, 6)
		handler := submatch(src, m, 8)
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hertz", "provenance", "INFERRED_FROM_HERTZ_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		if handler != "" {
			ent.Properties["handler"] = handler
		}
		add(ent)
	}

	// 4. Static mounts -> SCOPE.Operation/endpoint (GET).
	for _, m := range reHertzStatic.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		path := submatch(src, m, 4)
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := "GET " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hertz", "provenance", "INFERRED_FROM_HERTZ_ROUTE",
			"http_method", "GET", "route_path", path, "is_static", "true")
		add(ent)
	}

	// 5. Middleware -> ordered SCOPE.Pattern (+ auth classification), reusing
	//    the shared balanced .Use(...) parser.
	for _, uc := range findUseCalls(src) {
		chain := parseMiddlewareChain(uc.Args)
		emitMiddlewareChain(add, chain, "hertz",
			"INFERRED_FROM_HERTZ_MIDDLEWARE", "INFERRED_FROM_HERTZ_AUTH",
			file.Path, file.Language, uc.Line)
	}

	// 6. NoRoute/NoMethod error handlers -> SCOPE.Pattern.
	for _, m := range reHertzNoRoute.FindAllStringSubmatchIndex(src, -1) {
		handlerKind := submatch(src, m, 4)
		ent := makeEntity(handlerKind, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hertz", "provenance", "INFERRED_FROM_HERTZ_ERROR_HANDLER",
			"handler_kind", handlerKind, "pattern_kind", "error_handler")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
