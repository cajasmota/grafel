// Swift effect-sink sniffer (#2776 Phase 1A T3).
//
// Recognises Swift sink primitives:
//
//   - http_out  : URLSession.shared.data(from:)/dataTask(with:)/upload(for:from:)
//                 Alamofire.request/AF.request, URLRequest + URLSession
//   - db_read   : CoreData NSFetchRequest / context.fetch / .count / .object
//                 SQLite.swift Table.select / run / fetchOne / scalar
//   - db_write  : CoreData context.save / context.delete / context.insert /
//                 context.perform (write-flavoured),
//                 SQLite.swift run(.insert) / run(.update) / run(.delete)
//   - fs_read   : FileManager.default.fileExists / contentsOfDirectory /
//                 contentsAtPath / String(contentsOfFile:) /
//                 Data(contentsOf:) / url.resourceValues
//   - fs_write  : FileManager.default.createDirectory / copyItem / moveItem /
//                 removeItem / createFile,
//                 try data.write(to:) / string.write(to:toFile:)
//   - mutation  : `self.field = ...` inside a method
//
// Function attribution uses `func name(` header lines.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("swift", sniffEffectsSwift) }

// swiftFuncHeaderRe matches Swift function/method declarations.
// Capture group 1 is the function name.
var swiftFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|private|internal|fileprivate|open|override|class|static|mutating|nonmutating|final)\s+)*` +
		`func\s+([A-Za-z_][\w]*)\s*[(<]`,
)

// swiftHTTPRe matches outbound HTTP primitives.
var swiftHTTPRe = regexp.MustCompile(
	`\bURLSession\s*\.\s*(?:shared\s*\.\s*)?(?:data|dataTask|upload|download|webSocketTask|streamTask)\s*\(` +
		`|\bAF\s*\.\s*request\s*\(` +
		`|\bAlamofire\s*\.\s*request\s*\(` +
		`|\bURLSession\s*\(\s*configuration\s*:` +
		`|\.\s*(?:dataTask|data|upload)\s*\(\s*with\s*:`,
)

// swiftDBReadRe matches CoreData / SQLite.swift read primitives.
var swiftDBReadRe = regexp.MustCompile(
	`\bNSFetchRequest\b` +
		`|\bcontext\s*\.\s*(?:fetch|count|object|existingObject)\s*\(` +
		`|\btry\s+context\s*\.\s*fetch\s*\(` +
		`|\b(?:db|database)\s*\.\s*(?:prepare|scalar|pluck)\s*\(`,
)

// swiftDBWriteRe matches CoreData / SQLite.swift write primitives.
var swiftDBWriteRe = regexp.MustCompile(
	`\bcontext\s*\.\s*(?:save|delete|insert|perform|performAndWait)\s*\(` +
		`|\btry\s+context\s*\.\s*save\s*\(` +
		`|\b(?:db|database)\s*\.\s*run\s*\(\s*(?:[a-z]+\s*\.\s*)?(?:insert|update|delete|setters)`,
)

// swiftFSReadRe matches FileManager and Data read primitives.
var swiftFSReadRe = regexp.MustCompile(
	`\bFileManager\s*\.\s*default\s*\.\s*(?:fileExists|contentsOfDirectory|contents|attributesOfItem|enumerator|subpaths|isReadableFile|isExecutableFile)\s*\(` +
		`|\bFileManager\s*\(\s*\)\s*\.\s*(?:fileExists|contentsOfDirectory)\s*\(` +
		`|\bString\s*\(\s*contentsOf(?:File|URL|URL:encoding:)` +
		`|\bData\s*\(\s*contentsOf\s*:`,
)

// swiftFSWriteRe matches FileManager and Data write primitives.
var swiftFSWriteRe = regexp.MustCompile(
	`\bFileManager\s*\.\s*default\s*\.\s*(?:createDirectory|copyItem|moveItem|removeItem|createFile|createSymbolicLink|createLink)\s*\(` +
		`|\bFileManager\s*\(\s*\)\s*\.\s*(?:createDirectory|copyItem|moveItem|removeItem|createFile)\s*\(` +
		`|\btry\s+data\s*\.\s*write\s*\(\s*to\s*:` +
		`|\btry\s+[a-z][\w]*\s*\.\s*write\s*\(\s*to\s*:` +
		`|\btry\s+[a-z][\w]*\s*\.\s*write\s*\(\s*toFile\s*:`,
)

// swiftMutationRe matches `self.field = ...`.
var swiftMutationRe = regexp.MustCompile(
	`\bself\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsSwift(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanSwiftFuncHeaders(content)
	var out []EffectMatch
	out = appendSwiftMatches(out, content, headers, swiftHTTPRe, EffectHTTPOut, "URLSession/Alamofire", 1.0)
	out = appendSwiftMatches(out, content, headers, swiftDBReadRe, EffectDBRead, "CoreData.read/SQLite.read", 0.9)
	out = appendSwiftMatches(out, content, headers, swiftDBWriteRe, EffectDBWrite, "CoreData.write/SQLite.write", 0.9)
	out = appendSwiftMatches(out, content, headers, swiftFSReadRe, EffectFSRead, "FileManager.read/Data(contentsOf:)", 1.0)
	out = appendSwiftMatches(out, content, headers, swiftFSWriteRe, EffectFSWrite, "FileManager.write/data.write", 1.0)
	out = appendSwiftMatches(out, content, headers, swiftMutationRe, EffectMutation, "self.field=", 0.7)
	return out
}

func scanSwiftFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range swiftFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendSwiftMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
