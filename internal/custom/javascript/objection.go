package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_objection", &objectionExtractor{})
}

type objectionExtractor struct{}

func (e *objectionExtractor) Language() string { return "custom_js_objection" }

var (
	// class User extends Model {} — Objection models subclass Model.
	reObjectionModel = regexp.MustCompile(
		`(?:export\s+)?(?:default\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+Model\b`,
	)
	// static get tableName() { return 'users' } — binds a model to its table.
	reObjectionTableName = regexp.MustCompile(
		`static\s+get\s+tableName\s*\(\s*\)\s*\{[^}]*return\s+['"]([A-Za-z0-9_.]+)['"]`,
	)
	// static get jsonSchema() — JSON-schema based field definitions.
	reObjectionJSONSchema = regexp.MustCompile(
		`static\s+get\s+jsonSchema\s*\(\s*\)`,
	)
	// static get relationMappings() — relation declarations.
	reObjectionRelationMappings = regexp.MustCompile(
		`static\s+(?:get\s+)?relationMappings\b`,
	)
	// Individual relation entries inside relationMappings: `friends: { relation: Model.HasManyRelation`.
	reObjectionRelation = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\{[^}]*relation\s*:\s*Model\s*\.\s*(BelongsToOneRelation|HasManyRelation|HasOneRelation|ManyToManyRelation|HasOneThroughRelation)`,
	)
)

func (e *objectionExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.objection_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "objection"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Model classes (extends Model). Capture an optional bound table name.
	tableName := ""
	if tm := reObjectionTableName.FindStringSubmatch(src); tm != nil {
		tableName = tm[1]
	}
	for _, m := range reObjectionModel.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "objection", "provenance", "INFERRED_FROM_OBJECTION_MODEL")
		if tableName != "" {
			setProps(&ent, "table", tableName)
		}
		addEntity(ent)
	}

	// jsonSchema field-set marker.
	for _, m := range reObjectionJSONSchema.FindAllStringIndex(src, -1) {
		ent := makeEntity("jsonSchema", "SCOPE.Component", "json_schema", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "objection", "provenance", "INFERRED_FROM_OBJECTION_JSON_SCHEMA")
		addEntity(ent)
	}

	// relationMappings.
	for _, m := range reObjectionRelationMappings.FindAllStringIndex(src, -1) {
		ent := makeEntity("relationMappings", "SCOPE.Pattern", "relation_mappings", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "objection", "provenance", "INFERRED_FROM_OBJECTION_RELATION_MAPPINGS")
		addEntity(ent)
	}

	// Individual relations.
	for _, m := range reObjectionRelation.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		relType := src[m[4]:m[5]]
		ent := makeEntity(relType+":"+field, "SCOPE.Component", "relation", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "objection", "relation_type", relType, "field_name", field,
			"provenance", "INFERRED_FROM_OBJECTION_RELATION")
		addEntity(ent)
	}

	// Migration schema-change ops. Objection runs migrations through Knex, so a
	// migration file uses the same schema-builder DSL (knex.schema.createTable).
	for _, m := range reKnexSchemaOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		opSubtype := knexSchemaOpSubtype(method)
		ent := makeEntity(opSubtype+":"+table, "SCOPE.Evolution", opSubtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "objection", "migration_op", method, "table", table,
			"provenance", "INFERRED_FROM_OBJECTION_MIGRATION_OP")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
