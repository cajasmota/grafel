// Dart template-pattern sniffer (#2779 Phase 3D T3).
//
// Recognises:
//   - i18n       : AppLocalizations.of(context).translate("key"),
//                  S.of(context).key (Flutter gen-l10n), intl.Intl.message("..."),
//                  tr("key") (easy_localization), AppLocalizations("key").
//   - log_format : print("..."), debugPrint("..."),
//                  Logger().d/i/w/e("..."), log("...").
//   - sql        : Quoted string literals whose first non-whitespace
//                  token is a SQL verb (case-insensitive), as used with
//                  sqflite / drift raw queries.
//
// No i18n standard is canonical in Dart; multiple popular packages are
// detected conservatively.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("dart", sniffTemplatePatternsdart)
}

// dartI18nRe matches common Dart/Flutter i18n patterns.
// Group 1 = the bare literal key.
var dartI18nRe = regexp.MustCompile(
	`\b(?:tr|gettext|translate|Intl\.message|AppLocalizations\.of\s*\([^)]*\)\s*\.\s*\w+)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`,
)

// dartLogRe matches print / debugPrint / log / Logger method calls.
// Group 1 = level or tag, Group 2 = literal payload.
var dartLogRe = regexp.MustCompile(
	`\b(?:print|debugPrint|log)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]` +
		`|\bLogger\s*\(\s*\)\s*\.\s*([a-z])\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`,
)

// dartSQLRe matches a quoted/raw literal whose first word is a SQL verb.
// Group 1 = literal payload.
var dartSQLRe = regexp.MustCompile(
	`['"` + "`" + `](\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[\s\S]*?)['"` + "`" + `]`,
)

func sniffTemplatePatternsdart(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanDartFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range dartI18nRe.FindAllStringSubmatchIndex(content, -1) {
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
	for _, m := range dartLogRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		// The first alternation captures: group 1 = literal (print form)
		// The second: group 3 = literal (Logger.x form)
		var lit string
		switch {
		case m[2] >= 0:
			lit = content[m[2]:m[3]]
		case len(m) >= 8 && m[6] >= 0:
			lit = content[m[6]:m[7]]
		}
		if lit == "" {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(lit),
			Tag:      "print/debugPrint/Logger",
		})
	}
	for _, m := range dartSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
