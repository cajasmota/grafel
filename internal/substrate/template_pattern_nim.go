// Nim template-pattern sniffer (#2779 Phase 3D T3).
//
// Recognises:
//   - i18n       : tr("key") / translate("key") (nim-i18n / i18n packages).
//   - log_format : echo "...", writeLine(stdout, "..."),
//                  logging.info("...") / debug("...") / warn("...") / error("...").
//   - sql        : Quoted string literals whose first non-whitespace
//                  token is a SQL verb — used with nim-db / norm raw queries.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("nim", sniffTemplatePatternsNim)
}

// nimI18nRe matches tr() / translate() i18n patterns.
// Group 1 = the key literal.
var nimI18nRe = regexp.MustCompile(
	`\b(?:tr|translate|gettext)\s*\(\s*"([^"]+)"`,
)

// nimLogRe matches echo, writeLine, and logging module calls.
// Group 1 = literal payload.
var nimLogRe = regexp.MustCompile(
	`\becho\s+"([^"]+)"` +
		`|\bwriteLine\s*\(\s*\w+\s*,\s*"([^"]+)"` +
		`|\blogging\s*\.\s*(?:debug|info|warn|warning|error|fatal)\s*\(\s*"([^"]+)"` +
		`|\b(?:debug|info|warn|error|fatal)\s*\(\s*"([^"]+)"`,
)

// nimSQLRe matches a quoted literal whose first word is a SQL verb.
// Group 1 = literal payload.
var nimSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsNim(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanNimFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range nimI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "tr()/translate()",
		})
	}
	for _, m := range nimLogRe.FindAllStringSubmatchIndex(content, -1) {
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
			Tag:      "echo/logging",
		})
	}
	for _, m := range nimSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
