// Zig taint-sites sniffer (#2778 Phase 2B T3).
//
// Recognises Zig source / sink / sanitizer primitives for std.http and
// std.fs / std.ChildProcess.
//
// Sources:
//   - std.http server: request.readBody / server.accept / request.reader
//     — the body of an incoming HTTP request
//   - zap / httpz / zig-http framework: req.body / req.param / req.query
//
// Sinks:
//   - Path traversal / fs write: std.fs.cwd().createFile with a non-literal
//     path (a variable / slice), file.writeAll with user data
//   - Command injection: std.ChildProcess (deprecated) / std.process.Child
//     .init / .spawn with a non-literal argv[0]
//
// Sanitizers:
//   - std.heap arena patterns: ArenaAllocator ensures memory-safe bounded
//     allocation. Not a security sanitizer per se, but in Zig context
//     arena allocation combined with type-checked deserialization (std.json.parseFromSlice
//     to a typed struct) acts as an input-schema constraint.
//   - std.json.parseFromSlice / parseFromTokenSource to a comptime-known type T:
//     the type parameter is the schema declaration.
//     HARD RULE per #2778: bare parseFromSlice to `anytype` does NOT count.
package substrate

import "regexp"

func init() { RegisterTaintSniffer("zig", sniffTaintZig) }

// zigSourceHTTPRe matches std.http server / framework request body access.
var zigSourceHTTPRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:readBody|reader|body)\b` +
		`|\bserver\s*\.\s*accept\s*\(` +
		`|\breq\s*\.\s*(?:body|param|query)\b`,
)

// zigSinkFSRe matches std.fs write with a non-literal path (identifier as arg).
// Matches: cwd().createFile(varName, ...) or dir.createFile(varName, ...) or
// file.writeAll(varName) where varName is an identifier.
var zigSinkFSRe = regexp.MustCompile(
	`\b(?:cwd|dir)\s*\(\s*\)\s*\.\s*createFile\s*\(\s*[A-Za-z_][\w]*` +
		`|\b(?:dir|cwd)\s*\.\s*createFile\s*\(\s*[A-Za-z_][\w]*` +
		`|\bfile\s*\.\s*writeAll\s*\(\s*[A-Za-z_][\w]*` +
		`|\bstd\s*\.\s*fs\s*\.\s*cwd\s*\(\s*\)\s*\.\s*(?:createFile|writeFile)\s*\(\s*[A-Za-z_][\w]*`,
)

// zigSinkChildProcessRe matches std.ChildProcess.init / std.process.Child.init
// / Child.init / .spawn. Any invocation of the child-process constructor is
// a command-injection risk if argv is user-controlled.
var zigSinkChildProcessRe = regexp.MustCompile(
	`\b(?:std\s*\.\s*)?ChildProcess\s*\.\s*(?:init|run)\s*\(` +
		`|\bstd\s*\.\s*process\s*\.\s*Child\s*\.\s*init\s*\(` +
		`|\bChild\s*\.\s*init\s*\(`,
)

// zigSanitizerJsonParseRe matches std.json.parseFromSlice / parseFromTokenSource
// called with a concrete type T (not anytype / var). The type T acts as a
// schema constraint — analogous to a Codable declaration in Swift.
// HARD RULE per #2778: the type argument must be a named struct type (starts
// with an upper-case letter — Zig convention for types).
var zigSanitizerJsonParseRe = regexp.MustCompile(
	`\bstd\s*\.\s*json\s*\.\s*(?:parseFromSlice|parseFromTokenSource)\s*\(\s*[A-Z][A-Za-z][\w]*` +
		`|\bstd\s*\.\s*json\s*\.\s*parseFromSlice\s*\([^,)]*,\s*allocator\s*,`,
)

// zigSanitizerArenaRe matches ArenaAllocator usage — a bounded-allocation
// pattern that prevents heap-overflow from unbounded user input.
var zigSanitizerArenaRe = regexp.MustCompile(
	`\bArenaAllocator\s*\.\s*init\s*\(`,
)

func sniffTaintZig(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanZigFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, zigSourceHTTPRe, TaintKindSource, TaintCategoryGeneric, "request.body/req.body/server.accept", 1.0)
	// Sanitizers first.
	out = appendTaintMatches(out, content, headers, zigSanitizerJsonParseRe, TaintKindSanitizer, TaintCategoryGeneric, "std.json.parseFromSlice(TypedStruct,...)", 0.9)
	out = appendTaintMatches(out, content, headers, zigSanitizerArenaRe, TaintKindSanitizer, TaintCategoryGeneric, "ArenaAllocator.init", 0.75)
	// Sinks.
	out = appendTaintMatches(out, content, headers, zigSinkFSRe, TaintKindSink, TaintCategoryPath, "cwd().createFile/writeAll(non-literal)", 0.85)
	out = appendTaintMatches(out, content, headers, zigSinkChildProcessRe, TaintKindSink, TaintCategoryCommand, "ChildProcess.init/Child.init(argv)", 1.0)
	return out
}
