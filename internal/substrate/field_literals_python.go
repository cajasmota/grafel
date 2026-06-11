// Python field-literal analyzer (#4669) — DRF Response dicts & dict literals.
//
// Flagship target: Django/DRF handlers that construct a response payload as a
// dict literal — `return Response({...})`, `return JsonResponse({...})`,
// `return Response(data)` where `data = {...}`, or a bare `return {...}`. We
// locate each such dict literal in the function body and classify every
// top-level `key: value` pair: a value that is a bare literal (number / string
// / bool / None) is literal-bound; anything that names a variable, call,
// attribute (item.name), subscript, or expression is derived.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterFieldLiteralAnalyzer("python", analyzeFieldLiteralsPython) }

var (
	// pyResponseDictOpenRe finds the start of a response dict literal. Matches a
	// `{` that follows `return `, a `Response(`/`JsonResponse(`/`Response(data=`
	// wrapper, or an assignment `<name> = {` / `data = {`. We then balance-scan
	// from that `{`.
	pyRespDictTriggerRe = regexp.MustCompile(
		`(?:\breturn\b\s*|` +
			`\b(?:Response|JsonResponse|JSONResponse)\s*\(\s*(?:data\s*=\s*)?|` +
			`\b\w+\s*=\s*)\{`,
	)
	// pyFieldKeyRe matches a dict entry header `"key":` or `'key':` at the start
	// of a (trimmed) line, capturing the key name and the remaining RHS text.
	pyFieldKeyRe = regexp.MustCompile(`^\s*['"]([A-Za-z_][\w]*)['"]\s*:\s*(.*)$`)
)

func analyzeFieldLiteralsPython(funcSource string, startLine int) []FieldFacet {
	if strings.TrimSpace(funcSource) == "" {
		return nil
	}
	src := ClampToFunctionBody(funcSource, "python")
	var out []FieldFacet
	// Scan for every response dict literal in the body; classify each one's
	// top-level fields. Multiple dicts (e.g. several return branches) all
	// contribute facets; PartialStubFields composes them safely.
	idx := 0
	for {
		loc := pyRespDictTriggerRe.FindStringIndex(src[idx:])
		if loc == nil {
			break
		}
		open := idx + loc[1] - 1 // position of the `{`
		body, closeIdx := balancedBrace(src, open)
		if closeIdx < 0 {
			break
		}
		// Line of the opening brace, absolute.
		openLine := startLine + strings.Count(src[:open], "\n")
		out = append(out, classifyPyDictFields(body, src, open, openLine)...)
		idx = closeIdx + 1
	}
	return out
}

// classifyPyDictFields parses the TOP-LEVEL `key: value` pairs of a dict body
// (the text between the outer braces, exclusive) and classifies each. Nested
// braces/brackets/parens are skipped so an inner dict's keys aren't mistaken
// for top-level fields. fullSrc/open are used only to compute absolute lines.
func classifyPyDictFields(dictBody, fullSrc string, open, openLine int) []FieldFacet {
	var out []FieldFacet
	for _, seg := range topLevelDictEntries(dictBody) {
		m := pyFieldKeyRe.FindStringSubmatch(seg.text)
		if m == nil {
			continue
		}
		field := m[1]
		rhs := m[2]
		binding, lit := classifyFieldRHS(rhs)
		line := openLine + strings.Count(dictBody[:seg.offset], "\n")
		ff := FieldFacet{Field: field, Binding: binding, Line: line}
		if binding == BindingLiteral {
			ff.LiteralValue = lit
			ff.Envelope = isEnvelopeField(field, lit)
		}
		out = append(out, ff)
	}
	return out
}

// dictEntry is one top-level comma-separated entry of a dict/object body.
type dictEntry struct {
	text   string
	offset int // byte offset of the entry within the dict body
}

// topLevelDictEntries splits a dict/object body (text between the outer braces)
// into its TOP-LEVEL comma-separated entries, ignoring commas nested inside
// {}, [], (), or string literals. Each returned entry keeps its offset within
// the body so callers can recover absolute line numbers. Reused by the JS/TS
// analyzer (object literals split identically).
func topLevelDictEntries(body string) []dictEntry {
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
		case '"', '\'', '`':
			quote = c
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
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

// balancedBrace returns the substring strictly BETWEEN the brace at openIdx and
// its matching close brace, and the index of the close brace. String literals
// are skipped so braces inside strings don't unbalance the scan. closeIdx is -1
// when no match (truncated window).
func balancedBrace(s string, openIdx int) (string, int) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '{' {
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
		case '"', '\'', '`':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], i
			}
		}
	}
	return "", -1
}
