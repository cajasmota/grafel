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
	extractor.Register("custom_elixir_plug", &plugExtractor{})
}

type plugExtractor struct{}

func (e *plugExtractor) Language() string { return "custom_elixir_plug" }

// Plug patterns:
//   - use Plug.Router              → router module
//   - plug :name / plug Module     → middleware step
//   - match "/path" do ... end     → route (Plug.Router DSL)
//   - forward "/prefix", to: ...   → route forward
//   - def call(conn, opts)         → Plug.call/2 implementation
//   - def init(opts)               → Plug.init/1 implementation
//   - use Plug.Builder             → plug builder module
//   - Plug.Conn.* accesses         → request handling

var (
	rePlugRouter = regexp.MustCompile(
		`(?m)use\s+Plug\.Router\b`,
	)
	rePlugBuilder = regexp.MustCompile(
		`(?m)use\s+Plug\.Builder\b`,
	)
	rePlugStep = regexp.MustCompile(
		`(?m)^\s*plug\s+(:[\w]+|[\w.]+)(?:\s*,.*)?$`,
	)
	rePlugMatch = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|options|head|match)\s+"([^"]+)"`,
	)
	rePlugForward = regexp.MustCompile(
		`(?m)^\s*forward\s+"([^"]+)"`,
	)
	rePlugCallImpl = regexp.MustCompile(
		`(?m)def\s+call\s*\(\s*(?:conn|%Plug\.Conn{})`,
	)
	rePlugModuleDecl = regexp.MustCompile(
		`(?m)^defmodule\s+([\w.]+)`,
	)
)

func (e *plugExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.plug_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "plug"),
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

	// 1. use Plug.Router → SCOPE.Component/router
	for _, m := range rePlugRouter.FindAllStringIndex(src, -1) {
		prefix := src[:m[0]]
		parentMod := "PlugRouter"
		if mm := rePlugModuleDecl.FindAllStringSubmatch(prefix, -1); len(mm) > 0 {
			parentMod = mm[len(mm)-1][1]
		}
		ent := makeEntity(parentMod, "SCOPE.Component", "router", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "plug", "provenance", "INFERRED_FROM_PLUG_ROUTER")
		add(ent)
	}

	// 2. use Plug.Builder → SCOPE.Component/builder
	for _, m := range rePlugBuilder.FindAllStringIndex(src, -1) {
		prefix := src[:m[0]]
		parentMod := "PlugBuilder"
		if mm := rePlugModuleDecl.FindAllStringSubmatch(prefix, -1); len(mm) > 0 {
			parentMod = mm[len(mm)-1][1]
		}
		ent := makeEntity(parentMod, "SCOPE.Component", "builder", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "plug", "provenance", "INFERRED_FROM_PLUG_BUILDER")
		add(ent)
	}

	// 3. plug :name / plug Module  → SCOPE.Pattern/middleware
	for _, m := range rePlugStep.FindAllStringSubmatchIndex(src, -1) {
		plugName := strings.TrimSpace(src[m[2]:m[3]])
		ent := makeEntity("plug:"+plugName, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "plug", "provenance", "INFERRED_FROM_PLUG_STEP",
			"plug_name", plugName)
		add(ent)
	}

	// 4. match "/path" / get "/path" / post "/path" etc. → SCOPE.Operation/endpoint
	for _, m := range rePlugMatch.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "plug", "provenance", "INFERRED_FROM_PLUG_MATCH",
			"http_method", method, "route_path", path)
		add(ent)
	}

	// 5. forward "/prefix" → SCOPE.Operation/forward
	for _, m := range rePlugForward.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		ent := makeEntity("forward:"+path, "SCOPE.Operation", "forward", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "plug", "provenance", "INFERRED_FROM_PLUG_FORWARD",
			"forward_path", path)
		add(ent)
	}

	// 6. def call(conn, opts) → SCOPE.Operation/plug_impl
	for _, m := range rePlugCallImpl.FindAllStringIndex(src, -1) {
		ent := makeEntity("call", "SCOPE.Operation", "plug_impl", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "plug", "provenance", "INFERRED_FROM_PLUG_CALL_IMPL")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
