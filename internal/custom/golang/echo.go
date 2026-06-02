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
	extractor.Register("custom_go_echo", &echoExtractor{})
}

type echoExtractor struct{}

func (e *echoExtractor) Language() string { return "custom_go_echo" }

var (
	reEchoEngine = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*echo\.New\s*\(\s*\)`,
	)
	reEchoGroup = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]+)"`,
	)
	reEchoRoute = regexp.MustCompile(
		`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|CONNECT|TRACE|Any)\s*\(\s*"([^"]+)"`,
	)
	reEchoBind = regexp.MustCompile(
		`(?m)c\.(Bind|BindJSON|BindQuery|BindParam|BindBody|BindHeader)\s*\(\s*&?(\w+)`,
	)
	reEchoStatic = regexp.MustCompile(
		`(?m)(\w+)\.(Static|File)\s*\(\s*"([^"]+)"`,
	)
	reEchoValidator = regexp.MustCompile(
		`(?m)(\w+)\.Validator\s*=\s*&?(\w+)\s*\{`,
	)
)

func (e *echoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.echo_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "echo"),
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

	// #3734 — endpoint protection. Echo passes trailing middleware after the
	// handler (`e.GET(path, h, mw)`); the shared index classifies each route /
	// group / engine arg independently, so both orderings resolve.
	authIdx := buildGoRouteAuthIndex(src)

	// 1. echo.New() engine -> SCOPE.Service
	for _, m := range reEchoEngine.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "echo", "provenance", "INFERRED_FROM_ECHO_ENGINE",
			"constructor", "echo.New")
		add(ent)
	}

	// 2. .Group() -> SCOPE.Component
	groupPaths := make(map[string]string)
	for _, m := range reEchoGroup.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		path := src[m[6]:m[7]]
		groupPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "echo", "provenance", "INFERRED_FROM_ECHO_GROUP",
			"group_path", path)
		add(ent)
	}

	// 3. HTTP routes -> SCOPE.Operation/endpoint
	for _, m := range reEchoRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := src[m[2]:m[3]]
		method := strings.ToUpper(src[m[4]:m[5]])
		ownPath := src[m[6]:m[7]]
		path := ownPath
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "echo", "provenance", "INFERRED_FROM_ECHO_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		// #3734 — stamp endpoint protection (inline > group > engine-wide).
		authIdx.resolve(routerVar, method, ownPath).stamp(ent.Properties)
		add(ent)
	}

	// 4. .Use(middleware, …) -> ordered SCOPE.Pattern middleware (+ auth)
	for _, uc := range findUseCalls(src) {
		chain := parseMiddlewareChain(uc.Args)
		emitMiddlewareChain(add, chain, "echo",
			"INFERRED_FROM_ECHO_MIDDLEWARE", "INFERRED_FROM_ECHO_AUTH",
			file.Path, file.Language, uc.Line)
	}

	// 5. c.Bind* -> SCOPE.Schema
	for _, m := range reEchoBind.FindAllStringSubmatchIndex(src, -1) {
		bindMethod := src[m[2]:m[3]]
		bindType := src[m[4]:m[5]]
		name := "bind:" + bindMethod + ":" + bindType
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "echo", "provenance", "INFERRED_FROM_ECHO_BINDING",
			"bind_method", bindMethod)
		add(ent)
	}

	// 6. Static/File -> SCOPE.Operation/endpoint
	for _, m := range reEchoStatic.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[6]:m[7]]
		name := "GET " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "echo", "provenance", "INFERRED_FROM_ECHO_ROUTE",
			"http_method", "GET", "route_path", path, "is_static", "true")
		add(ent)
	}

	// 7. Custom validator -> SCOPE.Pattern
	for _, m := range reEchoValidator.FindAllStringSubmatchIndex(src, -1) {
		validatorType := src[m[4]:m[5]]
		ent := makeEntity("validator:"+validatorType, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "echo", "provenance", "INFERRED_FROM_ECHO_VALIDATOR",
			"pattern_kind", "validator")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
