// PHP template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : __("key"), _e("key"), trans("key"), __t("key"),
//                   gettext("key") (Laravel / WordPress / Symfony idioms).
//   - log_format  : Log::<level>("..."), logger->info("...") (Monolog),
//                   error_log("..."), echo "<literal>".
//   - sql         : Quoted strings starting with a SQL verb.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("php", sniffTemplatePatternsPHP)
}

// phpI18nRe matches __("..."), _e("..."), trans("..."), gettext("...").
// Group 1 = the bare literal payload.
var phpI18nRe = regexp.MustCompile(
	`\b(?:__|_e|trans|__t|gettext|ngettext)\s*\(\s*['"]([^'"]+)['"]`,
)

// phpLogRe matches Log::<level>("..."), $logger->info("..."), error_log("...").
// Group 1 = method/level, Group 2 = literal.
var phpLogRe = regexp.MustCompile(
	`\b(?:Log::|(?:\$\w+\s*->)|(?:\$this\s*->logger\s*->))([a-zA-Z_]+)\s*\(\s*['"]([^'"]+)['"]`,
)

// phpErrorLogRe matches the standalone error_log("..."). Group 1 = literal.
var phpErrorLogRe = regexp.MustCompile(
	`\berror_log\s*\(\s*['"]([^'"]+)['"]`,
)

// phpEchoRe matches `echo "literal"`. Group 1 = literal.
var phpEchoRe = regexp.MustCompile(
	`\becho\s+['"]([^'"]+)['"]`,
)

// phpSQLRe matches a quoted literal whose first word is a SQL verb.
var phpSQLRe = regexp.MustCompile(
	`['"](\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[\s\S]*?)['"]`,
)

func sniffTemplatePatternsPHP(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanPHPFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range phpI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "trans",
		})
	}
	for _, m := range phpLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range phpErrorLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "error_log",
		})
	}
	for _, m := range phpEchoRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "echo",
		})
	}
	for _, m := range phpSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
