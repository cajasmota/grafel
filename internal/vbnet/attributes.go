package vbnet

import "strings"

// SplitAttributes removes attribute groups from a logical line and returns
// their bodies (without the angle brackets) in source order, plus the code
// with those groups removed.
//
// Two positions are recognised, and only two, because they are the only two
// where '<' cannot be a less-than operator:
//
//	<Serializable()> Public Class X      ' leading, may repeat
//	Sub F(<Out> ByRef n As Integer)      ' immediately after '(' or ','
//
// Attribute bodies nest parentheses (<Attr(GetType(List(Of T)))>) and may
// contain string literals holding '>' — both are tracked, so the terminating
// '>' is the one at paren depth zero outside a literal.
func SplitAttributes(code string) (attrs []string, rest string) {
	var out strings.Builder
	i := 0

	// Leading groups: skip whitespace, then consume every adjacent <...>.
	for {
		j := i
		for j < len(code) && (code[j] == ' ' || code[j] == '\t') {
			j++
		}
		if j >= len(code) || code[j] != '<' {
			break
		}
		end, ok := attributeEnd(code, j)
		if !ok {
			break
		}
		attrs = append(attrs, strings.TrimSpace(code[j+1:end]))
		i = end + 1
	}

	// Inline groups: '<' that directly follows '(' or ',' (whitespace aside).
	for i < len(code) {
		c := code[i]
		if c == '"' {
			// Copy the literal verbatim; '<' inside it is data.
			end := literalEnd(code, i)
			out.WriteString(code[i:end])
			i = end
			continue
		}
		if c == '(' || c == ',' {
			out.WriteByte(c)
			i++
			j := i
			for j < len(code) && (code[j] == ' ' || code[j] == '\t') {
				j++
			}
			for j < len(code) && code[j] == '<' {
				end, ok := attributeEnd(code, j)
				if !ok {
					break
				}
				attrs = append(attrs, strings.TrimSpace(code[j+1:end]))
				// Consume the group but not the whitespace around it, so that
				// removing an attribute never joins two tokens together.
				i = end + 1
				j = i
				for j < len(code) && (code[j] == ' ' || code[j] == '\t') {
					j++
				}
			}
			continue
		}
		out.WriteByte(c)
		i++
	}
	return attrs, strings.TrimSpace(out.String())
}

// attributeEnd returns the index of the '>' closing the attribute group that
// opens at code[start], which must be '<'.
func attributeEnd(code string, start int) (int, bool) {
	depth := 0
	for i := start + 1; i < len(code); i++ {
		switch code[i] {
		case '"':
			i = literalEnd(code, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '>':
			if depth <= 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// literalEnd returns the index one past the string literal beginning at
// code[start], which must be '"'. An unterminated literal ends at the line
// end. "" inside the literal is an escaped quote.
func literalEnd(code string, start int) int {
	i := start + 1
	for i < len(code) {
		if code[i] != '"' {
			i++
			continue
		}
		if i+1 < len(code) && code[i+1] == '"' {
			i += 2
			continue
		}
		return i + 1
	}
	return len(code)
}
