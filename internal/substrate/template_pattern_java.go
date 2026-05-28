// Java template-pattern sniffer (#2774 Phase 3D T1).
//
// Recognises:
//   - i18n        : messages.getMessage("key", ...), messageSource.getMessage,
//                   bundle.getString("key"), Translator.translate("key").
//   - log_format  : logger.<level>("..."), log.<level>("..."),
//                   slf4j-style {} placeholders captured in the literal.
//   - sql         : Quoted string literals (single and concatenated)
//                   whose first non-whitespace token is a SQL verb.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("java", sniffTemplatePatternsJava)
}

// javaI18nRe matches getMessage("key", ...) / getString("key") / translate("key").
// Group 1 = the bare literal.
var javaI18nRe = regexp.MustCompile(
	`\b(?:getMessage|getString|translate|t)\s*\(\s*"([^"]+)"`,
)

// javaLogRe matches logger.<level>("...") / log.<level>("...").
// Group 1 = level, Group 2 = literal.
var javaLogRe = regexp.MustCompile(
	`\b(?:logger|log|LOGGER|LOG)\s*\.\s*([a-z]+)\s*\(\s*"([^"]+)"`,
)

// javaSQLRe matches a quoted literal whose first word is a SQL verb.
// Group 1 = the literal payload.
var javaSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsJava(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanJavaFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range javaI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "getMessage",
		})
	}
	for _, m := range javaLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range javaSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
