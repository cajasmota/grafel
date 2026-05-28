// Rust template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : t!("key") (fluent / rust-i18n macros), gettext("key").
//   - log_format  : log::<level>!("..."), tracing::<level>!("..."),
//                   println!("...{}..."), eprintln!("...{}...").
//   - sql         : sqlx::query("SELECT ..."), bare quoted/raw string
//                   literals whose first non-whitespace token is a SQL verb.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("rust", sniffTemplatePatternsRust)
}

// rustI18nRe matches t!("..."), tr!("..."), gettext("...").
// Group 1 = the bare literal payload.
var rustI18nRe = regexp.MustCompile(
	`\b(?:t!|tr!|gettext)\s*\(?\s*"([^"]+)"`,
)

// rustLogRe matches log::<lvl>!("...") and tracing::<lvl>!("...").
// Group 1 = level, Group 2 = literal.
var rustLogRe = regexp.MustCompile(
	`\b(?:log|tracing)\s*::\s*([a-z]+)!\s*\(\s*"([^"]+)"`,
)

// rustPrintRe matches println!("...") / eprintln!("...") / print!("...").
// Group 1 = literal.
var rustPrintRe = regexp.MustCompile(
	`\b(?:println|eprintln|print|eprint)!\s*\(\s*"([^"]+)"`,
)

// rustSQLRe matches a quoted literal whose first word is a SQL verb.
var rustSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsRust(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanRustFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range rustI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "t!",
		})
	}
	for _, m := range rustLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range rustPrintRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "println!",
		})
	}
	for _, m := range rustSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
