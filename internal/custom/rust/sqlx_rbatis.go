package rust

// sqlx_rbatis.go — custom extractors for SQLx and rbatis (Rust async ORMs).
//
// SQLx extractor covers:
//   - Models/schema_extraction: structs with #[derive(sqlx::FromRow)] or
//     FromRow derive → detected as ORM model mappings
//   - Migrations/migration_parsing: sqlx::migrate!() macro invocations and
//     files named like `migrations/V\d+__*.sql` detected via comment headers
//
// rbatis extractor covers:
//   - Models/model_extraction: #[derive(Debug, Clone)] structs adjacent to
//     #[crud_table(table_name="...")] attribute, or #[html_sql] / py_sql usage
//   - Models/schema_extraction: #[crud_table] table mapping
//   - Queries/query_attribution: #[sql("SELECT ...")] / #[py_sql("...")] /
//     #[html_sql] macro annotations on functions
//   - Migrations/migration_parsing: sqlx-style migrations directory patterns
//     and rbatis Snowflake / table-schema annotations
//
// Honesty:
//
//	partial — heuristic regex match on source text. We detect the registration
//	surface (macros, attributes) but cannot resolve cross-file type references
//	or verify migration ordering. Fixtures prove the detection surface.
//
// Issue #3267 — lang.rust.orm.sqlx schema_extraction + migration_parsing,
//               lang.rust.orm.rbatis all four missing cells.

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
	extractor.Register("custom_rust_sqlx", &rustSqlxExtractor{})
	extractor.Register("custom_rust_rbatis", &rustRbatisExtractor{})
}

// ---------------------------------------------------------------------------
// Shared SQL helpers
// ---------------------------------------------------------------------------

// reSQLTargetTable matches the table reference in the major SQL DML statements,
// covering the first table named after FROM / INSERT INTO / UPDATE / DELETE FROM /
// JOIN. Used to attribute a compile-time SQL string to a concrete table.
var reSQLTargetTable = regexp.MustCompile(
	`(?i)\b(?:FROM|INTO|UPDATE|JOIN|DELETE\s+FROM)\s+["` + "`" + `]?([A-Za-z_][\w.]*)["` + "`" + `]?`,
)

// sqlPrimaryTable returns the first/primary table referenced by a SQL string,
// or "" if none can be resolved. UPDATE/INSERT/DELETE targets take precedence
// over FROM/JOIN since they appear first in those statements anyway.
func sqlPrimaryTable(sql string) string {
	m := reSQLTargetTable.FindStringSubmatch(sql)
	if m == nil {
		return ""
	}
	return m[1]
}

// ===========================================================================
// SQLx
// ===========================================================================

type rustSqlxExtractor struct{}

func (e *rustSqlxExtractor) Language() string { return "custom_rust_sqlx" }

var (
	// #[derive(...FromRow...)] — sqlx row-mapping model
	reSqlxFromRow = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:FromRow|sqlx::FromRow)\b[^)]*\)\]`,
	)

	// sqlx::migrate!() or sqlx::migrate!("./migrations")
	reSqlxMigrateMacro = regexp.MustCompile(
		`sqlx::migrate!\s*\(([^)]*)\)`,
	)

	// sqlx::migrate!() invocation chain: .run(&pool)
	reSqlxMigrateRun = regexp.MustCompile(
		`sqlx::migrate!\s*\([^)]*\)\s*\.run\s*\(`,
	)

	// Migration file header comment pattern (detected in .rs file referencing migrations):
	// -- V001__create_users.sql
	reSqlxMigrationFileRef = regexp.MustCompile(
		`(?m)--\s*V\d+__\w+\.sql|migrations/\d+_`,
	)

	// Pool creation → schema connection evidence
	reSqlxPoolConnect = regexp.MustCompile(
		`(?:PgPool|MySqlPool|SqlitePool|AnyPool|Pool)\s*::\s*(?:connect|new|builder)\s*\(`,
	)

	// query! / query_as! macro with SQL literal.
	// query!("SELECT ...") — sql is first arg
	// query_as!(Type, "SELECT ...") — sql is second arg
	// Match both forms.
	reSqlxQueryMacro = regexp.MustCompile(
		`(?:sqlx::)?query(?:_as|_scalar|_as_unchecked)?!\s*\(\s*(?:[^,)]+,\s*)?"([^"]{5,})"`,
	)

	// struct Name (after FromRow derive)
	reStructNameSqlx = regexp.MustCompile(`\bstruct\s+(\w+)`)
)

func (e *rustSqlxExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_sqlx_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// -----------------------------------------------------------------------
	// 1. schema_extraction — #[derive(FromRow)] structs
	// -----------------------------------------------------------------------
	for _, m := range reSqlxFromRow.FindAllStringIndex(src, -1) {
		tail := src[m[1]:]
		if len(tail) > 500 {
			tail = tail[:500]
		}
		sm := reStructNameSqlx.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]
		ent := makeEntity("sqlx:model:"+sname, "SCOPE.Component", "orm_model",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "sqlx",
			"struct_name", sname,
			"provenance", "INFERRED_FROM_SQLX_FROM_ROW",
		)
		add(ent)
	}

	// Pool connection → schema connection entity
	for _, m := range reSqlxPoolConnect.FindAllStringIndex(src, -1) {
		ent := makeEntity("sqlx:pool_connect", "SCOPE.Component", "db_connection",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "sqlx",
			"provenance", "INFERRED_FROM_SQLX_POOL_CONNECT",
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 2. migration_parsing — sqlx::migrate!() macro
	// -----------------------------------------------------------------------
	for _, m := range reSqlxMigrateMacro.FindAllStringSubmatchIndex(src, -1) {
		migPath := strings.TrimSpace(strings.Trim(src[m[2]:m[3]], `"`))
		if migPath == "" {
			migPath = "./migrations"
		}
		ent := makeEntity("sqlx:migrate:"+migPath, "SCOPE.Component", "migration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "sqlx",
			"migration_path", migPath,
			"provenance", "INFERRED_FROM_SQLX_MIGRATE_MACRO",
		)
		add(ent)
	}

	// Migration file references in comments/strings
	for _, m := range reSqlxMigrationFileRef.FindAllStringIndex(src, -1) {
		ent := makeEntity("sqlx:migration_file_ref", "SCOPE.Pattern", "migration_reference",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "sqlx",
			"provenance", "INFERRED_FROM_SQLX_MIGRATION_FILE_REF",
		)
		add(ent)
	}

	// query! macros → query_attribution. Resolve the primary target table
	// from the compile-time SQL so the query is attributed to a concrete
	// table; encode that table in the entity name.
	for _, m := range reSqlxQueryMacro.FindAllStringSubmatchIndex(src, -1) {
		fullSQL := src[m[2]:m[3]]
		table := sqlPrimaryTable(fullSQL)
		sql := fullSQL
		if len(sql) > 80 {
			sql = sql[:80] + "..."
		}
		name := "sqlx:query"
		if table != "" {
			name = "sqlx:query:" + table
		}
		ent := makeEntity(name, "SCOPE.Operation", "sql_query",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "sqlx",
			"sql_fragment", sql,
			"target_table", table,
			"provenance", "INFERRED_FROM_SQLX_QUERY_MACRO",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// rbatis
// ===========================================================================

type rustRbatisExtractor struct{}

func (e *rustRbatisExtractor) Language() string { return "custom_rust_rbatis" }

var (
	// #[crud_table(table_name="...")] — rbatis model annotation
	reRbatisCrudTable = regexp.MustCompile(
		`#\[crud_table\s*\(([^)]*)\)\]`,
	)

	// #[derive(...)] followed by crud_table attr or rbatis types
	reRbatisDerive = regexp.MustCompile(
		`#\[derive\([^)]*\b(?:Debug|Clone|Serialize|Deserialize)\b[^)]*\)\]`,
	)

	// struct Name
	reStructNameRbatis = regexp.MustCompile(`\bstruct\s+(\w+)`)

	// #[py_sql("SELECT ...")] / #[py_sql(sql = "...")] on async fn
	reRbatisPySql = regexp.MustCompile(
		`#\[py_sql\s*\(\s*(?:sql\s*=\s*)?"([^"]{3,})"[^)]*\)\][\s\S]{0,200}?(?:async\s+)?fn\s+(\w+)\s*\(`,
	)

	// #[sql("SELECT ...")]  (rbatis v4 style)
	reRbatisSql = regexp.MustCompile(
		`#\[sql\s*\(\s*"([^"]{3,})"\s*\)\][\s\S]{0,200}?(?:async\s+)?fn\s+(\w+)\s*\(`,
	)

	// #[html_sql("...")] or #[html_sql]
	reRbatisHtmlSql = regexp.MustCompile(
		`#\[html_sql(?:\s*\([^)]*\))?\][\s\S]{0,200}?(?:async\s+)?fn\s+(\w+)\s*\(`,
	)

	// rbatis::Rbatis::new() — connection init
	reRbatisNew = regexp.MustCompile(
		`(?:rbatis::)?Rbatis\s*::\s*new\s*\(\s*\)`,
	)

	// rbatis migration: refactor of sqlx-migrate or custom migration structs
	// detect table_meta! or migration-related macros
	reRbatisMigration = regexp.MustCompile(
		`table_meta!\s*\([^)]+\)|rbatis::table_sync\s*!`,
	)

	// crud_table table_name extraction
	reTableNameAttr = regexp.MustCompile(`table_name\s*=\s*"([^"]+)"`)
)

func (e *rustRbatisExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_rbatis_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
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

	// -----------------------------------------------------------------------
	// 1. model_extraction + schema_extraction — #[crud_table] structs
	// -----------------------------------------------------------------------
	for _, m := range reRbatisCrudTable.FindAllStringSubmatchIndex(src, -1) {
		attrContent := src[m[2]:m[3]]

		// Extract table_name if present
		tableName := ""
		if tn := reTableNameAttr.FindStringSubmatch(attrContent); tn != nil {
			tableName = tn[1]
		}

		// Look forward for struct name
		tail := src[m[1]:]
		if len(tail) > 600 {
			tail = tail[:600]
		}
		sm := reStructNameRbatis.FindStringSubmatchIndex(tail)
		if sm == nil {
			continue
		}
		sname := tail[sm[2]:sm[3]]

		modelKey := sname
		if tableName != "" {
			modelKey = tableName
		}

		ent := makeEntity("rbatis:model:"+modelKey, "SCOPE.Component", "orm_model",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rbatis",
			"struct_name", sname,
			"table_name", tableName,
			"provenance", "INFERRED_FROM_RBATIS_CRUD_TABLE",
		)
		add(ent)

		// schema_extraction: also emit the schema entity
		if tableName != "" {
			schEnt := makeEntity("rbatis:schema:"+tableName, "SCOPE.Component", "schema_table",
				file.Path, file.Language, lineOf(src, m[0]))
			setProps(&schEnt,
				"framework", "rbatis",
				"table_name", tableName,
				"struct_name", sname,
				"provenance", "INFERRED_FROM_RBATIS_CRUD_TABLE_SCHEMA",
			)
			add(schEnt)
		}
	}

	// -----------------------------------------------------------------------
	// 2. query_attribution — #[py_sql("...")] fn
	// -----------------------------------------------------------------------
	for _, m := range reRbatisPySql.FindAllStringSubmatchIndex(src, -1) {
		sql := src[m[2]:m[3]]
		fnName := src[m[4]:m[5]]
		if len(sql) > 100 {
			sql = sql[:100] + "..."
		}
		ent := makeEntity("rbatis:py_sql:"+fnName, "SCOPE.Operation", "sql_query",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rbatis",
			"function_name", fnName,
			"sql_fragment", sql,
			"target_table", sqlPrimaryTable(src[m[2]:m[3]]),
			"query_style", "py_sql",
			"provenance", "INFERRED_FROM_RBATIS_PY_SQL",
		)
		add(ent)
	}

	// #[sql("...")] fn
	for _, m := range reRbatisSql.FindAllStringSubmatchIndex(src, -1) {
		sql := src[m[2]:m[3]]
		fnName := src[m[4]:m[5]]
		if len(sql) > 100 {
			sql = sql[:100] + "..."
		}
		ent := makeEntity("rbatis:sql:"+fnName, "SCOPE.Operation", "sql_query",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rbatis",
			"function_name", fnName,
			"sql_fragment", sql,
			"target_table", sqlPrimaryTable(src[m[2]:m[3]]),
			"query_style", "sql_attr",
			"provenance", "INFERRED_FROM_RBATIS_SQL_ATTR",
		)
		add(ent)
	}

	// #[html_sql] fn
	for _, m := range reRbatisHtmlSql.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		ent := makeEntity("rbatis:html_sql:"+fnName, "SCOPE.Operation", "sql_query",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rbatis",
			"function_name", fnName,
			"query_style", "html_sql",
			"provenance", "INFERRED_FROM_RBATIS_HTML_SQL",
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 3. Rbatis connection init → schema connection evidence
	// -----------------------------------------------------------------------
	for _, m := range reRbatisNew.FindAllStringIndex(src, -1) {
		ent := makeEntity("rbatis:Rbatis::new", "SCOPE.Component", "db_connection",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rbatis",
			"provenance", "INFERRED_FROM_RBATIS_NEW",
		)
		add(ent)
	}

	// -----------------------------------------------------------------------
	// 4. migration_parsing — table_meta! / rbatis::table_sync!
	// -----------------------------------------------------------------------
	for _, m := range reRbatisMigration.FindAllStringIndex(src, -1) {
		ent := makeEntity("rbatis:migration", "SCOPE.Component", "migration",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rbatis",
			"provenance", "INFERRED_FROM_RBATIS_MIGRATION_MACRO",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
