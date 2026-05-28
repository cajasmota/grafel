// Python template-pattern sniffer (#2774 Phase 3D T1).
//
// Recognises:
//   - i18n        : gettext("..."), _("..."), ngettext("...","...",n),
//                   _l("..."), translate("...").
//   - log_format  : logger.<level>("..."), logging.<level>("..."),
//                   print("...{}..."). Only literal first arg is captured.
//   - sql         : Quoted/triple-quoted string literals whose first
//                   non-whitespace token is a SQL verb (case-insensitive).
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("python", sniffTemplatePatternsPython)
}

// pyI18nRe matches gettext("..."), _("..."), ngettext("..."), translate("...").
// Group 1 = the bare literal payload.
var pyI18nRe = regexp.MustCompile(
	`\b(?:gettext|ngettext|pgettext|_l|translate|_)\s*\(\s*['"]([^'"]+)['"]`,
)

// pyLogRe matches logger.<level>("...") / logging.<level>("..."), print("...").
// Group 1 = level (info/warning/error/debug), Group 2 = literal.
var pyLogRe = regexp.MustCompile(
	`\b(?:logger|logging|log)\s*\.\s*([a-z_]+)\s*\(\s*['"]([^'"]+)['"]`,
)

// pyPrintRe matches print("..."). Group 1 = literal.
var pyPrintRe = regexp.MustCompile(
	`\bprint\s*\(\s*['"]([^'"]+)['"]`,
)

// pySQLRe matches a quoted literal whose first word is a SQL verb. Group
// 1 = the literal payload.
var pySQLRe = regexp.MustCompile(
	`['"](\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[\s\S]*?)['"]`,
)

func sniffTemplatePatternsPython(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanPyFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range pyI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "gettext",
		})
	}
	for _, m := range pyLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[4]:m[5]]),
			Tag:      "logger." + content[m[2]:m[3]],
		})
	}
	for _, m := range pyPrintRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "print",
		})
	}
	for _, m := range pySQLRe.FindAllStringSubmatchIndex(content, -1) {
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
