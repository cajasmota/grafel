// orm_query_migration.go — shared query-attribution, migration, and transaction
// extraction for the Crystal Avram / Clear / Crecto / Jennifer ORMs (#5366,
// follow-up to #4936). Mirrors the Granite implementation (granite_orm.go) so
// the four remaining ORMs reach feature parity on:
//
//   - query_attribution            — a QUERIES edge model → table per attributed
//                                     canonical SQL op (select/insert/update/
//                                     delete) at a query-DSL call site.
//   - migration_parsing /
//     migration_schema_ops         — a shared SCOPE.Evolution migration-op entity
//                                     (create_table/drop_table/alter_table) per
//                                     migration DSL call or raw schema-op SQL.
//   - transaction_function_stamping — a SCOPE.Pattern/transaction_boundary entity
//                                     per DB transaction block.
//
// Each ORM passes its own framework name + the set of query verbs and migration
// idioms it uses; the entity/edge SHAPE is identical across all five Crystal
// ORMs (and the cross-language Nim/Norm + Rails shape), so a single downstream
// resolver converges them. Honest-partial: only a query receiver naming a model
// declared in the same file is attributed (no false counts on arbitrary types).
package crystal

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// crystalQueryOp maps a query verb to its canonical SQL operation. Verbs not in
// the explicit insert/update/delete sets default to a read (select). The set is
// the union of the four ORMs' query/persistence DSLs (Avram SaveOperation/Query,
// Clear query builder, Crecto Repo, Jennifer Model query DSL).
func crystalQueryOp(verb string) string {
	switch verb {
	case "create", "insert", "import", "create!":
		return "insert"
	case "save", "update", "update!", "update_all", "set":
		return "update"
	case "delete", "destroy", "delete_all", "clear", "truncate":
		return "delete"
	default: // all/find/find_by/where/first/last/count/query/get/...
		return "select"
	}
}

// crystalQueryOpOrder returns the attributed ops in a stable order for
// deterministic edge emission.
func crystalQueryOpOrder(ops map[string]bool) []string {
	var out []string
	for _, op := range []string{"select", "insert", "update", "delete"} {
		if ops[op] {
			out = append(out, op)
		}
	}
	return out
}

// crystalModelQueryRe matches a `<Model>.<verb>` class-method query DSL call.
// Group 1 = the receiver (matched against the known-model set); group 2 = verb.
var crystalModelQueryRe = regexp.MustCompile(
	`(?m)\b([A-Z]\w*)\s*\.\s*(all|find_by|find|where|first|last|count|exists\?|query|get|create!?|save|update!?|update_all|delete|delete_all|destroy|import|clear)\b`)

// collectCrystalModelQueries scans the whole file for `<Model>.<verb>` query DSL
// call sites and attributes each to its model → the set of canonical SQL ops.
// Only receivers naming a recognised in-file model are attributed (honest).
func collectCrystalModelQueries(src string, modelNames map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range crystalModelQueryRe.FindAllStringSubmatch(src, -1) {
		recv, verb := m[1], m[2]
		if !modelNames[recv] {
			continue
		}
		op := crystalQueryOp(strings.TrimSuffix(verb, "!"))
		if out[recv] == nil {
			out[recv] = map[string]bool{}
		}
		out[recv][op] = true
	}
	return out
}

// crystalQueryRels builds the QUERIES edges (model → table) for a model from its
// attributed op set, in stable order.
func crystalQueryRels(model, table string, ops map[string]bool, framework string) []types.RelationshipRecord {
	var rels []types.RelationshipRecord
	for _, op := range crystalQueryOpOrder(ops) {
		rels = append(rels, types.RelationshipRecord{
			ToID: table,
			Kind: "QUERIES",
			Properties: map[string]string{
				"operation": op,
				"table":     table,
				"model":     model,
				"framework": framework,
			},
		})
	}
	return rels
}

// crystalTxRe matches a Crystal-DB / ORM transaction block header
// `<recv>.transaction do` — Avram/Clear/Crecto/Jennifer all expose a
// `<handle>.transaction do … end` block (over the shared crystal-db driver).
// The receiver may be a dotted/`::`-qualified chain (`db`, `Clear::SQL`,
// `Jennifer::Adapter.adapter`); group 1 captures the full chain, group 2 its
// trailing segment (the effective db handle).
var crystalTxRe = regexp.MustCompile(
	`(?m)^[ \t]*((?:[A-Za-z_][\w:]*\.)*([A-Za-z_][\w:]*))\s*\.\s*transaction\s+do\b`)

// collectCrystalTransactions emits a SCOPE.Pattern/transaction_boundary entity
// per `<recv>.transaction do … end` block, mirroring the Granite + Nim/Norm +
// Kotlin/Java @Transactional boundary shape.
func collectCrystalTransactions(src, path, framework string) []types.EntityRecord {
	idx := crystalTxRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var out []types.EntityRecord
	for _, m := range idx {
		recv := src[m[2]:m[3]]   // full receiver chain
		handle := src[m[4]:m[5]] // trailing segment
		line := strings.Count(src[:m[0]], "\n") + 1
		ent := types.EntityRecord{
			Name:       recv + ".transaction",
			Kind:       "SCOPE.Pattern",
			Subtype:    "transaction_boundary",
			SourceFile: path,
			Language:   "crystal",
			StartLine:  line,
			EndLine:    line,
			Properties: map[string]string{
				"framework":     framework,
				"transactional": "true",
				"db_handle":     handle,
				"db_receiver":   recv,
				"provenance":    "INFERRED_FROM_CRYSTAL_TRANSACTION",
			},
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	return out
}

var (
	// crystalMigrateDSLRe matches a migration-DSL call that names its table with a
	// symbol/string argument: `create_table :users do` / `drop_table "posts"` /
	// `alter_table(:orders)` — the form shared by Avram, Clear and Jennifer
	// migration classes. Group 1 = the op (create/drop/alter); group 2 = table.
	crystalMigrateDSLRe = regexp.MustCompile(
		`(?m)^[ \t]*(create|drop|alter)_table[ \t(]+:?["']?([A-Za-z_]\w*)["']?`)

	// crystalSchemaSQLRe matches a raw schema-op SQL string passed to an `.exec`
	// call (`db.exec "CREATE TABLE users (…)"`). Group 1 = the op keyword(s);
	// group 2 = the target table name (quotes/backticks trimmed).
	crystalSchemaSQLRe = regexp.MustCompile(
		"(?is)\\.exec[ \t(]+[\"'`]\\s*(CREATE\\s+TABLE(?:\\s+IF\\s+NOT\\s+EXISTS)?|DROP\\s+TABLE(?:\\s+IF\\s+EXISTS)?|ALTER\\s+TABLE)\\s+[\"'`]?([A-Za-z_]\\w*)")
)

// collectCrystalMigrations emits a shared SCOPE.Evolution migration-op entity per
// migration schema op found in the file: a `create_table/drop_table/alter_table`
// DSL call (the table is the symbol/string argument) and a raw `CREATE/DROP/ALTER
// TABLE <name>` SQL string passed to an `.exec(...)`. Mirrors the Granite + Nim
// Allographer + JS knex migration shape so the engine migration-schema-ops pass
// can converge op→table. Honest: only statically-named tables emit an op.
func collectCrystalMigrations(src, path, framework string) []types.EntityRecord {
	var out []types.EntityRecord
	seen := make(map[string]bool)
	emit := func(op, table string, line int) {
		if table == "" {
			return
		}
		name := op + ":" + table
		if seen[name] {
			return
		}
		seen[name] = true
		ent := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Evolution",
			Subtype:    op,
			SourceFile: path,
			Language:   "crystal",
			StartLine:  line,
			EndLine:    line,
			Properties: map[string]string{
				"framework":    framework,
				"migration_op": op,
				"table":        table,
				"provenance":   "INFERRED_FROM_CRYSTAL_MIGRATION",
			},
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}

	for _, m := range crystalMigrateDSLRe.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		emit(verb+"_table", table, strings.Count(src[:m[0]], "\n")+1)
	}
	for _, m := range crystalSchemaSQLRe.FindAllStringSubmatchIndex(src, -1) {
		kw := strings.ToUpper(strings.Fields(src[m[2]:m[3]])[0])
		table := src[m[4]:m[5]]
		op := "alter_table"
		switch kw {
		case "CREATE":
			op = "create_table"
		case "DROP":
			op = "drop_table"
		}
		emit(op, table, strings.Count(src[:m[0]], "\n")+1)
	}
	return out
}
