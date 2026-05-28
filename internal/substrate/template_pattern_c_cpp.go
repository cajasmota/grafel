// C / C++ template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : gettext("key"), _("key"), N_("key") (GNU gettext idioms).
//   - log_format  : printf("..."), fprintf(stderr, "..."), std::cerr <<,
//                   spdlog::<level>("..."), LOG(...) << "..." (glog).
//                   Conservative: only captures the literal first arg of
//                   printf-family calls and the explicit spdlog/LOG forms.
//   - sql         : Quoted string literals whose first non-whitespace
//                   token is a SQL verb.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("c-cpp", sniffTemplatePatternsCCPP)
}

// cppI18nRe matches gettext("..."), _("..."), N_("..."). Group 1 = literal.
var cppI18nRe = regexp.MustCompile(
	`\b(?:gettext|_|N_|dgettext|ngettext)\s*\(\s*"([^"]+)"`,
)

// cppPrintfRe matches printf / fprintf / sprintf / snprintf with a literal
// first format arg (after the optional stream arg). Group 1 = literal.
var cppPrintfRe = regexp.MustCompile(
	`\b(?:printf|fprintf|sprintf|snprintf|vprintf|vfprintf|puts|fputs)\s*\(\s*(?:[A-Za-z_][\w]*\s*,\s*)?"([^"]+)"`,
)

// cppSpdlogRe matches spdlog::<level>("..."). Group 1 = level, Group 2 = literal.
var cppSpdlogRe = regexp.MustCompile(
	`\bspdlog\s*::\s*([a-z]+)\s*\(\s*"([^"]+)"`,
)

// cppGlogRe matches LOG(INFO) << "..." / LOG(ERROR) << "...".
// Group 1 = level, Group 2 = literal.
var cppGlogRe = regexp.MustCompile(
	`\bLOG\s*\(\s*([A-Z]+)\s*\)\s*<<\s*"([^"]+)"`,
)

// cppSQLRe matches a quoted literal whose first word is a SQL verb.
var cppSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsCCPP(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanCCPPFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range cppI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "gettext",
		})
	}
	for _, m := range cppPrintfRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "printf",
		})
	}
	for _, m := range cppSpdlogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[4]:m[5]]),
			Tag:      "spdlog." + content[m[2]:m[3]],
		})
	}
	for _, m := range cppGlogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[4]:m[5]]),
			Tag:      "LOG." + content[m[2]:m[3]],
		})
	}
	for _, m := range cppSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
