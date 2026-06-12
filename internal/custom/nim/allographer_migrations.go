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
// Honest exclusions / follow-ups (#5111):
//   - `schema().alter` accepts the same Column() builder chains as create; we
//     capture the column NAME + the op, not the full column type re-declaration
//     for change()/alter ops (the engine pass keys on op+table+column).
//   - foreign-key add/drop inside an alter() block is not yet a REFERENCES edge
//     (the create-time FK path in allographer_orm.go is unchanged).
//   - dynamic table names (a variable, not a string literal) are skipped — no
//     fabricated op.
//
// Registration key: "custom_nim_allographer_migrations".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_nim_allographer_migrations", &nimAllographerMigrationsExtractor{})
}

type nimAllographerMigrationsExtractor struct{}

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
	nimAlloChainColumnRe = regexp.MustCompile(`\bColumn\s*(?:\(\s*\))?\s*\.\s*[A-Za-z_][A-Za-z0-9_]*\s*\(\s*"([^"]+)"`)
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

	var out []types.EntityRecord
	seen := make(map[string]bool)
	emit := func(op, table, column string, line int) {
		if table == "" || op == "" {
			return
		}
		name := op + ":" + table
		if column != "" {
			name += "." + column
		}
		key := name
		if seen[key] {
			return
		}
		seen[key] = true
		props := map[string]string{
			"framework":    "allographer",
			"migration_op": op,
			"table":        table,
			"provenance":   "INFERRED_FROM_ALLOGRAPHER_MIGRATION",
		}
		if column != "" {
			props["column"] = column
		}
		out = append(out, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Evolution",
			Subtype:    op,
			SourceFile: file.Path,
			Language:   "nim",
			StartLine:  line,
			EndLine:    line,
			Properties: props,
		})
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

	// --- schema().alter( ... ) -----------------------------------------------
	for _, h := range nimAlloAlterHeadRe.FindAllStringIndex(src, -1) {
		openIdx := h[1] - 1 // index of the '(' that opens alter(
		body := balancedParen(src, openIdx)
		bodyBase := nimLineOf(src, h[0])
		parseAlterBody(body, bodyBase, emit)
	}

	return out, nil
}

// parseAlterBody scans an alter() block body for per-table op chains. Each
// `table("name")` anchors a chain bounded by the next `table(` / `renameTable(`
// (or end of body); the builder method on that chain is the op.
func parseAlterBody(body string, lineBase int, emit func(op, table, column string, line int)) {
	// renameTable("old","new") ops are table-level and not anchored by table().
	for _, m := range nimAlloRenameTableRe.FindAllStringSubmatchIndex(body, -1) {
		old := body[m[2]:m[3]]
		line := lineBase + strings.Count(body[:m[0]], "\n")
		emit("rename_table", old, "", line)
	}

	// Find every table("name") anchor and bound its chain.
	anchors := nimAlloAlterTableRe.FindAllStringSubmatchIndex(body, -1)
	for i, m := range anchors {
		table := body[m[2]:m[3]]
		chainStart := m[1]
		chainEnd := len(body)
		if i+1 < len(anchors) {
			chainEnd = anchors[i+1][0]
		}
		// renameTable(...) boundaries also terminate a chain.
		if rt := nimAlloRenameTableRe.FindStringIndex(body[chainStart:chainEnd]); rt != nil {
			chainEnd = chainStart + rt[0]
		}
		chain := body[chainStart:chainEnd]
		line := lineBase + strings.Count(body[:m[0]], "\n")

		switch {
		case nimAlloDeleteColumnRe.MatchString(chain):
			cm := nimAlloDeleteColumnRe.FindStringSubmatch(chain)
			emit("drop_column", table, cm[1], line)
		case nimAlloRenameColumnRe.MatchString(chain):
			cm := nimAlloRenameColumnRe.FindStringSubmatch(chain)
			emit("rename_column", table, cm[1], line)
		case nimAlloAddRe.MatchString(chain):
			col := ""
			if cm := nimAlloChainColumnRe.FindStringSubmatch(chain); cm != nil {
				col = cm[1]
			}
			emit("add_column", table, col, line)
		case nimAlloChangeRe.MatchString(chain):
			col := ""
			if cm := nimAlloChainColumnRe.FindStringSubmatch(chain); cm != nil {
				col = cm[1]
			}
			emit("alter_column", table, col, line)
		}
	}
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
