package golang

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	"regexp"
)

func init() {
	extractor.Register("custom_go_beego", &beegoExtractor{})
}

type beegoExtractor struct{}

func (e *beegoExtractor) Language() string { return "custom_go_beego" }

var (
	// beego.Router("/path", &UserController{}, "get:GetAll;post:Post")
	// web.Router("/path", &UserController{})
	// ns.Router("/path", &UserController{}, "*:Index")
	// The optional 3rd argument is the method-mapping string.
	reBeegoRouter = regexp.MustCompile(
		`(?m)(?:beego|web|\w+)\.Router\s*\(\s*"([^"]+)"\s*,\s*&?(\w+)\{?\}?\s*(?:,\s*"([^"]+)")?`,
	)
	// web.NewNamespace("/v1", ...) / beego.NewNamespace("/api/v1", ...)
	reBeegoNamespace = regexp.MustCompile(
		`(?m)(?:beego|web)\.NewNamespace\s*\(\s*"([^"]+)"`,
	)
	// Auto-routing: beego.AutoRouter(&UserController{}) / web.AutoRouter(...)
	reBeegoAutoRouter = regexp.MustCompile(
		`(?m)(?:beego|web)\.AutoRouter\s*\(\s*&?(\w+)\{?\}?`,
	)
	// Annotation-comment routing: // @router /path/:id [get,post]
	reBeegoAnnotation = regexp.MustCompile(
		`(?m)//\s*@router\s+(\S+)\s*\[([^\]]+)\]`,
	)
	// beego.Run() / web.Run() server entry -> SCOPE.Service
	reBeegoRun = regexp.MustCompile(
		`(?m)(?:beego|web)\.Run\s*\(`,
	)
)

// beegoMethodMap parses Beego's "get:GetAll;post:Post" mapping string into a
// slice of (httpMethod, handlerMethod) pairs. A "*" maps to every verb, which
// we represent as the ANY pseudo-method. Bare entries with no ":" are treated
// as a "*" mapping to that handler method.
func beegoMethodMap(spec string) [][2]string {
	var out [][2]string
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		verb, handler := "ANY", part
		if i := strings.Index(part, ":"); i >= 0 {
			verb = strings.TrimSpace(part[:i])
			handler = strings.TrimSpace(part[i+1:])
			if verb == "*" || verb == "" {
				verb = "ANY"
			} else {
				verb = strings.ToUpper(verb)
			}
		}
		out = append(out, [2]string{verb, handler})
	}
	return out
}

func (e *beegoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.beego_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "beego"),
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

	// 1. beego.Run()/web.Run() -> SCOPE.Service (the app server).
	for _, m := range reBeegoRun.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("beego_app", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "beego", "provenance", "INFERRED_FROM_BEEGO_RUN")
		add(ent)
	}

	// 2. Namespaces -> SCOPE.Component (route-prefix groups).
	for _, m := range reBeegoNamespace.FindAllStringSubmatchIndex(src, -1) {
		path := submatch(src, m, 2)
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "beego", "provenance", "INFERRED_FROM_BEEGO_NAMESPACE",
			"group_path", path)
		add(ent)
	}

	// 3. Method-style router registrations -> SCOPE.Operation/endpoint.
	for _, m := range reBeegoRouter.FindAllStringSubmatchIndex(src, -1) {
		path := submatch(src, m, 2)
		controller := submatch(src, m, 4)
		methodSpec := submatch(src, m, 6)
		line := lineOf(src, m[0])

		pairs := beegoMethodMap(methodSpec)
		if len(pairs) == 0 {
			// No explicit mapping string: Beego maps the request to the
			// controller method matching the HTTP verb (RESTful default).
			pairs = [][2]string{{"ANY", ""}}
		}
		for _, p := range pairs {
			verb, handler := p[0], p[1]
			name := verb + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
			setProps(&ent, "framework", "beego", "provenance", "INFERRED_FROM_BEEGO_ROUTER",
				"http_method", verb, "route_path", path, "controller", controller)
			if handler != "" {
				ent.Properties["handler"] = controller + "." + handler
			}
			add(ent)
		}
	}

	// 4. AutoRouter -> SCOPE.Operation/endpoint (controller-driven auto routes).
	for _, m := range reBeegoAutoRouter.FindAllStringSubmatchIndex(src, -1) {
		controller := submatch(src, m, 2)
		name := "ANY /" + strings.TrimSuffix(controller, "Controller")
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "beego", "provenance", "INFERRED_FROM_BEEGO_AUTOROUTER",
			"http_method", "ANY", "controller", controller, "is_auto", "true")
		add(ent)
	}

	// 5. Annotation-comment routes: // @router /path [get,post]
	for _, m := range reBeegoAnnotation.FindAllStringSubmatchIndex(src, -1) {
		path := submatch(src, m, 2)
		verbs := submatch(src, m, 4)
		line := lineOf(src, m[0])
		for _, v := range strings.Split(verbs, ",") {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			verb := strings.ToUpper(v)
			if verb == "*" {
				verb = "ANY"
			}
			name := verb + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, line)
			setProps(&ent, "framework", "beego", "provenance", "INFERRED_FROM_BEEGO_ANNOTATION",
				"http_method", verb, "route_path", path)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
