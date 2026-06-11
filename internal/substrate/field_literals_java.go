// Java/Spring field-literal analyzer (#4728, follow-up to #4669) — Map.of(...)
// response maps & Jackson ObjectNode.put(...) chains.
//
// Target: Spring controller methods that build a response body — `return
// ResponseEntity.ok(Map.of("k", literal, ...))`, a bare `Map.of("k", literal)`,
// or a Jackson `ObjectNode` built with chained `.put("k", literal)` calls. Both
// forms key a field name to a value expression positionally (not via a brace
// `key: value`), so they need bespoke argument parsing rather than the shared
// brace splitter:
//
//   - Map.of("k1", v1, "k2", v2, ...) — even args are string keys, odd args are
//     the corresponding values. We balance-scan the call's argument list and
//     pair them up.
//   - node.put("k", v) — each call contributes one (key, value) facet.
//
// A value that is a bare literal (number / quoted string / true|false|null) is
// literal-bound; a value naming a variable, method call, field access
// (item.getName()), or expression is derived. Builder DTOs and POJO setters are
// intentionally NOT classified here (their key→value binding is not statically
// recoverable from a single window without type info) — honest-partial:
// unrecognised constructs simply produce no facet.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("java", analyzeFieldLiteralsJava) }

var (
	// javaMapOfTriggerRe finds the start of a `Map.of(` / `Map.ofEntries(`-less
	// immutable-map constructor's argument list `(`. We balance-scan from `(`.
	javaMapOfTriggerRe = regexp.MustCompile(`\bMap\s*\.\s*of\s*\(`)
	// javaPutCallRe matches a single Jackson `.put("k", rhs)` call, capturing the
	// key and the value-arg text (up to the call's closing paren — value args
	// here are simple, no nested parens needed for the common literal case; a
	// nested-call value is captured greedily-minimally and classified derived).
	javaPutCallRe = regexp.MustCompile(`\.\s*put\s*\(\s*"([A-Za-z_][\w]*)"\s*,\s*([^;]*?)\s*\)`)
)

func analyzeFieldLiteralsJava(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "java")
	var out []FieldFacet
	out = append(out, javaMapOfFacets(src, startLine)...)
	out = append(out, javaPutFacets(src, startLine)...)
	return out
}

// javaMapOfFacets parses every `Map.of(...)` call's argument list into
// (key, value) pairs and classifies each value.
func javaMapOfFacets(src string, startLine int) []FieldFacet {
	var out []FieldFacet
	idx := 0
	for {
		loc := javaMapOfTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `(`
		args, closeIdx := balancedParen(src, open)
		if closeIdx < 0 {
			break
		}
		openLine := startLine + strings.Count(src[:open], "\n")
		entries := topLevelParenArgs(args)
		// Pair even (key) / odd (value) arguments.
		for i := 0; i+1 < len(entries); i += 2 {
			keyText := strings.TrimSpace(entries[i].text)
			if len(keyText) < 2 || keyText[0] != '"' {
				continue // key must be a string literal to name a field
			}
			field := strings.Trim(keyText, `"`)
			if field == "" || !isJavaIdent(field) {
				continue
			}
			rhs := entries[i+1].text
			binding, lit := classifyFieldRHS(rhs)
			line := openLine + strings.Count(args[:entries[i].offset], "\n")
			ff := FieldFacet{Field: field, Binding: binding, Line: line}
			if binding == BindingLiteral {
				ff.LiteralValue = lit
				ff.Envelope = isEnvelopeField(field, lit)
			}
			out = append(out, ff)
		}
		idx = closeIdx + 1
	}
	return out
}

// javaPutFacets classifies each Jackson `.put("k", v)` call as one facet.
func javaPutFacets(src string, startLine int) []FieldFacet {
	var out []FieldFacet
	for _, m := range javaPutCallRe.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		rhs := src[m[4]:m[5]]
		binding, lit := classifyFieldRHS(rhs)
		line := startLine + strings.Count(src[:m[0]], "\n")
		ff := FieldFacet{Field: field, Binding: binding, Line: line}
		if binding == BindingLiteral {
			ff.LiteralValue = lit
			ff.Envelope = isEnvelopeField(field, lit)
		}
		out = append(out, ff)
	}
	return out
}

func isJavaIdent(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// topLevelParenArgs splits a call's argument-list body (text strictly between
// the outer parens) into its TOP-LEVEL comma-separated arguments, ignoring
// commas nested inside (), {}, [], or string literals. Each argument keeps its
// offset within the body. The paren analogue of topLevelDictEntries.
func topLevelParenArgs(body string) []dictEntry {
	var out []dictEntry
	depth := 0
	start := 0
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]
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
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, dictEntry{text: body[start:i], offset: start})
				start = i + 1
			}
		}
	}
	if start < len(body) {
		out = append(out, dictEntry{text: body[start:], offset: start})
	}
	return out
}

// balancedParen returns the substring strictly BETWEEN the paren at openIdx and
// its matching close paren, and the index of the close paren. String literals
// are skipped. closeIdx is -1 when no match. The paren analogue of
// balancedBrace, used by the Java Map.of analyzer.
func balancedParen(s string, openIdx int) (string, int) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '(' {
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
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], i
			}
		}
	}
	return "", -1
}
