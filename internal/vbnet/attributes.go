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
	ranges := attributeRanges(code)
	if len(ranges) == 0 {
		return nil, strings.TrimSpace(code)
	}
	var out strings.Builder
	prev := 0
	for _, r := range ranges {
		attrs = append(attrs, strings.TrimSpace(code[r.start+1:r.end]))
		seg := code[prev:r.start]
		// Drop the whitespace an inline group was separated from its '(' or
		// ',' by, so removing `<Out>` from `F(a, <Out> ByRef n)` leaves one
		// space rather than two. The space AFTER the group is kept, which is
		// what stops the removal from ever joining two tokens.
		if t := strings.TrimRight(seg, " \t"); t != seg &&
			(strings.HasSuffix(t, "(") || strings.HasSuffix(t, ",")) {
			seg = t
		}
		out.WriteString(seg)
		prev = r.end + 1
	}
	out.WriteString(code[prev:])
	return attrs, strings.TrimSpace(out.String())
}

// BlankAttributes replaces every attribute group with spaces, preserving byte
// length so offsets into the result still index the input.
//
// SplitAttributes removes the groups, which shifts every later offset; the
// parser needs them gone *and* needs a use site's offset to still map back to
// the physical line it came from, which is what this preserves.
func BlankAttributes(code string) string {
	ranges := attributeRanges(code)
	if len(ranges) == 0 {
		return code
	}
	b := []byte(code)
	for _, r := range ranges {
		for i := r.start; i <= r.end && i < len(b); i++ {
			b[i] = ' '
		}
	}
	return string(b)
}

// attributeRanges returns the inclusive [start,end] byte ranges of every
// attribute group in code, in source order. start indexes the '<' and end the
// matching '>'.
func attributeRanges(code string) []span {
	var ranges []span
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
		ranges = append(ranges, span{j, end})
		i = end + 1
	}

	// Inline groups: '<' that directly follows '(' or ',' (whitespace aside).
	for i < len(code) {
		c := code[i]
		if c == '"' {
			// '<' inside a literal is data.
			i = literalEnd(code, i)
			continue
		}
		if c == '(' || c == ',' {
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
				ranges = append(ranges, span{j, end})
				i = end + 1
				j = i
				for j < len(code) && (code[j] == ' ' || code[j] == '\t') {
					j++
				}
			}
			continue
		}
		i++
	}
	return ranges
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
