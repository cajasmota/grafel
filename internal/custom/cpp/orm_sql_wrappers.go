package cpp

// orm_sql_wrappers.go — query attribution for three detection-only C/C++
// datastore wrappers (#4978, follow-up from #4926):
//
//  1. custom_cpp_nanodbc  — nanodbc thin ODBC wrapper
//     Detects:
//     - nanodbc::execute(conn, "SQL")            → query_attribution
//     - nanodbc::prepare(stmt, "SQL")            → query_attribution
//     - nanodbc::statement stmt(conn, "SQL")     → query_attribution
//     - stmt.prepare("SQL") / conn.execute("SQL")→ query_attribution
//
//  2. custom_cpp_sqlitecpp — SQLiteCpp RAII SQLite wrapper
//     Detects:
//     - SQLite::Statement query(db, "SQL")       → query_attribution
//     - db.exec("SQL")                           → query_attribution
//
//  3. custom_cpp_sqlite_capi — direct SQLite3 C API
//     Detects:
//     - sqlite3_prepare_v2(db, "SQL", …)         → query_attribution
//     - sqlite3_exec(db, "SQL", …)               → query_attribution
//
// All three are thin SQL execution wrappers (no ORM model/relationship/lazy
// layer), so they emit ONLY:
//   SCOPE.Operation (subtype="query") with sql_verb + sql_text + sql_table.
//
// Status: partial (regex heuristic, no AST). String-literal SQL only; SQL built
// at runtime / spread across variables is a known cross-file dataflow gap.

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_nanodbc", &nanodbcExtractor{})
	extractor.Register("custom_cpp_sqlitecpp", &sqliteCppExtractor{})
	extractor.Register("custom_cpp_sqlite_capi", &sqliteCAPIExtractor{})
}

// sqlVerbKeywords is the verb-classification set shared by the SQL-wrapper
// extractors, ordered so multi-word prefixes never shadow a shorter sibling.
var sqlVerbKeywords = []string{
	"SELECT", "INSERT", "UPDATE", "DELETE", "REPLACE",
	"CREATE", "DROP", "ALTER", "TRUNCATE", "PRAGMA", "WITH",
}

// reSQLTable extracts the primary table name from a SQL string for the common
// FROM/INTO/UPDATE/JOIN/TABLE clauses. Best-effort: the first match wins.
var reSQLTable = regexp.MustCompile(`(?is)\b(?:FROM|INTO|UPDATE|JOIN|TABLE)\s+` +
	"[`\"\\[]?([A-Za-z_][A-Za-z0-9_.]*)[`\"\\]]?")

// classifySQL returns the SQL verb (or "QUERY" when unknown) and the best-effort
// primary table name ("" when none could be parsed) for a SQL string literal.
func classifySQL(sqlText string) (verb, table string) {
	sqlUpper := strings.ToUpper(strings.TrimSpace(sqlText))
	verb = "QUERY"
	for _, kw := range sqlVerbKeywords {
		if strings.HasPrefix(sqlUpper, kw) {
			verb = kw
			break
		}
	}
	if m := reSQLTable.FindStringSubmatch(sqlText); m != nil {
		table = m[1]
	}
	return verb, table
}

// emitSQLQuery builds a SCOPE.Operation(query) record for one SQL string literal.
func emitSQLQuery(framework, provenance, sqlText, filePath string, lineNum int) types.EntityRecord {
	verb, table := classifySQL(sqlText)
	queryName := verb + "@L" + strconv.Itoa(lineNum)
	ent := makeEntity(queryName, "SCOPE.Operation", "query", filePath, "cpp", lineNum)
	props := []string{
		"framework", framework,
		"provenance", provenance,
		"sql_verb", verb,
		"sql_text", truncate(sqlText, 120),
	}
	if table != "" {
		props = append(props, "sql_table", table)
	}
	setProps(&ent, props...)
	return ent
}

// ============================================================================
// nanodbc — thin ODBC wrapper
// ============================================================================

type nanodbcExtractor struct{}

func (e *nanodbcExtractor) Language() string { return "custom_cpp_nanodbc" }

var (
	// nanodbc::execute(conn, "SQL") / nanodbc::prepare(stmt, "SQL")
	// Capture: (1) SQL string literal (second argument).
	reNanodbcFreeFn = regexp.MustCompile(`(?s)\bnanodbc\s*::\s*(?:execute|prepare)\s*\([^,]+,\s*"([^"]*)"`)

	// nanodbc::statement stmt(conn, "SQL")
	// Capture: (1) SQL string literal (second ctor argument).
	reNanodbcStmtCtor = regexp.MustCompile(`(?s)\bnanodbc\s*::\s*statement\s+[A-Za-z_]\w*\s*\([^,]+,\s*"([^"]*)"`)

	// stmt.prepare("SQL") / conn.execute("SQL") — method form on a nanodbc handle.
	reNanodbcMethod = regexp.MustCompile(`\.\s*(?:prepare|execute)\s*\(\s*"([^"]*)"`)
)

func (e *nanodbcExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.nanodbc_extractor.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "nanodbc"),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" && file.Language != "c" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "nanodbc") {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := map[int]bool{} // dedup by line so overlapping regexes don't double-emit

	add := func(re *regexp.Regexp, provenance string) {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			sqlText := src[m[2]:m[3]]
			lineNum := lineOf(src, m[0])
			if seen[lineNum] {
				continue
			}
			seen[lineNum] = true
			out = append(out, emitSQLQuery("nanodbc", provenance, sqlText, file.Path, lineNum))
		}
	}
	add(reNanodbcFreeFn, "INFERRED_FROM_NANODBC_FREE_FN")
	add(reNanodbcStmtCtor, "INFERRED_FROM_NANODBC_STATEMENT")
	add(reNanodbcMethod, "INFERRED_FROM_NANODBC_METHOD")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// SQLiteCpp — RAII SQLite wrapper
// ============================================================================

type sqliteCppExtractor struct{}

func (e *sqliteCppExtractor) Language() string { return "custom_cpp_sqlitecpp" }

var (
	// SQLite::Statement query(db, "SQL")
	// Capture: (1) SQL string literal (second ctor argument).
	reSQLiteCppStmt = regexp.MustCompile(`(?s)\bSQLite\s*::\s*Statement\s+[A-Za-z_]\w*\s*\([^,]+,\s*"([^"]*)"`)

	// db.exec("SQL") / database.exec("SQL") — direct exec on a SQLiteCpp Database.
	// Capture: (1) SQL string literal.
	reSQLiteCppExec = regexp.MustCompile(`\.\s*exec\s*\(\s*"([^"]*)"`)
)

func (e *sqliteCppExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.sqlitecpp_extractor.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "sqlitecpp"),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" && file.Language != "c" {
		return nil, nil
	}
	src := string(file.Content)
	// Gate on the SQLiteCpp namespace/include — `.exec(` alone is too generic.
	if !strings.Contains(src, "SQLiteCpp") && !strings.Contains(src, "SQLite::") {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := map[int]bool{}

	add := func(re *regexp.Regexp, provenance string) {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			sqlText := src[m[2]:m[3]]
			lineNum := lineOf(src, m[0])
			if seen[lineNum] {
				continue
			}
			seen[lineNum] = true
			out = append(out, emitSQLQuery("sqlitecpp", provenance, sqlText, file.Path, lineNum))
		}
	}
	add(reSQLiteCppStmt, "INFERRED_FROM_SQLITECPP_STATEMENT")
	add(reSQLiteCppExec, "INFERRED_FROM_SQLITECPP_EXEC")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// SQLite direct C API
// ============================================================================

type sqliteCAPIExtractor struct{}

func (e *sqliteCAPIExtractor) Language() string { return "custom_cpp_sqlite_capi" }

var (
	// sqlite3_prepare_v2(db, "SQL", …) / sqlite3_prepare(db, "SQL", …)
	// Capture: (1) SQL string literal (second argument).
	reSQLiteCAPIPrepare = regexp.MustCompile(`(?s)\bsqlite3_prepare(?:_v2|_v3|16)?\s*\([^,]+,\s*"([^"]*)"`)

	// sqlite3_exec(db, "SQL", …)
	// Capture: (1) SQL string literal (second argument).
	reSQLiteCAPIExec = regexp.MustCompile(`(?s)\bsqlite3_exec\s*\([^,]+,\s*"([^"]*)"`)
)

func (e *sqliteCAPIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.sqlite_capi_extractor.extract",
		trace.WithAttributes(
			attribute.String("file_path", file.Path),
			attribute.String("framework", "sqlite_direct_c_api"),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" && file.Language != "c" {
		return nil, nil
	}
	src := string(file.Content)
	if !strings.Contains(src, "sqlite3_") {
		return nil, nil
	}

	var out []types.EntityRecord
	seen := map[int]bool{}

	add := func(re *regexp.Regexp, provenance string) {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			sqlText := src[m[2]:m[3]]
			lineNum := lineOf(src, m[0])
			if seen[lineNum] {
				continue
			}
			seen[lineNum] = true
			out = append(out, emitSQLQuery("sqlite_direct_c_api", provenance, sqlText, file.Path, lineNum))
		}
	}
	add(reSQLiteCAPIPrepare, "INFERRED_FROM_SQLITE3_PREPARE")
	add(reSQLiteCAPIExec, "INFERRED_FROM_SQLITE3_EXEC")

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
