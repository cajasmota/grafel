// Package vbnet holds the hand-written VB.NET pre-pass: line-continuation
// joining, comment/string handling, attribute stripping, and the per-file
// declaration table that later stories use to disambiguate parentheses.
//
// # Why this lives outside internal/extractors/
//
// There is no usable tree-sitter grammar for VB.NET (see #6327), so the
// extractor is a hand-written line scanner. The scanner's accuracy rests
// entirely on the machinery in this package, which is therefore worth testing
// on its own — no EntityRecord, no registry, no substrate.
//
// It cannot live at internal/extractors/vbnet/ yet. #6332 (590ec11ce) made
// tools/coverage derive the supported-language matrix from the depth-1
// directories of internal/extractors/ at run time and bind them to a reviewed
// languageRoster. Creating that directory before an extractor exists would
// publish a VB.NET coverage row for a language that emits nothing. The
// precedent for language machinery outside internal/extractors/ is
// internal/generated/ (#6329) and internal/treesitter/.
//
// # Case sensitivity
//
// VB.NET is case-insensitive in both keywords and identifiers: DIM, Dim and
// dim are one keyword, Foo and FOO are one identifier. Every comparison in
// this package folds case, and the declaration table is keyed on FoldName.
//
// # Verification status
//
// UNVERIFIED against real VB.NET source. No .vb file exists anywhere on the
// machine this was written on (checked during #6327 S2). Every fixture here is
// constructed from the VB.NET language reference and from the shapes reported
// in #6321 (WinForms .Designer.vb, Inherits clauses, Handles wiring, file-top
// Imports). Per AGENTS.md "Evidence", that gap is stated rather than papered
// over: the mechanism is pinned by tests, the distribution is not.
package vbnet

import "strings"

// CommentKind classifies the comment terminating a physical line.
type CommentKind int

const (
	// CommentNone means the line carries no comment.
	CommentNone CommentKind = iota
	// CommentTick is an ordinary ' comment.
	CommentTick
	// CommentREM is a REM comment. Legal only at a statement boundary.
	CommentREM
	// CommentXMLDoc is a ''' documentation comment.
	//
	// These are stripped from code like any other comment, but they are
	// classified rather than discarded and carried on LogicalLine.Doc. ''' is
	// the standard documentation form in the legacy WinForms code #6321 is
	// about; throwing it away in the pre-pass would make docstrings
	// unrecoverable downstream, and telling it apart costs one comparison.
	CommentXMLDoc
)

// StringFill is the byte MaskStringLiterals writes over string-literal
// content. It is not an identifier byte, a parenthesis or a quote, so a masked
// line cannot make the later passes see a declaration or a call that only
// existed inside a literal. Masking preserves byte length, so column offsets
// into the masked line remain valid against the original.
const StringFill = byte(0x00)

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// span is a half-open [start,end) byte range of string-literal *content*,
// excluding the delimiting quotes.
type span struct{ start, end int }

type scanResult struct {
	commentAt   int // byte index where the comment starts, or -1
	commentKind CommentKind
	strings     []span
}

// scanLine walks one physical line, tracking string-literal state, and reports
// where a comment begins and where every string literal's content sits.
//
// The rule that matters: inside a string literal, "" is an escaped quote and '
// is ordinary data. So `Dim s = "a ” b"` contains no comment, and the literal
// survives intact.
func scanLine(line string) scanResult {
	res := scanResult{commentAt: -1}
	atStmt := true // at a statement boundary: start of line, or just after ':'
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case c == '"':
			i++
			contentStart := i
			contentEnd := len(line)
			for i < len(line) {
				if line[i] != '"' {
					i++
					continue
				}
				if i+1 < len(line) && line[i+1] == '"' {
					// Escaped quote inside the literal, not a terminator.
					i += 2
					continue
				}
				contentEnd = i
				i++
				break
			}
			res.strings = append(res.strings, span{contentStart, contentEnd})
			atStmt = false
		case c == '\'':
			res.commentAt = i
			res.commentKind = CommentTick
			if strings.HasPrefix(line[i:], "'''") {
				res.commentKind = CommentXMLDoc
			}
			return res
		case c == ' ' || c == '\t':
			// Whitespace does not close a statement boundary.
			i++
		case c == ':':
			// ':' separates statements, except in a `name:=value` named
			// argument, where it is part of the operator.
			if i+1 < len(line) && line[i+1] == '=' {
				i += 2
				atStmt = false
				continue
			}
			i++
			atStmt = true
		default:
			if atStmt && (c == 'R' || c == 'r') && isREMAt(line, i) {
				res.commentAt = i
				res.commentKind = CommentREM
				return res
			}
			atStmt = false
			i++
		}
	}
	return res
}

// isREMAt reports whether a REM comment keyword starts at line[i]. The caller
// has already established that i sits at a statement boundary, which is what
// keeps `obj.Rem` and `Dim remaining` from being read as comments.
func isREMAt(line string, i int) bool {
	if i+3 > len(line) {
		return false
	}
	if !strings.EqualFold(line[i:i+3], "REM") {
		return false
	}
	// REMaining is an identifier; REM and "REM " are comments.
	if i+3 < len(line) && isIdentByte(line[i+3]) {
		return false
	}
	return true
}

// SplitComment splits one physical line into its code and its comment.
//
// code excludes the comment; comment includes the introducer (' , ”' or REM)
// so callers can tell what they were given. A ' inside a string literal is
// data, not a comment introducer.
func SplitComment(line string) (code, comment string, kind CommentKind) {
	r := scanLine(line)
	if r.commentAt < 0 {
		return line, "", CommentNone
	}
	return line[:r.commentAt], line[r.commentAt:], r.commentKind
}

// CommentBody returns a comment with its introducer and one leading space
// removed, which is what a docstring consumer wants.
func CommentBody(comment string, kind CommentKind) string {
	switch kind {
	case CommentXMLDoc:
		comment = strings.TrimPrefix(comment, "'''")
	case CommentTick:
		comment = strings.TrimPrefix(comment, "'")
	case CommentREM:
		if len(comment) >= 3 {
			comment = comment[3:]
		}
	default:
		return ""
	}
	return strings.TrimSpace(comment)
}

// MaskStringLiterals overwrites the *content* of every string literal with
// StringFill, keeping the delimiting quotes and the byte length of the line.
//
// The declaration and reference passes run over masked text so that a '(' or
// an identifier appearing inside a literal cannot be mistaken for code.
func MaskStringLiterals(line string) string {
	r := scanLine(line)
	if len(r.strings) == 0 {
		return line
	}
	b := []byte(line)
	for _, s := range r.strings {
		for j := s.start; j < s.end && j < len(b); j++ {
			b[j] = StringFill
		}
	}
	return string(b)
}

// FoldName normalises a VB.NET identifier for comparison. VB.NET is
// case-insensitive, so Foo, FOO and foo are one name.
func FoldName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
