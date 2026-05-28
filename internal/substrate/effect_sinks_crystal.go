// Crystal effect-sink sniffer (#2776 Phase 1A T3).
//
// Recognises Crystal sink primitives:
//
//   - http_out  : HTTP::Client.get/post/put/patch/delete/exec,
//                 HTTP::Client.new(...).get/post/exec, Crest.get/post
//   - db_read   : DB.open / db.query / db.query_one / db.scalar /
//                 db.query_each (crystal-db SELECT-flavoured)
//   - db_write  : db.exec("INSERT|UPDATE|DELETE|CREATE ..."),
//                 db.exec (generic — lower confidence)
//   - fs_read   : File.read / File.read_lines / File.open (read mode) /
//                 File.each_line / Dir.entries / Dir.glob
//   - fs_write  : File.write / File.open(..., "w") / File.delete /
//                 Dir.mkdir / Dir.mkdir_p / Dir.delete / FileUtils.cp / mv / rm
//   - mutation  : instance variable assignment `@field = ...`
//
// Function attribution uses `def name` / `private def name` header lines.
// Crystal's explicit `def` keyword makes this precise.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("crystal", sniffEffectsCrystal) }

// crystalFuncHeaderRe matches Crystal def declarations.
// Capture group 1 is the method name.
var crystalFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:private|protected|abstract|class|module)\s+)?def\s+([A-Za-z_][\w?!]*)\s*[(\s]`,
)

// crystalHTTPRe matches HTTP::Client primitives.
var crystalHTTPRe = regexp.MustCompile(
	`\bHTTP\s*::\s*Client\s*\.\s*(?:get|post|put|patch|delete|head|exec)\s*\(` +
		`|\bHTTP\s*::\s*Client\s*\.new\s*\([^)]*\)\s*\.\s*(?:get|post|put|patch|delete|exec)\s*\(` +
		`|\bCrest\s*\.\s*(?:get|post|put|patch|delete)\s*\(` +
		`|\bclient\s*\.\s*(?:get|post|put|patch|delete|exec)\s*\(\s*["'](?:https?://|/)`,
)

// crystalDBReadRe matches crystal-db read calls.
var crystalDBReadRe = regexp.MustCompile(
	`\b(?:db|database|conn|connection)\s*\.\s*(?:query|query_one|query_one\?|scalar|query_each|exec_iter)\s*\(` +
		`|\bDB\s*\.\s*open\s*\(` +
		`|\bDB\.connect\s*\(`,
)

// crystalDBWriteRe matches crystal-db write calls (exec with write SQL).
var crystalDBWriteRe = regexp.MustCompile(
	`\b(?:db|database|conn|connection)\s*\.\s*exec\s*\(\s*["'](?i:\s*(?:INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|REPLACE|TRUNCATE)\b)` +
		`|\b(?:db|database|conn|connection)\s*\.\s*exec\s*\(`,
)

// crystalFSReadRe matches File / Dir read primitives.
var crystalFSReadRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:read|read_lines|each_line|open|size|exists\?|info|stat)\s*\(` +
		`|\bDir\s*\.\s*(?:entries|glob|open|current|exists\?)\s*\(`,
)

// crystalFSWriteRe matches File / Dir / FileUtils write primitives.
var crystalFSWriteRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:write|open\s*\([^,)]+\s*,\s*["'](?:w|a|w\+|a\+)["']|delete|rename|symlink|chmod|chown)\s*\(` +
		`|\bFile\s*\.\s*write\s*\(` +
		`|\bDir\s*\.\s*(?:mkdir|mkdir_p|delete|rmdir)\s*\(` +
		`|\bFileUtils\s*\.\s*(?:cp|mv|rm|rm_r|mkdir_p|cp_r)\s*\(`,
)

// crystalMutationRe matches instance variable assignment `@field = ...`.
var crystalMutationRe = regexp.MustCompile(
	`\s@[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsCrystal(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanCrystalFuncHeaders(content)
	var out []EffectMatch
	out = appendCrystalMatches(out, content, headers, crystalHTTPRe, EffectHTTPOut, "HTTP::Client/Crest", 1.0)
	out = appendCrystalMatches(out, content, headers, crystalDBReadRe, EffectDBRead, "crystal-db.read", 0.85)
	out = appendCrystalMatches(out, content, headers, crystalDBWriteRe, EffectDBWrite, "crystal-db.exec(WRITE)", 0.85)
	out = appendCrystalMatches(out, content, headers, crystalFSReadRe, EffectFSRead, "File.read/Dir.entries", 1.0)
	out = appendCrystalMatches(out, content, headers, crystalFSWriteRe, EffectFSWrite, "File.write/Dir.mkdir", 1.0)
	out = appendCrystalMatches(out, content, headers, crystalMutationRe, EffectMutation, "@field=", 0.7)
	return out
}

func scanCrystalFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range crystalFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendCrystalMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
