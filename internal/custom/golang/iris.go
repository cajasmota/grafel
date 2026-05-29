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
	extractor.Register("custom_go_iris", &irisExtractor{})
}

type irisExtractor struct{}

func (e *irisExtractor) Language() string { return "custom_go_iris" }

var (
	// app := iris.New() / app := iris.Default()
	reIrisApp = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*iris\.(?:New|Default)\s*\(\s*\)`,
	)
	// v1 := app.Party("/v1") / api := app.PartyFunc("/api", ...)
	reIrisParty = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Party(?:Func)?\s*\(\s*"([^"]+)"`,
	)
	// app.Get/Post/Put/Delete/Patch/Head/Options/Connect/Trace/Any("/path", h)
	reIrisRoute = regexp.MustCompile(
		`(?m)(\w+)\.(Get|Post|Put|Delete|Patch|Head|Options|Connect|Trace|Any)\s*\(\s*"([^"]+)"`,
	)
	// app.Handle("GET", "/path", h) / app.HandleMany("GET POST", "/path", h)
	reIrisHandle = regexp.MustCompile(
		`(?m)(\w+)\.Handle(?:Many)?\s*\(\s*"([^"]+)"\s*,\s*"([^"]+)"`,
	)
	// app.Use(mw) / v1.UseRouter(mw)
	reIrisUse = regexp.MustCompile(
		`(?m)(\w+)\.Use(?:Router|Global)?\s*\(\s*((?:[^()]+|\([^)]*\))+?)\s*\)`,
	)
)

func (e *irisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.iris_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "iris"),
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

	// 1. iris.New()/iris.Default() application -> SCOPE.Service.
	for _, m := range reIrisApp.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "iris", "provenance", "INFERRED_FROM_IRIS_APP")
		add(ent)
	}

	// 2. Party groups -> SCOPE.Component. Resolve nested-party prefixes.
	partyPaths := make(map[string]string) // varName -> full path
	for _, m := range reIrisParty.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		parent := submatch(src, m, 4)
		path := submatch(src, m, 6)
		if pp, ok := partyPaths[parent]; ok {
			path = pp + path
		}
		partyPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "iris", "provenance", "INFERRED_FROM_IRIS_PARTY",
			"group_path", path)
		add(ent)
	}

	// 3. Verb-method routes -> SCOPE.Operation/endpoint.
	for _, m := range reIrisRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		method := strings.ToUpper(submatch(src, m, 4))
		path := submatch(src, m, 6)
		if pp, ok := partyPaths[routerVar]; ok {
			path = pp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "iris", "provenance", "INFERRED_FROM_IRIS_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		add(ent)
	}

	// 4. Handle("METHOD", "/path", h) -> SCOPE.Operation/endpoint. The method
	//    arg may be space-separated for HandleMany.
	for _, m := range reIrisHandle.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		methods := submatch(src, m, 4)
		path := submatch(src, m, 6)
		if pp, ok := partyPaths[routerVar]; ok {
			path = pp + path
		}
		for _, mm := range strings.Fields(methods) {
			method := strings.ToUpper(mm)
			name := method + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "iris", "provenance", "INFERRED_FROM_IRIS_HANDLE",
				"http_method", method, "route_path", path, "router_var", routerVar)
			add(ent)
		}
	}

	// 5. Use(middleware) -> SCOPE.Pattern.
	for _, m := range reIrisUse.FindAllStringSubmatchIndex(src, -1) {
		mwExpr := strings.TrimSpace(submatch(src, m, 4))
		if mwExpr == "" {
			continue
		}
		ent := makeEntity(mwExpr, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "iris", "provenance", "INFERRED_FROM_IRIS_MIDDLEWARE",
			"pattern_kind", "middleware")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
