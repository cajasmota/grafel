// Nim effect-sink sniffer (#2776 Phase 1A T3).
//
// Recognises Nim sink primitives:
//
//   - http_out  : newHttpClient() / httpclient.get / client.get / client.post /
//                 client.request, AsyncHttpClient methods
//   - db_read   : db_sqlite / db_postgres / db_mysql: db.getValue / db.getAllRows /
//                 db.getRow / db.rows (SELECT-flavoured cursor ops)
//   - db_write  : db.exec("INSERT|UPDATE|DELETE|CREATE"), db.execAffectedRows
//   - fs_read   : readFile() / readLines() / open(..., fmRead) / lines() /
//                 readAll() on FileHandle
//   - fs_write  : writeFile() / open(..., fmWrite|fmAppend) / write() on FileHandle /
//                 createDir() / removeFile() / removeDir() / copyFile() / moveFile()
//   - mutation  : field assignment on object ref (receiver.field = ...) —
//                 approximated as `result.<field> = ` or `obj.<field> = `
//
// Function attribution uses `proc name` / `method name` / `func name` headers.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("nim", sniffEffectsNim) }

// nimFuncHeaderRe matches `proc`, `method`, `func`, `template`, `macro`
// declarations. Capture group 1 is the name.
var nimFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:proc|method|func|template|macro|iterator)\s+([A-Za-z_][\w`+"`"+`]*)\s*[*\s({]`,
)

// nimHTTPRe matches httpclient primitives.
var nimHTTPRe = regexp.MustCompile(
	`\bnewHttpClient\s*\(|\bnewAsyncHttpClient\s*\(` +
		`|\b(?:client|http|httpClient|c)\s*\.\s*(?:get|post|put|patch|delete|head|request|fetch)\s*\(` +
		`|\bHttpClient\s*\(\s*\)\s*\.\s*(?:get|post|request)\s*\(`,
)

// nimDBReadRe matches db_* module read calls.
var nimDBReadRe = regexp.MustCompile(
	`\b(?:db|conn|connection)\s*\.\s*(?:getValue|getAllRows|getRow|rows|fastRows|tryExec)\s*\(`,
)

// nimDBCursorSelectRe matches db.exec with SELECT-shaped SQL.
var nimDBCursorSelectRe = regexp.MustCompile(
	`\b(?:db|conn|connection)\s*\.\s*(?:getValue|getRow|getAllRows)\s*\(\s*sql\s*["'](?i:\s*(?:SELECT|WITH)\b)`,
)

// nimDBWriteRe matches db.exec with write SQL.
var nimDBWriteRe = regexp.MustCompile(
	`\b(?:db|conn|connection)\s*\.\s*(?:exec|execAffectedRows|tryExec)\s*\(\s*sql\s*["'](?i:\s*(?:INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|REPLACE|TRUNCATE)\b)` +
		`|\b(?:db|conn|connection)\s*\.\s*(?:exec|execAffectedRows)\s*\(`,
)

// nimFSReadRe matches file-read primitives.
var nimFSReadRe = regexp.MustCompile(
	`\breadFile\s*\(` +
		`|\breadLines\s*\(` +
		`|\blines\s*\(` +
		`|\bopen\s*\(\s*[^,)]+\s*,\s*(?:fmRead|FileMode\.read)\b`,
)

// nimFSWriteRe matches file-write primitives.
var nimFSWriteRe = regexp.MustCompile(
	`\bwriteFile\s*\(` +
		`|\bopen\s*\(\s*[^,)]+\s*,\s*(?:fmWrite|fmAppend|FileMode\.write|FileMode\.append)\b` +
		`|\bcreateDir\s*\(|\bremoveFile\s*\(|\bremoveDir\s*\(` +
		`|\bcopyFile\s*\(|\bmoveFile\s*\(|\brenameFile\s*\(` +
		`|\b(?:f|file|handle|fs)\s*\.\s*write\s*\(`,
)

// nimMutationRe matches `result.field = ` or `obj.field = ` (non-comparison).
var nimMutationRe = regexp.MustCompile(
	`\b(?:result|self|this)\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsNim(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanNimFuncHeaders(content)
	var out []EffectMatch
	out = appendNimMatches(out, content, headers, nimHTTPRe, EffectHTTPOut, "httpclient", 1.0)
	out = appendNimMatches(out, content, headers, nimDBReadRe, EffectDBRead, "db_*.read", 0.85)
	out = appendNimMatches(out, content, headers, nimDBCursorSelectRe, EffectDBRead, "db.exec(SELECT)", 1.0)
	out = appendNimMatches(out, content, headers, nimDBWriteRe, EffectDBWrite, "db_*.write", 0.85)
	out = appendNimMatches(out, content, headers, nimFSReadRe, EffectFSRead, "readFile/open(fmRead)", 1.0)
	out = appendNimMatches(out, content, headers, nimFSWriteRe, EffectFSWrite, "writeFile/createDir", 1.0)
	out = appendNimMatches(out, content, headers, nimMutationRe, EffectMutation, "result.field=", 0.7)
	return out
}

func scanNimFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range nimFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendNimMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
