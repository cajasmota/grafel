// JS/TS template-pattern sniffer (#2774 Phase 3D T1).
//
// Recognises:
//   - i18n        : t("key"), i18n.t("key"), $t("key"), useTranslation
//                   exported t-callable invocations.
//   - log_format  : console.<level>("...{}..."), logger.<level>("...{}..."),
//                   log.<level>("..."). Only literal first arg is captured.
//   - sql         : Backtick or quoted string literals whose first non-
//                   whitespace token is a SQL verb (case-insensitive).
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("jsts", sniffTemplatePatternsJSTS)
}

// jstsI18nRe matches t("..."), i18n.t("..."), $t("..."), trans("..."),
// gettext("..."). Group 1 = the bare literal payload.
var jstsI18nRe = regexp.MustCompile(
	`\b(?:t|\$t|i18n\.t|i18next\.t|trans|gettext|_t)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`,
)

// jstsLogRe matches console.<level>(...) and logger/log.<level>(...).
// Group 1 = level (info/log/warn/error/debug/trace), Group 2 = literal.
var jstsLogRe = regexp.MustCompile(
	`\b(?:console|logger|log)\.([a-z]+)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`,
)

// jstsSQLRe matches a quoted/backticked literal whose first word is a
// SQL verb. Group 1 = the literal payload (used for both Literal and
// kind detection).
var jstsSQLRe = regexp.MustCompile(
	`['"` + "`" + `](\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[\s\S]*?)['"` + "`" + `]`,
)

func sniffTemplatePatternsJSTS(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanJSTSFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range jstsI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "t()",
		})
	}
	for _, m := range jstsLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[4]:m[5]]),
			Tag:      "console/logger." + content[m[2]:m[3]],
		})
	}
	for _, m := range jstsSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
