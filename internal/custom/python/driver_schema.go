package python

// driver_schema.go — raw-driver schema extraction for the MySQL, PostgreSQL,
// and SQLite Python drivers (pymysql / mysqlclient, psycopg2 / asyncpg,
// sqlite3).
//
// Issue #3189 — Raw driver schema_extraction extractor (mysql/postgres/sqlite).
//
// Pattern: raw drivers do not model schema as Python classes; instead the
// schema is embedded in SQL string literals passed to `cursor.execute(...)`
// (or `cur.executescript(...)`, `conn.execute(...)`, etc.). This extractor
// scans for `CREATE TABLE` SQL embedded in such calls, parses the table name
// and column definitions from the SQL body, and emits SCOPE.Schema entities:
//
//   - one table-level entity per `CREATE TABLE`
//   - one column-level entity (subtype=column) per column definition
//
// This is heuristic (regex over embedded SQL string literals), so the
// corresponding registry cells are flipped to `partial`, not `full`.

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
	extractor.Register("python_driver_schema", &DriverSchemaExtractor{})
}

// DriverSchemaExtractor emits SCOPE.Schema entities for CREATE TABLE
// statements embedded in raw-driver `cursor.execute(...)` calls.
type DriverSchemaExtractor struct{}

func (e *DriverSchemaExtractor) Language() string { return "python_driver_schema" }

var (
	// driverImportRe gates the extractor: the file must import one of the
	// three supported raw drivers. Without one of these we bail early so we
	// don't misattribute ORM-generated DDL (SQLAlchemy etc.) to a raw driver.
	driverImportRe = regexp.MustCompile(
		`(?m)^\s*(?:import|from)\s+(pymysql|MySQLdb|mysql\.connector|psycopg2|psycopg|asyncpg|sqlite3|aiosqlite)\b`)

	// driverExecuteRe matches a driver execute-style call whose first
	// argument begins a string literal. We capture the call name to record
	// provenance and the opening quote so we can read the literal.
	//   cursor.execute("CREATE TABLE ...")
	//   cur.executescript('''CREATE TABLE ...''')
	//   await conn.execute(r"CREATE TABLE ...")
	driverExecuteRe = regexp.MustCompile(
		`(?:\.|\b)(execute|executescript|executemany)\s*\(\s*[rbuRBU]*("""|'''|"|')`)

	// createTableRe matches the head of a CREATE TABLE statement inside an
	// SQL string and captures the (optionally schema-qualified, optionally
	// quoted) table name.
	createTableRe = regexp.MustCompile(
		"(?is)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"']?([A-Za-z_][A-Za-z0-9_.]*)[`\"']?\\s*\\(")

	// sqlColumnRe matches a single column definition line inside the
	// parenthesised body of a CREATE TABLE. Captures the column name and the
	// type token that follows it.
	//   id INTEGER PRIMARY KEY
	//   `name` VARCHAR(255) NOT NULL
	//   "created_at" TIMESTAMP DEFAULT now()
	sqlColumnRe = regexp.MustCompile(
		"(?im)^\\s*[`\"']?([A-Za-z_][A-Za-z0-9_]*)[`\"']?\\s+([A-Za-z][A-Za-z0-9_]*)")

	// sqlConstraintLead flags lines that open a table-level constraint rather
	// than a column definition; these are skipped.
	sqlConstraintLead = map[string]bool{
		"PRIMARY": true, "FOREIGN": true, "UNIQUE": true, "CONSTRAINT": true,
		"CHECK": true, "KEY": true, "INDEX": true, "FULLTEXT": true, "SPATIAL": true,
	}
)

// driverFor maps an import-detected driver token to a stable framework label.
func driverFrameworks(source string) []string {
	var fws []string
	add := func(token, fw string) {
		if regexp.MustCompile(`(?m)^\s*(?:import|from)\s+` + regexp.QuoteMeta(token) + `\b`).MatchString(source) {
			fws = append(fws, fw)
		}
	}
	add("pymysql", "mysql")
	add("MySQLdb", "mysql")
	add("mysql.connector", "mysql")
	add("psycopg2", "postgres")
	add("psycopg", "postgres")
	add("asyncpg", "postgres")
	add("sqlite3", "sqlite")
	add("aiosqlite", "sqlite")
	return fws
}

// closingDelim returns the delimiter that terminates a string literal opened
// with the given opening delimiter.
func closingDelim(open string) string { return open }

// readStringLiteral returns the contents of a string literal that starts at
// position open (the index of the first delimiter char) given the opening
// delimiter. It returns the literal body and the index just past the closing
// delimiter. If unterminated, end is len(source).
func readStringLiteral(source string, bodyStart int, delim string) (string, int) {
	idx := strings.Index(source[bodyStart:], delim)
	if idx < 0 {
		return source[bodyStart:], len(source)
	}
	return source[bodyStart : bodyStart+idx], bodyStart + idx + len(delim)
}

// firstFramework picks a deterministic framework when the SQL dialect is
// ambiguous: prefer the order mysql, postgres, sqlite.
func firstFramework(fws []string) string {
	for _, want := range []string{"mysql", "postgres", "sqlite"} {
		for _, f := range fws {
			if f == want {
				return want
			}
		}
	}
	if len(fws) > 0 {
		return fws[0]
	}
	return ""
}

func (e *DriverSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_driver_schema")
	_, span := tracer.Start(ctx, "custom.python_driver_schema")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	source := string(file.Content)

	// Cheap gates: must import a supported driver and contain CREATE TABLE.
	if !driverImportRe.MatchString(source) {
		return nil, nil
	}
	if !strings.Contains(strings.ToUpper(source), "CREATE TABLE") {
		return nil, nil
	}

	fws := driverFrameworks(source)
	framework := firstFramework(fws)
	if framework == "" {
		return nil, nil
	}

	var out []types.EntityRecord
	seenTables := make(map[string]bool)

	for _, idx := range allMatchesIndex(driverExecuteRe, source) {
		callName := source[idx[2]:idx[3]]
		delim := source[idx[4]:idx[5]]
		bodyStart := idx[5]
		literal, _ := readStringLiteral(source, bodyStart, closingDelim(delim))

		// Parse every CREATE TABLE inside this literal (executescript may hold
		// several).
		for _, ct := range createTableRe.FindAllStringSubmatchIndex(literal, -1) {
			tableName := literal[ct[2]:ct[3]]
			// Strip a schema qualifier (public.users -> users) for the name.
			displayTable := tableName
			if dot := strings.LastIndex(displayTable, "."); dot >= 0 {
				displayTable = displayTable[dot+1:]
			}

			// Line number = line of the execute call + offset of CREATE inside
			// the literal.
			tableLine := lineOf(source, idx[0]) + strings.Count(literal[:ct[0]], "\n")

			if !seenTables[displayTable] {
				seenTables[displayTable] = true
				out = append(out, entity(displayTable, "SCOPE.Schema", "", file.Path, tableLine,
					map[string]string{
						"framework":    framework,
						"pattern_type": "table",
						"table_name":   displayTable,
						"driver_call":  callName,
						"source":       "raw_sql_ddl",
					}))
			}

			// Extract the parenthesised column body.
			bodyOpen := ct[1] // index just past the "(" captured by createTableRe
			colBody := balancedParenBody(literal, bodyOpen-1)
			for _, cm := range sqlColumnRe.FindAllStringSubmatchIndex(colBody, -1) {
				colName := colBody[cm[2]:cm[3]]
				colType := colBody[cm[4]:cm[5]]
				if sqlConstraintLead[strings.ToUpper(colName)] {
					continue
				}
				colLine := tableLine + strings.Count(colBody[:cm[0]], "\n")
				out = append(out, entity(displayTable+"."+colName, "SCOPE.Schema", "column", file.Path, colLine,
					map[string]string{
						"framework":    framework,
						"pattern_type": "column",
						"column_type":  strings.ToUpper(colType),
						"parent_table": displayTable,
						"source":       "raw_sql_ddl",
					}))
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// balancedParenBody returns the text between the parenthesis at openIdx and
// its matching close paren (exclusive of both). openIdx must point at a "(".
func balancedParenBody(s string, openIdx int) string {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '(' {
		return ""
	}
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i]
			}
		}
	}
	return s[openIdx+1:]
}
