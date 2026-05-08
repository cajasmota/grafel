package csharp

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
	extractor.Register("custom_csharp_ef_core", &efCoreExtractor{})
}

type efCoreExtractor struct{}

func (e *efCoreExtractor) Language() string { return "custom_csharp_ef_core" }

var (
	reEFContext = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+:\s+DbContext\b`,
	)
	reEFDbSet = regexp.MustCompile(
		`DbSet\s*<\s*(\w+)\s*>`,
	)
	reEFFluentHasOne = regexp.MustCompile(
		`\.Has(?:One|Many)\s*\(\s*(?:x\s*=>\s*x\.)?(\w+)`,
	)
	reEFLINQWhere = regexp.MustCompile(
		`(?m)\.Where\s*\([^)]+\)`,
	)
	reEFLINQSelect = regexp.MustCompile(
		`(?m)\.Select\s*\([^)]+\)`,
	)
	reEFLINQInclude = regexp.MustCompile(
		`\.Include\s*\(\s*(?:x\s*=>\s*x\.)?(\w+)`,
	)
	reEFMigration = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+:\s+Migration\b`,
	)
	reEFProvider = regexp.MustCompile(
		`\.(UseSqlServer|UseNpgsql|UseSqlite|UseMySql|UseOracle|UseInMemoryDatabase)\s*\(`,
	)
	reEFModelBuilder = regexp.MustCompile(
		`modelBuilder\.Entity\s*<\s*(\w+)\s*>`,
	)
)

func (e *efCoreExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.ef_core_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ef_core"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
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

	// 1. DbContext subclasses -> SCOPE.Service
	for _, m := range reEFContext.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_CONTEXT")
		add(ent)
	}

	// 2. DbSet<T> -> SCOPE.Component
	for _, m := range reEFDbSet.FindAllStringSubmatchIndex(src, -1) {
		entityType := src[m[2]:m[3]]
		ent := makeEntity(entityType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_ENTITY")
		add(ent)
	}

	// 3. Fluent API HasOne/HasMany -> SCOPE.Component
	for _, m := range reEFFluentHasOne.FindAllStringSubmatchIndex(src, -1) {
		navProp := src[m[2]:m[3]]
		ent := makeEntity("relation:"+navProp, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_RELATIONSHIP",
			"navigation_property", navProp)
		add(ent)
	}

	// 4. modelBuilder.Entity<T>() -> SCOPE.Component (configuration target)
	for _, m := range reEFModelBuilder.FindAllStringSubmatchIndex(src, -1) {
		entityType := src[m[2]:m[3]]
		ent := makeEntity(entityType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_ENTITY",
			"configured_by", "model_builder")
		add(ent)
	}

	// 5. LINQ .Where() queries -> SCOPE.Operation/query
	whereCount := 0
	for _, m := range reEFLINQWhere.FindAllStringIndex(src, -1) {
		whereCount++
		name := "where_query_" + string(rune('0'+whereCount%10))
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_QUERY",
			"query_type", "where")
		add(ent)
	}

	// 6. LINQ .Include() -> SCOPE.Operation/query
	for _, m := range reEFLINQInclude.FindAllStringSubmatchIndex(src, -1) {
		navProp := src[m[2]:m[3]]
		ent := makeEntity("include:"+navProp, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_QUERY",
			"query_type", "include", "navigation_property", navProp)
		add(ent)
	}

	// 7. Migration class declarations -> SCOPE.Component
	for _, m := range reEFMigration.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_MIGRATION")
		add(ent)
	}

	// 8. Database provider -> SCOPE.Service/provider
	for _, m := range reEFProvider.FindAllStringSubmatchIndex(src, -1) {
		providerName := src[m[2]:m[3]]
		ent := makeEntity(providerName, "SCOPE.Service", "provider", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "ef_core", "provenance", "INFERRED_FROM_EF_PROVIDER",
			"provider_name", providerName)
		add(ent)
	}

	_ = reEFLINQSelect // used for future relationship tracking

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
