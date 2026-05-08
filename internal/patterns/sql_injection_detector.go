package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// sqlInjectionDetector flags SQL queries built with string interpolation.
// Matches Python sql_injection_detector.py.
type sqlInjectionDetector struct{}

var (
	// f-string: capture the body between the quotes (single or double)
	sqlFStringSingleRE  = regexp.MustCompile(`f'((?:[^'\\{]|\\.)*\{[^}]+\}(?:[^'\\]|\\.)*)'`)
	sqlFStringDoubleRE  = regexp.MustCompile(`f"((?:[^"\\{]|\\.)*\{[^}]+\}(?:[^"\\]|\\.)*)"`)
	sqlFormatStrRE      = regexp.MustCompile(`(['"](?:[^'"\\]|\\.)*?['"])\s*\.format\s*\(`)
	sqlPercentStrRE     = regexp.MustCompile(`(['"](?:[^'"\\]|\\.)*?['"])\s*%\s*[A-Za-z_({\[]`)
	sqlConcatStrIdentRE = regexp.MustCompile(`(['"](?:[^'"\\]|\\.)*?['"])\s*\+\s*[A-Za-z_]\w*`)
	sqlIdentConcatStrRE = regexp.MustCompile(`[A-Za-z_]\w*\s*\+\s*(['"](?:[^'"\\]|\\.)*?['"])`)
	sqlParamExecRE      = regexp.MustCompile(`(?i)(?:cursor|conn|db|session)\s*\.\s*execute\s*\([^,)]+,\s*[^)]+\)`)
	sqlFStringTriggerRE = regexp.MustCompile(`f['"](?:[^'"\\]|\\.)*\{[^}]+\}(?:[^'"\\]|\\.)*['"]`)
	sqlFormatTriggerRE  = regexp.MustCompile(`['"](?:[^'"\\]|\\.)*['"]\s*\.format\s*\(`)
	sqlPercentTriggerRE = regexp.MustCompile(`['"](?:[^'"\\]|\\.)*['"]\s*%\s*\S`)
	sqlConcatTriggerRE  = regexp.MustCompile(`(?:['"](?:[^'"\\]|\\.)*['"]\s*\+\s*[A-Za-z_]\w*|[A-Za-z_]\w*\s*\+\s*['"](?:[^'"\\]|\\.)*['"])`)
)

var sqlKeywords = map[string]bool{
	"select": true, "insert": true, "update": true, "delete": true, "drop": true, "create": true,
}

func isSQLString(text string) bool {
	stripped := strings.TrimLeft(text, " \t\n\r'\"")
	if stripped == "" {
		return false
	}
	parts := strings.Fields(stripped)
	if len(parts) == 0 {
		return false
	}
	return sqlKeywords[strings.ToLower(parts[0])]
}

func (s *sqlInjectionDetector) Category() string { return "sql_injection_risk" }

func (s *sqlInjectionDetector) AppliesTo(src string) bool {
	return sqlFStringTriggerRE.MatchString(src) ||
		sqlFormatTriggerRE.MatchString(src) ||
		sqlPercentTriggerRE.MatchString(src) ||
		sqlConcatTriggerRE.MatchString(src)
}

func (s *sqlInjectionDetector) Detect(filePath, language, src string) []types.EntityRecord {
	if src == "" {
		return nil
	}
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Build set of parameterized-execute line numbers to suppress false positives.
	paramLines := map[int]bool{}
	for _, m := range sqlParamExecRE.FindAllStringIndex(src, -1) {
		paramLines[lineOf(src, m[0])] = true
	}

	emit := func(riskLevel, text string, offset int) {
		if !isSQLString(text) {
			return
		}
		line := lineOf(src, offset)
		if paramLines[line] {
			return
		}
		key := fmt.Sprintf("%s:%d", filePath, line)
		if seen[key] {
			return
		}
		seen[key] = true
		snippet := text
		if len(snippet) > 120 {
			snippet = snippet[:120]
		}
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("sql_injection_risk@%s:%d", filePath, line),
			"SCOPE.Pattern", "sql_injection_risk", language, line,
			map[string]string{
				"kind":          "sql_injection_risk",
				"risk_level":    riskLevel,
				"query_snippet": snippet,
			}))
	}

	// High: f-strings (single-quoted)
	for _, m := range sqlFStringSingleRE.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			emit("high", src[m[2]:m[3]], m[0])
		}
	}
	// High: f-strings (double-quoted)
	for _, m := range sqlFStringDoubleRE.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			emit("high", src[m[2]:m[3]], m[0])
		}
	}

	// High: .format()
	for _, m := range sqlFormatStrRE.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			lit := strings.Trim(src[m[2]:m[3]], "'\"")
			emit("high", lit, m[0])
		}
	}

	// High: string + identifier
	for _, m := range sqlConcatStrIdentRE.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			lit := strings.Trim(src[m[2]:m[3]], "'\"")
			emit("high", lit, m[0])
		}
	}

	// High: identifier + string
	for _, m := range sqlIdentConcatStrRE.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			lit := strings.Trim(src[m[2]:m[3]], "'\"")
			emit("high", lit, m[0])
		}
	}

	// Medium: % formatting
	for _, m := range sqlPercentStrRE.FindAllStringSubmatchIndex(src, -1) {
		if m[2] >= 0 {
			lit := strings.Trim(src[m[2]:m[3]], "'\"")
			emit("medium", lit, m[0])
		}
	}

	return results
}

func init() {
	Register(&sqlInjectionDetector{})
}
