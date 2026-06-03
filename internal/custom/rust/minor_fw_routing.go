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
	// Route::new().at("/path", get(handler)) — captures the path plus the full
	// method-router argument (one or more verb(handler) calls chained with
	// `.verb(handler)`), e.g. `.at("/users", get(list).post(create))`. The verb
	// chain is re-scanned with rePoemVerb so each verb yields its own endpoint
	// (flagship axum parity for chained method routers). The verb-argument body
	// excludes parens/semicolons so the match cannot run past the method router.
	rePoemAt = regexp.MustCompile(
		`\.at\s*\(\s*"([^"]+)"\s*,\s*((?:get|post|put|delete|patch|head|options)\s*\(\s*\w+\s*\)(?:\s*\.\s*(?:get|post|put|delete|patch|head|options)\s*\(\s*\w+\s*\))*)`,
	)
	// Individual verb(handler) calls inside a poem method-router argument.
	rePoemVerb = regexp.MustCompile(
		`(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
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

	// 1. .at("/path", get(handler).post(handler2)) -> endpoint + handler
	// attribution. Each verb in the method-router chain becomes its own endpoint.
	for _, m := range rePoemAt.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		methodRouter := src[m[4]:m[5]]
		for _, vm := range rePoemVerb.FindAllStringSubmatch(methodRouter, -1) {
			method := strings.ToUpper(vm[1])
			handler := vm[2]
			name := method + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "poem", "provenance", "INFERRED_FROM_POEM_ROUTE",
				"http_method", method, "route_pattern", path, "handler_name", handler)
			add(ent)
		}
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
	// Composite warp filter chain ending in a handler. Captures the whole chain
	// (from the first `warp::path` to the `.and_then(handler)`/`.map(handler)`
	// terminal) as one blob, then method / path / handler are recovered
	// independently with the helper regexes below. This makes the endpoint
	// synthesis order-independent: the method filter may appear before or after
	// the path filter, and the path may use either the `path!(...)` macro or the
	// function form `warp::path("seg")`.
	reWarpChain = regexp.MustCompile(
		`warp::(?:path|get|post|put|delete|patch|head|options)[!]?\s*\([^;]*?\.(?:and_then|map)\s*\(\s*(\w+)\s*\)`,
	)
	// Path macro `warp::path!("a" / b / "c")` — captures the macro argument list.
	reWarpPathMacroIn = regexp.MustCompile(`warp::path!\s*\(([^)]*)\)`)
	// Path function form `warp::path("seg")` — single string literal segment.
	reWarpPathFn = regexp.MustCompile(`warp::path\s*\(\s*"([^"]+)"\s*\)`)
	// Method filter `warp::get()` inside a chain.
	reWarpChainMethod = regexp.MustCompile(`warp::(get|post|put|delete|patch|head|options)\s*\(\s*\)`)
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

	// 1. Full filter chain ending in .and_then/.map(handler) -> endpoint.
	// Method/path are recovered from the chain blob independently so the
	// synthesis is order-independent and accepts both warp::path! macro and
	// warp::path("seg") function forms. A chain without a resolvable method
	// defaults to GET (warp's path filters match any method until constrained;
	// when no warp::verb() is present the route is method-agnostic and GET is
	// the conventional read default — kept explicit via http_method).
	for _, m := range reWarpChain.FindAllStringSubmatchIndex(src, -1) {
		blob := src[m[0]:m[1]]
		handler := src[m[2]:m[3]]

		method := "GET"
		if mm := reWarpChainMethod.FindStringSubmatch(blob); mm != nil {
			method = strings.ToUpper(mm[1])
		}

		// Path: prefer the macro form (multi-segment / typed params), else the
		// function form (single string segment).
		path := ""
		if pm := reWarpPathMacroIn.FindStringSubmatch(blob); pm != nil {
			path = normWarpPath(pm[1])
		} else if pf := reWarpPathFn.FindStringSubmatch(blob); pf != nil {
			path = "/" + strings.Trim(pf[1], "/")
		}
		if path == "" {
			continue
		}

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
	// app.at("/path").get(handler)         (single verb)
	// app.at("/path").get(a).post(b)       (chained verbs on the same route)
	// Captures the path plus the trailing `.verb(handler)` chain; the chain is
	// re-scanned with reTideVerb so each verb yields its own endpoint (flagship
	// parity for chained method routers).
	reTideAt = regexp.MustCompile(
		`\.at\s*\(\s*"([^"]+)"\s*\)\s*((?:\.(?:get|post|put|delete|patch|head|options)\s*\(\s*\w+\s*\))+)`,
	)
	// Individual `.verb(handler)` calls in a tide route chain.
	reTideVerb = regexp.MustCompile(
		`\.(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
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

	// 1. .at("/path").get(a).post(b) -> one endpoint per verb in the chain.
	for _, m := range reTideAt.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		verbChain := src[m[4]:m[5]]
		for _, vm := range reTideVerb.FindAllStringSubmatch(verbChain, -1) {
			method := strings.ToUpper(vm[1])
			handler := vm[2]
			name := method + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "tide", "provenance", "INFERRED_FROM_TIDE_ROUTE",
				"http_method", method, "route_pattern", path, "handler_name", handler)
			add(ent)
		}
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
	// route.associate("/path", |assoc| { assoc.get().to(h); assoc.post().to(h2); })
	// The path lives on associate(...) and each verb is an `assoc.verb().to(h)`
	// inside the closure body. We capture the path and the closure body, then
	// re-scan the body with reGothamAssocVerb for verb+handler pairs.
	reGothamAssociate = regexp.MustCompile(
		`\.associate\s*\(\s*"([^"]+)"\s*,\s*\|[^|]*\|\s*\{([^}]*)\}`,
	)
	// assoc.get().to(handler) inside an associate closure.
	reGothamAssocVerb = regexp.MustCompile(
		`\.(get|post|put|delete|patch|head|options)\s*\(\s*\)\s*\.to\s*\(\s*(\w+)\s*\)`,
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

	// 1b. route.associate("/path", |assoc| { assoc.get().to(h); ... }) -> endpoint
	// per verb in the closure body.
	for _, m := range reGothamAssociate.FindAllStringSubmatchIndex(src, -1) {
		path := rustNormalizePath(src[m[2]:m[3]])
		body := src[m[4]:m[5]]
		for _, vm := range reGothamAssocVerb.FindAllStringSubmatch(body, -1) {
			method := strings.ToUpper(vm[1])
			handler := vm[2]
			name := method + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "gotham", "provenance", "INFERRED_FROM_GOTHAM_ASSOCIATE",
				"http_method", method, "route_pattern", path, "handler_name", handler)
			add(ent)
		}
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
	// Salvo route chains. A router fragment carries its path via either
	// `.path("p")` or `Router::with_path("p")`, then one or more `.verb(handler)`
	// calls (possibly chained: `.get(a).post(b)`). We capture the path from
	// whichever form supplied it (group 1 = .path, group 2 = with_path) plus the
	// trailing verb chain, then re-scan the chain with reSalvoVerb so every verb
	// yields its own endpoint with the path preserved (flagship parity).
	reSalvoPath = regexp.MustCompile(
		`(?:Router::with_path\s*\(\s*"([^"]+)"\s*\)|Router::new\(\)|router)\s*(?:\.path\s*\(\s*"([^"]+)"\s*\))?\s*((?:\.(?:get|post|put|delete|patch|head|options)\s*\(\s*\w+\s*\)\s*)+)`,
	)
	// Individual `.verb(handler)` calls in a salvo route chain.
	reSalvoVerb = regexp.MustCompile(
		`\.(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
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

	// 1. Router(.with_path|.path).verb(a).verb(b) -> one endpoint per verb.
	for _, m := range reSalvoPath.FindAllStringSubmatchIndex(src, -1) {
		// group 1 = Router::with_path("..") path; group 2 = .path("..") segment.
		// When both are present compose them (with_path is the prefix).
		var withPath, dotPath string
		if m[2] >= 0 {
			withPath = rustNormalizePath(src[m[2]:m[3]])
		}
		if m[4] >= 0 {
			dotPath = rustNormalizePath(src[m[4]:m[5]])
		}
		path := rustJoinPaths(withPath, dotPath)
		// salvo with_path("users") / .path("users") segments are written without a
		// leading slash as often as with one; canonicalise to a rooted path so the
		// same logical route compares equal to the flagship `/users` form.
		if path != "" && !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		verbChain := src[m[6]:m[7]]
		for _, vm := range reSalvoVerb.FindAllStringSubmatch(verbChain, -1) {
			method := strings.ToUpper(vm[1])
			handler := vm[2]
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
