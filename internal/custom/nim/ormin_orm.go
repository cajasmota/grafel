// ormin_orm.go — Nim ormin compile-time ORM model → table/column synthesis (#5028).
//
// ormin (https://github.com/Araq/ormin) is a COMPILE-TIME Nim ORM: the schema is
// NOT declared as Nim object types. Instead a model module imports an external
// SQL DSL file at compile time:
//
//	import ormin
//	importModel(DbBackend.postgre, "model")   # loads model.sql at compile time
//
// and the schema itself lives in that SQL DSL file (`model.sql`), which is a set
// of `create table` DDL statements ormin parses to generate typed query bindings:
//
//	create table User(
//	  id integer primary key,
//	  name string not null,
//	  email string not null
//	);
//
//	create table Post(
//	  id integer primary key,
//	  title string not null,
//	  author integer references User(id)
//	);
//
// So unlike Norm/Debby (Nim object scanning) and Allographer (a Nim schema
// builder), ormin's model→table mapping requires SQL-DSL parsing. This extractor
// covers the two surfaces ormin presents in a Nim project:
//
//  1. The Nim side: an `importModel(DbBackend.<be>, "model")` call is recorded as
//     a SCOPE.Schema/model_import entity (framework=ormin) stamping the backend
//     and the referenced model-file name — this is the binding signal that the
//     repo uses ormin and which DSL file holds its schema.
//  2. The SQL DSL side: a `*.sql` file is parsed for `create table T(...)`
//     statements, each yielding a SCOPE.Schema/table + one SCOPE.Schema/column per
//     column definition (column_type = the SQL type), with `<col> references
//     Other(col)` foreign keys yielding a REFERENCES edge table → referenced table
//     and stamping foreign_key on the column.
//
// What this extractor emits (framework=ormin, SCOPE.Schema shape):
//   - one SCOPE.Schema/model_import per `importModel(DbBackend.<be>, "file")` (Nim)
//   - one SCOPE.Schema/table per `create table T(...)` (SQL DSL)
//   - one SCOPE.Schema/column per column definition, column_type = SQL type,
//     primary_key=true / not_null=true stamped when present
//   - a REFERENCES edge table → referenced table for a column
//     `references Other(col)` foreign key
//
// Honest exclusions / follow-ups (no fabricated schema; #5031):
//   - ormin query DSL attribution (`query: select id from User`) is a follow-up
//     (#5031) — this record covers model→table/column mapping only.
//   - table-level `foreign key (...) references ...(...)` constraint syntax and
//     composite keys beyond the inline column-level `references` form are a
//     follow-up (#5031).
//   - Debby (the other #5028 ORM) is covered by debby_orm.go.
//
// Registration key: "custom_nim_ormin_orm".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_nim_ormin_orm", &nimOrminORMExtractor{})
}

type nimOrminORMExtractor struct{}

func (e *nimOrminORMExtractor) Language() string { return "custom_nim_ormin_orm" }

var (
	// nimOrminImportRe matches the Nim-side `importModel(DbBackend.<be>, "file")`
	// binding. Group 1 is the backend (sqlite/postgre/…), group 2 the referenced
	// model-file base name (the `.sql` DSL file).
	nimOrminImportRe = regexp.MustCompile(
		`\bimportModel\s*\(\s*DbBackend\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)\s*,\s*"([^"]+)"`)

	// orminCreateTableRe matches a `create table T(` DDL header in the SQL DSL.
	// Group 1 is the table name.
	orminCreateTableRe = regexp.MustCompile(
		`(?i)\bcreate\s+table\s+(?:if\s+not\s+exists\s+)?"?([A-Za-z_][A-Za-z0-9_]*)"?\s*\(`)
)

// nimOrminHasImport / orminSQLHasCreate are the per-surface pre-filters.
func nimOrminHasImport(content string) bool {
	return strings.Contains(content, "importModel") && strings.Contains(content, "DbBackend")
}

func orminSQLHasCreate(content string) bool {
	lc := strings.ToLower(content)
	return strings.Contains(lc, "create table")
}

func (e *nimOrminORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Nim side: importModel(...) binding + query DSL attribution.
	if file.Language == "nim" {
		if !nimOrminHasImport(src) {
			return nil, nil
		}
		out := e.extractNimImport(src, file.Path)
		out = append(out, e.extractNimQueries(src, file.Path)...)
		return out, nil
	}

	// SQL DSL side: ormin model files are `.sql`. We parse create-table DDL only
	// for files that look like an ormin model DSL (a `create table` is present).
	// The indexer surfaces sql files with language "sql"; gate on the suffix too
	// so we don't claim arbitrary SQL.
	if strings.HasSuffix(strings.ToLower(file.Path), ".sql") && orminSQLHasCreate(src) {
		return e.extractSQLSchema(src, file.Path), nil
	}
	return nil, nil
}

// extractNimImport emits a SCOPE.Schema/model_import per importModel(...) call.
func (e *nimOrminORMExtractor) extractNimImport(src, path string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, m := range nimOrminImportRe.FindAllStringSubmatchIndex(src, -1) {
		backend := src[m[2]:m[3]]
		modelFile := src[m[4]:m[5]]
		line := strings.Count(src[:m[0]], "\n") + 1
		ent := newOrminSchema(modelFile, "model_import", path, line,
			"INFERRED_FROM_ORMIN_IMPORT")
		ent.Properties["backend"] = backend
		ent.Properties["model_file"] = modelFile
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	return out
}

var (
	// nimOrminQueryBlockRe matches an ormin `query:` DSL invocation head (the
	// macro that compiles a typed SQL query). Group 0 anchors the block; the body
	// (the indentation-delimited statements that follow, or the inline tail on the
	// same line) is scanned by nimOrminQueryTableRe for table references. Both the
	// block form (`query:`) and the call form (`query do:` / `query(...)`) open
	// with the `query` keyword followed by a `:`.
	nimOrminQueryBlockRe = regexp.MustCompile(`(?m)^[ \t]*(?:let|var|const)?[ \t]*(?:[A-Za-z_][A-Za-z0-9_]*[ \t]*=[ \t]*)?query\s*:`)

	// nimOrminQueryTableRe extracts the verb + target table from an ormin query
	// DSL body. ormin mirrors SQL: `select … from T`, `insert into T`,
	// `update T`, `delete from T`. Group 1 is the keyword phrase, group 2 the
	// table identifier.
	nimOrminQueryTableRe = regexp.MustCompile(
		`(?i)\b(from|insert\s+into|update|delete\s+from)\s+"?([A-Za-z_][A-Za-z0-9_]*)"?`)
)

// extractNimQueries scans ormin `query:` DSL blocks and emits one QUERIES edge
// (table-targeted, operation-stamped) per attributed (operation, table) on a
// SCOPE.Schema/query entity. ormin's query DSL is compile-time and SQL-shaped;
// the verb keyword (select/insert/update/delete) before the table maps to the
// operation. Honest partial: nested sub-selects and dynamic table identifiers
// are not attributed (no fabricated query).
func (e *nimOrminORMExtractor) extractNimQueries(src, path string) []types.EntityRecord {
	if !strings.Contains(src, "query") {
		return nil
	}
	lines := strings.Split(src, "\n")
	type qedge struct {
		op    string
		table string
	}
	var edges []qedge
	seen := map[string]bool{}
	headLine := 0
	for _, m := range nimOrminQueryBlockRe.FindAllStringSubmatchIndex(src, -1) {
		startLine := strings.Count(src[:m[0]], "\n") + 1
		if headLine == 0 {
			headLine = startLine
		}
		headerIndent := leadingIndent(lineAt(lines, startLine))
		// The query body is the inline tail of the header line plus the lines
		// indented strictly more than the header (the off-side block).
		var body strings.Builder
		body.WriteString(lineAt(lines, startLine))
		body.WriteByte('\n')
		for ln := startLine + 1; ln <= len(lines); ln++ {
			raw := lineAt(lines, ln)
			if strings.TrimSpace(raw) == "" {
				continue
			}
			if leadingIndent(raw) <= headerIndent {
				break
			}
			body.WriteString(raw)
			body.WriteByte('\n')
		}
		for _, tm := range nimOrminQueryTableRe.FindAllStringSubmatch(body.String(), -1) {
			op := orminQueryVerb(tm[1])
			table := tm[2]
			key := op + ":" + table
			if op == "" || table == "" || seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, qedge{op: op, table: table})
		}
	}
	if len(edges) == 0 {
		return nil
	}
	ent := newOrminSchema("ormin:query", "query", path, headLine, "INFERRED_FROM_ORMIN_QUERY")
	var rels []types.RelationshipRecord
	for _, e := range edges {
		rels = append(rels, types.RelationshipRecord{
			ToID: e.table,
			Kind: "QUERIES",
			Properties: map[string]string{
				"operation": e.op,
				"table":     e.table,
			},
		})
	}
	ent.Relationships = rels
	ent.ID = ent.ComputeID()
	return []types.EntityRecord{ent}
}

// orminQueryVerb normalises an ormin query DSL keyword phrase to an operation.
func orminQueryVerb(kw string) string {
	switch strings.ToLower(strings.Fields(kw)[0]) {
	case "from":
		return "select"
	case "insert":
		return "insert"
	case "update":
		return "update"
	case "delete":
		return "delete"
	}
	return ""
}

// extractSQLSchema parses `create table T(...)` DDL into table + column entities.
func (e *nimOrminORMExtractor) extractSQLSchema(src, path string) []types.EntityRecord {
	tables := collectOrminTables(src)
	if len(tables) == 0 {
		return nil
	}
	var out []types.EntityRecord
	for _, t := range tables {
		table := newOrminSchema(t.name, "table", path, t.line, "INFERRED_FROM_ORMIN_TABLE")
		var rels []types.RelationshipRecord
		for _, c := range t.columns {
			if c.fkTable != "" && c.fkTable != t.name {
				props := map[string]string{"fk_field": c.name, "to_table": c.fkTable}
				if c.fkColumn != "" {
					props["references"] = c.fkColumn
				}
				rels = append(rels, types.RelationshipRecord{
					ToID: c.fkTable, Kind: "REFERENCES", Properties: props,
				})
			}
		}
		table.Relationships = rels
		table.ID = table.ComputeID()
		out = append(out, table)

		colSeen := make(map[string]bool)
		for _, c := range t.columns {
			if colSeen[c.name] {
				continue
			}
			colSeen[c.name] = true
			col := newOrminSchema(c.name, "column", path, c.line, "INFERRED_FROM_ORMIN_COLUMN")
			col.Properties["column_type"] = c.typ
			col.Properties["table"] = t.name
			if c.primaryKey {
				col.Properties["primary_key"] = "true"
			}
			if c.notNull {
				col.Properties["not_null"] = "true"
			}
			if c.fkTable != "" && c.fkTable != t.name {
				col.Properties["foreign_key"] = "true"
				col.Properties["fk_target"] = c.fkTable
				if c.fkColumn != "" {
					col.Properties["fk_column"] = c.fkColumn
				}
			}
			col.ID = col.ComputeID()
			out = append(out, col)
		}
	}
	return out
}

type orminTable struct {
	name    string
	line    int
	columns []orminColumn
}

type orminColumn struct {
	name       string
	typ        string
	primaryKey bool
	notNull    bool
	fkTable    string
	fkColumn   string
	line       int
}

var (
	// orminColLineRe matches a single column definition line inside a create-table
	// body: `name type ...`. Group 1 is the column name, group 2 the SQL type.
	// Reserved table-level constraint keywords (primary/foreign/unique/constraint
	// /check) are filtered out by the caller.
	orminColLineRe = regexp.MustCompile(
		`^\s*"?([A-Za-z_][A-Za-z0-9_]*)"?\s+([A-Za-z_][A-Za-z0-9_]*)`)

	// orminRefRe extracts an inline column-level FK: `references Other(col)` /
	// `references Other (col)` / `references Other`. Group 1 is the referenced
	// table, group 2 the optional referenced column.
	orminRefRe = regexp.MustCompile(
		`(?i)\breferences\s+"?([A-Za-z_][A-Za-z0-9_]*)"?\s*(?:\(\s*"?([A-Za-z_][A-Za-z0-9_]*)"?\s*\))?`)
)

// orminReserved is the set of column-line leading tokens that are table-level
// constraints, not real columns.
var orminReserved = map[string]bool{
	"primary": true, "foreign": true, "unique": true,
	"constraint": true, "check": true, "index": true,
}

// collectOrminTables parses every `create table T( ... )` block by scanning from
// the header `(` to the matching `)` at paren depth 0, then splitting the body on
// top-level commas into column definitions.
func collectOrminTables(src string) []orminTable {
	idx := orminCreateTableRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var tables []orminTable
	for _, m := range idx {
		name := src[m[2]:m[3]]
		headerLine := strings.Count(src[:m[0]], "\n") + 1
		// Body begins right after the matched `(` (m[1] is end of full match).
		bodyStart := m[1]
		body, _ := orminParenBody(src[bodyStart-1:]) // include the `(`
		cols := collectOrminColumns(body, headerLine)
		tables = append(tables, orminTable{name: name, line: headerLine, columns: cols})
	}
	return tables
}

// orminParenBody returns the content between the first `(` and its matching `)`
// (exclusive), and the number of bytes consumed including both parens.
func orminParenBody(s string) (string, int) {
	start := strings.IndexByte(s, '(')
	if start < 0 {
		return "", 0
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start+1 : i], i + 1
			}
		}
	}
	return s[start+1:], len(s)
}

// collectOrminColumns splits a create-table body on top-level commas and parses
// each segment as a column definition. lineBase is the table header line; the
// segment's offset within the body gives its relative line.
func collectOrminColumns(body string, lineBase int) []orminColumn {
	var cols []orminColumn
	segs := splitTopLevelCommas(body)
	off := 0
	for _, seg := range segs {
		segOff := off
		off += len(seg) + 1 // +1 for the comma
		trimmed := strings.TrimSpace(seg)
		if trimmed == "" {
			continue
		}
		// Skip table-level constraints.
		lead := strings.ToLower(strings.Fields(trimmed)[0])
		if orminReserved[lead] {
			continue
		}
		cm := orminColLineRe.FindStringSubmatch(trimmed)
		if cm == nil {
			continue
		}
		name, typ := cm[1], strings.ToLower(cm[2])
		lc := strings.ToLower(seg)
		// Line: count newlines from body start up to this segment's offset.
		line := lineBase + strings.Count(body[:segOff], "\n")
		c := orminColumn{name: name, typ: typ, line: line}
		if strings.Contains(lc, "primary key") {
			c.primaryKey = true
		}
		if strings.Contains(lc, "not null") {
			c.notNull = true
		}
		if rm := orminRefRe.FindStringSubmatch(seg); rm != nil {
			c.fkTable = rm[1]
			c.fkColumn = rm[2]
		}
		cols = append(cols, c)
	}
	return cols
}

// splitTopLevelCommas splits s on commas that are not nested inside parentheses.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// newOrminSchema builds a SCOPE.Schema entity with framework=ormin and the given
// provenance stamp.
func newOrminSchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "nim",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "ormin",
			"provenance": provenance,
		},
	}
}
