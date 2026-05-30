package rust

// rusqlite.go — custom extractor for the rusqlite SQLite bindings (Rust).
//
// rusqlite is a thin, synchronous SQLite binding — not a full ORM. It owns
// neither model derives, relationship macros, nor a migration framework, so
// the only meaningful extraction surface is raw-SQL query attribution. This
// extractor detects SQL passed to the rusqlite execution APIs and attributes
// each statement to its primary target table.
//
// Detects and emits entities for:
//
//   - conn.execute("INSERT INTO ...", params) → SCOPE.Operation (sql_query)
//   - conn.prepare("SELECT ... FROM ...")     → SCOPE.Operation (sql_query)
//   - conn.query_row("SELECT ... FROM ...")   → SCOPE.Operation (sql_query)
//   - Connection::open(...) / open_in_memory() → SCOPE.Component (db_connection)
//
// Honesty:
//
//	query_attribution is full for inline string literals: the target table is
//	resolved from the SQL text. Queries built from non-literal expressions
//	(format!/runtime concatenation) cannot be attributed and are skipped.
//
// Issue #3412 — lang.rust.orm.rusqlite query_attribution deepening.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_rusqlite", &rustRusqliteExtractor{})
}

type rustRusqliteExtractor struct{}

func (e *rustRusqliteExtractor) Language() string { return "custom_rust_rusqlite" }

var (
	// SQL passed to a rusqlite execution method as the first string-literal arg:
	//   .execute("...")  .prepare("...")  .query_row("...")  .query_map("...")
	//   .prepare_cached("...")  .execute_batch("...")
	// Captures the SQL literal. Requires a leading dot so we only match method
	// calls, reducing false positives on unrelated string literals.
	reRusqliteSQL = regexp.MustCompile(
		`\.(?:execute|execute_batch|prepare|prepare_cached|query_row|query_map|query_and_then|query)\s*\(\s*"([^"]{5,})"`,
	)

	// Connection::open("path") / Connection::open_in_memory()
	reRusqliteOpen = regexp.MustCompile(
		`(?:rusqlite::)?Connection\s*::\s*(open|open_in_memory|open_with_flags)\s*\(`,
	)

	// rusqlite-specific markers to avoid mis-attributing generic .execute( calls
	// (e.g. sqlx, diesel) to rusqlite. We only emit query entities when the file
	// shows a rusqlite signal.
	reRusqliteSignal = regexp.MustCompile(
		`\brusqlite\b|\bConnection::open|params!\s*\[|named_params!\s*\{`,
	)
)

func (e *rustRusqliteExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_rusqlite_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	// Gate on a rusqlite signal: many ORMs expose .execute(...) so we require
	// at least one rusqlite-specific marker before attributing queries.
	if !reRusqliteSignal.MatchString(src) {
		return nil, nil
	}

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

	// 1. Connection::open* → db_connection
	for _, m := range reRusqliteOpen.FindAllStringSubmatchIndex(src, -1) {
		openKind := src[m[2]:m[3]]
		ent := makeEntity("rusqlite:connection:"+openKind, "SCOPE.Component", "db_connection",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rusqlite",
			"open_kind", openKind,
			"provenance", "INFERRED_FROM_RUSQLITE_CONNECTION_OPEN",
		)
		add(ent)
	}

	// 2. query_attribution — raw SQL passed to execution APIs, attributed to
	//    the primary target table resolved from the SQL text.
	for _, m := range reRusqliteSQL.FindAllStringSubmatchIndex(src, -1) {
		fullSQL := src[m[2]:m[3]]
		table := sqlPrimaryTable(fullSQL)
		sql := fullSQL
		if len(sql) > 100 {
			sql = sql[:100] + "..."
		}
		name := "rusqlite:query"
		if table != "" {
			name = "rusqlite:query:" + table
		}
		ent := makeEntity(name, "SCOPE.Operation", "sql_query",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rusqlite",
			"sql_fragment", sql,
			"target_table", table,
			"provenance", "INFERRED_FROM_RUSQLITE_RAW_SQL",
		)
		add(ent)
	}

	// Also attribute CREATE TABLE statements (common in rusqlite setup code)
	// even when wrapped in execute_batch with multiple statements.
	for _, m := range reSQLCreateTable.FindAllStringSubmatchIndex(src, -1) {
		// Only when the CREATE TABLE appears inside a string literal handed to
		// a rusqlite API — approximated by the rusqlite signal gate above.
		if !strings.Contains(src, "execute") && !strings.Contains(src, "query") {
			continue
		}
		tableName := src[m[2]:m[3]]
		ent := makeEntity("rusqlite:create_table:"+tableName, "SCOPE.Operation", "sql_query",
			file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "rusqlite",
			"target_table", tableName,
			"sql_op", "create_table",
			"provenance", "INFERRED_FROM_RUSQLITE_CREATE_TABLE",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
