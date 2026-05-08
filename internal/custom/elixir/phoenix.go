package elixir

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
	extractor.Register("custom_elixir_phoenix", &phoenixExtractor{})
}

type phoenixExtractor struct{}

func (e *phoenixExtractor) Language() string { return "custom_elixir_phoenix" }

var (
	rePhoenixHTTPRoute = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|options|head)\s+"([^"]+)"`,
	)
	rePhoenixLiveRoute = regexp.MustCompile(
		`(?m)^\s*live\s+"([^"]+)"`,
	)
	rePhoenixResources = regexp.MustCompile(
		`(?m)^\s*resources\s+"([^"]+)"`,
	)
	rePhoenixScope = regexp.MustCompile(
		`(?m)^\s*scope\s+"([^"]+)"`,
	)
	rePhoenixPipeline = regexp.MustCompile(
		`(?m)^\s*pipeline\s+:([a-z_]+)\s+do`,
	)
	rePhoenixPlug = regexp.MustCompile(
		`(?m)^\s*plug\s+:?(\w+)`,
	)
	rePhoenixLiveView = regexp.MustCompile(
		`(?m)use\s+Phoenix\.LiveView\b`,
	)
	rePhoenixLiveComponent = regexp.MustCompile(
		`(?m)use\s+Phoenix\.LiveComponent\b`,
	)
	rePhoenixModuleDecl = regexp.MustCompile(
		`(?m)^defmodule\s+([\w.]+)`,
	)
	rePhoenixLiveViewHandler = regexp.MustCompile(
		`(?m)def\s+(mount|handle_event|handle_info|handle_params|render)\s*\(`,
	)
	rePhoenixControllerAction = regexp.MustCompile(
		`(?m)def\s+(index|show|new|create|edit|update|delete|action)\s*\(`,
	)
)

// phoenixCRUDRoutes are the 8 REST routes for resources.
var phoenixCRUDRoutes = []struct{ method, suffix string }{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/new"},
	{"GET", "/:id"},
	{"GET", "/:id/edit"},
	{"PATCH", "/:id"},
	{"PUT", "/:id"},
	{"DELETE", "/:id"},
}

func (e *phoenixExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.phoenix_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "phoenix"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "elixir" {
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

	// 1. HTTP routes -> SCOPE.Operation/endpoint
	for _, m := range rePhoenixHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_ROUTE",
			"http_method", method, "route_path", path)
		add(ent)
	}

	// 2. live routes -> SCOPE.Operation/endpoint
	for _, m := range rePhoenixLiveRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("LIVE "+path, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_ROUTE",
			"route_path", path, "route_type", "live")
		add(ent)
	}

	// 3. resources -> CRUD expansion
	for _, m := range rePhoenixResources.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ln := lineOf(src, m[0])
		for _, cr := range phoenixCRUDRoutes {
			routePath := path + cr.suffix
			name := cr.method + " " + routePath
			ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_RESOURCES",
				"http_method", cr.method, "route_path", routePath)
			add(ent)
		}
	}

	// 4. scope blocks -> SCOPE.Pattern
	for _, m := range rePhoenixScope.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("scope:"+path, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_SCOPE",
			"scope_path", path)
		add(ent)
	}

	// 5. pipeline declarations -> SCOPE.Pattern
	for _, m := range rePhoenixPipeline.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("pipeline:"+name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_PIPELINE",
			"pipeline_name", name)
		add(ent)
	}

	// 6. plug declarations -> SCOPE.Pattern
	for _, m := range rePhoenixPlug.FindAllStringSubmatchIndex(src, -1) {
		plugName := src[m[2]:m[3]]
		ent := makeEntity("plug:"+plugName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_PLUG",
			"plug_name", plugName)
		add(ent)
	}

	// 7. LiveView module -> SCOPE.UIComponent
	liveViewMatches := rePhoenixLiveView.FindAllStringIndex(src, -1)
	for _, m := range liveViewMatches {
		// Find preceding defmodule
		prefix := src[:m[0]]
		cm := rePhoenixModuleDecl.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			moduleName := cm[len(cm)-1][1]
			ent := makeEntity(moduleName, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_VIEW")
			add(ent)
		}
	}

	// 8. LiveComponent module -> SCOPE.UIComponent
	for _, m := range rePhoenixLiveComponent.FindAllStringIndex(src, -1) {
		prefix := src[:m[0]]
		cm := rePhoenixModuleDecl.FindAllStringSubmatch(prefix, -1)
		if len(cm) > 0 {
			moduleName := cm[len(cm)-1][1]
			ent := makeEntity(moduleName, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_COMPONENT")
			add(ent)
		}
	}

	// 9. LiveView handlers -> SCOPE.Operation/function
	for _, m := range rePhoenixLiveViewHandler.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		ent := makeEntity(handler, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_LIVE_VIEW_HANDLER",
			"handler_type", handler)
		add(ent)
	}

	// 10. Controller actions -> SCOPE.Operation/function
	for _, m := range rePhoenixControllerAction.FindAllStringSubmatchIndex(src, -1) {
		action := src[m[2]:m[3]]
		ent := makeEntity("action:"+action, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "phoenix", "provenance", "INFERRED_FROM_PHOENIX_CONTROLLER_ACTION",
			"action_name", action)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
