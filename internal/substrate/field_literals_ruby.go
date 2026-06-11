// Ruby/Rails field-literal analyzer (#4728, follow-up to #4669) — render json /
// hash literals.
//
// Target: Rails controller actions that render a response hash — `render json:
// { ... }`, `render json: { ... }, status: :ok`, or a returned hash literal
// `{ ... }`. We locate each brace hash literal and classify every top-level
// entry. Ruby hash keys come in three syntaxes:
//
//   - symbol shorthand:  `k: value`
//   - hash-rocket symbol: `:k => value`
//   - hash-rocket string: `"k" => value`
//
// A value that is a bare literal (number / quoted string / true|false|nil) is
// literal-bound; a value naming a variable, method call, attribute (item.name),
// index, symbol (`:active`), or interpolation is derived.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("ruby", analyzeFieldLiteralsRuby) }

var (
	// rubyHashTriggerRe finds the start of a response hash literal: a `{`
	// following `render json:` / `render :json =>`, a bare `render` arg, or an
	// assignment / return `= {` / `json.merge!({`. The most general trigger is
	// `json:`/`json =>` immediately preceding the `{`, plus a `=`/`return`/`(`
	// brace. We balance-scan from `{`.
	rubyHashTriggerRe = regexp.MustCompile(
		`(?:\bjson\s*:\s*|` + // render json: {
			`:json\s*=>\s*|` + // render :json => {
			`\breturn\b\s*|` + // return {
			`=\s*|` + // x = {
			`\(\s*` + // render({  / foo({
			`)\{`,
	)
	// rubySymbolKeyRe matches `k: rhs` (symbol shorthand).
	rubySymbolKeyRe = regexp.MustCompile(`^\s*([A-Za-z_][\w]*)\s*:\s*(.*)$`)
	// rubyRocketSymKeyRe matches `:k => rhs`.
	rubyRocketSymKeyRe = regexp.MustCompile(`^\s*:([A-Za-z_][\w]*)\s*=>\s*(.*)$`)
	// rubyRocketStrKeyRe matches `"k" => rhs` / `'k' => rhs`.
	rubyRocketStrKeyRe = regexp.MustCompile(`^\s*['"]([A-Za-z_][\w]*)['"]\s*=>\s*(.*)$`)
)

func analyzeFieldLiteralsRuby(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "ruby")
	var out []FieldFacet
	idx := 0
	for {
		loc := rubyHashTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `{`
		body, closeIdx := balancedBrace(src, open)
		if closeIdx < 0 {
			break
		}
		openLine := startLine + strings.Count(src[:open], "\n")
		out = append(out, classifyRubyHashFields(body, openLine)...)
		idx = closeIdx + 1
	}
	return out
}

func classifyRubyHashFields(body string, openLine int) []FieldFacet {
	var out []FieldFacet
	for _, seg := range topLevelDictEntries(body) {
		var field, rhs string
		if m := rubyRocketSymKeyRe.FindStringSubmatch(seg.text); m != nil {
			field, rhs = m[1], m[2]
		} else if m := rubyRocketStrKeyRe.FindStringSubmatch(seg.text); m != nil {
			field, rhs = m[1], m[2]
		} else if m := rubySymbolKeyRe.FindStringSubmatch(seg.text); m != nil {
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
