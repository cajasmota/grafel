package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// kysely.go — the BASE Kysely migration-evolution extractor (#5599). It mirrors
// knex.go: it emits the migration ENTRY points (up()/down()) and the per-op
// SCOPE.Evolution entities (create_table / alter_table / drop_table) that the
// #3628 engine pass (migration_schema_ops.go, framework=kysely) converges to
// MODIFIES_TABLE. The companion kysely_migrations.go emits the relational view
// (Schema / Component / foreign_key / relation).
//
// Kysely declares schema imperatively in migration modules via the schema
// builder:
//
//	export async function up(db: Kysely<any>): Promise<void> {
//	  await db.schema.createTable('person')
//	    .addColumn('id', 'integer', (cb) => cb.primaryKey())
//	    .addColumn('owner_id', 'integer', (cb) => cb.references('user.id'))
//	    .addForeignKeyConstraint('fk_owner', ['owner_id'], 'user', ['id'])
//	    .execute()
//	}
//
// Unlike knex (`t.string('name')` column builders on a closure arg) the table
// name is the FIRST string arg to createTable/alterTable/dropTable on the
// `.schema.` receiver, and columns are `.addColumn('name', 'type', ...)`.
func init() {
	extreg.Register("custom_js_kysely", &kyselyExtractor{})
}

type kyselyExtractor struct{}

func (e *kyselyExtractor) Language() string { return "custom_js_kysely" }

var (
	// db.schema.createTable('person') / .alterTable('person') / .dropTable('person').
	// Group 1 = method, group 2 = table-name literal. The `.schema.` receiver and
	// the table-named methods keep this from firing on unrelated createTable calls.
	reKyselySchemaOp = regexp.MustCompile(
		`\.\s*(createTable|alterTable|dropTable)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	// .addColumn('name', 'type', ...) — Kysely column builder. Group 1 = column.
	reKyselyAddColumn = regexp.MustCompile(
		`\.\s*addColumn\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// .dropColumn('name') — column drop op.
	reKyselyDropColumn = regexp.MustCompile(
		`\.\s*dropColumn\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// Index ops: .createIndex('idx') / .dropIndex('idx').
	reKyselyIndexOp = regexp.MustCompile(
		`\.\s*(createIndex|dropIndex)\s*\(`,
	)
	// up()/down() migration entry points (named exports or object props).
	//
	//	export async function up(db) {...}  /  export const up = async (db) => {...}
	//	export function down(db) {...}       /  up: async (db) => {...}
	reKyselyMigrationFn = regexp.MustCompile(
		`(?:export\s+(?:async\s+)?function\s+(up|down)\b|export\s+const\s+(up|down)\s*=|\b(up|down)\s*:\s*(?:async\s*)?\()`,
	)
)

func kyselySchemaOpSubtype(method string) string {
	switch method {
	case "createTable":
		return "create_table"
	case "dropTable":
		return "drop_table"
	case "alterTable":
		return "alter_table"
	default:
		return "schema_change"
	}
}

func (e *kyselyExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.kysely_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "kysely"),
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

	// Gate to plausible Kysely migration sources: the schema builder must be
	// reached via a `.schema.` receiver carrying a table-named op. This keeps a
	// stray createTable/alterTable call in unrelated code from being read as a
	// migration op.
	if !looksLikeKyselyMigration(file.Path, src) {
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

	// Migration up()/down() entry points (migration_parsing).
	for _, m := range reKyselyMigrationFn.FindAllStringSubmatchIndex(src, -1) {
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
		setProps(&ent, "framework", "kysely", "direction", dir,
			"provenance", "INFERRED_FROM_KYSELY_MIGRATION_FN")
		addEntity(ent)
	}

	// Schema-builder table ops (migration_schema_ops → MODIFIES_TABLE via #3628).
	for _, m := range reKyselySchemaOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		opSubtype := kyselySchemaOpSubtype(method)
		ent := makeEntity(opSubtype+":"+table, "SCOPE.Evolution", opSubtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kysely", "migration_op", method, "table", table,
			"provenance", "INFERRED_FROM_KYSELY_SCHEMA_OP")
		addEntity(ent)
	}

	// Column add/drop builders (attribute columns mutated by a migration).
	for _, m := range reKyselyAddColumn.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity("add_column:"+colName, "SCOPE.Evolution", "add_column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kysely", "column", colName,
			"provenance", "INFERRED_FROM_KYSELY_ADD_COLUMN")
		addEntity(ent)
	}
	for _, m := range reKyselyDropColumn.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity("drop_column:"+colName, "SCOPE.Evolution", "drop_column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kysely", "column", colName,
			"provenance", "INFERRED_FROM_KYSELY_DROP_COLUMN")
		addEntity(ent)
	}

	// Index ops.
	for _, m := range reKyselyIndexOp.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		subtype := "create_index"
		if strings.HasPrefix(method, "drop") {
			subtype = "drop_index"
		}
		ent := makeEntity(subtype+":"+method, "SCOPE.Evolution", subtype, file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kysely", "migration_op", method,
			"provenance", "INFERRED_FROM_KYSELY_INDEX_OP")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
