// Package sql implements the tree-sitter–based extractor for SQL source files.
//
// Extracted entities:
//   - CREATE TABLE  → Kind="SCOPE.Datastore", Subtype="table"
//   - column        → Kind="SCOPE.Schema",    Subtype="column" (one per column inside a CREATE TABLE)
//   - CREATE VIEW   → Kind="SCOPE.Datastore", Subtype="view"
//   - CREATE INDEX  → Kind="SCOPE.Datastore", Subtype="index"
//   - dbt {{ ref('model') }}    → Kind="SCOPE.Component",  Subtype="dbt_ref"
//   - dbt {{ source('s','t') }} → Kind="SCOPE.Datastore",  Subtype="dbt_source"
//   - dbt {{ config(...) }}     → Kind="SCOPE.Component",  Subtype="dbt_config"
//
// Relationships emitted:
//   - Table  → Column        : CONTAINS
//   - Column → ReferencedTable : REFERENCES (foreign key)
//   - Index  → Table         : INDEXES
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
	"regexp"
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
	funcRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:AGGREGATE\s+|PROCEDURE\s+|FUNCTION\s+)(\w+)\s*\(`,
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
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	entities := extractSQL(src, file.Path)
	if isDbtModel(src) {
		entities = append(entities, extractDbt(src, file.Path)...)
	}
	return entities, nil
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

		var rels []types.RelationshipRecord
		for _, col := range columns {
			rels = append(rels, types.RelationshipRecord{
				FromID: name,
				ToID:   col.Name,
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
		for _, col := range columns {
			var colRels []types.RelationshipRecord
			for _, fk := range fks {
				if fk.FromColumn != col.Name {
					continue
				}
				colRels = append(colRels, types.RelationshipRecord{
					FromID: col.Name,
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
			rel := types.RelationshipRecord{
				FromID: fc,
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
			attached := false
			for j := range entities {
				e := &entities[j]
				if e.Subtype != "column" || e.Name != fc {
					continue
				}
				if e.Properties == nil || e.Properties["table"] != fromTable {
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
			entities = append(entities, types.EntityRecord{
				Name:               fc,
				Kind:               "SCOPE.Schema",
				Subtype:            "column",
				SourceFile:         filePath,
				Language:           "sql",
				StartLine:          startLine,
				EndLine:            startLine,
				QualifiedName:      fromTable + "." + fc,
				Signature:          fmt.Sprintf("ALTER TABLE %s ADD FOREIGN KEY (%s) REFERENCES %s(%s)", fromTable, fc, toTable, tc),
				EnrichmentRequired: false,
				Properties: map[string]string{
					"table": fromTable,
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

	// Functions / Procedures.
	for _, m := range funcRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "function:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[1]-1)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "function",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE FUNCTION %s", name),
			EnrichmentRequired: false,
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
)

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

		colEntity := types.EntityRecord{
			Name:               colName,
			Kind:               "SCOPE.Schema",
			Subtype:            "column",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          entryStartLine,
			EndLine:            entryStartLine,
			QualifiedName:      tableName + "." + colName,
			Signature:          strings.TrimSpace(entry),
			EnrichmentRequired: false,
			Properties: map[string]string{
				"table": tableName,
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

// findStmtEnd returns the line number of the next semicolon after startPos.
func findStmtEnd(src string, startPos int) int {
	idx := strings.Index(src[startPos:], ";")
	if idx < 0 {
		return strings.Count(src, "\n") + 1
	}
	return strings.Count(src[:startPos+idx], "\n") + 1
}
