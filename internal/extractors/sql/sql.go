// Package sql implements the tree-sitter–based extractor for SQL source files.
//
// Extracted entities:
//   - CREATE TABLE        → Kind="SCOPE.Datastore", Subtype="table"
//   - column              → Kind="SCOPE.Schema",    Subtype="column" (one per column inside a CREATE TABLE)
//   - CREATE VIEW         → Kind="SCOPE.Datastore", Subtype="view"
//   - CREATE INDEX        → Kind="SCOPE.Datastore", Subtype="index"
//   - CREATE FUNCTION     → Kind="SCOPE.Datastore", Subtype="function"
//   - CREATE PROCEDURE    → Kind="SCOPE.Datastore", Subtype="procedure"
//   - RETURNS TRIGGER fn  → Kind="SCOPE.Datastore", Subtype="trigger_function"
//   - CREATE TRIGGER      → Kind="SCOPE.Datastore", Subtype="trigger"
//   - dbt {{ ref('model') }}    → Kind="SCOPE.Component",  Subtype="dbt_ref"
//   - dbt {{ source('s','t') }} → Kind="SCOPE.Datastore",  Subtype="dbt_source"
//   - dbt {{ config(...) }}     → Kind="SCOPE.Component",  Subtype="dbt_config"
//
// Relationships emitted:
//   - Table            → Column              : CONTAINS
//   - Column           → ReferencedTable     : REFERENCES (foreign key)
//   - Index            → Table              : INDEXES
//   - View             → Table              : READS_FROM   (Issue #389)
//   - Function         → Table              : READS_FROM   (Issue #389; SELECT in body)
//   - Function         → Table              : WRITES_TO    (Issue #389; INSERT/UPDATE/DELETE in body)
//   - Procedure        → Table              : READS_FROM / WRITES_TO (same as function)
//   - Trigger          → TriggerFunction    : FIRES        (Issue #1414)
//   - Trigger          → Table              : DEFINED_ON   (Issue #1414)
//   - TriggerFunction  → Table              : READS_FROM / WRITES_TO (body DML)
//
// Migration metadata (Issue #1275):
//
//	When a .sql file lives under a migrations/ directory, every emitted table
//	entity gets two extra Properties:
//	  - "migration_file"  = basename of the file (e.g. "0003_add_orders.sql")
//	  - "migration_order" = numeric prefix extracted from the filename, zero-padded
//	    to 8 chars for lexicographic stability (e.g. "00000003"). Used by the
//	    ORM-linker and topology endpoint to order schema evolution.
//
// dbt model files are SQL files containing Jinja templating. They are classified
// as "sql" by the file classifier and receive enhanced entity extraction here.
//
// Uses the sql grammar from smacker/go-tree-sitter.
// Falls back to regex extraction when no tree is available.
// Registers itself via init() and is imported by registry_gen.go.
package sql

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("sql", &Extractor{})
}

// Extractor implements extractor.Extractor for SQL.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "sql" }

// Regex patterns — mirrors Python SqlParser regexes.
var (
	tableRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w+)\s*\(`,
	)
	viewRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:MATERIALIZED\s+)?VIEW\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w+)\s+AS\b`,
	)
	indexRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:UNIQUE\s+)?(?:CONCURRENTLY\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+ON\s+(?:\w+\.)?(\w+)`,
	)
	// funcRE matches CREATE [OR REPLACE] FUNCTION (but NOT PROCEDURE — handled separately).
	funcRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:AGGREGATE\s+|FUNCTION\s+)(\w+)\s*\(`,
	)

	// procRE matches CREATE [OR REPLACE] PROCEDURE name(
	procRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?PROCEDURE\s+(?:\w+\.)?(\w+)\s*\(`,
	)

	// triggerFuncBodyRE detects "RETURNS TRIGGER" in a function body —
	// used to reclassify FUNCTION entities that are PostgreSQL trigger
	// handler functions (Subtype="trigger_function").
	triggerFuncBodyRE = regexp.MustCompile(`(?i)\bRETURNS\s+TRIGGER\b`)

	// triggerRE matches CREATE [CONSTRAINT] TRIGGER name ... ON table ...
	// EXECUTE {FUNCTION|PROCEDURE} func_name
	// Captures: (1) trigger_name, (2) event (BEFORE|AFTER|INSTEAD OF),
	//           (3) table_name, (4) func_name
	//
	// Issue #1708: PostgreSQL supports an optional column list on UPDATE
	// triggers — `UPDATE OF col[, col2]` — and column-list events may chain
	// with `OR INSERT|DELETE`. The original DML segment only handled the
	// `UPDATE [OR INSERT [OR DELETE]]` form (no `OF`). The .*? before `ON`
	// now non-greedily skips ANY tokens between the event keyword and the
	// `ON` clause, so column-list, FROM/REFERENCING, deferral, and WHEN
	// clauses no longer prevent extraction.
	triggerRE = regexp.MustCompile(
		`(?is)CREATE\s+(?:CONSTRAINT\s+)?TRIGGER\s+(\w+)\s+(?:(BEFORE|AFTER|INSTEAD\s+OF)\s+)?.*?\bON\s+(?:\w+\.)?(\w+)\b.*?EXECUTE\s+(?:FUNCTION|PROCEDURE)\s+(?:\w+\.)?(\w+)\s*\(`,
	)

	// dbt Jinja patterns.

	// {{ ref('model_name') }} or {{ ref("model_name") }}
	dbtRefRE = regexp.MustCompile(`\{\{-?\s*ref\s*\(\s*['"](\w+)['"]\s*\)\s*-?\}\}`)

	// {{ source('source_name', 'table_name') }} or double-quoted variants
	dbtSourceRE = regexp.MustCompile(`\{\{-?\s*source\s*\(\s*['"](\w+)['"]\s*,\s*['"](\w+)['"]\s*\)\s*-?\}\}`)

	// {{ config(materialized='table', ...) }} — capture first key=value or keyword arg
	dbtConfigRE = regexp.MustCompile(`\{\{-?\s*config\s*\(([^)]+)\)\s*-?\}\}`)

	// config key=value pairs inside {{ config(...) }}
	dbtConfigKeyRE = regexp.MustCompile(`(\w+)\s*=`)
)

// Extract uses regex-based extraction (tree-sitter SQL grammar node names vary widely
// by dialect; regex gives parity with the Python SqlParser).
//
// dbt model detection: if the file contains Jinja template markers ({{ ref(...)}}
// or {{ source(...) }} or {{ config(...) }}), dbt-specific entities are emitted in
// addition to any standard SQL entities.
//
// Migration metadata: if the file path contains a "migrations" path segment,
// table entities are annotated with migration_file and migration_order properties.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	entities := extractSQL(src, file.Path)
	if isDbtModel(src) {
		entities = append(entities, extractDbt(src, file.Path)...)
	}
	// Issue #1275: stamp migration metadata on table entities when the file
	// lives under a migrations directory.
	if isMigrationFile(file.Path) {
		base := filepath.Base(file.Path)
		order := migrationOrder(base)
		for i := range entities {
			if entities[i].Subtype != "table" {
				continue
			}
			if entities[i].Properties == nil {
				entities[i].Properties = make(map[string]string)
			}
			entities[i].Properties["migration_file"] = base
			entities[i].Properties["migration_order"] = order
		}
	}
	return entities, nil
}

// migrationFilePathRE matches any path component named "migrations", "migration",
// "migrate", or "db/migrate" (Rails convention). The check is case-insensitive.
var migrationDirRE = regexp.MustCompile(`(?i)(?:^|/)migrations?(?:/|$)|(?:^|/)db/migrate(?:/|$)`)

// isMigrationFile returns true when the file path indicates a SQL migration file.
func isMigrationFile(path string) bool {
	return migrationDirRE.MatchString(path)
}

// migrationOrderRE captures a leading numeric sequence (Django 4-digit, Rails
// timestamp, Flyway V<n>, or arbitrary integer prefix).
//
// Supported prefix forms:
//
//	0001_       Django-style (4+ digits)
//	20240501_   timestamp prefix
//	V1__        Flyway (V followed by integer)
//	1_          bare integer
var migrationOrderRE = regexp.MustCompile(`^(?:[Vv])?(\d+)[_.]`)

// migrationOrder extracts an 8-char zero-padded numeric sort key from a
// migration filename. Returns "00000000" when no prefix is found (sorts first
// so schema files without a prefix don't get lost).
func migrationOrder(basename string) string {
	m := migrationOrderRE.FindStringSubmatch(basename)
	if m == nil {
		return "00000000"
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return "00000000"
	}
	return fmt.Sprintf("%08d", n)
}

// isDbtModel returns true when the SQL file contains Jinja template markers
// characteristic of dbt model files.
func isDbtModel(src string) bool {
	return dbtRefRE.MatchString(src) || dbtSourceRE.MatchString(src) || dbtConfigRE.MatchString(src)
}

func extractSQL(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// Tables.
	for _, m := range tableRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "table:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[1]-1)

		// Body of the CREATE TABLE — between the opening "(" and matching ")".
		bodyStart := m[1] - 1
		bodyEnd := findBlockEndOffset(src, bodyStart)
		body := ""
		if bodyEnd > bodyStart && bodyEnd <= len(src) {
			body = src[bodyStart+1 : bodyEnd]
		}
		columns, fks := parseTableBody(body, name, filePath, startLine)

		// Issue #141: emit a structural-ref ToID (Format B) qualified by
		// file + table so column-name CONTAINS edges no longer collide with
		// same-named operations in other languages (e.g. Java methods named
		// "name"). Resolved through Index.byMember[file][table][column];
		// columns are emitted with Name="<table>.<column>" so the dotted-
		// name split during BuildIndex populates that bucket.
		var rels []types.RelationshipRecord
		for _, col := range columns {
			shortName := col.Properties["column"]
			rels = append(rels, types.RelationshipRecord{
				FromID: name,
				ToID:   extractor.BuildSchemaColumnStructuralRef(filePath, name, shortName),
				Kind:   "CONTAINS",
				Properties: map[string]string{
					"contained_kind": "column",
				},
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "table",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE TABLE %s", name),
			EnrichmentRequired: false,
			Relationships:      rels,
		})

		// Column entities, each carrying FOREIGN_KEY REFERENCES edges if any.
		// Issue #141: FromID uses the same Format B structural-ref as the
		// table→column CONTAINS edge so the column collision class is
		// removed from REFERENCES too. fk.FromColumn is the bare column
		// identifier parsed from the FK declaration; match it against
		// Properties["column"] (the short name) since col.Name is now the
		// qualified "<table>.<column>".
		for _, col := range columns {
			var colRels []types.RelationshipRecord
			shortName := col.Properties["column"]
			for _, fk := range fks {
				if fk.FromColumn != shortName {
					continue
				}
				colRels = append(colRels, types.RelationshipRecord{
					FromID: extractor.BuildSchemaColumnStructuralRef(filePath, name, shortName),
					ToID:   fk.ToTable,
					Kind:   "REFERENCES",
					Properties: map[string]string{
						"reference_kind": "foreign_key",
						"to_column":      fk.ToColumn,
						"from_table":     name,
					},
				})
			}
			ent := col
			ent.Relationships = colRels
			entities = append(entities, ent)
		}
	}

	// ALTER TABLE ... ADD [CONSTRAINT ...] FOREIGN KEY (...)
	//
	// Real migration suites split FK declarations into ALTER statements rather
	// than inlining them in CREATE TABLE bodies. We synthesize Column→Table
	// REFERENCES edges identical to the inline-FK shape so downstream consumers
	// don't need to handle two shapes. When a column entity for (table, col)
	// already exists from a prior CREATE TABLE pass in this file, the edge is
	// attached to it; otherwise a stand-alone column entity is emitted carrying
	// the edge (mirroring the inline-FK column shape).
	for _, m := range alterTableFkRE.FindAllStringSubmatchIndex(src, -1) {
		fromTable := src[m[2]:m[3]]
		fromColsRaw := src[m[4]:m[5]]
		toTable := src[m[6]:m[7]]
		toColsRaw := src[m[8]:m[9]]
		fromCols := splitAndTrim(fromColsRaw, ",")
		toCols := splitAndTrim(toColsRaw, ",")
		startLine := strings.Count(src[:m[0]], "\n") + 1

		for i, fc := range fromCols {
			tc := ""
			if i < len(toCols) {
				tc = toCols[i]
			}
			// Issue #141: FromID is a Format B structural-ref so the
			// column-name collision class disappears for ALTER-TABLE FK
			// emissions too.
			rel := types.RelationshipRecord{
				FromID: extractor.BuildSchemaColumnStructuralRef(filePath, fromTable, fc),
				ToID:   toTable,
				Kind:   "REFERENCES",
				Properties: map[string]string{
					"reference_kind": "foreign_key",
					"to_column":      tc,
					"from_table":     fromTable,
				},
			}

			// Attach to an existing column entity if one was emitted for this
			// (table, column) pair. Otherwise emit a synthetic column entity.
			// Match on Properties["column"] (short name) since e.Name is now
			// the qualified "<table>.<column>".
			attached := false
			for j := range entities {
				e := &entities[j]
				if e.Subtype != "column" {
					continue
				}
				if e.Properties == nil || e.Properties["table"] != fromTable || e.Properties["column"] != fc {
					continue
				}
				e.Relationships = append(e.Relationships, rel)
				attached = true
				break
			}
			if attached {
				continue
			}

			altKey := "alter_fk_col:" + fromTable + "." + fc
			if seen[altKey] {
				continue
			}
			seen[altKey] = true
			qualName := fromTable + "." + fc
			entities = append(entities, types.EntityRecord{
				Name:               qualName,
				Kind:               "SCOPE.Schema",
				Subtype:            "column",
				SourceFile:         filePath,
				Language:           "sql",
				StartLine:          startLine,
				EndLine:            startLine,
				QualifiedName:      qualName,
				Signature:          fmt.Sprintf("ALTER TABLE %s ADD FOREIGN KEY (%s) REFERENCES %s(%s)", fromTable, fc, toTable, tc),
				EnrichmentRequired: false,
				Properties: map[string]string{
					"table":  fromTable,
					"column": fc,
				},
				Relationships: []types.RelationshipRecord{rel},
			})
		}
	}

	// Views.
	for _, m := range viewRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "view:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findStmtEnd(src, m[0])

		// Issue #389: capture the view body (text from end of header up to
		// the next ';') and emit READS_FROM edges to every table referenced
		// in FROM / JOIN clauses. Writes are not expected in a CREATE VIEW
		// body but we run the full detector for symmetry — INSERT/UPDATE/
		// DELETE inside a view body would still surface a real dependency.
		body := sliceStmtBody(src, m[1])
		reads, writes := extractDMLTargets(body)
		var rels []types.RelationshipRecord
		for _, t := range reads {
			if t == name {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     name,
				ToID:       t,
				Kind:       "READS_FROM",
				Properties: map[string]string{"dml": "select"},
			})
		}
		for _, t := range writes {
			if t == name {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     name,
				ToID:       t,
				Kind:       "WRITES_TO",
				Properties: map[string]string{"dml": "write"},
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "view",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE VIEW %s", name),
			EnrichmentRequired: false,
			Relationships:      rels,
		})
	}

	// Indexes.
	for _, m := range indexRE.FindAllStringSubmatchIndex(src, -1) {
		indexName := src[m[2]:m[3]]
		tableName := src[m[4]:m[5]]
		key := "index:" + indexName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findStmtEnd(src, m[0])
		entities = append(entities, types.EntityRecord{
			Name:               indexName,
			Kind:               "SCOPE.Datastore",
			Subtype:            "index",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE INDEX %s ON %s", indexName, tableName),
			EnrichmentRequired: false,
			Relationships: []types.RelationshipRecord{
				{
					FromID: indexName,
					ToID:   tableName,
					Kind:   "INDEXES",
					Properties: map[string]string{
						"reference_kind": "index_on",
					},
				},
			},
		})
	}

	// Functions (including trigger-handler functions reclassified to trigger_function).
	//
	// Issue #1414: a FUNCTION whose body contains "RETURNS TRIGGER" is a
	// PostgreSQL trigger handler. Emit it with Subtype="trigger_function" so
	// downstream graph queries can distinguish it from plain functions and so
	// TRIGGER entities can reference it via a typed FIRES edge.
	for _, m := range funcRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "function:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[1]-1)

		// Issue #389: scan the function/procedure body for DML targets.
		// PostgreSQL functions are dollar-quoted ($$ ... $$); other dialects
		// (MySQL, MSSQL) use BEGIN ... END or just a SELECT after AS. We
		// scan from the open-paren of the function header down to the end
		// of the statement, which is permissive enough for all three.
		body := sliceFuncBody(src, m[1])
		reads, writes := extractDMLTargets(body)
		var rels []types.RelationshipRecord
		for _, t := range reads {
			if t == name {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     name,
				ToID:       t,
				Kind:       "READS_FROM",
				Properties: map[string]string{"dml": "select"},
			})
		}
		for _, t := range writes {
			if t == name {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     name,
				ToID:       t,
				Kind:       "WRITES_TO",
				Properties: map[string]string{"dml": "write"},
			})
		}

		// Determine whether this is a trigger handler function.
		// RETURNS TRIGGER appears between the parameter list close-paren and
		// the AS $$ body start. We build a "declaration region" that covers
		// from the CREATE keyword through the first $$ (or first semicolon for
		// non-dollar-quoted bodies) so the triggerFuncBodyRE can find it.
		subtype := "function"
		signature := fmt.Sprintf("CREATE FUNCTION %s", name)
		declRegion := sliceFuncDecl(src, m[0], m[1])
		if triggerFuncBodyRE.MatchString(declRegion) {
			subtype = "trigger_function"
			signature = fmt.Sprintf("CREATE FUNCTION %s RETURNS TRIGGER", name)
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          signature,
			EnrichmentRequired: false,
			Relationships:      rels,
		})
	}

	// Procedures (Issue #1414): CREATE [OR REPLACE] PROCEDURE name(...)
	// Distinct from FUNCTION — emitted with Subtype="procedure" and a
	// correct "CREATE PROCEDURE" signature.
	for _, m := range procRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "procedure:" + name
		if seen[key] {
			continue
		}
		// Also guard against a name that was already claimed by the funcRE pass
		// (shouldn't happen given the split regex, but be defensive).
		if seen["function:"+name] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[1]-1)

		body := sliceFuncBody(src, m[1])
		reads, writes := extractDMLTargets(body)
		var rels []types.RelationshipRecord
		for _, t := range reads {
			if t == name {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     name,
				ToID:       t,
				Kind:       "READS_FROM",
				Properties: map[string]string{"dml": "select"},
			})
		}
		for _, t := range writes {
			if t == name {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				FromID:     name,
				ToID:       t,
				Kind:       "WRITES_TO",
				Properties: map[string]string{"dml": "write"},
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "procedure",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE PROCEDURE %s", name),
			EnrichmentRequired: false,
			Relationships:      rels,
		})
	}

	// Triggers (Issue #1414): CREATE [CONSTRAINT] TRIGGER name ... ON table
	// EXECUTE FUNCTION|PROCEDURE func_name(...)
	//
	// Emits two edges from the trigger entity:
	//   - FIRES      → trigger function (the EXECUTE FUNCTION target)
	//   - DEFINED_ON → table            (the ON <table> target)
	for _, m := range triggerRE.FindAllStringSubmatchIndex(src, -1) {
		triggerName := src[m[2]:m[3]]
		// m[4]:m[5] is the event (BEFORE/AFTER/INSTEAD OF) — optional
		tableName := src[m[6]:m[7]]
		funcName := src[m[8]:m[9]]

		key := "trigger:" + triggerName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findStmtEnd(src, m[0])

		rels := []types.RelationshipRecord{
			{
				FromID:     triggerName,
				ToID:       funcName,
				Kind:       "FIRES",
				Properties: map[string]string{"trigger_target": "function"},
			},
			{
				FromID:     triggerName,
				ToID:       tableName,
				Kind:       "DEFINED_ON",
				Properties: map[string]string{"trigger_table": tableName},
			},
		}

		entities = append(entities, types.EntityRecord{
			Name:               triggerName,
			Kind:               "SCOPE.Datastore",
			Subtype:            "trigger",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE TRIGGER %s ON %s EXECUTE FUNCTION %s", triggerName, tableName, funcName),
			EnrichmentRequired: false,
			Relationships:      rels,
		})
	}

	return entities
}

// extractDbt extracts dbt-specific entities from a SQL file containing Jinja.
//
// Emitted entity kinds:
//   - {{ ref('model') }}          → SCOPE.Component / dbt_ref
//   - {{ source('src','tbl') }}   → SCOPE.Datastore / dbt_source
//   - {{ config(key=val, ...) }}  → SCOPE.Component / dbt_config (one per config key)
func extractDbt(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// ref() — model dependency references.
	for _, m := range dbtRefRE.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		key := "dbt_ref:" + modelName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:          modelName,
			Kind:          "SCOPE.Component",
			Subtype:       "dbt_ref",
			QualifiedName: "dbt/ref/" + modelName,
			SourceFile:    filePath,
			Language:      "sql",
			StartLine:     startLine,
			EndLine:       startLine,
			Signature:     fmt.Sprintf("ref('%s')", modelName),
			QualityScore:  0.75,
		})
	}

	// source() — source table references.
	for _, m := range dbtSourceRE.FindAllStringSubmatchIndex(src, -1) {
		sourceName := src[m[2]:m[3]]
		tableName := src[m[4]:m[5]]
		qualName := sourceName + "." + tableName
		key := "dbt_source:" + qualName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:          qualName,
			Kind:          "SCOPE.Datastore",
			Subtype:       "dbt_source",
			QualifiedName: "dbt/source/" + qualName,
			SourceFile:    filePath,
			Language:      "sql",
			StartLine:     startLine,
			EndLine:       startLine,
			Signature:     fmt.Sprintf("source('%s', '%s')", sourceName, tableName),
			QualityScore:  0.75,
		})
	}

	// config() — configuration block keys.
	for _, m := range dbtConfigRE.FindAllStringSubmatchIndex(src, -1) {
		configBody := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1

		// Extract individual key names from the config body.
		seenKeys := make(map[string]bool)
		for _, km := range dbtConfigKeyRE.FindAllStringSubmatch(configBody, -1) {
			keyName := km[1]
			if seenKeys[keyName] {
				continue
			}
			seenKeys[keyName] = true
			globalKey := "dbt_config:" + keyName
			if seen[globalKey] {
				continue
			}
			seen[globalKey] = true
			entities = append(entities, types.EntityRecord{
				Name:          keyName,
				Kind:          "SCOPE.Component",
				Subtype:       "dbt_config",
				QualifiedName: "dbt/config/" + keyName,
				SourceFile:    filePath,
				Language:      "sql",
				StartLine:     startLine,
				EndLine:       startLine,
				Signature:     fmt.Sprintf("config(%s=...)", keyName),
				QualityScore:  0.65,
			})
		}
	}

	return entities
}

// findBlockEnd returns the line number of the closing ) for a CREATE TABLE body.
func findBlockEnd(src string, openPos int) int {
	depth := 0
	for i, ch := range src[openPos:] {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.Count(src[:openPos+i], "\n") + 1
			}
		}
	}
	return strings.Count(src, "\n") + 1
}

// findBlockEndOffset returns the byte offset of the matching ')' for the '(' at openPos.
// Returns -1 if no match is found.
func findBlockEndOffset(src string, openPos int) int {
	if openPos < 0 || openPos >= len(src) || src[openPos] != '(' {
		return -1
	}
	depth := 0
	for i := openPos; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// columnEntry is a parsed column definition from a CREATE TABLE body.
// It is converted into a SCOPE.Schema column entity by the caller.
type columnEntry = types.EntityRecord

// fkEntry is a parsed foreign-key declaration (either inline column-level
// REFERENCES or table-level FOREIGN KEY (col) REFERENCES other(col)).
type fkEntry struct {
	FromColumn string
	ToTable    string
	ToColumn   string
}

var (
	// Inline column-level FK: "user_id INTEGER REFERENCES users(id)"
	colInlineFkRE = regexp.MustCompile(`(?i)REFERENCES\s+(?:\w+\.)?(\w+)\s*\(\s*(\w+)\s*\)`)

	// Table-level FK constraint:
	//   "FOREIGN KEY (user_id) REFERENCES users(id)"
	//   "CONSTRAINT fk_a FOREIGN KEY (a, b) REFERENCES other(x, y)"
	tableLevelFkRE = regexp.MustCompile(`(?i)FOREIGN\s+KEY\s*\(\s*([^)]+?)\s*\)\s+REFERENCES\s+(?:\w+\.)?(\w+)\s*\(\s*([^)]+?)\s*\)`)

	// ALTER TABLE ... ADD [CONSTRAINT name] FOREIGN KEY (cols) REFERENCES tbl(cols) [ON DELETE/UPDATE ...]
	// Matches both forms (with and without explicit CONSTRAINT name) across newlines.
	alterTableFkRE = regexp.MustCompile(
		`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?(?:\w+\.)?(\w+)\s+ADD\s+(?:CONSTRAINT\s+\w+\s+)?FOREIGN\s+KEY\s*\(\s*([^)]+?)\s*\)\s+REFERENCES\s+(?:\w+\.)?(\w+)\s*\(\s*([^)]+?)\s*\)`,
	)

	// Identifier-then-rest: column lines start with an identifier (or quoted ident)
	// at the top level — used to filter constraint clauses.
	columnIdentRE = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\b`)

	// Issue #389 (PORT-RELS-SQL): DML target detection inside view / function
	// bodies. We attach READS_FROM / WRITES_TO edges from the surrounding
	// scope entity (view or function) to each referenced table.
	//
	// Patterns are intentionally permissive — schema-qualified names are
	// allowed; quoted identifiers fall through (rare in views/functions and
	// covered by sibling extractors).
	dmlFromRE   = regexp.MustCompile(`(?is)\b(?:FROM|JOIN)\s+(?:\w+\.)?(\w+)`)
	dmlInsertRE = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+(?:\w+\.)?(\w+)`)
	dmlUpdateRE = regexp.MustCompile(`(?is)\bUPDATE\s+(?:ONLY\s+)?(?:\w+\.)?(\w+)`)
	dmlDeleteRE = regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+(?:ONLY\s+)?(?:\w+\.)?(\w+)`)
)

// dmlReservedWord matches identifiers that the FROM/JOIN regex can pick up
// when SQL syntax follows the keyword with another keyword instead of a
// table name (e.g. "DELETE FROM ONLY table"). Filter these out.
var dmlReservedWords = map[string]bool{
	"ONLY":   true,
	"SELECT": true,
	"WHERE":  true,
	"WITH":   true,
	"AS":     true,
}

// extractDMLTargets scans a SQL body (view body or function body) for table
// references and returns deduplicated read and write target lists. Each
// returned slice preserves first-seen order so emitted edges are stable.
func extractDMLTargets(body string) (reads, writes []string) {
	seenR := map[string]bool{}
	seenW := map[string]bool{}
	add := func(target string, m map[string]bool, out *[]string) {
		t := strings.TrimSpace(target)
		if t == "" {
			return
		}
		upper := strings.ToUpper(t)
		if dmlReservedWords[upper] {
			return
		}
		if m[t] {
			return
		}
		m[t] = true
		*out = append(*out, t)
	}
	for _, m := range dmlFromRE.FindAllStringSubmatch(body, -1) {
		add(m[1], seenR, &reads)
	}
	for _, m := range dmlInsertRE.FindAllStringSubmatch(body, -1) {
		add(m[1], seenW, &writes)
	}
	for _, m := range dmlUpdateRE.FindAllStringSubmatch(body, -1) {
		add(m[1], seenW, &writes)
	}
	for _, m := range dmlDeleteRE.FindAllStringSubmatch(body, -1) {
		add(m[1], seenW, &writes)
	}
	return reads, writes
}

// parseTableBody parses a CREATE TABLE body (text between '(' and ')') into
// column entities and a list of foreign-key relationships. Columns named
// "PRIMARY", "FOREIGN", "CONSTRAINT", "UNIQUE", "CHECK", "INDEX", "KEY" are
// treated as table-level constraints, not columns.
func parseTableBody(body, tableName, filePath string, tableStartLine int) ([]columnEntry, []fkEntry) {
	var cols []columnEntry
	var fks []fkEntry

	// Split top-level by commas (depth-aware: don't split inside parens).
	entries := splitTopLevelCommas(body)
	lineCursor := tableStartLine
	consumed := 0

	for _, raw := range entries {
		// Track approximate line of this entry.
		entryStartLine := lineCursor + strings.Count(body[:consumed], "\n")
		consumed += len(raw) + 1 // +1 for the comma we removed (best-effort)

		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		upper := strings.ToUpper(entry)

		// Table-level FK constraint
		if strings.HasPrefix(upper, "FOREIGN KEY") || strings.Contains(upper, "FOREIGN KEY") && strings.HasPrefix(upper, "CONSTRAINT") {
			if m := tableLevelFkRE.FindStringSubmatch(entry); m != nil {
				fromCols := splitAndTrim(m[1], ",")
				toTable := m[2]
				toCols := splitAndTrim(m[3], ",")
				for i, fc := range fromCols {
					tc := ""
					if i < len(toCols) {
						tc = toCols[i]
					}
					fks = append(fks, fkEntry{FromColumn: fc, ToTable: toTable, ToColumn: tc})
				}
			}
			continue
		}

		// Skip pure constraint clauses
		if isConstraintClause(upper) {
			continue
		}

		// Pull the column identifier
		idMatch := columnIdentRE.FindStringSubmatch(entry)
		if idMatch == nil {
			continue
		}
		colName := idMatch[1]

		// Skip when the leading identifier is a SQL constraint keyword.
		if isReservedColumnKeyword(strings.ToUpper(colName)) {
			continue
		}

		// Inline FK on this column
		if m := colInlineFkRE.FindStringSubmatch(entry); m != nil {
			fks = append(fks, fkEntry{
				FromColumn: colName,
				ToTable:    m[1],
				ToColumn:   m[2],
			})
		}

		// Issue #141: column entity Name is qualified as "<table>.<column>"
		// so EntityRecord.ComputeID (which hashes file+kind+name) produces
		// distinct IDs for same-named columns on sibling tables in a single
		// SQL file. The dotted Name also drives Index.byMember population
		// (last-dot split → scope=table, member=column), enabling Format B
		// structural-ref resolution for CONTAINS / REFERENCES edges. The
		// short column name is preserved in Properties["column"] for
		// downstream consumers that need it without re-parsing.
		qualName := tableName + "." + colName
		colEntity := types.EntityRecord{
			Name:               qualName,
			Kind:               "SCOPE.Schema",
			Subtype:            "column",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          entryStartLine,
			EndLine:            entryStartLine,
			QualifiedName:      qualName,
			Signature:          strings.TrimSpace(entry),
			EnrichmentRequired: false,
			Properties: map[string]string{
				"table":  tableName,
				"column": colName,
			},
		}
		cols = append(cols, colEntity)
	}

	return cols, fks
}

// splitTopLevelCommas splits s on commas that are not inside a parenthesized
// group. This is the safe way to split CREATE TABLE column definitions.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// splitAndTrim splits s on sep and trims whitespace from each part.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isConstraintClause returns true when an entry (uppercased) is a top-level
// constraint clause rather than a column definition.
func isConstraintClause(upper string) bool {
	prefixes := []string{
		"PRIMARY KEY",
		"UNIQUE",
		"CHECK",
		"CONSTRAINT",
		"INDEX",
		"KEY ",
		"EXCLUDE",
		"LIKE ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

// isReservedColumnKeyword returns true when an identifier we parsed as a
// column name is actually a SQL constraint keyword.
func isReservedColumnKeyword(upper string) bool {
	switch upper {
	case "PRIMARY", "FOREIGN", "CONSTRAINT", "UNIQUE", "CHECK", "INDEX", "KEY", "EXCLUDE", "LIKE":
		return true
	}
	return false
}

// sliceStmtBody returns the substring of src from headerEnd up to the next
// top-level ';' (or end of file). Used by the VIEW emitter to capture the
// SELECT body that follows "CREATE VIEW name AS".
func sliceStmtBody(src string, headerEnd int) string {
	if headerEnd < 0 || headerEnd >= len(src) {
		return ""
	}
	idx := strings.Index(src[headerEnd:], ";")
	if idx < 0 {
		return src[headerEnd:]
	}
	return src[headerEnd : headerEnd+idx]
}

// sliceFuncBody returns the body of a function/procedure declaration. The
// body region runs from the parameter list onward to the end of the next
// trailing-statement terminator. For dollar-quoted bodies ($$ ... $$) we
// take the content between the two $$ markers; otherwise we fall back to
// sliceStmtBody.
func sliceFuncBody(src string, headerEnd int) string {
	if headerEnd < 0 || headerEnd >= len(src) {
		return ""
	}
	// Skip past the parameter list of the function header so DML scanning
	// does not pick up "FROM" inside e.g. parameter defaults or column types.
	paramEnd := findBlockEndOffset(src, headerEnd-1)
	scanFrom := headerEnd
	if paramEnd > 0 && paramEnd < len(src) {
		scanFrom = paramEnd + 1
	}
	rest := src[scanFrom:]
	// Dollar-quoted body: content between the first two "$$" markers.
	if i := strings.Index(rest, "$$"); i >= 0 {
		j := strings.Index(rest[i+2:], "$$")
		if j >= 0 {
			return rest[i+2 : i+2+j]
		}
	}
	return sliceStmtBody(src, scanFrom)
}

// findStmtEnd returns the line number of the next semicolon after startPos.
func findStmtEnd(src string, startPos int) int {
	idx := strings.Index(src[startPos:], ";")
	if idx < 0 {
		return strings.Count(src, "\n") + 1
	}
	return strings.Count(src[:startPos+idx], "\n") + 1
}

// sliceFuncDecl returns the "declaration region" of a CREATE FUNCTION statement:
// the text from createStart up to (but not including) the start of the $$ body
// or the first semicolon.
//
// This region contains the function header including the RETURNS clause (e.g.
// "RETURNS TRIGGER") that sits between the parameter list close-paren and the
// body marker. It is used by the trigger_function reclassification to find
// "RETURNS TRIGGER" without scanning the full dollar-quoted body.
//
// createStart is the byte offset of "CREATE" in src.
// headerEnd is the byte offset immediately AFTER the opening "(" of the
// parameter list (i.e. m[1] from the funcRE match).
func sliceFuncDecl(src string, createStart, headerEnd int) string {
	if createStart < 0 || headerEnd < 0 || headerEnd > len(src) {
		return ""
	}
	// Find the close-paren of the parameter list.
	paramCloseIdx := findBlockEndOffset(src, headerEnd-1)
	scanFrom := headerEnd
	if paramCloseIdx > 0 && paramCloseIdx < len(src) {
		scanFrom = paramCloseIdx + 1
	}
	if scanFrom >= len(src) {
		return src[createStart:]
	}
	rest := src[scanFrom:]
	// Stop at the first $$ (dollar-quote body start).
	if i := strings.Index(rest, "$$"); i >= 0 {
		return src[createStart : scanFrom+i]
	}
	// No dollar-quote — stop at the first semicolon.
	if i := strings.Index(rest, ";"); i >= 0 {
		return src[createStart : scanFrom+i]
	}
	return src[createStart:]
}
