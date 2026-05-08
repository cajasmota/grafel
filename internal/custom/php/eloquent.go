package php

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
	extractor.Register("custom_php_eloquent", &eloquentExtractor{})
}

type eloquentExtractor struct{}

func (e *eloquentExtractor) Language() string { return "custom_php_eloquent" }

var (
	reEloquentModel = regexp.MustCompile(
		`(?m)class\s+(\w+)\s+extends\s+(?:Model|Authenticatable|Pivot)\b`,
	)
	reEloquentRelationship = regexp.MustCompile(
		`(?s)public\s+function\s+(\w+)\s*\(\s*\)\s*(?::\s*\w+\s*)?\{[^}]*?\b(hasOne|hasMany|belongsTo|belongsToMany|morphTo|morphMany|morphOne|hasManyThrough|hasOneThrough)\s*\(`,
	)
	reEloquentScope = regexp.MustCompile(
		`(?m)public\s+function\s+scope([A-Z][A-Za-z0-9_]*)\s*\(`,
	)
	reEloquentObserver = regexp.MustCompile(
		`(?m)public\s+function\s+(creating|created|updating|updated|deleting|deleted|saving|saved|restoring|restored|forceDeleted)\s*\(`,
	)
	reEloquentAccessor = regexp.MustCompile(
		`(?m)public\s+function\s+get([A-Z][A-Za-z0-9_]*)Attribute\s*\(`,
	)
	reEloquentMutator = regexp.MustCompile(
		`(?m)public\s+function\s+set([A-Z][A-Za-z0-9_]*)Attribute\s*\(`,
	)
	reEloquentDBTable = regexp.MustCompile(
		`(?m)DB::table\s*\(\s*['"]([^'"]+)['"]`,
	)
)

func (e *eloquentExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.eloquent_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "eloquent"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "php" {
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

	// 1. Eloquent Model classes -> SCOPE.Schema
	for _, m := range reEloquentModel.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_MODEL")
		add(ent)
	}

	// 2. Relationship methods -> SCOPE.Component
	for _, m := range reEloquentRelationship.FindAllStringSubmatchIndex(src, -1) {
		methodName := src[m[2]:m[3]]
		relType := src[m[4]:m[5]]
		ent := makeEntity(methodName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_RELATIONSHIP",
			"relationship_type", relType)
		add(ent)
	}

	// 3. Query scopes -> SCOPE.Operation/function
	for _, m := range reEloquentScope.FindAllStringSubmatchIndex(src, -1) {
		name := "scope" + src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_SCOPE")
		add(ent)
	}

	// 4. Observer hooks -> SCOPE.Pattern
	for _, m := range reEloquentObserver.FindAllStringSubmatchIndex(src, -1) {
		hook := src[m[2]:m[3]]
		ent := makeEntity(hook, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_OBSERVER",
			"hook_type", hook)
		add(ent)
	}

	// 5. Accessors -> SCOPE.Operation/function
	for _, m := range reEloquentAccessor.FindAllStringSubmatchIndex(src, -1) {
		attrName := src[m[2]:m[3]]
		ent := makeEntity("get"+attrName+"Attribute", "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_ACCESSOR",
			"attribute_name", attrName)
		add(ent)
	}

	// 6. Mutators -> SCOPE.Operation/function
	for _, m := range reEloquentMutator.FindAllStringSubmatchIndex(src, -1) {
		attrName := src[m[2]:m[3]]
		ent := makeEntity("set"+attrName+"Attribute", "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_ACCESSOR",
			"attribute_name", attrName, "accessor_kind", "mutator")
		add(ent)
	}

	// 7. DB::table() query scopes -> SCOPE.Operation/query
	for _, m := range reEloquentDBTable.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("query:"+tableName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "eloquent", "provenance", "INFERRED_FROM_ELOQUENT_QUERY",
			"table_name", tableName)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
