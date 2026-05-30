// Lua taint-sites sniffer (Phase 2B).
//
// Recognises Lua source / sink / sanitizer primitives for OpenResty
// and Lapis web applications.
//
// Sources (untrusted input):
//   - ngx.req.get_post_args() / ngx.req.get_uri_args() — query/form params
//   - ngx.req.get_body_data() — raw HTTP body
//   - ngx.req.get_headers() — HTTP headers (user-controlled)
//   - ngx.var.arg_<name> / ngx.var.http_<name> — nginx variables
//   - cjson.decode(...) / json.decode(...) — JSON deserialization
//   - lapis params table: params.<field>
//
// Sinks (security-sensitive operations):
//   - SQL injection: direct string concatenation in db:query/db.execute with %s/%q or ..
//   - Command injection: os.execute(...) / io.popen(...) with a non-literal arg
//   - Path traversal: io.open(...) with a non-literal path
//   - XSS: ngx.say(...) / ngx.print(...) with non-literal content (unsanitized output)
//   - Code execution: load(...)(...) / loadstring(...) — dynamic code execution
//
// Sanitizers:
//   - ngx.quote_sql_str(...) — MySQL/MariaDB SQL escaping
//   - ngx.escape_uri(...) — URL encoding
//   - lapis.db.escape_literal / lapis.db.escape_identifier — SQL escaping
//   - cjson.encode / json.encode (output encoding)
//   - string.format with "%q" — basic Lua string quoting
package substrate

import "regexp"

func init() { RegisterTaintSniffer("lua", sniffTaintLua) }

// ---------------------------------------------------------------------------
// Compiled regexes — Sources
// ---------------------------------------------------------------------------

// ngx.req.get_post_args() / ngx.req.get_uri_args()
var luaSrcArgsRe = regexp.MustCompile(
	`\bngx\.req\.get_(?:post|uri)_args\s*\(\s*\)`)

// ngx.req.get_body_data()
var luaSrcBodyDataRe = regexp.MustCompile(
	`\bngx\.req\.get_body_data\s*\(\s*\)`)

// ngx.req.get_headers() — user-controlled header values
var luaSrcHeadersRe = regexp.MustCompile(
	`\bngx\.req\.get_headers\s*\(\s*\)`)

// ngx.var.arg_X / ngx.var.http_X — nginx variable sources
var luaSrcNgxVarRe = regexp.MustCompile(
	`\bngx\.var\.(?:arg_|http_|query_string|request_body)\w*`)

// cjson.decode / json.decode — untrusted JSON deserialization
var luaSrcJSONDecodeRe = regexp.MustCompile(
	`\b(?:cjson|json)\s*(?:\.new\s*\(\s*\)\s*\.)?\.\s*decode\s*\(`)

// Lapis params: params.<field>
var luaSrcLapisParamsRe = regexp.MustCompile(
	`\bparams\s*\.\s*[a-z_]\w*`)

// ---------------------------------------------------------------------------
// Compiled regexes — Sinks
// ---------------------------------------------------------------------------

// SQL: db:query("...") or db.execute("...") with string concat/interpolation
// Detects raw string building: "SELECT " .. var or %s-based formatting
var luaSinkSQLRe = regexp.MustCompile(
	`\b(?:db\s*[:.]\s*query|db\s*[:.]\s*execute|lapis\.db\.query)\s*\(\s*["'][^'"]*["']\s*\.\.`)

// Command injection: os.execute / io.popen with non-literal arg
var luaSinkExecRe = regexp.MustCompile(
	`\b(?:os\.execute|io\.popen)\s*\(\s*[^"'\s)][^)]*\)`)

// Path traversal: io.open with a variable path
var luaSinkFileOpenRe = regexp.MustCompile(
	`\bio\.open\s*\(\s*[^"'\s)][^)]*,`)

// XSS: ngx.say / ngx.print with a variable (non-literal) — potential unescaped output
var luaSinkNgxSayRe = regexp.MustCompile(
	`\bngx\s*\.\s*(?:say|print|send_headers)\s*\(\s*[^"'\s)][^)]*\)`)

// Dynamic code execution: load / loadstring
var luaSinkLoadRe = regexp.MustCompile(
	`\b(?:load|loadstring)\s*\(\s*[^"'\s)][^)]*\)`)

// ---------------------------------------------------------------------------
// Compiled regexes — Sanitizers
// ---------------------------------------------------------------------------

// ngx.quote_sql_str — proper MySQL escaping
var luaSanSQLEscRe = regexp.MustCompile(
	`\bngx\.quote_sql_str\s*\(`)

// ngx.escape_uri — URL encoding
var luaSanEscapeURIRe = regexp.MustCompile(
	`\bngx\.escape_uri\s*\(`)

// lapis.db.escape_literal / lapis.db.escape_identifier
var luaSanLapisEscRe = regexp.MustCompile(
	`\blapis\.db\.(?:escape_literal|escape_identifier)\s*\(`)

// cjson.encode / json.encode — output encoding
var luaSanJSONEncodeRe = regexp.MustCompile(
	`\b(?:cjson|json)\s*(?:\.new\s*\(\s*\)\s*\.)?\.\s*encode\s*\(`)

// string.format with %q
var luaSanStringFormatRe = regexp.MustCompile(
	`\bstring\.format\s*\(\s*"[^"]*%q`)

// ---------------------------------------------------------------------------
// Sniffer implementation
// ---------------------------------------------------------------------------

func sniffTaintLua(content string) []TaintMatch {
	if content == "" {
		return nil
	}

	// Build function header list for attribution.
	headers := buildLuaTaintHeaders(content)

	var out []TaintMatch

	// --- Sources ---
	for _, m := range luaSrcArgsRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSource, Category: TaintCategoryPath,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.9,
			Primitive: "ngx.req.get_args"})
	}
	for _, m := range luaSrcBodyDataRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSource, Category: TaintCategoryPath,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.9,
			Primitive: "ngx.req.get_body_data"})
	}
	for _, m := range luaSrcHeadersRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSource, Category: TaintCategoryPath,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.85,
			Primitive: "ngx.req.get_headers"})
	}
	for _, m := range luaSrcNgxVarRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSource, Category: TaintCategoryPath,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.85,
			Primitive: "ngx.var.*"})
	}
	for _, m := range luaSrcJSONDecodeRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSource, Category: TaintCategoryDeserialization,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.75,
			Primitive: "cjson.decode"})
	}
	for _, m := range luaSrcLapisParamsRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSource, Category: TaintCategoryPath,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.80,
			Primitive: "params.*"})
	}

	// --- Sinks ---
	for _, m := range luaSinkSQLRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSink, Category: TaintCategorySQL,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.85,
			Primitive: "db:query concat"})
	}
	for _, m := range luaSinkExecRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSink, Category: TaintCategoryCommand,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.90,
			Primitive: "os.execute/io.popen"})
	}
	for _, m := range luaSinkFileOpenRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSink, Category: TaintCategoryPath,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.85,
			Primitive: "io.open"})
	}
	for _, m := range luaSinkNgxSayRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSink, Category: TaintCategoryXSS,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.75,
			Primitive: "ngx.say/ngx.print"})
	}
	for _, m := range luaSinkLoadRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSink, Category: TaintCategoryCommand,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.95,
			Primitive: "load/loadstring"})
	}

	// --- Sanitizers ---
	for _, m := range luaSanSQLEscRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSanitizer, Category: TaintCategorySQL,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 1.0,
			Primitive: "ngx.quote_sql_str"})
	}
	for _, m := range luaSanEscapeURIRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSanitizer, Category: TaintCategoryXSS,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.9,
			Primitive: "ngx.escape_uri"})
	}
	for _, m := range luaSanLapisEscRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSanitizer, Category: TaintCategorySQL,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 1.0,
			Primitive: "lapis.db.escape_literal"})
	}
	for _, m := range luaSanJSONEncodeRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSanitizer, Category: TaintCategoryXSS,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.8,
			Primitive: "cjson.encode"})
	}
	for _, m := range luaSanStringFormatRe.FindAllStringIndex(content, -1) {
		fn := nearestLuaTaintHeader(headers, lineOfOffset(content, m[0]))
		out = append(out, TaintMatch{Kind: TaintKindSanitizer, Category: TaintCategorySQL,
			Function: fn, Line: lineOfOffset(content, m[0]), Confidence: 0.7,
			Primitive: "string.format %q"})
	}

	return out
}

// ---------------------------------------------------------------------------
// Helper — function-header scanner for taint attribution
// ---------------------------------------------------------------------------

type luaTaintHeader struct {
	Line int
	Name string
}

var luaTaintFuncRe = regexp.MustCompile(
	`(?m)^\s*(?:local\s+)?function\s+(?:[A-Za-z_][\w]*[.:])*([A-Za-z_][\w]*)\s*\(`,
)

func buildLuaTaintHeaders(content string) []luaTaintHeader {
	var hs []luaTaintHeader
	for _, m := range luaTaintFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		hs = append(hs, luaTaintHeader{
			Line: lineOfOffset(content, m[0]),
			Name: content[m[2]:m[3]],
		})
	}
	return hs
}

func nearestLuaTaintHeader(headers []luaTaintHeader, line int) string {
	best := ""
	for _, h := range headers {
		if h.Line <= line {
			best = h.Name
		}
	}
	return best
}
