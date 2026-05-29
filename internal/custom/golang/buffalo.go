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
	extractor.Register("custom_go_buffalo", &buffaloExtractor{})
}

type buffaloExtractor struct{}

func (e *buffaloExtractor) Language() string { return "custom_go_buffalo" }

var (
	// app := buffalo.New(opts) / app = buffalo.New(buffalo.Options{...})
	reBuffaloApp = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*buffalo\.New\s*\(`,
	)
	// app.GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS("/path", Handler)
	reBuffaloRoute = regexp.MustCompile(
		`(?m)(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"\s*,\s*([\w.]+)`,
	)
	// app.Resource("/users", UsersResource{}) — RESTful resource expansion.
	reBuffaloResource = regexp.MustCompile(
		`(?m)(\w+)\.Resource\s*\(\s*"([^"]+)"\s*,\s*&?([\w.]+)\{?\}?`,
	)
	// g := app.Group("/api") — route-prefix group (returns a *buffalo.App).
	reBuffaloGroup = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]+)"`,
	)
	// app.Mount("/admin", admin.App()) — sub-app mount at a path prefix.
	reBuffaloMount = regexp.MustCompile(
		`(?m)(\w+)\.Mount\s*\(\s*"([^"]+)"`,
	)
	// app.Use(mw) / app.Middleware.Use(mw) — middleware registration.
	reBuffaloUse = regexp.MustCompile(
		`(?m)(\w+)(?:\.Middleware)?\.Use\s*\(\s*([\w.]+)`,
	)
)

// buffaloResourceRoutes expands a Buffalo Resource registration into its seven
// conventional RESTful routes (mirroring buffalo's Resource interface:
// List/Show/New/Create/Edit/Update/Destroy). path is the resource mount path
// (e.g. "/users"); the returned slice is (httpMethod, routePath, action) tuples.
func buffaloResourceRoutes(path string) [][3]string {
	base := strings.TrimRight(path, "/")
	return [][3]string{
		{"GET", base, "List"},
		{"GET", base + "/new", "New"},
		{"POST", base, "Create"},
		{"GET", base + "/{id}", "Show"},
		{"GET", base + "/{id}/edit", "Edit"},
		{"PUT", base + "/{id}", "Update"},
		{"DELETE", base + "/{id}", "Destroy"},
	}
}

func (e *buffaloExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.buffalo_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "buffalo"),
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

	// 1. buffalo.New(...) application -> SCOPE.Service.
	for _, m := range reBuffaloApp.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "buffalo", "provenance", "INFERRED_FROM_BUFFALO_APP")
		add(ent)
	}

	// 2. Group prefixes + Mount prefixes -> SCOPE.Component. Track group var
	//    prefixes so verb routes on a group resolve their full path.
	groupPaths := make(map[string]string) // varName -> full prefix
	for _, m := range reBuffaloGroup.FindAllStringSubmatchIndex(src, -1) {
		varName := submatch(src, m, 2)
		parent := submatch(src, m, 4)
		path := submatch(src, m, 6)
		if pp, ok := groupPaths[parent]; ok {
			path = pp + path
		}
		groupPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "buffalo", "provenance", "INFERRED_FROM_BUFFALO_GROUP",
			"group_path", path)
		add(ent)
	}
	for _, m := range reBuffaloMount.FindAllStringSubmatchIndex(src, -1) {
		path := submatch(src, m, 4)
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "buffalo", "provenance", "INFERRED_FROM_BUFFALO_MOUNT",
			"group_path", path, "is_mount", "true")
		add(ent)
	}

	// 3. Verb routes -> SCOPE.Operation/endpoint, with group-prefix resolution
	//    and handler attribution from the 4th argument.
	for _, m := range reBuffaloRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		method := strings.ToUpper(submatch(src, m, 4))
		path := submatch(src, m, 6)
		handler := submatch(src, m, 8)
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "buffalo", "provenance", "INFERRED_FROM_BUFFALO_ROUTE",
			"http_method", method, "route_path", path, "router_var", routerVar)
		if handler != "" {
			ent.Properties["handler"] = handler
		}
		add(ent)
	}

	// 4. Resource registrations -> seven conventional RESTful endpoints, each
	//    attributed to the resource type's conventional action method.
	for _, m := range reBuffaloResource.FindAllStringSubmatchIndex(src, -1) {
		routerVar := submatch(src, m, 2)
		path := submatch(src, m, 4)
		resource := submatch(src, m, 6)
		line := lineOf(src, m[0])
		prefix := ""
		if gp, ok := groupPaths[routerVar]; ok {
			prefix = gp
		}
		for _, r := range buffaloResourceRoutes(path) {
			method, routePath, action := r[0], r[1], r[2]
			fullPath := prefix + routePath
			name := method + " " + fullPath
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
			setProps(&ent, "framework", "buffalo", "provenance", "INFERRED_FROM_BUFFALO_RESOURCE",
				"http_method", method, "route_path", fullPath,
				"resource", resource, "handler", resource+"."+action, "is_resource", "true")
			add(ent)
		}
	}

	// 5. Middleware registration -> SCOPE.Pattern (+ auth classification).
	for _, m := range reBuffaloUse.FindAllStringSubmatchIndex(src, -1) {
		mwExpr := submatch(src, m, 4)
		if mwExpr == "" {
			continue
		}
		ent := makeEntity(mwExpr, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "buffalo", "provenance", "INFERRED_FROM_BUFFALO_MIDDLEWARE",
			"pattern_kind", "middleware", "middleware_name", mwExpr)
		if kind := classifyAuthMiddleware(mwExpr); kind != "" {
			setProps(&ent, "is_auth", "true", "auth_kind", kind)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
