// Go template-pattern sniffer (#2774 Phase 3D T1).
//
// Recognises:
//   - i18n        : i18n.T("key") / message.NewPrinter().Sprintf("key", ...) —
//                   conservative pattern, no widely-used canonical idiom
//                   so we restrict to .T("...") method calls.
//   - log_format  : fmt.Printf / fmt.Println / log.Printf / log.Println /
//                   log.<Level>f("..."), klog.V(...).Infof("...") —
//                   captures the format string literal.
//   - sql         : Backtick/quoted literals whose first non-whitespace
//                   token is a SQL verb (case-insensitive).
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("go", sniffTemplatePatternsGo)
}

// goI18nRe matches a .T("...") method call shape used by go-i18n,
// nicksnyder/go-i18n, and message.NewPrinter()-style printers.
// Group 1 = the bare literal.
var goI18nRe = regexp.MustCompile(
	`\.\s*(?:T|Tr|Translate|GetMessage)\s*\(\s*"([^"]+)"`,
)

// goLogRe matches fmt.Printf / fmt.Println / log.Printf / log.Println /
// log.<Level>f("..."). Group 1 = the function name; Group 2 = literal.
var goLogRe = regexp.MustCompile(
	`\b(?:fmt|log|klog|slog|logger|zap|logrus)\s*\.\s*([A-Za-z][A-Za-z0-9_]*)\s*\(\s*"([^"]+)"`,
)

// goSQLRe matches a quoted or backticked literal whose first word is a
// SQL verb. Group 1 = literal payload.
var goSQLRe = regexp.MustCompile(
	"[\"`](\\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\\s+TABLE|DROP\\s+TABLE|ALTER\\s+TABLE)[^\"`]*)[\"`]",
)

func sniffTemplatePatternsGo(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanGoFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range goI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "i18n.T()",
		})
	}
	for _, m := range goLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range goSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
