// allographer_migrations.go — Nim Allographer alter()/drop() schema migrations
// (#5029, follow-up to #4933).
//
// allographer_orm.go covers the CREATE-time schema (`schema().create(table(...))`).
// Allographer also expresses schema EVOLUTION imperatively against the schema
// builder via `schema().alter(...)` and `schema().drop(...)`:
//
//	import allographer/schema_builder
//
//	schema().alter(
//	  table("users").add(Column().string("bio")),
//	  table("users").change(Column().string("name"), "full_name"),
//	  table("users").renameColumn("name", "full_name"),
//	  table("users").deleteColumn("age"),
//	  renameTable("users", "members"),
//	)
//
//	schema().drop("posts")
//	schema().drop(table("comments"))
//
// Each alter/drop operation is a schema-migration step. We model them with the
// shared SCOPE.Evolution migration-op entity (same Kind the JS knex/typeorm/
// sequelize migration extractors use), so the engine migration-schema-ops pass
// (internal/engine/migration_schema_ops.go) derives a MODIFIES_TABLE edge
// op-entity → SCOPE.Table convergence node, unifying migration→table evolution
// with query→table access on one logical table. The engine pass recognises an
// Allographer SCOPE.Evolution by framework=allographer + a `table` property + an
// op subtype (see evolutionOp's allographer case).
//
// What this extractor emits (framework=allographer):
//   - one SCOPE.Evolution per recognised op, subtype = the normalised op
//     (add_column | drop_column | rename_column | alter_column | rename_table |
//     drop_table), with props: framework, migration_op (the raw builder method),
//     table, and column (when the op is column-scoped).
//
// FK + column-type + dynamic-table evolution (#5111):
//   - a `.foreign("col").reference("refCol").on("refTable")` chain added inside
//     an alter() add()/change() block yields a REFERENCES edge op-entity ->
//     referenced table (same fk_field/to_table/references props the create-time
//     path emits) and stamps foreign_key=true / fk_target / fk_column on the op.
//     A `.dropForeign("col")` chain inside alter() yields a drop_foreign op.
//   - change()/add() ops now re-extract the new column TYPE re-declared in the
//     `Column().<type>("col")` chain into the `new_column_type` property (the
//     builder method name: string/integer/…), so the type delta is preserved.
//   - dynamic table names bound to a string-literal `const`/`let`/`var`
//     (`const tbl = "users"` … `schema().drop(tbl)` / `table(tbl)`) are
//     resolved to the literal; truly dynamic (non-literal) names are still
//     skipped (no fabricated op).
//
// Registration key: "custom_nim_allographer_migrations".
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
	extractor.Register("custom_nim_allographer_migrations", &nimAllographerMigrationsExtractor{})
}

type nimAllographerMigrationsExtractor struct{}

// migOp is a fully-described migration op (#5111): beyond op/table/column it
// carries the re-extracted new column type and any FK chain added in alter().
type migOp struct {
	op            string
	table         string
	column        string
	line          int
	newColumnType string // builder method re-declared in a change()/add() Column() chain
	fkTable       string // .on("table") target of an FK added inside alter()
	fkColumn      string // .reference("col") target
}

func (e *nimAllographerMigrationsExtractor) Language() string {
	return "custom_nim_allographer_migrations"
}

var (
	// nimAlloAlterBlockRe matches a `schema().alter(` head; the balanced body is
	// read from the opening paren. Group 0 only — we just need the position.
	nimAlloAlterHeadRe = regexp.MustCompile(`\bschema\s*\(\s*\)\s*\.\s*alter\s*\(`)

	// nimAlloDropStrRe matches `schema().drop("table")` (string-literal form).
	nimAlloDropStrRe = regexp.MustCompile(`\bschema\s*\(\s*\)\s*\.\s*drop\s*\(\s*"([^"]+)"`)
	// nimAlloDropTableRe matches `schema().drop(table("table"))` (table()-wrapped form).
	nimAlloDropTableRe = regexp.MustCompile(`\bschema\s*\(\s*\)\s*\.\s*drop\s*\(\s*table\s*\(\s*"([^"]+)"`)

	// Within an alter() block, each `table("name")` anchors an op chain; the
	// following builder method (.add/.change/.deleteColumn/.renameColumn) is the
	// op, and the column literal(s) carry the column name.
	nimAlloAlterTableRe = regexp.MustCompile(`\btable\s*\(\s*"([^"]+)"\s*\)`)

	// renameTable("old", "new") — a table-level rename op inside alter().
	nimAlloRenameTableRe = regexp.MustCompile(`\brenameTable\s*\(\s*"([^"]+)"\s*,\s*"([^"]+)"`)

	// op-chain method recognisers (applied to the chain following a table("x")).
	nimAlloAddRe          = regexp.MustCompile(`\.\s*add\s*\(`)
	nimAlloChangeRe       = regexp.MustCompile(`\.\s*change\s*\(`)
	nimAlloDeleteColumnRe = regexp.MustCompile(`\.\s*deleteColumn\s*\(\s*"([^"]+)"`)
	nimAlloRenameColumnRe = regexp.MustCompile(`\.\s*renameColumn\s*\(\s*"([^"]+)"`)
	// column-name literal inside an add()/change() chain: Column().<type>("col").
	// Group 1 = the builder method (column type), group 2 = the column name.
	nimAlloChainColumnRe = regexp.MustCompile(`\bColumn\s*(?:\(\s*\))?\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(\s*"([^"]+)"`)

	// FK chain links inside an alter() add()/change() block (mirror allographer_orm.go).
	nimAlloMigOnRe        = regexp.MustCompile(`\.\s*on\s*\(\s*"([^"]+)"`)
	nimAlloMigReferenceRe = regexp.MustCompile(`\.\s*reference\s*\(\s*"([^"]+)"`)
	nimAlloMigForeignRe   = regexp.MustCompile(`\.\s*foreign\s*\(\s*"([^"]+)"`)
	// .dropForeign("col") — FK removal inside alter().
	nimAlloDropForeignRe = regexp.MustCompile(`\.\s*dropForeign\s*\(\s*"([^"]+)"`)

	// schema().drop(IDENT) / table(IDENT) — non-literal (bare identifier) forms
	// whose name must be resolved from a const/let/var string-literal binding.
	nimAlloDropIdentRe   = regexp.MustCompile(`\bschema\s*\(\s*\)\s*\.\s*drop\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	nimAlloTableIdentRe  = regexp.MustCompile(`\btable\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	// const/let/var binding of an identifier to a string literal: `const x = "users"`.
	nimAlloStrBindingRe = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^\n=]+)?=\s*"([^"]+)"`)
)

// nimAllographerHasMigration is a fast pre-filter: the file must reference the
// Allographer schema builder migration ops (`schema().alter` or `schema().drop`)
// to be worth scanning.
func nimAllographerHasMigration(content string) bool {
	if !strings.Contains(content, "schema(") {
		return false
	}
	if !strings.Contains(content, "allographer") && !strings.Contains(content, "Column") {
		return false
	}
	return strings.Contains(content, ".alter(") || strings.Contains(content, ".drop(")
}

func (e *nimAllographerMigrationsExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimAllographerHasMigration(src) {
		return nil, nil
	}

	// bindings maps a const/let/var identifier to its string-literal value so a
	// `schema().drop(tbl)` / `table(tbl)` referencing it can be resolved (#5111).
	bindings := make(map[string]string)
	for _, m := range nimAlloStrBindingRe.FindAllStringSubmatch(src, -1) {
		bindings[m[1]] = m[2]
	}

	var out []types.EntityRecord
	seen := make(map[string]bool)
	// emitFull is the full op emitter; emit is the column-less convenience form.
	emitFull := func(o migOp) {
		if o.table == "" || o.op == "" {
			return
		}
		name := o.op + ":" + o.table
		if o.column != "" {
			name += "." + o.column
		}
		if seen[name] {
			return
		}
		seen[name] = true
		props := map[string]string{
			"framework":    "allographer",
			"migration_op": o.op,
			"table":        o.table,
			"provenance":   "INFERRED_FROM_ALLOGRAPHER_MIGRATION",
		}
		if o.column != "" {
			props["column"] = o.column
		}
		// #5111: re-extract the new column TYPE declared in the Column() chain.
		if o.newColumnType != "" {
			props["new_column_type"] = o.newColumnType
		}
		rec := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Evolution",
			Subtype:    o.op,
			SourceFile: file.Path,
			Language:   "nim",
			StartLine:  o.line,
			EndLine:    o.line,
			Properties: props,
		}
		// #5111: an FK added inside alter() yields a REFERENCES edge op -> ref
		// table (same shape the create-time path in allographer_orm.go emits).
		if o.fkTable != "" && o.fkTable != o.table {
			props["foreign_key"] = "true"
			props["fk_target"] = o.fkTable
			if o.fkColumn != "" {
				props["fk_column"] = o.fkColumn
			}
			relProps := map[string]string{"fk_field": o.column, "to_table": o.fkTable}
			if o.fkColumn != "" {
				relProps["references"] = o.fkColumn
			}
			rec.Relationships = []types.RelationshipRecord{{
				ToID: o.fkTable, Kind: "REFERENCES", Properties: relProps,
			}}
			rec.ID = rec.ComputeID()
		}
		out = append(out, rec)
	}
	emit := func(op, table, column string, line int) {
		emitFull(migOp{op: op, table: table, column: column, line: line})
	}

	// --- schema().drop("table") / schema().drop(table("table")) --------------
	for _, m := range nimAlloDropTableRe.FindAllStringSubmatchIndex(src, -1) {
		table := src[m[2]:m[3]]
		emit("drop_table", table, "", nimLineOf(src, m[0]))
	}
	for _, m := range nimAlloDropStrRe.FindAllStringSubmatchIndex(src, -1) {
		// The table("...") form also matches drop( ... ) loosely; skip if this is
		// actually the table()-wrapped form (handled above) — the string literal
		// captured here would be the method name `table`'s arg only when not
		// wrapped. nimAlloDropStrRe requires a quote immediately after `drop(`, so
		// `drop(table("x"))` does NOT match it (there's `table(` between). Safe.
		table := src[m[2]:m[3]]
		emit("drop_table", table, "", nimLineOf(src, m[0]))
	}
	// schema().drop(IDENT) — resolve a const/let/var-bound string-literal table
	// name to its literal (#5111); skip truly dynamic (unbound) identifiers.
	for _, m := range nimAlloDropIdentRe.FindAllStringSubmatchIndex(src, -1) {
		ident := src[m[2]:m[3]]
		if lit, ok := bindings[ident]; ok {
			emit("drop_table", lit, "", nimLineOf(src, m[0]))
		}
	}

	// --- schema().alter( ... ) -----------------------------------------------
	for _, h := range nimAlloAlterHeadRe.FindAllStringIndex(src, -1) {
		openIdx := h[1] - 1 // index of the '(' that opens alter(
		body := balancedParen(src, openIdx)
		bodyBase := nimLineOf(src, h[0])
		parseAlterBody(body, bodyBase, bindings, emitFull)
	}

	return out, nil
}

// parseAlterBody scans an alter() block body for per-table op chains. Each
// `table("name")` (or `table(IDENT)` resolved via bindings) anchors a chain
// bounded by the next table(...) / renameTable( (or end of body); the builder
// method on that chain is the op. add()/change() chains re-extract the new
// column type and any FK chain; a .dropForeign("col") yields a drop_foreign op.
func parseAlterBody(body string, lineBase int, bindings map[string]string, emit func(migOp)) {
	// renameTable("old","new") ops are table-level and not anchored by table().
	for _, m := range nimAlloRenameTableRe.FindAllStringSubmatchIndex(body, -1) {
		old := body[m[2]:m[3]]
		line := lineBase + strings.Count(body[:m[0]], "\n")
		emit(migOp{op: "rename_table", table: old, line: line})
	}

	// Collect every table anchor: string-literal table("name") + identifier
	// table(IDENT) forms (resolved via bindings), merged in source order so chain
	// bounds are correct regardless of which form precedes which.
	type anchor struct {
		start, end int    // byte span of the table(...) match
		table      string // resolved table name ("" if unresolved identifier)
	}
	var anchors []anchor
	for _, m := range nimAlloAlterTableRe.FindAllStringSubmatchIndex(body, -1) {
		anchors = append(anchors, anchor{start: m[0], end: m[1], table: body[m[2]:m[3]]})
	}
	for _, m := range nimAlloTableIdentRe.FindAllStringSubmatchIndex(body, -1) {
		ident := body[m[2]:m[3]]
		anchors = append(anchors, anchor{start: m[0], end: m[1], table: bindings[ident]})
	}
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].start < anchors[j].start })

	for i, a := range anchors {
		if a.table == "" { // unresolved dynamic table identifier — no fabricated op
			continue
		}
		table := a.table
		chainStart := a.end
		chainEnd := len(body)
		if i+1 < len(anchors) {
			chainEnd = anchors[i+1].start
		}
		// renameTable(...) boundaries also terminate a chain.
		if rt := nimAlloRenameTableRe.FindStringIndex(body[chainStart:chainEnd]); rt != nil {
			chainEnd = chainStart + rt[0]
		}
		chain := body[chainStart:chainEnd]
		line := lineBase + strings.Count(body[:a.start], "\n")

		switch {
		case nimAlloDropForeignRe.MatchString(chain):
			cm := nimAlloDropForeignRe.FindStringSubmatch(chain)
			emit(migOp{op: "drop_foreign", table: table, column: cm[1], line: line})
		case nimAlloDeleteColumnRe.MatchString(chain):
			cm := nimAlloDeleteColumnRe.FindStringSubmatch(chain)
			emit(migOp{op: "drop_column", table: table, column: cm[1], line: line})
		case nimAlloRenameColumnRe.MatchString(chain):
			cm := nimAlloRenameColumnRe.FindStringSubmatch(chain)
			emit(migOp{op: "rename_column", table: table, column: cm[1], line: line})
		case nimAlloAddRe.MatchString(chain):
			emit(alterColumnOp("add_column", table, chain, line))
		case nimAlloChangeRe.MatchString(chain):
			emit(alterColumnOp("alter_column", table, chain, line))
		}
	}
}

// alterColumnOp builds an add_column/alter_column migOp from a Column() chain,
// re-extracting the column name + new column TYPE (#5111) and any FK chain
// (.foreign(...).reference(...).on(...)) so an alter-time FK yields a REFERENCES
// edge. An FK column with no explicit type defaults its type from .foreign.
func alterColumnOp(op, table, chain string, line int) migOp {
	o := migOp{op: op, table: table, line: line}
	if cm := nimAlloChainColumnRe.FindStringSubmatch(chain); cm != nil {
		o.newColumnType = cm[1] // builder method = column type
		o.column = cm[2]
	}
	// FK chain added inside alter(): .foreign("col").reference("c").on("table").
	if fm := nimAlloMigForeignRe.FindStringSubmatch(chain); fm != nil && o.column == "" {
		o.column = fm[1] // a bare .foreign("col") carries the column name
	}
	if om := nimAlloMigOnRe.FindStringSubmatch(chain); om != nil {
		o.fkTable = om[1]
	}
	if rm := nimAlloMigReferenceRe.FindStringSubmatch(chain); rm != nil {
		o.fkColumn = rm[1]
	}
	return o
}

// nimLineOf returns the 1-based line number of the byte offset in src.
func nimLineOf(src string, offset int) int {
	if offset < 0 || offset > len(src) {
		return 1
	}
	return strings.Count(src[:offset], "\n") + 1
}

// balancedParen returns the substring inside a balanced () pair starting at the
// '(' at openIdx (exclusive of the outer parens). If unbalanced, returns the
// remainder of src after openIdx.
func balancedParen(src string, openIdx int) string {
	if openIdx < 0 || openIdx >= len(src) || src[openIdx] != '(' {
		return ""
	}
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openIdx+1 : i]
			}
		}
	}
	return src[openIdx+1:]
}
