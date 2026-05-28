// Swift template-pattern sniffer (#2779 Phase 3D T3).
//
// Recognises:
//   - i18n       : NSLocalizedString("key", ...), String(localized: "key"),
//                  Bundle.main.localizedString(forKey: "key", ...),
//                  SwiftUI Text("key") (common pattern).
//   - log_format : print("..."), NSLog("..."),
//                  os.Logger().debug/info/warning/error("..."),
//                  Logger.<level>("...") (swift-log / OSLog).
//   - sql        : Quoted string literals whose first non-whitespace
//                  token is a SQL verb — used with GRDB / SQLite.swift
//                  raw query APIs.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("swift", sniffTemplatePatternsSwift)
}

// swiftI18nRe matches NSLocalizedString / String(localized:) / Text() i18n patterns.
// Group 1 = the key literal.
var swiftI18nRe = regexp.MustCompile(
	`\bNSLocalizedString\s*\(\s*"([^"]+)"` +
		`|\bString\s*\(\s*localized\s*:\s*"([^"]+)"` +
		`|\bBundle\.main\.localizedString\s*\(\s*forKey\s*:\s*"([^"]+)"`,
)

// swiftLogRe matches print / NSLog / os.Logger / swift-log calls.
// Group 1 = literal payload.
var swiftLogRe = regexp.MustCompile(
	`\b(?:print|NSLog|debugPrint)\s*\(\s*"([^"]+)"` +
		`|\b(?:logger|log|Logger)\s*\.\s*(?:debug|info|notice|warning|error|critical|fault|log)\s*\(\s*"([^"]+)"`,
)

// swiftSQLRe matches a quoted literal whose first word is a SQL verb.
// Group 1 = literal payload.
var swiftSQLRe = regexp.MustCompile(
	`"(\s*(?i:SELECT|INSERT|UPDATE|DELETE|REPLACE|MERGE|WITH|CREATE\s+TABLE|DROP\s+TABLE|ALTER\s+TABLE)[^"]*)"`,
)

func sniffTemplatePatternsSwift(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanSwiftFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range swiftI18nRe.FindAllStringSubmatchIndex(content, -1) {
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
			Kind:     TemplateKindI18n,
			Literal:  TruncateLiteral(lit),
			Tag:      "NSLocalizedString/String(localized:)",
		})
	}
	for _, m := range swiftLogRe.FindAllStringSubmatchIndex(content, -1) {
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
			Tag:      "print/NSLog/logger",
		})
	}
	for _, m := range swiftSQLRe.FindAllStringSubmatchIndex(content, -1) {
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
