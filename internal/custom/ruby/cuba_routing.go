// cuba_routing.go — Cuba routing tree endpoint_synthesis + handler_attribution.
//
// Cuba uses a nested `on "path" do ... end` DSL for routing. Unlike Rails
// resources or Sinatra verb blocks, Cuba's router is a Rack-level tree:
//
//	Cuba.define do
//	  on "users" do
//	    on get do
//	      res.write users.all.to_json
//	    end
//	    on post do
//	      res.write create_user(req.params).to_json
//	    end
//	    on :id do
//	      on get  do ... end
//	      on delete do ... end
//	    end
//	  end
//	end
//
// This extractor synthesises endpoint entities by combining the path segments
// and the verb signals (get/post/put/patch/delete) it finds in the tree.
// Because Cuba nesting is free-form, we do a best-effort single-level parse:
// each `on "path"` or `on :param` block is emitted as an endpoint entity,
// and any immediate verb (`on get`, `on post`, etc.) inside it is attributed
// as the handler.
//
// Coverage cells flipped:
//
//	lang.ruby.framework.cuba  Routing/endpoint_synthesis   → partial
//	lang.ruby.framework.cuba  Routing/handler_attribution  → partial
//
// Part of #3282.
package ruby

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
	extractor.Register("ruby_cuba_routing", &cubaSynthesisExtractor{})
}

type cubaSynthesisExtractor struct{}

func (e *cubaSynthesisExtractor) Language() string { return "ruby_cuba_routing" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// Cuba.define do / App = Cuba.new / class App < Cuba
	reCubaEntry = regexp.MustCompile(
		`(?m)\bCuba\.(?:define|new)\b|<\s*Cuba\b`,
	)

	// on "path" do
	reCubaOnString = regexp.MustCompile(
		`(?m)^\s*on\s+['"]([^'"]+)['"]\s+do\b`,
	)

	// on :param do (parametric path segment)
	reCubaOnParam = regexp.MustCompile(
		`(?m)^\s*on\s+:([a-z_]+)\s+do\b`,
	)

	// on get do / on post do / on put do / on patch do / on delete do
	reCubaOnVerb = regexp.MustCompile(
		`(?m)^\s*on\s+(get|post|put|patch|delete)\b`,
	)

	// Cuba run sub-app: run SubApp inside define
	reCubaRun = regexp.MustCompile(
		`(?m)^\s*run\s+([A-Z][A-Za-z0-9_:]+)`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *cubaSynthesisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.cuba_routing_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "ruby" {
		return nil, nil
	}

	src := string(file.Content)

	// Only process Cuba files.
	if !reCubaEntry.MatchString(src) {
		return nil, nil
	}

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

	// ---- endpoint_synthesis: on "path" string segments ----
	for _, idx := range reCubaOnString.FindAllStringSubmatchIndex(src, -1) {
		path := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		routeName := "cuba_on:/" + path
		ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "cuba",
			"provenance", "INFERRED_FROM_CUBA_ON_STRING",
			"route_path", "/"+path,
		)
		add(ent)
	}

	// ---- endpoint_synthesis: on :param parametric segments ----
	for _, idx := range reCubaOnParam.FindAllStringSubmatchIndex(src, -1) {
		param := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		routeName := fmt.Sprintf("cuba_on:/:%s", param)
		ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "cuba",
			"provenance", "INFERRED_FROM_CUBA_ON_PARAM",
			"route_path", "/:"+param,
			"param_name", param,
		)
		add(ent)
	}

	// ---- handler_attribution: on <verb> blocks ----
	// Each `on get` / `on post` etc. is attributed as a handler entity.
	for _, idx := range reCubaOnVerb.FindAllStringSubmatchIndex(src, -1) {
		verb := strings.ToUpper(src[idx[2]:idx[3]])
		ln := lineOf(src, idx[0])
		name := "cuba_handler:" + verb
		ent := makeEntity(name, "SCOPE.Pattern", "handler", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "cuba",
			"provenance", "INFERRED_FROM_CUBA_ON_VERB",
			"http_method", verb,
		)
		add(ent)
	}

	// ---- run SubApp (sub-app mounting) ----
	for _, idx := range reCubaRun.FindAllStringSubmatchIndex(src, -1) {
		appName := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		name := "cuba_mount:" + appName
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, ln)
		setProps(&ent,
			"framework", "cuba",
			"provenance", "INFERRED_FROM_CUBA_RUN",
			"mounted_app", appName,
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
