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
const continuationTrailingBytes = ",({.&=+-*/\\^<"

// JoinContinuations turns VB.NET source into logical lines.
//
// It handles, in order: comment removal (so a '_' or a bracket inside a
// comment or a literal can never drive joining), explicit '_' continuation,
// implicit continuation, and attribute splitting.
func JoinContinuations(src string) []LogicalLine {
	raw := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	var out []LogicalLine
	var buf []string
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
			})
			pendingDoc = nil
		}
		buf, doc, comments = nil, nil, nil
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
		return true
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
	return continuationKeywords[word]
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
// Positions honoured: 16 of 21. The five that are not are all bare contextual
// keywords from LINQ query syntax which are also ordinary identifiers, so
// honouring them costs more than it buys: `Dim n = q.Take` would silently
// swallow the next statement. All five appear only inside method bodies, so
// none of them can split a *declaration* — the cost falls on CALLS recall in
// S5, not on the declaration table this story exists to build.
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
		Why:      "`End Select` ends a line with Select; honouring it would merge the statement after every Select Case block",
	},
}
