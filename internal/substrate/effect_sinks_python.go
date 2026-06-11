// Python effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises Python sink primitives:
//
//   - http_out  : requests.<verb>(), httpx.<verb>(), urllib.request.urlopen,
//     aiohttp.ClientSession (.<verb>), urllib3.request,
//     boto3.client/resource(...) + AWS client ops (S3 upload/
//     download/put/get/copy, SQS send/receive, SNS publish,
//     Lambda invoke) — every boto3 call crosses the network
//   - db_read   : Django ORM Model.objects.<filter|get|all|...>,
//     SQLAlchemy session.query / .execute (SELECT),
//     raw cursor.execute("SELECT ..."), cursor.fetchall/fetchone,
//     DB-API driver connect/cursor (mysql.connector, psycopg2,
//     sqlite3, pymysql, MySQLdb, cx_Oracle, pyodbc, asyncpg),
//     MongoDB pymongo/motor reads (.aggregate, .find, .find_one,
//     .count_documents, .distinct, ...)
//   - db_write  : Django .save() / .create() / .update() / .delete() /
//     .bulk_create / .bulk_update,
//     SQLAlchemy session.add / .commit / .delete,
//     cursor.execute("INSERT|UPDATE|DELETE ..."),
//     MongoDB pymongo/motor writes (.insert_one/.update_many/
//     .delete_one/.bulk_write/.find_one_and_update/...)
//   - fs_read   : open(...), pathlib.Path.read_*, os.listdir, os.scandir,
//     io.open
//   - fs_write  : open(..., "w"|"a"|"x"|"wb"|...), pathlib.Path.write_*,
//     os.mkdir, os.remove, os.rename, shutil.copy*
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
//
// #4668 — read/write attribution asymmetry. pyDBWriteRe matches the write
// verbs (.save/.create/.delete/...) on ANY receiver (a bare `.save(`), so a
// layered repository's `self.queryset.save(obj)` correctly resolves db_write
// and propagates up the controller→service→repo CALLS chain. The read side
// only matched the `.objects.<verb>` MANAGER form, so a repository/query-builder
// read held on a variable or attribute — `self.queryset.filter(...)`,
// `qs.exclude(...)`, `self.get_queryset().annotate(...)` — was MISSED, and the
// GET/list handler that delegates to it resolved PURE (the ~40 false-pure read
// endpoints stub_detector flagged). We now ALSO match the distinctive Django
// queryset read terminals as BARE verbs on any receiver, mirroring the write
// side. Only queryset-distinctive names are bare-matched (filter/exclude/
// annotate/select_related/...) — names that collide with builtins on dict/list
// (`.get(`, `.all(`, `.first(`, `.count(`, `.values(`) stay gated to the
// `.objects.` manager form to avoid false positives. The bare set is the
// long-term general fix for the read-sink reach gap, not a per-endpoint patch.
var pyDBReadRe = regexp.MustCompile(
	`\.\s*objects\s*\.\s*(?:all|filter|exclude|get|first|last|count|exists|values|values_list|annotate|aggregate|raw|none|in_bulk|earliest|latest)\b` +
		`|\.\s*(?:filter|exclude|annotate|select_related|prefetch_related|values_list|only|defer|distinct|order_by|get_queryset)\s*\(` +
		`|\.\s*query\s*\(` +
		`|\.\s*fetchall\s*\(|\.\s*fetchone\s*\(|\.\s*fetchmany\s*\(` +
		`|\bsession\s*\.\s*(?:query|execute|scalar|scalars|get)\s*\(`,
)

// pyDBReadHelperRe matches the Django shortcut read helpers that fetch a row /
// list and 404 otherwise. These are distinctive names with no builtin/collection
// collision, so they are safe to bare-match anywhere (#4691).
var pyDBReadHelperRe = regexp.MustCompile(
	`\bget_object_or_404\s*\(|\bget_list_or_404\s*\(`,
)

// --- #4691 queryset-receiver typing (ambiguous read terminals) ---
//
// #4694 (pyDBReadRe) bare-matched the DISTINCTIVE queryset read verbs
// (filter/exclude/annotate/...) but kept the builtin-colliding terminals
// (.get/.first/.last/.all/.exists/.count/.values/.values_list) gated to the
// `.objects.<verb>` manager form, because `d.get("k")` (dict) and
// `m.count()` (list/Mock) would otherwise be mis-credited as db_read.
//
// #4691 closes that gap with lightweight, content-local receiver typing: we
// learn which local/attribute names hold a QuerySet/Manager (assigned from a
// queryset-producing expression, or the inherent `self.queryset` /
// `self.get_queryset()` handles), then credit the ambiguous read terminals
// ONLY when the receiver is one of those typed names. A read terminal on an
// untyped receiver (a dict, a Mock, a generic object) stays non-read — the
// false-positive guard #4694 introduced is preserved exactly.

// pyQSAssignRe captures `<name> = <rhs>` assignments whose RHS is a
// queryset-producing expression. Group 1 is the assigned (now queryset-typed)
// name. The RHS alternatives:
//   - `<recv>.objects.<verb>(` / `<recv>.objects`            (Django manager)
//   - `<recv>.filter|exclude|annotate|...(`                  (chained queryset op)
//   - `self.get_queryset(` / `self.queryset`                 (DRF/CBV handles)
//   - `<typedName>.filter|...` chains feed back via the iterative pass below.
var pyQSAssignRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_]\w*)\s*=\s*` +
		`(?:[A-Za-z_][\w.]*\s*\.\s*objects\b` +
		`|[A-Za-z_][\w.]*\s*\.\s*(?:filter|exclude|annotate|select_related|prefetch_related|values_list|only|defer|distinct|order_by|get_queryset)\s*\(` +
		`|self\s*\.\s*get_queryset\s*\(` +
		`|self\s*\.\s*queryset\b)`,
)

// pyQSChainAssignRe captures `<name> = <knownQS>.<op>(...)` — a reassignment
// whose RHS receiver is an already-queryset-typed name. Group 1 = assigned
// name, group 2 = source receiver name. Used to propagate typing across
// `qs = qs.filter(...)` style chains in a fixpoint loop.
var pyQSChainAssignRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*)\s*\.\s*` +
		`(?:filter|exclude|annotate|select_related|prefetch_related|values_list|only|defer|distinct|order_by|all|none|union|intersection|difference|reverse)\s*\(`,
)

// pyQSAmbiguousVerbs are the read terminals that collide with Python builtins
// on dict/list/etc., so they are credited db_read ONLY on a queryset-typed
// receiver (see querysetReadMatches).
const pyQSAmbiguousVerbs = `get|first|last|all|exists|count|values|values_list`

// pyInherentQSReadRe credits the ambiguous read terminals when chained
// DIRECTLY off an inherent queryset handle — `self.get_queryset().first()`,
// `self.queryset.count()`, `Model.objects.filter(...).get(...)` — without
// needing an intermediate assignment. The `.objects` chain form is already
// covered by pyDBReadRe's manager alternative; here we add the
// `self.get_queryset()` / `self.queryset` handles, whose terminal read verb
// would otherwise be missed.
var pyInherentQSReadRe = regexp.MustCompile(
	`self\s*\.\s*get_queryset\s*\(\s*\)\s*\.\s*(?:` + pyQSAmbiguousVerbs + `)\s*\(` +
		`|self\s*\.\s*queryset\s*\.\s*(?:` + pyQSAmbiguousVerbs + `)\s*\(`,
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

// pyMongoReadRe matches MongoDB collection read primitives for the pymongo
// (sync) and motor (async) drivers (#3440 ask 4). These call-shapes — e.g.
// `collection.aggregate(pipeline)`, `coll.find_one({...})` — are NOT covered
// by pyDBReadRe because they lack the Django `.objects.` prefix and the
// receiver is a plain collection handle. The snake_case `_one`/`_documents`
// and `aggregate`/`distinct` names are Mongo-specific enough to be safe;
// bare `.find(` is the canonical Mongo cursor read. find_one_and_* are
// excluded here (they mutate) and handled by pyMongoWriteRe.
var pyMongoReadRe = regexp.MustCompile(
	`\.\s*(?:aggregate|aggregate_raw_batches|find_raw_batches|count_documents|estimated_document_count|distinct|list_indexes|index_information)\s*\(` +
		`|\.\s*find_one\s*\(` +
		`|\.\s*find\s*\(`,
)

// pyMongoWriteRe matches MongoDB collection write primitives for pymongo /
// motor (#3440 ask 4). Covers the document mutators plus the find-and-modify
// family (which both read and mutate — classified write, the stronger
// signal). `insert_one`/`update_many`/`bulk_write`/`find_one_and_update` are
// distinctive Mongo names with no common false-positive collisions.
var pyMongoWriteRe = regexp.MustCompile(
	`\.\s*(?:insert_one|insert_many|update_one|update_many|replace_one|delete_one|delete_many|bulk_write|find_one_and_update|find_one_and_replace|find_one_and_delete|create_index|create_indexes|drop_index|drop_indexes)\s*\(`,
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
	out = appendPyMatches(out, content, headers, pyDBReadHelperRe, EffectDBRead, "orm.read.shortcut", 0.9)
	out = appendPyMatches(out, content, headers, pyInherentQSReadRe, EffectDBRead, "orm.read.queryset", 0.85)
	out = append(out, querysetReadMatches(content, headers)...)
	out = appendPyMatches(out, content, headers, pyCursorSelectRe, EffectDBRead, "cursor.execute(SELECT)", 1.0)
	out = appendPyMatches(out, content, headers, pyDBConnectRe, EffectDBRead, "db.connect/cursor", 0.8)
	out = appendPyMatches(out, content, headers, pyDBWriteRe, EffectDBWrite, "orm.write", 0.85)
	out = appendPyMatches(out, content, headers, pyCursorWriteRe, EffectDBWrite, "cursor.execute(WRITE)", 1.0)
	out = appendPyMatches(out, content, headers, pyMongoReadRe, EffectDBRead, "mongo.read", 0.8)
	out = appendPyMatches(out, content, headers, pyMongoWriteRe, EffectDBWrite, "mongo.write", 0.85)
	out = appendPyMatches(out, content, headers, pyFSReadRe, EffectFSRead, "open/pathlib", 0.9)
	out = appendPyMatches(out, content, headers, pyFSWriteRe, EffectFSWrite, "open(w)/shutil", 1.0)
	out = appendPyMatches(out, content, headers, pyMutationRe, EffectMutation, "self.field=", 0.7)
	out = appendPyMatches(out, content, headers, pyEnvReadRe, EffectEnvRead, "os.environ/getenv", 1.0)
	return out
}

// querysetReadMatches implements the #4691 receiver-typed read credit. It
// (1) collects the set of local/attribute names known to hold a QuerySet/
// Manager (assigned from a queryset-producing RHS, propagated across chained
// reassignments to a fixpoint), then (2) emits a db_read EffectMatch for each
// ambiguous read terminal (.get/.first/.last/.all/.exists/.count/.values/
// .values_list) invoked on one of those typed receiver names. A read terminal
// on an UNTYPED receiver (a dict, a Mock, a generic object) yields nothing —
// the #4694 false-positive guard is preserved: only queryset-typed receivers
// earn the credit. Module/function scope is not segmented (the sniffer is a
// flat content scan, like every other sink here); typing is conservative
// enough that cross-scope name reuse is a non-issue in practice.
func querysetReadMatches(content string, headers []funcHeader) []EffectMatch {
	typed := collectQuerysetTypedNames(content)
	if len(typed) == 0 {
		return nil
	}
	var out []EffectMatch
	for name := range typed {
		// `<name> . <ambiguous-verb> (` — receiver is the typed queryset name.
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\.\s*(?:` + pyQSAmbiguousVerbs + `)\s*\(`)
		for _, m := range re.FindAllStringIndex(content, -1) {
			line := lineOfOffset(content, m[0])
			out = append(out, EffectMatch{
				Function:   nearestHeader(headers, line),
				Line:       line,
				Effect:     EffectDBRead,
				Sink:       "orm.read.queryset",
				Confidence: 0.85,
			})
		}
	}
	return out
}

// collectQuerysetTypedNames returns the set of names that hold a QuerySet /
// Manager. Seeds from queryset-producing assignments (pyQSAssignRe), then
// iterates pyQSChainAssignRe to a fixpoint so `qs = base.filter(...)` followed
// by `qs2 = qs.exclude(...)` types both names.
func collectQuerysetTypedNames(content string) map[string]bool {
	typed := map[string]bool{}
	for _, m := range pyQSAssignRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 && m[1] != "" {
			typed[m[1]] = true
		}
	}
	chains := pyQSChainAssignRe.FindAllStringSubmatch(content, -1)
	for {
		changed := false
		for _, m := range chains {
			if len(m) < 3 {
				continue
			}
			dst, srcName := m[1], m[2]
			if dst != "" && srcName != "" && typed[srcName] && !typed[dst] {
				typed[dst] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return typed
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
