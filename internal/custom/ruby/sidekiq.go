package ruby

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_sidekiq", &sidekiqExtractor{})
}

type sidekiqExtractor struct{}

func (e *sidekiqExtractor) Language() string { return "custom_ruby_sidekiq" }

var (
	reSkClassDecl = regexp.MustCompile(
		`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)`,
	)
	reSkWorkerInclude = regexp.MustCompile(
		`(?m)^\s*include\s+Sidekiq::(Worker|Job)\b`,
	)
	reSkPerform = regexp.MustCompile(
		`(?m)^\s*def\s+perform\s*[\(\n]`,
	)
	reSkDispatch = regexp.MustCompile(
		`(?m)(\w+)\.(perform_async|perform_in|perform_at)\s*\(`,
	)
	reSkConfig = regexp.MustCompile(
		`(?m)\bSidekiq\.(configure_server|configure_client)\s+do`,
	)
	reSkMiddleware = regexp.MustCompile(
		`(?m)\bSidekiq(?:::(?:ServerMiddleware|ClientMiddleware)|\.configure_(?:server|client)[^}]*?(?:server|client)_middleware)\b`,
	)
)

func (e *sidekiqExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.sidekiq_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "sidekiq"),
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

	// Detect which classes include Sidekiq::Worker/Job
	workerIncludes := reSkWorkerInclude.FindAllStringIndex(src, -1)
	workerClassNames := make(map[string]bool)
	for _, wi := range workerIncludes {
		// Find the class declaration before this include
		prefix := src[:wi[0]]
		classMatches := reSkClassDecl.FindAllStringSubmatch(prefix, -1)
		if len(classMatches) > 0 {
			workerClassNames[classMatches[len(classMatches)-1][1]] = true
		}
	}

	// 1. Worker class declarations -> SCOPE.Service
	for _, m := range reSkClassDecl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if !workerClassNames[name] && len(workerIncludes) == 0 {
			continue
		}
		// emit if it's a known worker or if file has any worker include
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sidekiq", "provenance", "INFERRED_FROM_SIDEKIQ_WORKER")
		add(ent)
	}

	// 2. def perform -> SCOPE.Operation/function
	for _, m := range reSkPerform.FindAllStringIndex(src, -1) {
		ent := makeEntity("perform", "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sidekiq", "provenance", "INFERRED_FROM_SIDEKIQ_PERFORM")
		add(ent)
	}

	// 3. perform_async/in/at dispatch calls -> SCOPE.Operation/function
	for _, m := range reSkDispatch.FindAllStringSubmatchIndex(src, -1) {
		workerClass := src[m[2]:m[3]]
		dispatchMethod := src[m[4]:m[5]]
		name := workerClass + "." + dispatchMethod
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sidekiq", "provenance", "INFERRED_FROM_SIDEKIQ_DISPATCH",
			"worker_class", workerClass, "dispatch_method", dispatchMethod)
		add(ent)
	}

	// 4. Sidekiq.configure_server/client -> SCOPE.Pattern
	for _, m := range reSkConfig.FindAllStringSubmatchIndex(src, -1) {
		configType := src[m[2]:m[3]]
		name := "sidekiq." + configType
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sidekiq", "provenance", "INFERRED_FROM_SIDEKIQ_CONFIG",
			"config_type", configType)
		add(ent)
	}

	// 5. Sidekiq middleware -> SCOPE.Pattern
	mwCount := 0
	for _, m := range reSkMiddleware.FindAllStringIndex(src, -1) {
		mwCount++
		name := "sidekiq_middleware_" + string(rune('0'+mwCount))
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "sidekiq", "provenance", "INFERRED_FROM_SIDEKIQ_MIDDLEWARE")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
