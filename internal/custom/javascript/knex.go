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
	extreg.Register("custom_js_knex", &knexExtractor{})
}

type knexExtractor struct{}

func (e *knexExtractor) Language() string { return "custom_js_knex" }

var (
	// knex.schema.createTable('users', ...) and friends. The leading receiver
	// is commonly `knex.schema`, `this.schema`, or a destructured `schema`.
	// Group 1 = method, group 2 = table name literal.
	reKnexSchemaOp = regexp.MustCompile(
		`\.\s*(createTable|createTableIfNotExists|dropTable|dropTableIfExists|renameTable|alterTable|table)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	// Column builder calls inside a table closure: t.string('name'), t.integer('age').
	reKnexColumnBuilder = regexp.MustCompile(
		`\.\s*(increments|bigIncrements|integer|bigInteger|text|string|float|decimal|boolean|date|datetime|timestamp|time|binary|json|jsonb|uuid|enu|enum|uuid)\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// Index ops: t.index([...]), t.unique('col'), t.dropIndex(...).
	reKnexIndexOp = regexp.MustCompile(
		`\.\s*(index|unique|dropIndex|dropUnique)\s*\(`,
	)
	// exports.up / export async function up / export const up — migration entry points.
	reKnexMigrationFn = regexp.MustCompile(
		`(?:exports\.(up|down)\s*=|export\s+(?:async\s+)?function\s+(up|down)\b|export\s+const\s+(up|down)\s*=)`,
	)
)

func knexSchemaOpSubtype(method string) string {
	switch method {
	case "createTable", "createTableIfNotExists":
		return "create_table"
	case "dropTable", "dropTableIfExists":
		return "drop_table"
	case "renameTable":
		return "rename_table"
	case "alterTable", "table":
		return "alter_table"
	default:
		return "schema_change"
	}
}

func (e *knexExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.knex_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "knex"),
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

	// Migration up()/down() entry points.
	for _, m := range reKnexMigrationFn.FindAllStringSubmatchIndex(src, -1) {
		var dir string
		for i := 2; i+1 < len(m); i += 2 {
			if m[i] >= 0 {
				dir = src[m[i]:m[i+1]]
				break
			}
		}
		if dir == "" {
			continue
		}
		ent := makeEntity("migration:"+dir, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "knex", "direction", dir,
			"provenance", "INFERRED_FROM_KNEX_MIGRATION_FN")
		addEntity(ent)
	}

	// Schema-builder ops.
	for _, m := range reKnexSchemaOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		opSubtype := knexSchemaOpSubtype(method)
		ent := makeEntity(opSubtype+":"+table, "SCOPE.Evolution", opSubtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "knex", "migration_op", method, "table", table,
			"provenance", "INFERRED_FROM_KNEX_SCHEMA_OP")
		addEntity(ent)
	}

	// Column builders (attribute columns added by a migration).
	for _, m := range reKnexColumnBuilder.FindAllStringSubmatchIndex(src, -1) {
		colType := src[m[2]:m[3]]
		colName := src[m[4]:m[5]]
		ent := makeEntity("add_column:"+colName, "SCOPE.Evolution", "add_column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "knex", "column", colName, "column_type", colType,
			"provenance", "INFERRED_FROM_KNEX_COLUMN_BUILDER")
		addEntity(ent)
	}

	// Index ops.
	for _, m := range reKnexIndexOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		subtype := "create_index"
		if strings.HasPrefix(method, "drop") {
			subtype = "drop_index"
		}
		ent := makeEntity(subtype+":"+method, "SCOPE.Evolution", subtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "knex", "migration_op", method,
			"provenance", "INFERRED_FROM_KNEX_INDEX_OP")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
