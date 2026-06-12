// Package dbmap — per-ORM and raw-SQL detection logic.
//
// Each ORM entry declares:
//   - Name (used as the `orm` property on SCOPE.DataAccess entities)
//   - Import markers that must be present in the file to enable detection
//   - A detector function that scans the source and yields access records
//
// Detection order is deterministic: all ORMs whose import hints match are
// run, in declaration order. The first matched ORM is reported on the OTel
// span as the primary ORM for observability purposes.
package dbmap

import (
	"regexp"
	"strings"
)

// ormDetector runs an ORM-specific scan over source bytes.
type ormDetector func(source string) []access

// ormEntry describes how to recognise and scan a single ORM / driver.
type ormEntry struct {
	name        string   // canonical ORM name ("gorm", "sqlalchemy", …)
	importHints []string // substrings checked against import tokens
	detect      ormDetector
}

// ---------------------------------------------------------------------------
// Import list extraction (shared with endpoint package logic, duplicated
// intentionally to keep dbmap self-contained — no cross-package coupling).
// ---------------------------------------------------------------------------

// importTokenRE captures common import/require tokens across languages.
var importTokenRE = regexp.MustCompile(
	`(?mi)(?:import|from|require|use|using|open|package)\s+["']?([\w@][\w\-./:]*)["']?`,
)

// importCallRE captures function-style import forms: `require('x')` / `import('x')`.
var importCallRE = regexp.MustCompile(
	`(?mi)\b(?:require|import)\s*\(\s*["']([\w@][\w\-./:]*)["']\s*\)`,
)

// quotedImportRE captures quoted import paths inside Go-style grouped/blank
// import blocks where the token does not directly follow the `import`
// keyword:
//
//	import (
//	    _ "github.com/lib/pq"
//	    "database/sql"
//	)
//
// It deliberately requires the path to contain a `/` or `.` so it does not
// match arbitrary string literals (e.g. SQL fragments) — only module paths.
var quotedImportRE = regexp.MustCompile(
	`(?m)^\s*(?:import\s+)?(?:[\w.]+\s+)?"([\w@][\w\-]*[./][\w\-./:]*)"\s*$`,
)

// extractImportTokens returns the lower-cased set of import tokens found in source.
func extractImportTokens(source string) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		if raw == "" {
			return
		}
		tok := strings.ToLower(raw)
		out[tok] = true
		if idx := strings.IndexAny(tok, "/."); idx > 0 {
			out[tok[:idx]] = true
		}
	}
	for _, m := range importTokenRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range importCallRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range quotedImportRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

// matchesAnyImport reports whether any hint is present in the token set.
func matchesAnyImport(tokens map[string]bool, hints []string) bool {
	for _, h := range hints {
		hLower := strings.ToLower(h)
		if tokens[hLower] {
			return true
		}
		for t := range tokens {
			if strings.Contains(t, hLower) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Enclosing function discovery
// ---------------------------------------------------------------------------

// fnHeaderRE matches common function declarations across the supported
// languages. It is intentionally permissive — when more than one matches,
// enclosingFunc picks the last header preceding the hit position.
var fnHeaderRE = regexp.MustCompile(
	`(?m)^\s*(?:func|def|fn|function|async\s+function|async\s+def|public\s+[\w<>,\[\] ]+|private\s+[\w<>,\[\] ]+|protected\s+[\w<>,\[\] ]+|static\s+[\w<>,\[\] ]+)\s+(\w+)\s*\(`,
)

// enclosingFunc returns the name of the function whose header most recently
// appeared before pos. Returns "" when no candidate exists.
func enclosingFunc(source string, pos int) string {
	if pos <= 0 {
		return ""
	}
	slice := source[:pos]
	matches := fnHeaderRE.FindAllStringSubmatchIndex(slice, -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	if len(last) < 4 {
		return ""
	}
	return slice[last[2]:last[3]]
}

// ---------------------------------------------------------------------------
// Raw SQL detection (shared by every ORM that hands through string literals
// and always runs as a fallback pass when a DB driver import is present).
// ---------------------------------------------------------------------------

// sqlLiteralRE captures SQL-looking string literals in the source. A string
// literal is considered SQL-like if it starts with (SELECT | INSERT | UPDATE
// | DELETE | TRUNCATE | WITH | UPSERT) followed by a space. Both single
// and double quotes are supported; backtick-quoted strings (JS template
// literals) are matched separately.
var (
	sqlSingleQuoteRE = regexp.MustCompile(
		`(?is)'\s*(SELECT|INSERT|UPDATE|DELETE|TRUNCATE|WITH|UPSERT)\b[^']{0,800}'`,
	)
	sqlDoubleQuoteRE = regexp.MustCompile(
		`(?is)"\s*(SELECT|INSERT|UPDATE|DELETE|TRUNCATE|WITH|UPSERT)\b[^"]{0,800}"`,
	)
	sqlBacktickRE = regexp.MustCompile(
		"(?is)`\\s*(SELECT|INSERT|UPDATE|DELETE|TRUNCATE|WITH|UPSERT)\\b[^`]{0,800}`",
	)
)

// tableClauseRE extracts table names from FROM, INTO, UPDATE, JOIN, and
// TRUNCATE clauses. The captured name may include a schema qualifier, which
// is kept as-is (dots preserved).
var tableClauseRE = regexp.MustCompile(
	`(?i)(?:\bFROM|\bINTO|\bUPDATE|\bJOIN|\bTRUNCATE(?:\s+TABLE)?)\s+([A-Za-z_][\w.]*)`,
)

// sqlOperationOf returns the operation keyword that leads a SQL statement.
func sqlOperationOf(sql string) string {
	s := strings.TrimSpace(strings.ToUpper(sql))
	switch {
	case strings.HasPrefix(s, "SELECT"), strings.HasPrefix(s, "WITH"):
		return OpSelect
	case strings.HasPrefix(s, "INSERT"):
		return OpInsert
	case strings.HasPrefix(s, "UPDATE"):
		return OpUpdate
	case strings.HasPrefix(s, "DELETE"):
		return OpDelete
	case strings.HasPrefix(s, "TRUNCATE"):
		return OpTruncate
	case strings.HasPrefix(s, "UPSERT"):
		return OpUpsert
	}
	return ""
}

// detectRawSQL scans for SQL string literals and emits one access per
// table referenced in each statement (JOINs → multiple records).
func detectRawSQL(source string) []access {
	var out []access
	seen := map[string]bool{}

	scan := func(hit string, pos int) {
		// Strip the surrounding quote character.
		if len(hit) < 2 {
			return
		}
		body := hit[1 : len(hit)-1]
		op := sqlOperationOf(body)
		if op == "" {
			return
		}
		fn := enclosingFunc(source, pos)
		tableMatches := tableClauseRE.FindAllStringSubmatch(body, -1)
		if len(tableMatches) == 0 {
			key := "raw|" + op + "|" + UnknownTable + "|" + fn
			if seen[key] {
				return
			}
			seen[key] = true
			out = append(out, access{
				table:         UnknownTable,
				operation:     op,
				orm:           "raw",
				pattern:       body,
				functionQName: fn,
			})
			return
		}
		for _, tm := range tableMatches {
			if len(tm) < 2 {
				continue
			}
			table := strings.ToLower(tm[1])
			key := "raw|" + op + "|" + table + "|" + fn
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, access{
				table:         table,
				operation:     op,
				orm:           "raw",
				pattern:       body,
				functionQName: fn,
			})
		}
	}

	for _, m := range sqlSingleQuoteRE.FindAllStringIndex(source, -1) {
		scan(source[m[0]:m[1]], m[0])
	}
	for _, m := range sqlDoubleQuoteRE.FindAllStringIndex(source, -1) {
		scan(source[m[0]:m[1]], m[0])
	}
	for _, m := range sqlBacktickRE.FindAllStringIndex(source, -1) {
		scan(source[m[0]:m[1]], m[0])
	}
	return out
}

// ---------------------------------------------------------------------------
// Go — GORM
// ---------------------------------------------------------------------------

// gormCallRE matches a chained GORM call such as:
//
//	db.Where("age > ?", 18).Find(&users)
//	db.Create(&order)
//	db.Table("orders").Delete(&Order{})
//
// The inner model receiver is extracted from the pointer-to-struct or
// pointer-to-slice argument; the table name is inferred via
// modelNameToTable (snake_case + pluralise).
var gormCallRE = regexp.MustCompile(
	`(?m)\b(?:Find|First|Last|Take|Pluck|Scan|Create|Save|Updates?|Delete|FirstOrCreate|FirstOrInit)\s*\(\s*&?(\w+)(?:\{[^}]*\})?`,
)

// gormTableCallRE matches .Table("name") hints that override the inferred
// table name.
var gormTableRE = regexp.MustCompile(`(?m)\.Table\s*\(\s*"([^"]+)"\s*\)`)

// gormModelRE matches .Model(&Struct{}) hints. When a verb follows that
// does not itself carry a model argument (common with Updates), the model
// from the preceding .Model call supplies the table.
var gormModelRE = regexp.MustCompile(`(?m)\.Model\s*\(\s*&?(\w+)(?:\{[^}]*\})?`)

// gormMethodOp maps a GORM call verb to a SQL operation.
func gormMethodOp(verb string) string {
	switch verb {
	case "Find", "First", "Last", "Take", "Pluck", "Scan":
		return OpSelect
	case "Create", "FirstOrCreate", "FirstOrInit":
		return OpInsert
	case "Update", "Updates":
		return OpUpdate
	case "Delete":
		return OpDelete
	case "Save":
		return OpUpsert // ambiguous — per Error-Handling rule #2
	}
	return ""
}

// gormVerbRE captures the verb token so we can dispatch to gormMethodOp.
var gormVerbRE = regexp.MustCompile(
	`\b(Find|First|Last|Take|Pluck|Scan|Create|Save|Updates?|Delete|FirstOrCreate|FirstOrInit)\b`,
)

// modelNameToTable converts a Go struct name to the GORM table name using
// the Go-community snake_case plural convention. E.g. User → users,
// OrderItem → order_items, Person → persons (simple s-suffix — good enough
// for the minimum-detection contract).
func modelNameToTable(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if !strings.HasSuffix(s, "s") {
		s += "s"
	}
	return s
}

func detectGORM(source string) []access {
	// First pass: find explicit .Table("…") overrides keyed by byte range.
	// For simplicity the override is applied when a .Table() call appears
	// on the same line or within 120 bytes before the verb call.
	var out []access
	tableOverrides := gormTableRE.FindAllStringSubmatchIndex(source, -1)
	modelOverrides := gormModelRE.FindAllStringSubmatchIndex(source, -1)

	lookupTable := func(pos int) string {
		best := ""
		for _, m := range tableOverrides {
			if m[1] <= pos && pos-m[1] < 120 {
				best = source[m[2]:m[3]]
			}
		}
		return best
	}
	lookupModel := func(pos int) string {
		best := ""
		for _, m := range modelOverrides {
			if m[1] <= pos && pos-m[1] < 120 {
				best = source[m[2]:m[3]]
			}
		}
		return best
	}

	// We treat receiver arguments that do not start with an uppercase
	// letter as a "bare variable" — in that case the table must come
	// from a preceding .Model() or .Table() call.
	isModelLike := func(name string) bool {
		return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
	}

	for _, m := range gormCallRE.FindAllStringSubmatchIndex(source, -1) {
		verb := ""
		if vm := gormVerbRE.FindStringSubmatch(source[m[0]:m[1]]); len(vm) >= 2 {
			verb = vm[1]
		}
		op := gormMethodOp(verb)
		if op == "" {
			continue
		}
		model := source[m[2]:m[3]]
		table := lookupTable(m[0])
		if table == "" {
			switch {
			case isModelLike(model):
				// PascalCase — treat as model class name.
				table = modelNameToTable(model)
			default:
				if mm := lookupModel(m[0]); mm != "" {
					// Variable receiver — take the table from a
					// preceding .Model(&Struct{}) call.
					table = modelNameToTable(mm)
				} else {
					// Variable receiver with no Model override —
					// assume the variable itself is named after
					// the slice it holds (e.g. `&users` → "users").
					low := strings.ToLower(model)
					if strings.HasSuffix(low, "s") {
						table = low
					} else {
						table = low + "s"
					}
				}
			}
		}
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "gorm",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Go — database/sql (driver-level raw SQL)
// ---------------------------------------------------------------------------

// detectDatabaseSQL delegates to detectRawSQL — every hit is tagged with
// the "database_sql" ORM name rather than "raw" so the final entity says
// exactly which driver surface was used.
func detectDatabaseSQL(source string) []access {
	return retagRaw(detectRawSQL(source), "database_sql")
}

// detectNpgsqlFSharp delegates to the shared raw-SQL scanner for the
// Npgsql.FSharp data driver (#5000). The driver's idiomatic surface is a
// `Sql.query "SELECT … FROM users"` string literal (single-, double-, or
// triple-quoted) passed into the fluent `Sql.connect … |> Sql.query …`
// pipeline; detectRawSQL already recognises the SQL string literal and
// parses FROM/INTO/UPDATE/JOIN table clauses, so the F# table attribution
// is wired through the same extractor the C#/Crystal precedent uses. Every
// hit is retagged "npgsql_fsharp" so the SCOPE.DataAccess entity records
// exactly which driver surface was used.
func detectNpgsqlFSharp(source string) []access {
	return retagRaw(detectRawSQL(source), "npgsql_fsharp")
}

// detectDapperFSharp delegates to the shared raw-SQL scanner for Dapper /
// Dapper.FSharp on the F# stack (#5000). Dapper hands a string-literal SQL
// statement into `conn.Query<T>("SELECT … FROM …")` / `conn.Execute(...)`;
// detectRawSQL recognises the literal and attributes the table(s). Hits are
// retagged "dapper_fsharp".
func detectDapperFSharp(source string) []access {
	return retagRaw(detectRawSQL(source), "dapper_fsharp")
}

// retagRaw rewrites the ORM field of every raw-SQL hit. Used by driver-level
// detectors that piggy-back on the shared SQL scanner.
func retagRaw(in []access, orm string) []access {
	out := make([]access, 0, len(in))
	for _, a := range in {
		a.orm = orm
		out = append(out, a)
	}
	return out
}

// ---------------------------------------------------------------------------
// Python — SQLAlchemy
// ---------------------------------------------------------------------------

// SQLAlchemy ORM style: session.query(User).filter(...).all()
// Core style: session.execute(select(users).where(...)) — we match both.
var sqlalchemyQueryRE = regexp.MustCompile(
	`(?m)\b(?:session|db\.session)\.(query|add|delete|merge)\s*\(\s*(\w+)`,
)

// SQLAlchemy class-body __tablename__ declarations map a model class to
// an explicit table. We keep a class→table map for this file.
var sqlalchemyTablenameRE = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+)[^:]*:[^=]*?__tablename__\s*=\s*['"]([^'"]+)['"]`,
)

func detectSQLAlchemy(source string) []access {
	// Build class→table lookup.
	tableByClass := map[string]string{}
	for _, m := range sqlalchemyTablenameRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 3 {
			continue
		}
		tableByClass[m[1]] = m[2]
	}

	var out []access
	for _, m := range sqlalchemyQueryRE.FindAllStringSubmatchIndex(source, -1) {
		verb := source[m[2]:m[3]]
		model := source[m[4]:m[5]]

		var op string
		switch verb {
		case "query":
			op = OpSelect
		case "add":
			op = OpInsert
		case "delete":
			op = OpDelete
		case "merge":
			op = OpUpsert
		}
		if op == "" {
			continue
		}
		table, ok := tableByClass[model]
		if !ok {
			table = modelNameToTable(model)
		}
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "sqlalchemy",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Python — psycopg2
// ---------------------------------------------------------------------------

// psycopg2 exposes cursor.execute("SQL …", params). We locate calls to
// .execute( and then reuse detectRawSQL's literal-aware scanner on the
// whole source (safer than trying to re-parse argument lists with regex).
func detectPsycopg2(source string) []access {
	return retagRaw(detectRawSQL(source), "psycopg2")
}

// ---------------------------------------------------------------------------
// Java — Hibernate / JPA
// ---------------------------------------------------------------------------

// @Entity + @Table(name="…") declarations at class level plus JPQL queries
// inside createQuery("…") and createNativeQuery("…"). For JPQL (FROM Entity e)
// we map the entity to its @Table name if a mapping exists, else to the
// lower-cased entity class name.
var jpaEntityTableRE = regexp.MustCompile(
	`(?ms)@Entity\b[^@]*?@Table\s*\(\s*name\s*=\s*"([^"]+)"[^)]*\)[^@]*?class\s+(\w+)`,
)

var jpaCreateQueryRE = regexp.MustCompile(
	`(?i)createQuery\s*\(\s*"\s*([^"]{1,800})"`,
)

var jpaCreateNativeQueryRE = regexp.MustCompile(
	`(?i)createNativeQuery\s*\(\s*"\s*([^"]{1,800})"`,
)

var jpqlFromRE = regexp.MustCompile(`(?i)\bFROM\s+(\w+)`)

func detectJPA(source string) []access {
	// Build entity→table map.
	tableByEntity := map[string]string{}
	for _, m := range jpaEntityTableRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			tableByEntity[m[2]] = m[1]
		}
	}

	var out []access

	// JPQL via createQuery.
	for _, m := range jpaCreateQueryRE.FindAllStringSubmatchIndex(source, -1) {
		jpql := source[m[2]:m[3]]
		op := sqlOperationOf(jpql)
		if op == "" {
			op = OpSelect
		}
		fn := enclosingFunc(source, m[0])
		for _, fm := range jpqlFromRE.FindAllStringSubmatch(jpql, -1) {
			if len(fm) < 2 {
				continue
			}
			entity := fm[1]
			table, ok := tableByEntity[entity]
			if !ok {
				table = strings.ToLower(entity)
			}
			out = append(out, access{
				table:         table,
				operation:     op,
				orm:           "hibernate",
				pattern:       jpql,
				functionQName: fn,
			})
		}
	}

	// Native queries → reuse raw SQL table extraction on the literal body.
	for _, m := range jpaCreateNativeQueryRE.FindAllStringSubmatchIndex(source, -1) {
		native := source[m[2]:m[3]]
		op := sqlOperationOf(native)
		if op == "" {
			continue
		}
		fn := enclosingFunc(source, m[0])
		for _, tm := range tableClauseRE.FindAllStringSubmatch(native, -1) {
			if len(tm) < 2 {
				continue
			}
			out = append(out, access{
				table:         strings.ToLower(tm[1]),
				operation:     op,
				orm:           "jpa",
				pattern:       native,
				functionQName: fn,
			})
		}
	}

	// Also scan for @Entity classes with no explicit queries — we still
	// emit a SELECT record so the model → table mapping is present in the
	// graph. Operation is SELECT as a placeholder.
	for entity, table := range tableByEntity {
		out = append(out, access{
			table:         table,
			operation:     OpSelect,
			orm:           "hibernate",
			functionQName: entity,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Java — plain JDBC (java.sql)
// ---------------------------------------------------------------------------

// JDBC raw SQL is passed as a string literal to Statement.executeQuery /
// executeUpdate / execute / Connection.prepareStatement / prepareCall.
// We reuse the shared raw-SQL table scanner (FROM/INTO/UPDATE/JOIN) over the
// whole source — it already handles double-quoted SELECT/INSERT/UPDATE/DELETE
// literals — and retag the hits as orm=jdbc. Files reach this detector only
// when a java.sql / javax.sql import gate matched (see ormOrder), so the
// scan is import-gated like every other driver.
//
// Honest-partial: dynamically built SQL ("SELECT * FROM " + tbl) is not a
// single literal and resolves to no table (no fabricated edge).
func detectJDBC(source string) []access {
	return retagRaw(detectRawSQL(source), "jdbc")
}

// ---------------------------------------------------------------------------
// Python — raw stdlib / third-party DB-API drivers (sqlite3, pymysql,
// mysql.connector, cx_Oracle/oracledb, pyodbc, asyncpg, aiosqlite, MySQLdb).
// These hand raw SQL strings to cursor.execute(...)/conn.execute(...). The
// shared raw-SQL scanner resolves the table(s); we retag as orm=dbapi.
// ---------------------------------------------------------------------------

func detectPyDBAPI(source string) []access {
	return retagRaw(detectRawSQL(source), "dbapi")
}

// ---------------------------------------------------------------------------
// Go — raw database/sql drivers imported directly (lib/pq, go-sql-driver,
// go-sqlite3, jackc/pgx, jmoiron/sqlx). Files using sqlx or a driver via a
// blank import may never import "database/sql" by that literal token, so we
// gate on the driver import as well and reuse the shared raw-SQL scanner.
// ---------------------------------------------------------------------------

func detectGoSQLDriver(source string) []access {
	return retagRaw(detectRawSQL(source), "go_sql_driver")
}

// ---------------------------------------------------------------------------
// Ruby — ActiveRecord
// ---------------------------------------------------------------------------

// ActiveRecord is convention-over-config: `class User < ApplicationRecord`
// maps to the `users` table. Calls like `User.find(1)` / `User.create(...)`
// are mapped to the corresponding operation.
var arModelClassRE = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+)\s*<\s*(?:ApplicationRecord|ActiveRecord::Base)\b`,
)

var arCallRE = regexp.MustCompile(
	`(?m)\b(\w+)\.(find|find_by|where|all|first|last|create|new|save|update|destroy|delete)\b`,
)

func detectActiveRecord(source string) []access {
	// First, find all AR model classes in this file.
	models := map[string]bool{}
	for _, m := range arModelClassRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			models[m[1]] = true
		}
	}

	// If the file declares no AR model, we still detect calls that look
	// like AR (Model.find(...)) and rely on the plural table name.
	opOf := map[string]string{
		"find":    OpSelect,
		"find_by": OpSelect,
		"where":   OpSelect,
		"all":     OpSelect,
		"first":   OpSelect,
		"last":    OpSelect,
		"create":  OpInsert,
		"new":     OpInsert,
		"save":    OpUpsert,
		"update":  OpUpdate,
		"destroy": OpDelete,
		"delete":  OpDelete,
	}

	var out []access
	seen := map[string]bool{}
	for _, m := range arCallRE.FindAllStringSubmatchIndex(source, -1) {
		receiver := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		op, ok := opOf[method]
		if !ok {
			continue
		}
		// Only accept receivers that look like an AR model name (PascalCase).
		if len(receiver) == 0 || receiver[0] < 'A' || receiver[0] > 'Z' {
			continue
		}
		table := modelNameToTable(receiver)
		key := "ar|" + op + "|" + table
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "activerecord",
			functionQName: enclosingFunc(source, m[0]),
		})
	}

	// Association query builders: `.joins(:orders)` / `.includes(:orders)`
	// pull in a second table via the association name. The association symbol
	// is conventionally the singular/plural table name; we pluralise it to the
	// table name (orders → orders, order → orders). Honest-partial: a string
	// or variable association arg is skipped.
	for _, m := range arJoinsRE.FindAllStringSubmatchIndex(source, -1) {
		assoc := source[m[2]:m[3]]
		table := assoc
		if !strings.HasSuffix(table, "s") {
			table += "s"
		}
		key := "ar|" + OpSelect + "|" + table
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     OpSelect,
			orm:           "activerecord",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

// arJoinsRE matches `.joins(:orders)` / `.includes(:orders)` /
// `.preload(:orders)` — association-name symbol only (string/variable args
// are intentionally not matched: honest-partial).
var arJoinsRE = regexp.MustCompile(
	`(?m)\.(?:joins|includes|preload|eager_load)\s*\(\s*:(\w+)\b`,
)

// ---------------------------------------------------------------------------
// Elixir — Ecto
// ---------------------------------------------------------------------------

// Ecto schema: `schema "users" do … end` — explicit table name literal.
// Calls: `Repo.all(User)` / `Repo.insert(%User{})` / `Repo.update(changeset)`.
var ectoSchemaRE = regexp.MustCompile(
	`(?m)^\s*schema\s+"([^"]+)"\s+do`,
)

var ectoRepoCallRE = regexp.MustCompile(
	`(?m)\bRepo\.(all|one|get|get_by|insert|insert_all|update|update_all|delete|delete_all)\b`,
)

func detectEcto(source string) []access {
	// Extract first schema declaration as the file's primary table.
	primaryTable := ""
	if m := ectoSchemaRE.FindStringSubmatch(source); len(m) >= 2 {
		primaryTable = m[1]
	}
	if primaryTable == "" {
		return nil
	}

	opOf := map[string]string{
		"all":        OpSelect,
		"one":        OpSelect,
		"get":        OpSelect,
		"get_by":     OpSelect,
		"insert":     OpInsert,
		"insert_all": OpInsert,
		"update":     OpUpdate,
		"update_all": OpUpdate,
		"delete":     OpDelete,
		"delete_all": OpDelete,
	}

	var out []access
	seen := map[string]bool{}
	for _, m := range ectoRepoCallRE.FindAllStringSubmatchIndex(source, -1) {
		method := source[m[2]:m[3]]
		op := opOf[method]
		if op == "" {
			continue
		}
		key := "ecto|" + op + "|" + primaryTable
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, access{
			table:         primaryTable,
			operation:     op,
			orm:           "ecto",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// TypeScript — Prisma
// ---------------------------------------------------------------------------

// prisma.user.findMany(...) / prisma.order.create(...)
var prismaCallRE = regexp.MustCompile(
	`(?m)\bprisma\.(\w+)\.(findMany|findUnique|findFirst|findFirstOrThrow|create|createMany|update|updateMany|upsert|delete|deleteMany|count)\b`,
)

func detectPrisma(source string) []access {
	opOf := map[string]string{
		"findMany":         OpSelect,
		"findUnique":       OpSelect,
		"findFirst":        OpSelect,
		"findFirstOrThrow": OpSelect,
		"count":            OpSelect,
		"create":           OpInsert,
		"createMany":       OpInsert,
		"update":           OpUpdate,
		"updateMany":       OpUpdate,
		"upsert":           OpUpsert,
		"delete":           OpDelete,
		"deleteMany":       OpDelete,
	}
	var out []access
	for _, m := range prismaCallRE.FindAllStringSubmatchIndex(source, -1) {
		model := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		op := opOf[method]
		if op == "" {
			continue
		}
		// Prisma exposes models in camelCase; the underlying table name
		// is typically the same camelCase identifier unless @@map() is
		// used (which we can't detect without the schema.prisma file).
		out = append(out, access{
			table:         strings.ToLower(model),
			operation:     op,
			orm:           "prisma",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// TypeScript — TypeORM
// ---------------------------------------------------------------------------

// @Entity({ name: "users" }) / @Entity("users") / @Entity class X { … }
var typeormEntityRE = regexp.MustCompile(
	`(?ms)@Entity\s*\(\s*(?:\{\s*name\s*:\s*["']([^"']+)["']|["']([^"']+)["'])?\s*[^)]*\)\s*(?:export\s+)?class\s+(\w+)`,
)

// getRepository(User).find(...)
var typeormRepoCallRE = regexp.MustCompile(
	`(?m)\bgetRepository\s*\(\s*(\w+)\s*\)\s*\.(find|findOne|findOneBy|findBy|save|insert|update|delete|remove)\b`,
)

func detectTypeORM(source string) []access {
	tableByEntity := map[string]string{}
	for _, m := range typeormEntityRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 4 {
			continue
		}
		class := m[3]
		table := m[1]
		if table == "" {
			table = m[2]
		}
		if table == "" {
			table = modelNameToTable(class)
		}
		tableByEntity[class] = table
	}

	opOf := map[string]string{
		"find":      OpSelect,
		"findOne":   OpSelect,
		"findOneBy": OpSelect,
		"findBy":    OpSelect,
		"save":      OpUpsert,
		"insert":    OpInsert,
		"update":    OpUpdate,
		"delete":    OpDelete,
		"remove":    OpDelete,
	}

	var out []access
	for _, m := range typeormRepoCallRE.FindAllStringSubmatchIndex(source, -1) {
		entity := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		op := opOf[method]
		if op == "" {
			continue
		}
		table, ok := tableByEntity[entity]
		if !ok {
			table = modelNameToTable(entity)
		}
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "typeorm",
			functionQName: enclosingFunc(source, m[0]),
		})
	}

	// Also emit a record per declared @Entity so the MAPS_TO relationship
	// has at least one anchor even when no explicit repo call is found.
	for entity, table := range tableByEntity {
		out = append(out, access{
			table:         table,
			operation:     OpSelect,
			orm:           "typeorm",
			functionQName: entity,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript — Sequelize
// ---------------------------------------------------------------------------

// sequelize.define("users", { … })
var sequelizeDefineRE = regexp.MustCompile(
	`(?m)\bsequelize\.define\s*\(\s*["']([^"']+)["']`,
)

// Model.findAll() / Model.create(...) / Model.update(...)
var sequelizeCallRE = regexp.MustCompile(
	`(?m)\b(\w+)\.(findAll|findOne|findByPk|findAndCountAll|create|bulkCreate|update|destroy|upsert|count)\b`,
)

func detectSequelize(source string) []access {
	definedTables := map[string]bool{}
	for _, m := range sequelizeDefineRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			definedTables[m[1]] = true
		}
	}

	opOf := map[string]string{
		"findAll":         OpSelect,
		"findOne":         OpSelect,
		"findByPk":        OpSelect,
		"findAndCountAll": OpSelect,
		"count":           OpSelect,
		"create":          OpInsert,
		"bulkCreate":      OpInsert,
		"update":          OpUpdate,
		"destroy":         OpDelete,
		"upsert":          OpUpsert,
	}

	var out []access
	for _, m := range sequelizeCallRE.FindAllStringSubmatchIndex(source, -1) {
		receiver := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		op := opOf[method]
		if op == "" {
			continue
		}
		if len(receiver) == 0 || receiver[0] < 'A' || receiver[0] > 'Z' {
			continue
		}
		table := strings.ToLower(receiver)
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "sequelize",
			functionQName: enclosingFunc(source, m[0]),
		})
	}

	for name := range definedTables {
		out = append(out, access{
			table:         name,
			operation:     OpSelect,
			orm:           "sequelize",
			functionQName: "",
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Rust — Diesel
// ---------------------------------------------------------------------------

// Diesel uses a `table!` macro to declare tables and `use schema::users::dsl::*`
// to bring them into scope. Calls look like `users::table.load(...)`.
var dieselTableMacroRE = regexp.MustCompile(
	`(?m)\btable!\s*\{\s*(\w+)`,
)

var dieselCallRE = regexp.MustCompile(
	`(?m)\b(\w+)::table\s*(?:\.[\w_]+\([^)]*\))*\s*\.(load|first|get_result|execute|insert_into|update|delete)\b`,
)

func detectDiesel(source string) []access {
	declared := map[string]bool{}
	for _, m := range dieselTableMacroRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			declared[m[1]] = true
		}
	}

	opOf := map[string]string{
		"load":        OpSelect,
		"first":       OpSelect,
		"get_result":  OpSelect,
		"execute":     OpUpdate, // best-effort default
		"insert_into": OpInsert,
		"update":      OpUpdate,
		"delete":      OpDelete,
	}

	var out []access
	for _, m := range dieselCallRE.FindAllStringSubmatchIndex(source, -1) {
		table := source[m[2]:m[3]]
		method := source[m[4]:m[5]]
		op := opOf[method]
		if op == "" {
			continue
		}
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "diesel",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	for name := range declared {
		out = append(out, access{
			table:         name,
			operation:     OpSelect,
			orm:           "diesel",
			functionQName: "",
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// ORM registry
// ---------------------------------------------------------------------------

// ormOrder is the deterministic detection order. Every ORM whose import
// hints match is run — the first match is reported on the OTel span as the
// "primary" ORM.
var ormOrder = []ormEntry{
	{
		name:        "gorm",
		importHints: []string{"gorm.io/gorm", "gorm"},
		detect:      detectGORM,
	},
	{
		name:        "database_sql",
		importHints: []string{"database/sql"},
		detect:      detectDatabaseSQL,
	},
	{
		name:        "sqlalchemy",
		importHints: []string{"sqlalchemy"},
		detect:      detectSQLAlchemy,
	},
	{
		name:        "psycopg2",
		importHints: []string{"psycopg2"},
		detect:      detectPsycopg2,
	},
	{
		// Raw Python DB-API drivers (stdlib + third-party). Reuses the
		// shared raw-SQL table scanner. psycopg2/sqlalchemy keep their own
		// entries above so their orm tags are preserved.
		name: "dbapi",
		importHints: []string{
			"sqlite3", "aiosqlite",
			"pymysql", "mysql.connector", "mysqldb", "mysqlclient",
			"oracledb", "cx_oracle",
			"pyodbc", "asyncpg",
		},
		detect: detectPyDBAPI,
	},
	{
		// Raw Go SQL drivers imported directly (driver token, sqlx, pgx).
		// database/sql keeps its own entry above; this catches files that
		// import only the driver / sqlx wrapper.
		name: "go_sql_driver",
		importHints: []string{
			"github.com/lib/pq",
			"github.com/go-sql-driver/mysql",
			"github.com/mattn/go-sqlite3",
			"github.com/jackc/pgx",
			"github.com/jmoiron/sqlx",
		},
		detect: detectGoSQLDriver,
	},
	{
		// Npgsql.FSharp (F# Postgres data driver, #5000). The driver's
		// idiomatic surface is a `Sql.query "SELECT … FROM users"` string
		// literal; the shared raw-SQL scanner attributes the table(s).
		// Import marker: `open Npgsql.FSharp`.
		name:        "npgsql_fsharp",
		importHints: []string{"npgsql.fsharp"},
		detect:      detectNpgsqlFSharp,
	},
	{
		// Dapper / Dapper.FSharp string-literal SQL on the F# stack (#5000).
		// `conn.Query<T>("SELECT … FROM …")` / `conn.Execute(...)` hand a SQL
		// literal that the shared raw-SQL scanner parses for table clauses.
		// Import marker: `open Dapper` (also matches Dapper.FSharp).
		name:        "dapper_fsharp",
		importHints: []string{"dapper.fsharp", "dapper"},
		detect:      detectDapperFSharp,
	},
	{
		// Plain JDBC (java.sql / javax.sql). Hibernate/JPA keep their own
		// entry below; this catches Statement.executeQuery("SELECT … FROM t")
		// style raw SQL.
		name:        "jdbc",
		importHints: []string{"java.sql", "javax.sql"},
		detect:      detectJDBC,
	},
	{
		name:        "hibernate",
		importHints: []string{"javax.persistence", "jakarta.persistence", "hibernate"},
		detect:      detectJPA,
	},
	{
		name:        "activerecord",
		importHints: []string{"activerecord", "applicationrecord"},
		detect:      detectActiveRecord,
	},
	{
		name:        "ecto",
		importHints: []string{"ecto"},
		detect:      detectEcto,
	},
	{
		name:        "prisma",
		importHints: []string{"@prisma/client", "prisma"},
		detect:      detectPrisma,
	},
	{
		name:        "typeorm",
		importHints: []string{"typeorm"},
		detect:      detectTypeORM,
	},
	{
		name:        "sequelize",
		importHints: []string{"sequelize"},
		detect:      detectSequelize,
	},
	{
		name:        "diesel",
		importHints: []string{"diesel"},
		detect:      detectDiesel,
	},
	// Fluent query BUILDERS (oracle-priority #3, ticket under #3628). These
	// name the table through a builder call, not a SQL string, so the
	// raw-SQL scanner misses them. See query_builders.go.
	{
		name:        "knex",
		importHints: []string{"knex"},
		detect:      detectKnex,
	},
	{
		name:        "drizzle",
		importHints: []string{"drizzle-orm", "drizzle"},
		detect:      detectDrizzle,
	},
	{
		name:        "jooq",
		importHints: []string{"org.jooq", "jooq"},
		detect:      detectJOOQ,
	},
	{
		name:        "querydsl",
		importHints: []string{"com.querydsl", "querydsl"},
		detect:      detectQueryDSL,
	},
	{
		// SQLAlchemy Core builder surface (select(table) / table.insert()).
		// The ORM `sqlalchemy` entry above keeps the session.query(Model)
		// surface; both run when the import gate matches.
		name:        "sqlalchemy_core",
		importHints: []string{"sqlalchemy"},
		detect:      detectSQLAlchemyCore,
	},
}

// selectORMs returns every ORM entry whose import hints match the file's
// import token set. Order is preserved from ormOrder so the first match is
// used as the "primary" ORM on the OTel span.
func selectORMs(tokens map[string]bool) []ormEntry {
	// Special-case: ActiveRecord sources rarely use `import` statements;
	// rely on the model-class sentinel instead. Same for Ecto schemas,
	// which often live in a module that only `use`s Ecto.Schema.
	var out []ormEntry
	for i := range ormOrder {
		if matchesAnyImport(tokens, ormOrder[i].importHints) {
			out = append(out, ormOrder[i])
		}
	}
	// The "database_sql" and "go_sql_driver" entries both run the shared
	// raw-SQL scanner over the same source; when a file matches both (e.g.
	// imports "database/sql" and "github.com/lib/pq") keep only the first
	// so the SCOPE.DataAccess entities are not duplicated under two orm tags.
	out = dropDuplicateRawScanner(out, "database_sql", "go_sql_driver")
	// F# data drivers (#5000) both delegate to the shared raw-SQL scanner.
	// When a single F# file matches both `Sql.query` (Npgsql.FSharp) and
	// Dapper, keep the Npgsql.FSharp tag so the same SQL literal does not
	// produce duplicate ACCESSES_TABLE edges under two orm tags.
	out = dropDuplicateRawScanner(out, "npgsql_fsharp", "dapper_fsharp")
	return out
}

// dropDuplicateRawScanner removes the `drop` entry from sel when `keep` is
// also present — both delegate to detectRawSQL and would otherwise emit
// identical table edges differing only by the orm tag.
func dropDuplicateRawScanner(sel []ormEntry, keep, drop string) []ormEntry {
	hasKeep := false
	for i := range sel {
		if sel[i].name == keep {
			hasKeep = true
			break
		}
	}
	if !hasKeep {
		return sel
	}
	out := sel[:0]
	for i := range sel {
		if sel[i].name == drop {
			continue
		}
		out = append(out, sel[i])
	}
	return out
}
