// Zig effect-sink sniffer (#2776 Phase 1A T3).
//
// Recognises Zig sink primitives:
//
//   - http_out  : std.http.Client.fetch / .request / std.net.tcpConnectToHost /
//                 std.net.tcpConnectToAddress / zig-http / http.Client.fetch
//   - db_read   : sqlite.Db.prepare / .exec (SELECT) / .one / .all /
//                 zqlite SELECT-shaped calls
//   - db_write  : sqlite.Db.exec("INSERT|UPDATE|DELETE") / .exec (write) /
//                 zqlite write-shaped calls
//   - fs_read   : std.fs.cwd().openFile / std.fs.openFileAbsolute /
//                 std.fs.Dir.readFile / dir.openFile / file.readAll /
//                 file.readToEndAlloc / std.fs.cwd().readFile
//   - fs_write  : std.fs.cwd().createFile / std.fs.Dir.createFile /
//                 file.writeAll / file.writeAllBytes / std.fs.cwd().writeFile /
//                 dir.deleteFile / dir.deleteDir / dir.makeDir / std.os.rename
//   - mutation  : field assignment through a pointer-to-struct
//                 (`self.field = ` or `ptr.field = `) — pointer semantics
//
// Function attribution uses `fn name` header lines.
// Zig's explicit `fn` keyword makes this unambiguous.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("zig", sniffEffectsZig) }

// zigFuncHeaderRe matches Zig function declarations.
// Capture group 1 is the function name.
var zigFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:pub\s+)?fn\s+([A-Za-z_][\w]*)\s*\(`,
)

// zigHTTPRe matches std.http / std.net primitives.
var zigHTTPRe = regexp.MustCompile(
	`\bstd\s*\.\s*http\s*\.\s*Client\b` +
		`|\b(?:client|http_client)\s*\.\s*(?:fetch|request|send)\s*\(` +
		`|\bstd\s*\.\s*net\s*\.\s*(?:tcpConnectToHost|tcpConnectToAddress|Stream\.connect)\s*\(` +
		`|\bhttp\s*\.\s*Client\s*\.\s*(?:fetch|request)\s*\(`,
)

// zigDBReadRe matches SQLite/zqlite read primitives.
var zigDBReadRe = regexp.MustCompile(
	`\b(?:db|database|conn|sqlite)\s*\.\s*(?:prepare|one|all|exec)\s*\(` +
		`|\bDb\s*\.\s*(?:init|open)\s*\(`,
)

// zigDBWriteRe matches SQLite exec with write SQL.
var zigDBWriteRe = regexp.MustCompile(
	`\b(?:db|database|conn|sqlite)\s*\.\s*exec\s*\(\s*["'](?i:\s*(?:INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|REPLACE|TRUNCATE)\b)`,
)

// zigFSReadRe matches std.fs read primitives.
var zigFSReadRe = regexp.MustCompile(
	`\bstd\s*\.\s*fs\s*\.\s*(?:cwd|openDirAbsolute|openFileAbsolute)\b[^;]*\.\s*(?:openFile|readFile|readToEndAlloc|readAll)\s*\(` +
		`|\b(?:dir|cwd)\s*\.\s*openFile\s*\(` +
		`|\bfile\s*\.\s*(?:readAll|readToEndAlloc|readAllAlloc|readToEnd)\s*\(` +
		`|\bstd\s*\.\s*fs\s*\.\s*cwd\s*\(\s*\)\s*\.\s*readFile\s*\(`,
)

// zigFSWriteRe matches std.fs write primitives.
var zigFSWriteRe = regexp.MustCompile(
	`\bstd\s*\.\s*fs\s*\.\s*(?:cwd|openDirAbsolute)\b[^;]*\.\s*(?:createFile|writeFile|deleteFile|deleteDir|makeDir|rename)\s*\(` +
		`|\b(?:dir|cwd)\s*\.\s*(?:createFile|writeFile|deleteFile|deleteDir|makeDir)\s*\(` +
		`|\bfile\s*\.\s*(?:writeAll|writeAllBytes|writeAll|pwrite|seekAndWrite|truncate)\s*\(` +
		`|\bstd\s*\.\s*os\s*\.\s*(?:rename|unlink|rmdir|mkdir|chmod|symlink)\s*\(`,
)

// zigMutationRe matches `self.field = ` or pointer-field assignment `ptr.field = `.
var zigMutationRe = regexp.MustCompile(
	`\b(?:self|this|ptr|state)\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsZig(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanZigFuncHeaders(content)
	var out []EffectMatch
	out = appendZigMatches(out, content, headers, zigHTTPRe, EffectHTTPOut, "std.http.Client/std.net", 1.0)
	out = appendZigMatches(out, content, headers, zigDBReadRe, EffectDBRead, "sqlite.read", 0.8)
	out = appendZigMatches(out, content, headers, zigDBWriteRe, EffectDBWrite, "sqlite.exec(WRITE)", 1.0)
	out = appendZigMatches(out, content, headers, zigFSReadRe, EffectFSRead, "std.fs.read/openFile", 1.0)
	out = appendZigMatches(out, content, headers, zigFSWriteRe, EffectFSWrite, "std.fs.write/createFile", 1.0)
	out = appendZigMatches(out, content, headers, zigMutationRe, EffectMutation, "self.field=", 0.7)
	return out
}

func scanZigFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range zigFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendZigMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
