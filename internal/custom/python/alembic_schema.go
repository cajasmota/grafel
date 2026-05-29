package python

// alembic_schema.go — schema extraction for Alembic migration operations.
//
// Issue #3192 — Alembic schema_extraction extractor
// (op.create_table / op.add_column / op.create_index).
//
// Pattern: Alembic migration files (under a versions/ directory) express
// schema mutations imperatively through the `op` migration-operations proxy
// rather than as ORM model classes. The schema is therefore not visible to
// the SQLAlchemy class-body scanner (sqlalchemy.go) nor to the raw-SQL-literal
// scanner (driver_schema.go, #3189). This extractor scans for the three
// structural Alembic operations and emits SCOPE.Schema entities:
//
//   - op.create_table("users", sa.Column("id", sa.Integer), ...)
//       -> one table entity ("users") + one column entity per sa.Column(...)
//   - op.add_column("users", sa.Column("email", sa.String))
//       -> one column entity ("users.email") attributed to its parent table
//   - op.create_index("ix_users_email", "users", ["email"])
//       -> one index entity ("users.ix_users_email")
//
// This is heuristic (regex over the migration source, not a full Python
// parse), so the registry cell is flipped to `partial`, not `full`. It is
// deliberately distinct from and non-overlapping with driver_schema.go: that
// extractor only fires on raw-driver imports + embedded CREATE TABLE SQL,
// whereas this one only fires on the Alembic `op.` operations proxy and never
// parses SQL string literals.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_alembic_schema", &AlembicSchemaExtractor{})
}

// AlembicSchemaExtractor emits SCOPE.Schema entities for the structural
// Alembic migration operations (create_table / add_column / create_index).
type AlembicSchemaExtractor struct{}

func (e *AlembicSchemaExtractor) Language() string { return "python_alembic_schema" }

var (
	// alembicGateRe gates the extractor: the file must look like an Alembic
	// migration. Alembic-generated migrations import the operations proxy as
	// `from alembic import op` and almost always carry a `revision = "..."`
	// module variable. Requiring the `op` import keeps us from misfiring on
	// arbitrary modules that happen to define an `op` local.
	alembicGateRe = regexp.MustCompile(
		`(?m)^\s*from\s+alembic\s+import\s+[^\n]*\bop\b`)

	// alembicCreateTableRe matches the head of an op.create_table call and
	// captures the (string-literal) table name.
	//   op.create_table("users", ...)
	//   op.create_table('orders', ...)
	alembicCreateTableRe = regexp.MustCompile(
		`(?:\bop|\.)\.?create_table\s*\(\s*[rbuRBU]*["']([A-Za-z_][A-Za-z0-9_]*)["']`)

	// alembicAddColumnRe matches an op.add_column call, capturing the target
	// table name. The column itself is parsed from the sa.Column(...) argument
	// that follows.
	//   op.add_column("users", sa.Column("email", sa.String()))
	alembicAddColumnRe = regexp.MustCompile(
		`(?:\bop|\.)\.?add_column\s*\(\s*[rbuRBU]*["']([A-Za-z_][A-Za-z0-9_]*)["']`)

	// alembicCreateIndexRe matches an op.create_index call, capturing the
	// index name and the table it targets.
	//   op.create_index("ix_users_email", "users", ["email"])
	//   op.create_index(op.f("ix_users_email"), "users", ["email"])
	alembicCreateIndexRe = regexp.MustCompile(
		`(?:\bop|\.)\.?create_index\s*\(\s*(?:op\.f\s*\(\s*)?[rbuRBU]*["']([A-Za-z_][A-Za-z0-9_]*)["']\s*\)?\s*,\s*[rbuRBU]*["']([A-Za-z_][A-Za-z0-9_]*)["']`)

	// alembicColumnRe matches an `sa.Column("name", <type>...)` definition and
	// captures the column name and the leading type token. The type may be a
	// dotted reference (sa.Integer, sa.String, postgresql.JSONB) — we capture
	// the final identifier as the column type.
	//   sa.Column("id", sa.Integer(), primary_key=True)
	//   sa.Column('email', sa.String(255), nullable=False)
	alembicColumnRe = regexp.MustCompile(
		`(?:sa\.)?[Cc]olumn\s*\(\s*[rbuRBU]*["']([A-Za-z_][A-Za-z0-9_]*)["']\s*,\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)*([A-Za-z_][A-Za-z0-9_]*)`)
)

func (e *AlembicSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_alembic_schema")
	_, span := tracer.Start(ctx, "custom.python_alembic_schema")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	source := string(file.Content)

	// Cheap gate: must be an Alembic migration (imports the `op` proxy).
	if !alembicGateRe.MatchString(source) {
		return nil, nil
	}

	var out []types.EntityRecord
	seenTables := make(map[string]bool)
	seenCols := make(map[string]bool)
	seenIdx := make(map[string]bool)

	emitTable := func(name string, line int) {
		if seenTables[name] {
			return
		}
		seenTables[name] = true
		out = append(out, entity(name, "SCOPE.Schema", "", file.Path, line,
			map[string]string{
				"framework":    "alembic",
				"pattern_type": "table",
				"table_name":   name,
				"source":       "alembic_migration",
			}))
	}

	emitColumn := func(table, col, colType string, line int) {
		key := table + "." + col
		if seenCols[key] {
			return
		}
		seenCols[key] = true
		out = append(out, entity(key, "SCOPE.Schema", "column", file.Path, line,
			map[string]string{
				"framework":    "alembic",
				"pattern_type": "column",
				"column_type":  colType,
				"parent_table": table,
				"source":       "alembic_migration",
			}))
	}

	// --- op.create_table("name", sa.Column(...), ...) ---------------------
	for _, ct := range allMatchesIndex(alembicCreateTableRe, source) {
		table := source[ct[2]:ct[3]]
		tableLine := lineOf(source, ct[0])
		emitTable(table, tableLine)

		// The column definitions live inside the create_table(...) argument
		// list. Read the balanced parenthesised body starting at the "(" that
		// opens the call (the last "(" before the table-name literal).
		openIdx := strings.LastIndex(source[:ct[3]], "(")
		body := balancedParenBody(source, openIdx)
		for _, cm := range alembicColumnRe.FindAllStringSubmatchIndex(body, -1) {
			col := body[cm[2]:cm[3]]
			colType := body[cm[4]:cm[5]]
			colLine := tableLine + strings.Count(body[:cm[0]], "\n")
			emitColumn(table, col, colType, colLine)
		}
	}

	// --- op.add_column("name", sa.Column(...)) ----------------------------
	for _, ac := range allMatchesIndex(alembicAddColumnRe, source) {
		table := source[ac[2]:ac[3]]
		addLine := lineOf(source, ac[0])
		// add_column targets an existing table; record it as a (table) entity
		// so the column has a parent, but it is a column-level operation.
		emitTable(table, addLine)

		openIdx := strings.LastIndex(source[:ac[3]], "(")
		body := balancedParenBody(source, openIdx)
		if cm := alembicColumnRe.FindStringSubmatchIndex(body); cm != nil {
			col := body[cm[2]:cm[3]]
			colType := body[cm[4]:cm[5]]
			colLine := addLine + strings.Count(body[:cm[0]], "\n")
			emitColumn(table, col, colType, colLine)
		}
	}

	// --- op.create_index("ix_name", "table", [...]) -----------------------
	for _, ix := range allMatchesIndex(alembicCreateIndexRe, source) {
		idxName := source[ix[2]:ix[3]]
		table := source[ix[4]:ix[5]]
		idxLine := lineOf(source, ix[0])
		key := table + "." + idxName
		if seenIdx[key] {
			continue
		}
		seenIdx[key] = true
		out = append(out, entity(key, "SCOPE.Schema", "index", file.Path, idxLine,
			map[string]string{
				"framework":    "alembic",
				"pattern_type": "index",
				"index_name":   idxName,
				"parent_table": table,
				"source":       "alembic_migration",
			}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
