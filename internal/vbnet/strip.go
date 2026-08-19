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
// VERIFIED against real VB.NET source as of #6327 S4. The corpus that did not
// exist when S1-S3 were written is now on disk: 302 .vb files, 148,308 lines,
// 88 of them .Designer.vb, across WakeOnLAN, staxrip and
// display-drivers-uninstaller (#6363). 300 of the 302 parse with no
// diagnostic; see TestCorpusParseRate, which is a gate rather than a report.
//
// Running S4's parser over that corpus corrected this pre-pass in four places
// that no constructed fixture had reached. Each is priced by how many files
// lose a clean parse when the fix is removed and the corpus re-parsed:
//
//	a UTF-8 byte-order mark ahead of the first declaration   22 files
//	$"..." interpolation scanned inverted, hole as text       0 files
//	an enum member named `Custom` eaten by the modifier peel   3 files
//	multi-line lambdas leaving the container stack open      45 files
//
// The interpolation row is 0 by that measure and was still worth fixing: 106
// of the 302 files contain a $"..." literal, and scanning it inverted exposed
// its text as code and hid its holes, so it corrupted masking wherever it
// appeared rather than failing loudly anywhere. The lambda row also recorded
// method-body locals as type-scope FIELDS, which no parse rate can see.
//
// Still unverified: the distribution in the reporter's own ~670-file tree
// (#6321). Nothing here claims a recall or precision figure for CALLS.
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
		case c == '"' && i > 0 && line[i-1] == '$':
			i = scanInterpolated(line, i, &res)
			atStmt = false
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

// scanInterpolated walks a $"..." interpolated string starting at the opening
// quote line[start] and returns the index just past its closing quote.
//
// An interpolated string is text with holes: `$"a {f(x)} b"` is two literal
// chunks and one expression. The chunks are recorded as string spans — and,
// unlike an ordinary literal, the recorded span INCLUDES its delimiters, so
// masking removes the quote and the brace too. That is what keeps a later
// literal-skipping scan from pairing the interpolation's opening quote with a
// nested literal's quote: after masking there is no quote left to pair.
//
// The hole itself is left as code, which is both correct and useful — a call
// inside a hole is a real call, and `$"{f("x")}"` only balances its
// parentheses if the hole is counted rather than skipped over as text.
//
// `{{` and `}}` are escaped braces and stay inside the chunk; `""` is the
// escaped quote, as everywhere else in VB.NET.
func scanInterpolated(line string, start int, res *scanResult) int {
	i := start + 1
	chunkStart := start // includes the delimiter
	depth := 0
	for i < len(line) {
		c := line[i]
		if depth == 0 {
			switch {
			case c == '"' && i+1 < len(line) && line[i+1] == '"':
				i += 2
			case c == '"':
				res.strings = append(res.strings, span{chunkStart, i + 1})
				return i + 1
			case c == '{' && i+1 < len(line) && line[i+1] == '{':
				i += 2
			case c == '{':
				res.strings = append(res.strings, span{chunkStart, i + 1})
				depth = 1
				i++
			case c == '}' && i+1 < len(line) && line[i+1] == '}':
				i += 2
			default:
				i++
			}
			continue
		}
		switch c {
		case '"':
			// A nested literal inside a hole: mask its content only, so its
			// own quotes stay paired for any later scan.
			end := literalEnd(line, i)
			if end > i+1 {
				res.strings = append(res.strings, span{i + 1, end - 1})
			}
			i = end
		case '{':
			depth++
			i++
		case '}':
			depth--
			i++
			if depth == 0 {
				chunkStart = i - 1 // the '}' opens the next chunk
			}
		default:
			i++
		}
	}
	// Unterminated: mask what is left rather than dropping the span.
	res.strings = append(res.strings, span{chunkStart, len(line)})
	return len(line)
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
