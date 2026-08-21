package vbnet

import "strings"

// The reference pass: find every `name(` use site and ask the declaration
// table what it means.
//
// This is the accuracy problem #6327 exists for. `Foo(1)` is an invocation, an
// array index, a default-property access or a generic instantiation, and
// Microsoft's grammar makes InvocationExpression and IndexExpression the same
// production. A `name\s*\(` regex over-fires on every array access and emits
// phantom CALLS edges, which is worse than emitting none. So the classification
// here is never decided by shape alone: it is either settled syntactically
// (`New X(`, `X(Of T)`) or delegated to Table.ClassifyParen.

// parenKeywords are the words that may sit immediately before a '(' without
// being invoked.
//
// Without this set `If (x) Then` reports a call to If, `Return (n)` a call to
// Return and `Function(x) x + 1` a call to Function. That half is
// load-bearing. Measured by deleting the named entries and re-parsing the
// 302-file corpus, against a baseline of 41,748 use sites of which 8,702
// satisfy IsCall:
//
//	deleted                              extra use sites   extra IsCall
//	the 11 operator entries                        +299              +0
//	the 31 control-flow entries                  +1,329            +264
//	both                                         +1,628            +264
//
// The two halves fail differently and the difference is the point: the
// operator entries guard against use sites that are all non-calls, while the
// control-flow entries guard against 264 sites that would be reported as
// CALLS — `If(…)`, `Return(…)`, `Case(…)`, `While(…)`, statement-position
// unknowns that IsCall promotes. Deleting them does not merely add noise, it
// adds wrong edges.
//
// The other half is a PRECISION TRADE, stated here because it is a recall
// cost with no diagnostic attached. Eighteen entries are VB.NET CONTEXTUAL
// keywords — aggregate, ascending, by, descending, distinct, equals, from,
// group, into, join, order, preserve, skip, take, until, where, alias, lib —
// which are legal identifiers, and the corpus does declare members with such
// names. An unqualified call to one is dropped silently. Measured the same
// way: deleting all eighteen surfaces 4 additional use sites out of 41,748,
// exactly 1 of them a call, all spelled `where`. The trade is taken
// deliberately at that price, not because a declaration "cannot" shadow these
// words — it can.
//
// A qualified use is unaffected: the check is skipped when the name has a
// qualifier, so `q.Take(5)` is still a use site.
var parenKeywords = map[string]bool{
	"if": true, "elseif": true, "then": true, "else": true, "while": true,
	"until": true, "do": true, "loop": true, "for": true, "each": true,
	"to": true, "step": true, "next": true, "select": true, "case": true,
	"return": true, "throw": true, "exit": true, "continue": true,
	"goto": true, "resume": true, "when": true, "catch": true, "finally": true,
	"try": true, "with": true, "using": true, "synclock": true,
	"and": true, "andalso": true, "or": true, "orelse": true, "xor": true,
	"not": true, "mod": true, "is": true, "isnot": true, "like": true,
	"in": true, "into": true, "join": true, "on": true, "where": true,
	"group": true, "order": true, "by": true, "aggregate": true, "from": true,
	"let": true, "skip": true, "take": true, "distinct": true,
	"ascending": true, "descending": true, "equals": true,
	"as": true, "of": true, "new": true, "addressof": true, "byval": true,
	"byref": true, "optional": true, "paramarray": true,
	"sub": true, "function": true, "call": true, "redim": true, "erase": true,
	"preserve": true, "declare": true, "lib": true, "alias": true,
	"handles": true, "implements": true, "inherits": true, "imports": true,
	"raiseevent": true, "addhandler": true, "removehandler": true,
	"dim": true, "const": true, "static": true, "global": true, "me": true,
	"mybase": true, "myclass": true, "nothing": true, "end": true,
	"stop": true, "error": true, "option": true, "property": true,
	"event": true, "operator": true, "class": true, "module": true,
	"structure": true, "interface": true, "enum": true, "namespace": true,
	"delegate": true, "shared": true, "widening": true, "narrowing": true,
}

// vbIntrinsics are call-shaped operators built into the language. They resolve
// to no declaration anywhere, so S5 must not emit a CALLS edge for them; the
// alternative is one dangling edge per CType in a WinForms designer file, and
// a single designer file in the corpus contains hundreds.
var vbIntrinsics = map[string]bool{
	"ctype": true, "directcast": true, "trycast": true, "gettype": true,
	"typeof": true, "nameof": true, "cbool": true, "cbyte": true,
	"cchar": true, "cdate": true, "cdbl": true, "cdec": true, "cint": true,
	"clng": true, "cobj": true, "csbyte": true, "cshort": true, "csng": true,
	"cstr": true, "cuint": true, "culng": true, "cushort": true,
	"ubound": true, "lbound": true, "getxmlnamespace": true,
}

// scanRefs records every use site in text[from:] on the innermost open node.
//
// text is masked and attribute-blanked; textOff is its offset within ll.Text,
// which is what makes a use site on the fourth physical line of a continued
// statement report that line rather than the statement's first.
func (p *parser) scanRefs(text string, textOff int, ll LogicalLine, from int) {
	if from < 0 {
		from = 0
	}
	if from >= len(text) {
		return
	}
	owner := p.top().node
	scope := p.scope()
	typeScope := enclosingTypePath(owner)

	// S7b (#6327): the AddressOf operand scan shares this entry point because
	// it needs the identical (text, textOff, ll, from) tuple — same masking,
	// same continuation map, same declarator-skipping offset. See addressof.go.
	p.scanAddressOf(text, textOff, ll, from)

	for i := from; i < len(text); i++ {
		switch text[i] {
		case '"':
			i = literalEnd(text, i) - 1
			continue
		case '(':
		default:
			continue
		}

		// The name is the identifier to the left, whitespace allowed: VB.NET
		// accepts `Foo (1)`.
		j := i
		for j > 0 && (text[j-1] == ' ' || text[j-1] == '\t') {
			j--
		}
		nameEnd := j
		for j > 0 && isIdentByte(text[j-1]) {
			j--
		}
		if j == nameEnd {
			continue // a grouping parenthesis, not a use site
		}
		name := text[j:nameEnd]
		if name[0] >= '0' && name[0] <= '9' {
			continue // part of a numeric literal, not an identifier
		}
		qual, qualified := qualifierBefore(text, j)
		prev := wordBefore(text, qualStart(text, j, qualified))

		folded := FoldName(name)
		if !qualified && parenKeywords[folded] {
			continue
		}

		ofKeyword := false
		if w, _ := takeWord(text[i+1:]); w == "of" {
			ofKeyword = true
		}

		ref := Ref{
			Name:          name,
			Qualifier:     qual,
			Qualified:     qualified,
			New:           prev == "new",
			Generic:       ofKeyword,
			Intrinsic:     !qualified && vbIntrinsics[folded],
			StatementHead: j == 0 || prev == "call",
			Scope:         scope,
			Line:          ll.LineAt(textOff + j),
		}
		ref.Kind = p.classify(ref, scope, typeScope, qualified, ofKeyword)
		owner.Refs = append(owner.Refs, ref)
	}
}

// classify decides what the use site's parenthesis means.
func (p *parser) classify(ref Ref, scope, typeScope string, qualified, ofKeyword bool) ParenKind {
	// `New X(...)` is a construction whatever the table knows, and `New
	// List(Of T)` is a construction rather than a bare type-argument list —
	// so New is tested before Of, not after.
	if ref.New {
		return ParenCall
	}
	if ofKeyword {
		return ParenGeneric
	}
	switch {
	case !qualified:
		return p.table.ClassifyParen(ref.Name, scope, false)
	case FoldName(ref.Qualifier) == "me" || FoldName(ref.Qualifier) == "myclass":
		// Me.Foo resolves in the enclosing type, not in the local scope: a
		// local named Foo must not decide what Me.Foo means.
		return p.table.ClassifyParen(ref.Name, typeScope, false)
	}
	// A qualifier this file DECLARES with an explicit `As` type is knowable:
	// the table has its declared type, so `writer.WriteStartElement(...)` is a
	// member access on an XmlWriter and the paren is an invocation (#6454).
	// Without this the site stays ParenUnknown and, not being at statement
	// head, IsCall reports false — which is why NO qualified member call
	// produced a CALLS edge at all before #6454, not merely a mis-named one.
	//
	// ReceiverType is the whole guard, and it is deliberately narrow: only a
	// VISIBLE declaration of a VALUE with a non-array `As` type answers. So
	// every site promoted here is one whose target is ALSO rendered from the
	// declared type — the classification and the ToID cannot disagree.
	//
	// The residual imprecision, stated plainly: `obj.Items(3)` where Items is
	// an array field of obj's type is an INDEX, and this file cannot see the
	// members of obj's type to know that. Such a site becomes a CALLS edge to
	// a real member of a real type — a wrong edge KIND, not a wrong target.
	//
	// Anything else names something declared outside this file (or an
	// expression), which the per-file table cannot see. Saying so is the
	// point: a guess there would be the confidently-wrong edge the epic is
	// about.
	if p.table.ReceiverType(ref.Qualifier, scope) != "" {
		return ParenCall
	}
	return ParenUnknown
}

// enclosingTypePath returns the dotted path of the nearest enclosing type.
func enclosingTypePath(n *Node) string {
	for x := n; x != nil; x = x.parent {
		if x.Kind.IsType() {
			return x.Path()
		}
	}
	return ""
}

// qualifierBefore reads the dotted prefix ending just before index i.
//
// qualified is true whenever a '.' precedes the name, even when no identifier
// chain could be read — `Foo(1).Bar(2)` qualifies Bar with an expression, and
// treating it as unqualified would resolve Bar against this file's locals.
func qualifierBefore(text string, i int) (qualifier string, qualified bool) {
	j := i
	for j > 0 && (text[j-1] == ' ' || text[j-1] == '\t') {
		j--
	}
	if j == 0 || text[j-1] != '.' {
		return "", false
	}
	var parts []string
	for j > 0 && text[j-1] == '.' {
		j--
		for j > 0 && (text[j-1] == ' ' || text[j-1] == '\t') {
			j--
		}
		end := j
		for j > 0 && isIdentByte(text[j-1]) {
			j--
		}
		if j == end {
			break // the qualifier is an expression, not a name
		}
		parts = append([]string{text[j:end]}, parts...)
		for j > 0 && (text[j-1] == ' ' || text[j-1] == '\t') {
			j--
		}
	}
	return strings.Join(parts, "."), true
}

// wordBefore returns the identifier ending just before index i, folded.
//
// It differs from precedingWord in one way that matters: precedingWord
// requires whitespace between the word and i, because it was written to answer
// "is this `End With`". Here the word is usually glued to what follows it —
// `Sub(value)` — so requiring a space would silently answer "" every time.
func wordBefore(s string, i int) string {
	j := i
	for j > 0 && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	k := j
	for k > 0 && isIdentByte(s[k-1]) {
		k--
	}
	return FoldName(s[k:j])
}

// qualStart returns the index at which the qualified name begins, so the word
// before it (New, Call, AddressOf) can be read.
func qualStart(text string, i int, qualified bool) int {
	if !qualified {
		return i
	}
	j := i
	for j > 0 && (text[j-1] == ' ' || text[j-1] == '\t' || text[j-1] == '.' || isIdentByte(text[j-1])) {
		j--
	}
	return j
}
