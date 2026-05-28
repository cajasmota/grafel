// Python effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises Python sink primitives:
//
//   - http_out  : requests.<verb>(), httpx.<verb>(), urllib.request.urlopen,
//                 aiohttp.ClientSession (.<verb>), urllib3.request,
//                 boto3.client/resource(...) + AWS client ops (S3 upload/
//                 download/put/get/copy, SQS send/receive, SNS publish,
//                 Lambda invoke) — every boto3 call crosses the network
//   - db_read   : Django ORM Model.objects.<filter|get|all|...>,
//                 SQLAlchemy session.query / .execute (SELECT),
//                 raw cursor.execute("SELECT ..."), cursor.fetchall/fetchone,
//                 DB-API driver connect/cursor (mysql.connector, psycopg2,
//                 sqlite3, pymysql, MySQLdb, cx_Oracle, pyodbc, asyncpg)
//   - db_write  : Django .save() / .create() / .update() / .delete() /
//                 .bulk_create / .bulk_update,
//                 SQLAlchemy session.add / .commit / .delete,
//                 cursor.execute("INSERT|UPDATE|DELETE ...")
//   - fs_read   : open(...), pathlib.Path.read_*, os.listdir, os.scandir,
//                 io.open
//   - fs_write  : open(..., "w"|"a"|"x"|"wb"|...), pathlib.Path.write_*,
//                 os.mkdir, os.remove, os.rename, shutil.copy*
//   - mutation  : self.<attr> = ... (assignment to a method receiver)
//   - env_read  : os.getenv(...), os.environ[...] / os.environ.get(...)
//
// Function attribution uses the same "nearest preceding header" heuristic
// as the JS/TS sniffer; Python's indentation rules make this more reliable
// in practice because nested defs are visibly indented.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("python", sniffEffectsPython) }

// pyFuncHeaderRe matches `def name(` or `async def name(`. Capture group 1
// is the declaring name. Indentation is allowed (class methods) but the
// header must own the line up to the first `(`.
var pyFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_][\w]*)\s*\(`,
)

// pyHTTPRe matches outbound HTTP / network-client primitives. Includes
// the common requests/httpx/urllib3/aiohttp clients plus boto3/botocore
// AWS clients (S3/SQS/... upload/download/get/put/list/copy/send) — every
// boto3 call crosses the network, so we classify it as http_out (the
// lattice has no separate "network" element). urlopen and the bare
// `.get("http://...")` shape are also caught.
var pyHTTPRe = regexp.MustCompile(
	`\b(?:requests|httpx|urllib3)\s*\.\s*(?:get|post|put|patch|delete|head|options|request)\s*\(` +
		`|\burllib\s*\.\s*request\s*\.\s*urlopen\s*\(|\burlopen\s*\(` +
		`|\baiohttp\s*\.\s*ClientSession\b` +
		`|\bboto3\s*\.\s*(?:client|resource)\s*\(` +
		`|\.\s*(?:upload_file|upload_fileobj|download_file|download_fileobj|put_object|get_object|copy_object|delete_object|list_objects(?:_v2)?|head_object|generate_presigned_url|send_message|receive_message|publish|invoke)\s*\(` +
		`|\.\s*(?:get|post|put|patch|delete|head|options)\s*\(\s*['"]https?://`,
)

// pyDBReadRe matches ORM / raw-cursor read primitives. We deliberately
// pair `cursor.execute(...)` with a SELECT heuristic via a separate
// regex (pyCursorSelectRe) so we don't double-count execute() as both
// read and write.
var pyDBReadRe = regexp.MustCompile(
	`\.\s*objects\s*\.\s*(?:all|filter|exclude|get|first|last|count|exists|values|values_list|annotate|aggregate|raw|none|in_bulk|earliest|latest)\b` +
		`|\.\s*query\s*\(` +
		`|\.\s*fetchall\s*\(|\.\s*fetchone\s*\(|\.\s*fetchmany\s*\(` +
		`|\bsession\s*\.\s*(?:query|execute|scalar|scalars|get)\s*\(`,
)

// pyCursorSelectRe matches `cursor.execute("SELECT ...")` style raw reads.
// Case-insensitive on the SQL keyword. Quote-agnostic.
var pyCursorSelectRe = regexp.MustCompile(
	`\.\s*execute\s*\(\s*['"](?i:\s*(?:SELECT|WITH)\b)`,
)

// pyDBConnectRe matches raw-driver connection / cursor primitives for the
// common DB-API drivers (mysql.connector, psycopg2, sqlite3, pymysql,
// MySQLdb, cx_Oracle, pyodbc, asyncpg) plus a bare `.cursor()` call and
// `cursor.execute(<non-literal>)` where the SQL is a variable (so the
// SELECT/WRITE keyword sniffers can't see it). Establishing a connection
// or opening a cursor is itself DB I/O — we classify it db_read as the
// conservative "this function talks to a database" signal; explicit
// writes are still upgraded to db_write by pyCursorWriteRe / pyDBWriteRe.
var pyDBConnectRe = regexp.MustCompile(
	`\b(?:mysql\s*\.\s*connector|psycopg2|psycopg|sqlite3|pymysql|MySQLdb|cx_Oracle|pyodbc|asyncpg)\s*\.\s*(?:connect|Connection)\s*\(` +
		`|\.\s*cursor\s*\(\s*\)` +
		`|\.\s*execute\s*\(\s*[A-Za-z_]`,
)

// pyDBWriteRe matches ORM / session / raw-connection write primitives.
// A bare `.commit(` / `.rollback(` is a transaction boundary on any
// DB-API connection (conn.commit()), so it counts as db_write even when
// the receiver is not a named `session`.
var pyDBWriteRe = regexp.MustCompile(
	`\.\s*(?:save|delete|update|bulk_create|bulk_update|create|get_or_create|update_or_create)\s*\(` +
		`|\bsession\s*\.\s*(?:add|add_all|delete|commit|flush|merge)\s*\(` +
		`|\.\s*(?:commit|rollback)\s*\(\s*\)`,
)

// pyCursorWriteRe matches raw cursor INSERT/UPDATE/DELETE.
var pyCursorWriteRe = regexp.MustCompile(
	`\.\s*execute\s*\(\s*['"](?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)`,
)

// pyFSReadRe matches read-only filesystem primitives.
var pyFSReadRe = regexp.MustCompile(
	`\bopen\s*\(\s*[^,)]+\s*(?:,\s*['"](?:r|rb|rt)['"][\s,)])` +
		`|\bopen\s*\(\s*[^,)]+\s*\)` + // single-arg open() defaults to "r"
		`|\.\s*read_(?:text|bytes)\s*\(` +
		`|\bos\s*\.\s*(?:listdir|scandir|stat|lstat|walk)\s*\(` +
		`|\bpathlib\s*\.\s*Path\b[^=]*\.\s*read_`,
)

// pyFSWriteRe matches write filesystem primitives, including mode-arg
// open() calls.
var pyFSWriteRe = regexp.MustCompile(
	`\bopen\s*\(\s*[^,)]+\s*,\s*['"](?:w|wb|wt|a|ab|at|x|xb|xt|r\+|rb\+|w\+|wb\+|a\+|ab\+)['"]` +
		`|\.\s*write_(?:text|bytes)\s*\(` +
		`|\bos\s*\.\s*(?:mkdir|makedirs|remove|unlink|rmdir|rename|replace|chmod|chown|symlink|link)\s*\(` +
		`|\bshutil\s*\.\s*(?:copy|copy2|copyfile|copytree|move|rmtree)\s*\(`,
)

// pyMutationRe matches `self.<attr> = ...`. Excludes `==` comparison by
// requiring a non-`=` continuation. Excludes `self.attr += ...` style
// augmented assignment via the same anchor — those are also mutations
// but the simple-assignment shape is the common case.
var pyMutationRe = regexp.MustCompile(
	`\bself\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

// pyEnvReadRe matches process-environment reads: os.environ[...] /
// os.environ.get(...) / os.getenv(...) / os.environb. Module-style
// `environ` (from os import environ) is also caught via the bare form.
var pyEnvReadRe = regexp.MustCompile(
	`\bos\s*\.\s*getenv\s*\(` +
		`|\bos\s*\.\s*environb?\s*(?:\.\s*get\s*\(|\[)` +
		`|\bgetenv\s*\(` +
		`|\benviron\s*\.\s*get\s*\(`,
)

func sniffEffectsPython(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanPyFuncHeaders(content)
	var out []EffectMatch
	out = appendPyMatches(out, content, headers, pyHTTPRe, EffectHTTPOut, "requests/httpx", 1.0)
	out = appendPyMatches(out, content, headers, pyDBReadRe, EffectDBRead, "orm.read", 0.85)
	out = appendPyMatches(out, content, headers, pyCursorSelectRe, EffectDBRead, "cursor.execute(SELECT)", 1.0)
	out = appendPyMatches(out, content, headers, pyDBConnectRe, EffectDBRead, "db.connect/cursor", 0.8)
	out = appendPyMatches(out, content, headers, pyDBWriteRe, EffectDBWrite, "orm.write", 0.85)
	out = appendPyMatches(out, content, headers, pyCursorWriteRe, EffectDBWrite, "cursor.execute(WRITE)", 1.0)
	out = appendPyMatches(out, content, headers, pyFSReadRe, EffectFSRead, "open/pathlib", 0.9)
	out = appendPyMatches(out, content, headers, pyFSWriteRe, EffectFSWrite, "open(w)/shutil", 1.0)
	out = appendPyMatches(out, content, headers, pyMutationRe, EffectMutation, "self.field=", 0.7)
	out = appendPyMatches(out, content, headers, pyEnvReadRe, EffectEnvRead, "os.environ/getenv", 1.0)
	return out
}

func scanPyFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range pyFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

func appendPyMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     eff,
			Sink:       sink,
			Confidence: conf,
		})
	}
	return out
}
