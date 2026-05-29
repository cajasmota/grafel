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
	extractor.Register("custom_go_gorilla_mux", &gorillaMuxExtractor{})
}

type gorillaMuxExtractor struct{}

func (e *gorillaMuxExtractor) Language() string { return "custom_go_gorilla_mux" }

var (
	reGorillaRouter = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*mux\.NewRouter\s*\(\s*\)`,
	)
	// Subrouter scope: api := r.PathPrefix("/api").Subrouter()
	reGorillaSubrouter = regexp.MustCompile(
		`(?m)(\w+)\s*:?=\s*\w+\.PathPrefix\s*\(\s*"([^"]+)"\s*\)\.Subrouter\s*\(\s*\)`,
	)
	// Route registration: r.HandleFunc("/path", handler) optionally chained
	// with .Methods("GET", "POST"). We capture the registration plus the rest
	// of the line, then scan the tail for a .Methods(...) call to recover the
	// HTTP verb(s).
	reGorillaRoute = regexp.MustCompile(
		`(?m)(\w+)\.(HandleFunc|Handle)\s*\(\s*"([^"]+)"([^\n]*)`,
	)
	reGorillaMethods = regexp.MustCompile(`\.Methods\s*\(\s*((?:"[^"]+"\s*,?\s*)+)\)`)
	reGorillaMethod  = regexp.MustCompile(`"([^"]+)"`)
	reGorillaUse     = regexp.MustCompile(
		`(?m)(\w+)\.Use\s*\(\s*((?:[^()]+|\([^)]*\))+?)\s*\)`,
	)
)

func (e *gorillaMuxExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gorilla_mux_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gorilla-mux"),
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

	// 1. mux.NewRouter() -> SCOPE.Service
	for _, m := range reGorillaRouter.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorilla-mux", "provenance", "INFERRED_FROM_GORILLA_ROUTER",
			"constructor", "mux.NewRouter")
		add(ent)
	}

	// 2. PathPrefix(...).Subrouter() -> SCOPE.Component (prefix scope)
	groupPaths := make(map[string]string)
	for _, m := range reGorillaSubrouter.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		path := src[m[4]:m[5]]
		groupPaths[varName] = path
		ent := makeEntity(path, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorilla-mux", "provenance", "INFERRED_FROM_GORILLA_SUBROUTER",
			"group_path", path)
		add(ent)
	}

	// 3. HandleFunc/Handle routes (+ optional .Methods) -> SCOPE.Operation/endpoint
	for _, m := range reGorillaRoute.FindAllStringSubmatchIndex(src, -1) {
		routerVar := src[m[2]:m[3]]
		path := src[m[6]:m[7]]
		if gp, ok := groupPaths[routerVar]; ok {
			path = gp + path
		}
		tail := src[m[8]:m[9]]
		methods := []string{}
		if mm := reGorillaMethods.FindStringSubmatch(tail); mm != nil {
			for _, vm := range reGorillaMethod.FindAllStringSubmatch(mm[1], -1) {
				methods = append(methods, strings.ToUpper(vm[1]))
			}
		}
		if len(methods) == 0 {
			// No .Methods() chain: gorilla matches any verb for this path.
			methods = []string{"ANY"}
		}
		for _, method := range methods {
			name := method + " " + path
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "gorilla-mux", "provenance", "INFERRED_FROM_GORILLA_ROUTE",
				"http_method", method, "route_path", path, "router_var", routerVar)
			add(ent)
		}
	}

	// 4. r.Use(middleware) -> SCOPE.Pattern
	for _, m := range reGorillaUse.FindAllStringSubmatchIndex(src, -1) {
		mwExpr := strings.TrimSpace(src[m[4]:m[5]])
		ent := makeEntity(mwExpr, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorilla-mux", "provenance", "INFERRED_FROM_GORILLA_MIDDLEWARE",
			"pattern_kind", "middleware")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
