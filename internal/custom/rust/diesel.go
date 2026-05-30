package rust

// diesel.go — custom extractor for the Diesel ORM (Rust).
//
// Detects and emits entities for:
//
//   - table! {} macro declarations → SCOPE.Component (subtype="schema_table")
//   - #[derive(Queryable, Insertable, AsChangeset, ...)] struct annotations →
//     SCOPE.Component (subtype="orm_model") with the derive list in properties
//   - joinable!(table1 -> table2 (fk_col)) → SCOPE.Pattern (subtype="orm_relationship")
//   - #[belongs_to(Parent)] attribute → SCOPE.Pattern (subtype="orm_relationship")
//   - diesel migration files: diesel_migrations::embed_migrations! / run_pending_migrations /
//     MigrationHarness impls → SCOPE.Component (subtype="migration")
//   - Foreign-key columns in table! macro → SCOPE.Pattern (subtype="foreign_key")
//
// Honesty:
//
//	partial — heuristic regex match on source text. Does NOT perform
//	type-system analysis or resolve schema paths from diesel.toml.
//	Fixtures prove the detection surface; semantic cross-file resolution
//	requires import-graph analysis beyond this scanner.
//
// Issue #3267 — lang.rust.orm.diesel Relationships + Migrations cells.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_diesel", &rustDieselExtractor{})
}

type rustDieselExtractor struct{}

func (e *rustDieselExtractor) Language() string { return "custom_rust_diesel" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// table! { users (id) { id -> Integer, name -> Text, } }
	// Captures table name.
	reDieselTable = regexp.MustCompile(
		`\btable!\s*\{\s*(\w+)\s*[\({]`,
	)

	// #[derive(Queryable)] / #[derive(Queryable, Insertable, AsChangeset)]
	// Followed (within a few lines) by struct Name.
	// We capture the derive list and then the struct name via a two-step scan.
	reDieselDerive = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:Queryable|Insertable|AsChangeset|Identifiable|Associations|Selectable)\b[^)]*\)\]`,
	)
	reDieselDeriveList = regexp.MustCompile(
		`#\[derive\(([^)]+)\)\]`,
	)

	// struct Name following a diesel derive
	reStructName = regexp.MustCompile(`\bstruct\s+(\w+)`)

	// joinable!(posts -> users (user_id));
	reDieselJoinable = regexp.MustCompile(
		`\bjoinable!\s*\(\s*(\w+)\s*->\s*(\w+)\s*\(\s*(\w+)\s*\)\s*\)`,
	)

	// #[belongs_to(Parent)] / #[belongs_to(Parent, foreign_key = "parent_id")]
	reDieselBelongsTo = regexp.MustCompile(
		`#\[belongs_to\(\s*(\w+)(?:\s*,\s*[^)]+)?\s*\)\]`,
	)

	// diesel_migrations::embed_migrations!("path") or embed_migrations!()
	reDieselEmbedMigrations = regexp.MustCompile(
		`diesel_migrations::embed_migrations!\s*\(([^)]*)\)|embed_migrations!\s*\(([^)]*)\)`,
	)

	// run_pending_migrations(...) — migration execution
	reDieselRunMigrations = regexp.MustCompile(
		`run_pending_migrations\s*\(|connection\.run_pending_migrations\s*\(`,
	)

	// impl MigrationHarness for T  (diesel 2.x migration trait)
	reDieselMigrationHarness = regexp.MustCompile(
		`\bimpl\s+(?:<[^>]*>\s+)?MigrationHarness\b`,
	)

	// Foreign-key column pattern in table! body: col_name -> Nullable<Integer> or col_name -> Integer
	// We detect *_id columns as FK signals within table! macro bodies.
	// Capture: table name (from reDieselTable) then scan body for _id columns.
	reDieselTableBody = regexp.MustCompile(
		`\btable!\s*\{\s*(\w+)\s*[\({][^}]*\}`,
	)

	// Inside a table! body: field_name (ending in _id) -> SomeType
	reDieselFKColumn = regexp.MustCompile(
		`(\w+_id)\s*->\s*\w+`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustDieselExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_diesel_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. table! {} macro → schema_table entity
	for _, m := range reDieselTable.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		ent := makeEntity("diesel:schema:"+tableName, "SCOPE.Component", "schema_table",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"table_name", tableName,
			"provenance", "INFERRED_FROM_DIESEL_TABLE_MACRO",
		)
		add(ent)
	}

	// 2. #[derive(Queryable/Insertable/...)] struct → orm_model entity.
	//    We scan all derive attrs; for each diesel-bearing derive we look
	//    for the next struct declaration within 10 lines.
	deriveMatches := reDieselDerive.FindAllStringIndex(src, -1)
	for _, dm := range deriveMatches {
		// Full attribute text for the derive list.
		attrText := src[dm[0]:dm[1]]
		listMatch := reDieselDeriveList.FindStringSubmatch(attrText)
		deriveList := ""
		if len(listMatch) >= 2 {
			deriveList = listMatch[1]
		}

		// Scan forward from end of derive attr for `struct Name`.
		tail := src[dm[1]:]
		// Limit lookahead to the next ~500 characters (roughly 10 lines).
		if len(tail) > 500 {
			tail = tail[:500]
		}
		structMatch := reStructName.FindStringSubmatchIndex(tail)
		if structMatch == nil {
			continue
		}
		structName := tail[structMatch[2]:structMatch[3]]
		line := lineOf(src, dm[0])
		ent := makeEntity("diesel:model:"+structName, "SCOPE.Component", "orm_model",
			file.Path, file.Language, line)
		setProps(&ent,
			"framework", "diesel",
			"struct_name", structName,
			"derive_traits", strings.TrimSpace(deriveList),
			"provenance", "INFERRED_FROM_DIESEL_DERIVE",
		)
		add(ent)
	}

	// 3. joinable!(table1 -> table2 (fk)) → orm_relationship pattern
	for _, m := range reDieselJoinable.FindAllStringSubmatchIndex(src, -1) {
		fromTable := src[m[2]:m[3]]
		toTable := src[m[4]:m[5]]
		fkCol := src[m[6]:m[7]]
		name := "diesel:joinable:" + fromTable + "->" + toTable
		ent := makeEntity(name, "SCOPE.Pattern", "orm_relationship",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"from_table", fromTable,
			"to_table", toTable,
			"foreign_key", fkCol,
			"relationship_type", "joinable",
			"provenance", "INFERRED_FROM_DIESEL_JOINABLE_MACRO",
		)
		add(ent)
	}

	// 4. #[belongs_to(Parent)] → orm_relationship pattern
	for _, m := range reDieselBelongsTo.FindAllStringSubmatchIndex(src, -1) {
		parent := src[m[2]:m[3]]
		name := "diesel:belongs_to:" + parent
		ent := makeEntity(name, "SCOPE.Pattern", "orm_relationship",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"parent_model", parent,
			"relationship_type", "belongs_to",
			"provenance", "INFERRED_FROM_DIESEL_BELONGS_TO",
		)
		add(ent)
	}

	// 5. Foreign-key column extraction — scan table! macro bodies for *_id columns
	for _, m := range reDieselTableBody.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		tableBody := src[m[0]:m[1]]
		for _, fkm := range reDieselFKColumn.FindAllStringSubmatchIndex(tableBody, -1) {
			colName := tableBody[fkm[2]:fkm[3]]
			// Skip the primary key "id" itself — only *_id references
			if colName == "id" {
				continue
			}
			fkName := "diesel:fk:" + tableName + "." + colName
			fkEnt := makeEntity(fkName, "SCOPE.Pattern", "foreign_key",
				file.Path, file.Language, lineOf(src, m[0]))
			setProps(&fkEnt,
				"framework", "diesel",
				"table_name", tableName,
				"fk_column", colName,
				"provenance", "INFERRED_FROM_DIESEL_FK_COLUMN",
			)
			add(fkEnt)
		}
	}

	// 6. migration_parsing — embed_migrations! macro
	for _, m := range reDieselEmbedMigrations.FindAllStringSubmatchIndex(src, -1) {
		migPath := ""
		for i := 2; i < len(m); i += 2 {
			if m[i] >= 0 {
				migPath = strings.TrimSpace(strings.Trim(src[m[i]:m[i+1]], `"`))
				break
			}
		}
		if migPath == "" {
			migPath = "./migrations"
		}
		ent := makeEntity("diesel:embed_migrations:"+migPath, "SCOPE.Component", "migration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"migration_path", migPath,
			"provenance", "INFERRED_FROM_DIESEL_EMBED_MIGRATIONS",
		)
		add(ent)
	}

	// 7. run_pending_migrations → migration execution entity
	for _, m := range reDieselRunMigrations.FindAllStringIndex(src, -1) {
		ent := makeEntity("diesel:run_pending_migrations", "SCOPE.Component", "migration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"provenance", "INFERRED_FROM_DIESEL_RUN_MIGRATIONS",
		)
		add(ent)
	}

	// 8. impl MigrationHarness → migration trait implementation
	for _, m := range reDieselMigrationHarness.FindAllStringIndex(src, -1) {
		ent := makeEntity("diesel:MigrationHarness", "SCOPE.Pattern", "migration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "diesel",
			"provenance", "INFERRED_FROM_DIESEL_MIGRATION_HARNESS",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
