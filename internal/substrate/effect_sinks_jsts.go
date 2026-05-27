// JS/TS effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises the canonical sink primitives that contribute to each
// effect in JavaScript / TypeScript codebases:
//
//   - http_out  : fetch(...), axios.<method>(...), got/ky/superagent
//   - db_read   : Sequelize/TypeORM/Prisma `.find*` / `.findOne` / `.query`,
//                 Mongoose `.find()` / `.aggregate()`, knex `.select`
//   - db_write  : `.save`/`.create`/`.update`/`.delete`/`.insert`/`.upsert`
//   - fs_read   : `fs.readFile*`, `fs.readSync`, `fs.createReadStream`
//   - fs_write  : `fs.writeFile*`, `fs.writeSync`, `fs.createWriteStream`,
//                 `fs.appendFile*`, `fs.mkdir*`, `fs.unlink*`
//   - mutation  : `this.<field> = ...` assignment inside a method body
//
// Function attribution is line-based: each match is assigned to the
// nearest enclosing `function` / `=>` / class-method header on a line ≤
// the match's line. This is intentionally syntactic — accurate enough
// for the substrate, with full lexical accuracy deferred to Phase 4.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("jsts", sniffEffectsJSTS) }

// jstsFuncHeaderRe matches function-introducing syntax we attribute sinks to:
//
//   function foo(             // named function declaration
//   async function foo(
//   foo(args) {               // method shorthand in object/class
//   foo = function(           // function-expression assignment
//   foo = (args) => {         // arrow function assignment
//   const foo = (args) =>     // const arrow
//   async foo(args) {         // async class method shorthand
//
// Capture group 1 is the declaring identifier name.
var jstsFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:export\s+)?(?:default\s+)?` +
		`(?:async\s+)?` +
		`(?:` +
		`function\s*\*?\s+([A-Za-z_$][\w$]*)\s*\(` +
		`|(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:function\b|\([^)]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)` +
		`|([A-Za-z_$][\w$]*)\s*\(([^)\n]*)\)\s*(?::\s*[A-Za-z_$][\w$<>\[\],\s|&?]+)?\s*\{` +
		`)`,
)

// jstsHTTPRe matches outbound HTTP call sites. Conservative — we want
// the function name on the LHS of the `(` so we don't tag every `fetch`
// substring (e.g. inside a string literal).
var jstsHTTPRe = regexp.MustCompile(
	`\b(?:fetch|axios(?:\s*\.\s*(?:get|post|put|patch|delete|head|options|request))?|` +
		`got(?:\s*\.\s*(?:get|post|put|patch|delete|head))?|ky(?:\s*\.\s*(?:get|post|put|patch|delete|head))?|` +
		`superagent\s*\.\s*(?:get|post|put|patch|delete|head)|` +
		`XMLHttpRequest|navigator\s*\.\s*sendBeacon)\s*\(`,
)

// jstsDBReadRe matches read-flavoured ORM / query-builder primitives.
var jstsDBReadRe = regexp.MustCompile(
	`\.\s*(findOne|findAll|findMany|findUnique|findFirst|find|aggregate|count|exists|query|select|raw|exec)\s*\(`,
)

// jstsDBWriteRe matches write-flavoured ORM / query-builder primitives.
var jstsDBWriteRe = regexp.MustCompile(
	`\.\s*(save|create|createMany|update(?:Many|One)?|upsert|delete(?:Many|One)?|destroy|insert(?:Many|One)?|bulkCreate|bulkUpdate|bulkDelete)\s*\(`,
)

// jstsFSReadRe matches Node fs / fs/promises read primitives.
var jstsFSReadRe = regexp.MustCompile(
	`\b(?:fs|fsp|fs\s*\.\s*promises)\s*\.\s*(readFile|readFileSync|readdir|readdirSync|createReadStream|stat|statSync|lstat|lstatSync|access|accessSync|open|openSync)\s*\(`,
)

// jstsFSWriteRe matches Node fs / fs/promises write primitives.
var jstsFSWriteRe = regexp.MustCompile(
	`\b(?:fs|fsp|fs\s*\.\s*promises)\s*\.\s*(writeFile|writeFileSync|appendFile|appendFileSync|mkdir|mkdirSync|rmdir|rmdirSync|unlink|unlinkSync|rm|rmSync|rename|renameSync|copyFile|copyFileSync|createWriteStream|chmod|chmodSync|chown|chownSync|truncate|truncateSync|symlink|symlinkSync)\s*\(`,
)

// jstsMutationRe matches `this.<field> = ...` — assignment to a receiver
// field. We require an assignment operator on the same line and reject
// `this.<field> ==` (comparison) by anchoring on `=` followed by a
// non-`=` character.
var jstsMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_$][\w$]*\s*=(?:[^=])`,
)

// sniffEffectsJSTS is the JS/TS effect sniffer entry point.
func sniffEffectsJSTS(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanJSTSFuncHeaders(content)
	var out []EffectMatch
	out = appendJSTSMatches(out, content, headers, jstsHTTPRe, EffectHTTPOut, "fetch/axios", 1.0)
	out = appendJSTSMatches(out, content, headers, jstsDBReadRe, EffectDBRead, "orm.read", 0.8)
	out = appendJSTSMatches(out, content, headers, jstsDBWriteRe, EffectDBWrite, "orm.write", 0.8)
	out = appendJSTSMatches(out, content, headers, jstsFSReadRe, EffectFSRead, "fs.read", 1.0)
	out = appendJSTSMatches(out, content, headers, jstsFSWriteRe, EffectFSWrite, "fs.write", 1.0)
	out = appendJSTSMatches(out, content, headers, jstsMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

// funcHeader records a function-header line so sinks can be attributed
// to the nearest preceding header. Lexical scoping is approximated by
// "nearest preceding" — fine for module-level and class-level methods,
// imprecise for deeply nested closures (acceptable for Phase 1A; Phase
// 4 will tighten via the real AST).
type funcHeader struct {
	Line int
	Name string
}

func scanJSTSFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range jstsFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		case m[6] >= 0:
			name = content[m[6]:m[7]]
		}
		if name == "" || jstsControlKeyword(name) {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

// jstsControlKeyword rejects keywords that the method-shorthand regex
// can accidentally match (e.g. `if (...) {`). Keeps the header set
// honest without an AST.
func jstsControlKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "catch", "do", "return", "throw", "typeof", "instanceof", "new", "delete", "void", "in", "of", "yield", "await":
		return true
	}
	return false
}

func appendJSTSMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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

// nearestHeader returns the name of the function header whose Line is
// the greatest value ≤ line, or "" when no header precedes line (i.e.
// the match is at module scope).
func nearestHeader(headers []funcHeader, line int) string {
	name := ""
	for _, h := range headers {
		if h.Line <= line {
			name = h.Name
		} else {
			break
		}
	}
	return name
}

