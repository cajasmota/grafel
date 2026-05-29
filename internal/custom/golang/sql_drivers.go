package golang

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// sql_drivers.go: struct-tag (`db:`) + raw-SQL extractor for the
// non-ORM Go SQL access libraries — sqlx, pgx and the sqlite driver.
//
// These libraries deliberately have NO object-relational mapping layer:
// they scan rows into structs by `db:` tag and run hand-written SQL.
// Consequently the honest coverage shape is:
//
//   - Models       — partial. Structs carrying `db:"col"` tags are
//                    recognised as schemas and their columns enumerated.
//                    A `db:` tag is a heuristic (it does not prove the
//                    struct is a table), hence partial rather than full.
//   - Schema       — partial. Columns come from `db:` tags AND from
//                    CREATE TABLE statements in SQL string literals.
//   - Queries      — partial. Exec/Query/QueryRow/Get/Select/NamedExec
//                    call sites plus their SQL string literal are captured.
//                    Binding a query to a concrete model from a regex is
//                    not reliable, so this stays partial.
//   - Relationships— foreign_key_extraction is partial (FOREIGN KEY clauses
//                    parsed from CREATE TABLE SQL). association /
//                    relationship / lazy_loading have no concept in a
//                    driver with no relation layer → honesty-NA (recorded
//                    as not_applicable in the registry, no code claim).
//   - Migrations   — partial. File-based NNN_slug.up/down.sql migrations
//                    are recognised by filename. (pgx/sqlite drivers ship
//                    no migration runner of their own; sqlx is commonly
//                    paired with golang-migrate whose files match here.)
//
// The extractor attributes each match to a driver by inspecting the
// imports actually present in the file, so a file importing only pgx is
// not tagged sqlx and vice-versa. When no recognised driver import is
// present the Go branch no-ops (file-based .sql migrations are handled
// by filename and remain driver-agnostic, tagged "sql").

func init() {
	extractor.Register("custom_go_sql_drivers", &sqlDriversExtractor{})
}

type sqlDriversExtractor struct{}

func (e *sqlDriversExtractor) Language() string { return "custom_go_sql_drivers" }

var (
	// Driver import markers. Presence of any of these in the file gates
	// the Go-source branch and selects the attributed driver name.
	reImportSqlx   = regexp.MustCompile(`"github\.com/jmoiron/sqlx"`)
	reImportPgx    = regexp.MustCompile(`"github\.com/jackc/pgx(?:/v\d+)?(?:/pgxpool)?"`)
	reImportSqlite = regexp.MustCompile(`"(?:github\.com/mattn/go-sqlite3|modernc\.org/sqlite)"`)

	// A struct field carrying a `db:"column"` tag.
	//   ID   int    `db:"id"`
	//   Name string `db:"name"`
	reDBStruct = regexp.MustCompile(`(?ms)type\s+(\w+)\s+struct\s*\{(.*?)\n\}`)
	reDBField  = regexp.MustCompile("(?m)^\\s*(\\w+)\\s+([\\w\\.\\[\\]\\*]+)\\s+`[^`]*\\bdb:\"([^\"]*)\"[^`]*`")

	// Query call sites. Captures the verb so query_type can be stamped.
	reSQLQueryCall = regexp.MustCompile(
		`(?m)\.(ExecContext|Exec|QueryxContext|QueryRowxContext|QueryRowContext|QueryContext|Queryx|QueryRowx|QueryRow|Query|GetContext|SelectContext|NamedExecContext|NamedQueryContext|NamedExec|NamedQuery|Get|Select)\s*\(`,
	)

	// SQL string literals: double-quoted or raw back-quoted strings whose
	// content starts with a SQL DML/DDL keyword (heuristic). Used both to
	// surface queries and to mine CREATE TABLE schema/FK information.
	reSQLDoubleQuoted = regexp.MustCompile(
		"(?is)\"(\\s*(?:SELECT|INSERT|UPDATE|DELETE|CREATE\\s+TABLE|ALTER\\s+TABLE)\\b[^\"]*)\"",
	)
	reSQLBackQuoted = regexp.MustCompile(
		"(?is)`(\\s*(?:SELECT|INSERT|UPDATE|DELETE|CREATE\\s+TABLE|ALTER\\s+TABLE)\\b[^`]*)`",
	)

	// CREATE TABLE <name> ( ... ) — table name + body for column/FK mining.
	reCreateTable = regexp.MustCompile(
		"(?is)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[\"`']?([A-Za-z_][A-Za-z0-9_]*)[\"`']?\\s*\\((.*)\\)",
	)
	// FOREIGN KEY (col) REFERENCES other(othercol)
	reForeignKey = regexp.MustCompile(
		"(?is)FOREIGN\\s+KEY\\s*\\(\\s*[\"`']?([A-Za-z0-9_]+)[\"`']?\\s*\\)\\s*REFERENCES\\s+[\"`']?([A-Za-z0-9_]+)[\"`']?",
	)

	// File-based migration filename: 000123_add_users.up.sql.
	reSQLMigrationFile = regexp.MustCompile(
		`^(\d+)_([A-Za-z0-9_\-]+)\.(up|down)\.sql$`,
	)
)

func (e *sqlDriversExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.sql_drivers_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "sql_drivers"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// File-based SQL migrations are recognised by filename regardless of
	// the source language, so handle them before the Go-only gate.
	base := filepath.Base(file.Path)
	if m := reSQLMigrationFile.FindStringSubmatch(base); m != nil {
		version, slug, direction := m[1], m[2], m[3]
		ent := makeEntity("migration:"+version+"_"+slug+"."+direction, "SCOPE.Schema", "migration", file.Path, file.Language, 1)
		setProps(&ent, "framework", "sql", "provenance", "INFERRED_FROM_SQL_MIGRATION_FILE",
			"migration_version", version, "migration_slug", slug, "migration_direction", direction)
		add(ent)
		span.SetAttributes(attribute.Int("entity_count", len(entities)))
		return entities, nil
	}

	if file.Language != "go" {
		return nil, nil
	}

	driver := detectSQLDriver(src)
	if driver == "" {
		// No recognised sqlx/pgx/sqlite import: not our file.
		return nil, nil
	}

	// 1. Models / Schema: structs with `db:"col"` field tags.
	for _, sm := range reDBStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])
		fields := reDBField.FindAllStringSubmatch(body, -1)
		if len(fields) == 0 {
			continue
		}
		ent := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&ent, "framework", driver, "provenance", "INFERRED_FROM_DB_STRUCT_TAGS")
		add(ent)

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			column := strings.TrimSpace(fm[3])
			// "-" means "ignore this field"; skip it as a column.
			if column == "" || column == "-" {
				continue
			}
			// Strip option suffixes such as `db:"id,omitempty"`.
			if i := strings.IndexByte(column, ','); i >= 0 {
				column = column[:i]
			}
			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", driver, "provenance", "INFERRED_FROM_DB_STRUCT_TAGS",
				"model_name", structName, "field_name", fieldName, "column_name", column, "go_type", fieldType)
			add(fieldEnt)
		}
	}

	// 2. Schema + foreign keys mined from CREATE TABLE SQL literals.
	//    Also surfaces queries from SELECT/INSERT/UPDATE/DELETE literals.
	for _, sqlLit := range collectSQLLiterals(src) {
		stmt, line := sqlLit.text, sqlLit.line
		upper := strings.ToUpper(strings.TrimSpace(stmt))
		switch {
		case strings.HasPrefix(upper, "CREATE TABLE"):
			if ct := reCreateTable.FindStringSubmatch(stmt); ct != nil {
				table := ct[1]
				tblBody := ct[2]
				tblEnt := makeEntity("table:"+table, "SCOPE.Schema", "", file.Path, file.Language, line)
				setProps(&tblEnt, "framework", driver, "provenance", "INFERRED_FROM_CREATE_TABLE",
					"table_name", table)
				add(tblEnt)
				for _, fk := range reForeignKey.FindAllStringSubmatch(tblBody, -1) {
					col, refTable := fk[1], fk[2]
					fkEnt := makeEntity("fk:"+table+"."+col, "SCOPE.Component", "relation", file.Path, file.Language, line)
					setProps(&fkEnt, "framework", driver, "provenance", "INFERRED_FROM_SQL_FOREIGN_KEY",
						"table_name", table, "foreign_key", col, "references_table", refTable,
						"relationship", "foreign_key")
					add(fkEnt)
				}
			}
		default:
			// SELECT/INSERT/UPDATE/DELETE/ALTER literal => a query.
			verb := strings.ToLower(strings.Fields(upper)[0])
			qEnt := makeEntity("sql:"+driver+":"+shortHash(stmt), "SCOPE.Operation", "query", file.Path, file.Language, line)
			setProps(&qEnt, "framework", driver, "provenance", "INFERRED_FROM_SQL_LITERAL",
				"query_type", verb, "sql", squashWhitespace(stmt))
			add(qEnt)
		}
	}

	// 3. Queries: Exec/Query/QueryRow/Get/Select/NamedExec call sites.
	//    Heuristic — captures the verb but cannot bind to a concrete model.
	for _, m := range reSQLQueryCall.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("call:"+driver+":"+verb+":"+lineToken(src, m[0]), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", driver, "provenance", "INFERRED_FROM_SQL_CALL",
			"query_type", "call", "call_verb", verb)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// detectSQLDriver returns the attributed driver name for a Go source file
// based on the database libraries it imports, or "" when none match.
// sqlx wins when both sqlx and a bare driver are present, since sqlx is
// the access layer the developer writes against.
func detectSQLDriver(src string) string {
	switch {
	case reImportSqlx.MatchString(src):
		return "sqlx"
	case reImportPgx.MatchString(src):
		return "pgx"
	case reImportSqlite.MatchString(src):
		return "sqlite"
	default:
		return ""
	}
}

// sqlLiteral is one SQL string literal found in source with its line.
type sqlLiteral struct {
	text string
	line int
}

// collectSQLLiterals returns SQL string literals (double-quoted and raw
// back-quoted) whose content begins with a SQL keyword, each paired with
// its 1-based source line.
func collectSQLLiterals(src string) []sqlLiteral {
	var out []sqlLiteral
	for _, m := range reSQLDoubleQuoted.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, sqlLiteral{text: src[m[2]:m[3]], line: lineOf(src, m[0])})
	}
	for _, m := range reSQLBackQuoted.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, sqlLiteral{text: src[m[2]:m[3]], line: lineOf(src, m[0])})
	}
	return out
}

// squashWhitespace collapses runs of whitespace (incl. newlines) to single
// spaces so multi-line SQL literals stamp as a single readable property.
func squashWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// shortHash returns a short, stable token derived from a string, used to
// give SQL-literal query entities deterministic, collision-resistant names
// without embedding the (possibly long, quote-laden) statement in the ID.
func shortHash(s string) string {
	const fnvOffset = 2166136261
	const fnvPrime = 16777619
	h := uint32(fnvOffset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= fnvPrime
	}
	const hexDigits = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = hexDigits[h&0xf]
		h >>= 4
	}
	return string(b[:])
}

// lineToken returns the source line number of offset as a decimal string,
// used to disambiguate otherwise-identical query-call entity names.
func lineToken(src string, offset int) string {
	n := lineOf(src, offset)
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
