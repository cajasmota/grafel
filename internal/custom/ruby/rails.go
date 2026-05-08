package ruby

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_rails", &railsExtractor{})
}

type railsExtractor struct{}

func (e *railsExtractor) Language() string { return "custom_ruby_rails" }

var (
	reRailsResources = regexp.MustCompile(
		`(?m)^\s*resources?\s+:([a-z_]+)`,
	)
	reRailsHTTPRoute = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete|options|head)\s+['"]([^'"]+)['"]`,
	)
	reRailsNamespace = regexp.MustCompile(
		`(?m)^\s*namespace\s+:([a-z_]+)\s+do`,
	)
	reRailsConcern = regexp.MustCompile(
		`(?m)^\s*concern\s+:([a-z_]+)\s+do`,
	)
	reRailsCallback = regexp.MustCompile(
		`(?m)^\s*(before_save|after_save|before_create|after_create|before_update|after_update|before_destroy|after_destroy|after_commit|before_validation|after_validation)\s+:([a-z_]+)`,
	)
	reRailsAssociation = regexp.MustCompile(
		`(?m)^\s*(has_many|belongs_to|has_one|has_and_belongs_to_many)\s+:([a-z_]+)`,
	)
	reRailsBeforeAction = regexp.MustCompile(
		`(?m)^\s*(before_action|after_action|around_action)\s+:([a-z_]+)`,
	)
	reRailsScope = regexp.MustCompile(
		`(?m)^\s*scope\s+:([a-z_]+)`,
	)
	reRailsActiveJobPerform = regexp.MustCompile(
		`(?m)^\s*def\s+(perform)\s*\(`,
	)
	reRailsMailerMethod = regexp.MustCompile(
		`(?m)^\s*def\s+([a-z_]+)\s*(?:\()?`,
	)
	reRailsChannelStream = regexp.MustCompile(
		`(?m)\bstream_from\s+['"]([^'"]+)['"]`,
	)
)

// railsCRUDRoutes defines the 8 standard CRUD routes for resources.
var railsCRUDRoutes = []struct{ method, suffix string }{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/new"},
	{"GET", "/:id"},
	{"GET", "/:id/edit"},
	{"PATCH", "/:id"},
	{"PUT", "/:id"},
	{"DELETE", "/:id"},
}

func (e *railsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.rails_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "rails"),
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

	// 1. resources :name -> expand to 8 CRUD routes
	for _, m := range reRailsResources.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ln := lineOf(src, m[0])
		for _, cr := range railsCRUDRoutes {
			path := "/" + name + cr.suffix
			routeName := cr.method + " " + path
			ent := makeEntity(routeName, "SCOPE.Operation", "endpoint", file.Path, file.Language, ln)
			setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_RESOURCES",
				"http_method", cr.method, "route_path", path, "resource", name)
			add(ent)
		}
	}

	// 2. Explicit HTTP routes: get/post/put/patch/delete
	for _, m := range reRailsHTTPRoute.FindAllStringSubmatchIndex(src, -1) {
		method := strings.ToUpper(src[m[2]:m[3]])
		path := src[m[4]:m[5]]
		name := method + " " + path
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_ROUTE",
			"http_method", method, "route_path", path)
		add(ent)
	}

	// 3. namespace :name -> SCOPE.Component
	for _, m := range reRailsNamespace.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_NAMESPACE",
			"namespace", name)
		add(ent)
	}

	// 4. concern :name -> SCOPE.Pattern
	for _, m := range reRailsConcern.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_CONCERN",
			"concern_name", name)
		add(ent)
	}

	// 5. ActiveRecord callbacks -> SCOPE.Operation/function
	for _, m := range reRailsCallback.FindAllStringSubmatchIndex(src, -1) {
		cbType := src[m[2]:m[3]]
		cbName := src[m[4]:m[5]]
		name := fmt.Sprintf("%s:%s", cbType, cbName)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_CALLBACK",
			"callback_type", cbType, "callback_method", cbName)
		add(ent)
	}

	// 6. ActiveRecord associations -> SCOPE.Component
	for _, m := range reRailsAssociation.FindAllStringSubmatchIndex(src, -1) {
		assocType := src[m[2]:m[3]]
		assocName := src[m[4]:m[5]]
		name := fmt.Sprintf("%s:%s", assocType, assocName)
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_ASSOCIATION",
			"association_type", assocType, "association_name", assocName)
		add(ent)
	}

	// 7. before_action/after_action filters -> SCOPE.Pattern
	for _, m := range reRailsBeforeAction.FindAllStringSubmatchIndex(src, -1) {
		filterType := src[m[2]:m[3]]
		filterMethod := src[m[4]:m[5]]
		name := fmt.Sprintf("%s:%s", filterType, filterMethod)
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_FILTER",
			"filter_type", filterType, "filter_method", filterMethod)
		add(ent)
	}

	// 8. Model scopes -> SCOPE.Operation/function
	for _, m := range reRailsScope.FindAllStringSubmatchIndex(src, -1) {
		name := "scope:" + src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_SCOPE",
			"scope_name", src[m[2]:m[3]])
		add(ent)
	}

	// 9. ActiveJob perform -> SCOPE.Operation/function
	for _, m := range reRailsActiveJobPerform.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("perform", "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_JOB_PERFORM")
		add(ent)
	}

	// 10. stream_from -> SCOPE.Pattern
	for _, m := range reRailsChannelStream.FindAllStringSubmatchIndex(src, -1) {
		channel := src[m[2]:m[3]]
		name := "stream_from:" + channel
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rails", "provenance", "INFERRED_FROM_RAILS_CHANNEL",
			"channel", channel)
		add(ent)
	}

	_ = reRailsMailerMethod // used implicitly via SCOPE.Operation patterns

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
