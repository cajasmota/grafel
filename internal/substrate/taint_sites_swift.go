// Swift taint-sites sniffer (#2778 Phase 2B T3).
//
// Recognises Swift source / sink / sanitizer primitives for Vapor,
// Hummingbird, and stdlib Foundation / SQLite.swift.
//
// Sources:
//   - Vapor: req.parameters.get / req.query[...] / req.content.decode /
//     req.body.collect / request.body.string
//   - Hummingbird: request.parameters / request.uri.queryParameters /
//     request.body
//   - URLRequest body (client-side but still tainted input in test/CLI code):
//     URLRequest.httpBody / URLRequest.url?.queryItems
//
// Sinks:
//   - SQL injection: SQLite Connection.execute / Statement.run with a
//     string built from user input (non-literal first arg that isn't a
//     string literal starting with ' or ")
//   - Command injection: Process().launchPath = ... / Process().arguments /
//     Process.launchedProcess(launchPath: var, ...)
//   - Path traversal: FileManager.default.createFile / Data.write(to: url)
//     where the URL/path is derived from a non-literal variable
//
// Sanitizers:
//   - Parameterised SQLite: SQLite.swift Statement with `?` placeholders —
//     detected by `.run([binding, ...])` or `.run(binding1, binding2)`
//   - Codable/Decodable struct decoding: `try req.content.decode(T.self)` —
//     the Codable type constraint acts as a schema declaration
package substrate

import "regexp"

func init() { RegisterTaintSniffer("swift", sniffTaintSwift) }

// swiftSourceVaporRe matches Vapor request input accessors.
var swiftSourceVaporRe = regexp.MustCompile(
	`\breq\s*\.\s*(?:parameters\s*\.\s*get|query\s*\[|content\s*\.\s*decode|body)\b` +
		`|\brequest\s*\.\s*(?:parameters|uri\s*\.\s*queryParameters|body|query)\b`,
)

// swiftSourceURLRequestRe matches URLRequest body / queryItems access.
var swiftSourceURLRequestRe = regexp.MustCompile(
	`\bURLRequest\b.*\.\s*(?:httpBody|queryItems)\b` +
		`|\b[A-Za-z_][\w]*\s*\.\s*(?:httpBody|queryItems)\s*=`,
)

// swiftSinkSQLRe matches SQLite.swift execute / run with a non-literal
// first arg (an identifier rather than a string literal).
var swiftSinkSQLRe = regexp.MustCompile(
	`\b(?:db|database|connection|conn)\s*\.\s*(?:execute|run|scalar|prepare)\s*\(\s*[A-Za-z_][\w]*\s*[,)]` +
		`|\btry\s+(?:db|database|connection|conn)\s*\.\s*(?:execute|run)\s*\(\s*[A-Za-z_][\w]*\s*[,)]`,
)

// swiftSinkExecRe matches Process launch with a non-literal launchPath or
// arguments that include user-controlled data.
var swiftSinkExecRe = regexp.MustCompile(
	`\bProcess\s*\(\s*\)` +
		`|\bProcess\s*\.\s*launchedProcess\s*\(\s*launchPath\s*:\s*[A-Za-z_][\w]*`,
)

// swiftSinkFSRe matches FileManager / Data write with a non-literal path.
var swiftSinkFSRe = regexp.MustCompile(
	`\bFileManager\s*\.\s*default\s*\.\s*createFile\s*\(\s*atPath\s*:\s*[A-Za-z_][\w]*` +
		`|\btry\s+[A-Za-z_][\w]*\s*\.\s*write\s*\(\s*to\s*:\s*[A-Za-z_][\w]*`,
)

// swiftSanitizerSQLRe matches the SQLite.swift parameterised-binding form.
// `statement.run([binding])` or `statement.run(binding1, binding2)`.
var swiftSanitizerSQLRe = regexp.MustCompile(
	`\.\s*run\s*\(\s*\[` +
		`|\bStatement\s*\b` +
		`|\btry\s+(?:db|database|connection|conn)\s*\.\s*(?:execute|run)\s*\(\s*"[^"]*\?`,
)

// swiftSanitizerCodableRe matches Codable/Decodable struct decoding from request.
// HARD RULE per #2778: `try req.content.decode(T.self)` declares a schema type —
// the presence of `.decode(` paired with `.self` in the file counts.
var swiftSanitizerCodableRe = regexp.MustCompile(
	`\.\s*decode\s*\(\s*[A-Z][A-Za-z][\w]*\s*\.\s*self\s*\)` +
		`|:\s*Codable\b|:\s*Decodable\b`,
)

func sniffTaintSwift(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanSwiftFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, swiftSourceVaporRe, TaintKindSource, TaintCategoryGeneric, "req.parameters/query/body", 1.0)
	out = appendTaintMatches(out, content, headers, swiftSourceURLRequestRe, TaintKindSource, TaintCategoryGeneric, "URLRequest.httpBody/queryItems", 0.85)
	// Sanitizers first.
	out = appendTaintMatches(out, content, headers, swiftSanitizerSQLRe, TaintKindSanitizer, TaintCategorySQL, "Statement.run([binding])/execute(\"...?\")", 1.0)
	out = appendTaintMatches(out, content, headers, swiftSanitizerCodableRe, TaintKindSanitizer, TaintCategoryGeneric, ".decode(T.self)/Codable", 0.9)
	// Sinks.
	out = appendTaintMatches(out, content, headers, swiftSinkSQLRe, TaintKindSink, TaintCategorySQL, "db.execute/run(non-literal)", 0.9)
	out = appendTaintMatches(out, content, headers, swiftSinkExecRe, TaintKindSink, TaintCategoryCommand, "Process()/launchedProcess(launchPath:var)", 1.0)
	out = appendTaintMatches(out, content, headers, swiftSinkFSRe, TaintKindSink, TaintCategoryPath, "FileManager.createFile/data.write(to:var)", 0.85)
	return out
}
