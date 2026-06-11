// Scala effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises Scala sink primitives. Scala interoperates with the JVM and
// commonly draws sinks from both Scala-native libraries (Slick, Doobie,
// sttp, akka-http, http4s) and Java libraries (JPA, Files, java.io):
//
//   - http_out  : sttp `basicRequest.get(uri"...").send(backend)`, akka-http
//     `Http().singleRequest`, http4s `Client.expect / .run /
//     .stream`, dispatch, requests-scala (`requests.get/post`)
//   - db_read   : Slick `query.result / .filter / .map`, Doobie `sql"
//     SELECT...".query`, raw `Statement.executeQuery`, JPA
//     `em.find / em.createQuery`, Quill `quote(query[T])`
//   - db_write  : Slick `query += / .insertOrUpdate / .delete`, Doobie
//     `sql"INSERT/UPDATE/DELETE...".update`, JPA `em.persist
//     / merge / remove`, raw `executeUpdate`
//   - fs_read   : `scala.io.Source.fromFile`, `Files.readAllBytes /
//     readAllLines`, `new FileInputStream / new FileReader`,
//     `os.read / os.list / os-lib`
//   - fs_write  : `Files.write / writeString / createFile / delete /
//     move / copy`, `new FileOutputStream / new FileWriter`,
//     `os.write / os.remove / os.makeDir / os.move / os.copy`,
//     `PrintWriter`
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

// scalaDBReadRe matches the DISTINCTIVE Slick / Doobie / Quill / JPA read
// primitives — terminals whose names do NOT collide with the Scala collection
// combinators, so they are safe to bare-match on ANY receiver:
//
//   - `.result` / `.resultSet`  — Slick query materialisation (no Seq method
//     named `result`).
//   - `.query[T].to`            — Doobie query builder.
//   - `sql"SELECT…"` / `sql"WITH…"` — Doobie/anorm interpolated read.
//   - `em.find` / `em.createQuery…` — JPA reads.
//   - `quote(query[…])`         — Quill read.
//   - `.executeQuery(`          — raw JDBC.
//
// The AMBIGUOUS Slick combinators (`filter`/`map`/`sortBy`/`take`/`drop`/
// `groupBy`/`join`/`joinLeft`/`joinRight`) are NOT here — they are ALSO the
// standard Scala collection combinators (`List(1,2,3).filter(_>1).map(_*2)`),
// so bare-matching them over-credits db_read on plain in-memory collections
// (#4736 false-positive). They are credited db_read ONLY on a Slick
// TableQuery/Query-typed receiver by scalaSlickReadMatches (#4736 receiver-typed
// read credit, mirroring the Python #4691 / Ruby+Go+C#+PHP #4692 model).
var scalaDBReadRe = regexp.MustCompile(
	`\.\s*(?:result|resultSet)\b` +
		`|\.\s*query\s*\[[^\]]+\]\s*\.\s*to\b` +
		`|\bsql"(?i:\s*(?:SELECT|WITH)\b)` +
		`|\b(?:entityManager|em)\s*\.\s*(?:find|getReference|createQuery|createNamedQuery|createNativeQuery)\s*\(` +
		`|\bquote\s*\(\s*query\s*\[` +
		`|\.\s*executeQuery\s*\(`,
)

// --- #4736 Slick receiver-typed read credit (ambiguous combinators) ---
//
// scalaSlickAmbiguousVerbs collide with the Scala collection combinators, so
// they are credited db_read ONLY when invoked on a receiver typed as a Slick
// TableQuery / Query (assigned from `TableQuery[T]`, a `.result`-bearing chain,
// or a `db.run(...)` argument). On a plain List/Seq/Map they stay pure,
// preserving the false-positive guard #4736 calls out.
const scalaSlickAmbiguousVerbs = `filter|map|sortBy|take|drop|groupBy|join|joinLeft|joinRight`

// scalaTableQuerySeedRe seeds query-typed locals from the unambiguous Slick
// roots. Group 1 = assigned name. Three trusted shapes:
//
//	val q = TableQuery[Users]              — Slick table query literal
//	val q = TableQuery[Users].filter(...)  — query literal with a refinement tail
//	val active = users.filter(_.active)    — (handled by the chain regex below)
//
// Only `TableQuery[...]` is an UNAMBIGUOUS Slick root (a plain `users` could be
// a collection), so the seed anchors on it; relation-typed locals derived from a
// seed are propagated by scalaTableQueryChainRe to a fixpoint.
var scalaTableQuerySeedRe = regexp.MustCompile(
	`(?m)\b(?:val|var)\s+([A-Za-z_]\w*)\s*(?::[^=\n]+)?=\s*TableQuery\s*\[`,
)

// scalaTableQueryChainRe propagates query typing across assignment from an
// already-typed name — `val q2 = q.filter(...)`, `val sorted = q.sortBy(...)`.
// Group 1 = assigned name, group 2 = source receiver name (checked against the
// typed set in a fixpoint loop). The refinement verb set is the read combinators
// that RETURN a Query (so the result stays query-typed).
var scalaTableQueryChainRe = regexp.MustCompile(
	`(?m)\b(?:val|var)\s+([A-Za-z_]\w*)\s*(?::[^=\n]+)?=\s*([A-Za-z_]\w*)\s*\.\s*` +
		`(?:` + scalaSlickAmbiguousVerbs + `)\s*[\(\.]`,
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
	out = append(out, scalaSlickReadMatches(content, headers)...)
	out = appendScalaMatches(out, content, headers, scalaDBWriteRe, EffectDBWrite, "slick/doobie/quill.write", 0.85)
	out = appendScalaMatches(out, content, headers, scalaFSReadRe, EffectFSRead, "Source.fromFile/Files.read", 1.0)
	out = appendScalaMatches(out, content, headers, scalaFSWriteRe, EffectFSWrite, "Files.write/os.write", 1.0)
	out = appendScalaMatches(out, content, headers, scalaProcessRe, EffectFSWrite, "Process/sys.process", 0.9)
	out = appendScalaMatches(out, content, headers, scalaMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

// scalaSlickReadMatches implements the #4736 receiver-typed read credit for
// Slick. It emits db_read for an ambiguous combinator
// (`filter`/`map`/`sortBy`/`take`/`drop`/`groupBy`/`join`/…) ONLY when the
// receiver is a Slick query-typed local — `TableQuery[Users]` directly, or a
// local seeded from a `TableQuery[...]` and propagated across reassignment to a
// fixpoint. An ambiguous combinator on a plain List/Seq/Map (untyped) earns no
// credit — the collection false-positive guard is preserved.
//
// `TableQuery[Users].filter(...)` directly (a literal receiver) is also credited
// via the literal-root regex, since the inline `TableQuery[...]` head is itself
// the unambiguous Slick marker.
func scalaSlickReadMatches(content string, headers []funcHeader) []EffectMatch {
	var out []EffectMatch
	emit := func(off int) {
		line := lineOfOffset(content, off)
		out = append(out, EffectMatch{
			Function:   nearestHeader(headers, line),
			Line:       line,
			Effect:     EffectDBRead,
			Sink:       "slick.read.query",
			Confidence: 0.8,
		})
	}
	// (a) Inline literal root: `TableQuery[Users].filter(...)` — the
	// `TableQuery[...]` head immediately followed by an ambiguous combinator is an
	// unambiguous Slick read.
	for _, m := range scalaTableQueryLiteralReadRe.FindAllStringIndex(content, -1) {
		emit(m[0])
	}
	// (b) Typed-local receivers: `q.filter(...)` where `q` was seeded/propagated
	// from a TableQuery root.
	for name := range collectScalaQueryNames(content) {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\.\s*(?:` + scalaSlickAmbiguousVerbs + `)\s*[\(\.]`)
		for _, m := range re.FindAllStringIndex(content, -1) {
			emit(m[0])
		}
	}
	return out
}

// scalaTableQueryLiteralReadRe matches an ambiguous combinator invoked directly
// on a `TableQuery[T]` literal — `TableQuery[Users].filter(_.active)`.
var scalaTableQueryLiteralReadRe = regexp.MustCompile(
	`\bTableQuery\s*\[[^\]]+\]\s*\.\s*(?:` + scalaSlickAmbiguousVerbs + `)\s*[\(\.]`,
)

// collectScalaQueryNames returns the set of local names known to hold a Slick
// query. Seeds from `val q = TableQuery[T]…` and iterates `val q2 = q.<combinator>`
// to a fixpoint, so a chain of refinements stays query-typed.
func collectScalaQueryNames(content string) map[string]bool {
	typed := map[string]bool{}
	for _, m := range scalaTableQuerySeedRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 && m[1] != "" {
			typed[m[1]] = true
		}
	}
	chains := scalaTableQueryChainRe.FindAllStringSubmatch(content, -1)
	for {
		changed := false
		for _, m := range chains {
			if len(m) < 3 {
				continue
			}
			if typed[m[2]] && !typed[m[1]] {
				typed[m[1]] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return typed
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
