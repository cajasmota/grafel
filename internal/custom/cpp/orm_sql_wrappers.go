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
// Status (#5026): regex heuristic, no AST. Both string-literal SQL *and*
// intra-file variable-built SQL are attributed:
//   - `std::string sql = "..."`, `auto sql = "..."`, `const char* sql = "..."`
//   - `sql += "..."` concatenation and `sql << "..."` streamed builds
// When a wrapper call passes a bare identifier (e.g. `stmt.prepare(sql)`,
// `nanodbc::execute(conn, sql)`) instead of a string literal, the identifier is
// resolved against the file-local var→SQL map. Cross-FILE dataflow (SQL built
// in another translation unit) remains a gap.
//
// sql_table records the first referenced table (back-compat); sql_tables records
// ALL tables referenced by FROM/INTO/UPDATE/JOIN/TABLE clauses (multi-table
// JOINs / CTEs / subqueries), comma-joined and de-duplicated.

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

// reSQLTable extracts table names from a SQL string for the common
// FROM/INTO/UPDATE/JOIN/TABLE clauses. All matches are collected (#5026) so
// multi-table JOINs / subqueries / CTEs surface every referenced table.
var reSQLTable = regexp.MustCompile(`(?is)\b(?:FROM|INTO|UPDATE|JOIN|TABLE)\s+` +
	"[`\"\\[]?([A-Za-z_][A-Za-z0-9_.]*)[`\"\\]]?")

// classifySQL returns the SQL verb (or "QUERY" when unknown), the first-match
// table name ("" when none), and ALL distinct referenced tables (in source
// order) for a SQL string.
func classifySQL(sqlText string) (verb, table string, tables []string) {
	sqlUpper := strings.ToUpper(strings.TrimSpace(sqlText))
	verb = "QUERY"
	for _, kw := range sqlVerbKeywords {
		if strings.HasPrefix(sqlUpper, kw) {
			verb = kw
			break
		}
	}
	seenTbl := map[string]bool{}
	for _, m := range reSQLTable.FindAllStringSubmatch(sqlText, -1) {
		t := m[1]
		if seenTbl[t] {
			continue
		}
		seenTbl[t] = true
		tables = append(tables, t)
		if table == "" {
			table = t
		}
	}
	return verb, table, tables
}

// emitSQLQuery builds a SCOPE.Operation(query) record for one SQL string.
func emitSQLQuery(framework, provenance, sqlText, filePath string, lineNum int) types.EntityRecord {
	verb, table, tables := classifySQL(sqlText)
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
	if len(tables) > 0 {
		props = append(props, "sql_tables", strings.Join(tables, ","))
	}
	setProps(&ent, props...)
	return ent
}

// ============================================================================
// Intra-file variable-built SQL resolution (#5026)
// ============================================================================

var (
	// std::string sql = "..."; / auto sql = "..."; / const char* sql = "...";
	// std::string sql{"..."}; — declaration with a string-literal initializer.
	// Capture: (1) var name, (2) SQL literal.
	reSQLVarDecl = regexp.MustCompile(`(?s)\b(?:std\s*::\s*string|auto|const\s+char\s*\*|char\s*\*|string)\s+([A-Za-z_]\w*)\s*(?:=|\{)\s*"([^"]*)"`)

	// sql = "..."; / sql += "..."; / sql << "..."; — assignment, concatenation,
	// or stream-append onto an existing string variable.
	// Capture: (1) var name, (2) operator-less, (3) SQL fragment.
	reSQLVarAppend = regexp.MustCompile(`(?m)\b([A-Za-z_]\w*)\s*(?:\+?=|<<)\s*"([^"]*)"`)
)

// collectSQLVars builds a file-local map of identifier → assembled SQL text by
// scanning declarations, `+=`/`=` assignments, and `<<` stream-appends. Multiple
// fragments for the same variable are concatenated in source order so a
// streamed/concatenated build (`sql = "SELECT ..."; sql += " WHERE ...";`)
// resolves to the full statement. Best-effort, intra-file only.
func collectSQLVars(src string) map[string]string {
	if !strings.Contains(src, "\"") {
		return nil
	}
	vars := map[string]string{}
	// Declarations first (establish the variable + its leading fragment).
	for _, m := range reSQLVarDecl.FindAllStringSubmatch(src, -1) {
		name, frag := m[1], m[2]
		if looksLikeSQL(frag) {
			vars[name] = frag
		}
	}
	// Then assignments / concatenations / stream-appends in source order.
	for _, m := range reSQLVarAppend.FindAllStringSubmatch(src, -1) {
		name, frag := m[1], m[2]
		existing, known := vars[name]
		if !known {
			// A bare `sql = "SELECT ..."` first-assignment also seeds the map.
			if looksLikeSQL(frag) {
				vars[name] = frag
			}
			continue
		}
		vars[name] = joinSQLFragments(existing, frag)
	}
	return vars
}

// looksLikeSQL is a cheap guard so arbitrary string variables (paths, messages)
// don't get mistaken for SQL: the fragment must start with a known SQL verb or
// look like a clause continuation (whitespace/keyword) we can append.
func looksLikeSQL(frag string) bool {
	u := strings.ToUpper(strings.TrimSpace(frag))
	for _, kw := range sqlVerbKeywords {
		if strings.HasPrefix(u, kw) {
			return true
		}
	}
	return false
}

// joinSQLFragments concatenates two SQL fragments inserting a single space when
// neither side already supplies separating whitespace.
func joinSQLFragments(a, b string) string {
	if a == "" {
		return b
	}
	if strings.HasSuffix(a, " ") || strings.HasPrefix(b, " ") {
		return a + b
	}
	return a + " " + b
}

// sqlEmitter accumulates SCOPE.Operation(query) records, de-duplicating by line
// so overlapping literal/variable regexes never double-emit for one call site.
type sqlEmitter struct {
	out  []types.EntityRecord
	seen map[int]bool
	src  string
	path string
	vars map[string]string
}

func newSQLEmitter(src, path string) *sqlEmitter {
	return &sqlEmitter{seen: map[int]bool{}, src: src, path: path, vars: collectSQLVars(src)}
}

// addLiteral emits one query per string-literal capture (group 1 = SQL text).
func (s *sqlEmitter) addLiteral(re *regexp.Regexp, framework, provenance string) {
	for _, m := range re.FindAllStringSubmatchIndex(s.src, -1) {
		lineNum := lineOf(s.src, m[0])
		if s.seen[lineNum] {
			continue
		}
		s.seen[lineNum] = true
		s.out = append(s.out, emitSQLQuery(framework, provenance, s.src[m[2]:m[3]], s.path, lineNum))
	}
}

// addVar emits one query per identifier capture (group 1 = var name) that
// resolves to known SQL via the file-local var→SQL map.
func (s *sqlEmitter) addVar(re *regexp.Regexp, framework, provenance string) {
	for _, m := range re.FindAllStringSubmatchIndex(s.src, -1) {
		lineNum := lineOf(s.src, m[0])
		if s.seen[lineNum] {
			continue
		}
		ident := s.src[m[2]:m[3]]
		sqlText, ok := s.vars[ident]
		if !ok {
			continue
		}
		s.seen[lineNum] = true
		s.out = append(s.out, emitSQLQuery(framework, provenance, sqlText, s.path, lineNum))
	}
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

	// Variable forms (#5026): SQL passed as a bare identifier rather than a
	// string literal. Resolved against the file-local var→SQL map.
	//   nanodbc::execute(conn, sql) / nanodbc::prepare(stmt, sql)
	reNanodbcFreeFnVar = regexp.MustCompile(`\bnanodbc\s*::\s*(?:execute|prepare)\s*\([^,"]+,\s*([A-Za-z_]\w*)\s*[,)]`)
	//   nanodbc::statement stmt(conn, sql)
	reNanodbcStmtCtorVar = regexp.MustCompile(`\bnanodbc\s*::\s*statement\s+[A-Za-z_]\w*\s*\([^,"]+,\s*([A-Za-z_]\w*)\s*[,)]`)
	//   stmt.prepare(sql) / conn.execute(sql) — prepared-then-bound two-step form.
	reNanodbcMethodVar = regexp.MustCompile(`\.\s*(?:prepare|execute)\s*\(\s*([A-Za-z_]\w*)\s*[,)]`)
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

	em := newSQLEmitter(src, file.Path)
	// Literal forms first so a literal call site isn't re-matched as a variable.
	em.addLiteral(reNanodbcFreeFn, "nanodbc", "INFERRED_FROM_NANODBC_FREE_FN")
	em.addLiteral(reNanodbcStmtCtor, "nanodbc", "INFERRED_FROM_NANODBC_STATEMENT")
	em.addLiteral(reNanodbcMethod, "nanodbc", "INFERRED_FROM_NANODBC_METHOD")
	// Variable forms (#5026): SQL passed as a bare identifier.
	em.addVar(reNanodbcFreeFnVar, "nanodbc", "INFERRED_FROM_NANODBC_FREE_FN_VAR")
	em.addVar(reNanodbcStmtCtorVar, "nanodbc", "INFERRED_FROM_NANODBC_STATEMENT_VAR")
	em.addVar(reNanodbcMethodVar, "nanodbc", "INFERRED_FROM_NANODBC_METHOD_VAR")

	span.SetAttributes(attribute.Int("entity_count", len(em.out)))
	return em.out, nil
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

	// Variable forms (#5026): SQL passed as a bare identifier.
	//   SQLite::Statement query(db, sql)
	reSQLiteCppStmtVar = regexp.MustCompile(`\bSQLite\s*::\s*Statement\s+[A-Za-z_]\w*\s*\([^,"]+,\s*([A-Za-z_]\w*)\s*[,)]`)
	//   db.exec(sql)
	reSQLiteCppExecVar = regexp.MustCompile(`\.\s*exec\s*\(\s*([A-Za-z_]\w*)\s*[,)]`)
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

	em := newSQLEmitter(src, file.Path)
	em.addLiteral(reSQLiteCppStmt, "sqlitecpp", "INFERRED_FROM_SQLITECPP_STATEMENT")
	em.addLiteral(reSQLiteCppExec, "sqlitecpp", "INFERRED_FROM_SQLITECPP_EXEC")
	em.addVar(reSQLiteCppStmtVar, "sqlitecpp", "INFERRED_FROM_SQLITECPP_STATEMENT_VAR")
	em.addVar(reSQLiteCppExecVar, "sqlitecpp", "INFERRED_FROM_SQLITECPP_EXEC_VAR")

	span.SetAttributes(attribute.Int("entity_count", len(em.out)))
	return em.out, nil
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

	// Variable forms (#5026): SQL passed as a bare identifier.
	//   sqlite3_prepare_v2(db, sql, -1, &stmt, NULL) — when sql is a variable,
	//   it is often `sql.c_str()`; accept an optional `.c_str()`/`.data()` call.
	reSQLiteCAPIPrepareVar = regexp.MustCompile(`\bsqlite3_prepare(?:_v2|_v3|16)?\s*\([^,"]+,\s*([A-Za-z_]\w*)\s*(?:\.\s*(?:c_str|data)\s*\(\s*\))?\s*,`)
	//   sqlite3_exec(db, sql, …)
	reSQLiteCAPIExecVar = regexp.MustCompile(`\bsqlite3_exec\s*\([^,"]+,\s*([A-Za-z_]\w*)\s*(?:\.\s*(?:c_str|data)\s*\(\s*\))?\s*,`)
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

	em := newSQLEmitter(src, file.Path)
	em.addLiteral(reSQLiteCAPIPrepare, "sqlite_direct_c_api", "INFERRED_FROM_SQLITE3_PREPARE")
	em.addLiteral(reSQLiteCAPIExec, "sqlite_direct_c_api", "INFERRED_FROM_SQLITE3_EXEC")
	em.addVar(reSQLiteCAPIPrepareVar, "sqlite_direct_c_api", "INFERRED_FROM_SQLITE3_PREPARE_VAR")
	em.addVar(reSQLiteCAPIExecVar, "sqlite_direct_c_api", "INFERRED_FROM_SQLITE3_EXEC_VAR")

	span.SetAttributes(attribute.Int("entity_count", len(em.out)))
	return em.out, nil
}
