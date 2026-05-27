// Python effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises Python sink primitives:
//
//   - http_out  : requests.<verb>(), httpx.<verb>(), urllib.request.urlopen,
//                 aiohttp.ClientSession (.<verb>), urllib3.request
//   - db_read   : Django ORM Model.objects.<filter|get|all|...>,
//                 SQLAlchemy session.query / .execute (SELECT),
//                 raw cursor.execute("SELECT ..."), cursor.fetchall/fetchone
//   - db_write  : Django .save() / .create() / .update() / .delete() /
//                 .bulk_create / .bulk_update,
//                 SQLAlchemy session.add / .commit / .delete,
//                 cursor.execute("INSERT|UPDATE|DELETE ...")
//   - fs_read   : open(...), pathlib.Path.read_*, os.listdir, os.scandir,
//                 io.open
//   - fs_write  : open(..., "w"|"a"|"x"|"wb"|...), pathlib.Path.write_*,
//                 os.mkdir, os.remove, os.rename, shutil.copy*
//   - mutation  : self.<attr> = ... (assignment to a method receiver)
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

// pyHTTPRe matches HTTP-client primitives.
var pyHTTPRe = regexp.MustCompile(
	`\b(?:requests|httpx|urllib3)\s*\.\s*(?:get|post|put|patch|delete|head|options|request)\s*\(` +
		`|\burllib\s*\.\s*request\s*\.\s*urlopen\s*\(` +
		`|\baiohttp\s*\.\s*ClientSession\b` +
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

// pyDBWriteRe matches ORM / session write primitives.
var pyDBWriteRe = regexp.MustCompile(
	`\.\s*(?:save|delete|update|bulk_create|bulk_update|create|get_or_create|update_or_create)\s*\(` +
		`|\bsession\s*\.\s*(?:add|add_all|delete|commit|flush|merge)\s*\(`,
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

func sniffEffectsPython(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanPyFuncHeaders(content)
	var out []EffectMatch
	out = appendPyMatches(out, content, headers, pyHTTPRe, EffectHTTPOut, "requests/httpx", 1.0)
	out = appendPyMatches(out, content, headers, pyDBReadRe, EffectDBRead, "orm.read", 0.85)
	out = appendPyMatches(out, content, headers, pyCursorSelectRe, EffectDBRead, "cursor.execute(SELECT)", 1.0)
	out = appendPyMatches(out, content, headers, pyDBWriteRe, EffectDBWrite, "orm.write", 0.85)
	out = appendPyMatches(out, content, headers, pyCursorWriteRe, EffectDBWrite, "cursor.execute(WRITE)", 1.0)
	out = appendPyMatches(out, content, headers, pyFSReadRe, EffectFSRead, "open/pathlib", 0.9)
	out = appendPyMatches(out, content, headers, pyFSWriteRe, EffectFSWrite, "open(w)/shutil", 1.0)
	out = appendPyMatches(out, content, headers, pyMutationRe, EffectMutation, "self.field=", 0.7)
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
