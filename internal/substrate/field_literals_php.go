// PHP/Laravel field-literal analyzer (#4728, follow-up to #4669) — response()
// ->json([...]) / array literals.
//
// Target: Laravel controller actions that return a response array — `return
// response()->json([...])`, `return response()->json([...], 200)`, or a bare
// returned array `return [...]`. PHP arrays are bracket-delimited (`[ ... ]`)
// with `=>` key/value separators. We locate each top-level array literal and
// classify every `'key' => value` entry. A value that is a bare literal
// (number / quoted string / true|false|null) is literal-bound; a value naming a
// variable (`$x`), method/function call, property access (`$item->name`),
// constant, or index is derived.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("php", analyzeFieldLiteralsPHP) }

var (
	// phpArrayTriggerRe finds the start of a response array literal: a `[`
	// following `json(` / `->json(`, a `return`, an `=`, or a `(`. We then
	// balance-scan from the `[`.
	phpArrayTriggerRe = regexp.MustCompile(
		`(?:\bjson\s*\(\s*|` + // ->json([
			`\breturn\b\s*|` + // return [
			`=\s*|` + // $x = [
			`\(\s*` + // foo([
			`)\[`,
	)
	// phpArrayKeyRe matches an array entry `'key' => rhs` / `"key" => rhs`.
	phpArrayKeyRe = regexp.MustCompile(`^\s*['"]([A-Za-z_][\w]*)['"]\s*=>\s*(.*)$`)
)

func analyzeFieldLiteralsPHP(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "php")
	var out []FieldFacet
	idx := 0
	for {
		loc := phpArrayTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `[`
		body, closeIdx := balancedBracket(src, open)
		if closeIdx < 0 {
			break
		}
		openLine := startLine + strings.Count(src[:open], "\n")
		out = append(out, classifyPHPArrayFields(body, openLine)...)
		idx = closeIdx + 1
	}
	return out
}

func classifyPHPArrayFields(body string, openLine int) []FieldFacet {
	var out []FieldFacet
	for _, seg := range topLevelDictEntries(body) {
		m := phpArrayKeyRe.FindStringSubmatch(seg.text)
		if m == nil {
			continue
		}
		field, rhs := m[1], m[2]
		binding, lit := classifyFieldRHS(rhs)
		line := openLine + strings.Count(body[:seg.offset], "\n")
		ff := FieldFacet{Field: field, Binding: binding, Line: line}
		if binding == BindingLiteral {
			ff.LiteralValue = lit
			ff.Envelope = isEnvelopeField(field, lit)
		}
		out = append(out, ff)
	}
	return out
}

// balancedBracket returns the substring strictly BETWEEN the bracket at openIdx
// and its matching close bracket, and the index of the close bracket. String
// literals are skipped so brackets inside strings don't unbalance the scan.
// closeIdx is -1 when no match (truncated window). The bracket analogue of
// balancedBrace, used by the PHP array-literal analyzer.
func balancedBracket(s string, openIdx int) (string, int) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '[' {
		return "", -1
	}
	depth := 0
	var quote byte
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], i
			}
		}
	}
	return "", -1
}
