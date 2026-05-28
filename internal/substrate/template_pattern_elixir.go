// Elixir template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : Gettext.gettext("key"), gettext("key"), dgettext("..."),
//                   pgettext("..."). Phoenix's gen_gettext idioms.
//   - log_format  : Logger.<level>("..."), IO.puts("..."), IO.inspect("...").
//   - sql         : Quoted string literals whose first word is a SQL verb;
//                   Ecto.Adapters.SQL.query!(repo, "SELECT ...").
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("elixir", sniffTemplatePatternsElixir)
}

// elixirI18nRe matches gettext("..."), dgettext("..."), Gettext.gettext("...").
// Group 1 = the bare literal payload.
var elixirI18nRe = regexp.MustCompile(
	`\b(?:Gettext\.gettext|gettext|dgettext|pgettext|ngettext)\s*\(\s*"([^"]+)"`,
)

// elixirLogRe matches Logger.<level>("..."). Group 1 = level, Group 2 = literal.
var elixirLogRe = regexp.MustCompile(
	`\bLogger\s*\.\s*([a-z_]+)\s*\(\s*"([^"]+)"`,
)

// elixirIORe matches IO.puts / IO.inspect / IO.write with a string literal.
var elixirIORe = regexp.MustCompile(
	`\bIO\s*\.\s*(?:puts|inspect|write)\s*\(\s*"([^"]+)"`,
)

// elixirSQLRe matches a quoted literal whose first word is a SQL verb.
var elixirSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsElixir(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanElixirFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range elixirI18nRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range elixirLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[4]:m[5]]),
			Tag:      "Logger." + content[m[2]:m[3]],
		})
	}
	for _, m := range elixirIORe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "IO.puts",
		})
	}
	for _, m := range elixirSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
