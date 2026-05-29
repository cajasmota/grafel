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
	extractor.Register("custom_go_revel", &revelExtractor{})
}

type revelExtractor struct{}

func (e *revelExtractor) Language() string { return "custom_go_revel" }

var (
	// Revel conf/routes line:  METHOD  /path        Controller.Action
	//   GET    /                      App.Index
	//   POST   /users/:id             Users.Update
	//   *      /:controller/:action   :controller.:action   (auto-routing)
	// Capture groups: 1=verb, 2=path, 3=controller.action.
	reRevelRoute = regexp.MustCompile(
		`(?m)^[ \t]*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|WS|\*)\s+(/\S*)\s+([\w.:]+)`,
	)
	// module:jobs / module:testrunner mounts in conf/routes.
	reRevelModule = regexp.MustCompile(
		`(?m)^[ \t]*module:(\w+)`,
	)
	// Revel controller-action method on a struct embedding *revel.Controller:
	//   func (c App) Index() revel.Result { ... }
	// Capture groups: 1=receiver type, 2=action method name.
	reRevelAction = regexp.MustCompile(
		`(?m)func\s*\(\s*\w+\s+\*?(\w+)\s*\)\s*(\w+)\s*\([^)]*\)\s*revel\.Result`,
	)
	// Revel interceptor registration (before/after filters ~ middleware):
	//   revel.InterceptFunc(checkUser, revel.BEFORE, &App{})
	//   revel.InterceptMethod(App.checkUser, revel.BEFORE)
	reRevelIntercept = regexp.MustCompile(
		`(?m)revel\.Intercept(?:Func|Method)\s*\(\s*([\w.]+)`,
	)
)

// isRevelRoutesFile reports whether the file path is a Revel conf/routes file.
// Revel's routes live at conf/routes (no extension); the indexer may also tag
// such files with a routes-style basename.
func isRevelRoutesFile(filePath string) bool {
	if strings.Contains(filePath, "conf/routes") {
		return true
	}
	base := filePath
	if i := strings.LastIndex(filePath, "/"); i >= 0 {
		base = filePath[i+1:]
	}
	return base == "routes"
}

// revelCanonPath converts Revel's :param path segments to {param} form so paths
// are comparable across extractors (gin/iris/beego use {id}).
func revelCanonPath(raw string) string {
	if q := strings.Index(raw, "?"); q >= 0 {
		raw = raw[:q]
	}
	return reRevelColonParam.ReplaceAllString(raw, "{$1}")
}

var reRevelColonParam = regexp.MustCompile(`:(\w+)`)

func (e *revelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.revel_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "revel"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
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

	routesFile := isRevelRoutesFile(file.Path)

	// --- conf/routes file: definitive route table (full synthesis). ---
	if routesFile {
		for _, m := range reRevelRoute.FindAllStringSubmatchIndex(src, -1) {
			verb := strings.ToUpper(submatch(src, m, 2))
			if verb == "*" {
				verb = "ANY"
			}
			path := revelCanonPath(submatch(src, m, 4))
			action := submatch(src, m, 6)
			line := lineOf(src, m[0])
			name := verb + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
			setProps(&ent, "framework", "revel", "provenance", "INFERRED_FROM_REVEL_ROUTES",
				"http_method", verb, "route_path", path, "route_type", "conf_routes")
			// Attribute the handler unless it is an auto-routing wildcard
			// (:controller.:action), which resolves dynamically at runtime.
			if action != "" && !strings.Contains(action, ":") {
				ent.Properties["handler"] = action
			} else if strings.Contains(action, ":") {
				ent.Properties["is_auto"] = "true"
			}
			add(ent)
		}

		// module:<name> mounts -> SCOPE.Component (sub-app route prefixes).
		for _, m := range reRevelModule.FindAllStringSubmatchIndex(src, -1) {
			mod := submatch(src, m, 2)
			ent := makeEntity("module:"+mod, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "revel", "provenance", "INFERRED_FROM_REVEL_MODULE",
				"module", mod, "is_mount", "true")
			add(ent)
		}

		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	// --- Go controller files: controller-action handler attribution. ---
	if file.Language != "go" {
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	// Controller-action methods (returning revel.Result) -> SCOPE.Pattern. By
	// convention Revel maps Controller.Action to a route; without the routes
	// file we cannot synthesize the method+path, so these are handler patterns
	// available for attribution against conf/routes entries.
	for _, m := range reRevelAction.FindAllStringSubmatchIndex(src, -1) {
		recv := submatch(src, m, 2)
		action := submatch(src, m, 4)
		full := recv + "." + action
		ent := makeEntity("handler:"+full, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "revel", "provenance", "INFERRED_FROM_REVEL_ACTION",
			"pattern_kind", "controller_action", "handler", full,
			"controller", recv, "action", action)
		add(ent)
	}

	// Interceptors (before/after filters) -> SCOPE.Pattern middleware.
	for _, m := range reRevelIntercept.FindAllStringSubmatchIndex(src, -1) {
		fn := submatch(src, m, 2)
		if fn == "" {
			continue
		}
		ent := makeEntity("interceptor:"+fn, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "revel", "provenance", "INFERRED_FROM_REVEL_INTERCEPT",
			"pattern_kind", "middleware", "middleware_name", fn)
		if kind := classifyAuthMiddleware(fn); kind != "" {
			setProps(&ent, "is_auth", "true", "auth_kind", kind)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
