// Crystal taint-sites sniffer (#2778 Phase 2B T3).
//
// Recognises Crystal source / sink / sanitizer primitives for
// Kemal / Lucky / Amber and the crystal-db library.
//
// Sources:
//   - Kemal: env.params.url / env.params.query / env.request.body /
//     env.params.body / env.request.query_params
//   - Lucky / Amber: params[:name] / request.body
//   - Generic: context.params / context.request.body
//
// Sinks:
//   - SQL injection: DB.exec / db.exec with string concatenation
//     (the non-parameterised form — identified by a non-literal first arg
//     or string interpolation `"... #{var} ..."`)
//   - Command injection: Process.run / Process.new with a non-literal
//     shell command or executable
//   - Path traversal: File.write / File.open("w") / File.delete with a
//     non-literal path argument
//
// Sanitizers:
//   - Parameterised crystal-db: DB.exec(sql, args) or db.exec(sql, arg1, arg2)
//     where the SQL contains a `?` placeholder — detected by `db.exec("...?"`
//   - Type-safe params: Crystal's `params.read(Type)` / Lucky's typed actions —
//     approximated by presence of `params.read(` or `params.get(`
package substrate

import "regexp"

func init() { RegisterTaintSniffer("crystal", sniffTaintCrystal) }

// crystalSourceParamsRe matches Kemal / Lucky / generic params access.
var crystalSourceParamsRe = regexp.MustCompile(
	`\benv\s*\.\s*params\s*\.\s*(?:url|query|body)\b` +
		`|\benv\s*\.\s*request\s*\.\s*(?:body|query_params)\b` +
		`|\bparams\s*\[\s*:?[A-Za-z_][\w]*\s*\]` +
		`|\brequest\s*\.\s*(?:body|query_params)\b` +
		`|\bcontext\s*\.\s*params\b`,
)

// crystalSinkSQLRe matches DB.exec / db.exec with a non-literal SQL string
// (interpolation `"...#{var}..."` or a plain identifier as first arg).
var crystalSinkSQLRe = regexp.MustCompile(
	`\b(?:DB|db|database|conn|connection)\s*\.\s*exec\s*\(\s*"[^"]*#\{` +
		`|\b(?:DB|db|database|conn|connection)\s*\.\s*(?:exec|query|scalar|query_one)\s*\(\s*[A-Za-z_][\w]*\s*[,)]`,
)

// crystalSinkExecRe matches Process.run / Process.new with a non-literal cmd.
var crystalSinkExecRe = regexp.MustCompile(
	`\bProcess\s*\.\s*(?:run|new)\s*\(\s*[A-Za-z_$][\w]*` +
		`|\bProcess\s*\.\s*run\s*\(\s*"[^"]*#\{`,
)

// crystalSinkFSRe matches File.write / File.open with write mode / File.delete
// where the path argument is a non-literal.
var crystalSinkFSRe = regexp.MustCompile(
	`\bFile\s*\.\s*write\s*\(\s*[A-Za-z_$][\w]*` +
		`|\bFile\s*\.\s*open\s*\(\s*[A-Za-z_$][\w]*\s*,\s*["'](?:w|a|w\+|a\+)["']` +
		`|\bFile\s*\.\s*delete\s*\(\s*[A-Za-z_$][\w]*`,
)

// crystalSanitizerSQLRe matches the parameterised crystal-db form.
// db.exec("SELECT ... WHERE x = ?", value) — the `?` in the literal is the
// parameterisation signal.
var crystalSanitizerSQLRe = regexp.MustCompile(
	`\b(?:DB|db|database|conn|connection)\s*\.\s*(?:exec|query|scalar|query_one)\s*\(\s*["'][^"']*\?[^"']*["']\s*,`,
)

// crystalSanitizerParamsReadRe matches Crystal type-safe params reading.
// HARD RULE per #2778: `params.read(SomeType)` or `params.get(:name, Type)`
// is a typed extraction that counts as schema declaration.
var crystalSanitizerParamsReadRe = regexp.MustCompile(
	`\bparams\s*\.\s*(?:read|get)\s*\(\s*:?[A-Za-z_]` +
		`|\bparams\s*\.\s*get_all\s*\(`,
)

func sniffTaintCrystal(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanCrystalFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, crystalSourceParamsRe, TaintKindSource, TaintCategoryGeneric, "env.params/request.body/context.params", 1.0)
	// Sanitizers first.
	out = appendTaintMatches(out, content, headers, crystalSanitizerSQLRe, TaintKindSanitizer, TaintCategorySQL, "db.exec(\"...?\",args)", 1.0)
	out = appendTaintMatches(out, content, headers, crystalSanitizerParamsReadRe, TaintKindSanitizer, TaintCategoryGeneric, "params.read(Type)/params.get(:name,Type)", 0.9)
	// Sinks.
	out = appendTaintMatches(out, content, headers, crystalSinkSQLRe, TaintKindSink, TaintCategorySQL, "db.exec(\"...#{var}\")/db.exec(ident)", 0.9)
	out = appendTaintMatches(out, content, headers, crystalSinkExecRe, TaintKindSink, TaintCategoryCommand, "Process.run/new(non-literal)", 1.0)
	out = appendTaintMatches(out, content, headers, crystalSinkFSRe, TaintKindSink, TaintCategoryPath, "File.write/open(w,non-literal)/delete", 0.85)
	return out
}
