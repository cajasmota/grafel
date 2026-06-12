// allographer_orm.go — Nim Allographer schema-builder table/column synthesis (#4933).
//
// Allographer (https://github.com/itsumura-h/nim-allographer) is a Nim query
// builder + schema builder. Unlike Norm (#4904), an Allographer schema is NOT a
// `ref object` type — it is declared imperatively against the schema builder:
//
//	import allographer/schema_builder
//
//	schema().create(
//	  table("users", [
//	    Column().increments("id"),
//	    Column().string("name"),
//	    Column().string("email").unique(),
//	    Column().integer("age").nullable(),
//	    Column().foreign("post_id").reference("id").on("posts").onDelete(SET_NULL),
//	  ]),
//	  table("posts", [
//	    Column().increments("id"),
//	    Column().string("title"),
//	  ]),
//	)
//
// The table identity is the literal string passed to `table("...")`. Each
// `Column().<type>("name")` call inside that table block is a column whose
// `column_type` is the builder method name (string/integer/increments/…). A
// `Column().foreign("col").reference("refCol").on("refTable")` chain is a
// foreign key: it yields a REFERENCES edge table → referenced table (keyed by
// the `.on("...")` target) and stamps foreign_key on the column. `.unique()` and
// `.nullable()` modifiers on a column chain are stamped on the column.
//
// What this extractor emits (mirrors the Norm + PHP/Eloquent SCOPE.Schema shape,
// framework=allographer):
//   - one SCOPE.Schema/table per `table("name", [...])` block
//   - one SCOPE.Schema/column per `Column().<type>("col")` call, with column_type
//     = the builder method, plus unique/nullable when modifiers are present
//   - a REFERENCES edge table → referenced table for a `.foreign(...).on(...)`
//     column chain (the FK signal), keyed by the `.on()` target table name
//
// Honest exclusions / follow-ups (no fabricated schema):
//   - Allographer has no `ref object` model layer to map to the table — the
//     schema builder IS the schema, so there is no separate model entity (model
//     mapping is not_applicable for Allographer, unlike Norm).
//   - alter()/drop() schema migrations are a follow-up (#5029); the rdb() query
//     builder (rdb().table("x").select(...)) query attribution + rdb()
//     transactions are a follow-up (#5030).
//   - cross-file FK targets resolve via the shared resolver (REFERENCES carries
//     the bare table name).
//
// Registration key: "custom_nim_allographer_orm".
package nim

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_nim_allographer_orm", &nimAllographerORMExtractor{})
}

type nimAllographerORMExtractor struct{}

func (e *nimAllographerORMExtractor) Language() string { return "custom_nim_allographer_orm" }

var (
	// nimAlloTableRe matches a `table("name"` schema-builder table declaration.
	// Group 1 is the table name string literal.
	nimAlloTableRe = regexp.MustCompile(`\btable\s*\(\s*"([^"]+)"`)

	// nimAlloColumnRe matches a single `Column().<method>("col")` builder call.
	// Group 1 is the builder method (the column type), group 2 the column name.
	// `Column()` and `Column` (no parens) forms are both accepted.
	nimAlloColumnRe = regexp.MustCompile(
		`\bColumn\s*(?:\(\s*\))?\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(\s*"([^"]+)"`)

	// nimAlloOnRe extracts the referenced table from a `.on("table")` chain link.
	nimAlloOnRe = regexp.MustCompile(`\.\s*on\s*\(\s*"([^"]+)"`)
	// nimAlloReferenceRe extracts the referenced column from `.reference("col")`.
	nimAlloReferenceRe = regexp.MustCompile(`\.\s*reference\s*\(\s*"([^"]+)"`)
	// nimAlloUniqueRe / nimAlloNullableRe are column-chain modifier markers.
	nimAlloUniqueRe   = regexp.MustCompile(`\.\s*unique\s*\(`)
	nimAlloNullableRe = regexp.MustCompile(`\.\s*nullable\s*\(`)
)

// nimAllographerHasSchema is a fast pre-filter: the file must reference the
// Allographer schema builder (`table(` + `Column`) to be worth scanning, so we
// never misfire on arbitrary Nim code.
func nimAllographerHasSchema(content string) bool {
	return strings.Contains(content, "Column") &&
		strings.Contains(content, "table(") &&
		(strings.Contains(content, "allographer") || strings.Contains(content, "schema"))
}

func (e *nimAllographerORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimAllographerHasSchema(src) {
		return nil, nil
	}

	tables := collectAllographerTables(src)
	if len(tables) == 0 {
		return nil, nil
	}

	var out []types.EntityRecord
	for _, tbl := range tables {
		// 1. table entity (identity = the table("...") string literal).
		table := newAllographerSchema(tbl.name, "table", file.Path, tbl.line,
			"INFERRED_FROM_ALLOGRAPHER_TABLE")
		var rels []types.RelationshipRecord
		for _, c := range tbl.columns {
			if c.fkTable != "" && c.fkTable != tbl.name {
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

		// 2. column entities (one per Column().<type>("col") call).
		colSeen := make(map[string]bool)
		for _, c := range tbl.columns {
			if colSeen[c.name] {
				continue
			}
			colSeen[c.name] = true
			col := newAllographerSchema(c.name, "column", file.Path, c.line,
				"INFERRED_FROM_ALLOGRAPHER_COLUMN")
			col.Properties["column_type"] = c.typ
			col.Properties["table"] = tbl.name
			if c.unique {
				col.Properties["unique"] = "true"
			}
			if c.nullable {
				col.Properties["nullable"] = "true"
			}
			if c.fkTable != "" && c.fkTable != tbl.name {
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
	return out, nil
}

// allographerTable is a parsed Allographer schema-builder table block.
type allographerTable struct {
	name    string
	line    int
	columns []allographerColumn
}

type allographerColumn struct {
	name     string
	typ      string // builder method: string/integer/increments/…
	unique   bool
	nullable bool
	fkTable  string // .on("table") target when this is a .foreign(...) chain
	fkColumn string // .reference("col") target
	line     int
}

// collectAllographerTables finds every `table("name", [...])` block and the
// Column() builder calls in its body. The body of a table is bounded by the next
// `table(` declaration (or EOF) — Allographer tables are sibling args to
// schema().create(...), so a column belongs to the most recent table header
// above it. Each physical line is scanned for a Column() call; a column chain may
// span its own line (the common formatting), so we attribute per-line.
func collectAllographerTables(src string) []allographerTable {
	tblIdx := nimAlloTableRe.FindAllStringSubmatchIndex(src, -1)
	if len(tblIdx) == 0 {
		return nil
	}
	var tables []allographerTable
	for i, m := range tblIdx {
		name := src[m[2]:m[3]]
		start := m[1] // end of the table("name" match → body begins here
		end := len(src)
		if i+1 < len(tblIdx) {
			end = tblIdx[i+1][0] // next table( header bounds this body
		}
		line := strings.Count(src[:m[0]], "\n") + 1
		body := src[start:end]
		bodyLineBase := line
		tables = append(tables, allographerTable{
			name:    name,
			line:    line,
			columns: collectAllographerColumns(body, bodyLineBase),
		})
	}
	return tables
}

// collectAllographerColumns scans a table body for Column().<type>("col") calls
// (including Column().foreign("col"), whose type is "foreign"). Each Column()
// call may carry trailing .unique()/.nullable() modifiers and, for a
// .foreign("col").reference("c").on("table") chain, FK targets. The chain is read
// from the Column() match position up to the next Column() boundary within the
// body so modifiers attach to the right column.
func collectAllographerColumns(body string, lineBase int) []allographerColumn {
	colIdx := nimAlloColumnRe.FindAllStringSubmatchIndex(body, -1)
	if len(colIdx) == 0 {
		return nil
	}

	type chainStart struct {
		pos  int
		name string
		typ  string // builder method: string/integer/increments/foreign/…
	}
	var starts []chainStart
	for _, m := range colIdx {
		starts = append(starts, chainStart{pos: m[0], name: body[m[4]:m[5]], typ: body[m[2]:m[3]]})
	}
	// Sort starts by position so each chain's body is [pos, nextPos).
	sort.Slice(starts, func(i, j int) bool { return starts[i].pos < starts[j].pos })

	var cols []allographerColumn
	for i, s := range starts {
		end := len(body)
		if i+1 < len(starts) {
			end = starts[i+1].pos
		}
		chain := body[s.pos:end]
		line := lineBase + strings.Count(body[:s.pos], "\n")
		c := allographerColumn{name: s.name, typ: s.typ, line: line}
		if nimAlloUniqueRe.MatchString(chain) {
			c.unique = true
		}
		if nimAlloNullableRe.MatchString(chain) {
			c.nullable = true
		}
		// FK: a .on("table") within this chain (a .foreign chain, or an inline
		// .reference().on() on a typed column) makes this a foreign key.
		if om := nimAlloOnRe.FindStringSubmatch(chain); om != nil {
			c.fkTable = om[1]
		}
		if rm := nimAlloReferenceRe.FindStringSubmatch(chain); rm != nil {
			c.fkColumn = rm[1]
		}
		cols = append(cols, c)
	}
	return cols
}

// newAllographerSchema builds a SCOPE.Schema entity with framework=allographer
// and the given provenance stamp.
func newAllographerSchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "nim",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "allographer",
			"provenance": provenance,
		},
	}
}
