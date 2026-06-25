package javascript

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// kyselyMigrationsExtractor parses Kysely migration sources for the schema-builder
// DSL and recovers the relational structure that the query builder expresses
// imperatively (#5599). It mirrors knex_migrations.go:
//
//   - db.schema.createTable('person', ...)                → table schema (SCOPE.Schema)
//   - .addColumn('name', 'varchar', ...)                  → columns      (SCOPE.Component/column)
//   - .addForeignKeyConstraint('fk', ['owner_id'],
//     'user', ['id'])                                  → foreign key  (SCOPE.Component/foreign_key)
//   - .addColumn('owner_id','integer',c=>c.references('user.id'))
//     → column-level foreign key  (SCOPE.Component/foreign_key)
//   - each FK additionally yields a relationship/association edge          (SCOPE.Pattern/relation)
//
// Kysely is a type-safe SQL query builder (not an ORM): its `Database` interface
// is a compile-time TS type with no runtime model layer, so the *migration* DDL
// is the only place the schema and its foreign-key relationships are declared.
// This extractor therefore powers the schema_extraction / foreign_key_extraction
// / relationship_extraction capabilities for lang.jsts.orm.kysely.
//
// The base kysely.go extractor already emits migration-evolution ops
// (create_table/alter_table/drop_table, SCOPE.Evolution) for migration_parsing
// and migration_schema_ops; this extractor is additive and emits the relational
// view. The two are deduped downstream by (Kind, Name, Subtype).
func init() {
	extreg.Register("custom_js_kysely_migrations", &kyselyMigrationsExtractor{})
}

type kyselyMigrationsExtractor struct{}

func (e *kyselyMigrationsExtractor) Language() string { return "custom_js_kysely_migrations" }

var (
	// db.schema.createTable('person') / .alterTable('person').
	// Group 1 = method, group 2 = table name literal.
	reKyselyMigTable = regexp.MustCompile(
		`\.\s*(createTable|alterTable)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	// .addColumn('name', 'type', ...) column builder. Group 1 = column name.
	reKyselyMigColumn = regexp.MustCompile(
		`\.\s*addColumn\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// Explicit composite/named FK:
	//   .addForeignKeyConstraint('fk_owner', ['owner_id'], 'user', ['id'])
	// Group 1 = constraint name, group 2 = local cols list, group 3 = ref table,
	// group 4 = ref cols list.
	reKyselyMigFKConstraint = regexp.MustCompile(
		`\.\s*addForeignKeyConstraint\s*\(\s*['"][^'"]*['"]\s*,\s*\[([^\]]*)\]\s*,\s*['"]([A-Za-z0-9_.]+)['"]\s*,\s*\[([^\]]*)\]`,
	)
	// Column-level FK: a `.references('user.id')` call inside an addColumn
	// callback. Group 1 = "table.column" (or bare column) literal.
	reKyselyMigColRef = regexp.MustCompile(
		`\.\s*references\s*\(\s*['"]([A-Za-z0-9_.]+)['"]\s*\)`,
	)
	// First string literal inside a bracket list, e.g. ['owner_id'] → owner_id.
	reKyselyFirstListLiteral = regexp.MustCompile(`['"]([A-Za-z0-9_.]+)['"]`)
)

func (e *kyselyMigrationsExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.kysely_migrations_extractor.extract",
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

	// --- schema_extraction: tables -------------------------------------------
	for _, m := range reKyselyMigTable.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		ent := makeEntity(table, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kysely", "table", table, "builder_method", method,
			"provenance", "INFERRED_FROM_KYSELY_MIGRATION_TABLE")
		addEntity(ent)
	}

	// --- schema_extraction: columns ------------------------------------------
	for _, m := range reKyselyMigColumn.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		ent := makeEntity(colName, "SCOPE.Component", "column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "kysely", "column", colName,
			"provenance", "INFERRED_FROM_KYSELY_MIGRATION_COLUMN")
		addEntity(ent)
	}

	// --- foreign_key / association / relationship ----------------------------
	// (1) Explicit .addForeignKeyConstraint('fk', ['localCol'], 'refTable', ['refCol']).
	for _, m := range reKyselyMigFKConstraint.FindAllStringSubmatchIndex(src, -1) {
		localCol := firstListLiteral(src[m[2]:m[3]])
		refTable := src[m[4]:m[5]]
		refCol := firstListLiteral(src[m[6]:m[7]])
		emitKyselyFK(addEntity, file, lineOf(src, m[0]), localCol, refTable, refCol)
	}

	// (2) Column-level .references('refTable.refCol') inside an addColumn callback.
	// The local column is the nearest preceding .addColumn('col', ...).
	colAnchors := kyselyColumnAnchors(src)
	for _, m := range reKyselyMigColRef.FindAllStringSubmatchIndex(src, -1) {
		refLiteral := src[m[2]:m[3]]
		refTable, refCol := "", refLiteral
		if dot := strings.LastIndex(refLiteral, "."); dot >= 0 {
			refTable = refLiteral[:dot]
			refCol = refLiteral[dot+1:]
		}
		localCol := nearestAnchorBefore(colAnchors, m[0])
		emitKyselyFK(addEntity, file, lineOf(src, m[0]), localCol, refTable, refCol)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// emitKyselyFK emits the foreign_key + relation/association entities for a
// resolved (localCol, refTable, refCol) triple. refTable may be empty when a
// column-level .references() used a bare column with no table prefix.
func emitKyselyFK(addEntity func(types.EntityRecord), file extreg.FileInput, line int, localCol, refTable, refCol string) {
	ref := refCol
	if refTable != "" {
		ref = refTable + "." + refCol
	}

	// foreign_key entity (foreign_key_extraction).
	fkName := "fk:" + ref
	if localCol != "" {
		fkName = fmt.Sprintf("fk:%s->%s", localCol, ref)
	}
	fk := makeEntity(fkName, "SCOPE.Component", "foreign_key", file.Path, file.Language, line)
	fkProps := []string{
		"framework", "kysely",
		"ref_column", refCol,
		"provenance", "INFERRED_FROM_KYSELY_MIGRATION_FK",
	}
	if refTable != "" {
		fkProps = append(fkProps, "ref_table", refTable)
	}
	if localCol != "" {
		fkProps = append(fkProps, "local_column", localCol)
	}
	setProps(&fk, fkProps...)
	addEntity(fk)

	// relationship / association entity (relationship_extraction + association).
	relName := "relation:" + ref
	if refTable != "" {
		relName = "relation:->" + refTable
		if localCol != "" {
			relName = fmt.Sprintf("relation:%s->%s", localCol, refTable)
		}
	}
	rel := makeEntity(relName, "SCOPE.Pattern", "relation", file.Path, file.Language, line)
	relProps := []string{
		"framework", "kysely",
		"relation_kind", "belongs_to",
		"provenance", "INFERRED_FROM_KYSELY_MIGRATION_FK",
	}
	if refTable != "" {
		relProps = append(relProps, "ref_table", refTable)
	}
	if localCol != "" {
		relProps = append(relProps, "local_column", localCol)
	}
	setProps(&rel, relProps...)
	addEntity(rel)
}

// firstListLiteral returns the first string literal inside a Kysely column list
// such as `['owner_id']` (already sliced to the inner text). Returns "" when no
// literal is present (dynamic / spread list — honest-partial).
func firstListLiteral(inner string) string {
	if m := reKyselyFirstListLiteral.FindStringSubmatch(inner); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// kyselyColumnAnchors collects all .addColumn('col', ...) declarations so a
// column-level .references() can be attributed to the column it sits on.
func kyselyColumnAnchors(src string) []anchor {
	var out []anchor
	for _, m := range reKyselyMigColumn.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, anchor{col: src[m[2]:m[3]], offset: m[0]})
	}
	// Already in source order (FindAll returns left-to-right); keep stable.
	return out
}

// looksLikeKyselyMigration returns true when the file path or content indicates
// a Kysely migration rather than arbitrary source that happens to call a method
// named createTable()/addColumn(). Shared by the base and migrations extractors.
func looksLikeKyselyMigration(path, src string) bool {
	p := filepath.ToSlash(path)
	// A Kysely schema chain is the strongest signal: `.schema.` reached with a
	// table-named builder op.
	hasSchemaBuilder := strings.Contains(src, ".schema.") &&
		(reKyselyMigTable.MatchString(src) || strings.Contains(src, "dropTable"))
	inMigrationsDir := strings.Contains(p, "/migrations/") || strings.HasPrefix(p, "migrations/")
	if inMigrationsDir && hasSchemaBuilder {
		return true
	}
	// Import-of-Kysely + schema builder catches inline / non-conventional paths.
	importsKysely := strings.Contains(src, "from 'kysely'") ||
		strings.Contains(src, "from \"kysely\"") ||
		strings.Contains(src, "Kysely<")
	if hasSchemaBuilder && (importsKysely || reKyselyMigFKConstraint.MatchString(src)) {
		return true
	}
	return false
}
