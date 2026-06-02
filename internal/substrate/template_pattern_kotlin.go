// Kotlin template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : getString(R.string.key, ...) (Android), context
//     .getString("key"), I18n.translate("key").
//   - log_format  : Log.<level>(TAG, "..."), logger.<level>("..."),
//     println("...") with placeholder tokens.
//   - sql         : Quoted/triple-quoted string literals whose first non-
//     whitespace token is a SQL verb.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("kotlin", sniffTemplatePatternsKotlin)
}

// kotlinI18nRe matches getString("..."), translate("..."), t("...").
// Group 1 = the bare literal payload.
var kotlinI18nRe = regexp.MustCompile(
	`\b(?:getString|translate|t)\s*\(\s*"([^"]+)"`,
)

// kotlinLogRe matches Log.<level>(...) / logger.<level>(...).
// Group 1 = level, Group 2 = literal.
var kotlinLogRe = regexp.MustCompile(
	`\b(?:Log|logger|log|LOGGER|LOG)\s*\.\s*([a-zA-Z_]+)\s*\(\s*(?:[^,"]*,\s*)?"([^"]+)"`,
)

// kotlinPrintlnRe matches println("...") / print("...").
var kotlinPrintlnRe = regexp.MustCompile(
	`\b(?:println|print)\s*\(\s*"([^"]+)"`,
)

// kotlinSQLRe matches a quoted literal whose first word is a SQL verb.
var kotlinSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsKotlin(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanKotlinFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range kotlinI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "getString",
		})
	}
	for _, m := range kotlinLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[4]:m[5]]),
			Tag:      "log." + content[m[2]:m[3]],
		})
	}
	for _, m := range kotlinPrintlnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "println",
		})
	}
	for _, m := range kotlinSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
