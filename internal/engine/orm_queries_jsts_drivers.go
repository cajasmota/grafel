// JS/TS raw-DB-driver + query-builder query attribution (#2862).
//
// Where orm_queries_jsts.go attributes high-level ORM calls
// (`prisma.user.findUnique`, `User.findOne`, …) to a model class, this
// file attributes RAW database-driver call sites and ORM query-builder
// chains to the table / collection / resource they operate on, emitting
// the same QUERIES edge so the two attribution layers share one graph
// mechanism.
//
// Covered raw drivers (issue #2862 scope):
//   - postgres : pg / node-postgres / postgres.js — `pool.query('SELECT
//     ... FROM users')`. Table parsed from the SQL string.
//   - mysql    : mysql / mysql2 — `conn.query('... FROM users')` /
//     `conn.execute(...)`. Table parsed from the SQL string.
//   - sqlite   : sqlite3 / better-sqlite3 — `db.get('... FROM users')` /
//     `db.prepare('INSERT INTO users ...')`. Table from SQL.
//   - mssql    : `pool.request().query('... FROM users')`. Table from SQL.
//   - oracledb : `connection.execute('... FROM users')`. Table from SQL.
//   - cassandra: cassandra-driver — `client.execute('SELECT ... FROM
//     events')`. CQL — same FROM/INTO/UPDATE surface as SQL.
//   - mongodb  : `db.collection('users').find()` /
//     `collection.insertOne()`. Resource is the collection name.
//   - redis    : redis / ioredis — `redis.get('user:1')`,
//     `redis.hset('cart', …)`. Resource is the key (or its prefix).
//   - neo4j    : `session.run('MATCH (n:User) ...')`. Resource is the
//     node label parsed from the Cypher.
//   - dynamodb : AWS SDK DocumentClient —
//     `docClient.get({ TableName: 'Users' })`. Resource from `TableName`.
//   - elastic  : `@elastic/elasticsearch` — `client.search({ index:
//     'products' })`. Resource from `index`.
//
// Covered query builders / lower-level ORMs:
//   - knex     : `knex('users').select()`, `knex.select().from('users')`,
//     `knex.insert(row).into('users')`. Table from the builder.
//   - mikro-orm : `em.find(User, …)`, `em.persist(u)`,
//     `em.getRepository(User)`. Model from the entity-class argument.
//   - objection : `User.query()` / `Model.query().where(…)`. Model from
//     the receiver class.
package engine

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// SQL / CQL table extraction (shared by pg/mysql/sqlite/mssql/oracle/cassandra)
// ---------------------------------------------------------------------------

// sqlTableRe pulls the primary table out of a SQL/CQL statement. It
// recognises the four DML anchors that name a table:
//   - FROM <table>      (SELECT / DELETE)
//   - INTO <table>      (INSERT)
//   - UPDATE <table>    (UPDATE)
//   - JOIN <table>      (captured for is_join detection, not as primary)
//
// The table token may be schema-qualified (`public.users`) or quoted
// (`"users"`, “ `users` “); we capture the last dotted segment and strip
// quotes downstream. Case-insensitive on the keyword.
var sqlTableRe = regexp.MustCompile(
	`(?i)\b(?:from|into|update)\s+["` + "`" + `]?([a-zA-Z_][\w$]*(?:\.[a-zA-Z_][\w$]*)?)["` + "`" + `]?`,
)

var sqlJoinRe = regexp.MustCompile(`(?i)\bjoin\s+["` + "`" + `]?[a-zA-Z_]`)

// sqlVerbRe pulls the leading DML verb out of a SQL/CQL statement so the
// call site's operation can be canonicalised (find/create/update/delete).
var sqlVerbRe = regexp.MustCompile(`(?i)^\s*(?:\()?\s*(select|insert|update|delete|merge|upsert|with)\b`)

// extractSQLTable returns (table, verb, isJoin) for a SQL/CQL string. An
// empty table means no attributable table token was found (e.g. a `BEGIN`
// or a fully-parameterised statement) — the caller then skips emission.
func extractSQLTable(sql string) (table, verb string, isJoin bool) {
	m := sqlTableRe.FindStringSubmatch(sql)
	if m != nil {
		raw := m[1]
		// Keep only the table component of a schema.table reference.
		if dot := strings.LastIndexByte(raw, '.'); dot >= 0 {
			raw = raw[dot+1:]
		}
		table = raw
	}
	if v := sqlVerbRe.FindStringSubmatch(sql); v != nil {
		verb = strings.ToLower(v[1])
	}
	isJoin = sqlJoinRe.MatchString(sql)
	return table, verb, isJoin
}

// sqlOp maps a leading SQL verb to a canonical operation.
func sqlOp(verb string) string {
	switch verb {
	case "select", "with":
		return "find"
	case "insert", "upsert", "merge":
		return "create"
	case "update":
		return "update"
	case "delete":
		return "delete"
	default:
		return "find"
	}
}

// firstStringLiteral returns the first single/double/backtick-quoted string
// literal in `blob` (the call-args substring), or "" if none. Used to pull
// the SQL text out of `query('SELECT ...')` and the key out of
// `redis.get('user:1')`.
var firstStrLitRe = regexp.MustCompile("['\"`]((?:[^'\"`\\\\]|\\\\.)*)['\"`]")

func firstStringLiteral(blob string) string {
	m := firstStrLitRe.FindStringSubmatch(blob)
	if m == nil {
		return ""
	}
	return m[1]
}

// ---------------------------------------------------------------------------
// Raw-driver matchers
// ---------------------------------------------------------------------------

// jsSQLDriverRe matches a SQL-driver query call:
//
//	<recv>.query( / .execute( / .prepare( / .get( / .all( / .run(
//
// where the FIRST argument is a SQL string literal. The receiver is left
// open (pool/conn/db/client/connection/…) because the SQL string itself —
// not the receiver name — carries the table. We disambiguate the driver
// family with an import gate (mentionsSQLDriver) so the broad surface does
// not fire on unrelated `.run`/`.get` calls (e.g. test runners, lodash).
var jsSQLDriverRe = regexp.MustCompile(
	`(?:[A-Za-z_$][\w$]*|\))\.(query|execute|prepare|get|all|run|each|exec)\s*\(`,
)

// jsMongoCollectionRe matches `<recv>.collection('users')` and the chained
// op `<recv>.collection('users').<verb>(`. We capture the collection name
// and (optionally) the trailing verb.
var jsMongoCollectionRe = regexp.MustCompile(
	`\.collection\(\s*['"` + "`" + `]([a-zA-Z_][\w$.]*)['"` + "`" + `]\s*\)(?:\.(insertOne|insertMany|find|findOne|findOneAndUpdate|findOneAndDelete|updateOne|updateMany|deleteOne|deleteMany|aggregate|countDocuments|count|distinct|replaceOne|bulkWrite)\s*\()?`,
)

// jsRedisRe matches a redis/ioredis command with a key as first string
// literal: `<recv>.<cmd>('key', …)`. Verb list is the common command
// surface; the resource is the key (prefix before the first ':').
var jsRedisRe = regexp.MustCompile(
	`\b(?:redis|client|cache|ioredis|pub|sub|conn)\.(get|set|del|exists|expire|incr|decr|hget|hset|hdel|hgetall|lpush|rpush|lpop|rpop|sadd|srem|smembers|zadd|zrange|mget|mset|setex|getset|append)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`,
)

// jsNeo4jRe matches `<session>.run('MATCH (n:Label) ...')` /
// `tx.run('CREATE (n:Label) ...')`. We capture the Cypher string and pull
// the node label downstream.
var jsNeo4jRe = regexp.MustCompile(
	`\b(?:session|tx|txc|driver|neo4j)\.run\s*(\()`,
)

// cypherLabelRe pulls the first node label out of a Cypher pattern
// `(var:Label)` / `(:Label)`.
var cypherLabelRe = regexp.MustCompile(`\(\s*[A-Za-z_$][\w$]*?\s*:\s*([A-Za-z_$][\w$]*)`)

// cypherVerbRe pulls the leading Cypher clause for op canonicalisation.
var cypherVerbRe = regexp.MustCompile(`(?i)\b(match|create|merge|delete|set|remove)\b`)

// jsDynamoRe matches an AWS SDK DocumentClient / DynamoDBClient op whose
// args carry `TableName: '<table>'`. We capture the verb and the table.
var jsDynamoRe = regexp.MustCompile(
	`\.(get|put|update|delete|query|scan|batchGet|batchWrite|transactGet|transactWrite)\s*\(`,
)

var jsTableNameKeyRe = regexp.MustCompile(`TableName\s*:\s*['"` + "`" + `]([a-zA-Z_][\w$.-]*)['"` + "`" + `]`)

// jsElasticRe matches an Elasticsearch client op carrying `index: '<idx>'`.
var jsElasticRe = regexp.MustCompile(
	`\.(search|index|get|update|delete|bulk|count|msearch|deleteByQuery|updateByQuery)\s*\(`,
)

var jsIndexKeyRe = regexp.MustCompile(`\bindex\s*:\s*['"` + "`" + `]([a-zA-Z_][\w$.-]*)['"` + "`" + `]`)

// ---------------------------------------------------------------------------
// Query-builder / lower-level-ORM matchers
// ---------------------------------------------------------------------------

// jsKnexCallRe matches the `knex('users')` / `db('users')` constructor-call
// form that names the table directly. The receiver is gated to knex-ish
// identifiers to avoid matching arbitrary function calls.
var jsKnexCallRe = regexp.MustCompile(
	`\b(?:knex|db|trx|qb|builder)\(\s*['"` + "`" + `]([a-zA-Z_][\w$.]*)['"` + "`" + `]\s*\)`,
)

// jsKnexFromIntoRe matches the `.from('users')` / `.into('users')` /
// `.table('users')` builder methods that name the table on a chain.
var jsKnexFromIntoRe = regexp.MustCompile(
	`\.(from|into|table|withSchema)\(\s*['"` + "`" + `]([a-zA-Z_][\w$.]*)['"` + "`" + `]\s*\)`,
)

// jsKnexVerbRe finds a builder verb on the same chain so we can record an
// operation. We only consult it to canonicalise the op; the table comes
// from jsKnexCallRe / jsKnexFromIntoRe.
var jsKnexVerbRe = regexp.MustCompile(`\.(select|insert|update|delete|del|count|sum|avg|min|max|first|pluck)\s*\(`)

// jsMikroEMRe matches MikroORM EntityManager calls that take an entity
// class as the first argument: `em.find(User, …)`, `em.findOne(Book, …)`,
// `em.getRepository(User)`, `em.persist(u)` (instance — skipped),
// `em.nativeDelete(User, …)`. The receiver is gated to em-ish identifiers.
var jsMikroEMRe = regexp.MustCompile(
	`\b(?:em|orm\.em|this\.em|manager|entityManager)\.(find|findOne|findOneOrFail|findAndCount|getRepository|nativeInsert|nativeUpdate|nativeDelete|count|findAll|create|insert|upsert)\s*\(\s*([A-Z][A-Za-z0-9_$]*)\b`,
)

// jsObjectionRe matches `Model.query()` / `User.query().where(…)` — the
// Objection.js entry point. The receiver is a capitalised model class; we
// gate on import-presence to avoid matching arbitrary `.query()` chains.
var jsObjectionRe = regexp.MustCompile(
	`\b([A-Z][A-Za-z0-9_$]*)\.query\s*\(\s*\)\s*(?:\.(insert|insertGraph|patch|patchAndFetchById|update|updateAndFetchById|delete|deleteById|findById|findOne|where|select|first|count|relatedQuery)\s*\()?`,
)

// objectionVerbToOp maps the Objection chain verb to a canonical op.
func objectionVerbToOp(verb string) string {
	switch verb {
	case "insert", "insertGraph":
		return "create"
	case "patch", "patchAndFetchById", "update", "updateAndFetchById":
		return "update"
	case "delete", "deleteById":
		return "delete"
	case "count":
		return "aggregate"
	default:
		// findById/findOne/where/select/first/relatedQuery/"" → read.
		return "find"
	}
}

// ---------------------------------------------------------------------------
// Import gates
// ---------------------------------------------------------------------------

func mentionsSQLDriver(src string) bool {
	for _, s := range []string{
		"'pg'", "\"pg\"", "node-postgres", "'postgres'", "\"postgres\"",
		"@vercel/postgres", "slonik",
		"'mysql'", "\"mysql\"", "'mysql2'", "\"mysql2\"", "mysql2/promise",
		"'sqlite3'", "\"sqlite3\"", "better-sqlite3", "'sqlite'", "\"sqlite\"",
		"'mssql'", "\"mssql\"", "tedious",
		"'oracledb'", "\"oracledb\"",
		"cassandra-driver",
	} {
		if strings.Contains(src, s) {
			return true
		}
	}
	return false
}

func mentionsMongoDriver(src string) bool {
	// Native driver shape: a `db.collection('name')` access. Gate on the
	// MongoClient/mongodb import OR the collection() accessor itself, but
	// skip files that only use Mongoose (handled by scanJSORM).
	if strings.Contains(src, "mongoose") && !strings.Contains(src, "MongoClient") {
		return false
	}
	return strings.Contains(src, "mongodb") ||
		strings.Contains(src, "MongoClient") ||
		strings.Contains(src, ".collection(")
}

func mentionsRedis(src string) bool {
	return strings.Contains(src, "'redis'") || strings.Contains(src, "\"redis\"") ||
		strings.Contains(src, "ioredis") || strings.Contains(src, "createClient")
}

func mentionsNeo4j(src string) bool {
	return strings.Contains(src, "neo4j")
}

func mentionsDynamo(src string) bool {
	return strings.Contains(src, "DynamoDB") || strings.Contains(src, "dynamodb") ||
		strings.Contains(src, "@aws-sdk/lib-dynamodb")
}

func mentionsElastic(src string) bool {
	return strings.Contains(src, "@elastic/elasticsearch") ||
		strings.Contains(src, "@opensearch-project") ||
		strings.Contains(src, "elasticsearch")
}

func mentionsKnex(src string) bool {
	return strings.Contains(src, "knex") || strings.Contains(src, "Knex")
}

func mentionsMikro(src string) bool {
	return strings.Contains(src, "@mikro-orm") || strings.Contains(src, "MikroORM") ||
		strings.Contains(src, "EntityManager")
}

func mentionsObjection(src string) bool {
	return strings.Contains(src, "objection")
}

// ---------------------------------------------------------------------------
// Driver scanner
// ---------------------------------------------------------------------------

// scanJSDrivers walks `src` and emits QUERIES edges for raw-DB-driver and
// query-builder call sites. Each family is gated on an import sniff so a
// broad call surface (`.run(`, `.get(`, `.query(`) only fires in files
// that actually import the driver.
func scanJSDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	scanJSSQLDrivers(src, funcs, emit)
	scanJSMongoDriver(src, funcs, emit)
	scanJSRedis(src, funcs, emit)
	scanJSNeo4j(src, funcs, emit)
	scanJSDynamo(src, funcs, emit)
	scanJSElastic(src, funcs, emit)
	scanJSKnex(src, funcs, emit)
	scanJSMikro(src, funcs, emit)
	scanJSObjection(src, funcs, emit)
}

// scanJSSQLDrivers handles pg / mysql(2) / sqlite(3) / better-sqlite3 /
// mssql / oracledb / cassandra — all of which carry the table in a SQL/CQL
// string literal passed to a query/execute/prepare-style call.
func scanJSSQLDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsSQLDriver(src) {
		return
	}
	orm := sqlDriverFamily(src)
	for _, m := range jsSQLDriverRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		argsBlob := extractCallArgs(src, m[3])
		sql := firstStringLiteral(argsBlob)
		if sql == "" {
			continue
		}
		table, verb, isJoin := extractSQLTable(sql)
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		model := capitalisedSingular(table)
		emit(caller, model, sqlOp(verb), "", orm, isJoin)
	}
}

// sqlDriverFamily picks the most specific SQL driver name visible in the
// file so the emitted edge's `orm` property names the real driver.
func sqlDriverFamily(src string) string {
	switch {
	case strings.Contains(src, "cassandra-driver"):
		return "cassandra"
	case strings.Contains(src, "oracledb"):
		return "oracledb"
	case strings.Contains(src, "mssql") || strings.Contains(src, "tedious"):
		return "mssql"
	case strings.Contains(src, "better-sqlite3"):
		return "better-sqlite3"
	case strings.Contains(src, "sqlite"):
		return "sqlite"
	case strings.Contains(src, "mysql2"):
		return "mysql2"
	case strings.Contains(src, "mysql"):
		return "mysql"
	default:
		return "postgres"
	}
}

// scanJSMongoDriver handles the native mongodb driver:
// `db.collection('users').find(...)`.
func scanJSMongoDriver(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsMongoDriver(src) {
		return
	}
	for _, m := range jsMongoCollectionRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		collection := src[m[2]:m[3]]
		if collection == "" {
			continue
		}
		verb := ""
		if len(m) >= 6 && m[4] >= 0 {
			verb = src[m[4]:m[5]]
		}
		caller := enclosingFuncAt(funcs, m[0])
		op := "find"
		if verb != "" {
			op = mongoOp(verb)
		}
		model := capitalisedSingular(collection)
		emit(caller, model, op, "", "mongodb", false)
	}
}

// mongoOp maps a native-driver collection method to a canonical op.
func mongoOp(verb string) string {
	switch verb {
	case "insertOne", "insertMany", "bulkWrite":
		return "create"
	case "updateOne", "updateMany", "replaceOne", "findOneAndUpdate":
		return "update"
	case "deleteOne", "deleteMany", "findOneAndDelete":
		return "delete"
	case "aggregate", "countDocuments", "count", "distinct":
		return "aggregate"
	default:
		// find / findOne / "" → read.
		return "find"
	}
}

// scanJSRedis handles redis / ioredis key commands. The resource is the
// key prefix (the segment before the first ':' colon, e.g. `user` from
// `user:42`), which is the de-facto namespace for a Redis key family.
func scanJSRedis(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsRedis(src) {
		return
	}
	for _, m := range jsRedisRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		cmd := src[m[2]:m[3]]
		key := src[m[4]:m[5]]
		prefix := key
		if c := strings.IndexByte(key, ':'); c > 0 {
			prefix = key[:c]
		}
		if prefix == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		model := capitalisedSingular(prefix)
		emit(caller, model, redisOp(cmd), "", "redis", false)
	}
}

func redisOp(cmd string) string {
	switch cmd {
	case "get", "exists", "hget", "hgetall", "lpop", "rpop", "smembers",
		"zrange", "mget", "getset":
		return "find"
	case "set", "hset", "lpush", "rpush", "sadd", "zadd", "mset",
		"setex", "incr", "decr", "append", "expire":
		return "create"
	case "del", "hdel", "srem":
		return "delete"
	default:
		return "find"
	}
}

// scanJSNeo4j handles `session.run('<cypher>')`. The resource is the first
// node label in the Cypher pattern.
func scanJSNeo4j(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsNeo4j(src) {
		return
	}
	for _, m := range jsNeo4jRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		argsBlob := matchCall(src, m[2], 4096)
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

// scanJSDynamo handles AWS SDK DocumentClient ops with a TableName key.
func scanJSDynamo(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsDynamo(src) {
		return
	}
	for _, m := range jsDynamoRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		verb := src[m[2]:m[3]]
		argsBlob := extractCallArgs(src, m[3])
		tn := jsTableNameKeyRe.FindStringSubmatch(argsBlob)
		if tn == nil {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		model := capitalisedSingular(tn[1])
		emit(caller, model, dynamoOp(verb), "", "dynamodb", false)
	}
}

func dynamoOp(verb string) string {
	switch verb {
	case "get", "query", "scan", "batchGet", "transactGet":
		return "find"
	case "put", "batchWrite", "transactWrite":
		return "create"
	case "update":
		return "update"
	case "delete":
		return "delete"
	default:
		return "find"
	}
}

// scanJSElastic handles Elasticsearch/OpenSearch ops with an index key.
func scanJSElastic(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsElastic(src) {
		return
	}
	for _, m := range jsElasticRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		verb := src[m[2]:m[3]]
		argsBlob := extractCallArgs(src, m[3])
		ix := jsIndexKeyRe.FindStringSubmatch(argsBlob)
		if ix == nil {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		model := capitalisedSingular(ix[1])
		emit(caller, model, elasticOp(verb), "", "elastic", false)
	}
}

func elasticOp(verb string) string {
	switch verb {
	case "search", "get", "count", "msearch":
		return "find"
	case "index", "bulk":
		return "create"
	case "update", "updateByQuery":
		return "update"
	case "delete", "deleteByQuery":
		return "delete"
	default:
		return "find"
	}
}

// scanJSKnex handles the Knex query builder. The table is named either by
// the constructor call `knex('users')` or by a `.from/.into/.table` method
// on the chain. We pick the nearest builder verb for the operation.
func scanJSKnex(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsKnex(src) {
		return
	}
	seen := map[string]bool{}
	emitKnex := func(pos int, table string) {
		if table == "" {
			return
		}
		if dot := strings.LastIndexByte(table, '.'); dot >= 0 {
			table = table[dot+1:]
		}
		key := table + "@" + itoa(pos)
		if seen[key] {
			return
		}
		seen[key] = true
		caller := enclosingFuncAt(funcs, pos)
		// Look at the surrounding statement (up to the next ';' or newline)
		// for a builder verb so the op reflects select/insert/update/delete.
		op := knexOpAround(src, pos)
		model := capitalisedSingular(table)
		emit(caller, model, op, "", "knex", false)
	}
	for _, m := range jsKnexCallRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		emitKnex(m[0], src[m[2]:m[3]])
	}
	for _, m := range jsKnexFromIntoRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		emitKnex(m[0], src[m[4]:m[5]])
	}
}

// knexOpAround scans the statement containing `pos` for a builder verb and
// canonicalises it. Defaults to "find" (the dominant select case).
func knexOpAround(src string, pos int) string {
	start := pos
	for start > 0 && src[start-1] != '\n' && src[start-1] != ';' {
		start--
	}
	end := pos
	for end < len(src) && src[end] != '\n' && src[end] != ';' {
		end++
	}
	stmt := src[start:end]
	vm := jsKnexVerbRe.FindStringSubmatch(stmt)
	if vm == nil {
		return "find"
	}
	switch vm[1] {
	case "insert":
		return "create"
	case "update":
		return "update"
	case "delete", "del":
		return "delete"
	case "count", "sum", "avg", "min", "max":
		return "aggregate"
	default:
		return "find"
	}
}

// scanJSMikro handles MikroORM EntityManager calls that name an entity
// class as the first positional argument.
func scanJSMikro(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsMikro(src) {
		return
	}
	for _, m := range jsMikroEMRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		verb := src[m[2]:m[3]]
		model := src[m[4]:m[5]]
		if model == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, model, mikroOp(verb), "", "mikro-orm", false)
	}
}

func mikroOp(verb string) string {
	switch verb {
	case "find", "findOne", "findOneOrFail", "findAndCount", "findAll", "getRepository":
		return "find"
	case "create", "nativeInsert", "insert", "upsert":
		return "create"
	case "nativeUpdate":
		return "update"
	case "nativeDelete":
		return "delete"
	case "count":
		return "aggregate"
	default:
		return "find"
	}
}

// scanJSObjection handles Objection.js `Model.query()` entry points and
// the chained verb (insert/patch/delete/find/…).
func scanJSObjection(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if !mentionsObjection(src) {
		return
	}
	for _, m := range jsObjectionRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		model := src[m[2]:m[3]]
		if model == "" {
			continue
		}
		verb := ""
		if len(m) >= 6 && m[4] >= 0 {
			verb = src[m[4]:m[5]]
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, model, objectionVerbToOp(verb), "", "objection", false)
	}
}
