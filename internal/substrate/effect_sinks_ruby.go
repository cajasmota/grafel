// Ruby effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises Ruby sink primitives:
//
//   - http_out  : Net::HTTP.<get|post|start|...>, HTTParty.<verb>, RestClient
//     .<verb>, Faraday.<verb>, Excon.<verb>, open-uri's
//     URI.open / URI.parse(...).open
//   - db_read   : ActiveRecord `.where / .find / .find_by / .find_each /
//     .all / .first / .last / .pluck / .count / .exists?`,
//     raw `connection.execute("SELECT ...")`, `ActiveRecord::
//     Base.connection.exec_query`
//   - db_write  : ActiveRecord `.create / .create! / .update / .update! /
//     .update_all / .destroy / .destroy_all / .delete /
//     .delete_all / .save / .save! / .insert / .insert_all /
//     .upsert / .upsert_all`, raw `connection.execute(
//     "INSERT|UPDATE|DELETE ...")`, and Sequel dataset writes
//     `DB[:table]…insert/update/delete/multi_insert/import/
//     truncate` (table named in the sink, e.g. sequel.write(users))
//   - fs_read   : File.read / .open (default "r"), File.readlines, IO.read,
//     Dir.entries / .glob / .[], Pathname#read
//   - fs_write  : File.write / .open with "w"/"a"/"x"/binary modes,
//     File.delete / .unlink / .rename, FileUtils.cp / .mv /
//     .rm / .mkdir / .mkdir_p
//   - mutation  : `@ivar = ...` instance-variable assignment inside a
//     method body. Excludes `==` comparisons.
//
// Function attribution uses the nearest preceding `def` header — the
// same heuristic the other sniffers use. Ruby's indentation-free grammar
// makes this slightly less precise than Python but accurate enough for
// the substrate's needs.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("ruby", sniffEffectsRuby) }

// rubyFuncHeaderRe matches `def name` or `def self.name`. Capture group 1
// is the bare method name.
var rubyFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*def\s+(?:self\s*\.\s*)?([A-Za-z_][\w]*[!?=]?)\b`,
)

// rubyHTTPRe matches outbound HTTP primitives.
var rubyHTTPRe = regexp.MustCompile(
	`\bNet::HTTP\s*\.\s*(?:get|get_response|post|post_form|start|put|delete|head|patch|request)\b` +
		`|\bHTTParty\s*\.\s*(?:get|post|put|patch|delete|head|options)\s*\(` +
		`|\bRestClient\s*\.\s*(?:get|post|put|patch|delete|head)\s*\(` +
		`|\bFaraday\s*\.\s*(?:get|post|put|patch|delete|head|new)\b` +
		`|\bExcon\s*\.\s*(?:get|post|put|patch|delete|head|new)\b` +
		`|\bURI\s*\.\s*(?:open|parse)\s*\([^)]*\)\s*\.\s*(?:read|open)\b` +
		`|\bopen-uri\b`,
)

// rubyDBReadRe matches ActiveRecord and raw read primitives.
var rubyDBReadRe = regexp.MustCompile(
	`\.\s*(?:where|find|find_by|find_each|find_in_batches|all|first|last|pluck|count|exists\?|any\?|many\?|none\?|take|select|joins|includes|preload|eager_load|references|distinct|order|group|having|limit|offset)\s*[\(.]` +
		`|\.\s*find\s*\(\s*[A-Za-z_0-9:]` +
		`|\bconnection\s*\.\s*(?:execute|exec_query|select_all|select_one|select_value|select_values|select_rows)\s*\(`,
)

// rubyCursorSelectRe pairs raw `execute("SELECT ...")` calls as reads.
var rubyCursorSelectRe = regexp.MustCompile(
	`\.\s*execute\s*\(\s*['"](?i:\s*(?:SELECT|WITH)\b)`,
)

// rubyDBWriteRe matches ActiveRecord and raw write primitives.
var rubyDBWriteRe = regexp.MustCompile(
	`\.\s*(?:create|create!|update|update!|update_all|update_column|update_columns|destroy|destroy!|destroy_all|delete|delete_all|save|save!|insert|insert_all|insert_all!|upsert|upsert_all|increment!|decrement!|touch|toggle!)\s*[\(.!]?` +
		`|\.\s*save\s*$`,
)

// rubyCursorWriteRe matches raw INSERT/UPDATE/DELETE executes.
var rubyCursorWriteRe = regexp.MustCompile(
	`\.\s*execute\s*\(\s*['"](?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)`,
)

// rubySequelDatasetWriteRe matches Sequel dataset writes anchored on the
// `DB[:table]` dataset literal so the table name is recoverable (#3950):
//
//	DB[:users].insert(name: 'x')
//	DB[:users].where(id: 1).update(name: 'x')
//	DB[:logs].where(stale: true).delete
//	DB[:users].multi_insert(rows)  /  .import(cols, rows)  /  .update_all(...)
//
// Capture group 1 is the table symbol/string so the sink can name the table.
// The terminal write verb may be separated from the dataset by an arbitrary
// chain of read-only refinements (.where/.exclude/.filter/.join/...), matched
// permissively as `[^\n;]*?` up to the write verb. Only mutating verbs are
// listed, so a pure read chain (e.g. `DB[:users].where(id: 1).all`) does NOT
// match — preserving the read/write split.
var rubySequelDatasetWriteRe = regexp.MustCompile(
	`\bDB\s*\[\s*:?["']?(\w+)["']?\s*\]` + // DB[:users] / DB['users']
		`[^\n;]*?` + // optional read-only refinement chain
		`\.\s*(?:insert|multi_insert|import|update|update_all|delete|truncate|insert_ignore)\b`,
)

// rubyFSReadRe matches read-only filesystem primitives.
var rubyFSReadRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:read|readlines|open\s*\(\s*[^,)]+\s*\)|foreach)\b` +
		`|\bFile\s*\.\s*open\s*\(\s*[^,)]+\s*,\s*['"](?:r|rb|rt)['"]` +
		`|\bIO\s*\.\s*(?:read|readlines|foreach)\b` +
		`|\bDir\s*\.\s*(?:entries|glob|\[|children|each|foreach)\b` +
		`|\bPathname\b[^=]*\.\s*read\b`,
)

// rubyFSWriteRe matches write filesystem primitives.
var rubyFSWriteRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:write|delete|unlink|rename|chmod|chown|truncate)\b` +
		`|\bFile\s*\.\s*open\s*\(\s*[^,)]+\s*,\s*['"](?:w|wb|wt|a|ab|at|x|xb|r\+|w\+|a\+)['"]` +
		`|\bFileUtils\s*\.\s*(?:cp|cp_r|mv|rm|rm_rf|rm_f|mkdir|mkdir_p|chmod|chown|touch|ln|ln_s)\b` +
		`|\bIO\s*\.\s*write\b`,
)

// rubyProcessRe matches process-spawn primitives. We classify these as
// fs_write under the substrate's "external side-effect" interpretation
// — Process.spawn / system / exec / backticks can mutate the host.
var rubyProcessRe = regexp.MustCompile(
	`\bProcess\s*\.\s*(?:spawn|exec|fork|kill|wait|detach)\b` +
		`|\bKernel\s*\.\s*(?:system|exec|spawn)\b` +
		`|^\s*system\s*\(`,
)

// rubyMutationRe matches `@ivar = ...` instance-variable assignment.
// Excludes `==` by requiring a non-`=` continuation.
var rubyMutationRe = regexp.MustCompile(
	`@[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsRuby(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanRubyFuncHeaders(content)
	var out []EffectMatch
	out = appendRubyMatches(out, content, headers, rubyHTTPRe, EffectHTTPOut, "Net::HTTP/HTTParty/Faraday", 1.0)
	out = appendRubyMatches(out, content, headers, rubyDBReadRe, EffectDBRead, "activerecord.read", 0.85)
	out = appendRubyMatches(out, content, headers, rubyCursorSelectRe, EffectDBRead, "connection.execute(SELECT)", 1.0)
	out = appendRubyMatches(out, content, headers, rubyDBWriteRe, EffectDBWrite, "activerecord.write", 0.85)
	out = appendRubyMatches(out, content, headers, rubyCursorWriteRe, EffectDBWrite, "connection.execute(WRITE)", 1.0)
	out = appendRubySequelDatasetWrites(out, content, headers)
	out = appendRubyMatches(out, content, headers, rubyFSReadRe, EffectFSRead, "File.read/IO.read", 1.0)
	out = appendRubyMatches(out, content, headers, rubyFSWriteRe, EffectFSWrite, "File.write/FileUtils", 1.0)
	out = appendRubyMatches(out, content, headers, rubyProcessRe, EffectFSWrite, "Process.spawn/system", 0.9)
	out = appendRubyMatches(out, content, headers, rubyMutationRe, EffectMutation, "@ivar=", 0.7)
	return out
}

func scanRubyFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range rubyFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendRubyMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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

// appendRubySequelDatasetWrites stamps a db_write EffectMatch for each Sequel
// dataset write (`DB[:table]…insert/update/delete`), naming the table in the
// sink tag (#3950) — e.g. `sequel.write(users)`. Full confidence: the
// `DB[:table]` dataset literal makes the write unambiguous (unlike the generic
// `.update`/`.save` heuristic which can fire on an unknown receiver).
func appendRubySequelDatasetWrites(out []EffectMatch, content string, headers []funcHeader) []EffectMatch {
	for _, m := range rubySequelDatasetWriteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		table := content[m[2]:m[3]]
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     EffectDBWrite,
			Sink:       "sequel.write(" + table + ")",
			Confidence: 1.0,
		})
	}
	return out
}
