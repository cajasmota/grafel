// debby_migrations.go — Nim Debby ORM schema-migration ops + transaction
// boundaries (#5367, finishing the Debby ORM slice begun in debby_orm.go #5028).
//
// debby_orm.go covers the model→table/column schema synthesis and QUERIES
// attribution but deliberately deferred (its #5031 follow-up note) the two
// remaining ORM capabilities Debby presents:
//
//   - migrations — Debby creates and drops tables imperatively against a db
//     handle, taking the model TYPE as the argument (NOT a `Model()` constructor
//     like Norm — Debby models are plain objects):
//
//     db.createTable(User)        # CREATE TABLE for the User model
//     db.createTable(Post)
//     db.dropTable(Post)          # DROP TABLE for the Post model
//
//     Each op is emitted as a shared SCOPE.Evolution migration-op entity
//     (framework=debby, migration_op, table=model name, provenance) — the SAME
//     Kind the Norm/Allographer Nim migration extractors emit, so the engine
//     migration-schema-ops pass (internal/engine/migration_schema_ops.go,
//     `case "debby"`) derives a MODIFIES_TABLE edge op→table convergence node,
//     unifying migration→table evolution with the QUERIES→table access
//     debby_orm.go already records (table identity = the model type name).
//
//   - transactions — Debby wraps a unit of work in a `db.withTransaction:` block
//     (an indentation-delimited NimScript block). Each block header emits a
//     SCOPE.Pattern/transaction_boundary entity (transactional=true,
//     framework=debby, db_handle, provenance), mirroring the Norm
//     `db.transaction:` boundary and the Kotlin/Java @Transactional shape, so the
//     transaction_function_stamping capability lights up for Debby.
//
// Honest exclusions (no fabricated ops):
//   - a `createTable(handle)` whose argument is a lowercase variable (an
//     instance, not a model TYPE) is skipped — Debby's schema ops take the type.
//   - raw-SQL `db.query(sql"…")` DDL is left to the debby_orm #5031 follow-up.
//
// Registration key: "custom_nim_debby_migrations".
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_nim_debby_migrations", &nimDebbyMigrationsExtractor{})
}

type nimDebbyMigrationsExtractor struct{}

func (e *nimDebbyMigrationsExtractor) Language() string {
	return "custom_nim_debby_migrations"
}

var (
	// nimDebbyCreateTableRe matches a `<db>.createTable(Model)` /
	// `<db>.dropTable(Model)` schema op (also the receiver-less form). Group 1 =
	// the op verb (createTable|dropTable), group 2 = the model TYPE argument.
	// Debby takes the model type directly (a plain object), so the argument must
	// be a capitalised identifier to count.
	nimDebbyCreateTableRe = regexp.MustCompile(
		`(?m)\b(?:[A-Za-z_][A-Za-z0-9_]*\s*\.\s*)?(createTable|dropTable)\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// nimDebbyTxnRe matches a Debby `<db>.withTransaction:` block header. Group 1
	// is the db-handle receiver. The trailing `:` introduces the indentation-
	// delimited block body.
	nimDebbyTxnRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*withTransaction\s*:`)
)

// nimDebbyHasSchemaOp is a fast pre-filter: the file must reference Debby and
// perform a schema op or open a transaction.
func nimDebbyHasSchemaOp(content string) bool {
	if !strings.Contains(content, "debby") {
		return false
	}
	return strings.Contains(content, "createTable") ||
		strings.Contains(content, "dropTable") ||
		strings.Contains(content, "withTransaction")
}

func (e *nimDebbyMigrationsExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimDebbyHasSchemaOp(src) {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- migration ops: createTable/dropTable(Model) -------------------------
	seen := make(map[string]bool)
	for _, m := range nimDebbyCreateTableRe.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		arg := src[m[4]:m[5]]
		// Debby schema ops take the model TYPE (capitalised); a lowercase handle
		// is an instance, not a table target — skip it (no fabricated op).
		if arg == "" || arg[0] < 'A' || arg[0] > 'Z' {
			continue
		}
		op := "create_table"
		if verb == "dropTable" {
			op = "drop_table"
		}
		name := op + ":" + arg
		if seen[name] {
			continue
		}
		seen[name] = true
		ent := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Evolution",
			Subtype:    op,
			SourceFile: file.Path,
			Language:   "nim",
			StartLine:  nimLineOf(src, m[0]),
			EndLine:    nimLineOf(src, m[0]),
			Properties: map[string]string{
				"framework":    "debby",
				"migration_op": op,
				"table":        arg,
				"provenance":   "INFERRED_FROM_DEBBY_MIGRATION",
			},
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}

	// --- transaction boundaries: db.withTransaction: -------------------------
	out = append(out, collectDebbyTransactions(src, file.Path)...)

	return out, nil
}

// collectDebbyTransactions emits a SCOPE.Pattern/transaction_boundary entity per
// `<db>.withTransaction:` block header (transactional=true), mirroring the Norm
// `db.transaction:` boundary shape. The boundary is stamped with the set of
// write operations (insert/update/delete) issued inside the transaction body so
// it records WHAT it wraps, not merely WHERE it opens.
func collectDebbyTransactions(src, path string) []types.EntityRecord {
	idx := nimDebbyTxnRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
	var out []types.EntityRecord
	for _, m := range idx {
		recv := src[m[2]:m[3]]
		line := strings.Count(src[:m[0]], "\n") + 1
		txIndent := leadingIndent(lineAt(lines, line))
		props := map[string]string{
			"framework":     "debby",
			"transactional": "true",
			"db_handle":     recv,
			"provenance":    "INFERRED_FROM_DEBBY_TRANSACTION",
		}
		if writes := debbyTxWrites(lines, line, txIndent); writes != "" {
			props["writes"] = writes
			props["has_writes"] = "true"
		}
		ent := types.EntityRecord{
			Name:       recv + ".withTransaction",
			Kind:       "SCOPE.Pattern",
			Subtype:    "transaction_boundary",
			SourceFile: path,
			Language:   "nim",
			StartLine:  line,
			EndLine:    line,
			Properties: props,
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	return out
}

// debbyTxWriteRe matches a Debby write op (`db.insert(...)`/`update`/`delete`)
// inside a transaction body. Group 1 is the operation.
var debbyTxWriteRe = regexp.MustCompile(
	`\b[A-Za-z_][A-Za-z0-9_]*\s*\.\s*(insert|update|delete)\s*\(`)

// debbyTxWrites scans the indentation-delimited body of a withTransaction block
// (lines indented strictly more than the header) for write ops and returns them
// as a comma-joined, de-duplicated, stable-ordered string ("" when none).
func debbyTxWrites(lines []string, headerLine, txIndent int) string {
	found := map[string]bool{}
	for ln := headerLine + 1; ln <= len(lines); ln++ {
		raw := lineAt(lines, ln)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if leadingIndent(raw) <= txIndent {
			break // dedent — transaction body ended
		}
		for _, wm := range debbyTxWriteRe.FindAllStringSubmatch(raw, -1) {
			found[wm[1]] = true
		}
	}
	var parts []string
	for _, op := range []string{"insert", "update", "delete"} {
		if found[op] {
			parts = append(parts, op)
		}
	}
	return strings.Join(parts, ",")
}
