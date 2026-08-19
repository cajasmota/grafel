package vbnet

import "strings"

// splitTopLevel splits s on sep, ignoring separators inside brackets or string
// literals. VB.NET argument and declarator lists nest — `Dim d As
// Dictionary(Of String, List(Of Integer)), n As Integer` has one top-level
// comma and three nested ones — so a plain strings.Split is wrong here.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			i = literalEnd(s, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// matchBracket returns the index of the bracket closing the one at s[start],
// or -1 when it is unbalanced.
func matchBracket(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '"':
			i = literalEnd(s, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// takeIdent peels a leading VB.NET identifier off s and returns it with the
// remainder. Bracket-escaped identifiers ([Class], [Error]) are unwrapped, so
// the table stores the name the rest of the language sees.
func takeIdent(s string) (name, rest string) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", ""
	}
	if s[0] == '[' {
		if end := strings.IndexByte(s, ']'); end > 0 {
			return s[1:end], strings.TrimLeft(s[end+1:], " \t")
		}
		return "", s
	}
	i := 0
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	if i == 0 {
		return "", s
	}
	return s[:i], strings.TrimLeft(s[i:], " \t")
}

// takeWord peels the leading whitespace-delimited word off s, folded to lower
// case, and returns it with the untouched remainder.
func takeWord(s string) (word, rest string) {
	s = strings.TrimLeft(s, " \t")
	i := 0
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	if i == 0 {
		return "", s
	}
	return FoldName(s[:i]), strings.TrimLeft(s[i:], " \t")
}

// cutKeyword splits s at the first top-level occurrence of a standalone
// keyword, case-insensitively. Used to lop `Implements ...` and `Handles ...`
// clauses off a signature before the return type is read.
func cutKeyword(s, keyword string) (before, after string, found bool) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			i = literalEnd(s, i) - 1
			continue
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if i > 0 && (isIdentByte(s[i-1]) || s[i-1] == '.') {
			continue
		}
		end := i + len(keyword)
		if end > len(s) || !strings.EqualFold(s[i:end], keyword) {
			continue
		}
		if end < len(s) && isIdentByte(s[end]) {
			continue
		}
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[end:]), true
	}
	return s, "", false
}

// trailingGroup returns the index of the bracket that opens the group closing
// at the very last byte of s, or -1 when s does not end in a balanced group.
//
// This is not strings.LastIndexByte of the opener: that finds the INNERMOST
// opener, which for `StreamReader(New FileStream(p))` is the one before `p`.
// Callers that want "the group hanging off the tail" must match the closer,
// so the scan runs left to right and skips over each balanced group it meets.
func trailingGroup(s string) int {
	if s == "" {
		return -1
	}
	last := len(s) - 1
	switch s[last] {
	case ')', ']', '}':
	default:
		return -1
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			i = literalEnd(s, i) - 1
		case '(', '[', '{':
			end := matchBracket(s, i)
			if end < 0 {
				return -1
			}
			if end == last {
				return i
			}
			i = end
		}
	}
	return -1
}
