// Go field-literal analyzer (#4728, follow-up to #4669) — gin.H / map / struct
// response literals.
//
// Target: Go HTTP handlers that construct a response payload as a composite
// literal — `c.JSON(200, gin.H{"k": v})`, `json.NewEncoder(w).Encode(map[string]
// any{"k": v})`, a bare `gin.H{...}` / `map[string]any{...}`, or a returned
// struct literal `FooResp{Field: 0}`. We locate each such brace literal and
// classify every top-level entry:
//
//   - map/gin.H form: `"k": value` (quoted-string key, colon, value).
//   - struct form:    `Field: value` (bare identifier key, colon, value).
//
// A value that is a bare literal (number / quoted string / true|false|nil) is
// literal-bound; a value naming an identifier, call, selector (item.Name), index
// expression, or composite is derived. Honest-partial: only constructs we can
// key+classify produce facets.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("go", analyzeFieldLiteralsGo) }

var (
	// goRespLitTriggerRe finds the start of a response composite literal: a `{`
	// following `gin.H`, `map[...]...`, a struct type name `FooResp`/`pkg.Foo`,
	// or a `Render`/`Encode`/`JSON` call's brace. We then balance-scan from `{`.
	// The leading `]` alternative catches `map[string]any{` (the `{` directly
	// after the closing `]` of the value-type-less map element type).
	goRespLitTriggerRe = regexp.MustCompile(
		`(?:\bgin\.H\s*|` + // gin.H{...}
			`\bmap\s*\[[^\]]*\][\w\.\*\[\]]*\s*|` + // map[K]V{...}
			`\b[A-Z][\w]*\s*|` + // exported struct type FooResp{...}
			`\b[a-z][\w]*\.[A-Z][\w]*\s*` + // pkg.FooResp{...}
			`)\{`,
	)
	// goMapKeyRe matches a map/gin.H entry `"key": rhs`.
	goMapKeyRe = regexp.MustCompile(`^\s*["` + "`" + `]([A-Za-z_][\w]*)["` + "`" + `]\s*:\s*(.*)$`)
	// goStructKeyRe matches a struct entry `Field: rhs` (bare-identifier key).
	goStructKeyRe = regexp.MustCompile(`^\s*([A-Za-z_][\w]*)\s*:\s*(.*)$`)
)

func analyzeFieldLiteralsGo(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "go")
	var out []FieldFacet
	idx := 0
	for {
		loc := goRespLitTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `{`
		body, closeIdx := balancedBrace(src, open)
		if closeIdx < 0 {
			break
		}
		openLine := startLine + strings.Count(src[:open], "\n")
		out = append(out, classifyGoLiteralFields(body, openLine)...)
		idx = closeIdx + 1
	}
	return out
}

func classifyGoLiteralFields(body string, openLine int) []FieldFacet {
	var out []FieldFacet
	for _, seg := range topLevelDictEntries(body) {
		var field, rhs string
		if m := goMapKeyRe.FindStringSubmatch(seg.text); m != nil {
			field, rhs = m[1], m[2]
		} else if m := goStructKeyRe.FindStringSubmatch(seg.text); m != nil {
			field, rhs = m[1], m[2]
		} else {
			continue
		}
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
