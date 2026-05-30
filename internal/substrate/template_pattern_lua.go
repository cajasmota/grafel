// Lua template-pattern sniffer (Phase 3D).
//
// Recognises:
//   - i18n   : no standard i18n library in OpenResty/Lapis ecosystem;
//     however lapis-i18n / i18n.lua patterns:
//     i18n.translate("key") / i18n("key") / I18n.t("key")
//   - log_format : ngx.log(ngx.ERR, "literal") / ngx.log(level, "fmt...")
//     logger.info("literal") / print("literal")
//   - sql    : string literals whose content starts with a SQL verb keyword
//     (SELECT/INSERT/UPDATE/DELETE/WITH/CREATE TABLE/etc.)
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("lua", sniffTemplatePatternsLua)
}

// luaI18nRe matches lapis-i18n / i18n.lua translation calls.
// Group 1 = the literal key.
var luaI18nRe = regexp.MustCompile(
	`\b(?:i18n\.translate|i18n|I18n\.t)\s*\(\s*["']([^"']+)["']`,
)

// luaLogLiteralRe matches ngx.log(level, "literal") — captures group 2 = literal.
var luaLogLiteralRe = regexp.MustCompile(
	`\bngx\.log\s*\(\s*(?:ngx\.\w+|[0-9]+)\s*,\s*["']([^"'\n]{1,240})["']`,
)

// luaPrintLiteralRe matches print("literal") / io.write("literal").
// Group 1 = literal.
var luaPrintLiteralRe = regexp.MustCompile(
	`\b(?:print|io\.write)\s*\(\s*["']([^"'\n]{1,240})["']`,
)

// luaSQLRe matches a quoted literal whose first word is a SQL verb.
var luaSQLRe = regexp.MustCompile(
	`["'](\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[\s\S]*?)["']`,
)

// luaTplFuncRe is the same header scanner used in def_use_lua.go but
// defined separately to avoid cross-file init ordering issues.
var luaTplFuncRe = regexp.MustCompile(
	`(?m)^\s*(?:local\s+)?function\s+(?:[A-Za-z_][\w]*[.:])*([A-Za-z_][\w]*)\s*\(`,
)

func sniffTemplatePatternsLua(content string) []TemplatePattern {
	if content == "" {
		return nil
	}

	// Build lightweight header list for function attribution.
	type tplHeader struct {
		Line int
		Name string
	}
	var headers []tplHeader
	for _, m := range luaTplFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		headers = append(headers, tplHeader{
			Line: lineOfOffset(content, m[0]),
			Name: content[m[2]:m[3]],
		})
	}
	nearestFn := func(line int) string {
		best := ""
		for _, h := range headers {
			if h.Line <= line {
				best = h.Name
			}
		}
		return best
	}

	var out []TemplatePattern

	// --- i18n ---
	for _, m := range luaI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		out = append(out, TemplatePattern{
			Function: nearestFn(line),
			Line:     line,
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "i18n()",
		})
	}

	// --- log_format ---
	for _, m := range luaLogLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		out = append(out, TemplatePattern{
			Function: nearestFn(line),
			Line:     line,
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "ngx.log",
		})
	}
	for _, m := range luaPrintLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		out = append(out, TemplatePattern{
			Function: nearestFn(line),
			Line:     line,
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "print",
		})
	}

	// --- sql ---
	for _, m := range luaSQLRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		out = append(out, TemplatePattern{
			Function: nearestFn(line),
			Line:     line,
			Kind:     TemplateKindSQL,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "sql",
		})
	}

	return out
}
