// Clojure raw-DB-driver + datalog TOPOLOGY attribution (#5362, epic #5360).
//
// The Clojure framework rule manifests under internal/engine/rules/clojure/orms/
// DETECT that next.jdbc / clojure.java.jdbc / HoneySQL / Datomic / DataScript are
// present, but no pass attributed a query CALL SITE to the table/attribute it
// touches. This file closes that gap by emitting the SAME QUERIES edge shape the
// other languages emit (`caller → Class:<resource>`, operation, orm), so the
// Clojure persistence topology reaches sibling parity with the JS/Go/Java drivers.
//
// Covered idioms (the dominant, statically-resolvable forms — dynamic names are
// honest-skipped):
//
//   - next.jdbc / clojure.java.jdbc (SQL string in a vector)
//     (jdbc/execute!     ds ["SELECT * FROM users WHERE id = ?" id])
//     (jdbc/execute-one! ds ["INSERT INTO orders (..) VALUES (..)" ..])
//     table + verb parsed from the SQL string literal (reuses extractSQLTable).
//   - next.jdbc.sql friendly fns (table as a keyword/symbol arg)
//     (sql/insert! ds :users {...})   (sql/query ds ...)   (sql/update! ds :users ..)
//     (sql/delete! ds :users ..)      (sql/find-by-keys ds :users {...})
//     table is the keyword arg directly after the datasource.
//   - HoneySQL data DSL
//     {:select [:*] :from [:users] :where [:= :id 1]}   (h/from :users)
//     table from the `:from` clause; op from :select/:insert-into/:update/:delete-from.
//   - Datalog (Datomic / DataScript) — honest partial
//     (d/q '[:find ?e :where [?e :user/email ?v]] db)
//     no single "table" exists; the queried attribute NAMESPACE (`user` from
//     `:user/email`) is the closest resolvable resource, emitted as op=find.
//
// Resource names are singularised + capitalised via capitalisedSingular so the
// edge target matches the same Class:<Model> shape the resolver keys on.
package engine

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Detection gates (cheap import/marker pre-filters)
// ---------------------------------------------------------------------------

func mentionsClojureJDBC(src string) bool {
	return strings.Contains(src, "next.jdbc") ||
		strings.Contains(src, "clojure.java.jdbc") ||
		strings.Contains(src, "jdbc/execute") ||
		strings.Contains(src, "(sql/insert!") ||
		strings.Contains(src, "(sql/query") ||
		strings.Contains(src, "(sql/update!") ||
		strings.Contains(src, "(sql/delete!") ||
		strings.Contains(src, "(sql/find-by-keys")
}

func mentionsClojureHoneySQL(src string) bool {
	return strings.Contains(src, "honey.sql") ||
		strings.Contains(src, "honeysql") ||
		strings.Contains(src, "(h/from") ||
		strings.Contains(src, "(sql/format")
}

func mentionsClojureDatalog(src string) bool {
	return strings.Contains(src, "datomic.") ||
		strings.Contains(src, "datascript") ||
		strings.Contains(src, "(d/q ") ||
		strings.Contains(src, "[:find")
}

// ---------------------------------------------------------------------------
// next.jdbc / clojure.java.jdbc
// ---------------------------------------------------------------------------

// cljJdbcSQLCallRe matches a JDBC execute call whose SQL lives in a vector
// string literal: `(jdbc/execute! ds ["SELECT ..." args])`. Capture group 1 is
// the SQL text. The vector-wrapped SQL string is the next.jdbc idiom (the
// vector carries the parameterised args after the SQL).
var cljJdbcSQLCallRe = regexp.MustCompile(
	`\((?:jdbc/execute!|jdbc/execute-one!|jdbc/execute|sql/query)\b[^[]*\[\s*"((?:[^"\\]|\\.)*)"`,
)

// cljJdbcFriendlyRe matches a next.jdbc.sql friendly fn whose target table is a
// keyword/symbol arg right after the datasource:
//
//	(sql/insert! ds :users {...})
//	(sql/update! ds :users {...} {...})
//	(sql/delete! ds :users {...})
//	(sql/find-by-keys ds :users {...})
//	(sql/get-by-id ds :users id)
//
// Capture group 1 is the verb; group 2 is the table keyword/symbol name.
var cljJdbcFriendlyRe = regexp.MustCompile(
	`\(sql/(insert!|insert-multi!|update!|delete!|query|find-by-keys|get-by-id)\s+\S+\s+:?([A-Za-z_][\w.-]*)`,
)

func scanClojureJDBC(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsClojureJDBC(src) {
		return
	}
	// Raw SQL-string idiom — table + verb come from the SQL itself.
	for _, m := range cljJdbcSQLCallRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		sql := src[m[2]:m[3]]
		table, verb, isJoin := extractSQLTable(sql)
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(table), sqlOp(verb), "", "next.jdbc", isJoin)
	}
	// Friendly-fn idiom — table is the keyword arg after the datasource.
	for _, m := range cljJdbcFriendlyRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		verb := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(table), cljJdbcFriendlyOp(verb), "", "next.jdbc", false)
	}
}

// cljJdbcFriendlyOp canonicalises a next.jdbc.sql friendly-fn verb.
func cljJdbcFriendlyOp(verb string) string {
	switch verb {
	case "insert!", "insert-multi!":
		return "create"
	case "update!":
		return "update"
	case "delete!":
		return "delete"
	default: // query, find-by-keys, get-by-id
		return "find"
	}
}

// ---------------------------------------------------------------------------
// HoneySQL
// ---------------------------------------------------------------------------

// cljHoneyMapRe matches a HoneySQL data-DSL map literal carrying a `:from`
// clause: `{:select [...] :from [:users] ...}` / `{:from [:users] :update ...}`.
// Capture group 1 is the table keyword/symbol inside the `:from` vector. The
// `:from` clause is HoneySQL's canonical table marker.
var cljHoneyFromRe = regexp.MustCompile(
	`:from\s+\[\s*:?([A-Za-z_][\w.-]*)`,
)

// cljHoneyInsertRe matches the insert/update/delete table-target clauses:
//
//	{:insert-into :users ...}      {:insert-into [:users] ...}
//	{:update :users ...}           {:update [:users] ...}
//	{:delete-from :users ...}      {:delete-from [:users] ...}
//
// Capture group 1 is the clause; group 2 is the table keyword/symbol.
var cljHoneyDMLRe = regexp.MustCompile(
	`:(insert-into|update|delete-from)\s+\[?\s*:?([A-Za-z_][\w.-]*)`,
)

func scanClojureHoneySQL(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsClojureHoneySQL(src) {
		return
	}
	// SELECT-style: `:from [:table]`.
	for _, m := range cljHoneyFromRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		table := src[m[2]:m[3]]
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(table), "find", "", "honeysql", false)
	}
	// DML: insert-into / update / delete-from.
	for _, m := range cljHoneyDMLRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		clause := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(table), cljHoneyDMLOp(clause), "", "honeysql", false)
	}
}

func cljHoneyDMLOp(clause string) string {
	switch clause {
	case "insert-into":
		return "create"
	case "update":
		return "update"
	case "delete-from":
		return "delete"
	default:
		return "find"
	}
}

// ---------------------------------------------------------------------------
// Datalog (Datomic / DataScript) — honest partial (attribute-namespace target)
// ---------------------------------------------------------------------------

// cljDatalogQueryRe matches a datalog query form whose `:where` clauses carry
// namespaced attribute keywords: `[?e :user/email ?v]`. We capture each
// namespaced attribute (`user/email`); the attribute NAMESPACE (`user`) is the
// closest resolvable resource — datalog has no single table. Group 1 is the
// attribute namespace.
var cljDatalogAttrRe = regexp.MustCompile(
	`\[\s*\?[\w-]+\s+:([A-Za-z_][\w-]*)/[\w-]+`,
)

// cljDatalogQHeadRe locates the head of a `(d/q ...)` / `(q ...)` form so we
// only harvest attribute namespaces that live inside an actual datalog query,
// not from unrelated map literals elsewhere in the file.
var cljDatalogQHeadRe = regexp.MustCompile(`\((?:d/q|q)\s`)

func scanClojureDatalog(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsClojureDatalog(src) {
		return
	}
	// Walk each `(d/q ...)` form, harvest the namespaced attributes inside its
	// paren-balanced body, and emit a find edge to each distinct namespace.
	for _, head := range cljDatalogQHeadRe.FindAllStringIndex(src, -1) {
		start := head[0]
		body := datalogFormBody(src, start)
		if body == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, start)
		seen := map[string]bool{}
		for _, am := range cljDatalogAttrRe.FindAllStringSubmatch(body, -1) {
			if len(am) < 2 {
				continue
			}
			ns := am[1]
			// Skip Datomic's reserved `db` schema namespace (:db/ident, :db/id, …):
			// it describes the schema layer, not a domain entity.
			if ns == "" || ns == "db" {
				continue
			}
			if seen[ns] {
				continue
			}
			seen[ns] = true
			emit(caller, capitalisedSingular(ns), "find", "", "datalog", false)
		}
	}
}

// datalogFormBody returns the paren-balanced body of the `(d/q ...)` form whose
// opening `(` is at or after start, capped so a single malformed form cannot
// scan the whole file.
func datalogFormBody(src string, start int) string {
	open := strings.IndexByte(src[start:], '(')
	if open < 0 {
		return ""
	}
	abs := start + open
	depth := 0
	for i := abs; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[abs+1 : i]
			}
		}
	}
	return src[abs+1:]
}
