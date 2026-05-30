package rust

// minor_fw_routing.go — route extractors for Rust minor HTTP frameworks:
// poem, warp, tide, gotham, hyper, salvo, tower.
//
// Each extractor registers itself via init() and covers:
//   - endpoint_synthesis  (routes with HTTP method + path)
//   - handler_attribution (handler function name attributed to route)
//
// Patterns are regex-based, conservative, and fixture-proven.

import (
	"context"
	"strings"

	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_poem", &poemExtractor{})
	extractor.Register("custom_rust_warp", &warpExtractor{})
	extractor.Register("custom_rust_tide", &tideExtractor{})
	extractor.Register("custom_rust_gotham", &gothamExtractor{})
	extractor.Register("custom_rust_hyper", &hyperExtractor{})
	extractor.Register("custom_rust_salvo", &salvoExtractor{})
	extractor.Register("custom_rust_tower", &towerExtractor{})
}

// ---------------------------------------------------------------------------
// Poem
// ---------------------------------------------------------------------------

type poemExtractor struct{}

func (e *poemExtractor) Language() string { return "custom_rust_poem" }

var (
	// #[handler]  async fn my_handler(...) -> ...
	rePoemHandlerAttr = regexp.MustCompile(
		`#\[handler\][\s\S]*?(?:async\s+)?fn\s+(\w+)\s*\(`,
	)
	// Route::new().at("/path", get(handler))
	// also: .at("/path", post(handler))
	rePoemAt = regexp.MustCompile(
		`\.at\s*\(\s*"([^"]+)"\s*,\s*(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
	)
	// Route::new().nest("/prefix", sub)  -> SCOPE.Component
	rePoemNest = regexp.MustCompile(
		`\.nest\s*\(\s*"([^"]+)"\s*,`,
	)
	// Server::new(TcpListener::bind(...)).run(app)  -> SCOPE.Service
	rePoemServer = regexp.MustCompile(
		`Server\s*::\s*new\s*\(`,
	)
)

func (e *poemExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.poem_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "poem"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. .at("/path", get(handler)) -> endpoint + handler attribution
	for _, m := range rePoemAt.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		method := strings.ToUpper(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "poem", "provenance", "INFERRED_FROM_POEM_ROUTE",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		add(ent)
	}

	// 2. #[handler] fn name — bare handler declarations -> SCOPE.Function
	for _, m := range rePoemHandlerAttr.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Function", "handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "poem", "provenance", "INFERRED_FROM_POEM_HANDLER")
		add(ent)
	}

	// 3. .nest("/prefix", ...) -> SCOPE.Component
	for _, m := range rePoemNest.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		ent := makeEntity("nest:"+prefix, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "poem", "provenance", "INFERRED_FROM_POEM_NEST", "nest_prefix", prefix)
		add(ent)
	}

	// 4. Server::new(...) -> SCOPE.Service
	for _, m := range rePoemServer.FindAllStringIndex(src, -1) {
		ent := makeEntity("poem::Server", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "poem", "provenance", "INFERRED_FROM_POEM_SERVER")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Warp
// ---------------------------------------------------------------------------

type warpExtractor struct{}

func (e *warpExtractor) Language() string { return "custom_rust_warp" }

var (
	// warp::path!("segment" / "segment")  or  warp::path("segment")
	reWarpPathMacro = regexp.MustCompile(
		`warp::path!\s*\(([^)]+)\)`,
	)
	// .and(warp::get())  .and(warp::post())  etc.
	reWarpMethod = regexp.MustCompile(
		`warp::(get|post|put|delete|patch|head|options)\s*\(\s*\)`,
	)
	// .and_then(handler_fn)  ->  handler attribution
	reWarpAndThen = regexp.MustCompile(
		`\.and_then\s*\(\s*(\w+)\s*\)`,
	)
	// .map(handler_fn) -> handler attribution (simpler warp style)
	reWarpMap = regexp.MustCompile(
		`\.map\s*\(\s*(\w+)\s*\)`,
	)
	// warp::serve(routes).run(addr) -> SCOPE.Service
	reWarpServe = regexp.MustCompile(
		`warp::serve\s*\(`,
	)
	// let route_name = warp::path!(...).and(warp::get()).and_then(handler)
	// Composite pattern: capture method + path macro + handler on same chain
	reWarpChain = regexp.MustCompile(
		`warp::path!\s*\(([^)]+)\)[^;]*?warp::(get|post|put|delete|patch|head|options)\s*\(\s*\)[^;]*?\.(?:and_then|map)\s*\(\s*(\w+)\s*\)`,
	)
)

func normWarpPath(raw string) string {
	// Convert warp::path!("users" / u32 / "comments") segments to a canonical
	// path. String-literal segments are kept verbatim; bare (unquoted) segments
	// are typed path params (e.g. u32, String, Uuid) and become {param}.
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, "/")
	var segs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, `"`) {
			// String literal segment.
			segs = append(segs, strings.Trim(p, `"`))
		} else {
			// Typed param segment — canonicalise to {type} lower-cased.
			segs = append(segs, "{"+strings.ToLower(p)+"}")
		}
	}
	return "/" + strings.Join(segs, "/")
}

func (e *warpExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.warp_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "warp"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. Full chain: path!(...)  + method + handler  -> endpoint
	for _, m := range reWarpChain.FindAllStringSubmatchIndex(src, -1) {
		rawPath := src[m[2]:m[3]]
		method := strings.ToUpper(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		path := normWarpPath(rawPath)
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "warp", "provenance", "INFERRED_FROM_WARP_CHAIN",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		add(ent)
	}

	// 2. Standalone path!() without full chain -> SCOPE.Component (route fragment)
	for _, m := range reWarpPathMacro.FindAllStringSubmatchIndex(src, -1) {
		rawPath := src[m[2]:m[3]]
		path := normWarpPath(rawPath)
		ent := makeEntity("path:"+path, "SCOPE.Component", "route_fragment", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "warp", "provenance", "INFERRED_FROM_WARP_PATH", "route_pattern", path)
		add(ent)
	}

	// 3. Standalone method filters -> SCOPE.Pattern
	for _, m := range reWarpMethod.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		ent := makeEntity("filter:"+method, "SCOPE.Pattern", "method_filter", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "warp", "provenance", "INFERRED_FROM_WARP_METHOD", "http_method", method)
		add(ent)
	}

	// 4. .and_then(handler) -> SCOPE.Function/handler
	for _, m := range reWarpAndThen.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		ent := makeEntity(handler, "SCOPE.Function", "handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "warp", "provenance", "INFERRED_FROM_WARP_AND_THEN")
		add(ent)
	}

	// 5. warp::serve(...) -> SCOPE.Service
	for _, m := range reWarpServe.FindAllStringIndex(src, -1) {
		ent := makeEntity("warp::serve", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "warp", "provenance", "INFERRED_FROM_WARP_SERVE")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Tide
// ---------------------------------------------------------------------------

type tideExtractor struct{}

func (e *tideExtractor) Language() string { return "custom_rust_tide" }

var (
	// app.at("/path").get(handler)
	// app.at("/path").post(handler)
	reTideAt = regexp.MustCompile(
		`\.at\s*\(\s*"([^"]+)"\s*\)\s*\.(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
	)
	// tide::new() / Server::new(...) -> SCOPE.Service
	reTideNew = regexp.MustCompile(
		`tide::new\s*\(\s*\)|Server::with_state\s*\(`,
	)
	// app.with(middleware) -> SCOPE.Pattern
	reTideWith = regexp.MustCompile(
		`\.with\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
)

func (e *tideExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.tide_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "tide"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. .at("/path").get(handler) -> endpoint
	for _, m := range reTideAt.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		method := strings.ToUpper(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tide", "provenance", "INFERRED_FROM_TIDE_ROUTE",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		add(ent)
	}

	// 2. tide::new() / Server::with_state() -> SCOPE.Service
	for _, m := range reTideNew.FindAllStringIndex(src, -1) {
		ent := makeEntity("tide::Server", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tide", "provenance", "INFERRED_FROM_TIDE_SERVER")
		add(ent)
	}

	// 3. .with(Middleware) -> SCOPE.Pattern
	for _, m := range reTideWith.FindAllStringSubmatchIndex(src, -1) {
		mwType := src[m[2]:m[3]]
		ent := makeEntity("middleware:"+mwType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tide", "provenance", "INFERRED_FROM_TIDE_MIDDLEWARE", "middleware_type", mwType)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Gotham
// ---------------------------------------------------------------------------

type gothamExtractor struct{}

func (e *gothamExtractor) Language() string { return "custom_rust_gotham" }

var (
	// route.get("/path").to(handler)
	// route.post("/path").to(handler)
	reGothamRoute = regexp.MustCompile(
		`route\.(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"\s*\)\s*\.to\s*\(\s*(\w+)\s*\)`,
	)
	// build_simple_router(|route| { ... }) -> SCOPE.Component
	reGothamRouter = regexp.MustCompile(
		`build_simple_router\s*\(`,
	)
	// gotham::start("addr", router) -> SCOPE.Service
	reGothamStart = regexp.MustCompile(
		`gotham::start\s*\(`,
	)
	// gotham::start_with_num_threads -> SCOPE.Service
	reGothamStartThreaded = regexp.MustCompile(
		`gotham::start_with_num_threads\s*\(`,
	)
)

func (e *gothamExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.gotham_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gotham"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. route.get("/path").to(handler) -> endpoint + handler attribution
	for _, m := range reGothamRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := rustNormalizePath(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gotham", "provenance", "INFERRED_FROM_GOTHAM_ROUTE",
			"http_method", method, "route_pattern", path, "handler_name", handler)
		add(ent)
	}

	// 2. build_simple_router -> SCOPE.Component
	for _, m := range reGothamRouter.FindAllStringIndex(src, -1) {
		ent := makeEntity("gotham::Router", "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gotham", "provenance", "INFERRED_FROM_GOTHAM_ROUTER")
		add(ent)
	}

	// 3. gotham::start -> SCOPE.Service
	for _, m := range reGothamStart.FindAllStringIndex(src, -1) {
		ent := makeEntity("gotham::start", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gotham", "provenance", "INFERRED_FROM_GOTHAM_START")
		add(ent)
	}

	for _, m := range reGothamStartThreaded.FindAllStringIndex(src, -1) {
		ent := makeEntity("gotham::start_with_num_threads", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gotham", "provenance", "INFERRED_FROM_GOTHAM_START")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Hyper (raw)
// ---------------------------------------------------------------------------

type hyperExtractor struct{}

func (e *hyperExtractor) Language() string { return "custom_rust_hyper" }

var (
	// match (req.method(), req.uri().path()) {  (&Method::GET, "/path") => handler
	reHyperMatchArm = regexp.MustCompile(
		`&Method::(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*,\s*"([^"]+)"\s*\)?\s*=>`,
	)
	// Server::bind(&addr) -> SCOPE.Service
	reHyperBind = regexp.MustCompile(
		`Server\s*::\s*bind\s*\(`,
	)
	// hyper::server::Server::bind -> SCOPE.Service (full path form)
	reHyperFullBind = regexp.MustCompile(
		`hyper::server::Server\s*::\s*bind\s*\(`,
	)
	// service_fn(handler) -> handler attribution
	reHyperServiceFn = regexp.MustCompile(
		`service_fn\s*\(\s*(\w+)\s*\)`,
	)
)

func (e *hyperExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.hyper_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "hyper"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. match arm: (&Method::GET, "/path") =>  -> endpoint
	for _, m := range reHyperMatchArm.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hyper", "provenance", "INFERRED_FROM_HYPER_MATCH_ARM",
			"http_method", method, "route_pattern", path)
		add(ent)
	}

	// 2. service_fn(handler) -> SCOPE.Function/handler
	for _, m := range reHyperServiceFn.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		ent := makeEntity(handler, "SCOPE.Function", "handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hyper", "provenance", "INFERRED_FROM_HYPER_SERVICE_FN")
		add(ent)
	}

	// 3. Server::bind -> SCOPE.Service
	for _, m := range reHyperBind.FindAllStringIndex(src, -1) {
		ent := makeEntity("hyper::Server", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hyper", "provenance", "INFERRED_FROM_HYPER_BIND")
		add(ent)
	}

	for _, m := range reHyperFullBind.FindAllStringIndex(src, -1) {
		ent := makeEntity("hyper::Server", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "hyper", "provenance", "INFERRED_FROM_HYPER_BIND")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Salvo
// ---------------------------------------------------------------------------

type salvoExtractor struct{}

func (e *salvoExtractor) Language() string { return "custom_rust_salvo" }

var (
	// Router::new().path("/p").get(handler)
	// Router::with_path("/p").post(handler)
	reSalvoPath = regexp.MustCompile(
		`(?:Router::new\(\)|Router::with_path\s*\([^)]*\)|router)\s*(?:\.path\s*\(\s*"([^"]+)"\s*\))?\s*\.(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
	)
	// .path("/segment") standalone -> SCOPE.Component
	reSalvoPathOnly = regexp.MustCompile(
		`\.path\s*\(\s*"([^"]+)"\s*\)`,
	)
	// Server::new(TcpListener::bind(addr)).serve(router) -> SCOPE.Service
	reSalvoServer = regexp.MustCompile(
		`Server\s*::\s*new\s*\(`,
	)
	// hoop(middleware) -> SCOPE.Pattern  (salvo's middleware attach)
	reSalvoHoop = regexp.MustCompile(
		`\.hoop\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
)

func (e *salvoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.salvo_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "salvo"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. .get(handler) / .post(handler) on a router with optional .path(...)
	for _, m := range reSalvoPath.FindAllStringSubmatchIndex(src, -1) {
		path := ""
		if m[2] >= 0 {
			path = rustNormalizePath(src[m[2]:m[3]])
		}
		method := strings.ToUpper(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		name := method
		if path != "" {
			name = method + " " + path
		}
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "salvo", "provenance", "INFERRED_FROM_SALVO_ROUTE",
			"http_method", method, "handler_name", handler)
		if path != "" {
			setProps(&ent, "route_pattern", path)
		}
		add(ent)
	}

	// 2. .path("/segment") -> SCOPE.Component
	for _, m := range reSalvoPathOnly.FindAllStringSubmatchIndex(src, -1) {
		seg := rustNormalizePath(src[m[2]:m[3]])
		ent := makeEntity("path:"+seg, "SCOPE.Component", "route_segment", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "salvo", "provenance", "INFERRED_FROM_SALVO_PATH", "route_pattern", seg)
		add(ent)
	}

	// 3. Server::new(...) -> SCOPE.Service
	for _, m := range reSalvoServer.FindAllStringIndex(src, -1) {
		ent := makeEntity("salvo::Server", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "salvo", "provenance", "INFERRED_FROM_SALVO_SERVER")
		add(ent)
	}

	// 4. .hoop(middleware) -> SCOPE.Pattern
	for _, m := range reSalvoHoop.FindAllStringSubmatchIndex(src, -1) {
		mwType := src[m[2]:m[3]]
		ent := makeEntity("hoop:"+mwType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "salvo", "provenance", "INFERRED_FROM_SALVO_HOOP", "middleware_type", mwType)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ---------------------------------------------------------------------------
// Tower
// ---------------------------------------------------------------------------

type towerExtractor struct{}

func (e *towerExtractor) Language() string { return "custom_rust_tower" }

var (
	// ServiceBuilder::new().layer(mw).service(svc)  -> SCOPE.Service
	reTowerServiceBuilder = regexp.MustCompile(
		`ServiceBuilder\s*::\s*new\s*\(\s*\)`,
	)
	// .layer(LayerType) -> SCOPE.Pattern
	reTowerLayer = regexp.MustCompile(
		`\.layer\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
	// .service(svc) -> SCOPE.Component
	reTowerService = regexp.MustCompile(
		`\.service\s*\(\s*(\w+)\s*\)`,
	)
	// tower::service_fn(f) -> SCOPE.Function/handler
	reTowerServiceFn = regexp.MustCompile(
		`tower::service_fn\s*\(\s*(\w+)\s*\)`,
	)
)

func (e *towerExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.tower_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "tower"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// 1. ServiceBuilder::new() -> SCOPE.Service
	for _, m := range reTowerServiceBuilder.FindAllStringIndex(src, -1) {
		ent := makeEntity("ServiceBuilder::new", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tower", "provenance", "INFERRED_FROM_TOWER_BUILDER")
		add(ent)
	}

	// 2. .layer(LayerType) -> SCOPE.Pattern
	for _, m := range reTowerLayer.FindAllStringSubmatchIndex(src, -1) {
		layerType := src[m[2]:m[3]]
		ent := makeEntity("layer:"+layerType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tower", "provenance", "INFERRED_FROM_TOWER_LAYER", "layer_type", layerType)
		add(ent)
	}

	// 3. .service(svc) -> SCOPE.Component
	for _, m := range reTowerService.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		ent := makeEntity("service:"+svcName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tower", "provenance", "INFERRED_FROM_TOWER_SERVICE", "service_name", svcName)
		add(ent)
	}

	// 4. tower::service_fn(f) -> SCOPE.Function/handler
	for _, m := range reTowerServiceFn.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		ent := makeEntity(fnName, "SCOPE.Function", "handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "tower", "provenance", "INFERRED_FROM_TOWER_SERVICE_FN")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
