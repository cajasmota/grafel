// Rust effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises Rust sink primitives:
//
//   - http_out  : reqwest::Client / reqwest::get / .get|post|put|patch|
//                 delete().send(), hyper Client::request, surf::<verb>,
//                 ureq::get/post, isahc::<verb>
//   - db_read   : sqlx `query!` / `query_as!` / `query / query_as`
//                 with SELECT-shaped SQL or fetch_*, Diesel `.load /
//                 .first / .get_result / .get_results / .find / .filter
//                 ... .load`, sea-orm `.find / .find_by_id / .all / .one`
//   - db_write  : sqlx `execute / fetch_one` on INSERT/UPDATE/DELETE,
//                 Diesel `.insert_into / .update / .delete / .execute`,
//                 sea-orm `.insert / .update / .delete / .save`
//   - fs_read   : std::fs::read / read_to_string / read_dir / File::open,
//                 tokio::fs equivalents, std::path::Path::exists
//   - fs_write  : std::fs::write / create_dir(_all) / remove_file /
//                 remove_dir(_all) / rename / set_permissions /
//                 File::create, tokio::fs::write, std::process::Command
//   - mutation  : `self.field = ...` assignment inside a method
//
// Function attribution uses the nearest preceding `fn name(` header. The
// Rust grammar's strict `fn` keyword + visibility makes this very precise.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("rust", sniffEffectsRust) }

// rustFuncHeaderRe matches `fn name(` (with optional pub / pub(...) /
// async / const / unsafe / extern qualifiers). Capture group 1 is the
// function name.
var rustFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+|const\s+|unsafe\s+|extern\s+(?:"[^"]*"\s+)?)*` +
		`fn\s+([A-Za-z_][\w]*)\s*[<(]`,
)

// rustHTTPRe matches outbound HTTP primitives.
var rustHTTPRe = regexp.MustCompile(
	`\breqwest\s*::\s*(?:get|Client\s*::\s*new|Client\s*::\s*builder|blocking\s*::\s*(?:get|Client))\b` +
		`|\bhyper\s*::\s*Client\b` +
		`|\bClient\s*::\s*new\s*\(\s*\)\s*\.\s*(?:get|post|put|patch|delete|head|request)\s*\(` +
		`|\b(?:surf|ureq|isahc)\s*::\s*(?:get|post|put|patch|delete|head|connect)\s*\(` +
		`|\.\s*(?:get|post|put|patch|delete|head)\s*\(\s*"https?://` +
		`|\.\s*send\s*\(\s*\)\s*\.\s*await\b`,
)

// rustDBReadRe matches sqlx / Diesel / sea-orm read primitives.
var rustDBReadRe = regexp.MustCompile(
	`\bsqlx\s*::\s*query(?:_as|_scalar)?(?:_unchecked)?\s*[!\(]` +
		`|\.\s*(?:fetch_all|fetch_one|fetch_optional|fetch)\s*\(` +
		`|\bdiesel\s*::\s*(?:select|sql_query)\b` +
		`|\.\s*(?:load|first|get_result|get_results|find|filter|select|order|group_by|limit|offset|inner_join|left_join|on)\s*\(` +
		`|\.\s*(?:find_by_id|find_also_related|find_with_related|all|one|stream|paginate)\s*\(`,
)

// rustSqlxSelectRe matches sqlx `query!("SELECT ...")` literals.
var rustSqlxSelectRe = regexp.MustCompile(
	`\bsqlx\s*::\s*query(?:_as|_scalar)?(?:_unchecked)?\s*!?\s*\(\s*r?"(?i:\s*(?:SELECT|WITH)\b)`,
)

// rustDBWriteRe matches sqlx / Diesel / sea-orm write primitives.
var rustDBWriteRe = regexp.MustCompile(
	`\.\s*(?:execute|execute_many)\s*\(` +
		`|\bdiesel\s*::\s*(?:insert_into|update|delete|insert_or_ignore_into|replace_into)\b` +
		`|\.\s*(?:insert|update|delete|save|save_active|insert_many|update_many|exec|exec_with_returning)\s*\(`,
)

// rustSqlxWriteRe matches sqlx `query!("INSERT/UPDATE/DELETE ...")`.
var rustSqlxWriteRe = regexp.MustCompile(
	`\bsqlx\s*::\s*query(?:_as|_scalar)?(?:_unchecked)?\s*!?\s*\(\s*r?"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)`,
)

// rustFSReadRe matches read-only filesystem primitives.
var rustFSReadRe = regexp.MustCompile(
	`\b(?:std|tokio)\s*::\s*fs\s*::\s*(?:read|read_to_string|read_dir|read_link|metadata|symlink_metadata|canonicalize)\s*\(` +
		`|\bFile\s*::\s*open\s*\(` +
		`|\bOpenOptions\s*::\s*new\s*\(\s*\)\s*\.\s*read\s*\(\s*true\b`,
)

// rustFSWriteRe matches write filesystem primitives.
var rustFSWriteRe = regexp.MustCompile(
	`\b(?:std|tokio)\s*::\s*fs\s*::\s*(?:write|create_dir|create_dir_all|remove_file|remove_dir|remove_dir_all|rename|set_permissions|copy|hard_link|symlink|symlink_dir|symlink_file)\s*\(` +
		`|\bFile\s*::\s*create\s*\(` +
		`|\bOpenOptions\s*::\s*new\s*\(\s*\)\s*\.\s*(?:write|append|create)\s*\(\s*true\b`,
)

// rustProcessRe matches process-spawn primitives (modelled as fs_write).
var rustProcessRe = regexp.MustCompile(
	`\bstd\s*::\s*process\s*::\s*Command\s*::\s*new\b` +
		`|\btokio\s*::\s*process\s*::\s*Command\s*::\s*new\b` +
		`|\bCommand\s*::\s*new\s*\([^)]+\)\s*\.\s*(?:arg|args|spawn|output|status)\b`,
)

// rustMutationRe matches `self.field = ...` assignment.
var rustMutationRe = regexp.MustCompile(
	`\bself\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsRust(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanRustFuncHeaders(content)
	var out []EffectMatch
	out = appendRustMatches(out, content, headers, rustHTTPRe, EffectHTTPOut, "reqwest/hyper/surf", 1.0)
	out = appendRustMatches(out, content, headers, rustDBReadRe, EffectDBRead, "sqlx/diesel/sea-orm.read", 0.85)
	out = appendRustMatches(out, content, headers, rustSqlxSelectRe, EffectDBRead, "sqlx::query!(SELECT)", 1.0)
	out = appendRustMatches(out, content, headers, rustDBWriteRe, EffectDBWrite, "sqlx/diesel/sea-orm.write", 0.85)
	out = appendRustMatches(out, content, headers, rustSqlxWriteRe, EffectDBWrite, "sqlx::query!(WRITE)", 1.0)
	out = appendRustMatches(out, content, headers, rustFSReadRe, EffectFSRead, "std::fs::read/File::open", 1.0)
	out = appendRustMatches(out, content, headers, rustFSWriteRe, EffectFSWrite, "std::fs::write/File::create", 1.0)
	out = appendRustMatches(out, content, headers, rustProcessRe, EffectFSWrite, "process::Command", 0.9)
	out = appendRustMatches(out, content, headers, rustMutationRe, EffectMutation, "self.field=", 0.7)
	return out
}

func scanRustFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range rustFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendRustMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
