// C# template-pattern sniffer (#2775 Phase 3D T2).
//
// Recognises:
//   - i18n        : Localizer["key"], _localizer["key"], T("key"),
//                   ResourceManager.GetString("key").
//   - log_format  : _logger.Log<Level>("..."), logger.Information("..."),
//                   Console.WriteLine("..."). Captures literal first arg.
//   - sql         : Quoted/verbatim string literals whose first non-
//                   whitespace token is a SQL verb (case-insensitive).
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("csharp", sniffTemplatePatternsCSharp)
}

// csharpI18nRe matches Localizer["..."], _localizer["..."], T("..."),
// GetString("..."). Group 1 = the bare literal payload.
var csharpI18nRe = regexp.MustCompile(
	`\b(?:[Ll]ocalizer|_localizer|T|GetString)\s*[\["]\s*"?([^"\]]+)"?\s*[\]\)]`,
)

// csharpLogRe matches logger.<Level>("...") / _logger.<Level>("...") /
// Log.<Level>("...") / Console.<Method>("..."). Group 1 = level, Group 2 = literal.
var csharpLogRe = regexp.MustCompile(
	`\b(?:_?[Ll]ogger|Log|Console)\s*\.\s*([A-Za-z]+)\s*\(\s*"([^"]+)"`,
)

// csharpSQLRe matches quoted (and verbatim @"...") string literals whose
// first non-whitespace token is a SQL verb.
var csharpSQLRe = regexp.MustCompile(
	`@?"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsCSharp(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanCSharpFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range csharpI18nRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "Localizer",
		})
	}
	for _, m := range csharpLogRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range csharpSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
