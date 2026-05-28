// PHP effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises PHP sink primitives:
//
//   - http_out  : curl_exec / curl_setopt(CURLOPT_URL) chains, Guzzle
//                 (`$client->request|get|post|...`, `new GuzzleHttp\Client`),
//                 `file_get_contents("http://...")`, Symfony HttpClient,
//                 WordPress `wp_remote_get|post`
//   - db_read   : PDO `->query / ->prepare / ->execute` paired with
//                 SELECT-shaped SQL, mysqli `->query / ->prepare` (SELECT),
//                 Eloquent `::all / ::find / ::where / ::first / ::get /
//                 ::count / ::exists`, Doctrine `->find / ->findBy /
//                 ->findOneBy / ->createQuery`, raw `mysql_query("SELECT")`
//   - db_write  : Eloquent `::create / ::update / ::delete / ::insert /
//                 ::save / ::firstOrCreate / ::updateOrCreate`, Doctrine
//                 `->persist / ->remove / ->flush`, PDO/mysqli execute
//                 with INSERT|UPDATE|DELETE SQL, `mysql_query("INSERT")`
//   - fs_read   : file_get_contents (non-URL), fopen("r"), fread, file,
//                 readfile, scandir, glob, is_file, is_dir, is_readable
//   - fs_write  : file_put_contents, fopen("w"/"a"/"x"), fwrite, fputs,
//                 unlink, rename, mkdir, rmdir, chmod, touch, copy
//   - mutation  : `$this->prop = ...` assignment inside a method
//
// Function attribution uses the nearest preceding `function name(` header.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("php", sniffEffectsPHP) }

// phpFuncHeaderRe matches `function name(` (with optional visibility /
// static / abstract / final / & return-by-ref). Capture group 1 is the
// method name.
var phpFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|private|protected|static|final|abstract)\s+)*` +
		`function\s*&?\s*([A-Za-z_][\w]*)\s*\(`,
)

// phpHTTPRe matches outbound HTTP primitives.
var phpHTTPRe = regexp.MustCompile(
	`\bcurl_exec\s*\(` +
		`|\bcurl_setopt\s*\([^,]+,\s*CURLOPT_URL\b` +
		`|->\s*(?:request|get|post|put|patch|delete|head|options|send|sendAsync)\s*\(\s*['"](?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|https?://)` +
		`|\bnew\s+(?:GuzzleHttp\\)?Client\b` +
		`|\bfile_get_contents\s*\(\s*['"]https?://` +
		`|\b(?:wp_remote_get|wp_remote_post|wp_remote_request|wp_safe_remote_get)\s*\(` +
		`|->\s*(?:get|post|put|patch|delete|head)\s*\(\s*['"]https?://` +
		`|\bHttpClient\s*::\s*create\b`,
)

// phpDBReadRe matches Eloquent / Doctrine / PDO / mysqli read primitives.
var phpDBReadRe = regexp.MustCompile(
	`::\s*(?:all|find|findOrFail|findMany|where|whereIn|whereHas|first|firstOrFail|get|count|exists|pluck|chunk|cursor|with|select|orderBy|groupBy)\s*\(` +
		`|->\s*(?:find|findBy|findOneBy|findAll|findOneOrNullBy|createQuery|createQueryBuilder|getRepository|getReference)\s*\(` +
		`|->\s*(?:fetchAll|fetchAssoc|fetchOne|fetchColumn|fetch|fetchObject|rowCount)\s*\(` +
		`|->\s*query\s*\(\s*['"](?i:\s*(?:SELECT|WITH)\b)` +
		`|\bmysql_query\s*\(\s*['"](?i:\s*(?:SELECT|WITH)\b)`,
)

// phpDBWriteRe matches Eloquent / Doctrine / PDO / mysqli write
// primitives.
var phpDBWriteRe = regexp.MustCompile(
	`::\s*(?:create|update|delete|insert|insertOrIgnore|upsert|firstOrCreate|updateOrCreate|destroy|truncate|save|push)\s*\(` +
		`|->\s*(?:save|update|delete|insert|push|increment|decrement|forceDelete|restore|persist|remove|flush|merge)\s*\(` +
		`|->\s*(?:exec|execute)\s*\(\s*['"](?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)` +
		`|\bmysql_query\s*\(\s*['"](?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)`,
)

// phpFSReadRe matches read-only filesystem primitives. We match
// `file_get_contents` / `file` calls unconditionally and filter out the
// http(s)-URL form post-match in sniffEffectsPHP — RE2 has no negative
// lookahead, so the disambiguation moves out of the regex.
var phpFSReadRe = regexp.MustCompile(
	`\b(?:file_get_contents|file)\s*\(` +
		`|\bfopen\s*\(\s*[^,)]+\s*,\s*['"](?:r|rb|rt|r\+)['"]` +
		`|\b(?:fread|fgets|fgetss|fgetc|fgetcsv|readfile|scandir|glob|opendir|is_file|is_dir|is_readable|stat|lstat|filesize|filemtime|file_exists|realpath)\s*\(`,
)

// phpFSReadURLRe identifies a `file_get_contents("http://...")` or
// `file("https://...")` call so the FS-read sniffer can drop it (the HTTP
// sniffer already classified it as http_out).
var phpFSReadURLRe = regexp.MustCompile(
	`^\s*(?:file_get_contents|file)\s*\(\s*['"]https?://`,
)

// phpFSWriteRe matches write filesystem primitives.
var phpFSWriteRe = regexp.MustCompile(
	`\b(?:file_put_contents|fwrite|fputs|fputcsv|unlink|rename|mkdir|rmdir|chmod|chown|chgrp|touch|copy|symlink|link|tempnam)\s*\(` +
		`|\bfopen\s*\(\s*[^,)]+\s*,\s*['"](?:w|wb|wt|a|ab|at|x|xb|xt|w\+|a\+|x\+)['"]`,
)

// phpProcessRe matches process-spawn primitives (modelled as fs_write).
var phpProcessRe = regexp.MustCompile(
	`\b(?:exec|system|shell_exec|passthru|proc_open|popen|pcntl_exec)\s*\(`,
)

// phpMutationRe matches `$this->prop = ...` assignment.
var phpMutationRe = regexp.MustCompile(
	`\$this\s*->\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsPHP(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanPHPFuncHeaders(content)
	var out []EffectMatch
	out = appendPHPMatches(out, content, headers, phpHTTPRe, EffectHTTPOut, "curl/Guzzle/wp_remote", 1.0)
	out = appendPHPMatches(out, content, headers, phpDBReadRe, EffectDBRead, "eloquent/doctrine/pdo.read", 0.85)
	out = appendPHPMatches(out, content, headers, phpDBWriteRe, EffectDBWrite, "eloquent/doctrine/pdo.write", 0.85)
	out = appendPHPFSReadMatches(out, content, headers)
	out = appendPHPMatches(out, content, headers, phpFSWriteRe, EffectFSWrite, "file_put_contents/fwrite", 1.0)
	out = appendPHPMatches(out, content, headers, phpProcessRe, EffectFSWrite, "exec/system/proc_open", 0.9)
	out = appendPHPMatches(out, content, headers, phpMutationRe, EffectMutation, "$this->prop=", 0.7)
	return out
}

func scanPHPFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range phpFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

// appendPHPFSReadMatches walks phpFSReadRe matches and drops any
// `file_get_contents("http://...")` / `file("https://...")` instance —
// already classified as http_out. RE2's lack of negative lookahead forces
// this filter into Go rather than the regex.
func appendPHPFSReadMatches(out []EffectMatch, content string, headers []funcHeader) []EffectMatch {
	for _, m := range phpFSReadRe.FindAllStringIndex(content, -1) {
		snippet := content[m[0]:m[1]]
		if phpFSReadURLRe.MatchString(snippet) {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     EffectFSRead,
			Sink:       "file_get_contents/fopen(r)",
			Confidence: 1.0,
		})
	}
	return out
}

func appendPHPMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
