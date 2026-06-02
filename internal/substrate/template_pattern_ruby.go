// Ruby template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : I18n.t("key"), I18n.translate("key"), t("key"),
//     bare _("key") helpers commonly used in Rails views.
//   - log_format  : logger.<level>("..."), Rails.logger.<level>("..."),
//     puts("...").
//   - sql         : Quoted/heredoc strings whose first non-whitespace
//     token is a SQL verb (case-insensitive).
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("ruby", sniffTemplatePatternsRuby)
}

// rubyI18nRe matches I18n.t / I18n.translate / t / translate / _.
// Group 1 = the bare literal payload.
var rubyI18nRe = regexp.MustCompile(
	`\b(?:I18n\.t|I18n\.translate|t|translate|_)\s*\(\s*['"]([^'"]+)['"]`,
)

// rubyLogRe matches logger.<level>("...") and Rails.logger.<level>("...").
// Group 1 = level, Group 2 = literal.
var rubyLogRe = regexp.MustCompile(
	`\b(?:Rails\.logger|logger|log)\s*\.\s*([a-z_]+)\s*\(\s*['"]([^'"]+)['"]`,
)

// rubyPutsRe matches puts("...") / print("..."). Group 1 = literal.
var rubyPutsRe = regexp.MustCompile(
	`\b(?:puts|print|p)\s*\(?\s*['"]([^'"]+)['"]`,
)

// rubySQLRe matches a quoted literal whose first word is a SQL verb.
var rubySQLRe = regexp.MustCompile(
	`['"](\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[\s\S]*?)['"]`,
)

func sniffTemplatePatternsRuby(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanRubyFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range rubyI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "I18n.t",
		})
	}
	for _, m := range rubyLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range rubyPutsRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "puts",
		})
	}
	for _, m := range rubySQLRe.FindAllStringSubmatchIndex(content, -1) {
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
