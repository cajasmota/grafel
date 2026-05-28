// Dart effect-sink sniffer (#2776 Phase 1A T3).
//
// Recognises Dart sink primitives:
//
//   - http_out  : package:http Client.get/post/put/patch/delete/send,
//                 Dio.get/post/put/patch/delete/request/fetch,
//                 http.get/post/put/patch/delete (top-level dart:io wrappers)
//   - db_read   : sqflite db.query / db.rawQuery,
//                 drift (moor) SELECT-shaped generated methods .get()/.watch()
//   - db_write  : sqflite db.insert / db.update / db.delete / db.execute /
//                 db.rawInsert / db.rawUpdate / db.rawDelete,
//                 drift generated .into().insert / .update / .delete
//   - fs_read   : dart:io File(...).readAsString / readAsBytes / readAsLines /
//                 readAsStringSync / openRead / openSync
//   - fs_write  : dart:io File(...).writeAsString / writeAsBytes /
//                 writeAsStringSync / writeAsBytesSync / openWrite / create
//   - mutation  : `this.field = ...` inside a method body
//
// Function attribution uses the nearest preceding `name(` or `async name(`
// header line. Dart's explicit function keyword is rare at top-level; most
// effectful code lives in class methods.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("dart", sniffEffectsDart) }

// dartFuncHeaderRe matches method/function declarations. Capture group 1 is
// the function/method name. Covers:
//
//	Future<T> name(   / void name(   / dynamic name(
//	Future<List<Map>> name(  (nested generics — matched via [^(]* up to `(`)
//	async name(       / name({       (named params)
var dartFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:Future|Stream|void|dynamic|bool|int|double|String|List|Map|Set|Iterable|Object|T)[^(]{0,40}\s+)?` +
		`(?:async\s+)?([A-Za-z_][\w]*)\s*\(`,
)

// dartHTTPRe matches outbound HTTP primitives.
var dartHTTPRe = regexp.MustCompile(
	`\b(?:http|client|_client|dio|_dio)\s*\.\s*(?:get|post|put|patch|delete|head|request|fetch|send)\s*\(` +
		`|\bhttp\s*\.\s*(?:get|post|put|patch|delete)\s*\(\s*(?:Uri|url|'https?://|"https?://)`,
)

// dartDBReadRe matches sqflite / drift read primitives.
var dartDBReadRe = regexp.MustCompile(
	`\b(?:db|database|_db|_database)\s*\.\s*(?:query|rawQuery)\s*\(` +
		`|\.\s*(?:get|watch|getSingle|watchSingle|getSingleOrNull|watchSingleOrNull)\s*\(\s*\)`,
)

// dartDBWriteRe matches sqflite / drift write primitives.
var dartDBWriteRe = regexp.MustCompile(
	`\b(?:db|database|_db|_database)\s*\.\s*(?:insert|update|delete|execute|rawInsert|rawUpdate|rawDelete|batch)\s*\(` +
		`|\.\s*into\s*\(\s*\)\s*\.\s*insert\s*\(` +
		`|\.\s*(?:insertReturning|insertOnConflictUpdate)\s*\(`,
)

// dartFSReadRe matches dart:io File read primitives.
var dartFSReadRe = regexp.MustCompile(
	`\.\s*(?:readAsString|readAsStringSync|readAsBytes|readAsBytesSync|readAsLines|readAsLinesSync|openRead|openSync)\s*\(`,
)

// dartFSWriteRe matches dart:io File write primitives.
var dartFSWriteRe = regexp.MustCompile(
	`\.\s*(?:writeAsString|writeAsStringSync|writeAsBytes|writeAsBytesSync|openWrite|create|delete|rename|copy)\s*\(` +
		`|\bDirectory\s*\(\s*[^)]+\s*\)\s*\.\s*(?:create|createSync|delete|deleteSync)\s*\(`,
)

// dartMutationRe matches `this.field = ...` inside a method.
var dartMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsDart(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanDartFuncHeaders(content)
	var out []EffectMatch
	out = appendDartMatches(out, content, headers, dartHTTPRe, EffectHTTPOut, "http/dio", 1.0)
	out = appendDartMatches(out, content, headers, dartDBReadRe, EffectDBRead, "sqflite/drift.read", 0.9)
	out = appendDartMatches(out, content, headers, dartDBWriteRe, EffectDBWrite, "sqflite/drift.write", 0.9)
	out = appendDartMatches(out, content, headers, dartFSReadRe, EffectFSRead, "File.read", 1.0)
	out = appendDartMatches(out, content, headers, dartFSWriteRe, EffectFSWrite, "File.write", 1.0)
	out = appendDartMatches(out, content, headers, dartMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

func scanDartFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range dartFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		name := content[m[2]:m[3]]
		if dartControlKeyword(name) {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

func dartControlKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "catch", "do", "return", "throw",
		"new", "await", "yield", "assert", "try", "else", "final", "const",
		"var", "static", "class", "extends", "implements", "mixin":
		return true
	}
	return false
}

func appendDartMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
