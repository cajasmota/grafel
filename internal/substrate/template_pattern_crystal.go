// Crystal template-pattern sniffer (#2779 Phase 3D T3).
//
// Recognises:
//   - i18n       : I18n.translate("key"), I18n.t("key"), t("key").
//   - log_format : puts "...", print "...", Log.debug/info/warn/error("...").
//   - sql        : Quoted string literals whose first non-whitespace
//                  token is a SQL verb — used with crystal-db / jennifer raw queries.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("crystal", sniffTemplatePatternsCrystal)
}

// crystalI18nRe matches I18n.translate / I18n.t / t() patterns.
// Group 1 = the key literal.
var crystalI18nRe = regexp.MustCompile(
	`\b(?:I18n\s*\.\s*(?:translate|t)|translate|t)\s*\(\s*"([^"]+)"`,
)

// crystalLogRe matches puts / print and Log module calls.
// Group 1 = literal payload.
var crystalLogRe = regexp.MustCompile(
	`\b(?:puts|print|p)\s+"([^"]+)"` +
		`|\bLog\s*\.\s*(?:debug|info|notice|warn|warning|error|fatal)\s*\{\s*"([^"]+)"` +
		`|\bLog\s*\.\s*(?:debug|info|notice|warn|warning|error|fatal)\s*\(\s*"([^"]+)"`,
)

// crystalSQLRe matches a quoted literal whose first word is a SQL verb.
// Group 1 = literal payload.
var crystalSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsCrystal(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanCrystalFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range crystalI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "I18n.t/translate",
		})
	}
	for _, m := range crystalLogRe.FindAllStringSubmatchIndex(content, -1) {
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
			Tag:      "puts/Log",
		})
	}
	for _, m := range crystalSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
