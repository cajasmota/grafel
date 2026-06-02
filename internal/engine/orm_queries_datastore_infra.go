// Cross-language datastore-infra resource attribution for Neo4j, ClickHouse
// and Snowflake (sibling parity with cassandra/mongodb/elasticsearch/dynamodb).
//
// The cross-language driver-topology pass in orm_queries_drivers_other.go
// already attributes raw-driver call sites for Cassandra (CQL), MongoDB,
// DynamoDB and Elasticsearch to the resource they touch, emitting a QUERIES
// edge `Function:<caller> → Class:<Resource>`. That edge IS the
// dependency-attribution + resource-extraction mechanism: the `Class:<Resource>`
// node is the deployed datastore resource (graph label / table) and the edge
// records which code module depends on it.
//
// Before this file, three sibling datastores had no such cross-language
// attribution despite being first-class code-level citizens elsewhere:
//
//   - Neo4j      : the JS pass (orm_queries_jsts_drivers.go) recognised
//     `session.run("MATCH (n:Label) ...")` and emitted the node label, but
//     the cross-language driver pass (C#/PHP/Rust/Python/Java/Ruby) did not.
//   - ClickHouse : speaks SQL over its own client/driver; no attribution.
//   - Snowflake  : speaks SQL over the snowflake-connector / SQLAlchemy
//     snowflake dialect; no attribution.
//
// This file closes that gap by mirroring emitCQLTargets EXACTLY:
//
//   - emitCypherTargets  — Neo4j: pulls the Cypher string out of a
//     `session.run(...)` / `tx.run(...)` call, parses the first node label
//     `(n:Label)` out of it (reusing the shared cypherLabelRe / cypherVerbRe
//     from orm_queries_jsts_drivers.go), and emits a QUERIES edge to the
//     `Class:<Label>` resource. Cypher whose label is not a static literal
//     is honest-skipped — same precision bar as the CQL/dynamic-name skips.
//   - emitClickHouseTargets / emitSnowflakeTargets — SQL datastores: pull the
//     SQL string out of an execute/query call and reuse the shared
//     extractSQLTable() FROM/INTO/UPDATE parser, emitting a QUERIES edge to
//     the `Class:<Table>` resource. SQL whose table cannot be statically
//     parsed (runtime-built string) is honest-skipped.
//
// Gating is import-/driver-sniff based (mentions*), identical to the existing
// driver families, so the broad `.run(`/`.execute(`/`.query(` surface only
// fires in files that actually import the datastore's client.
package engine

import (
	"regexp"
	"strings"
)

// scanDatastoreInfraDrivers runs the Neo4j / ClickHouse / Snowflake
// datastore-infra resource attribution over `src`. Each family is
// import-gated, so this is cheap for files that touch none of them. Called
// from applyORMQueries for every supported language (the call shapes —
// `session.run(...)`, `cursor.execute("SQL")` — are language-agnostic).
func scanDatastoreInfraDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	emitCypherTargets(src, funcs, emit)
	emitClickHouseTargets(src, funcs, emit)
	emitSnowflakeTargets(src, funcs, emit)
}

// ---------------------------------------------------------------------------
// Neo4j (Cypher node-label attribution — cross-language)
// ---------------------------------------------------------------------------

// cypherRunRe matches a Neo4j driver `session.run(...)` / `tx.run(...)` /
// `txc.run(...)` / `driver.run(...)` call across languages. The receiver is
// gated to the canonical Neo4j session/transaction identifiers so the broad
// `.run(` surface does not fire on unrelated runners. Group 1 = the opening
// paren, used to walk the call args.
var cypherRunRe = regexp.MustCompile(
	`\b(?:session|tx|txc|tx2|driver|neo4j|_session|graph|graphDb|graphDB)\.[Rr]un\s*\(`,
)

// mentionsNeo4jDriver reports whether the file imports / references a Neo4j
// driver in any supported language: the official drivers expose `bolt://`
// connection URLs and `neo4j`/`Neo4j` package symbols; neomodel/neogma/neogma
// OGMs and the Spring/`GraphDatabase` Java API all reference the same tokens.
func mentionsNeo4jDriver(src string) bool {
	return strings.Contains(src, "neo4j") ||
		strings.Contains(src, "Neo4j") ||
		strings.Contains(src, "bolt://") ||
		strings.Contains(src, "neobolt://") ||
		strings.Contains(src, "GraphDatabase") ||
		strings.Contains(src, "neomodel") ||
		strings.Contains(src, "neogma")
}

// emitCypherTargets finds every Neo4j `session.run(<cypher>)` call matched by
// cypherRunRe, pulls the Cypher string literal out of the call's first
// argument, parses the first node label `(n:Label)` out of it, and emits a
// QUERIES edge to that label. Cypher whose label cannot be statically parsed
// (a runtime-built query string, or a parameterised label) is honest-skipped.
func emitCypherTargets(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsNeo4jDriver(src) {
		return
	}
	for _, m := range cypherRunRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 2 {
			continue
		}
		// The matcher ends at the opening paren of the call.
		argsBlob := matchCall(src, m[1]-1, 4096)
		cypher := firstStringLiteral(argsBlob)
		if cypher == "" {
			continue
		}
		lm := cypherLabelRe.FindStringSubmatch(cypher)
		if lm == nil {
			continue
		}
		label := lm[1]
		op := "find"
		if vm := cypherVerbRe.FindStringSubmatch(cypher); vm != nil {
			switch strings.ToLower(vm[1]) {
			case "create", "merge":
				op = "create"
			case "set":
				op = "update"
			case "delete", "remove":
				op = "delete"
			}
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, label, op, "", "neo4j", false)
	}
}

// ---------------------------------------------------------------------------
// ClickHouse (SQL-over-HTTP/native client — table attribution)
// ---------------------------------------------------------------------------

// clickhouseExecRe matches a ClickHouse client query/execute/insert call:
// `client.execute("SELECT ... FROM events")` (python clickhouse-driver),
// `client.query("...")` (clickhouse-connect / clickhouse-go), `db.Query(...)`
// (Go database/sql with the clickhouse driver), `client.insert(...)`.
// The receiver is left open because the SQL string carries the table; the
// import gate (mentionsClickHouse) keeps the broad surface from over-firing.
var clickhouseExecRe = regexp.MustCompile(
	`\b(?:[A-Za-z_$][\w$]*)\.(?:[Ee]xecute|[Qq]uery|[Ii]nsert|command|exec)\s*\(`,
)

// mentionsClickHouse reports whether the file imports / references a
// ClickHouse client or driver: the python `clickhouse-driver` /
// `clickhouse_connect`, the Go `clickhouse-go` (`ClickHouse/clickhouse-go`),
// the JS `@clickhouse/client`, the JDBC `clickhouse-jdbc` URL, or a native
// `clickhouse://` / `http://...:8123` endpoint.
func mentionsClickHouse(src string) bool {
	s := strings.ToLower(src)
	return strings.Contains(s, "clickhouse") ||
		strings.Contains(s, "clickhouse://") ||
		strings.Contains(s, ":8123")
}

// emitClickHouseTargets finds every ClickHouse execute/query call, pulls the
// SQL string out of its first argument, parses the FROM/INTO/UPDATE table out
// of it (via the shared extractSQLTable), and emits a QUERIES edge to that
// table. SQL whose table cannot be statically parsed is honest-skipped.
func emitClickHouseTargets(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsClickHouse(src) {
		return
	}
	emitSQLDatastoreTargets(src, funcs, emit, clickhouseExecRe, "clickhouse")
}

// ---------------------------------------------------------------------------
// Snowflake (SQL warehouse — table attribution)
// ---------------------------------------------------------------------------

// snowflakeExecRe matches a Snowflake connector cursor/connection
// execute/query call: `cur.execute("SELECT ... FROM orders")`
// (snowflake-connector-python), `conn.cursor().execute(...)`, the Go
// `gosnowflake` `db.Query(...)`, the Spark/JDBC `statement.executeQuery(...)`.
// Receiver open; the import gate keeps it from over-firing.
var snowflakeExecRe = regexp.MustCompile(
	`\b(?:[A-Za-z_$][\w$]*)\.(?:execute|executeQuery|executeUpdate|[Qq]uery|exec)\s*\(`,
)

// mentionsSnowflake reports whether the file imports / references a Snowflake
// connector: `snowflake-connector` / `snowflake.connector` (python), the
// SQLAlchemy `snowflake://` dialect URL, the Go `gosnowflake` driver, the
// JDBC `jdbc:snowflake:` URL, or a `*.snowflakecomputing.com` account URL.
func mentionsSnowflake(src string) bool {
	s := strings.ToLower(src)
	return strings.Contains(s, "snowflake") ||
		strings.Contains(s, "snowflake://") ||
		strings.Contains(s, "snowflakecomputing.com")
}

// emitSnowflakeTargets finds every Snowflake cursor execute/query call, pulls
// the SQL string out of its first argument, parses the table out of it, and
// emits a QUERIES edge to that table. Unparseable SQL is honest-skipped.
func emitSnowflakeTargets(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsSnowflake(src) {
		return
	}
	emitSQLDatastoreTargets(src, funcs, emit, snowflakeExecRe, "snowflake")
}

// ---------------------------------------------------------------------------
// Shared SQL-datastore emitter
// ---------------------------------------------------------------------------

// emitSQLDatastoreTargets is the SQL analogue of emitCQLTargets: for every
// execute/query call matched by `callRe`, it pulls the SQL string literal out
// of the call's first argument, parses the FROM/INTO/UPDATE table via the
// shared extractSQLTable, and emits a QUERIES edge to `Class:<Table>` tagged
// with `orm=<orm>`. SQL whose table cannot be statically parsed is skipped —
// the same precision bar as the existing cassandra / dynamic-name skips.
func emitSQLDatastoreTargets(src string, funcs []funcSpan, emit emitORMQueryFn, callRe *regexp.Regexp, orm string) {
	for _, m := range callRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 2 {
			continue
		}
		argsBlob := matchCall(src, m[1]-1, 4096)
		sql := firstStringLiteral(argsBlob)
		if sql == "" {
			continue
		}
		table, verb, isJoin := extractSQLTable(sql)
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(table), sqlOp(verb), "", orm, isJoin)
	}
}
