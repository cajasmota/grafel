// Package scala — ORM extractors for Slick, Doobie, Quill, ScalikeJDBC,
// Scanamo, and elastic4s.
//
// Coverage targets addressed by this file:
//   - Models.schema_extraction    (partial) for all six ORMs
//   - Relationships.*             (partial or not_applicable depending on ORM)
//   - Migrations.migration_parsing (partial or not_applicable depending on ORM)
//
// Scanamo and elastic4s are NoSQL libraries (DynamoDB / Elasticsearch);
// relational concepts (foreign_key, relationship, migration) are not_applicable.
// Lazy-loading is an eager-by-default concept in all these Scala ORMs and is
// not_applicable uniformly.
package scala

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
	extractor.Register("custom_scala_slick", &slickExtractor{})
	extractor.Register("custom_scala_doobie", &doobieExtractor{})
	extractor.Register("custom_scala_quill", &quillExtractor{})
	extractor.Register("custom_scala_scalikejdbc", &scalikejdbcExtractor{})
	extractor.Register("custom_scala_scanamo", &scanamoExtractor{})
	extractor.Register("custom_scala_elastic4s", &elastic4sExtractor{})
}

// ============================================================================
// Slick
// ============================================================================

// slickExtractor extracts:
//   - Table class definitions → SCOPE.Schema (schema_extraction)
//   - Column definitions inside Table → SCOPE.Schema (schema_extraction)
//   - foreignKey(...) declarations → SCOPE.Schema (foreign_key / relationship)
//   - def * (default projection) → SCOPE.Schema (schema_extraction)
//   - TableQuery[T] → SCOPE.Schema (table-level query object)
//   - DBIO.seq / schema.create → SCOPE.Schema (migration_parsing)
type slickExtractor struct{}

func (e *slickExtractor) Language() string { return "custom_scala_slick" }

var (
	// slickTableClassRe: class Users(tag: Tag) extends Table[User](tag, "users")
	slickTableClassRe = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*\([^)]*Tag[^)]*\)\s+extends\s+Table\[(\w+)\]`)

	// slickColumnRe: def id = column[Long]("id", O.PrimaryKey)
	slickColumnRe = regexp.MustCompile(
		`(?m)def\s+(\w+)\s*=\s*column\[([^\]]+)\]\s*\(`)

	// slickForeignKeyRe: def fkUserId = foreignKey("fk_user_id", userId, userTable)
	slickForeignKeyRe = regexp.MustCompile(
		`(?m)def\s+(\w+)\s*=\s*foreignKey\s*\(\s*"([^"]+)"`)

	// slickTableQueryRe: val userTable = TableQuery[Users]
	slickTableQueryRe = regexp.MustCompile(
		`(?m)(?:val|lazy val)\s+(\w+)\s*=\s*TableQuery\[(\w+)\]`)

	// slickMigrationRe: schema.create / schema.createIfNotExists / DBIO.seq
	slickMigrationRe = regexp.MustCompile(
		`(?m)(?:schema\.create(?:IfNotExists)?|DBIO\.seq)\s*[({]`)
)

func (e *slickExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.slick_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "slick"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	// Gate: must look like a Slick file
	if !strings.Contains(src, "slick") && !strings.Contains(src, "extends Table[") &&
		!strings.Contains(src, "TableQuery[") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// Table class definitions → SCOPE.Schema
	for _, m := range slickTableClassRe.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		rowType := src[m[4]:m[5]]
		ent := makeEntity(tableName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slick",
			"provenance", "INFERRED_FROM_SLICK_TABLE_CLASS",
			"row_type", rowType,
			"pattern_type", "table_class")
		add(ent)
	}

	// Column definitions → SCOPE.Schema
	for _, m := range slickColumnRe.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		colType := src[m[4]:m[5]]
		ent := makeEntity("col:"+colName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slick",
			"provenance", "INFERRED_FROM_SLICK_COLUMN_DEF",
			"column_name", colName,
			"column_type", colType,
			"pattern_type", "column_def")
		add(ent)
	}

	// Foreign key declarations → SCOPE.Schema
	for _, m := range slickForeignKeyRe.FindAllStringSubmatchIndex(src, -1) {
		defName := src[m[2]:m[3]]
		fkName := src[m[4]:m[5]]
		ent := makeEntity("fk:"+fkName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slick",
			"provenance", "INFERRED_FROM_SLICK_FOREIGN_KEY",
			"fk_def_name", defName,
			"fk_constraint_name", fkName,
			"pattern_type", "foreign_key")
		add(ent)
	}

	// TableQuery → SCOPE.Schema
	for _, m := range slickTableQueryRe.FindAllStringSubmatchIndex(src, -1) {
		queryName := src[m[2]:m[3]]
		tableClass := src[m[4]:m[5]]
		ent := makeEntity("query:"+queryName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slick",
			"provenance", "INFERRED_FROM_SLICK_TABLE_QUERY",
			"query_name", queryName,
			"table_class", tableClass,
			"pattern_type", "table_query")
		add(ent)
	}

	// Migration patterns → SCOPE.Schema
	for _, m := range slickMigrationRe.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("migration:schema_ddl", "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "slick",
			"provenance", "INFERRED_FROM_SLICK_SCHEMA_DDL",
			"pattern_type", "migration")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// Doobie
// ============================================================================

// doobieExtractor extracts:
//   - sql"..." fragments → SCOPE.Operation/query (query_attribution, schema_extraction)
//   - case class used with .query[T] → SCOPE.Schema (schema_extraction / model)
//   - Transactor definitions → SCOPE.Service
//
// Doobie is a functional JDBC wrapper, not an ORM. There are no ORM-style
// relationship declarations (foreignKey, hasMany, etc.); associations are
// expressed via raw SQL joins. Migration management is not part of doobie —
// use Flyway/Liquibase alongside it.
type doobieExtractor struct{}

func (e *doobieExtractor) Language() string { return "custom_scala_doobie" }

var (
	// doobieSQLRe matches sql"..." and fr"..." interpolated fragments.
	// Handles triple-quote (sql"""...""") and single-quote (sql"...") forms.
	doobieSQLRe = regexp.MustCompile(
		`(?m)(?:sql|fr)(?:"""([^"]{0,200})"""|"([^"]{0,200})")`)

	// doobieQueryRe: .query[UserRow] / .query[(Long, String)]
	doobieQueryRe = regexp.MustCompile(
		`(?m)\.query\[([^\]]+)\]`)

	// doobieTransactorRe: Transactor.fromDriverManager / HikariTransactor.newHikariTransactor
	doobieTransactorRe = regexp.MustCompile(
		`(?m)(Transactor\.fromDriverManager|HikariTransactor\.newHikariTransactor)\s*[\[(]`)

	// doobieCaseClassRe: case class used as row type in doobie-flavoured files
	doobieCaseClassRe = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*\(([^)]+)\)`)
)

func (e *doobieExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.doobie_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "doobie"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "doobie") && !strings.Contains(src, "ConnectionIO") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// sql"..." / sql"""...""" interpolated queries
	// Groups: m[2]:m[3] = triple-quote body, m[4]:m[5] = single-quote body
	for _, m := range doobieSQLRe.FindAllStringSubmatchIndex(src, -1) {
		body := ""
		if m[2] >= 0 {
			body = strings.TrimSpace(src[m[2]:m[3]])
		} else if m[4] >= 0 {
			body = strings.TrimSpace(src[m[4]:m[5]])
		}
		preview := body
		if len(preview) > 60 {
			preview = preview[:60]
		}
		ent := makeEntity("sql:"+preview, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doobie",
			"provenance", "INFERRED_FROM_DOOBIE_SQL_FRAGMENT",
			"pattern_type", "sql_fragment")
		add(ent)
	}

	// .query[T] type mappings — schema/model extraction
	for _, m := range doobieQueryRe.FindAllStringSubmatchIndex(src, -1) {
		rowType := strings.TrimSpace(src[m[2]:m[3]])
		ent := makeEntity("row_type:"+rowType, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doobie",
			"provenance", "INFERRED_FROM_DOOBIE_QUERY_TYPE",
			"row_type", rowType,
			"pattern_type", "row_type_mapping")
		add(ent)
	}

	// Transactor definitions
	for _, m := range doobieTransactorRe.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		ent := makeEntity("transactor:"+kind, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doobie",
			"provenance", "INFERRED_FROM_DOOBIE_TRANSACTOR",
			"transactor_kind", kind,
			"pattern_type", "transactor")
		add(ent)
	}

	// Case class definitions (row models)
	for _, m := range doobieCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "doobie",
			"provenance", "INFERRED_FROM_DOOBIE_CASE_CLASS",
			"pattern_type", "row_model")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// Quill
// ============================================================================

// quillExtractor extracts:
//   - querySchema[T](...) → SCOPE.Schema (schema_extraction / model mapping)
//   - case class definitions in Quill context → SCOPE.Schema (model)
//   - quote { query[T] ... } → SCOPE.Operation/query (schema_extraction)
//   - ctx.run(...) → SCOPE.Operation/query
//
// Quill uses compile-time macro expansion; the extractor captures source-level
// patterns. Quill does not manage migrations; use Flyway/Liquibase alongside.
// No ORM-style FK/hasMany declarations — joins are expressed via quote blocks.
type quillExtractor struct{}

func (e *quillExtractor) Language() string { return "custom_scala_quill" }

var (
	// quillQuerySchemaRe: querySchema[User]("users", _.id -> "user_id")
	quillQuerySchemaRe = regexp.MustCompile(
		`(?m)querySchema\[(\w+)\]\s*\(\s*"([^"]+)"`)

	// quillQuoteQueryRe: quote { query[User] }
	quillQuoteQueryRe = regexp.MustCompile(
		`(?m)quote\s*\{[^}]*query\[(\w+)\]`)

	// quillCtxRunRe: ctx.run(...)
	quillCtxRunRe = regexp.MustCompile(
		`(?m)ctx\.run\s*\(`)

	// quillCaseClassRe: case classes used as Quill entity types
	quillCaseClassRe = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*\(([^)]+)\)`)
)

func (e *quillExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.quill_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "quill"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "quill") && !strings.Contains(src, "quote {") &&
		!strings.Contains(src, "querySchema[") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// querySchema[T]("table") → schema_extraction
	for _, m := range quillQuerySchemaRe.FindAllStringSubmatchIndex(src, -1) {
		entityType := src[m[2]:m[3]]
		tableName := src[m[4]:m[5]]
		ent := makeEntity("schema:"+entityType, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "quill",
			"provenance", "INFERRED_FROM_QUILL_QUERY_SCHEMA",
			"entity_type", entityType,
			"table_name", tableName,
			"pattern_type", "query_schema")
		add(ent)
	}

	// quote { query[T] } blocks
	for _, m := range quillQuoteQueryRe.FindAllStringSubmatchIndex(src, -1) {
		entityType := src[m[2]:m[3]]
		ent := makeEntity("quote:"+entityType, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "quill",
			"provenance", "INFERRED_FROM_QUILL_QUOTE_BLOCK",
			"entity_type", entityType,
			"pattern_type", "quoted_query")
		add(ent)
	}

	// ctx.run() execution sites
	for _, m := range quillCtxRunRe.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("ctx_run", "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "quill",
			"provenance", "INFERRED_FROM_QUILL_CTX_RUN",
			"pattern_type", "query_execution")
		add(ent)
	}

	// Case class definitions (entity models)
	for _, m := range quillCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "quill",
			"provenance", "INFERRED_FROM_QUILL_CASE_CLASS",
			"pattern_type", "entity_model")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// ScalikeJDBC
// ============================================================================

// scalikejdbcExtractor extracts:
//   - object/class extends SQLSyntaxSupport → SCOPE.Schema (model + schema_extraction)
//   - column fields via autoColumns / #auto → SCOPE.Schema (schema_extraction)
//   - sql"..." queries → SCOPE.Operation/query
//   - hasMany / belongsTo relationship declarations → SCOPE.Schema (association_extraction)
//   - DB migration blocks (DB.autoCommit with DDL) → SCOPE.Schema (migration_parsing)
type scalikejdbcExtractor struct{}

func (e *scalikejdbcExtractor) Language() string { return "custom_scala_scalikejdbc" }

var (
	// scalikejdbcSyntaxSupportRe: object User extends SQLSyntaxSupport[User]
	scalikejdbcSyntaxSupportRe = regexp.MustCompile(
		`(?m)(?:object|class)\s+(\w+)\s+extends\s+SQLSyntaxSupport\[(\w+)\]`)

	// scalikejdbcAutoColumnsRe: val defaultAlias = syntax("u") / autoColumns
	scalikejdbcAutoColumnsRe = regexp.MustCompile(
		`(?m)(?:autoColumns|columnNames|\.syntax)\s*\(`)

	// scalikejdbcSQLRe: sql"..." / sql"""...""" within scalikejdbc context
	scalikejdbcSQLRe = regexp.MustCompile(
		`(?m)sql(?:"""([^"]{0,200})"""|"([^"]{0,200})")`)

	// scalikejdbcHasManyRe: hasMany[Order](...)
	scalikejdbcHasManyRe = regexp.MustCompile(
		`(?m)(hasMany|hasManyThrough|hasOne|belongsTo)\s*\[\s*(\w+)`)

	// scalikejdbcDBMigrationRe: DB autoCommit / DB localTx with CREATE TABLE / ALTER TABLE
	scalikejdbcDBMigrationRe = regexp.MustCompile(
		`(?m)DB\s+(?:autoCommit|localTx|readOnly)\s*\{`)

	// scalikejdbcCaseClassRe: case class as model
	scalikejdbcCaseClassRe = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*\(([^)]+)\)`)
)

func (e *scalikejdbcExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scalikejdbc_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "scalikejdbc"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "scalikejdbc") && !strings.Contains(src, "SQLSyntaxSupport") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// SQLSyntaxSupport companion objects → model schema
	for _, m := range scalikejdbcSyntaxSupportRe.FindAllStringSubmatchIndex(src, -1) {
		objName := src[m[2]:m[3]]
		modelType := src[m[4]:m[5]]
		ent := makeEntity(objName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scalikejdbc",
			"provenance", "INFERRED_FROM_SCALIKEJDBC_SYNTAX_SUPPORT",
			"model_type", modelType,
			"pattern_type", "syntax_support")
		add(ent)
	}

	// sql"..." / sql"""...""" queries
	// Groups: m[2]:m[3] = triple-quote body, m[4]:m[5] = single-quote body
	for _, m := range scalikejdbcSQLRe.FindAllStringSubmatchIndex(src, -1) {
		body := ""
		if m[2] >= 0 {
			body = strings.TrimSpace(src[m[2]:m[3]])
		} else if m[4] >= 0 {
			body = strings.TrimSpace(src[m[4]:m[5]])
		}
		preview := body
		if len(preview) > 60 {
			preview = preview[:60]
		}
		ent := makeEntity("sql:"+preview, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scalikejdbc",
			"provenance", "INFERRED_FROM_SCALIKEJDBC_SQL",
			"pattern_type", "sql_query")
		add(ent)
	}

	// hasMany / belongsTo relationship declarations
	for _, m := range scalikejdbcHasManyRe.FindAllStringSubmatchIndex(src, -1) {
		relKind := src[m[2]:m[3]]
		targetModel := src[m[4]:m[5]]
		ent := makeEntity("rel:"+relKind+":"+targetModel, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scalikejdbc",
			"provenance", "INFERRED_FROM_SCALIKEJDBC_RELATIONSHIP",
			"rel_kind", relKind,
			"target_model", targetModel,
			"pattern_type", "relationship")
		add(ent)
	}

	// DB block patterns (migration_parsing signal)
	for _, m := range scalikejdbcDBMigrationRe.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("db_block", "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scalikejdbc",
			"provenance", "INFERRED_FROM_SCALIKEJDBC_DB_BLOCK",
			"pattern_type", "db_session_block")
		add(ent)
	}

	// Case class row models
	for _, m := range scalikejdbcCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scalikejdbc",
			"provenance", "INFERRED_FROM_SCALIKEJDBC_CASE_CLASS",
			"pattern_type", "row_model")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// Scanamo (DynamoDB)
// ============================================================================

// scanamoExtractor extracts DynamoDB table definitions and case class schema
// mappings for Scanamo.
//
// Scanamo is a Scala library for AWS DynamoDB — a NoSQL key-value / document
// store. Relational concepts (foreign keys, SQL migrations) do not apply.
// There is no lazy-loading in the JDBC/ORM sense.
type scanamoExtractor struct{}

func (e *scanamoExtractor) Language() string { return "custom_scala_scanamo" }

var (
	// scanamoTableRe: Table[User]("users") / AsyncTable[User]("users")
	scanamoTableRe = regexp.MustCompile(
		`(?m)(?:Async)?Table\[(\w+)\]\s*\(\s*"([^"]+)"`)

	// scanamoScanamoCatsRe: ScanamoCats(client) / Scanamo(client)
	scanamoScanamoCatsRe = regexp.MustCompile(
		`(?m)(ScanamoCats|ScanamoAlpakka|Scanamo)\s*\(`)

	// scanamoCaseClassRe: case classes used as DynamoDB item types
	scanamoCaseClassRe = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*\(([^)]+)\)`)

	// scanamoDynamoFormatRe: implicit/given DynamoFormat derivation
	scanamoDynamoFormatRe = regexp.MustCompile(
		`(?m)DynamoFormat\[(\w+)\]`)
)

func (e *scanamoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.scanamo_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "scanamo"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "scanamo") && !strings.Contains(src, "Scanamo") &&
		!strings.Contains(src, "DynamoFormat") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// Table[T]("tableName") → SCOPE.Schema
	for _, m := range scanamoTableRe.FindAllStringSubmatchIndex(src, -1) {
		itemType := src[m[2]:m[3]]
		tableName := src[m[4]:m[5]]
		ent := makeEntity("table:"+tableName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scanamo",
			"provenance", "INFERRED_FROM_SCANAMO_TABLE_DEF",
			"item_type", itemType,
			"table_name", tableName,
			"pattern_type", "dynamodb_table")
		add(ent)
	}

	// Scanamo(client) / ScanamoCats(client) → SCOPE.Service
	for _, m := range scanamoScanamoCatsRe.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		ent := makeEntity("client:"+kind, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scanamo",
			"provenance", "INFERRED_FROM_SCANAMO_CLIENT",
			"client_kind", kind,
			"pattern_type", "dynamodb_client")
		add(ent)
	}

	// DynamoFormat[T] → SCOPE.Schema (schema mapping)
	for _, m := range scanamoDynamoFormatRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		ent := makeEntity("format:"+typeName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scanamo",
			"provenance", "INFERRED_FROM_SCANAMO_DYNAMO_FORMAT",
			"type_name", typeName,
			"pattern_type", "dynamo_format")
		add(ent)
	}

	// Case class definitions (item models)
	for _, m := range scanamoCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "scanamo",
			"provenance", "INFERRED_FROM_SCANAMO_CASE_CLASS",
			"pattern_type", "item_model")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ============================================================================
// elastic4s (Elasticsearch)
// ============================================================================

// elastic4sExtractor extracts Elasticsearch index definitions and document
// mappings for the elastic4s Scala client.
//
// elastic4s is a Scala client for Elasticsearch — a distributed document /
// full-text search engine, NOT a relational database. Foreign keys, SQL
// migrations, and ORM-style lazy-loading do not apply. Schema extraction
// is partial: index definitions and case class document types are captured.
type elastic4sExtractor struct{}

func (e *elastic4sExtractor) Language() string { return "custom_scala_elastic4s" }

var (
	// elastic4sClientRe: ElasticClient(JavaClient(...))
	elastic4sClientRe = regexp.MustCompile(
		`(?m)ElasticClient\s*\(`)

	// elastic4sIndexRe: createIndex("my-index") / indexInto("index")
	elastic4sIndexRe = regexp.MustCompile(
		`(?m)(?:createIndex|deleteIndex|indexInto|updateIndex)\s*\(\s*"([^"]+)"`)

	// elastic4sSearchRe: search("index").query(...)
	elastic4sSearchRe = regexp.MustCompile(
		`(?m)search\s*\(\s*"([^"]+)"\s*\)`)

	// elastic4sCaseClassRe: case classes used as ES document types
	elastic4sCaseClassRe = regexp.MustCompile(
		`(?m)case\s+class\s+(\w+)\s*\(([^)]+)\)`)

	// elastic4sHitReaderRe: implicit HitReader[T] / HitWriter[T]
	elastic4sHitReaderRe = regexp.MustCompile(
		`(?m)(?:Hit(?:Reader|Writer)|Indexable)\[(\w+)\]`)
)

func (e *elastic4sExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/scala")
	_, span := tracer.Start(ctx, "indexer.elastic4s_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "elastic4s"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "scala" {
		return nil, nil
	}

	src := string(file.Content)
	if !strings.Contains(src, "elastic4s") && !strings.Contains(src, "ElasticClient") &&
		!strings.Contains(src, "ElasticDsl") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if !seen[key] {
			seen[key] = true
			entities = append(entities, ent)
		}
	}

	// ElasticClient instantiation → SCOPE.Service
	for _, m := range elastic4sClientRe.FindAllStringSubmatchIndex(src, -1) {
		ent := makeEntity("elastic_client", "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elastic4s",
			"provenance", "INFERRED_FROM_ELASTIC4S_CLIENT",
			"pattern_type", "es_client")
		add(ent)
	}

	// createIndex / indexInto → SCOPE.Schema (index definition)
	for _, m := range elastic4sIndexRe.FindAllStringSubmatchIndex(src, -1) {
		indexName := src[m[2]:m[3]]
		ent := makeEntity("index:"+indexName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elastic4s",
			"provenance", "INFERRED_FROM_ELASTIC4S_INDEX",
			"index_name", indexName,
			"pattern_type", "es_index")
		add(ent)
	}

	// search("index") → SCOPE.Operation/query
	for _, m := range elastic4sSearchRe.FindAllStringSubmatchIndex(src, -1) {
		indexName := src[m[2]:m[3]]
		ent := makeEntity("search:"+indexName, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elastic4s",
			"provenance", "INFERRED_FROM_ELASTIC4S_SEARCH",
			"index_name", indexName,
			"pattern_type", "es_search")
		add(ent)
	}

	// HitReader[T] / HitWriter[T] → SCOPE.Schema (document type mapping)
	for _, m := range elastic4sHitReaderRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[m[2]:m[3]]
		ent := makeEntity("hit_type:"+typeName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elastic4s",
			"provenance", "INFERRED_FROM_ELASTIC4S_HIT_READER",
			"type_name", typeName,
			"pattern_type", "es_document_type")
		add(ent)
	}

	// Case class definitions (document models)
	for _, m := range elastic4sCaseClassRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elastic4s",
			"provenance", "INFERRED_FROM_ELASTIC4S_CASE_CLASS",
			"pattern_type", "document_model")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
