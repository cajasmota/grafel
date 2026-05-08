package golang

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_go_gin", &ginExtractor{})
}

type ginExtractor struct{}

func (e *ginExtractor) Language() string { return "custom_go_gin" }

var (
	reGinEngine = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*gin\.(?:Default|New)\s*\(\s*\)`,
	)
	reGinGroup = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]+)"`,
	)
	reGinRoute = regexp.MustCompile(
		`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any)\s*\(\s*"([^"]+)"`,
	)
	reGinUse = regexp.MustCompile(
		`(?m)(\w+)\.Use\s*\(\s*((?:[^()]+|\([^)]*\))+?)\s*\)`,
	)
	reGinBind = regexp.MustCompile(
		`(?m)c\.(ShouldBindJSON|BindJSON|ShouldBindQuery|ShouldBind|ShouldBindForm|ShouldBindUri|BindQuery)\s*\(\s*&?(\w+)`,
	)
	reGinValidator = regexp.MustCompile(
		`(?m)validate\.RegisterValidation\s*\(\s*"([^"]+)"`,
	)
	reGinNoRoute = regexp.MustCompile(
		`(?m)(\w+)\.(NoRoute|NoMethod)\s*\(`,
	)
	reGinStatic = regexp.MustCompile(
		`(?m)(\w+)\.Static(?:FS)?\s*\(\s*"([^"]+)"`,
	)
)

func (e *ginExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gin_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gin"),
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

	// 1. gin.Default()/gin.New() engine -> SCOPE.Service
	for _, m := range reGinEngine.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_ENGINE")
		add(ent)
	}

	// 2. router.Group -> SCOPE.Component
	groupPaths := make(map[string]string) // varName -> path
	for _, m := range reGinGroup.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		path := src[m[6]:m[7]]
		groupPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_GROUP",
			"group_path", path)
		add(ent)
	}

	// 3. HTTP routes -> SCOPE.Operation/endpoint
	for _, m := range reGinRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := src[m[2]:m[3]]
		method := strings.ToUpper(src[m[4]:m[5]])
		path := src[m[6]:m[7]]
		// Resolve group prefix
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		add(ent)
	}

	// 4. .Use(middleware) -> SCOPE.Pattern
	for _, m := range reGinUse.FindAllStringSubmatchIndex(src, -1) {
		mwExpr := strings.TrimSpace(src[m[4]:m[5]])
		ent := makeEntity(mwExpr, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_MIDDLEWARE",
			"pattern_kind", "middleware")
		add(ent)
	}

	// 5. c.ShouldBindJSON etc -> SCOPE.Schema
	for _, m := range reGinBind.FindAllStringSubmatchIndex(src, -1) {
		bindMethod := src[m[2]:m[3]]
		bindType := src[m[4]:m[5]]
		name := fmt.Sprintf("bind:%s:%s", bindMethod, bindType)
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_BINDING",
			"bind_method", bindMethod, "bind_type", bindType)
		add(ent)
	}

	// 6. validate.RegisterValidation -> SCOPE.Pattern
	for _, m := range reGinValidator.FindAllStringSubmatchIndex(src, -1) {
		tag := src[m[2]:m[3]]
		ent := makeEntity("validator:"+tag, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_VALIDATOR",
			"pattern_kind", "validator", "tag", tag)
		add(ent)
	}

	// 7. NoRoute/NoMethod -> SCOPE.Pattern
	for _, m := range reGinNoRoute.FindAllStringSubmatchIndex(src, -1) {
		handlerKind := src[m[4]:m[5]]
		ent := makeEntity(handlerKind, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_ERROR_HANDLER",
			"handler_kind", handlerKind, "pattern_kind", "error_handler")
		add(ent)
	}

	// 8. Static routes -> SCOPE.Operation/endpoint
	for _, m := range reGinStatic.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[4]:m[5]]
		name := "GET " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gin", "provenance", "INFERRED_FROM_GIN_ROUTE",
			"http_method", "GET", "route_path", path, "is_static", "true")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
