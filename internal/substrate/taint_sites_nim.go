// Nim taint-sites sniffer (#2778 Phase 2B T3).
//
// Recognises Nim source / sink / sanitizer primitives for Jester /
// Prologue and stdlib db_sqlite / db_postgres / osproc.
//
// Sources:
//   - Jester: request.params[...] / request.body / @params[...]
//   - Prologue: request.getQueryParams / request.formData / request.body
//   - Generic: request.query / request.params (widely used naming convention)
//
// Sinks:
//   - SQL injection: db.exec with %-style interpolation (Nim's `fmt` or
//     `&"..."` or `%` string formatting operator on a non-literal);
//     db.exec with a non-literal first-arg SQL string
//   - Command injection: osproc.execProcess / osproc.execCmd /
//     osproc.startProcess with a non-literal command
//   - Path traversal: writeFile / open(..., fmWrite) with a non-literal
//     path where the path is a variable
//
// Sanitizers:
//   - Parameterised SQL: db_sqlite / db_postgres / db_mysql `exec` / `query`
//     with `?` placeholder (the Nim db_* modules use `?` as the placeholder)
//   - nim-validation / norm / kapsis validators (approximated by presence
//     of `validate` procedure call with a schema type)
package substrate

import "regexp"

func init() { RegisterTaintSniffer("nim", sniffTaintNim) }

// nimSourceRequestRe matches Jester / Prologue / generic request input.
var nimSourceRequestRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:params|body|query|formData|getQueryParams)\b` +
		`|\b@params\s*\[` +
		`|\brequest\s*\.\s*params\s*\[`,
)

// nimSinkSQLExecRe matches db.exec / db.execAffectedRows with a non-literal
// first arg or with %-style string formatting (e.g. `&"SELECT ... {var}"`).
var nimSinkSQLExecRe = regexp.MustCompile(
	`\b(?:db|conn|connection)\s*\.\s*(?:exec|execAffectedRows|tryExec)\s*\(\s*(?:sql\s*&["']|sql\s*[A-Za-z_][\w]*|[A-Za-z_][\w]*\s*[,)])` +
		`|\b(?:db|conn|connection)\s*\.\s*(?:exec|execAffectedRows)\s*\(\s*sql\s*"[^"]*&\{`,
)

// nimSinkExecProcessRe matches osproc.execProcess / execCmd / startProcess
// with a non-literal command argument.
var nimSinkExecProcessRe = regexp.MustCompile(
	`\b(?:osproc\s*\.\s*)?(?:execProcess|execCmd|startProcess)\s*\(\s*[A-Za-z_$][\w]*`,
)

// nimSinkFSRe matches writeFile / open(fmWrite) with a non-literal path.
var nimSinkFSRe = regexp.MustCompile(
	`\bwriteFile\s*\(\s*[A-Za-z_$][\w]*` +
		`|\bopen\s*\(\s*[A-Za-z_$][\w]*\s*,\s*(?:fmWrite|fmAppend|FileMode\.write|FileMode\.append)\b`,
)

// nimSanitizerSQLRe matches the parameterised db_* form using `?` placeholder.
// db.exec(sql"SELECT ? FROM t", value) — the sql"..." form is the Nim
// db_* canonical parameterised shape.
var nimSanitizerSQLRe = regexp.MustCompile(
	`\b(?:db|conn|connection)\s*\.\s*(?:exec|query|getValue|getRow|getAllRows)\s*\(\s*sql\s*"[^"]*\?` +
		`|\b(?:db|conn|connection)\s*\.\s*(?:exec|query)\s*\(\s*sql\s*["'][^"']*\?`,
)

// nimSanitizerValidatorRe matches nim-validation / norm usage.
// HARD RULE per #2778: a `validate(...)` call must be on an object / struct
// that carries a declared type constraint — approximated as `validate(` used
// in the file with an identifier (not just `validate("literal")`).
var nimSanitizerValidatorRe = regexp.MustCompile(
	`\bvalidate\s*\(\s*[A-Za-z_][\w]*\s*[,)]` +
		`|\bisEmail\s*\(|\bisUrl\s*\(|\bisAlphanumeric\s*\(`,
)

func sniffTaintNim(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanNimFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, nimSourceRequestRe, TaintKindSource, TaintCategoryGeneric, "request.params/body/@params", 1.0)
	// Sanitizers first.
	out = appendTaintMatches(out, content, headers, nimSanitizerSQLRe, TaintKindSanitizer, TaintCategorySQL, "db.exec(sql\"...?\",args)", 1.0)
	out = appendTaintMatches(out, content, headers, nimSanitizerValidatorRe, TaintKindSanitizer, TaintCategoryGeneric, "validate(ident)/isEmail/isUrl", 0.85)
	// Sinks.
	out = appendTaintMatches(out, content, headers, nimSinkSQLExecRe, TaintKindSink, TaintCategorySQL, "db.exec(sql&{...}/non-literal)", 0.9)
	out = appendTaintMatches(out, content, headers, nimSinkExecProcessRe, TaintKindSink, TaintCategoryCommand, "execProcess/execCmd(non-literal)", 1.0)
	out = appendTaintMatches(out, content, headers, nimSinkFSRe, TaintKindSink, TaintCategoryPath, "writeFile/open(fmWrite,non-literal)", 0.85)
	return out
}
