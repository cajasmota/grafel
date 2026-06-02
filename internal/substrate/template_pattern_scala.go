// Scala template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : Messages("key", ...) (Play), messagesApi("key"),
//     translate("key"), t("key").
//   - log_format  : logger.<level>("..."), Logger.<level>("..."),
//     println("..."). Captures literal first arg.
//   - sql         : Quoted/triple-quoted string literals whose first non-
//     whitespace token is a SQL verb.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("scala", sniffTemplatePatternsScala)
}

// scalaI18nRe matches Messages("key"), messagesApi("key"), translate("..."), t("...").
// Group 1 = the bare literal payload.
var scalaI18nRe = regexp.MustCompile(
	`\b(?:Messages|messagesApi|translate|t)\s*\(\s*"([^"]+)"`,
)

// scalaLogRe matches logger.<level>(...) / Logger.<level>(...).
// Group 1 = level, Group 2 = literal.
var scalaLogRe = regexp.MustCompile(
	`\b(?:logger|Logger|log|LOG)\s*\.\s*([a-zA-Z_]+)\s*\(\s*"([^"]+)"`,
)

// scalaPrintlnRe matches println("..."). Group 1 = literal.
var scalaPrintlnRe = regexp.MustCompile(
	`\bprintln\s*\(\s*"([^"]+)"`,
)

// scalaSQLRe matches a quoted literal whose first word is a SQL verb.
var scalaSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsScala(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanScalaFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range scalaI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "Messages",
		})
	}
	for _, m := range scalaLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range scalaPrintlnRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range scalaSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
