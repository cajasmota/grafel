package vbnet

import "strings"

// LogicalLine is one VB.NET statement after continuation joining: comments
// removed, physical lines joined, attribute groups split off.
type LogicalLine struct {
	// Text is the joined source with comments removed and attributes intact.
	Text string
	// Code is Text with attribute groups removed.
	Code string
	// Attributes are the attribute-group bodies, in source order.
	Attributes []string
	// Doc holds ''' documentation-comment bodies attached to this line: the
	// run of comment-only ''' lines immediately above it, plus any ''' on the
	// physical lines it consumed.
	Doc []string
	// Comments holds ordinary ' and REM comment bodies from the physical lines
	// this logical line consumed.
	Comments []string
	// Line and EndLine are 1-based physical line numbers, inclusive.
	Line    int
	EndLine int
	// Pieces records, for each physical-line fragment joined into Text, the
	// byte offset in Text at which it starts and the physical line it came
	// from. Without it a use site on the third physical line of a continued
	// statement would be reported at the statement's first line, which is the
	// span accuracy S4 needs and the 46 corpus files carrying ' _' would lose.
	Pieces []LinePiece
}

// LinePiece maps one offset in LogicalLine.Text back to a physical line.
type LinePiece struct {
	// Offset is the byte offset in Text where the fragment starts.
	Offset int
	// Line is the 1-based physical line the fragment came from.
	Line int
}

// LineAt returns the 1-based physical line holding the byte at offset in Text.
func (l LogicalLine) LineAt(offset int) int {
	line := l.Line
	for _, p := range l.Pieces {
		if p.Offset > offset {
			break
		}
		line = p.Line
	}
	return line
}

// continuationKeywords are the keywords after which VB.NET 10+ permits an
// implicit line continuation and which this pre-pass honours at bracket depth
// zero. Folded to lower case; VB.NET is case-insensitive.
//
// The list is deliberately narrower than Microsoft's. See ImplicitRuleCoverage
// for the documented rules, which of them are honoured, and why the rest are
// not — the omissions are recorded in code so the gap is testable rather than
// asserted in a comment.
var continuationKeywords = map[string]bool{
	// Operators that may end a line mid-expression.
	"and": true, "andalso": true, "or": true, "orelse": true, "xor": true,
	"not": true, "mod": true, "like": true, "is": true, "isnot": true,
	// Object/collection initialisers.
	"with": true, "from": true,
	// Query operators that are not also plausible bare identifiers.
	"in": true, "into": true, "join": true, "on": true, "where": true,
	"group": true, "order": true, "by": true, "aggregate": true,
}

// continuationTrailingBytes are the final code characters after which VB.NET
// permits an implicit continuation.
//
// '>' is deliberately absent: a line ending in '>' is far more often a
// comparison than an open attribute, and the attribute case is handled
// precisely instead — a logical line that is nothing but attribute groups
// continues onto the declaration it decorates.
//
// '&' is present but qualified: see typeCharacterAmpersand.
const continuationTrailingBytes = ",({.&=+-*/\\^<"

// JoinContinuations turns VB.NET source into logical lines.
//
// It handles, in order: comment removal (so a '_' or a bracket inside a
// comment or a literal can never drive joining), explicit '_' continuation,
// implicit continuation, and attribute splitting.
func JoinContinuations(src string) []LogicalLine {
	// A UTF-8 byte-order mark is not whitespace, so leaving it in place makes
	// the first statement of the file start with three bytes that are neither
	// an identifier nor a keyword: `Public Class Crash` is never recognised
	// and every member of the file lands at file scope.
	//
	// Measured two ways, because the two numbers say different things: 232 of
	// the 302 corpus files carry a BOM (counted by reading the first three
	// bytes of each), and deleting this line costs 22 of them their clean
	// parse (300/302 -> 278/302). Most files survive because their first
	// declaration is preceded by a comment or Imports line that the BOM
	// merely prefixes harmlessly; the ones that break are those where it
	// lands directly on a declaration or an attribute group (#6363).
	src = strings.TrimPrefix(src, "\ufeff")
	raw := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	var out []LogicalLine
	var buf []string
	var pieces []LinePiece
	var doc, comments []string
	var pendingDoc []string
	depth := 0
	startLine := 0
	pending := false

	flush := func(endLine int) {
		if !pending {
			return
		}
		text := strings.TrimSpace(strings.Join(buf, " "))
		attrs, code := SplitAttributes(text)
		if text != "" {
			out = append(out, LogicalLine{
				Text:       text,
				Code:       code,
				Attributes: attrs,
				Doc:        append(append([]string{}, pendingDoc...), doc...),
				Comments:   append([]string{}, comments...),
				Line:       startLine,
				EndLine:    endLine,
				Pieces:     append([]LinePiece{}, pieces...),
			})
			pendingDoc = nil
		}
		buf, pieces, doc, comments = nil, nil, nil, nil
		depth = 0
		pending = false
	}

	for idx, line := range raw {
		lineNo := idx + 1
		code, comment, kind := SplitComment(strings.TrimSuffix(line, "\r"))
		body := CommentBody(comment, kind)

		trimmed := strings.TrimRight(code, " \t")
		explicit := false
		if strings.HasSuffix(trimmed, "_") {
			// A '_' is a continuation only when it stands alone. `my_var_` is
			// an identifier, not a continued line.
			if len(trimmed) == 1 || trimmed[len(trimmed)-2] == ' ' || trimmed[len(trimmed)-2] == '\t' {
				explicit = true
				trimmed = strings.TrimRight(trimmed[:len(trimmed)-1], " \t")
			}
		}

		if strings.TrimSpace(trimmed) == "" && !pending {
			// Comment-only or blank line outside any continuation. Hold ''' so
			// it attaches to the declaration below it.
			switch kind {
			case CommentXMLDoc:
				pendingDoc = append(pendingDoc, body)
			case CommentTick, CommentREM:
				pendingDoc = nil
			default:
				pendingDoc = nil
			}
			continue
		}

		if !pending {
			pending = true
			startLine = lineNo
			depth = 0
		}
		if s := strings.TrimSpace(trimmed); s != "" {
			// Text joins the fragments with a single space, so the offset of
			// the next one is the running length plus one separator per gap.
			off := 0
			for _, b := range buf {
				off += len(b) + 1
			}
			pieces = append(pieces, LinePiece{Offset: off, Line: lineNo})
			buf = append(buf, s)
		}
		switch kind {
		case CommentXMLDoc:
			doc = append(doc, body)
		case CommentTick, CommentREM:
			comments = append(comments, body)
		}

		depth += bracketDelta(MaskStringLiterals(trimmed))
		if depth < 0 {
			depth = 0
		}

		if explicit || depth > 0 || endsWithContinuation(trimmed) || isAttributesOnly(buf) {
			continue
		}
		flush(lineNo)
	}
	flush(len(raw))
	return out
}

// bracketDelta is the net change in bracket nesting contributed by one
// physical line. Square brackets are excluded: in VB.NET they escape a single
// identifier ([Class]) and never span lines.
func bracketDelta(masked string) int {
	d := 0
	for i := 0; i < len(masked); i++ {
		switch masked[i] {
		case '(', '{':
			d++
		case ')', '}':
			d--
		}
	}
	return d
}

// endsWithContinuation reports whether a line ends in a position from which
// VB.NET permits an implicit continuation at bracket depth zero.
func endsWithContinuation(code string) bool {
	masked := strings.TrimRight(MaskStringLiterals(code), " \t")
	if masked == "" {
		return false
	}
	if strings.IndexByte(continuationTrailingBytes, masked[len(masked)-1]) >= 0 {
		return !typeCharacterAmpersand(masked)
	}
	// Trailing keyword.
	i := len(masked)
	for i > 0 && isIdentByte(masked[i-1]) {
		i--
	}
	word := FoldName(masked[i:])
	if word == "" {
		return false
	}
	if i > 0 && masked[i-1] == '.' {
		// obj.Where is a member access, not the query operator.
		return false
	}
	if !continuationKeywords[word] {
		return false
	}
	// An `Option` header is a complete statement whatever it ends on.
	// `Option Strict On`, `Option Explicit On` and `Option Infer On` all end a
	// line with On, which LINQ's `Join … On` puts in the keyword set — and
	// these headers are the first line of a large fraction of legacy VB files,
	// so joining there swallows the first Class or Imports of the file and
	// drops every later member to file scope. A blank line after the header
	// hides it, which is why only real source shows it.
	if head, _ := takeWord(masked); head == "option" {
		return false
	}
	// A line ending `End <keyword>` closes a block; it never continues one.
	// `End With` is the only form that reaches here: `with` is the one word
	// in continuationKeywords that any `End <kw>` can end on. `End Select`
	// does not — `select` is an Honoured:false row in ImplicitRuleCoverage, so
	// the keyword-set check above already returned false. The guard exists
	// because joining an `End With` merges the statement that follows the
	// block — which, when that statement is the `End Sub`, leaves the
	// container stack open and misscopes every declaration in the rest of the
	// file. It is kept general rather than special-cased to `with` so that
	// promoting any other `End` word to a continuation keyword cannot
	// reintroduce the bug.
	if prev := precedingWord(masked, i); prev == "end" {
		return false
	}
	return true
}

// precedingWord returns the identifier word ending just before index i in
// masked, folded to lower case, or "" when there is none.
func precedingWord(masked string, i int) string {
	j := i
	for j > 0 && (masked[j-1] == ' ' || masked[j-1] == '\t') {
		j--
	}
	if j == i || j == 0 {
		return ""
	}
	k := j
	for k > 0 && isIdentByte(masked[k-1]) {
		k--
	}
	return FoldName(masked[k:j])
}

// typeCharacterAmpersand reports whether a trailing '&' is VB.NET's Long type
// character rather than the concatenation operator.
//
// `&` is one of six type-character suffixes (`&%!@#$`) and the only one that
// is also an operator. `32&`, `&HFFFF&` and `count&` are Long literals and
// complete statements; `Dim s = a &` is a dangling concatenation. The two are
// told apart by what precedes the '&': a type character is glued directly to
// the literal or identifier it types, while the operator is separated from its
// left operand by whitespace, a ')' or a closing quote. Choosing the glued
// case as "not a continuation" is the conservative direction — failing to join
// splits one statement in two, whereas joining wrongly deletes the next
// declaration outright, and `0&` / `&H…&` constants are pervasive in the
// Declare/Win32-interop code this package targets.
//
// Two glued cases are nevertheless decidable, because VB.NET forbids the type
// character there: a Char literal (`"x"c&`) and a fractional literal (`1.5&`)
// cannot be typed Long, so the '&' can only be concatenation. Being glued is
// therefore necessary but not sufficient, and both are excluded below.
func typeCharacterAmpersand(masked string) bool {
	if len(masked) < 2 || masked[len(masked)-1] != '&' || !isIdentByte(masked[len(masked)-2]) {
		return false
	}
	body := masked[:len(masked)-1]
	// `"x"c` is a Char literal; '&' does not type a Char.
	if n := len(body); n >= 2 && (body[n-1] == 'c' || body[n-1] == 'C') && body[n-2] == '"' {
		return false
	}
	// `1.5` is a fractional literal; '&' types a Long. The walk spans '.' so
	// that `obj.Count&` is read as a member access (mixed bytes, so a type
	// character) rather than as a number.
	i := len(body)
	for i > 0 && (isIdentByte(body[i-1]) || body[i-1] == '.') {
		i--
	}
	return !isFractionalLiteral(body[i:])
}

// isFractionalLiteral reports whether s is digits with at least one decimal
// point and nothing else.
func isFractionalLiteral(s string) bool {
	dot := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '.':
			dot = true
		case s[i] >= '0' && s[i] <= '9':
		default:
			return false
		}
	}
	return dot && len(s) > 1
}

// isAttributesOnly reports whether everything joined so far is attribute
// groups, which means the declaration they decorate is on a later line.
func isAttributesOnly(buf []string) bool {
	text := strings.TrimSpace(strings.Join(buf, " "))
	if text == "" || text[0] != '<' {
		return false
	}
	attrs, rest := SplitAttributes(text)
	return len(attrs) > 0 && rest == ""
}

// ImplicitRule records one implicit-line-continuation position from the
// VB.NET language reference and whether this pre-pass honours it.
//
// This table exists so the cost of the narrow choice is a fact in the test
// suite rather than a claim in a comment: TestImplicitRuleCoverage drives
// every Honoured rule through JoinContinuations and asserts it joins, and
// drives every rule with Honoured=false through and asserts it does NOT —
// so the day one is implemented, the table must be updated to stay green.
type ImplicitRule struct {
	// Name identifies the documented position.
	Name string
	// Sample is a two-physical-line VB.NET fragment exercising the position.
	Sample string
	// Honoured is whether JoinContinuations joins the sample.
	Honoured bool
	// Why explains a false.
	Why string
}

// ImplicitRuleCoverage is the measured coverage of implicit continuation.
//
// Positions honoured: 16 of 24. Seven of the eight that are not are bare
// contextual keywords from LINQ query syntax which are also ordinary
// identifiers, so honouring them costs more than it buys: `Dim n = q.Take`
// would silently swallow the next statement. The eighth is a line ending in
// '>', which is a comparison far more often than an open attribute list; the
// attribute case is handled precisely instead (see isAttributesOnly).
//
// What the unhonoured positions cost. The premise that they "appear only
// inside method bodies" is false: a field or Const initialiser is a
// declaration and may hold a query — `Private ReadOnly top As Object = From a
// In b Take` / `5` does split. What actually holds, and is what justifies
// shipping the gap, is weaker but sufficient for this story: the head line
// still carries the declarator and its As clause, so the name, kind and type
// are recorded correctly; the orphaned tail is an expression fragment that
// declares nothing, so it adds no phantom symbol to the table. The cost is
// therefore borne by expression-level recall in S5, not by the declaration
// table — the table under-reads a value, it never mis-declares a name.
//
// Every unhonoured position gets a row here rather than only the ones that
// seemed worth arguing about, so the table is derive-shaped: the count is a
// count of rows, and TestImplicitRuleCoverage fails the day a row's behaviour
// changes in either direction (#6361).
//
// UNVERIFIED: the frequency of these positions in real VB.NET is unknown to
// this package; no VB.NET corpus was available. The count below is a count of
// language-reference positions, not of occurrences in code.
var ImplicitRuleCoverage = []ImplicitRule{
	{Name: "after a comma", Sample: "Dim x = F(1,\n2)", Honoured: true},
	{Name: "after an open parenthesis", Sample: "Dim x = F(\n1)", Honoured: true},
	{Name: "before a closing parenthesis", Sample: "Dim x = F(1\n)", Honoured: true},
	{Name: "after an open brace", Sample: "Dim x = {\n1}", Honoured: true},
	{Name: "before a closing brace", Sample: "Dim x = {1\n}", Honoured: true},
	{Name: "after the concatenation operator", Sample: "Dim s = a &\nb", Honoured: true},
	{Name: "after an assignment operator", Sample: "Dim x =\n1", Honoured: true},
	{Name: "after an arithmetic operator", Sample: "Dim x = a +\nb", Honoured: true},
	{Name: "after a comparison operator", Sample: "Dim b = a <\nc", Honoured: true},
	{Name: "after a logical operator", Sample: "Dim b = a AndAlso\nc", Honoured: true},
	{Name: "after Is / IsNot", Sample: "Dim b = a Is\nNothing", Honoured: true},
	{Name: "after a member qualifier", Sample: "Dim x = a.\nB", Honoured: true},
	{Name: "after With in an initialiser", Sample: "Dim x = New F With\n{.A = 1}", Honoured: true},
	{Name: "after From in an initialiser", Sample: "Dim x = New L From\n{1}", Honoured: true},
	{Name: "after In in a query", Sample: "Dim q = From a In\nb", Honoured: true},
	{Name: "inside an attribute list", Sample: "<Serializable()>\nPublic Class C", Honoured: true},
	{
		Name: "after a bare Let", Sample: "Dim q = From a In b Let\nc = 1",
		Honoured: false,
		Why:      "Let is also an ordinary identifier; joining would swallow the next statement after `x.Let`",
	},
	{
		Name: "after a bare Skip", Sample: "Dim q = From a In b Skip\n1",
		Honoured: false,
		Why:      "Skip is also a common method name; `q.Skip` on its own line is a complete statement",
	},
	{
		Name: "after a bare Take", Sample: "Dim q = From a In b Take\n1",
		Honoured: false,
		Why:      "Take is also a common method name",
	},
	{
		Name: "after a bare Distinct", Sample: "Dim q = From a In b Distinct\nSelect a",
		Honoured: false,
		Why:      "Distinct is also a common method name",
	},
	{
		Name: "after a bare Select", Sample: "Dim q = From a In b Select\na",
		Honoured: false,
		Why:      "Select ends three different constructs — the query operator, `End Select` and a `Select Case` header — and the query form is the rarest of the three",
	},
	{
		Name: "after a bare Ascending", Sample: "Dim q = From a In b Order By c Ascending\nSelect c",
		Honoured: false,
		Why:      "Ascending is a bare contextual keyword and an ordinary identifier; `Dim x = e.Ascending` on its own line is a complete statement",
	},
	{
		Name: "after a bare Descending", Sample: "Dim q = From a In b Order By c Descending\nSelect c",
		Honoured: false,
		Why:      "Descending is a bare contextual keyword and an ordinary identifier, exactly as Ascending is",
	},
	{
		Name: "after a '>' comparison operator", Sample: "Dim b = a >\nc",
		Honoured: false,
		Why:      "'>' also closes an attribute list; joining after it would swallow the declaration under every attribute on its own line, so the attribute case is handled precisely (isAttributesOnly) and the operator case is left split",
	},
}
