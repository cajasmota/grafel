package java

// jooq.go — custom extractor for jOOQ generated Table/Record/Keys classes.
//
// jOOQ is a type-safe SQL DSL for Java. Its code generator produces:
//
//   - Table<R> subclasses (one per DB table) with TableField<> declarations
//     for every column. The generated class is placed under
//     src/generated/jooq/ (or a configured equivalent).
//   - Keys.java listing UniqueKey<> and ForeignKey<> declarations connecting
//     table records via static final fields.
//
// This extractor operates purely on the generated code because runtime
// DSLContext queries (create.selectFrom(...)) do not carry schema
// information — the schema is only available in the generated types.
//
// Extracted entities
// ------------------
//   SCOPE.Schema / table        — one per Table<R> subclass
//   SCOPE.Schema / column       — one per TableField<> declaration
//   SCOPE.Component / foreign_key — one per ForeignKey<> declaration in Keys.java
//   SCOPE.Component / relation   — one per ForeignKey<> (source→target table pair)
//
// Gate signals
// ------------
//   - import org.jooq.*
//   - extends TableImpl<
//   - public static final ForeignKey<   (Keys.java)
//   - DSLContext / DSL.using / create.selectFrom  (query files — not schema)
//
// Capability cells (issue #3098)
// --------------------------------
//   schema_extraction        — partial (generated Table/Column scan)
//   association_extraction   — partial (ForeignKey declarations in Keys.java)
//   foreign_key_extraction   — partial (ForeignKey declarations)
//   relationship_extraction  — partial (source→target table pairs from ForeignKey)
//   migration_parsing        — not_applicable (jOOQ is a query DSL; no migration files)
//   lazy_loading_recognition — not_applicable (jOOQ is a query DSL; no lazy-loading concept)

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_java_jooq", &jooqExtractor{})
}

type jooqExtractor struct{}

func (e *jooqExtractor) Language() string { return "custom_java_jooq" }

var (
	// Gate: jOOQ import or generated-class marker.
	jooqMarkerRE = regexp.MustCompile(
		`import\s+org\.jooq\.|extends\s+TableImpl\s*<|DSLContext\b|DSL\.using\s*\(|create\.selectFrom\s*\(`)

	// Table class: "public class FooTable extends TableImpl<FooRecord>"
	// or           "public class Foo extends TableImpl<FooRecord>"
	jooqTableClassRE = regexp.MustCompile(
		`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+TableImpl\s*<(\w+)>`)

	// TableField declaration inside a Table<R> subclass:
	// "public final TableField<FooRecord, String> COLUMN_NAME = ..."
	jooqTableFieldRE = regexp.MustCompile(
		`public\s+final\s+TableField\s*<\s*\w+\s*,\s*\w+\s*>\s+(\w+)\s*=`)

	// ForeignKey in Keys.java:
	// "public static final ForeignKey<ChildRecord, ParentRecord> FK_NAME = ..."
	jooqForeignKeyRE = regexp.MustCompile(
		`public\s+static\s+final\s+ForeignKey\s*<\s*(\w+)\s*,\s*(\w+)\s*>\s+(\w+)\s*=`)

	// createForeignKey call to extract FK names from Internal.createForeignKey:
	// Internal.createForeignKey(Table.TABLE, DSL.name("FK_NAME"), ...)
	jooqCreateFKNameRE = regexp.MustCompile(
		`\.createForeignKey\s*\([^,]+,\s*DSL\.name\s*\(\s*"([^"]+)"`)

	// UniqueKey declaration:
	// "public static final UniqueKey<FooRecord> KEY_FOO_PRIMARY = ..."
	jooqUniqueKeyRE = regexp.MustCompile(
		`public\s+static\s+final\s+UniqueKey\s*<\s*(\w+)\s*>\s+(\w+)\s*=`)
)

func (e *jooqExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	if strings.ToLower(file.Language) != "java" {
		return nil, nil
	}
	src := string(file.Content)
	if !jooqMarkerRE.MatchString(src) {
		return nil, nil
	}
	fp := file.Path

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

	// --- Table classes (schema_extraction) ---
	type tableInfo struct {
		name   string
		record string // e.g. "CustomerRecord"
		offset int
	}
	var tables []tableInfo

	for _, m := range jooqTableClassRE.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		recordType := src[m[4]:m[5]]
		ent := makeEntity(className, "SCOPE.Schema", "table", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jooq",
			"record_type", recordType,
			"provenance", "INFERRED_FROM_JOOQ_TABLE_CLASS")
		add(ent)
		tables = append(tables, tableInfo{name: className, record: recordType, offset: m[0]})
	}

	// owningTable returns the table class name whose declaration precedes offset.
	owningTable := func(offset int) string {
		var best string
		for _, t := range tables {
			if t.offset <= offset {
				best = t.name
			}
		}
		return best
	}

	// --- TableField declarations (schema_extraction — columns) ---
	for _, m := range jooqTableFieldRE.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		owner := owningTable(m[0])
		name := fieldName
		if owner != "" {
			name = owner + "." + fieldName
		}
		ent := makeEntity(name, "SCOPE.Schema", "column", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jooq",
			"field_name", fieldName,
			"owner_table", owner,
			"provenance", "INFERRED_FROM_JOOQ_TABLE_FIELD")
		add(ent)
	}

	// --- ForeignKey declarations (foreign_key_extraction + association_extraction) ---
	for _, m := range jooqForeignKeyRE.FindAllStringSubmatchIndex(src, -1) {
		childRecord := src[m[2]:m[3]]
		parentRecord := src[m[4]:m[5]]
		fkName := src[m[6]:m[7]]

		// Try to extract the constraint name from the createForeignKey call.
		constraintName := ""
		if nm := jooqCreateFKNameRE.FindStringSubmatchIndex(src[m[0]:]); nm != nil {
			constraintName = src[m[0]+nm[2] : m[0]+nm[3]]
		}
		if constraintName == "" {
			constraintName = fkName
		}

		// foreign_key entity (foreign_key_extraction).
		fkEnt := makeEntity(constraintName, "SCOPE.Component", "foreign_key", fp, file.Language, lineOf(src, m[0]))
		setProps(&fkEnt, "framework", "jooq",
			"constraint_name", constraintName,
			"child_record", childRecord,
			"parent_record", parentRecord,
			"provenance", "INFERRED_FROM_JOOQ_FOREIGN_KEY")
		add(fkEnt)

		// relation entity (association_extraction + relationship_extraction).
		relName := childRecord + "->" + parentRecord
		relEnt := makeEntity(relName, "SCOPE.Component", "relation", fp, file.Language, lineOf(src, m[0]))
		setProps(&relEnt, "framework", "jooq",
			"relation_type", "ForeignKey",
			"source_record", childRecord,
			"target_record", parentRecord,
			"constraint_name", constraintName,
			"provenance", "INFERRED_FROM_JOOQ_FK_RELATION")
		add(relEnt)
	}

	// --- UniqueKey declarations (schema_extraction — primary/unique constraints) ---
	for _, m := range jooqUniqueKeyRE.FindAllStringSubmatchIndex(src, -1) {
		recordType := src[m[2]:m[3]]
		keyName := src[m[4]:m[5]]
		ent := makeEntity(keyName, "SCOPE.Component", "unique_key", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "jooq",
			"record_type", recordType,
			"provenance", "INFERRED_FROM_JOOQ_UNIQUE_KEY")
		add(ent)
	}

	return entities, nil
}
