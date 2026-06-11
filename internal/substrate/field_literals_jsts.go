// JS/TS field-literal analyzer (#4669) — NestJS response object literals.
//
// Flagship target: NestJS controller/service methods that build a response
// object literal — `return { ... }`, `return res.json({ ... })`, or an
// assignment `const dto = { ... }` later returned. We locate each object
// literal and classify every top-level property: a value that is a bare literal
// (number / quoted string / non-interpolated template / true|false|null|
// undefined) is literal-bound; a property naming an identifier, call,
// member-access (item.name), or shorthand (`{ name }`) is derived.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("jsts", analyzeFieldLiteralsJSTS) }

var (
	// jstsObjTriggerRe finds the start of a response object literal: a `{`
	// following `return `, `res.json(` / `.send(` / `.status(n).json(`, or an
	// assignment `= {`. We then balance-scan from the `{`.
	jstsObjTriggerRe = regexp.MustCompile(
		`(?:\breturn\b\s*|` +
			`\.(?:json|send)\s*\(\s*|` +
			`=\s*)\{`,
	)
	// jstsPropKeyRe matches an object property header `key:` / `"key":` /
	// `'key':` capturing the key and RHS. Shorthand (`name` with no colon) does
	// not match here and is handled separately as derived.
	jstsPropKeyRe = regexp.MustCompile(`^\s*(?:['"]?)([A-Za-z_$][\w$]*)(?:['"]?)\s*:\s*(.*)$`)
	// jstsShorthandRe matches a shorthand property `name` (bare identifier, no
	// colon) — always derived (it aliases a variable).
	jstsShorthandRe = regexp.MustCompile(`^\s*([A-Za-z_$][\w$]*)\s*$`)
)

func analyzeFieldLiteralsJSTS(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "jsts")
	var out []FieldFacet
	idx := 0
	for {
		loc := jstsObjTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `{`
		body, closeIdx := balancedBrace(src, open)
		if closeIdx < 0 {
			break
		}
		openLine := startLine + strings.Count(src[:open], "\n")
		out = append(out, classifyJSTSObjectFields(body, openLine)...)
		idx = closeIdx + 1
	}
	return out
}

func classifyJSTSObjectFields(objBody string, openLine int) []FieldFacet {
	var out []FieldFacet
	for _, seg := range topLevelDictEntries(objBody) {
		// Shorthand `{ name }` → derived, no facet value to flag.
		if m := jstsShorthandRe.FindStringSubmatch(seg.text); m != nil {
			line := openLine + strings.Count(objBody[:seg.offset], "\n")
			out = append(out, FieldFacet{Field: m[1], Binding: BindingDerived, Line: line})
			continue
		}
		// Spread (`...rest`) carries no single field name — skip.
		if strings.HasPrefix(strings.TrimSpace(seg.text), "...") {
			continue
		}
		m := jstsPropKeyRe.FindStringSubmatch(seg.text)
		if m == nil {
			continue
		}
		field := m[1]
		rhs := m[2]
		binding, lit := classifyFieldRHS(rhs)
		line := openLine + strings.Count(objBody[:seg.offset], "\n")
		ff := FieldFacet{Field: field, Binding: binding, Line: line}
		if binding == BindingLiteral {
			ff.LiteralValue = lit
			ff.Envelope = isEnvelopeField(field, lit)
		}
		out = append(out, ff)
	}
	return out
}
