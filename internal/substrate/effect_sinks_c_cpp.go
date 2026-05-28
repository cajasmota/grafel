// C / C++ effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises sink primitives in plain C and modern C++. The C and C++
// substrates share one sniffer because they share extension space
// (#2732) and most sink primitives — libpq, MySQL C API, libcurl, the
// POSIX/stdio file primitives, and the POSIX process primitives — are
// callable from either language. C++-only primitives (std::ifstream /
// std::ofstream, std::filesystem) are included via separate alternates.
//
//   - http_out  : `curl_easy_perform`, `curl_easy_setopt(..., CURLOPT_URL, ...)`,
//                 `curl_multi_perform`, cpp-httplib `httplib::Client`,
//                 Boost.Beast, POCO `HTTPClientSession`
//   - db_read   : libpq `PQexec(... , "SELECT ...")`, `PQexecParams`,
//                 `PQprepare`, MySQL C `mysql_query("SELECT...")`,
//                 SQLite `sqlite3_prepare_v2(... "SELECT" ...)` /
//                 `sqlite3_step` after a SELECT, ODBC `SQLExecDirect`
//   - db_write  : libpq PQexec/PQexecParams with INSERT|UPDATE|DELETE,
//                 MySQL `mysql_query("INSERT|UPDATE|DELETE...")`, SQLite
//                 with non-SELECT, generic `PQexec` (assumed write at low
//                 confidence when SQL keyword cannot be recognised)
//   - fs_read   : `fopen(..., "r")`, `freopen(..., "r")`, `read`, `pread`,
//                 `open(..., O_RDONLY)`, std::ifstream, std::filesystem::
//                 directory_iterator / read_symlink
//   - fs_write  : `fopen(..., "w"|"a"|"x"|...)`, `freopen("w"...)`,
//                 `write`, `pwrite`, `open(..., O_WRONLY|O_CREAT...)`,
//                 `mkdir`, `rmdir`, `unlink`, `rename`, `chmod`,
//                 `std::ofstream`, `std::filesystem::create_directory*`,
//                 `std::filesystem::remove*`
//   - mutation  : `this->member = ...` assignment inside a member
//                 function. C-style struct mutation via `obj.field = ...`
//                 is too noisy without alias analysis; deferred to
//                 Phase 4.
//
// Function attribution uses the nearest preceding `name(...) {` header
// on a line. C and C++ grammars admit many declaration shapes — we accept
// the canonical "return-type name(...)" forms and let the substrate's
// line-based attribution handle the rest.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("c-cpp", sniffEffectsCCPP) }

// ccppFuncHeaderRe matches a function definition header. We accept any
// line that ends with `name(...args) {` (with optional trailing `const`,
// `noexcept`, `override`, etc., absorbed by allowing a brace anywhere on
// the same line OR on the next line — for the next-line case the
// nearest-header heuristic still picks the right function).
//
// Capture group 1 is the bare function name.
var ccppFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:[A-Za-z_][\w*&:<>,\s]+\s+)` + // return type (with qualifiers, pointers, refs, namespaces)
		`(?:[A-Za-z_][\w:]*::)?` + // optional class qualifier
		`([A-Za-z_][\w]*)\s*\([^;{]*\)\s*` + // name + params
		`(?:const\s+)?(?:noexcept(?:\([^)]*\))?\s+)?(?:override\s+)?(?:final\s+)?(?:->[^{;]+)?\s*` +
		`\{`,
)

// ccppHTTPRe matches outbound HTTP primitives.
var ccppHTTPRe = regexp.MustCompile(
	`\bcurl_easy_perform\s*\(` +
		`|\bcurl_easy_setopt\s*\([^,]+,\s*CURLOPT_URL\b` +
		`|\bcurl_multi_perform\s*\(` +
		`|\bhttplib\s*::\s*(?:Client|SSLClient)\b` +
		`|\bboost\s*::\s*beast\s*::\s*http\b` +
		`|\bPoco\s*::\s*Net\s*::\s*HTTPClientSession\b`,
)

// ccppDBReadRe matches database read primitives. We use SQL-keyword
// matching to disambiguate read vs write where the same API is used for
// both (PQexec, mysql_query, sqlite3_prepare_v2).
var ccppDBReadRe = regexp.MustCompile(
	`\bPQexec(?:Params|Prepared)?\s*\(\s*[^,]+,\s*"(?i:\s*(?:SELECT|WITH)\b)` +
		`|\bmysql_query\s*\(\s*[^,]+,\s*"(?i:\s*(?:SELECT|WITH)\b)` +
		`|\bsqlite3_prepare_v2\s*\(\s*[^,]+,\s*"(?i:\s*(?:SELECT|WITH)\b)` +
		`|\bSQLExecDirect\s*\(\s*[^,]+,\s*\(?\s*(?:SQLCHAR\s*\*\s*\)\s*)?"(?i:\s*(?:SELECT|WITH)\b)`,
)

// ccppDBWriteRe matches database write primitives.
var ccppDBWriteRe = regexp.MustCompile(
	`\bPQexec(?:Params|Prepared)?\s*\(\s*[^,]+,\s*"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)` +
		`|\bmysql_query\s*\(\s*[^,]+,\s*"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)` +
		`|\bsqlite3_prepare_v2\s*\(\s*[^,]+,\s*"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)` +
		`|\bsqlite3_exec\s*\(`,
)

// ccppDBExecRe matches generic `PQexec` / `mysql_query` calls where the
// SQL keyword could not be matched (variable, multi-line, concatenated).
// Classified as both db_read and db_write at low confidence.
var ccppDBExecRe = regexp.MustCompile(
	`\b(?:PQexec|mysql_query|sqlite3_exec)\s*\(`,
)

// ccppFSReadRe matches read-only filesystem primitives.
var ccppFSReadRe = regexp.MustCompile(
	`\bfopen\s*\(\s*[^,)]+\s*,\s*"r[bt+]?"` +
		`|\bfreopen\s*\(\s*[^,)]+\s*,\s*"r[bt+]?"` +
		`|\b(?:read|pread)\s*\(\s*[^,)]+\s*,` +
		`|\bopen\s*\(\s*[^,)]+\s*,\s*O_RDONLY\b` +
		`|\bstd\s*::\s*ifstream\s+[A-Za-z_]` +
		`|\bstd\s*::\s*filesystem\s*::\s*(?:directory_iterator|recursive_directory_iterator|read_symlink|exists|file_size|status)\b` +
		`|\bstat\s*\(`,
)

// ccppFSWriteRe matches write filesystem primitives.
var ccppFSWriteRe = regexp.MustCompile(
	`\bfopen\s*\(\s*[^,)]+\s*,\s*"(?:w|a|x)[bt+]?"` +
		`|\bfreopen\s*\(\s*[^,)]+\s*,\s*"(?:w|a|x)[bt+]?"` +
		`|\b(?:write|pwrite)\s*\(\s*[^,)]+\s*,` +
		`|\bopen\s*\(\s*[^,)]+\s*,\s*(?:O_WRONLY|O_CREAT|O_RDWR|O_APPEND|O_TRUNC)` +
		`|\b(?:mkdir|rmdir|unlink|rename|chmod|chown|symlink|link|truncate|ftruncate)\s*\(` +
		`|\bstd\s*::\s*ofstream\s+[A-Za-z_]` +
		`|\bstd\s*::\s*filesystem\s*::\s*(?:create_directory|create_directories|remove|remove_all|rename|copy|copy_file|resize_file|permissions)\b`,
)

// ccppProcessRe matches process-spawn primitives (modelled as fs_write).
var ccppProcessRe = regexp.MustCompile(
	`\b(?:system|popen|execv|execve|execvp|execvpe|execl|execlp|execle|fork|vfork|posix_spawn|posix_spawnp)\s*\(`,
)

// ccppMutationRe matches `this->member = ...` assignment.
var ccppMutationRe = regexp.MustCompile(
	`\bthis\s*->\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsCCPP(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanCCPPFuncHeaders(content)
	var out []EffectMatch
	out = appendCCPPMatches(out, content, headers, ccppHTTPRe, EffectHTTPOut, "libcurl/cpp-httplib/Boost.Beast", 1.0)
	out = appendCCPPMatches(out, content, headers, ccppDBReadRe, EffectDBRead, "libpq/mysql/sqlite/odbc(SELECT)", 1.0)
	out = appendCCPPMatches(out, content, headers, ccppDBWriteRe, EffectDBWrite, "libpq/mysql/sqlite(WRITE)", 1.0)
	out = appendCCPPMatches(out, content, headers, ccppDBExecRe, EffectDBWrite, "PQexec/mysql_query(generic)", 0.6)
	out = appendCCPPMatches(out, content, headers, ccppFSReadRe, EffectFSRead, "fopen(r)/read/ifstream", 1.0)
	out = appendCCPPMatches(out, content, headers, ccppFSWriteRe, EffectFSWrite, "fopen(w)/write/ofstream", 1.0)
	out = appendCCPPMatches(out, content, headers, ccppProcessRe, EffectFSWrite, "system/exec/posix_spawn", 0.9)
	out = appendCCPPMatches(out, content, headers, ccppMutationRe, EffectMutation, "this->field=", 0.7)
	return out
}

func scanCCPPFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range ccppFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if ccppControlKeyword(name) {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

// ccppControlKeyword rejects keywords that the conservative header regex
// can accidentally match (e.g. `if (...)` or `while (...)` introducing a
// block). The "return-type name(...) {" shape collides with control-flow
// when the return type slot eats the keyword's left-hand neighbour.
func ccppControlKeyword(s string) bool {
	switch s {
	case "if", "else", "for", "while", "switch", "case", "catch", "try", "do", "return", "throw", "new", "delete", "sizeof", "typeid", "decltype", "static_assert":
		return true
	}
	return false
}

func appendCCPPMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
