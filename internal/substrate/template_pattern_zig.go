// Zig template-pattern sniffer (#2779 Phase 3D T3).
//
// Recognises:
//   - i18n       : Zig has no widely-adopted i18n library; the closest
//                  static pattern is a bare .translate("key") method call
//                  or a zig-intl comptime lookup. Only gettext-style calls
//                  are recognised — minimal but not padded.
//   - log_format : std.debug.print("...", .{...}),
//                  std.log.debug/info/warn/err("..."),
//                  std.io.getStdOut().writer().print("...").
//   - sql        : Quoted string literals whose first non-whitespace
//                  token is a SQL verb — used with sqlite / zqlite raw queries.
//
// No SQL i18n candidates — Zig's ecosystem is pre-1.0 and i18n is rare.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("zig", sniffTemplatePatternsZig)
}

// zigI18nRe matches translate / gettext method calls (rare but present in
// Zig FFI wrappers around C gettext).
// Group 1 = the key literal.
var zigI18nRe = regexp.MustCompile(
	`\b(?:gettext|translate|_)\s*\(\s*"([^"]+)"`,
)

// zigLogRe matches std.debug.print, std.log.* and writer().print().
// Group 1 = literal payload.
var zigLogRe = regexp.MustCompile(
	`\bstd\s*\.\s*debug\s*\.\s*print\s*\(\s*"([^"]+)"` +
		`|\bstd\s*\.\s*log\s*\.\s*(?:debug|info|warn|err|scoped)\s*\(\s*"([^"]+)"` +
		`|\bwriter\s*\(\s*\)\s*\.\s*print\s*\(\s*"([^"]+)"`,
)

// zigSQLRe matches a quoted literal whose first word is a SQL verb.
// Group 1 = literal payload.
var zigSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsZig(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanZigFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range zigI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "gettext()",
		})
	}
	for _, m := range zigLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		var lit string
		for i := 2; i < len(m)-1; i += 2 {
			if m[i] >= 0 {
				lit = content[m[i]:m[i+1]]
				break
			}
		}
		if lit == "" {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(lit),
			Tag:      "std.debug.print/std.log",
		})
	}
	for _, m := range zigSQLRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindSQL,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "sql.literal",
		})
	}
	return out
}
