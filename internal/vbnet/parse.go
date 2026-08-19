package vbnet

import (
	"fmt"
	"strings"
)

// This file is the VB.NET parser (#6327 S4). It turns the pre-pass output of
// JoinContinuations and the declaration table of BuildTable into a
// containment tree with byte-accurate spans, plus a classified list of
// parenthesis use sites.
//
// # What it deliberately does not do
//
// No EntityRecord, no RelationshipRecord, no registry. S5 owns emission. The
// contract this file offers S5 is: a tree whose nodes carry spans that index
// the original source, and Ref values whose ParenKind was decided by the
// declaration table rather than guessed from shape.
//
// # Why the tree and the table are built by two walks
//
// BuildTable (S3) records every declared NAME, including method-body locals,
// For-loop variables and Catch variables, because ClassifyParen needs them.
// The tree records only what can become an entity. Running the table walk
// first and the tree walk second means the classification of a use site on
// line 10 can consult a declaration on line 400 — a single fused pass could
// not, and VB.NET code routinely calls a method declared later in the file.
// TestCorpusTreeAgreesWithTable pins the two walks against each other so they
// cannot drift.
//
// # Measured coverage
//
// 300 of 302 real .vb files parse with no diagnostic (99.3%), measured over
// WakeOnLAN, staxrip and display-drivers-uninstaller — 148,308 lines, 88 of
// them .Designer.vb. Run it with GRAFEL_VBNET_CORPUS set; see
// TestCorpusParseRate, which prints the number and every failure shape.
//
// The two residual files share one cause: VB 14 lets a string literal, and an
// interpolation hole, span physical lines, and this pre-pass is line-oriented.
// That limitation reaches further than the two diagnosed files - 9 corpus
// files contain such a literal and 7 of them parse clean with their interiors
// scanned as code. Measured damage from the silent 7, counting use sites
// recorded on lines inside a mis-scanned interior: 5 use sites, 0 of them
// IsCall, no phantom nodes. See TestMultiLineLiteralIsUnsupported for how the
// window was drawn and why 5 is an upper bound.
// TestMultiLineLiteralIsUnsupported pins the gap in the failing direction so
// it cannot quietly change size.
//
// Nothing here claims a recall or precision figure for CALLS. What is measured
// is that the parser understands the structure of these files; what an edge
// drawn from a Ref resolves to is S5's to measure.

// NodeKind is what a tree node declares.
type NodeKind int

const (
	// NodeFile is the synthetic root. It has no name.
	NodeFile NodeKind = iota
	// NodeNamespace is a Namespace block.
	NodeNamespace
	// NodeClass is a Class block.
	NodeClass
	// NodeModule is a Module block. Its members are promoted (see
	// Table.isModuleScope), which is why the keyword is kept distinct from
	// NodeClass rather than folded into it.
	NodeModule
	// NodeStructure is a Structure block.
	NodeStructure
	// NodeInterface is an Interface block.
	NodeInterface
	// NodeEnum is an Enum block.
	NodeEnum
	// NodeDelegate is a Delegate declaration. It opens no block.
	NodeDelegate
	// NodeMethod is Sub, Function, Operator, Declare or a Sub New constructor.
	NodeMethod
	// NodeProperty is a property, auto or full.
	NodeProperty
	// NodeAccessor is a Get/Set body, or a Custom Event's AddHandler,
	// RemoveHandler or RaiseEvent body.
	NodeAccessor
	// NodeEvent is an Event declaration.
	NodeEvent
	// NodeField is a type-level variable. Method-body locals are NOT nodes.
	NodeField
	// NodeConst is a type-level constant.
	NodeConst
	// NodeEnumMember is a member of an Enum body.
	NodeEnumMember
	// NodeImport is an Imports directive, plain or aliased.
	NodeImport
	// NodeOption is an Option Strict/Explicit/Infer/Compare header.
	NodeOption
)

var nodeKindNames = map[NodeKind]string{
	NodeFile: "file", NodeNamespace: "namespace", NodeClass: "class",
	NodeModule: "module", NodeStructure: "structure", NodeInterface: "interface",
	NodeEnum: "enum", NodeDelegate: "delegate", NodeMethod: "method",
	NodeProperty: "property", NodeAccessor: "accessor", NodeEvent: "event",
	NodeField: "field", NodeConst: "const", NodeEnumMember: "enum_member",
	NodeImport: "import", NodeOption: "option",
}

func (k NodeKind) String() string {
	if s, ok := nodeKindNames[k]; ok {
		return s
	}
	return "unknown"
}

// IsType reports whether the kind is one that can hold members and that S5
// must anchor EXTENDS/IMPLEMENTS edges to (#6295/#6298: the edge belongs to
// the type, never to the file).
func (k NodeKind) IsType() bool {
	switch k {
	case NodeClass, NodeModule, NodeStructure, NodeInterface, NodeEnum:
		return true
	}
	return false
}

// Span is a source range. Lines are 1-based and inclusive; bytes are a
// half-open [StartByte,EndByte) range into the ORIGINAL source string, so
// src[StartByte:EndByte] slices the declaration back out even when the file
// uses CRLF line endings.
type Span struct {
	StartLine int
	EndLine   int
	StartByte int
	EndByte   int
}

// Param is one parameter of a method, property or delegate.
type Param struct {
	Name       string
	TypeName   string
	IsArray    bool
	ByRef      bool
	Optional   bool
	ParamArray bool
	Default    string
}

// Node is one declaration in the containment tree.
type Node struct {
	Kind NodeKind
	// Name is the identifier as spelled at the declaration. An operator's
	// name is its symbol ("+"); a constructor's is "New".
	Name string
	// Keyword is the folded declaring keyword: class, module, sub, function,
	// operator, declare, property, get, set, event, dim, const, imports...
	Keyword string
	// Modifiers are the folded declaration modifiers in source order.
	Modifiers []string
	// TypeName is the return type, field type or property type as written.
	TypeName string
	// IsArray is set for an array-typed field or property.
	IsArray bool
	// Generic is set when the declaration carries an (Of ...) list.
	Generic bool
	// TypeParams are the names in that list.
	TypeParams []string
	// Params is the parameter list, empty for a parameterless declaration.
	Params []Param
	// Attributes are the attribute-group bodies decorating the declaration.
	Attributes []string
	// Doc holds ''' documentation-comment bodies attached to the declaration.
	Doc []string
	// Inherits are the names in an `Inherits` clause. A Class has at most one;
	// an Interface may have several.
	Inherits []string
	// Implements are the names in a type-level `Implements` clause, or the
	// `Implements IFoo.Bar` members a method/property implements.
	Implements []string
	// Handles are the `Handles obj.Event` targets of a method.
	Handles []string
	// Target is the right-hand side of an aliased Imports, the value of an
	// Option header, or the library of a Declare.
	Target string
	// Alias is the `Alias "name"` of a Declare.
	Alias string
	// Constructor marks `Sub New`.
	Constructor bool
	// Scope is the dotted path of the containing declarations, "" at file
	// level. It is the same string BuildTable puts on Symbol.Scope.
	Scope string
	// Span covers the whole declaration, header through its End line.
	Span Span
	// HeaderSpan covers only the declaring statement.
	HeaderSpan Span
	// Refs are the parenthesis use sites in the statements directly inside
	// this node, in source order.
	Refs []Ref
	// Children are the declarations directly inside this one.
	Children []*Node

	parent *Node
}

// Parent returns the enclosing node, nil for the file root.
func (n *Node) Parent() *Node { return n.parent }

// Path is the dotted path from the file root, excluding the root itself.
func (n *Node) Path() string {
	if n == nil || n.Kind == NodeFile {
		return ""
	}
	if n.Scope == "" {
		return n.Name
	}
	return n.Scope + "." + n.Name
}

// Walk calls fn on n and every descendant, in document order.
func (n *Node) Walk(fn func(*Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Dump renders the tree as indented text. Test failures print it, so a wrong
// shape is readable without a debugger.
func (n *Node) Dump() string {
	var b strings.Builder
	var rec func(*Node, int)
	rec = func(x *Node, depth int) {
		fmt.Fprintf(&b, "%s%s %s [%d..%d]", strings.Repeat("  ", depth),
			x.Kind, x.Name, x.Span.StartLine, x.Span.EndLine)
		if len(x.Refs) > 0 {
			names := make([]string, 0, len(x.Refs))
			for _, r := range x.Refs {
				names = append(names, r.String())
			}
			fmt.Fprintf(&b, " refs=%v", names)
		}
		b.WriteByte('\n')
		for _, c := range x.Children {
			rec(c, depth+1)
		}
	}
	rec(n, 0)
	return b.String()
}

// Ref is one `name(` use site with the meaning the declaration table gave it.
//
// Kind is the table's answer. The syntactic flags beside it are the facts the
// table cannot know, and IsCall composes the two — see its comment for why the
// composition lives here rather than in S5.
type Ref struct {
	// Name is the identifier immediately before the '('.
	Name string
	// Qualifier is the dotted prefix, "" for an unqualified use — but see
	// Qualified: "" does NOT mean unqualified.
	Qualifier string
	// Qualified is whether a '.' preceded the name at all.
	//
	// It is separate from Qualifier because the two disagree, and the
	// disagreement is the dangerous case. A `With` block member access
	// (`.OpenSubKey(k)`) and a call on an expression result
	// (`CType(x, I).BeginInit()`) are qualified by something this per-file
	// pass cannot name, so Qualifier is "" while Qualified is true.
	//
	// Measured by parsing the 302-file corpus and counting Refs with
	// Qualified true and Qualifier "": 1,624 of 41,748 use sites, 0 of which
	// satisfy IsCall.
	//
	// Without this bit those sites are byte-identical to an unqualified name
	// the table could not resolve, and an S5 author told that `Qualifier == ""`
	// means unqualified would resolve `.Foo(` against file-local declarations
	// — the confidently-wrong edge #6327 exists to prevent. Today no phantom
	// CALLS escapes only because StatementHead happens to be false for all of
	// them, which is an accident of the composition and not a guard.
	//
	// Recall cost, stated because `With` is pervasive in the WinForms code
	// #6321 is about: every `With`-block member invocation is DROPPED.
	// `.SetValue(k, v)` standing alone as a statement is a real call, and
	// IsCall reports false for it, because naming its receiver needs With-target
	// tracking this pass does not do. S5 must not infer a receiver here.
	Qualified bool
	// Kind is Table.ClassifyParen's answer, or ParenCall when the syntax
	// settles it (a `New` prefix).
	Kind ParenKind
	// New is set when the use site is preceded by the New keyword, which
	// makes it a construction whatever the table knows.
	New bool
	// Generic is set when the group opens with the Of keyword.
	Generic bool
	// Intrinsic marks a VB.NET intrinsic conversion or reflection operator
	// (CType, CInt, GetType, ...). These are call-shaped but call no
	// user-declared member; S5 must not emit a CALLS edge for them.
	Intrinsic bool
	// StatementHead is set when the use site opens its statement, possibly
	// behind a `Call`. An unknown name in statement position is a call — it
	// cannot be an index, because an index is not a statement.
	StatementHead bool
	// Scope is the dotted scope the use site sits in.
	Scope string
	// Line is the 1-based physical line of the use site, resolved through the
	// continuation map rather than reported at the statement's first line.
	Line int
}

// String renders a Ref compactly for test failure messages.
func (r Ref) String() string {
	name := r.Name
	switch {
	case r.Qualifier != "":
		name = r.Qualifier + "." + r.Name
	case r.Qualified:
		// Qualified by something this pass cannot name: rendered with a bare
		// leading dot so it can never be read as an unqualified use.
		name = "." + r.Name
	}
	s := fmt.Sprintf("%s:%s@%d", name, r.Kind, r.Line)
	if r.New {
		s += "+new"
	}
	if r.Intrinsic {
		s += "+intrinsic"
	}
	if r.StatementHead {
		s += "+head"
	}
	return s
}

// IsCall reports whether S5 should treat this use site as an invocation.
//
// The composition is here, not in the caller, because it is the one judgement
// in this package that trades precision for recall and it must be changed in
// one place. ParenCall is the table's verdict. A `New` prefix is syntactically
// decisive. A statement-position unknown is the epic's "unknown name in
// statement position is a call" rule: an index cannot stand alone as a
// statement, so the alternative is not an index but a no-op. An intrinsic is
// call-SHAPED but resolves to no declaration, so it is excluded.
func (r Ref) IsCall() bool {
	if r.Intrinsic {
		return false
	}
	if r.New || r.Kind == ParenCall {
		return true
	}
	return r.Kind == ParenUnknown && r.StatementHead
}

// Diagnostic is a place the parser could not make sense of the source.
type Diagnostic struct {
	Line    int
	Message string
}

func (d Diagnostic) String() string { return fmt.Sprintf("line %d: %s", d.Line, d.Message) }

// Result is the parse of one file.
type Result struct {
	// File is the synthetic root node.
	File *Node
	// Table is the declaration table the classification ran against.
	Table *Table
	// Diagnostics are the structural problems found. A file parses cleanly
	// when this is empty.
	Diagnostics []Diagnostic
}

// ---------------------------------------------------------------------------
// Parse
// ---------------------------------------------------------------------------

// lineRange is one physical line's byte range in the original source,
// excluding the newline and a CR before it.
type lineRange struct{ start, end int }

// indexLines maps physical lines to byte ranges in the ORIGINAL source.
//
// JoinContinuations normalises CRLF before splitting, which shifts every
// offset after the first CRLF. Spans must index what the caller holds, so the
// line index is built on the untouched string and the CR is trimmed off the
// end of each range instead.
func indexLines(src string) []lineRange {
	out := make([]lineRange, 0, strings.Count(src, "\n")+1)
	start := 0
	for i := 0; i <= len(src); i++ {
		if i == len(src) || src[i] == '\n' {
			end := i
			if end > start && src[end-1] == '\r' {
				end--
			}
			out = append(out, lineRange{start, end})
			start = i + 1
		}
	}
	return out
}

type frame struct {
	node  *Node
	ender string
	// lambda marks a frame opened by a multi-line lambda rather than by a
	// declaration. Its node is the enclosing declaration, shared rather than
	// owned, so closing it must not touch that node's span.
	lambda bool
	// property is the last property declared directly in this frame. An
	// auto-property opens no block, so accessors and `End Property` are
	// attached through it rather than through the container stack — pushing on
	// `Property` would desynchronise the stack from the first auto-property
	// onward, which is S3's recorded reason for not pushing either.
	property *Node
}

type parser struct {
	src   string
	lines []lineRange
	table *Table
	stack []*frame
	diags []Diagnostic
}

// Parse parses VB.NET source into a containment tree.
//
// It never returns nil and never panics on malformed input: a construct it
// cannot read becomes a Diagnostic, so a caller can measure how much of a real
// tree it understands rather than discovering a silent drop.
func Parse(src string) *Result {
	p := &parser{src: src, lines: indexLines(src), table: BuildTable(src)}
	root := &Node{Kind: NodeFile}
	if len(p.lines) > 0 {
		root.Span = Span{StartLine: 1, EndLine: len(p.lines), StartByte: 0, EndByte: len(src)}
	}
	p.stack = []*frame{{node: root}}

	for _, ll := range JoinContinuations(src) {
		p.walkLine(ll)
	}

	// Anything still open at EOF is a structural problem, and it is the one
	// that corrupts the most: an unclosed block swallows every later sibling.
	for i := len(p.stack) - 1; i >= 1; i-- {
		f := p.stack[i]
		if f.lambda {
			p.diag(f.node.Span.StartLine, "unclosed lambda in "+f.node.Name)
			continue
		}
		p.diag(f.node.Span.StartLine, fmt.Sprintf("unclosed %s %q", f.node.Kind, f.node.Name))
		p.closeNode(f.node, len(p.lines))
	}
	p.stack = p.stack[:1]
	p.closeNode(root, len(p.lines))
	return &Result{File: root, Table: p.table, Diagnostics: p.diags}
}

func (p *parser) diag(line int, msg string) {
	p.diags = append(p.diags, Diagnostic{Line: line, Message: msg})
}

func (p *parser) top() *frame { return p.stack[len(p.stack)-1] }

func (p *parser) scope() string { return p.top().node.Path() }

// spanOf builds a span covering physical lines [from,to], starting at the
// first non-blank byte of the first line.
func (p *parser) spanOf(from, to int) Span {
	s := Span{StartLine: from, EndLine: to}
	if from >= 1 && from <= len(p.lines) {
		r := p.lines[from-1]
		s.StartByte = r.start
		// The byte-order mark is skipped alongside indentation: the joiner
		// drops it from the text, so a span that still pointed at it would
		// slice three bytes the parser never saw.
		if strings.HasPrefix(p.src[s.StartByte:r.end], "\ufeff") {
			s.StartByte += len("\ufeff")
		}
		for s.StartByte < r.end && (p.src[s.StartByte] == ' ' || p.src[s.StartByte] == '\t') {
			s.StartByte++
		}
	}
	if to >= 1 && to <= len(p.lines) {
		s.EndByte = p.lines[to-1].end
	}
	if s.EndByte < s.StartByte {
		s.EndByte = s.StartByte
	}
	return s
}

func (p *parser) closeNode(n *Node, endLine int) {
	if endLine < n.Span.StartLine {
		endLine = n.Span.StartLine
	}
	end := p.spanOf(n.Span.StartLine, endLine)
	n.Span.EndLine = end.EndLine
	n.Span.EndByte = end.EndByte
}

// open attaches a node to the current frame and returns it.
func (p *parser) open(n *Node, startLine, endLine int) *Node {
	parent := p.top().node
	n.parent = parent
	n.Scope = parent.Path()
	n.Span = p.spanOf(startLine, endLine)
	n.HeaderSpan = p.spanOf(startLine, endLine)
	parent.Children = append(parent.Children, n)
	return n
}

func (p *parser) push(n *Node, ender string) {
	p.stack = append(p.stack, &frame{node: n, ender: ender})
}

// pop closes the innermost frame whose ender matches, plus anything left open
// inside it. An `End` word matching nothing open is reported rather than
// ignored: `End Class` with no class is exactly the desynchronisation that
// silently reparents the rest of the file.
func (p *parser) pop(ender string, line int) bool {
	for i := len(p.stack) - 1; i >= 1; i-- {
		if p.stack[i].ender != ender {
			continue
		}
		for j := len(p.stack) - 1; j > i; j-- {
			p.diag(p.stack[j].node.Span.StartLine,
				fmt.Sprintf("unclosed %s %q, closed by End %s on line %d",
					p.stack[j].node.Kind, p.stack[j].node.Name, ender, line))
			if !p.stack[j].lambda {
				p.closeNode(p.stack[j].node, line)
			}
		}
		if !p.stack[i].lambda {
			p.closeNode(p.stack[i].node, line)
		}
		p.stack = p.stack[:i]
		return true
	}
	return false
}

// blockEnders are the `End <word>` forms that close a block this parser does
// not model as a node. They must be recognised so they are not reported as
// unmatched, and they must NOT reach pop, which would unwind real containers.
var blockEnders = map[string]bool{
	"if": true, "while": true, "select": true, "try": true, "with": true,
	"using": true, "synclock": true, "namespace": true, "sub": true,
	"function": true, "operator": true, "get": true, "set": true,
	"addhandler": true, "removehandler": true, "raiseevent": true,
	"class": true, "module": true, "structure": true, "interface": true,
	"enum": true, "property": true, "event": true,
}

func (p *parser) walkLine(ll LogicalLine) {
	// Masked so a '(' or an identifier inside a literal cannot be read as
	// code; attributes blanked rather than removed so every offset in the
	// result still maps back to a physical line through ll.Pieces.
	text := BlankAttributes(MaskStringLiterals(ll.Text))
	for _, sp := range statementSpans(text) {
		stmt := strings.TrimSpace(text[sp.start:sp.end])
		if stmt == "" {
			continue
		}
		off := sp.start + indexNonSpace(text[sp.start:sp.end])
		// Masking and attribute-blanking both preserve byte length, so the
		// same offsets slice the untouched source.
		raw := ll.Text[off : off+len(stmt)]
		p.walkStatement(stmt, raw, off, ll)
		p.pushLambda(stmt)
	}
}

// pushLambda opens a frame for a multi-line lambda.
//
// `x = Sub(v)` with nothing after the parameter list starts a statement lambda
// whose `End Sub` closes the LAMBDA. Without this, that End Sub closes the
// enclosing method instead, every later member of the file is reparented, and
// the file's remaining `End`s cascade.
//
// It is the largest single failure class measured. Deleting the pushLambda
// call and re-parsing the 302-file corpus takes the clean count from 300 to
// 255 — 45 files — and the diagnostics it produces are 155 `End sub with no
// matching block`, 8 `End function with no matching block` and 7 unclosed
// accessors or methods.
//
// A single-line lambda (`Sub(x) Log(x)`) has its body on the same line and
// therefore no End, which is why the test is "nothing follows the parameter
// list" rather than "a lambda appears".
func (p *parser) pushLambda(stmt string) {
	if kw := lambdaEnder(stmt); kw != "" {
		p.stack = append(p.stack, &frame{node: p.top().node, ender: kw, lambda: true})
	}
}

// lambdaEnder returns the End keyword a multi-line lambda in stmt will be
// closed by, or "" when the statement opens no lambda block.
//
// A single-line lambda (`Sub(x) Log(x)`) has its body on the same line and
// therefore no End, which is why the test is "nothing follows the parameter
// list" rather than "a lambda appears".
//
// It is shared with BuildTable rather than duplicated: the tree walk and the
// table walk must agree about which blocks are open, or a method-body local
// after a lambda is recorded at type scope as a field — which is exactly what
// happened before this was shared (CodeEditor.vb:617, found by the
// bidirectional agreement test).
func lambdaEnder(stmt string) string {
	for i := 0; i < len(stmt); i++ {
		switch stmt[i] {
		case '"':
			i = literalEnd(stmt, i) - 1
			continue
		case '(':
		default:
			continue
		}
		kw := wordBefore(stmt, i)
		if kw != "sub" && kw != "function" {
			continue
		}
		end := matchBracket(stmt, i)
		if end < 0 {
			continue
		}
		rest := strings.TrimSpace(stmt[end+1:])
		// A multi-line Function lambda may declare its return type, and that
		// type may itself be generic (`Function() As List(Of Job)`). Only the
		// type is consumed, never everything after `As`: when the lambda sits
		// inside a `With { ... }` initialiser the joiner has already merged
		// its whole body into this one logical line, and the block needs no
		// frame because its End is inside the same line.
		if w, tail := takeWord(rest); w == "as" {
			rest = consumeTypeExpr(tail)
		}
		if rest != "" {
			continue
		}
		return kw
	}
	return ""
}

// consumeTypeExpr peels one type expression — dotted name, optional (Of ...)
// list, optional array suffixes — off the front of s and returns the rest.
func consumeTypeExpr(s string) string {
	name, rest := takeIdent(strings.TrimSpace(s))
	if name == "" {
		return strings.TrimSpace(s)
	}
	for strings.HasPrefix(rest, ".") {
		next, r := takeIdent(rest[1:])
		if next == "" {
			break
		}
		rest = r
	}
	for strings.HasPrefix(rest, "(") {
		end := matchBracket(rest, 0)
		if end < 0 {
			break
		}
		rest = strings.TrimSpace(rest[end+1:])
	}
	return strings.TrimSpace(rest)
}

func indexNonSpace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return i
		}
	}
	return 0
}

// statementSpans splits a logical line on top-level ':' separators, returning
// byte ranges so the caller keeps its offsets. It mirrors splitStatements,
// including the `name:=value` named-argument rule.
func statementSpans(s string) []span {
	var out []span
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			i = literalEnd(s, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ':':
			if depth != 0 {
				continue
			}
			if i+1 < len(s) && s[i+1] == '=' {
				i++ // named argument, not a separator
				continue
			}
			out = append(out, span{start, i})
			start = i + 1
		}
	}
	return append(out, span{start, len(s)})
}

func (p *parser) walkStatement(stmt, raw string, off int, ll LogicalLine) {
	line := ll.LineAt(off)

	// Conditional-compilation and region directives declare nothing. They are
	// skipped rather than parsed: #Region appears in 58 of the 302 corpus
	// files (counted with a case-insensitive line-anchored grep), and
	// `#End Region` reaching the End handler would unwind a real container.
	if stmt[0] == '#' {
		return
	}

	head, rest := takeWord(stmt)

	switch head {
	case "end":
		w, tail := takeWord(rest)
		if w == "" {
			return // bare `End` statement: terminates the program.
		}
		if strings.TrimSpace(tail) != "" {
			break // `End If x` is not an End statement at all.
		}
		p.walkEnd(w, line)
		return
	case "option":
		// takeIdent, not takeWord: the name is kept as spelled, because every
		// other node in this tree carries the source spelling and folding
		// only one of them would make Path() lookups inconsistent.
		name, value := takeIdent(rest)
		p.open(&Node{Kind: NodeOption, Keyword: "option", Name: name,
			Target: strings.TrimSpace(value)}, line, line)
		return
	case "imports":
		p.walkImports(rest, line, ll)
		return
	case "inherits", "implements":
		p.walkTypeClause(head, rest, line)
		return
	case "namespace":
		name := strings.TrimSpace(rest)
		if name == "" {
			p.diag(line, "Namespace with no name")
			return
		}
		n := p.open(&Node{Kind: NodeNamespace, Keyword: "namespace", Name: name}, line, line)
		p.push(n, "namespace")
		return
	}

	// An Enum body holds bare names and nothing else, so it is recognised
	// BEFORE modifiers are peeled. Three corpus files declare a member called
	// `Custom`, which is also the modifier that opens a Custom Event; peeling
	// first ate the name and left a declaration with no declarator.
	if p.top().node.Kind == NodeEnum {
		if name, tail := takeIdent(stmt); name != "" &&
			(tail == "" || strings.HasPrefix(tail, "=")) {
			n := p.open(&Node{Kind: NodeEnumMember, Keyword: "enum_member", Name: name,
				Attributes: ll.Attributes, Doc: ll.Doc}, line, line)
			n.Target = strings.TrimSpace(strings.TrimPrefix(tail, "="))
		}
		// Returns unconditionally, matching BuildTable's enum arm. An Enum
		// body holds nothing but members, so a statement that does not read as
		// one is malformed rather than a declaration of some other kind, and
		// falling through would let the declarator path invent a field inside
		// an enum. No corpus file reaches the else branch; the parity is kept
		// because the two walks are checked against each other.
		return
	}

	// Peel modifiers, then dispatch on the declaring keyword.
	mods, body := peelModifiers(stmt)
	kw, after := takeWord(body)

	switch kw {
	case "class", "module", "structure", "interface", "enum":
		p.walkType(kw, after, mods, line, ll)
		return
	case "delegate":
		p.walkDelegate(after, mods, line, ll)
		return
	case "declare":
		p.walkDeclare(raw, mods, line, ll)
		return
	case "sub", "function", "operator":
		p.walkMethod(kw, after, mods, line, ll)
		return
	case "property":
		p.walkProperty(after, mods, line, ll)
		return
	case "get", "set", "addhandler", "removehandler", "raiseevent":
		if p.walkAccessor(kw, after, line, ll) {
			return
		}
	case "event":
		p.walkEvent(after, mods, line, ll)
		return
	}

	// Variable declarators. As in BuildTable, at least one modifier is
	// required so a bare call statement `Foo(1)` is never read as one.
	if len(mods) > 0 && p.top().node.Kind.IsType() {
		p.walkFields(body, mods, line, ll, off+len(stmt)-len(body))
		return
	}
	if len(mods) > 0 {
		// A method-body local: no node, but its initialiser still holds use
		// sites, so scan from the As/= clause exactly as a field does.
		p.scanRefs(stmt, off, ll, declRefStart(body)+(len(stmt)-len(body)))
		return
	}

	// An ordinary statement. Everything in it is a use site.
	p.scanRefs(stmt, off, ll, 0)
}

func (p *parser) walkEnd(w string, line int) {
	switch w {
	case "property":
		if prop := p.top().property; prop != nil {
			p.closeNode(prop, line)
			p.top().property = nil
			return
		}
		p.diag(line, "End Property with no open property")
		return
	case "namespace", "class", "module", "structure", "interface", "enum",
		"sub", "function", "operator", "get", "set", "event",
		"addhandler", "removehandler", "raiseevent":
		if p.pop(w, line) {
			return
		}
		if blockEnders[w] {
			// `End Sub` for a MustOverride member, `End Enum` for a nested
			// type this parser did not open: report, do not unwind.
			p.diag(line, "End "+w+" with no matching block")
		}
		return
	}
	if !blockEnders[w] {
		// `End If`, `End While`, `End Try`, ... open no node here.
		return
	}
}

// peelModifiers splits leading declaration modifiers off a statement.
func peelModifiers(stmt string) (mods []string, body string) {
	body = stmt
	for {
		w, r := takeWord(body)
		if w == "" || !declModifiers[w] {
			return mods, body
		}
		mods = append(mods, w)
		body = r
	}
}

func hasMod(mods []string, want string) bool {
	for _, m := range mods {
		if m == want {
			return true
		}
	}
	return false
}

func (p *parser) walkImports(rest string, line int, ll LogicalLine) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		p.diag(line, "Imports with no target")
		return
	}
	n := &Node{Kind: NodeImport, Keyword: "imports", Name: rest, Doc: ll.Doc}
	if parts := splitTopLevel(rest, '='); len(parts) == 2 {
		if name, _ := takeIdent(parts[0]); name != "" {
			n.Name = name
			n.Target = strings.TrimSpace(parts[1])
		}
	}
	p.open(n, line, line)
}

// walkTypeClause records Inherits/Implements on the innermost open type.
//
// The clause is attached to the TYPE, never to the file. #6295/#6298 recorded
// the same misanchoring in three languages, and #6365 in two more; S5 must
// leave FromID empty so assembly stamps the owning type's id.
func (p *parser) walkTypeClause(head, rest string, line int) {
	owner := p.top().node
	if !owner.Kind.IsType() {
		p.diag(line, head+" outside a type declaration")
		return
	}
	for _, name := range splitTopLevel(strings.TrimSpace(rest), ',') {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if head == "inherits" {
			owner.Inherits = append(owner.Inherits, name)
		} else {
			owner.Implements = append(owner.Implements, name)
		}
	}
}

var typeKindByKeyword = map[string]NodeKind{
	"class": NodeClass, "module": NodeModule, "structure": NodeStructure,
	"interface": NodeInterface, "enum": NodeEnum,
}

func (p *parser) walkType(kw, after string, mods []string, line int, ll LogicalLine) {
	// `Enum Colour As Byte` names an underlying type, not a member type.
	head := after
	if kw == "enum" {
		if before, _, ok := cutKeyword(after, "As"); ok {
			head = before
		}
	}
	name, generic, tps := parseTypeHead(head)
	if name == "" {
		p.diag(line, kw+" with no name")
		return
	}
	n := p.open(&Node{
		Kind: typeKindByKeyword[kw], Keyword: kw, Name: name, Modifiers: mods,
		Generic: generic, TypeParams: tps, Attributes: ll.Attributes, Doc: ll.Doc,
	}, line, line)
	p.push(n, kw)
}

func (p *parser) walkDelegate(after string, mods []string, line int, ll LogicalLine) {
	kw, tail := takeWord(after)
	if kw != "sub" && kw != "function" {
		tail = after
	}
	name, generic, tps := parseTypeHead(tail)
	if name == "" {
		p.diag(line, "Delegate with no name")
		return
	}
	n := &Node{Kind: NodeDelegate, Keyword: "delegate", Name: name, Modifiers: mods,
		Generic: generic, TypeParams: tps, Attributes: ll.Attributes, Doc: ll.Doc}
	_, rest := takeIdent(tail)
	n.Params, rest = readParams(rest)
	if w, t := takeWord(rest); w == "as" {
		n.TypeName, n.IsArray = readType(t)
	}
	p.open(n, line, line)
}

// walkDeclare parses `Declare [Auto] Function X Lib "k32" Alias "y" (a) As T`.
//
// It runs on the RAW statement rather than the masked one, because the Lib and
// Alias names live inside the two string literals masking exists to hide. That
// is safe here and nowhere else: those are the only literals a Declare header
// can contain, and cutKeyword already skips over literals when it searches.
func (p *parser) walkDeclare(rawStmt string, mods []string, line int, ll LogicalLine) {
	_, body := peelModifiers(rawStmt)
	_, tail := takeWord(body) // drop `Declare`
	for {
		w, r := takeWord(tail)
		if w != "auto" && w != "ansi" && w != "unicode" {
			break
		}
		tail = r
	}
	kw, tail := takeWord(tail)
	if kw != "sub" && kw != "function" {
		p.diag(line, "Declare without Sub or Function")
		return
	}
	name, rest := takeIdent(tail)
	if name == "" {
		p.diag(line, "Declare with no name")
		return
	}
	n := &Node{Kind: NodeMethod, Keyword: "declare", Name: name, Modifiers: mods,
		Attributes: ll.Attributes, Doc: ll.Doc}
	if before, lib, ok := cutKeyword(rest, "Lib"); ok {
		n.Target = leadingLiteral(lib)
		rest = strings.TrimSpace(before + " " + stripLeadingLiteral(lib))
	}
	if before, alias, ok := cutKeyword(rest, "Alias"); ok {
		n.Alias = leadingLiteral(alias)
		rest = strings.TrimSpace(before + " " + stripLeadingLiteral(alias))
	}
	n.Params, rest = readParams(strings.TrimSpace(rest))
	if w, t := takeWord(rest); w == "as" {
		n.TypeName, n.IsArray = readType(t)
	}
	p.open(n, line, line)
}

// leadingLiteral returns the content of a string literal at the head of s,
// with VB.NET's "" escape collapsed to one quote.
func leadingLiteral(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '"' {
		return ""
	}
	end := literalEnd(s, 0)
	body := s[1 : end-1]
	if end-1 <= 0 || s[end-1] != '"' {
		body = s[1:end]
	}
	return strings.ReplaceAll(body, `""`, `"`)
}

func stripLeadingLiteral(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '"' {
		return s
	}
	return strings.TrimSpace(s[literalEnd(s, 0):])
}

func (p *parser) walkMethod(kw, after string, mods []string, line int, ll LogicalLine) {
	var handles, impls string
	after, handles, _ = cutKeyword(after, "Handles")
	after, impls, _ = cutKeyword(after, "Implements")
	// `Handles` may follow `Implements` in source order; cut both ways.
	if handles == "" {
		impls, handles, _ = cutKeyword(impls, "Handles")
	}

	var name, rest string
	if kw == "operator" {
		name, rest = takeOperator(after)
	} else {
		name, rest = takeIdent(after)
	}
	if name == "" {
		p.diag(line, kw+" with no name")
		return
	}
	n := &Node{Kind: NodeMethod, Keyword: kw, Name: name, Modifiers: mods,
		Attributes: ll.Attributes, Doc: ll.Doc,
		Constructor: kw == "sub" && FoldName(name) == "new"}
	if strings.HasPrefix(rest, "(") {
		if end := matchBracket(rest, 0); end > 0 {
			if w, tail := takeWord(rest[1:end]); w == "of" {
				n.Generic = true
				for _, tp := range splitTopLevel(tail, ',') {
					if id, _ := takeIdent(tp); id != "" {
						n.TypeParams = append(n.TypeParams, id)
					}
				}
				rest = strings.TrimSpace(rest[end+1:])
			}
		}
	}
	n.Params, rest = readParams(rest)
	if w, tail := takeWord(rest); w == "as" {
		n.TypeName, n.IsArray = readType(tail)
	}
	n.Handles = splitNames(handles)
	n.Implements = splitNames(impls)
	p.open(n, line, line)

	// A member with no body opens no block. Pushing one would swallow every
	// later sibling — the failure S3 recorded for auto-properties, here for
	// MustOverride members and Interface members.
	if hasMod(mods, "mustoverride") || p.top().node.Kind == NodeInterface {
		return
	}
	p.push(n, kw)
}

// takeOperator reads an operator's name, which is a symbol rather than an
// identifier (`Operator +`, `Operator <>`, `Operator CType`).
func takeOperator(s string) (name, rest string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if isIdentByte(s[0]) {
		return takeIdent(s)
	}
	i := 0
	for i < len(s) && s[i] != '(' && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func splitNames(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range splitTopLevel(s, ',') {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (p *parser) walkProperty(after string, mods []string, line int, ll LogicalLine) {
	var impls string
	after, impls, _ = cutKeyword(after, "Implements")
	name, rest := takeIdent(after)
	if name == "" {
		p.diag(line, "Property with no name")
		return
	}
	n := &Node{Kind: NodeProperty, Keyword: "property", Name: name, Modifiers: mods,
		Attributes: ll.Attributes, Doc: ll.Doc, Implements: splitNames(impls)}
	n.Params, rest = readParams(rest)
	if w, tail := takeWord(rest); w == "as" {
		n.TypeName, n.IsArray = readType(tail)
	}
	p.open(n, line, line)
	// The property is remembered rather than pushed: an auto-property has no
	// End Property, so the stack must not grow here. Its accessors and its End
	// find it through the frame.
	p.top().property = n
}

// walkAccessor attaches a Get/Set (or Custom Event accessor) body to the
// declaration it belongs to. It returns false when the keyword was not an
// accessor after all, so `Set` used as an ordinary identifier falls through.
func (p *parser) walkAccessor(kw, after string, line int, ll LogicalLine) bool {
	owner := p.top().property
	if owner == nil && p.top().node.Kind == NodeEvent {
		owner = p.top().node
	}
	if owner == nil {
		return false
	}
	saved := p.stack
	p.stack = append(p.stack, &frame{node: owner})
	n := p.open(&Node{Kind: NodeAccessor, Keyword: kw, Name: accessorName(kw)}, line, line)
	p.stack = saved
	n.Params, _ = readParams(strings.TrimSpace(after))
	p.push(n, kw)
	return true
}

func accessorName(kw string) string {
	switch kw {
	case "get":
		return "Get"
	case "set":
		return "Set"
	case "addhandler":
		return "AddHandler"
	case "removehandler":
		return "RemoveHandler"
	}
	return "RaiseEvent"
}

func (p *parser) walkEvent(after string, mods []string, line int, ll LogicalLine) {
	var impls string
	after, impls, _ = cutKeyword(after, "Implements")
	name, rest := takeIdent(after)
	if name == "" {
		p.diag(line, "Event with no name")
		return
	}
	n := &Node{Kind: NodeEvent, Keyword: "event", Name: name, Modifiers: mods,
		Attributes: ll.Attributes, Doc: ll.Doc, Implements: splitNames(impls)}
	n.Params, rest = readParams(rest)
	if w, tail := takeWord(rest); w == "as" {
		n.TypeName, _ = readType(tail)
	}
	p.open(n, line, line)
	// Only a Custom Event has a body, and therefore an End Event.
	if hasMod(mods, "custom") {
		p.push(n, "event")
	}
}

func (p *parser) walkFields(body string, mods []string, line int, ll LogicalLine, bodyOff int) {
	kind := NodeField
	keyword := "dim"
	if hasMod(mods, "const") {
		kind, keyword = NodeConst, "const"
	}
	syms := parseDeclarators(body, KindField, p.scope(), line)
	if len(syms) == 0 {
		p.diag(line, "declaration with no declarator")
		return
	}
	for _, s := range syms {
		p.open(&Node{Kind: kind, Keyword: keyword, Name: s.Name, Modifiers: mods,
			TypeName: s.TypeName, IsArray: s.IsArray,
			Attributes: ll.Attributes, Doc: ll.Doc}, line, line)
	}
	// Only the initialiser and the type clause hold use sites; the declarator
	// names themselves must not be read as calls, or every `Dim buf(10)` in
	// the file becomes a phantom CALLS edge.
	p.scanRefs(body, bodyOff, ll, declRefStart(body))
}

// declRefStart is the offset in a declarator list at which use sites begin:
// the first top-level `As` or `=`, whichever comes first. Before it are
// declarator names and array bounds, neither of which is a call.
func declRefStart(body string) int {
	best := len(body)
	depth := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '"':
			i = literalEnd(body, i) - 1
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
		if body[i] == '=' {
			return i + 1
		}
		if (body[i] == 'a' || body[i] == 'A') && i+2 <= len(body) &&
			strings.EqualFold(body[i:i+2], "as") &&
			(i == 0 || !isIdentByte(body[i-1])) &&
			(i+2 == len(body) || !isIdentByte(body[i+2])) {
			return i + 2
		}
	}
	return best
}

// readParams reads a leading parenthesised parameter list off s.
func readParams(s string) (params []Param, rest string) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return nil, s
	}
	end := matchBracket(s, 0)
	if end < 0 {
		return nil, s
	}
	for _, sym := range parseParams(s[1:end], "", 0) {
		params = append(params, Param{Name: sym.Name, TypeName: sym.TypeName, IsArray: sym.IsArray})
	}
	// Modifier flags come from the raw text of each declarator; parseParams
	// peels them, so they are re-read here rather than duplicating its loop.
	for i, raw := range splitTopLevel(s[1:end], ',') {
		if i >= len(params) {
			break
		}
		_, raw = SplitAttributes(raw)
		for {
			w, r := takeWord(raw)
			switch w {
			case "byref":
				params[i].ByRef = true
			case "optional":
				params[i].Optional = true
			case "paramarray":
				params[i].ParamArray = true
			case "byval":
			default:
				w = ""
			}
			if w == "" {
				break
			}
			raw = r
		}
		if _, def, ok := cutKeyword(raw, "="); ok {
			params[i].Default = strings.TrimSpace(def)
		}
	}
	return params, strings.TrimSpace(s[end+1:])
}
