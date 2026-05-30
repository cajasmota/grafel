// Package kotlin — schema and relationship extractors for Kotlin ORMs:
// Exposed, Ktorm, Room, and SQLDelight.
//
// Each extractor emits:
//   - SCOPE.Schema (subtype="table") for table/entity declarations  → schema_extraction
//   - SCOPE.Schema (subtype="column") for column/field definitions  → schema_extraction
//   - SCOPE.Relationship (subtype="foreign_key") for FK columns     → foreign_key_extraction
//   - SCOPE.Relationship (subtype="association") for @Relation etc. → association_extraction / relationship_extraction
//   - SCOPE.Schema (subtype="migration") for migration fragments    → migration_parsing
//
// Cells covered:
//
//	lang.kotlin.orm.exposed   Models.schema_extraction, Relationships.*,       Migrations.migration_parsing
//	lang.kotlin.orm.ktorm     Models.schema_extraction, Relationships.*,       Migrations.migration_parsing (N/A)
//	lang.kotlin.orm.room      Models.schema_extraction, Relationships.*,       Migrations.migration_parsing
//	lang.kotlin.orm.sqldelight Models.schema_extraction, Relationships.*,      Migrations.migration_parsing
//
// Issue #3275 — Part of Kotlin routing + ORM-depth builds.
package kotlin

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
	extractor.Register("custom_kotlin_exposed_schema", &kotlinExposedSchemaExtractor{})
	extractor.Register("custom_kotlin_ktorm_schema", &kotlinKtormSchemaExtractor{})
	extractor.Register("custom_kotlin_room_schema", &kotlinRoomSchemaExtractor{})
	extractor.Register("custom_kotlin_sqldelight_schema", &kotlinSQLDelightSchemaExtractor{})
}

// ===========================================================================
// Exposed — Jetbrains Exposed DSL / DAO
// ===========================================================================

// kotlinExposedSchemaExtractor emits schema entities for Jetbrains Exposed
// table objects and their column / foreign-key definitions.
//
// Patterns:
//
//	object Users : Table() { ... }
//	object Users : IntIdTable("users") { ... }
//	val name = varchar("name", 50)
//	val userId = reference("user_id", Users)
//	val orderId = (integer("order_id") references Orders.id)
type kotlinExposedSchemaExtractor struct{}

func (e *kotlinExposedSchemaExtractor) Language() string { return "custom_kotlin_exposed_schema" }

var (
	// reExposedTable matches: object Foo : Table() / IntIdTable("foo") / LongIdTable / UUIDTable / …
	reExposedTable = regexp.MustCompile(
		`(?m)^\s*object\s+([A-Z][A-Za-z0-9_]*)\s*:\s*(?:[A-Za-z0-9_]*[Tt]able\b)[^{]*\{`)

	// reExposedColumn matches column declarations:
	//   val name = varchar("col_name", 50)
	//   val age = integer("age")
	// Captures: (field_name, col_type)
	reExposedColumn = regexp.MustCompile(
		`(?m)^\s+val\s+([a-z][A-Za-z0-9_]*)\s*=\s*([a-z][A-Za-z0-9_]*)\s*\(`)

	// reExposedReference matches FK columns:
	//   val userId = reference("user_id", Users)
	//   val userId = (integer("user_id") references Users.id)
	// Captures: (field_name, referenced_table)
	reExposedReference = regexp.MustCompile(
		`(?m)^\s+val\s+([a-z][A-Za-z0-9_]*)\s*=\s*\(?\s*(?:reference\s*\(\s*"[^"]*"\s*,\s*([A-Za-z0-9_]+)|` +
			`[a-z]+\s*\([^)]*\)\s+references\s+([A-Za-z0-9_]+))`)

	// reExposedMigration matches SchemaUtils.create / addMissingColumnsStatements patterns.
	reExposedMigration = regexp.MustCompile(
		`(?m)\bSchemaUtils\s*\.\s*(create|createMissingTablesAndColumns|drop|addMissingColumnsStatements)\s*\(`)
)

func (e *kotlinExposedSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_exposed_schema.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("orm", "exposed"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	// Gate: must reference Table or Exposed idioms.
	if !strings.Contains(src, "Table") && !strings.Contains(src, "reference") {
		return nil, nil
	}

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

	// Tables.
	for _, m := range reExposedTable.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, "kotlin", line)
		setProps(&ent, "orm", "exposed", "provenance", "INFERRED_FROM_EXPOSED_TABLE")
		add(ent)
	}

	// Columns.
	for _, m := range reExposedColumn.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		colType := src[m[4]:m[5]]
		// Skip reference() calls — handled separately.
		if colType == "reference" || colType == "optReference" {
			continue
		}
		line := lineOf(src, m[0])
		ent := makeEntity(fieldName, "SCOPE.Schema", "column", file.Path, "kotlin", line)
		setProps(&ent, "orm", "exposed", "column_type", colType, "provenance", "INFERRED_FROM_EXPOSED_COLUMN")
		add(ent)
	}

	// Foreign keys.
	for _, m := range reExposedReference.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		refTable := ""
		if m[4] >= 0 {
			refTable = src[m[4]:m[5]]
		} else if m[6] >= 0 {
			refTable = src[m[6]:m[7]]
		}
		line := lineOf(src, m[0])
		name := fieldName + " -> " + refTable
		ent := makeEntity(name, "SCOPE.Relationship", "foreign_key", file.Path, "kotlin", line)
		setProps(&ent, "orm", "exposed", "field", fieldName, "references", refTable,
			"provenance", "INFERRED_FROM_EXPOSED_REFERENCE")
		add(ent)
	}

	// Migrations (SchemaUtils usage).
	for _, m := range reExposedMigration.FindAllStringSubmatchIndex(src, -1) {
		op := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "migration:" + op + ":" + file.Path
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "exposed", "operation", op, "provenance", "INFERRED_FROM_EXPOSED_SCHEMA_UTILS")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// Ktorm — schema and FK extraction
// ===========================================================================

// kotlinKtormSchemaExtractor emits schema entities for Ktorm table bindings.
//
// Patterns:
//
//	object Employees : Table<Employee>("t_employee") {
//	    val id = int("id").primaryKey().bindTo { it.id }
//	    val name = varchar("name").bindTo { it.name }
//	    val deptId = int("dept_id").references(Departments) { it.department }
//	}
type kotlinKtormSchemaExtractor struct{}

func (e *kotlinKtormSchemaExtractor) Language() string { return "custom_kotlin_ktorm_schema" }

var (
	// reKtormTable matches: object Foo : Table<T>("table_name") {
	reKtormTable = regexp.MustCompile(
		`(?m)^\s*object\s+([A-Z][A-Za-z0-9_]*)\s*:\s*Table\s*<[^>]+>\s*\(`)

	// reKtormColumn matches column bindings:
	//   val name = varchar("name").bindTo { it.name }
	// Captures: (field_name, sql_col_type)
	reKtormColumn = regexp.MustCompile(
		`(?m)^\s+val\s+([a-z][A-Za-z0-9_]*)\s*=\s*([a-z][A-Za-z0-9_]*)\s*\(\s*"[^"]*"\s*\)`)

	// reKtormReference matches FK:
	//   .references(Departments) { it.department }
	// We pair this with the preceding val name — capture via surrounding context.
	reKtormReference = regexp.MustCompile(
		`(?m)^\s+val\s+([a-z][A-Za-z0-9_]*)\s*=\s*[a-z][A-Za-z0-9_]*\s*\([^)]*\)[^.]*\.references\s*\(\s*([A-Za-z0-9_]+)\s*\)`)
)

func (e *kotlinKtormSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_ktorm_schema.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("orm", "ktorm"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "Table<") && !strings.Contains(src, "ktorm") {
		return nil, nil
	}

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

	// Tables.
	for _, m := range reKtormTable.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, "kotlin", line)
		setProps(&ent, "orm", "ktorm", "provenance", "INFERRED_FROM_KTORM_TABLE")
		add(ent)
	}

	// Columns.
	for _, m := range reKtormColumn.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		colType := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		ent := makeEntity(fieldName, "SCOPE.Schema", "column", file.Path, "kotlin", line)
		setProps(&ent, "orm", "ktorm", "column_type", colType, "provenance", "INFERRED_FROM_KTORM_COLUMN")
		add(ent)
	}

	// Foreign keys via .references().
	for _, m := range reKtormReference.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		refTable := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := fieldName + " -> " + refTable
		ent := makeEntity(name, "SCOPE.Relationship", "foreign_key", file.Path, "kotlin", line)
		setProps(&ent, "orm", "ktorm", "field", fieldName, "references", refTable,
			"provenance", "INFERRED_FROM_KTORM_REFERENCE")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// Room — Android Room ORM
// ===========================================================================

// kotlinRoomSchemaExtractor emits schema entities for Android Room.
//
// Patterns:
//
//	@Entity(tableName = "users")
//	data class User(@PrimaryKey val id: Int, val name: String)
//
//	@Entity(foreignKeys = [ForeignKey(entity = User::class, parentColumns = ["id"], childColumns = ["user_id"])])
//	data class Order(...)
//
//	@Relation(parentColumn = "id", entityColumn = "user_id")
//	val orders: List<Order>
//
//	@Database(entities = [...], version = 2)
//	abstract class AppDatabase : RoomDatabase()
type kotlinRoomSchemaExtractor struct{}

func (e *kotlinRoomSchemaExtractor) Language() string { return "custom_kotlin_room_schema" }

var (
	// reRoomEntity matches @Entity annotated data class / class declarations.
	// The optional annotation argument list may contain one level of nested
	// parens (e.g. foreignKeys = [ForeignKey(entity = User::class, ...)]), so
	// the argument group tolerates inner (...) groups before the closing paren.
	reRoomEntity = regexp.MustCompile(
		`(?s)@Entity\s*(?:\((?:[^()]|\([^()]*\))*\))?\s*(?:data\s+)?class\s+([A-Z][A-Za-z0-9_]*)`)

	// reRoomTableName extracts tableName from @Entity(tableName = "...").
	reRoomTableName = regexp.MustCompile(
		`@Entity\s*\([^)]*tableName\s*=\s*"([^"]+)"`)

	// reRoomForeignKey extracts entity from ForeignKey(entity = Foo::class, ...).
	// Captures: (entity_class)
	reRoomForeignKey = regexp.MustCompile(
		`ForeignKey\s*\(\s*entity\s*=\s*([A-Za-z0-9_]+)::class`)

	// reRoomRelation matches @Relation fields.
	// Captures: (parentColumn, entityColumn)
	reRoomRelation = regexp.MustCompile(
		`@Relation\s*\(\s*parentColumn\s*=\s*"([^"]+)"\s*,\s*entityColumn\s*=\s*"([^"]+)"`)

	// reRoomField matches constructor val/var params in data class body or primary constructor.
	// Captures: (field_name)
	reRoomField = regexp.MustCompile(
		`(?m)\bval\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*([A-Za-z][A-Za-z0-9_<>?,\s]*)`)

	// reRoomDatabase matches @Database class for migration version.
	reRoomDatabase = regexp.MustCompile(
		`@Database\s*\([^)]*version\s*=\s*(\d+)`)

	// reRoomMigration matches Migration(startVersion, endVersion) objects.
	reRoomMigration = regexp.MustCompile(
		`Migration\s*\(\s*(\d+)\s*,\s*(\d+)\s*\)`)
)

func (e *kotlinRoomSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_room_schema.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("orm", "room"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "@Entity") && !strings.Contains(src, "@Database") &&
		!strings.Contains(src, "Migration") && !strings.Contains(src, "@Relation") {
		return nil, nil
	}

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

	// Entity tables.
	for _, m := range reRoomEntity.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		// Check if a tableName is declared within the next 200 bytes (bounded).
		tableName := className
		end := m[0] + 200
		if end > len(src) {
			end = len(src)
		}
		if tn := reRoomTableName.FindStringSubmatch(src[m[0]:end]); tn != nil {
			tableName = tn[1]
		}
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, "kotlin", line)
		setProps(&ent, "orm", "room", "class_name", className, "provenance", "INFERRED_FROM_ROOM_ENTITY")
		add(ent)
	}

	// Foreign keys.
	for _, m := range reRoomForeignKey.FindAllStringSubmatchIndex(src, -1) {
		entityClass := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "fk -> " + entityClass
		ent := makeEntity(name, "SCOPE.Relationship", "foreign_key", file.Path, "kotlin", line)
		setProps(&ent, "orm", "room", "references", entityClass, "provenance", "INFERRED_FROM_ROOM_FOREIGN_KEY")
		add(ent)
	}

	// @Relation fields.
	for _, m := range reRoomRelation.FindAllStringSubmatchIndex(src, -1) {
		parentCol := src[m[2]:m[3]]
		entityCol := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "relation:" + parentCol + " -> " + entityCol
		ent := makeEntity(name, "SCOPE.Relationship", "association", file.Path, "kotlin", line)
		setProps(&ent, "orm", "room",
			"parent_column", parentCol,
			"entity_column", entityCol,
			"provenance", "INFERRED_FROM_ROOM_RELATION",
		)
		add(ent)
	}

	// @Database version → migration_parsing indicator.
	for _, m := range reRoomDatabase.FindAllStringSubmatchIndex(src, -1) {
		version := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "database:version:" + version
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "room", "db_version", version, "provenance", "INFERRED_FROM_ROOM_DATABASE")
		add(ent)
	}

	// Explicit Migration(from, to) objects.
	for _, m := range reRoomMigration.FindAllStringSubmatchIndex(src, -1) {
		from := src[m[2]:m[3]]
		to := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		name := "migration:" + from + "_to_" + to
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "room",
			"from_version", from,
			"to_version", to,
			"provenance", "INFERRED_FROM_ROOM_MIGRATION",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// ===========================================================================
// SQLDelight — .sq file schema extraction
// ===========================================================================

// kotlinSQLDelightSchemaExtractor emits schema entities from SQLDelight .sq files.
//
// Patterns in .sq files:
//
//	CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
//	FOREIGN KEY(user_id) REFERENCES users(id)
//	ALTER TABLE ... (migration)
type kotlinSQLDelightSchemaExtractor struct{}

func (e *kotlinSQLDelightSchemaExtractor) Language() string { return "custom_kotlin_sqldelight_schema" }

var (
	// reSQLDelightCreateTable matches CREATE TABLE declarations.
	// Captures: (table_name)
	reSQLDelightCreateTable = regexp.MustCompile(
		`(?im)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

	// reSQLDelightColumn matches column definitions inside CREATE TABLE.
	// Captures: (column_name, sql_type)
	reSQLDelightColumn = regexp.MustCompile(
		`(?im)^\s+([A-Za-z_][A-Za-z0-9_]*)\s+(INTEGER|TEXT|REAL|BLOB|BOOLEAN|DATETIME|VARCHAR[^,)]*|INT[^,)]*|NUMERIC[^,)*])\b`)

	// reSQLDelightForeignKey matches FOREIGN KEY constraints.
	// Captures: (local_col, referenced_table, referenced_col)
	reSQLDelightForeignKey = regexp.MustCompile(
		`(?im)FOREIGN\s+KEY\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)\s*REFERENCES\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)

	// reSQLDelightAlter matches ALTER TABLE for migration detection.
	// Captures: (table_name, operation)
	reSQLDelightAlter = regexp.MustCompile(
		`(?im)ALTER\s+TABLE\s+([A-Za-z_][A-Za-z0-9_]*)\s+(ADD\s+COLUMN|DROP\s+COLUMN|RENAME\s+TO|RENAME\s+COLUMN)`)

	// reSQLDelightMigration matches sqm migration file markers or version comments.
	reSQLDelightMigration = regexp.MustCompile(
		`(?m)--\s*(?:migration|version)\s*:?\s*(\d+)`)
)

// isSQLDelightFile returns true when the file is a SQLDelight source (.sq or .sqm)
// or a Kotlin file that imports SQLDelight.
func isSQLDelightFile(file extractor.FileInput) bool {
	if strings.HasSuffix(file.Path, ".sq") || strings.HasSuffix(file.Path, ".sqm") {
		return true
	}
	if file.Language != "kotlin" {
		return false
	}
	src := string(file.Content)
	return strings.Contains(src, "sqldelight") || strings.Contains(src, "SqlDelight") ||
		strings.Contains(src, "Database(") || strings.Contains(src, "CREATE TABLE")
}

func (e *kotlinSQLDelightSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_sqldelight_schema.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("orm", "sqldelight"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if !isSQLDelightFile(file) {
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

	// Tables.
	for _, m := range reSQLDelightCreateTable.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(tableName, "SCOPE.Schema", "table", file.Path, "kotlin", line)
		setProps(&ent, "orm", "sqldelight", "provenance", "INFERRED_FROM_SQLDELIGHT_CREATE_TABLE")
		add(ent)
	}

	// Columns.
	for _, m := range reSQLDelightColumn.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		colType := strings.TrimSpace(src[m[4]:m[5]])
		line := lineOf(src, m[0])
		ent := makeEntity(colName, "SCOPE.Schema", "column", file.Path, "kotlin", line)
		setProps(&ent, "orm", "sqldelight", "sql_type", colType, "provenance", "INFERRED_FROM_SQLDELIGHT_COLUMN")
		add(ent)
	}

	// Foreign keys.
	for _, m := range reSQLDelightForeignKey.FindAllStringSubmatchIndex(src, -1) {
		localCol := src[m[2]:m[3]]
		refTable := src[m[4]:m[5]]
		refCol := src[m[6]:m[7]]
		line := lineOf(src, m[0])
		name := localCol + " -> " + refTable + "." + refCol
		ent := makeEntity(name, "SCOPE.Relationship", "foreign_key", file.Path, "kotlin", line)
		setProps(&ent, "orm", "sqldelight",
			"local_column", localCol,
			"references", refTable,
			"referenced_column", refCol,
			"provenance", "INFERRED_FROM_SQLDELIGHT_FOREIGN_KEY",
		)
		add(ent)
	}

	// Migrations — ALTER TABLE.
	for _, m := range reSQLDelightAlter.FindAllStringSubmatchIndex(src, -1) {
		tableName := src[m[2]:m[3]]
		op := strings.ToUpper(strings.TrimSpace(src[m[4]:m[5]]))
		line := lineOf(src, m[0])
		name := "migration:alter:" + tableName + ":" + op
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "sqldelight",
			"table", tableName,
			"operation", op,
			"provenance", "INFERRED_FROM_SQLDELIGHT_ALTER",
		)
		add(ent)
	}

	// Version markers in .sqm / migration comments.
	for _, m := range reSQLDelightMigration.FindAllStringSubmatchIndex(src, -1) {
		version := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		name := "migration:version:" + version
		ent := makeEntity(name, "SCOPE.Schema", "migration", file.Path, "kotlin", line)
		setProps(&ent, "orm", "sqldelight", "version", version, "provenance", "INFERRED_FROM_SQLDELIGHT_MIGRATION_MARKER")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
