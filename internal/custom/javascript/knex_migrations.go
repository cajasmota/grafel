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

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// knexMigrationsExtractor parses Knex migration / knexfile sources for the
// schema-builder DSL and recovers the relational structure that the query-builder
// expresses imperatively:
//
//   - knex.schema.createTable('users', (t) => { ... })  → table schema (SCOPE.Schema)
//   - t.string('name') / t.integer('age') / ...         → columns      (SCOPE.Component/column)
//   - t.integer('author_id')
//     .references('id').inTable('users')             → foreign key  (SCOPE.Component/foreign_key)
//   - t.foreign('author_id')
//     .references('id').inTable('users')             → foreign key  (SCOPE.Component/foreign_key)
//   - each FK additionally yields a relationship/association edge        (SCOPE.Pattern/relation)
//
// Knex is a query/schema builder (not an ORM), so there is no decorator/model
// layer — the *migration* DDL is the only place the schema and its foreign-key
// relationships are declared. This extractor therefore powers the
// schema_extraction / foreign_key_extraction / association_extraction /
// relationship_extraction capabilities for lang.jsts.orm.knex.
//
// The base knex.go extractor already emits migration-evolution ops
// (create_table / add_column / index, SCOPE.Evolution) for migration_parsing;
// this extractor is additive and emits the *relational* view. The two are
// deduped downstream by (Kind, Name, Subtype), so the differing kinds coexist.
func init() {
	extreg.Register("custom_js_knex_migrations", &knexMigrationsExtractor{})
}

type knexMigrationsExtractor struct{}

func (e *knexMigrationsExtractor) Language() string { return "custom_js_knex_migrations" }

var (
	// knex.schema.createTable('users', ...) — also createTableIfNotExists/alterTable/table.
	// Group 1 = method, group 2 = table name literal.
	reKnexMigTable = regexp.MustCompile(
		`\.\s*(createTable|createTableIfNotExists|alterTable|table)\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	// Column builder calls inside a table closure: t.string('name'), t.integer('age'),
	// t.specificType('geom', 'point'). Group 1 = builder method, group 2 = column name.
	reKnexMigColumn = regexp.MustCompile(
		`\.\s*(increments|bigIncrements|integer|bigInteger|tinyint|smallint|mediumint|bigint|text|mediumtext|longtext|string|varchar|char|float|double|decimal|boolean|date|datetime|timestamp|time|binary|json|jsonb|uuid|enu|enum|specificType|geometry|point)\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// Explicit foreign-key declaration: t.foreign('author_id').
	// Group 1 = local column name.
	reKnexMigForeign = regexp.MustCompile(
		`\.\s*foreign\s*\(\s*['"]([A-Za-z0-9_]+)['"]`,
	)
	// FK target chain: .references('id').inTable('users')  (two-arg form) OR
	//                  .references('users.id')              (qualified single-arg form).
	// We match the .references(...) call and resolve .inTable(...) separately so
	// either spelling produces a foreign_key + relation edge.
	//
	// reKnexMigReferences group 1 = referenced column or "table.column" literal.
	reKnexMigReferences = regexp.MustCompile(
		`\.\s*references\s*\(\s*['"]([A-Za-z0-9_.]+)['"]\s*\)`,
	)
	// .inTable('users') — referenced table for the immediately-preceding .references().
	reKnexMigInTable = regexp.MustCompile(
		`\.\s*inTable\s*\(\s*['"]([A-Za-z0-9_.]+)['"]\s*\)`,
	)
)

func (e *knexMigrationsExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.knex_migrations_extractor.extract",
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

	// Gate to plausible Knex migration / knexfile sources to avoid emitting
	// schema entities for unrelated table.* method calls in arbitrary code.
	if !looksLikeKnexMigration(file.Path, src) {
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
	for _, m := range reKnexMigTable.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		ent := makeEntity(table, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "knex", "table", table, "builder_method", method,
			"provenance", "INFERRED_FROM_KNEX_MIGRATION_TABLE")
		addEntity(ent)
	}

	// --- schema_extraction: columns ------------------------------------------
	for _, m := range reKnexMigColumn.FindAllStringSubmatchIndex(src, -1) {
		colType := src[m[2]:m[3]]
		colName := src[m[4]:m[5]]
		ent := makeEntity(colName, "SCOPE.Component", "column", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "knex", "column", colName, "column_type", colType,
			"provenance", "INFERRED_FROM_KNEX_MIGRATION_COLUMN")
		addEntity(ent)
	}

	// --- foreign_key / association / relationship ----------------------------
	// A foreign key in Knex is expressed as a .references()[.inTable()] chain,
	// optionally anchored by .foreign('col') or .integer('col'). We collect the
	// local column from the nearest preceding .foreign()/column builder and the
	// referenced (table, column) from the .references()/.inTable() chain.
	localCols := columnAnchors(src)
	inTables := reKnexMigInTable.FindAllStringSubmatchIndex(src, -1)

	for _, m := range reKnexMigReferences.FindAllStringSubmatchIndex(src, -1) {
		refLiteral := src[m[2]:m[3]]
		refOffset := m[0]

		refTable, refCol := "", refLiteral
		if dot := strings.LastIndex(refLiteral, "."); dot >= 0 {
			// qualified "table.column" form
			refTable = refLiteral[:dot]
			refCol = refLiteral[dot+1:]
		} else if it := nextInTableAfter(inTables, src, m[1]); it != "" {
			// ".references('id').inTable('users')" form
			refTable = it
		}

		localCol := nearestAnchorBefore(localCols, refOffset)

		// foreign_key entity (foreign_key_extraction).
		fkName := "fk:" + refLiteral
		if localCol != "" {
			fkName = fmt.Sprintf("fk:%s->%s", localCol, refLiteral)
		}
		fk := makeEntity(fkName, "SCOPE.Component", "foreign_key", file.Path, file.Language, lineOf(src, refOffset))
		props := []string{
			"framework", "knex",
			"ref_column", refCol,
			"provenance", "INFERRED_FROM_KNEX_MIGRATION_FK",
		}
		if refTable != "" {
			props = append(props, "ref_table", refTable)
		}
		if localCol != "" {
			props = append(props, "local_column", localCol)
		}
		setProps(&fk, props...)
		addEntity(fk)

		// relationship / association entity (relationship_extraction + association_extraction).
		// Both capabilities are proven by the same FK-derived relation record:
		// the migration's foreign key *is* the association between the two tables.
		relName := "relation:" + refLiteral
		if refTable != "" {
			relName = "relation:->" + refTable
			if localCol != "" {
				relName = fmt.Sprintf("relation:%s->%s", localCol, refTable)
			}
		}
		rel := makeEntity(relName, "SCOPE.Pattern", "relation", file.Path, file.Language, lineOf(src, refOffset))
		relProps := []string{
			"framework", "knex",
			"relation_kind", "belongs_to",
			"provenance", "INFERRED_FROM_KNEX_MIGRATION_FK",
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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// looksLikeKnexMigration returns true when the file path or content indicates a
// Knex migration / knexfile rather than arbitrary source that happens to call a
// method named table()/foreign().
func looksLikeKnexMigration(path, src string) bool {
	p := filepath.ToSlash(path)
	base := strings.ToLower(filepath.Base(p))
	if base == "knexfile.js" || base == "knexfile.ts" {
		return true
	}
	inMigrationsDir := strings.Contains(p, "/migrations/") || strings.HasPrefix(p, "migrations/")
	// Knex migration files export up/down and drive a schema builder.
	hasSchemaBuilder := strings.Contains(src, ".schema.") ||
		reKnexMigTable.MatchString(src)
	if inMigrationsDir && hasSchemaBuilder {
		return true
	}
	// Fall back to a strong content signal: a knex schema builder chain plus an
	// FK/column DSL call. This catches inline migrations and seed/setup files.
	if strings.Contains(src, ".schema.") && (reKnexMigForeign.MatchString(src) || reKnexMigReferences.MatchString(src) || reKnexMigTable.MatchString(src)) {
		return true
	}
	return false
}

// anchor records a local FK-candidate column and the byte offset where it was declared.
type anchor struct {
	col    string
	offset int
}

// columnAnchors collects all local-column declarations that can anchor a
// subsequent .references() chain: explicit .foreign('col') calls and column
// builders (t.integer('author_id')). Returned in source order.
func columnAnchors(src string) []anchor {
	var out []anchor
	for _, m := range reKnexMigForeign.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, anchor{col: src[m[2]:m[3]], offset: m[0]})
	}
	for _, m := range reKnexMigColumn.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, anchor{col: src[m[4]:m[5]], offset: m[0]})
	}
	// Sort by offset so nearestAnchorBefore can scan in source order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].offset > out[j].offset; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// nearestAnchorBefore returns the column of the anchor with the greatest offset
// strictly less than refOffset (the column that the .references() chain modifies).
func nearestAnchorBefore(anchors []anchor, refOffset int) string {
	col := ""
	for _, a := range anchors {
		if a.offset < refOffset {
			col = a.col
			continue
		}
		break
	}
	return col
}

// nextInTableAfter returns the table named by the first .inTable(...) match
// whose start offset is at or after the given offset (the referenced table for
// a two-arg .references('col').inTable('table') chain).
func nextInTableAfter(inTables [][]int, src string, afterOffset int) string {
	for _, m := range inTables {
		if m[0] >= afterOffset {
			return src[m[2]:m[3]]
		}
	}
	return ""
}
