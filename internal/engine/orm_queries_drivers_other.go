// Cross-language raw-DB-driver TOPOLOGY attribution for C#, PHP, Rust,
// Python, Java and Ruby (#3645, epic #3625).
//
// Where orm_queries_jsts_drivers.go attributes JS/TS raw-driver call sites
// to the collection / table / index they touch, this file does the same for
// the datastore drivers the #3625 audit flagged as pure-loss (C#, PHP, Rust)
// or shallow (Python, Java, Ruby). It emits the SAME QUERIES edge shape
// (`caller → Class:<resource>`, operation, orm) so the topology surface is
// cross-language consistent.
//
// Covered idioms (the dominant, statically-resolvable forms — dynamic names
// are honest-skipped):
//
//   - Mongo
//     C#     : db.GetCollection<T>("users")
//     PHP    : $mongo->selectCollection("db", "users") / ->users
//     Ruby   : client[:users]
//     Rust   : db.collection::<T>("users") / db.collection("users")
//     Python : db.users / db["users"] / get_collection("users")
//     Java   : db.getCollection("users")
//   - DynamoDB (TableName / table_name literal)
//     C#     : new GetItemRequest { TableName = "Products" }
//     PHP    : $dynamodb->getItem(['TableName' => 'Products'])
//     Ruby   : dynamodb.get_item(table_name: 'Products')
//     Python : table = dynamodb.Table('Products') / TableName='Products'
//     Java   : GetItemRequest.builder().tableName("Products")
//   - Elasticsearch (index literal)
//     C#     : .Index("products")
//     PHP    : ['index' => 'products']
//     Python : es.search(index='products')
//     Ruby   : client.search(index: 'products')
//   - Cassandra (CQL FROM/INTO/UPDATE table — reuses extractSQLTable)
//     any lang: session.execute("SELECT ... FROM events")
//
// Resource names are singularised + capitalised via capitalisedSingular so
// the edge target matches the same Class:<Model> shape the resolver keys on.
package engine

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared datastore-target regexes (literal collection/table/index keys)
// ---------------------------------------------------------------------------

// dynamoTableNameKeyRe matches a `TableName = "X"` (C#) or `TableName => 'X'`
// / `'TableName' => 'X'` (PHP) or `table_name: 'X'` (Ruby) or
// `TableName='X'` / `table_name='X'` (Python) literal. The key spelling is
// normalised case-insensitively; only quoted string values are captured
// (dynamic table names — a variable — are skipped).
var dynamoTableNameKeyRe = regexp.MustCompile(
	`(?i)\b(?:TableName|table_name)\s*['"` + "`" + `]?\s*(?::|=>|=)\s*['"` + "`" + `]([A-Za-z_][\w$.-]*)['"` + "`" + `]`,
)

// esIndexKeyRe matches an Elasticsearch index literal: `index: 'x'` /
// `'index' => 'x'` / `index='x'` / `.Index("x")`. Case-insensitive key.
var esIndexKeyRe = regexp.MustCompile(
	`(?i)(?:\bindex\s*['"` + "`" + `]?\s*(?::|=>|=)\s*|\.Index\s*\(\s*)['"` + "`" + `]([A-Za-z_][\w$.-]*)['"` + "`" + `]`,
)

// cqlStringRe pulls the first double/single-quoted string out of a blob so a
// Cassandra session.execute("SELECT ... FROM t") call can be table-parsed.
// Reuses the shared firstStringLiteral() from orm_queries_jsts_drivers.go.

// ---------------------------------------------------------------------------
// C#
// ---------------------------------------------------------------------------

// csGetCollectionRe matches the MongoDB.Driver idiom
// `db.GetCollection<User>("users")` — the generic type arg is optional. The
// collection name is the quoted literal.
var csGetCollectionRe = regexp.MustCompile(
	`\.GetCollection\s*(?:<[^>]+>)?\s*\(\s*['"]([A-Za-z_][\w$.-]*)['"]`,
)

// csCqlExecuteRe matches a Cassandra `session.Execute("CQL")` /
// `await session.ExecuteAsync(new SimpleStatement("CQL"))`. We capture the
// open-paren position and pull the first string literal as CQL downstream.
var csCqlExecuteRe = regexp.MustCompile(
	`\b(?:session|_session|cluster|cql)\.(?:Execute|ExecuteAsync|Prepare)\s*\(`,
)

func mentionsCSharpMongo(src string) bool {
	return strings.Contains(src, "MongoDB.Driver") ||
		strings.Contains(src, "IMongoDatabase") ||
		strings.Contains(src, "GetCollection")
}

func mentionsCSharpDynamo(src string) bool {
	return strings.Contains(src, "Amazon.DynamoDBv2") ||
		strings.Contains(src, "AmazonDynamoDB") ||
		strings.Contains(src, "DynamoDBContext")
}

func mentionsCSharpElastic(src string) bool {
	return strings.Contains(src, "Nest") || strings.Contains(src, "Elastic.Clients") ||
		strings.Contains(src, "ElasticClient") || strings.Contains(src, "Elasticsearch")
}

func mentionsCSharpCassandra(src string) bool {
	return strings.Contains(src, "Cassandra") || strings.Contains(src, "ISession")
}

func scanCSharpDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	// Mongo collection target.
	if mentionsCSharpMongo(src) {
		for _, m := range csGetCollectionRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			coll := src[m[2]:m[3]]
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(coll), "find", "", "mongodb", false)
		}
	}
	// DynamoDB TableName.
	if mentionsCSharpDynamo(src) {
		emitDynamoTargets(src, funcs, emit)
	}
	// Elasticsearch index.
	if mentionsCSharpElastic(src) {
		emitElasticTargets(src, funcs, emit)
	}
	// Cassandra CQL.
	if mentionsCSharpCassandra(src) {
		emitCQLTargets(src, funcs, emit, csCqlExecuteRe)
	}
}

// ---------------------------------------------------------------------------
// PHP
// ---------------------------------------------------------------------------

// phpSelectCollectionRe matches the MongoDB PHP driver idiom
// `$mongo->selectCollection('db', 'users')` (collection is the LAST quoted
// arg) and `$db->users` property access is handled separately. We also
// accept `->collection('users')` (Laravel mongodb / jenssegers) where the
// single quoted arg is the collection.
var phpSelectCollectionRe = regexp.MustCompile(
	`->selectCollection\s*\(([^)]*)\)`,
)
var phpCollectionMethodRe = regexp.MustCompile(
	`->collection\s*\(\s*['"]([A-Za-z_][\w$.-]*)['"]\s*\)`,
)

// phpCqlRe matches a Cassandra PHP driver `$session->execute(new ... ("CQL"))`
// or `$session->execute("CQL")`.
var phpCqlRe = regexp.MustCompile(
	`\$\w+->execute\s*\(`,
)

func mentionsPHPMongo(src string) bool {
	return strings.Contains(src, "MongoDB\\") || strings.Contains(src, "MongoDB\\Client") ||
		strings.Contains(src, "selectCollection") || strings.Contains(src, "mongodb")
}

func mentionsPHPDynamo(src string) bool {
	return strings.Contains(src, "Aws\\DynamoDb") || strings.Contains(src, "DynamoDbClient") ||
		strings.Contains(src, "dynamodb")
}

func mentionsPHPElastic(src string) bool {
	return strings.Contains(src, "Elasticsearch\\") || strings.Contains(src, "ClientBuilder") ||
		strings.Contains(src, "elasticsearch")
}

func mentionsPHPCassandra(src string) bool {
	return strings.Contains(src, "Cassandra\\") || strings.Contains(src, "Cassandra::cluster")
}

func scanPHPDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if mentionsPHPMongo(src) {
		// selectCollection('db', 'users') → last quoted literal is the coll.
		for _, m := range phpSelectCollectionRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			argsBlob := src[m[2]:m[3]]
			coll := lastStringLiteral(argsBlob)
			if coll == "" {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(coll), "find", "", "mongodb", false)
		}
		for _, m := range phpCollectionMethodRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			coll := src[m[2]:m[3]]
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(coll), "find", "", "mongodb", false)
		}
	}
	if mentionsPHPDynamo(src) {
		emitDynamoTargets(src, funcs, emit)
	}
	if mentionsPHPElastic(src) {
		emitElasticTargets(src, funcs, emit)
	}
	if mentionsPHPCassandra(src) {
		emitCQLTargets(src, funcs, emit, phpCqlRe)
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

// rustCollectionRe matches the mongodb crate idiom
// `db.collection::<User>("users")` / `db.collection("users")`. The turbofish
// type arg is optional; the collection name is the quoted literal.
var rustCollectionRe = regexp.MustCompile(
	`\.collection\s*(?:::\s*<[^>]+>)?\s*\(\s*"([A-Za-z_][\w$.-]*)"`,
)

// rustCqlRe matches a scylla/cassandra-rs `session.query("CQL", ...)` /
// `session.execute(...)`. For scylla the CQL text is the first arg.
var rustCqlRe = regexp.MustCompile(
	`\b(?:session|_session|sess)\.(?:query|execute|query_unpaged|prepare)\s*\(`,
)

// rustSqlRe matches a raw-SQL driver query call whose FIRST argument is a SQL
// string literal, covering the dominant sqlx / tokio-postgres / rusqlite /
// mysql_async surfaces:
//
//	sqlx::query("SELECT ... FROM t")
//	sqlx::query_as::<_, User>("SELECT ... FROM users")
//	query("...")              // `use sqlx::query;`
//	query_as::<_, T>("...")   // `use sqlx::query_as;`
//	client.query("...")       // tokio-postgres / postgres
//	conn.execute("...")       // postgres / rusqlite / mysql
//	conn.query_one("...")     // tokio-postgres
//
// The receiver / path is left open because the SQL string — not the receiver —
// carries the table. The crate import gate (mentionsRust* below) keeps the
// broad surface from firing on unrelated `.query(`/`.execute(` calls, and
// emitSQLDatastoreTargets only emits when extractSQLTable resolves a literal
// table (interpolated `format!` SQL yields no literal → honest-skipped).
var rustSqlRe = regexp.MustCompile(
	`(?:[A-Za-z_$][\w$]*(?:::)?)?\b(?:query|query_as|query_scalar|query_one|query_opt|execute|fetch_all|fetch_one|fetch_optional|prepare)(?:::\s*<[^>]*>)?\s*\(`,
)

func mentionsRustMongo(src string) bool {
	return strings.Contains(src, "mongodb") || strings.Contains(src, "Collection<")
}

func mentionsRustScylla(src string) bool {
	return strings.Contains(src, "scylla") || strings.Contains(src, "cassandra") ||
		strings.Contains(src, "cdrs")
}

// mentionsRustPostgres reports whether the file uses a Postgres Rust driver:
// the sqlx Postgres backend (`sqlx::Postgres` / `PgPool` / `postgres://`) or
// the standalone `tokio-postgres` / `postgres` crate.
func mentionsRustPostgres(src string) bool {
	return strings.Contains(src, "tokio_postgres") || strings.Contains(src, "tokio-postgres") ||
		strings.Contains(src, "PgPool") || strings.Contains(src, "sqlx::Postgres") ||
		strings.Contains(src, "postgres::") || strings.Contains(src, "postgres://")
}

// mentionsRustMySQL reports whether the file uses a MySQL Rust driver: the
// sqlx MySQL backend (`sqlx::MySql` / `MySqlPool` / `mysql://`) or the
// standalone `mysql` / `mysql_async` crate.
func mentionsRustMySQL(src string) bool {
	return strings.Contains(src, "mysql_async") || strings.Contains(src, "MySqlPool") ||
		strings.Contains(src, "sqlx::MySql") || strings.Contains(src, "mysql::") ||
		strings.Contains(src, "mysql://")
}

// mentionsRustSQLite reports whether the file uses a SQLite Rust driver: the
// sqlx SQLite backend (`sqlx::Sqlite` / `SqlitePool` / `sqlite:`) or the
// standalone `rusqlite` crate.
func mentionsRustSQLite(src string) bool {
	return strings.Contains(src, "rusqlite") || strings.Contains(src, "SqlitePool") ||
		strings.Contains(src, "sqlx::Sqlite") || strings.Contains(src, "sqlite:")
}

// mentionsRustElastic reports whether the file imports / references the
// elasticsearch-rs client (`elasticsearch::` crate, `Elasticsearch::` client,
// or the `SearchParts` request-path enum). The gate keeps the broad
// `.index("x")` literal surface (esIndexKeyRe) from firing on unrelated Rust
// builder calls.
func mentionsRustElastic(src string) bool {
	return strings.Contains(src, "elasticsearch") || strings.Contains(src, "Elasticsearch") ||
		strings.Contains(src, "SearchParts") || strings.Contains(src, "IndexParts")
}

// rustEsIndexRe matches the elasticsearch-rs index selectors, capturing the
// first quoted literal index name in either form:
//
//	SearchParts::Index(&["products"])   // request-path enum (one or more)
//	IndexParts::Index("products")
//	.index("products")                   // lowercase fluent builder
//
// The shared esIndexKeyRe handles the `index: 'x'` / `'index' => 'x'` /
// `.Index("x")` (capital-I) forms used by other languages; Rust's lowercase
// `.index(` and the `*Parts::Index(` enum are Rust-specific and matched here.
var rustEsIndexRe = regexp.MustCompile(
	`(?:\b(?:Search|Index|Get|Update|Delete|Count|Bulk)Parts::Index\s*\(\s*&?\s*\[?\s*|\.index\s*\(\s*)"([A-Za-z_][\w$.-]*)"`,
)

func scanRustDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if mentionsRustMongo(src) {
		for _, m := range rustCollectionRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			coll := src[m[2]:m[3]]
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(coll), "find", "", "mongodb", false)
		}
	}
	if mentionsRustScylla(src) {
		emitCQLTargets(src, funcs, emit, rustCqlRe)
	}
	// Raw SQL (sqlx / tokio-postgres / mysql_async / rusqlite). The SQL string
	// literal carries the table; the backend crate import selects the orm tag
	// (postgres / mysql / sqlite) so the per-driver query_attribution cell is
	// attributed accurately. Files that mention several backends emit under
	// each matched backend — rare, and the edge target (the table) is identical.
	switch {
	case mentionsRustPostgres(src):
		emitSQLDatastoreTargets(src, funcs, emit, rustSqlRe, "postgres")
	case mentionsRustMySQL(src):
		emitSQLDatastoreTargets(src, funcs, emit, rustSqlRe, "mysql")
	case mentionsRustSQLite(src):
		emitSQLDatastoreTargets(src, funcs, emit, rustSqlRe, "sqlite")
	}
	// DynamoDB: the aws-sdk-rust fluent builder carries the table as a
	// `.table_name("X")` METHOD call (not a `table_name = "X"` assignment), so
	// the shared dynamoTableNameKeyRe (key/value form) does not match it. We
	// capture the builder-method form with rustDynamoTableNameRe and also run
	// emitDynamoTargets for any `table_name = "X"` literal. Dynamic table names
	// (a variable) are honest-skipped because both only capture quoted literals.
	if mentionsRustDynamo(src) {
		for _, m := range rustDynamoTableNameRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "dynamodb", false)
		}
		emitDynamoTargets(src, funcs, emit)
	}
	// Elasticsearch (elasticsearch-rs): the `.index("x")` builder literal is
	// captured by the shared esIndexKeyRe / emitElasticTargets; the
	// `SearchParts::Index(&["x"])` request-path form is captured here. Both are
	// gated on mentionsRustElastic so the broad index-literal surface does not
	// fire on unrelated Rust code. Dynamic index names are honest-skipped.
	if mentionsRustElastic(src) {
		// Shared key/value + capital-`.Index(` forms (cross-language).
		emitElasticTargets(src, funcs, emit)
		// Rust-specific lowercase `.index("x")` builder + `*Parts::Index(...)`.
		for _, m := range rustEsIndexRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "elastic", false)
		}
	}
}

// mentionsRustDynamo reports whether the file uses the aws-sdk-rust DynamoDB
// client (`aws_sdk_dynamodb` crate / `aws-sdk-dynamodb` dependency).
func mentionsRustDynamo(src string) bool {
	return strings.Contains(src, "aws_sdk_dynamodb") || strings.Contains(src, "aws-sdk-dynamodb") ||
		strings.Contains(src, "DynamoDb") || strings.Contains(src, "dynamodb")
}

// rustDynamoTableNameRe matches the aws-sdk-rust fluent-builder table selector
// `.table_name("Products")`, where the table is the first quoted literal. The
// key/value `table_name = "X"` form is handled separately by emitDynamoTargets.
var rustDynamoTableNameRe = regexp.MustCompile(
	`\.table_name\s*\(\s*"([A-Za-z_][\w$.-]*)"\s*\)`,
)

// ---------------------------------------------------------------------------
// Python (pymongo collection / boto3 dynamo / elasticsearch / cassandra)
// ---------------------------------------------------------------------------

// pyDynamoTableRe matches `dynamodb.Table('Products')` (boto3 resource).
var pyDynamoTableRe = regexp.MustCompile(
	`\.Table\s*\(\s*['"]([A-Za-z_][\w$.-]*)['"]\s*\)`,
)

// pyMongoCollGetRe matches `db.get_collection('users')` and the subscript
// form `db['users']` where the db handle is a pymongo Database.
var pyMongoCollGetRe = regexp.MustCompile(
	`\.get_collection\s*\(\s*['"]([A-Za-z_][\w$.-]*)['"]\s*\)`,
)
var pyMongoSubscriptRe = regexp.MustCompile(
	`\b(?:db|database|mongo|client)\s*\[\s*['"]([A-Za-z_][\w$.-]*)['"]\s*\]`,
)

// pyCqlRe matches a cassandra-driver `session.execute("CQL")`.
var pyCqlRe = regexp.MustCompile(
	`\b(?:session|_session|sess)\.(?:execute|execute_async|prepare)\s*\(`,
)

func mentionsPyMongo(src string) bool {
	return strings.Contains(src, "pymongo") || strings.Contains(src, "motor") ||
		strings.Contains(src, "MongoClient") || strings.Contains(src, "get_collection")
}
func mentionsPyDynamo(src string) bool {
	return strings.Contains(src, "boto3") || strings.Contains(src, "dynamodb") ||
		strings.Contains(src, "DynamoDB")
}
func mentionsPyElastic(src string) bool {
	return strings.Contains(src, "elasticsearch") || strings.Contains(src, "Elasticsearch") ||
		strings.Contains(src, "opensearchpy")
}
func mentionsPyCassandra(src string) bool {
	return strings.Contains(src, "cassandra")
}

func scanPythonDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if mentionsPyMongo(src) {
		for _, m := range pyMongoCollGetRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "pymongo", false)
		}
		for _, m := range pyMongoSubscriptRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "pymongo", false)
		}
	}
	if mentionsPyDynamo(src) {
		for _, m := range pyDynamoTableRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "dynamodb", false)
		}
		// Also catch `TableName='X'` keyword in low-level client calls.
		emitDynamoTargets(src, funcs, emit)
	}
	if mentionsPyElastic(src) {
		emitElasticTargets(src, funcs, emit)
	}
	if mentionsPyCassandra(src) {
		emitCQLTargets(src, funcs, emit, pyCqlRe)
	}
}

// ---------------------------------------------------------------------------
// Java (native Mongo driver / DynamoDB SDK v2 / Cassandra CQL)
// ---------------------------------------------------------------------------

// javaGetCollectionRe matches `database.getCollection("users")`.
var javaGetCollectionRe = regexp.MustCompile(
	`\.getCollection\s*\(\s*"([A-Za-z_][\w$.-]*)"`,
)

// javaTableNameRe matches AWS SDK v2 builder `.tableName("Products")`.
var javaTableNameRe = regexp.MustCompile(
	`\.tableName\s*\(\s*"([A-Za-z_][\w$.-]*)"\s*\)`,
)

// javaCqlRe matches a DataStax `session.execute("CQL")`.
var javaCqlRe = regexp.MustCompile(
	`\b(?:session|cqlSession|_session)\.(?:execute|executeAsync|prepare)\s*\(`,
)

func mentionsJavaMongo(src string) bool {
	return strings.Contains(src, "com.mongodb") || strings.Contains(src, "MongoCollection") ||
		strings.Contains(src, "MongoDatabase")
}
func mentionsJavaDynamo(src string) bool {
	return strings.Contains(src, "software.amazon.awssdk.services.dynamodb") ||
		strings.Contains(src, "com.amazonaws.services.dynamodbv2") ||
		strings.Contains(src, "DynamoDb")
}
func mentionsJavaCassandra(src string) bool {
	return strings.Contains(src, "com.datastax") || strings.Contains(src, "CqlSession")
}

func scanJavaDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if mentionsJavaMongo(src) {
		for _, m := range javaGetCollectionRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "mongodb", false)
		}
	}
	if mentionsJavaDynamo(src) {
		for _, m := range javaTableNameRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "dynamodb", false)
		}
		// AWS SDK v1 GetItemRequest carries `TableName` differently; the
		// shared key matcher covers `withTableName("X")`-style assignments
		// only when quoted, which we treat via emitDynamoTargets below.
		emitDynamoTargets(src, funcs, emit)
	}
	if mentionsJavaCassandra(src) {
		emitCQLTargets(src, funcs, emit, javaCqlRe)
	}
}

// ---------------------------------------------------------------------------
// Ruby (mongo ruby driver / aws-sdk-dynamodb / cassandra)
// ---------------------------------------------------------------------------

// rubyMongoCollRe matches `client[:users]` / `db[:users]` (Mongo Ruby
// driver collection accessor with a symbol key) and `client[:users]` with a
// string key.
var rubyMongoCollRe = regexp.MustCompile(
	`\b(?:client|db|database|mongo)\s*\[\s*:([A-Za-z_]\w*)\s*\]`,
)
var rubyMongoCollStrRe = regexp.MustCompile(
	`\b(?:client|db|database|mongo)\s*\[\s*['"]([A-Za-z_][\w$.-]*)['"]\s*\]`,
)

// rubyCqlRe matches a cassandra-driver `session.execute("CQL")`.
var rubyCqlRe = regexp.MustCompile(
	`\b(?:session|_session|sess)\.execute\s*\(`,
)

func mentionsRubyMongo(src string) bool {
	return strings.Contains(src, "Mongo::Client") || strings.Contains(src, "mongo") ||
		strings.Contains(src, "Mongoid")
}
func mentionsRubyDynamo(src string) bool {
	return strings.Contains(src, "aws-sdk-dynamodb") || strings.Contains(src, "Aws::DynamoDB") ||
		strings.Contains(src, "dynamodb")
}
func mentionsRubyCassandra(src string) bool {
	return strings.Contains(src, "cassandra") || strings.Contains(src, "Cassandra.cluster")
}

// mentionsRubyElastic reports whether the file references the elasticsearch-ruby
// client (the `elasticsearch` gem / `Elasticsearch::Client`) or the OpenSearch
// fork (`opensearch-ruby` / `OpenSearch::Client`), which share the identical
// `client.search(index: 'x', ...)` call shape. The import gate keeps the broad
// `index:` literal surface (esIndexKeyRe) from firing on unrelated Ruby hashes.
func mentionsRubyElastic(src string) bool {
	return strings.Contains(src, "Elasticsearch::Client") ||
		strings.Contains(src, "elasticsearch") ||
		strings.Contains(src, "Elasticsearch") ||
		strings.Contains(src, "OpenSearch::Client") ||
		strings.Contains(src, "opensearch")
}

func scanRubyDrivers(src string, funcs []funcSpan, emit emitORMQueryFn) {
	if mentionsRubyMongo(src) {
		for _, m := range rubyMongoCollRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "mongodb", false)
		}
		for _, m := range rubyMongoCollStrRe.FindAllStringSubmatchIndex(src, -1) {
			if len(m) < 4 {
				continue
			}
			caller := enclosingFuncAt(funcs, m[0])
			emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "mongodb", false)
		}
	}
	if mentionsRubyDynamo(src) {
		emitDynamoTargets(src, funcs, emit)
	}
	if mentionsRubyCassandra(src) {
		emitCQLTargets(src, funcs, emit, rubyCqlRe)
	}
	// Elasticsearch (elasticsearch-ruby / opensearch-ruby): the dominant Ruby
	// ES idiom is `client.search(index: 'users', body: {...})`, whose index
	// literal is captured by the shared esIndexKeyRe and attributed to the
	// resource by the language-agnostic emitElasticTargets — exactly as the
	// C#/PHP/Python driver passes do (#3645). Dynamic index names (a variable)
	// are honest-skipped because esIndexKeyRe only captures quoted literals.
	if mentionsRubyElastic(src) {
		emitElasticTargets(src, funcs, emit)
	}
}

// ---------------------------------------------------------------------------
// Shared target emitters (language-agnostic — keyed on the literal value)
// ---------------------------------------------------------------------------

// emitDynamoTargets finds every `TableName`/`table_name` literal in `src`
// and emits a QUERIES edge from the enclosing function to the table. Dynamic
// table names (a variable rather than a quoted literal) are skipped because
// dynamoTableNameKeyRe only captures quoted values.
func emitDynamoTargets(src string, funcs []funcSpan, emit emitORMQueryFn) {
	for _, m := range dynamoTableNameKeyRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "dynamodb", false)
	}
}

// emitElasticTargets finds every Elasticsearch index literal and emits a
// QUERIES edge to it.
func emitElasticTargets(src string, funcs []funcSpan, emit emitORMQueryFn) {
	for _, m := range esIndexKeyRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(src[m[2]:m[3]]), "find", "", "elastic", false)
	}
}

// emitCQLTargets finds every Cassandra session-execute call matched by
// `callRe`, pulls the CQL string literal out of the call's first argument,
// parses the FROM/INTO/UPDATE table out of it (via the shared SQL/CQL
// table extractor), and emits a QUERIES edge to that table. CQL whose table
// cannot be statically parsed (e.g. a runtime-built query string) is
// skipped.
func emitCQLTargets(src string, funcs []funcSpan, emit emitORMQueryFn, callRe *regexp.Regexp) {
	for _, m := range callRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 2 {
			continue
		}
		// The matcher ends at the opening paren of the call.
		argsBlob := matchCall(src, m[1]-1, 4096)
		cql := firstStringLiteral(argsBlob)
		if cql == "" {
			continue
		}
		table, verb, isJoin := extractSQLTable(cql)
		if table == "" {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(caller, capitalisedSingular(table), sqlOp(verb), "", "cassandra", isJoin)
	}
}

// lastStringLiteral returns the LAST single/double-quoted string literal in
// `blob`. Used for PHP `selectCollection('db', 'users')` where the
// collection is the final argument.
var allStrLitRe = regexp.MustCompile(`['"]((?:[^'"\\]|\\.)*)['"]`)

func lastStringLiteral(blob string) string {
	ms := allStrLitRe.FindAllStringSubmatch(blob, -1)
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1][1]
}
