// C#/.NET field-literal analyzer (#4728, follow-up to #4669) — anonymous-object
// / object-initializer response literals.
//
// Target: ASP.NET Core controller actions that return a response object —
// `return Ok(new { k = literal })`, `return Ok(new XResponse { Field = literal
// })`, or a returned object-initializer / record-init `new FooDto { Field =
// literal }`. C# object initializers and anonymous objects are brace-delimited
// with `=` member assignments and bare-identifier member names. We locate each
// `new ... { ... }` brace literal and classify every `Field = value` entry. A
// value that is a bare literal (number / quoted string / true|false|null) is
// literal-bound; a value naming a variable, method call, property access
// (item.Name), index, or expression is derived.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("csharp", analyzeFieldLiteralsCSharp) }

var (
	// csharpObjTriggerRe finds the start of an object/anonymous-object literal:
	// a `{` following `new` (optionally with a type name) — `new {`, `new Foo {`,
	// `new Foo() {`, `new Foo<T> {`. We then balance-scan from the `{`.
	csharpObjTriggerRe = regexp.MustCompile(
		`\bnew\b\s*(?:[A-Za-z_][\w\.]*\s*(?:<[^>{]*>)?\s*(?:\([^){]*\))?\s*)?\{`,
	)
	// csharpMemberRe matches an initializer entry `Field = rhs` (bare-identifier
	// member name, `=` separator). Distinguished from `==` by requiring a single
	// `=` not followed by another `=`.
	csharpMemberRe = regexp.MustCompile(`^\s*([A-Za-z_][\w]*)\s*=\s*([^=].*|)$`)
)

func analyzeFieldLiteralsCSharp(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "csharp")
	var out []FieldFacet
	idx := 0
	for {
		loc := csharpObjTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `{`
		body, closeIdx := balancedBrace(src, open)
		if closeIdx < 0 {
			break
		}
		openLine := startLine + strings.Count(src[:open], "\n")
		out = append(out, classifyCSharpObjectFields(body, openLine)...)
		idx = closeIdx + 1
	}
	return out
}

func classifyCSharpObjectFields(body string, openLine int) []FieldFacet {
	var out []FieldFacet
	for _, seg := range topLevelDictEntries(body) {
		m := csharpMemberRe.FindStringSubmatch(seg.text)
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
