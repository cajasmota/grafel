// Dart taint-sites sniffer (#2778 Phase 2B T3).
//
// Recognises Dart source / sink / sanitizer primitives for sqflite,
// dart:io, and package:http / Dio.
//
// Sources:
//   - HTTP request input (server-side): HttpRequest.uri.queryParameters,
//     HttpRequest.requestedUri, body-bytes read from HttpRequest
//   - shelf: Request.url.queryParameters, Request.readAsString,
//     Request.read (body stream)
//
// Sinks:
//   - SQL injection: sqflite db.rawQuery / db.rawInsert / db.rawUpdate /
//     db.rawDelete with a string that is not a literal (i.e. involves a
//     variable / concatenation)
//   - Command injection: Process.run / Process.start with a non-literal
//     executable argument
//   - Path traversal: File.writeAsString / File.writeAsBytes /
//     File.writeAsBytesSync / File.writeAsStringSync with a non-literal
//     File path
//
// Sanitizers:
//   - Parameterised sqflite: db.query(..., where: '?', whereArgs: [...])
//     — the whereArgs list form; db.rawQuery with substitutionValues map
//   - Validation: package:validators / validators: prefix in pubspec
//     (approximated as the presence of a `validate(...)` / `Validator`
//     usage in the file)
package substrate

import "regexp"

func init() { RegisterTaintSniffer("dart", sniffTaintDart) }

// dartSourceHTTPRe matches dart:io HttpRequest / shelf Request inputs.
var dartSourceHTTPRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:uri\s*\.\s*queryParameters|requestedUri|readAsString|read)\b` +
		`|\bHttpRequest\b` +
		`|\breq\s*\.\s*(?:url\s*\.\s*queryParameters|readAsString|read)\b`,
)

// dartSinkSQLRawRe matches sqflite raw* methods with a non-literal SQL string.
// Two forms are dangerous:
//  1. A bare identifier as the SQL arg (the string is built elsewhere).
//  2. A string literal followed by concatenation (`'SELECT ...' + userVar`).
var dartSinkSQLRawRe = regexp.MustCompile(
	`\b(?:db|database|_db|_database)\s*\.\s*(?:rawQuery|rawInsert|rawUpdate|rawDelete)\s*\(\s*(?:[A-Za-z_$][\w$]*|['"][^'"]*['"]\s*\+)`,
)

// dartSinkExecRe matches Process.run / Process.start with a non-literal arg.
var dartSinkExecRe = regexp.MustCompile(
	`\bProcess\s*\.\s*(?:run|start)\s*\(\s*[A-Za-z_$][\w$]*`,
)

// dartSinkFSRe matches File(...).writeAsString / writeAsBytes etc. with a
// non-literal File path (identifier as the path argument).
var dartSinkFSRe = regexp.MustCompile(
	`\bFile\s*\(\s*[A-Za-z_$][\w$]*\s*\)\s*\.\s*(?:writeAsString|writeAsStringSync|writeAsBytes|writeAsBytesSync|openWrite|create)\s*\(`,
)

// dartSanitizerSQLRe matches the safe sqflite parameterised form:
//
//	db.query(..., whereArgs: [...]) or db.rawQuery(sql, {substitutionValues: ...})
var dartSanitizerSQLRe = regexp.MustCompile(
	`\b(?:db|database|_db|_database)\s*\.\s*query\s*\([^)]*whereArgs\s*:` +
		`|\b(?:db|database|_db|_database)\s*\.\s*rawQuery\s*\([^)]*substitutionValues\s*:`,
)

// dartSanitizerValidatorRe matches the validators package usage:
// a `Validator` class or `validate(...)` method in the file.
// HARD RULE: presence of `Validator` class usage in the file counts;
// a bare method call without a declared schema does NOT.
var dartSanitizerValidatorRe = regexp.MustCompile(
	`\bValidator\s*\(\s*\)` +
		`|\bvalidators\s*\.\s*isEmail\b|\bvalidators\s*\.\s*isURL\b|\bvalidators\s*\.\s*isNumeric\b`,
)

func sniffTaintDart(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanDartFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, dartSourceHTTPRe, TaintKindSource, TaintCategoryGeneric, "HttpRequest/shelf.Request input", 1.0)
	// Sanitizers before sinks.
	out = appendTaintMatches(out, content, headers, dartSanitizerSQLRe, TaintKindSanitizer, TaintCategorySQL, "sqflite.query(whereArgs)/rawQuery(substitutionValues)", 1.0)
	out = appendTaintMatches(out, content, headers, dartSanitizerValidatorRe, TaintKindSanitizer, TaintCategoryGeneric, "validators.isEmail/isURL/Validator()", 0.85)
	// Sinks.
	out = appendTaintMatches(out, content, headers, dartSinkSQLRawRe, TaintKindSink, TaintCategorySQL, "db.rawQuery/rawInsert(non-literal)", 0.9)
	out = appendTaintMatches(out, content, headers, dartSinkExecRe, TaintKindSink, TaintCategoryCommand, "Process.run/start(non-literal)", 1.0)
	out = appendTaintMatches(out, content, headers, dartSinkFSRe, TaintKindSink, TaintCategoryPath, "File(non-literal).writeAsString/writeAsBytes", 0.85)
	return out
}
