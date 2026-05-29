// JS/TS taint-sites sniffer (#2772 Phase 2B T1).
//
// Recognises the canonical source / sink / sanitizer primitives for
// JavaScript / TypeScript codebases. The propagation pass at
// internal/links/taint_flow.go consumes these matches to compute
// SecurityFinding records.
//
// Sources (untrusted input):
//   - req.body / req.query / req.params / req.headers / req.cookies
//     (Express / Koa / Fastify / Next.js handler arg conventions)
//   - request.body / request.query (Hono / generic)
//   - ctx.request.body (Koa)
//   - process.env.<NAME>
//   - JSON.parse(<non-literal>)
//   - NestJS @Body()/@Query()/@Param()/@Headers()/@Req()/@Request()
//     decorator-injected controller-method parameters (#3163)
//
// Sinks (security-sensitive operations):
//   - SQL injection: db.query / pool.query / connection.execute /
//     sequelize.query passed a template string or concatenation
//   - Command injection: child_process.exec / execSync / spawn
//     with shell=true, eval, new Function(...)
//   - Path traversal: fs.readFile / fs.writeFile / fs.createReadStream
//     / fs.createWriteStream with a path that is not a string literal
//   - XSS: res.send / res.write of an unescaped string, response.html
//     templating without explicit escape, dangerouslySetInnerHTML
//   - ReDoS: new RegExp(<non-literal>)
//   - Deserialisation: not a JS concern (JSON.parse is not RCE-able)
//
// Sanitizers:
//   - Parameterised SQL: db.query(sql, [params]) — pattern: second arg
//     is an array literal
//   - HTML escape: DOMPurify.sanitize, validator.escape, lodash.escape,
//     he.encode
//   - Validation libs (schema-declaration required): z.object / z.string
//     etc, joi.object / joi.string, yup.object / yup.string
//
// Hard rule: a bare `parse()` call on a name without a paired schema
// declaration does NOT count. The schema check is enforced by
// requiring the dotted-receiver `.object|.string|.array|...` form on
// well-known validator names.
package substrate

import "regexp"

func init() { RegisterTaintSniffer("jsts", sniffTaintJSTS) }

// jstsSourceReqRe matches request-input access — the canonical taint
// source in HTTP frameworks. Anchored on `req`/`request`/`ctx.request`
// followed by `.body|.query|.params|.headers|.cookies`. Conservative —
// other names (e.g. `event.body` for AWS Lambda) require a separate
// pass.
var jstsSourceReqRe = regexp.MustCompile(
	`\b(?:req|request|ctx\s*\.\s*request)\s*\.\s*(?:body|query|params|headers|cookies|rawBody)\b`,
)

// jstsSourceLoaderParamsRe matches the meta-framework loader / data-function
// route-parameter source (#2858, gap surfaced by #2868). SvelteKit `load`,
// Remix `loader`/`action`, Next.js route handlers and Nuxt routes all receive a
// `params` object on their argument and read route segments off it
// (`params.id`, `params['slug']`). The generic `jstsSourceReqRe` only matches
// `req.*`-shaped access, so these bare `params.*` reads went undetected as taint
// sources. The leading `\b` prevents matching inside `searchParams` /
// `useParams` (the `p` there is preceded by a word char, so no boundary).
var jstsSourceLoaderParamsRe = regexp.MustCompile(
	`\bparams\s*(?:\.\s*[A-Za-z_$][\w$]*|\[\s*['"][^'"]+['"]\s*\])`,
)

// jstsSourceNestJSDecoratorRe matches NestJS controller-method parameters
// annotated with @Body, @Query, @Param, @Headers, @Req, or @Request (#3163).
// These decorators inject untrusted HTTP input directly as a typed parameter
// binding — the bound identifier is a taint source in exactly the same way
// as `req.body` in Express.  The pattern anchors on the decorator token
// (possibly with an optional string-literal key inside the parens) followed
// by an optional TypeScript type annotation and then the parameter name.
// Examples matched:
//   @Body() dto                       → "dto" is tainted
//   @Query('q') q: string             → "q" is tainted
//   @Param('id') id: string           → "id" is tainted
//   @Headers() headers: Record<…>     → "headers" is tainted
//   @Req() req: Request               → "req" is tainted
//   @Request() request: any           → "request" is tainted
var jstsSourceNestJSDecoratorRe = regexp.MustCompile(
	`@(?:Body|Query|Param|Headers|Req|Request)\s*\([^)]*\)\s*[A-Za-z_$][\w$]*`,
)

// jstsSourceEnvRe matches process.env access. The fallback-literal
// case is handled by the constant-binding pass; here we only mark the
// access site as a taint source.
var jstsSourceEnvRe = regexp.MustCompile(
	`\bprocess\s*\.\s*env\s*\.\s*[A-Z_][A-Z0-9_]*\b`,
)

// jstsSourceJSONParseRe matches JSON.parse of a non-literal — we
// conservatively flag every JSON.parse(<ident>) call. The propagation
// pass only emits a finding when the input is itself proven tainted.
var jstsSourceJSONParseRe = regexp.MustCompile(
	`\bJSON\s*\.\s*parse\s*\(\s*[A-Za-z_$][\w$]*\s*\)`,
)

// jstsSinkSQLRe matches raw SQL exec calls with a template-string or
// concatenation argument — never the parameterised `(sql, [params])`
// shape which is matched separately as a sanitizer. We require the
// argument to begin with a backtick (template string) or contain a `+`
// inside the parens; the parameterised form has a literal string
// followed by `,` then an array.
var jstsSinkSQLRe = regexp.MustCompile(
	`\b(?:db|pool|connection|conn|sequelize|knex|client)\s*\.\s*(?:query|execute|raw)\s*\(\s*` +
		"(?:`[^`]*\\$\\{[^}]+\\}|['\"][^'\"]*['\"]\\s*\\+|" + // template / concat
		`[A-Za-z_$][\w$]*\s*\))`, // bare identifier as first arg
)

// jstsSinkExecRe matches command-injection sinks. `child_process.exec`
// always runs through the shell; `spawn` with `{shell: true}` does
// too. eval / new Function are dynamic-code sinks. We do not require
// the input to be tainted — the propagation pass enforces that.
var jstsSinkExecRe = regexp.MustCompile(
	`\b(?:child_process|cp)\s*\.\s*(?:exec|execSync)\s*\(` +
		`|\b(?:child_process|cp)\s*\.\s*(?:spawn|spawnSync)\s*\([^)]*shell\s*:\s*true` +
		`|\beval\s*\(` +
		`|\bnew\s+Function\s*\(`,
)

// jstsSinkFSRe matches fs.* with a non-literal first arg. The literal
// case is benign (a hardcoded path); the propagation pass only flags
// when the first arg flows from a taint source.
var jstsSinkFSRe = regexp.MustCompile(
	`\b(?:fs|fsp|fs\s*\.\s*promises)\s*\.\s*(?:readFile|readFileSync|writeFile|writeFileSync|appendFile|appendFileSync|unlink|unlinkSync|createReadStream|createWriteStream|open|openSync)\s*\(\s*[A-Za-z_$][\w$]*\s*[,)]`,
)

// jstsSinkXSSRe matches HTML output sinks. dangerouslySetInnerHTML is
// the canonical React XSS sink; res.send / res.write of a non-literal
// is the Express equivalent.
var jstsSinkXSSRe = regexp.MustCompile(
	`\bdangerouslySetInnerHTML\s*[:=]\s*\{` +
		`|\b(?:res|response)\s*\.\s*(?:send|write|end)\s*\(\s*[A-Za-z_$][\w$]*\s*[,)]` +
		`|\.innerHTML\s*=`,
)

// jstsSinkReDoSRe matches `new RegExp(<non-literal>)`. Constructed
// from a variable, this is a ReDoS vector if the variable is tainted.
var jstsSinkReDoSRe = regexp.MustCompile(
	`\bnew\s+RegExp\s*\(\s*[A-Za-z_$][\w$]*\s*[,)]`,
)

// jstsSanitizerSQLRe matches the parameterised-query shape:
// db.query("...", [args]) or db.execute("SELECT ... WHERE x = ?", [v]).
// The second arg starts with `[` or `{` (named-param object).
var jstsSanitizerSQLRe = regexp.MustCompile(
	`\b(?:db|pool|connection|conn|sequelize|knex|client)\s*\.\s*(?:query|execute)\s*\(\s*['"\` + "`" + `][^'"\` + "`" + `]+['"\` + "`" + `]\s*,\s*[\[\{]`,
)

// jstsSanitizerHTMLRe matches HTML-escape libraries. Conservative —
// must match the library-qualified call form.
var jstsSanitizerHTMLRe = regexp.MustCompile(
	`\bDOMPurify\s*\.\s*sanitize\s*\(` +
		`|\bvalidator\s*\.\s*escape\s*\(` +
		`|\b(?:_|lodash)\s*\.\s*escape\s*\(` +
		`|\bhe\s*\.\s*(?:encode|escape)\s*\(`,
)

// jstsSanitizerSchemaRe matches validation-library schema declarations.
// HARD RULE per #2772: bare parse() does not count — must be a schema
// declaration (`z.object`, `joi.string`, `yup.array`, etc.).
var jstsSanitizerSchemaRe = regexp.MustCompile(
	`\b(?:z|zod|joi|Joi|yup)\s*\.\s*(?:object|string|number|array|boolean|date|enum|union|literal|tuple|record|map|set|nullable|optional|any)\s*\(`,
)

// sniffTaintJSTS is the entry point for JS/TS taint detection.
func sniffTaintJSTS(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanJSTSFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, jstsSourceReqRe, TaintKindSource, TaintCategoryGeneric, "req.body/query/headers", 1.0)
	out = appendTaintMatches(out, content, headers, jstsSourceLoaderParamsRe, TaintKindSource, TaintCategoryGeneric, "loader params.*", 0.8)
	out = appendTaintMatches(out, content, headers, jstsSourceNestJSDecoratorRe, TaintKindSource, TaintCategoryGeneric, "@Body/@Query/@Param/@Headers/@Req", 1.0)
	out = appendTaintMatches(out, content, headers, jstsSourceEnvRe, TaintKindSource, TaintCategoryGeneric, "process.env", 0.85)
	out = appendTaintMatches(out, content, headers, jstsSourceJSONParseRe, TaintKindSource, TaintCategoryDeserialization, "JSON.parse(ident)", 0.7)
	// Sanitizers MUST be appended before sinks of the same category so
	// the propagation pass sees the sanitizer first when both occur on
	// the same line (rare but possible).
	out = appendTaintMatches(out, content, headers, jstsSanitizerSQLRe, TaintKindSanitizer, TaintCategorySQL, "parameterised-query", 1.0)
	out = appendTaintMatches(out, content, headers, jstsSanitizerHTMLRe, TaintKindSanitizer, TaintCategoryXSS, "DOMPurify/validator/lodash.escape", 1.0)
	out = appendTaintMatches(out, content, headers, jstsSanitizerSchemaRe, TaintKindSanitizer, TaintCategoryGeneric, "zod/joi/yup schema", 0.9)
	out = appendTaintMatches(out, content, headers, jstsSinkSQLRe, TaintKindSink, TaintCategorySQL, "db.query(non-literal)", 0.9)
	out = appendTaintMatches(out, content, headers, jstsSinkExecRe, TaintKindSink, TaintCategoryCommand, "exec/eval/new Function", 1.0)
	out = appendTaintMatches(out, content, headers, jstsSinkFSRe, TaintKindSink, TaintCategoryPath, "fs.*(non-literal)", 0.85)
	out = appendTaintMatches(out, content, headers, jstsSinkXSSRe, TaintKindSink, TaintCategoryXSS, "innerHTML/res.send/dangerouslySetInnerHTML", 0.85)
	out = appendTaintMatches(out, content, headers, jstsSinkReDoSRe, TaintKindSink, TaintCategoryReDoS, "new RegExp(non-literal)", 0.9)
	return out
}

// appendTaintMatches is the shared kernel for every per-language taint
// sniffer. Mirrors appendJSTSMatches in effect_sinks_jsts.go to keep
// the two passes structurally identical for reviewers.
func appendTaintMatches(out []TaintMatch, content string, headers []funcHeader, re *regexp.Regexp, kind TaintKind, cat TaintCategory, prim string, conf float64) []TaintMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, TaintMatch{
			Function:   fn,
			Line:       line,
			Kind:       kind,
			Category:   cat,
			Primitive:  prim,
			Confidence: conf,
		})
	}
	return out
}
