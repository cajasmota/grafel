package golang

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_go_gorm", &gormExtractor{})
}

type gormExtractor struct{}

func (e *gormExtractor) Language() string { return "custom_go_gorm" }

var (
	reGORMOpen = regexp.MustCompile(
		`(?m)(\w+)\s*,\s*\w+\s*:?=\s*gorm\.Open\s*\(`,
	)
	reGORMAutoMigrate = regexp.MustCompile(
		`(?m)\.AutoMigrate\s*\(\s*&?([^)]+)\)`,
	)
	reGORMAutoMigrateType = regexp.MustCompile(
		`&?(\w+)\s*\{`,
	)
	reGORMModel = regexp.MustCompile(
		`(?m)type\s+(\w+)\s+struct\s*\{[^}]*\bgorm\.Model\b`,
	)
	reGORMQuery = regexp.MustCompile(
		`(?m)\.(?:Model|Table)\s*\(\s*(?:&?(\w+)\s*\{|"([^"]+)"\s*)`,
	)
	reGORMCreate = regexp.MustCompile(
		`(?m)db\.Create\s*\(\s*&?(\w+)`,
	)
	reGORMFind = regexp.MustCompile(
		`(?m)db\.(?:Find|First|Last|Take)\s*\(\s*&?(\w+)`,
	)
	reGORMScope = regexp.MustCompile(
		`(?m)\.Scopes\s*\(\s*(\w+)`,
	)
)

func (e *gormExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.gorm_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "gorm"),
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

	// 1. gorm.Open() -> SCOPE.Service
	for _, m := range reGORMOpen.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		ent := makeEntity(varName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_OPEN")
		add(ent)
	}

	// 2. AutoMigrate models -> SCOPE.Schema
	for _, m := range reGORMAutoMigrate.FindAllStringSubmatchIndex(src, -1) {
		argsStr := src[m[2]:m[3]]
		for _, tm := range reGORMAutoMigrateType.FindAllStringSubmatch(argsStr, -1) {
			modelName := tm[1]
			ent := makeEntity(modelName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_AUTOMIGRATE")
			add(ent)
		}
	}

	// 3. struct with gorm.Model embed -> SCOPE.Schema
	for _, m := range reGORMModel.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		ent := makeEntity(modelName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_MODEL")
		add(ent)
	}

	// 4. db.Model(&T{}) / db.Table("name") queries -> SCOPE.Operation
	for _, m := range reGORMQuery.FindAllStringSubmatchIndex(src, -1) {
		var modelName string
		if m[2] >= 0 {
			modelName = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			modelName = src[m[4]:m[5]]
		}
		if modelName == "" {
			continue
		}
		ent := makeEntity("query:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_QUERY",
			"model_name", modelName)
		add(ent)
	}

	// 5. db.Create -> SCOPE.Operation
	for _, m := range reGORMCreate.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		ent := makeEntity("create:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_QUERY",
			"query_type", "create", "model_name", modelName)
		add(ent)
	}

	// 6. db.Find/First/Last/Take -> SCOPE.Operation
	for _, m := range reGORMFind.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		ent := makeEntity("find:"+modelName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_QUERY",
			"query_type", "find", "model_name", modelName)
		add(ent)
	}

	// 7. .Scopes(fn) -> SCOPE.Pattern
	for _, m := range reGORMScope.FindAllStringSubmatchIndex(src, -1) {
		scopeFn := src[m[2]:m[3]]
		ent := makeEntity("scope:"+scopeFn, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "gorm", "provenance", "INFERRED_FROM_GORM_SCOPE",
			"scope_func", scopeFn)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
