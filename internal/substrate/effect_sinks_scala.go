// Scala effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises Scala sink primitives. Scala interoperates with the JVM and
// commonly draws sinks from both Scala-native libraries (Slick, Doobie,
// sttp, akka-http, http4s) and Java libraries (JPA, Files, java.io):
//
//   - http_out  : sttp `basicRequest.get(uri"...").send(backend)`, akka-http
//                 `Http().singleRequest`, http4s `Client.expect / .run /
//                 .stream`, dispatch, requests-scala (`requests.get/post`)
//   - db_read   : Slick `query.result / .filter / .map`, Doobie `sql"
//                 SELECT...".query`, raw `Statement.executeQuery`, JPA
//                 `em.find / em.createQuery`, Quill `quote(query[T])`
//   - db_write  : Slick `query += / .insertOrUpdate / .delete`, Doobie
//                 `sql"INSERT/UPDATE/DELETE...".update`, JPA `em.persist
//                 / merge / remove`, raw `executeUpdate`
//   - fs_read   : `scala.io.Source.fromFile`, `Files.readAllBytes /
//                 readAllLines`, `new FileInputStream / new FileReader`,
//                 `os.read / os.list / os-lib`
//   - fs_write  : `Files.write / writeString / createFile / delete /
//                 move / copy`, `new FileOutputStream / new FileWriter`,
//                 `os.write / os.remove / os.makeDir / os.move / os.copy`,
//                 `PrintWriter`
//   - mutation  : `this.<field> = ...` assignment in a class body
//
// Function attribution uses the nearest preceding `def name(` header.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("scala", sniffEffectsScala) }

// scalaFuncHeaderRe matches `def name` (with optional modifiers and type
// parameters). Capture group 1 is the bare function name. Scala allows
// `def name = ...` (no parens) for parameterless methods; the regex
// accepts either `(`, `[`, `:`, or `=` immediately after the name.
var scalaFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:override|final|implicit|private|protected|inline|transparent|sealed|abstract|@[A-Za-z_][\w]*(?:\([^)]*\))?)\s+)*` +
		`def\s+([A-Za-z_][\w]*)\s*[\[\(:=]`,
)

// scalaHTTPRe matches outbound HTTP primitives.
var scalaHTTPRe = regexp.MustCompile(
	`\bbasicRequest\s*\.\s*(?:get|post|put|patch|delete|head|options)\s*\(` +
		`|\.\s*send\s*\(\s*backend\s*\)` +
		`|\bHttp\s*\(\s*\)\s*\.\s*singleRequest\b` +
		`|\bHttpRequest\s*\(` +
		`|\b(?:Client|client)\s*\.\s*(?:expect|run|stream|fetch|fetchAs|stream_)\s*\[` +
		`|\bdispatch\s*\.\s*(?:url|Http)\b` +
		`|\brequests\s*\.\s*(?:get|post|put|patch|delete|head|options|send)\s*\(` +
		`|\bsttp\s*\.\s*client3\b`,
)

// scalaDBReadRe matches Slick / Doobie / Quill / JPA read primitives.
var scalaDBReadRe = regexp.MustCompile(
	`\.\s*(?:result|resultSet|filter|map|sortBy|take|drop|groupBy|join|joinLeft|joinRight)\s*[\(\.]` +
		`|\.\s*query\s*\[[^\]]+\]\s*\.\s*to\b` +
		`|\bsql"(?i:\s*(?:SELECT|WITH)\b)` +
		`|\b(?:entityManager|em)\s*\.\s*(?:find|getReference|createQuery|createNamedQuery|createNativeQuery)\s*\(` +
		`|\bquote\s*\(\s*query\s*\[` +
		`|\.\s*executeQuery\s*\(`,
)

// scalaDBWriteRe matches Slick / Doobie / Quill / JPA write primitives.
var scalaDBWriteRe = regexp.MustCompile(
	`\.\s*(?:\+=|\+\+=|insertOrUpdate|insertAll|delete|deleteWhere|update|forceInsert|forceInsertAll)\s*[\(\.]` +
		`|\bsql"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)` +
		`|\b(?:entityManager|em)\s*\.\s*(?:persist|merge|remove|refresh|flush)\s*\(` +
		`|\.\s*executeUpdate\s*\(` +
		`|\.\s*(?:insert|update|delete)\s*\.\s*returning\b`,
)

// scalaFSReadRe matches read-only filesystem primitives.
var scalaFSReadRe = regexp.MustCompile(
	`\bSource\s*\.\s*fromFile\s*\(` +
		`|\bFiles\s*\.\s*(?:readAllBytes|readAllLines|readString|lines|newInputStream|newBufferedReader|exists|isReadable|size|getAttribute|readAttributes|isDirectory|isRegularFile|list|walk|find)\s*\(` +
		`|\bnew\s+(?:FileInputStream|FileReader|BufferedReader\s*\(\s*new\s+FileReader|Scanner\s*\(\s*new\s+File)\s*\(` +
		`|\bos\s*\.\s*(?:read|list|exists|isDir|isFile|stat|walk)\b`,
)

// scalaFSWriteRe matches write filesystem primitives.
var scalaFSWriteRe = regexp.MustCompile(
	`\bFiles\s*\.\s*(?:write|writeString|newOutputStream|newBufferedWriter|createFile|createDirectory|createDirectories|delete|deleteIfExists|move|copy|setAttribute|setPosixFilePermissions)\s*\(` +
		`|\bnew\s+(?:FileOutputStream|FileWriter|PrintWriter\s*\(\s*new\s+File)\s*\(` +
		`|\bos\s*\.\s*(?:write|remove|makeDir|makeDir\.all|move|copy|truncate|symlink|hardlink)\b`,
)

// scalaProcessRe matches process-spawn primitives (modelled as fs_write).
var scalaProcessRe = regexp.MustCompile(
	`\bProcess\s*\(\s*"` +
		`|\bRuntime\s*\.\s*getRuntime\s*\(\s*\)\s*\.\s*exec\s*\(` +
		`|\bsys\s*\.\s*process\s*\.\s*Process\b`,
)

// scalaMutationRe matches `this.<field> = ...` assignment.
var scalaMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsScala(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanScalaFuncHeaders(content)
	var out []EffectMatch
	out = appendScalaMatches(out, content, headers, scalaHTTPRe, EffectHTTPOut, "sttp/akka-http/http4s/requests", 1.0)
	out = appendScalaMatches(out, content, headers, scalaDBReadRe, EffectDBRead, "slick/doobie/quill.read", 0.8)
	out = appendScalaMatches(out, content, headers, scalaDBWriteRe, EffectDBWrite, "slick/doobie/quill.write", 0.85)
	out = appendScalaMatches(out, content, headers, scalaFSReadRe, EffectFSRead, "Source.fromFile/Files.read", 1.0)
	out = appendScalaMatches(out, content, headers, scalaFSWriteRe, EffectFSWrite, "Files.write/os.write", 1.0)
	out = appendScalaMatches(out, content, headers, scalaProcessRe, EffectFSWrite, "Process/sys.process", 0.9)
	out = appendScalaMatches(out, content, headers, scalaMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

func scanScalaFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range scalaFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendScalaMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
