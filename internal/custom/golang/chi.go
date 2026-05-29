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
	extractor.Register("custom_go_chi", &chiExtractor{})
}

type chiExtractor struct{}

func (e *chiExtractor) Language() string { return "custom_go_chi" }

var (
	reChiRouter = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*chi\.NewRouter\s*\(\s*\)`,
	)
	// chi sub-routers: r.Route("/prefix", func(r chi.Router) {...}) and
	// chi.NewRouter() assigned to a nested var. Route() establishes a path
	// prefix scope; we capture the prefix as a component.
	reChiGroup = regexp.MustCompile(
		`(?m)(\w+)\.Route\s*\(\s*"([^"]+)"`,
	)
	// chi uses Title-case verb methods: r.Get/Post/Put/Patch/Delete/...
	reChiRoute = regexp.MustCompile(
		`(?m)(\w+)\.(Get|Post|Put|Patch|Delete|Head|Options|Connect|Trace)\s*\(\s*"([^"]+)"`,
	)
	// Generic registration: r.Handle("/path", h) / r.HandleFunc("/path", h)
	reChiHandle = regexp.MustCompile(
		`(?m)(\w+)\.(Handle|HandleFunc)\s*\(\s*"([^"]+)"`,
	)
	// Sub-router mount: r.Mount("/prefix", handler)
	reChiMount = regexp.MustCompile(
		`(?m)(\w+)\.Mount\s*\(\s*"([^"]+)"`,
	)
	reChiUse = regexp.MustCompile(
		`(?m)(\w+)\.Use\s*\(\s*((?:[^()]+|\([^)]*\))+?)\s*\)`,
	)
)

func (e *chiExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.chi_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "chi"),
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

	// 1. chi.NewRouter() -> SCOPE.Service
	for _, m := range reChiRouter.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "chi", "provenance", "INFERRED_FROM_CHI_ROUTER",
			"constructor", "chi.NewRouter")
		add(ent)
	}

	// 2. r.Route("/prefix", func(r chi.Router){...}) sub-router scope ->
	//    SCOPE.Component. chi groups are closure-scoped (the prefix is applied
	//    to the closure's router param, not a returned variable), so we record
	//    the prefix as a component but leave prefix-joining of nested routes to
	//    the AST-driven go_routes.go composition pass.
	for _, m := range reChiGroup.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[4]:m[5]]
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "chi", "provenance", "INFERRED_FROM_CHI_ROUTE_GROUP",
			"group_path", path)
		add(ent)
	}

	// 3. HTTP verb routes -> SCOPE.Operation/endpoint
	for _, m := range reChiRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := src[m[2]:m[3]]
		method := strings.ToUpper(src[m[4]:m[5]])
		path := src[m[6]:m[7]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "chi", "provenance", "INFERRED_FROM_CHI_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		add(ent)
	}

	// 4. r.Handle/HandleFunc("/path", h) -> SCOPE.Operation/endpoint (method-agnostic)
	for _, m := range reChiHandle.FindAllStringSubmatchIndex(src, -1) {
		routerVar := src[m[2]:m[3]]
		path := src[m[6]:m[7]]
		name := "ANY " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "chi", "provenance", "INFERRED_FROM_CHI_HANDLE",
			"http_method", "ANY", "route_path", path, "router_var", routerVar)
		add(ent)
	}

	// 5. r.Mount("/prefix", handler) sub-router mount -> SCOPE.Component
	for _, m := range reChiMount.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[4]:m[5]]
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "chi", "provenance", "INFERRED_FROM_CHI_MOUNT",
			"mount_path", path)
		add(ent)
	}

	// 6. r.Use(middleware) -> SCOPE.Pattern
	for _, m := range reChiUse.FindAllStringSubmatchIndex(src, -1) {
		mwExpr := strings.TrimSpace(src[m[4]:m[5]])
		ent := makeEntity(mwExpr, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "chi", "provenance", "INFERRED_FROM_CHI_MIDDLEWARE",
			"pattern_kind", "middleware")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
