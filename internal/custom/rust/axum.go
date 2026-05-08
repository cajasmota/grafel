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
	extractor.Register("custom_rust_axum", &axumExtractor{})
}

type axumExtractor struct{}

func (e *axumExtractor) Language() string { return "custom_rust_axum" }

var (
	reAxumRoute = regexp.MustCompile(
		`\.route\s*\(\s*"([^"]+)"\s*,\s*(get|post|put|delete|patch|head|options)\s*\(\s*(\w+)\s*\)`,
	)
	reAxumNest = regexp.MustCompile(
		`\.nest\s*\(\s*"([^"]+)"\s*,\s*(\w+)`,
	)
	reAxumLayer = regexp.MustCompile(
		`\.layer\s*\(\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`,
	)
	reAxumState = regexp.MustCompile(
		`State\s*<\s*([A-Za-z_]\w*)`,
	)
	reAxumExtension = regexp.MustCompile(
		`Extension\s*<\s*([A-Za-z_]\w*)`,
	)
	reAxumJsonExtractor = regexp.MustCompile(
		`(?:Json|Path|Query|Form)\s*<\s*([A-Za-z_]\w*)`,
	)
	reAxumServe = regexp.MustCompile(
		`axum::serve\s*\(`,
	)
	reAxumServerBind = regexp.MustCompile(
		`Server\s*::\s*bind\s*\(`,
	)
	reAxumWebSocket = regexp.MustCompile(
		`WebSocketUpgrade`,
	)
)

func (e *axumExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.axum_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "axum"),
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

	// 1. .route("/path", get(handler)) -> SCOPE.Operation/endpoint
	for _, m := range reAxumRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		method := strings.ToUpper(src[m[4]:m[5]])
		handler := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_ROUTE",
			"http_method", method, "route_path", path, "handler_name", handler)
		add(ent)
	}

	// 2. .nest("/api", sub_router) -> SCOPE.Component
	for _, m := range reAxumNest.FindAllStringSubmatchIndex(src, -1) {
		prefix := src[m[2]:m[3]]
		routerVar := src[m[4]:m[5]]
		ent := makeEntity("nest:"+prefix, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_NEST",
			"nest_prefix", prefix, "router_var", routerVar)
		add(ent)
	}

	// 3. .layer(Middleware) -> SCOPE.Pattern
	for _, m := range reAxumLayer.FindAllStringSubmatchIndex(src, -1) {
		mwType := src[m[2]:m[3]]
		ent := makeEntity("layer:"+mwType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_LAYER",
			"middleware_type", mwType)
		add(ent)
	}

	// 4. State<T> -> SCOPE.Pattern
	for _, m := range reAxumState.FindAllStringSubmatchIndex(src, -1) {
		stateType := src[m[2]:m[3]]
		ent := makeEntity("state:"+stateType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_STATE",
			"state_type", stateType)
		add(ent)
	}

	// 5. Extension<T> -> SCOPE.Pattern
	for _, m := range reAxumExtension.FindAllStringSubmatchIndex(src, -1) {
		extType := src[m[2]:m[3]]
		ent := makeEntity("extension:"+extType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_EXTENSION",
			"extension_type", extType)
		add(ent)
	}

	// 6. Json<T>/Path<T>/Query<T>/Form<T> extractors -> SCOPE.Schema
	for _, m := range reAxumJsonExtractor.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		ent := makeEntity("extractor:"+typeParam, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_EXTRACTOR",
			"type_param", typeParam)
		add(ent)
	}

	// 7. axum::serve() -> SCOPE.Service
	for _, m := range reAxumServe.FindAllStringIndex(src, -1) {
		ent := makeEntity("axum::serve", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_SERVE")
		add(ent)
	}

	// 7b. Server::bind() -> SCOPE.Service (older axum)
	for _, m := range reAxumServerBind.FindAllStringIndex(src, -1) {
		ent := makeEntity("Server::bind", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_SERVE")
		add(ent)
	}

	// 8. WebSocketUpgrade -> SCOPE.Operation/websocket
	for _, m := range reAxumWebSocket.FindAllStringIndex(src, -1) {
		ent := makeEntity("WebSocketUpgrade", "SCOPE.Operation", "websocket", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "axum", "provenance", "INFERRED_FROM_AXUM_WEBSOCKET")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
