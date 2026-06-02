// Package dbmap — fluent query-builder table detection (oracle-priority
// area #3 of #3628).
//
// The ORM detectors in orms.go resolve a table either from a raw-SQL string
// literal (FROM/INTO/UPDATE) or from a model class that names a table. Fluent
// query BUILDERS are different: they name the table through a builder call and
// never produce a SQL string at the call site. This file adds detectors for
// the common builders that the raw-SQL scanner misses entirely:
//
//   - Knex (JS/TS)        knex('users').where(...).insert(...)
//   - Drizzle (TS)        db.select().from(users) — table OBJECT resolved
//     back to its pgTable('users', …) declaration
//   - jOOQ (Java)         dsl.selectFrom(USERS) — generated table constant
//   - QueryDSL (Java)     queryFactory.selectFrom(user) — Q-type instance
//   - SQLAlchemy Core     select(users), users.insert() — Table('users', …)
//
// Every detector resolves a STATIC table-name literal (or a static
// builder-constant → table-name mapping) and emits an `access` exactly like
// the ORM detectors, so the SCOPE.DataAccess entity + ACCESSES_TABLE edge are
// byte-for-byte consistent with raw-SQL-derived ones (same entity-id shape,
// same edge Kind, same property set via buildEntity).
//
// Honest-partial contract: a dynamic table reference (a variable, not a
// literal) yields NO edge. `knex(tableVar)`, `select(unknownTbl)` resolve to
// nothing rather than fabricating a table.
package dbmap

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// JS/TS — Knex query builder
// ---------------------------------------------------------------------------

// knexBuilderRE matches the builder-entry call `knex('users')` /
// `knex("users")` and the chained terminal verb that determines the
// operation. The table name MUST be a string literal — a bare identifier
// (`knex(tableVar)`) does not match group 1 and is skipped (honest-partial).
//
//	knex('users').where({ id }).first()        → SELECT users
//	knex('users').insert({ ... })              → INSERT users
//	knex('users').where(...).update({ ... })   → UPDATE users
//	knex('users').where(...).del()             → DELETE users
//
// The verb is found independently by knexVerbRE over the remainder of the
// statement so chained where()/join() between the entry and the terminal verb
// do not break the match.
var knexBuilderRE = regexp.MustCompile(
	`(?m)\bknex\s*\(\s*['"]([A-Za-z_][\w.]*)['"]\s*\)`,
)

// knexFromRE matches the `.from('users')` / `.into('users')` table source
// used when the builder is entered via knex.select()/knex.queryBuilder().
//
//	knex.select('*').from('orders')   → SELECT orders
var knexFromRE = regexp.MustCompile(
	`(?m)\.(?:from|into|table)\s*\(\s*['"]([A-Za-z_][\w.]*)['"]\s*\)`,
)

// knexVerbRE captures the mutating verb that follows a builder entry so the
// operation can be classified. Read is the default when no mutating verb is
// present on the statement.
var knexVerbRE = regexp.MustCompile(
	`\.(insert|update|del|delete|truncate)\s*\(`,
)

// knexStatementEnd finds the end of the JS statement (the next `;` or
// newline-with-no-trailing-dot) so a verb on a LATER statement is not
// attributed to this builder entry. Coarse but adequate: query builders are
// near-universally single fluent expressions.
func knexOpFrom(stmt string) string {
	if m := knexVerbRE.FindStringSubmatch(stmt); len(m) >= 2 {
		switch m[1] {
		case "insert":
			return OpInsert
		case "update":
			return OpUpdate
		case "del", "delete":
			return OpDelete
		case "truncate":
			return OpTruncate
		}
	}
	return OpSelect
}

// detectKnex scans for knex builder entries and resolves the table from the
// string-literal argument. It runs for both `knex('t')` and the
// `.from('t')`/`.into('t')` builder-source forms.
func detectKnex(source string) []access {
	var out []access
	seen := map[string]bool{}

	emit := func(table string, pos int, stmt string) {
		table = strings.ToLower(table)
		op := knexOpFrom(stmt)
		key := "knex|" + op + "|" + table
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "knex",
			functionQName: enclosingFunc(source, pos),
		})
	}

	for _, m := range knexBuilderRE.FindAllStringSubmatchIndex(source, -1) {
		table := source[m[2]:m[3]]
		stmt := knexStatement(source, m[1])
		emit(table, m[0], stmt)
	}
	for _, m := range knexFromRE.FindAllStringSubmatchIndex(source, -1) {
		table := source[m[2]:m[3]]
		stmt := knexStatement(source, m[1])
		emit(table, m[0], stmt)
	}
	return out
}

// knexStatement returns the slice from `start` to the end of the current
// fluent statement (terminating at the first `;` or a newline that is not
// immediately continued by a `.`). Used to scope the verb search.
func knexStatement(source string, start int) string {
	end := len(source)
	for i := start; i < len(source); i++ {
		c := source[i]
		if c == ';' {
			end = i
			break
		}
		if c == '\n' {
			// Continue past the newline only if the next non-space char is a
			// dot (method chain continuation).
			j := i + 1
			for j < len(source) && (source[j] == ' ' || source[j] == '\t') {
				j++
			}
			if j < len(source) && source[j] == '.' {
				continue
			}
			end = i
			break
		}
	}
	return source[start:end]
}

// ---------------------------------------------------------------------------
// TS — Drizzle ORM (query builder over table OBJECTS)
// ---------------------------------------------------------------------------

// Drizzle declares tables with `export const users = pgTable('users', {...})`
// (or mysqlTable / sqliteTable). Queries reference the JS table OBJECT, not a
// string: `db.select().from(users)` / `db.insert(users)`. We resolve the
// object identifier back to the declared table-name literal.
var drizzleTableDeclRE = regexp.MustCompile(
	`(?m)\b(?:export\s+)?const\s+(\w+)\s*=\s*(?:pg|mysql|sqlite)Table\s*\(\s*['"]([^'"]+)['"]`,
)

// db.select()....from(orders) / db.insert(users).values(...) /
// db.update(users).set(...) / db.delete(users)
var drizzleFromRE = regexp.MustCompile(
	`(?m)\.from\s*\(\s*(\w+)\s*\)`,
)
var drizzleWriteRE = regexp.MustCompile(
	`(?m)\bdb\s*\.\s*(insert|update|delete)\s*\(\s*(\w+)\s*\)`,
)

func detectDrizzle(source string) []access {
	tableByObj := map[string]string{}
	for _, m := range drizzleTableDeclRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			tableByObj[m[1]] = m[2]
		}
	}
	if len(tableByObj) == 0 {
		// No resolvable table object in this file — honest-partial: skip
		// rather than guess the object name is the table name.
		return nil
	}

	var out []access
	seen := map[string]bool{}
	emit := func(obj string, op string, pos int) {
		table, ok := tableByObj[obj]
		if !ok {
			return // dynamic / cross-file table object — skip.
		}
		key := "drizzle|" + op + "|" + table
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "drizzle",
			functionQName: enclosingFunc(source, pos),
		})
	}

	for _, m := range drizzleFromRE.FindAllStringSubmatchIndex(source, -1) {
		emit(source[m[2]:m[3]], OpSelect, m[0])
	}
	for _, m := range drizzleWriteRE.FindAllStringSubmatchIndex(source, -1) {
		verb := source[m[2]:m[3]]
		var op string
		switch verb {
		case "insert":
			op = OpInsert
		case "update":
			op = OpUpdate
		case "delete":
			op = OpDelete
		}
		emit(source[m[4]:m[5]], op, m[0])
	}
	return out
}

// ---------------------------------------------------------------------------
// Java — jOOQ (generated table constants)
// ---------------------------------------------------------------------------

// jOOQ generates a constant per table (e.g. `Tables.USERS` or a static-imported
// `USERS`) and queries read like `dsl.selectFrom(USERS)` /
// `dsl.insertInto(USERS)` / `dsl.update(USERS)` / `dsl.deleteFrom(USERS)`.
// The constant name maps to the table by lower-casing it (the jOOQ codegen
// default), which is the best static resolution available without the
// generated sources.
var jooqCallRE = regexp.MustCompile(
	`(?m)\.(selectFrom|insertInto|update|deleteFrom|truncate)\s*\(\s*(?:[\w.]+\.)?([A-Z][A-Z0-9_]*)\b`,
)

func jooqOp(verb string) string {
	switch verb {
	case "selectFrom":
		return OpSelect
	case "insertInto":
		return OpInsert
	case "update":
		return OpUpdate
	case "deleteFrom":
		return OpDelete
	case "truncate":
		return OpTruncate
	}
	return ""
}

func detectJOOQ(source string) []access {
	var out []access
	seen := map[string]bool{}
	for _, m := range jooqCallRE.FindAllStringSubmatchIndex(source, -1) {
		verb := source[m[2]:m[3]]
		constant := source[m[4]:m[5]]
		op := jooqOp(verb)
		if op == "" {
			continue
		}
		table := strings.ToLower(constant)
		key := "jooq|" + op + "|" + table
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "jooq",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Java — QueryDSL (generated Q-type instances)
// ---------------------------------------------------------------------------

// QueryDSL generates a `QUser` class with a static `user` instance. Queries
// read `queryFactory.selectFrom(user)` / `.from(qOrder)`. The instance name is
// the entity name (often un-prefixed); we resolve the table by stripping a
// leading `q`/`Q` and lower-casing + pluralising the entity name, mirroring
// the JPA default-table convention already used by detectJPA.
var querydslCallRE = regexp.MustCompile(
	`(?m)\b(?:selectFrom|from|update|delete)\s*\(\s*(q[A-Z]\w*|[a-z]\w*)\s*\)`,
)

// querydslImportHint distinguishes QueryDSL files (com.querydsl.*) so the Q-type
// heuristic does not fire on unrelated `.from(x)` calls. Enforced by the
// import gate in ormOrder; here we additionally require the matched identifier
// to look like a Q-instance (lower-camel entity name).
func detectQueryDSL(source string) []access {
	var out []access
	seen := map[string]bool{}
	for _, m := range querydslCallRE.FindAllStringSubmatchIndex(source, -1) {
		ident := source[m[2]:m[3]]
		// Resolve the entity name: a QUser-style instance is conventionally
		// the lower-camel entity name (`user`, `orderItem`). Strip an optional
		// leading `q` prefix used by some teams (`qUser`).
		entity := ident
		if strings.HasPrefix(entity, "q") && len(entity) > 1 &&
			entity[1] >= 'A' && entity[1] <= 'Z' {
			entity = strings.ToLower(entity[1:2]) + entity[2:]
		}
		table := modelNameToTable(strings.Title(entity))
		// Operation: QueryDSL fetches are reads; mutations use update/delete
		// clauses. Classify from the verb token preceding the identifier.
		op := OpSelect
		pre := source[maxInt(0, m[0]-12):m[0]]
		switch {
		case strings.Contains(pre, "update"):
			op = OpUpdate
		case strings.Contains(pre, "delete"):
			op = OpDelete
		}
		key := "querydsl|" + op + "|" + table
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "querydsl",
			functionQName: enclosingFunc(source, m[0]),
		})
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Python — SQLAlchemy Core (Table objects + functional construct)
// ---------------------------------------------------------------------------

// SQLAlchemy Core defines tables as `users = Table('users', metadata, …)` and
// queries them via the functional construct `select(users)` or the table
// method builders `users.insert()` / `users.update()` / `users.delete()`.
// The existing detectSQLAlchemy handles only the ORM `session.query(Model)`
// surface; this resolves the Core builder surface.
var saCoreTableDeclRE = regexp.MustCompile(
	`(?m)\b(\w+)\s*=\s*Table\s*\(\s*['"]([^'"]+)['"]`,
)

// select(users) / select([users]) — functional read construct.
var saCoreSelectRE = regexp.MustCompile(
	`(?m)\bselect\s*\(\s*\[?\s*(\w+)\b`,
)

// users.insert() / users.update() / users.delete() — table method builders.
var saCoreWriteRE = regexp.MustCompile(
	`(?m)\b(\w+)\.(insert|update|delete)\s*\(`,
)

func detectSQLAlchemyCore(source string) []access {
	tableByObj := map[string]string{}
	for _, m := range saCoreTableDeclRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			tableByObj[m[1]] = m[2]
		}
	}
	if len(tableByObj) == 0 {
		return nil // no Core Table() objects → nothing to resolve.
	}

	var out []access
	seen := map[string]bool{}
	emit := func(obj, op string, pos int) {
		table, ok := tableByObj[obj]
		if !ok {
			return // not a known Table object → skip (dynamic / ORM model).
		}
		key := "sacore|" + op + "|" + table
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, access{
			table:         table,
			operation:     op,
			orm:           "sqlalchemy_core",
			functionQName: enclosingFunc(source, pos),
		})
	}

	for _, m := range saCoreSelectRE.FindAllStringSubmatchIndex(source, -1) {
		emit(source[m[2]:m[3]], OpSelect, m[0])
	}
	for _, m := range saCoreWriteRE.FindAllStringSubmatchIndex(source, -1) {
		obj := source[m[2]:m[3]]
		verb := source[m[4]:m[5]]
		var op string
		switch verb {
		case "insert":
			op = OpInsert
		case "update":
			op = OpUpdate
		case "delete":
			op = OpDelete
		}
		emit(obj, op, m[0])
	}
	return out
}
