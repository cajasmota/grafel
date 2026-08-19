package vbnet

import "strings"

// SymbolKind is what a declared name *is*.
//
// The kinds are not decoration. VB.NET's grammar makes InvocationExpression
// and IndexExpression the same production — `Foo(1)` is a call, an array
// index, a default-property access or a generic instantiation, and nothing in
// the token stream says which. The only thing that can decide is knowing what
// Foo was declared as, which is what these kinds record. Each one below states
// the disambiguation it carries.
type SymbolKind int

const (
	// KindUnknown is the zero value; no symbol has it.
	KindUnknown SymbolKind = iota
	// KindNamespace scopes everything below it. Needed so a qualified use site
	// (Ns.Type.Member) can be split from a member access on a value.
	KindNamespace
	// KindType is Class, Module, Structure, Interface, Enum or Delegate.
	// `Foo(Of T)` on a type is a generic instantiation; `Foo(x)` on a type is
	// a conversion or a constructor, never an array index.
	KindType
	// KindTypeParameter is the T in `Class Foo(Of T)`. Without it, `T(0)`
	// inside the class body has no declaration and falls to Unknown.
	KindTypeParameter
	// KindMethod is Sub, Function, Operator or Declare. `Foo(1)` on a method
	// is unambiguously a CALL — the single most valuable fact in the table.
	KindMethod
	// KindProperty is a property, possibly parameterised. `Items(3)` on a
	// property is a call-shaped access, not an array index, and VB's Default
	// property makes this common.
	KindProperty
	// KindField is a member variable. `buf(3)` on an array-typed field is an
	// INDEX; emitting a CALLS edge there is the phantom-edge failure #6327
	// exists to avoid.
	KindField
	// KindConst is a constant. Never callable and never indexable, so a
	// `Const(` use site is a parse error rather than an ambiguity.
	KindConst
	// KindLocal is a Dim/For/Using/Catch local. Shadows a same-named field
	// inside its method, so the index-vs-call answer can differ per scope.
	KindLocal
	// KindParameter is a method or property parameter. Same index-vs-call role
	// as KindLocal, and the only declaration site for a signature's names.
	KindParameter
	// KindEvent is an event. `RaiseEvent Foo(...)` and `Handles x.Foo` both
	// need the name to resolve to an event rather than to a method.
	KindEvent
	// KindEnumMember is a member of an Enum body. Never takes parentheses, so
	// it turns a would-be ambiguity into a definite "neither".
	KindEnumMember
	// KindImportAlias is the X in `Imports X = A.B.C`. Without it a use site
	// spelled X.Foo cannot be resolved back to A.B.C.Foo.
	KindImportAlias
)

var symbolKindNames = map[SymbolKind]string{
	KindUnknown:       "unknown",
	KindNamespace:     "namespace",
	KindType:          "type",
	KindTypeParameter: "type_parameter",
	KindMethod:        "method",
	KindProperty:      "property",
	KindField:         "field",
	KindConst:         "const",
	KindLocal:         "local",
	KindParameter:     "parameter",
	KindEvent:         "event",
	KindEnumMember:    "enum_member",
	KindImportAlias:   "import_alias",
}

func (k SymbolKind) String() string {
	if s, ok := symbolKindNames[k]; ok {
		return s
	}
	return "unknown"
}

// Symbol is one declared name in one file.
type Symbol struct {
	// Name is the identifier as it was spelled at the declaration.
	Name string
	// Kind is what it is.
	Kind SymbolKind
	// TypeName is the declared type as written, or "" when undeclared.
	TypeName string
	// IsArray is true for `Dim a(10)`, `Dim a()` and `As Integer()`.
	IsArray bool
	// Generic is true for a declaration carrying an (Of ...) list.
	Generic bool
	// Scope is the dotted container path, "" at file level.
	Scope string
	// Target is the right-hand side of an `Imports X = A.B` alias.
	Target string
	// Line is the 1-based physical line the declaration starts on.
	Line int
}

// Table is the per-file declaration table.
type Table struct {
	byName map[string][]*Symbol
	order  []*Symbol
}

func newTable() *Table {
	return &Table{byName: map[string][]*Symbol{}}
}

func (t *Table) add(s *Symbol) {
	if s == nil || s.Name == "" {
		return
	}
	key := FoldName(s.Name)
	t.byName[key] = append(t.byName[key], s)
	t.order = append(t.order, s)
}

// Lookup returns every symbol declared under name, case-insensitively.
func (t *Table) Lookup(name string) []*Symbol {
	if t == nil {
		return nil
	}
	return t.byName[FoldName(name)]
}

// All returns every symbol in declaration order.
func (t *Table) All() []*Symbol {
	if t == nil {
		return nil
	}
	return t.order
}

// Len is the number of declared symbols.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return len(t.order)
}

// visibleFrom reports whether a symbol declared in scope decl is in scope at
// use site scope. "" (file level) is visible everywhere; otherwise decl must
// be use or a dotted ancestor of it.
func visibleFrom(decl, use string) bool {
	if decl == "" {
		return true
	}
	if strings.EqualFold(decl, use) {
		return true
	}
	return len(use) > len(decl) &&
		strings.EqualFold(use[:len(decl)], decl) &&
		use[len(decl)] == '.'
}

// Resolve picks the declaration of name that governs a use site in scope.
// The innermost enclosing declaration wins, which is how a local shadows a
// same-named field.
func (t *Table) Resolve(name, scope string) *Symbol {
	var best *Symbol
	for _, s := range t.Lookup(name) {
		if !visibleFrom(s.Scope, scope) {
			continue
		}
		if best == nil || len(s.Scope) > len(best.Scope) {
			best = s
		}
	}
	if best != nil {
		return best
	}
	// Out of scope but declared in this file: better than nothing, and the
	// caller can see the Scope mismatch on the returned symbol.
	if all := t.Lookup(name); len(all) > 0 {
		return all[0]
	}
	return nil
}

// ParenKind is what a '(' following a name means.
type ParenKind int

const (
	// ParenUnknown means the table cannot decide. Callers should emit with
	// reduced confidence rather than guess — a confidently wrong CALLS edge is
	// worse than an uncertain one (#6327).
	ParenUnknown ParenKind = iota
	// ParenIndex is an array index or an indexed value access.
	ParenIndex
	// ParenCall is an invocation.
	ParenCall
	// ParenGeneric is a generic type-argument list, `Foo(Of T)`.
	ParenGeneric
)

var parenKindNames = map[ParenKind]string{
	ParenUnknown: "unknown",
	ParenIndex:   "index",
	ParenCall:    "call",
	ParenGeneric: "generic",
}

func (p ParenKind) String() string {
	if s, ok := parenKindNames[p]; ok {
		return s
	}
	return "unknown"
}

// ClassifyParen decides what `name(` means at a use site in scope.
//
// ofKeyword must be true when the text after the '(' begins with the `Of`
// keyword, which settles the question before the table is consulted.
func (t *Table) ClassifyParen(name, scope string, ofKeyword bool) ParenKind {
	if ofKeyword {
		return ParenGeneric
	}
	sym := t.Resolve(name, scope)
	if sym == nil {
		return ParenUnknown
	}
	// Resolve falls back to any same-named declaration in the file when none
	// is in scope. That fallback is a hint, not an answer: `Items(3) = 1` in
	// class B would otherwise be classified as a call to A.Items — an
	// assignment target reported as an invocation. S4 acts on a ParenCall
	// confidently, so out of scope is exactly the case ParenUnknown exists
	// for.
	if !visibleFrom(sym.Scope, scope) {
		return ParenUnknown
	}
	switch sym.Kind {
	case KindMethod, KindEvent, KindProperty:
		return ParenCall
	case KindType, KindTypeParameter:
		// A bare `SomeType(x)` without Of is a conversion or a constructor
		// invocation; either way it is call-shaped, never an index.
		return ParenCall
	case KindField, KindLocal, KindParameter, KindConst:
		if sym.IsArray {
			return ParenIndex
		}
		// A non-array value with a '(' is a Default-property access or a
		// delegate invocation. Both exist; the table cannot tell them apart,
		// and saying so is the point.
		return ParenUnknown
	case KindEnumMember, KindNamespace, KindImportAlias:
		return ParenUnknown
	}
	return ParenUnknown
}

// ---------------------------------------------------------------------------
// Declaration walk
// ---------------------------------------------------------------------------

// declModifiers are the words that may precede a declaration keyword. Peeling
// them is what lets one code path handle `Dim x`, `Public Shared x` and
// `Private ReadOnly WithEvents x`.
var declModifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "friend": true,
	"shared": true, "shadows": true, "overloads": true, "overrides": true,
	"overridable": true, "mustoverride": true, "notoverridable": true,
	"partial": true, "mustinherit": true, "notinheritable": true,
	"readonly": true, "writeonly": true, "default": true, "static": true,
	"withevents": true, "async": true, "iterator": true, "narrowing": true,
	"widening": true, "dim": true, "const": true, "global": true,
	"custom": true, "protectedfriend": true,
}

// containerEnders maps a block-opening keyword to the word its `End` carries.
var containerEnders = map[string]bool{
	"namespace": true, "class": true, "module": true, "structure": true,
	"interface": true, "enum": true, "sub": true, "function": true,
	"property": true, "operator": true, "get": true, "set": true,
	"event": true,
}

type container struct {
	name  string
	ender string
	kind  SymbolKind
}

// BuildTable runs the pre-pass over src and records every declaration it
// finds. It emits no entities and reads no configuration: it is a pure
// function of the source text, which is what makes it testable on its own.
func BuildTable(src string) *Table {
	t := newTable()
	var stack []container

	scopeOf := func() string {
		parts := make([]string, 0, len(stack))
		for _, c := range stack {
			parts = append(parts, c.name)
		}
		return strings.Join(parts, ".")
	}
	push := func(name, ender string, kind SymbolKind) {
		stack = append(stack, container{name: name, ender: ender, kind: kind})
	}
	pop := func(ender string) {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].ender == ender {
				stack = stack[:i]
				return
			}
		}
		// Tolerant: `End If`, `End While`, `End Try` open no container.
	}
	innerKind := func() SymbolKind {
		if len(stack) == 0 {
			return KindUnknown
		}
		return stack[len(stack)-1].kind
	}

	for _, ll := range JoinContinuations(src) {
		code := MaskStringLiterals(ll.Code)
		for _, stmt := range splitStatements(code) {
			walkStatement(t, stmt, ll.Line, scopeOf, push, pop, innerKind, &stack)
		}
	}
	return t
}

// splitStatements splits a logical line on top-level ':' separators. A label
// (`Cleanup:`) yields an empty tail, which the walker ignores.
//
// The ContainsRune guard is a fast path only, and deliberately not covered by
// a test: with no ':' anywhere, splitTopLevel returns the whole string as its
// single element and walkStatement trims it, so removing the guard cannot
// change any answer. A mutation that deletes it is an equivalent mutant.
func splitStatements(code string) []string {
	if !strings.ContainsRune(code, ':') {
		return []string{code}
	}
	var out []string
	for _, part := range splitTopLevel(code, ':') {
		// `name:=value` is a named argument, not a separator; splitTopLevel
		// cut it, so stitch a leading '=' back onto the previous piece.
		if strings.HasPrefix(part, "=") && len(out) > 0 {
			out[len(out)-1] += ":" + part
			continue
		}
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{code}
	}
	return out
}

func walkStatement(
	t *Table,
	stmt string,
	line int,
	scopeOf func() string,
	push func(name, ender string, kind SymbolKind),
	pop func(ender string),
	innerKind func() SymbolKind,
	stack *[]container,
) {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return
	}

	head, rest := takeWord(stmt)

	if head == "end" {
		w, _ := takeWord(rest)
		if containerEnders[w] {
			pop(w)
		}
		return
	}
	if head == "imports" {
		parseImports(t, rest, line)
		return
	}
	if head == "namespace" {
		name := strings.TrimSpace(rest)
		if name != "" {
			t.add(&Symbol{Name: name, Kind: KindNamespace, Scope: scopeOf(), Line: line})
			push(name, "namespace", KindNamespace)
		}
		return
	}
	// Locals introduced by control-flow headers.
	switch head {
	case "for":
		parseForHeader(t, rest, scopeOf(), line)
		return
	case "using":
		// `Using a As New X, b As New Y` declares one local per resource.
		for _, res := range splitTopLevel(rest, ',') {
			parseAsClauseLocal(t, res, scopeOf(), line)
		}
		return
	case "catch":
		parseAsClauseLocal(t, rest, scopeOf(), line)
		return
	case "inherits", "implements":
		return // S5 owns these edges; the table only needs not to choke.
	}

	// Peel modifiers.
	mods := map[string]bool{}
	body := stmt
	for {
		w, r := takeWord(body)
		if w == "" || !declModifiers[w] {
			break
		}
		mods[w] = true
		body = r
	}
	sawModifier := len(mods) > 0

	kw, after := takeWord(body)
	scope := scopeOf()

	switch kw {
	case "class", "module", "structure", "interface", "enum":
		name, generic, tps := parseTypeHead(after)
		if name == "" {
			return
		}
		t.add(&Symbol{Name: name, Kind: KindType, TypeName: kw, Generic: generic, Scope: scope, Line: line})
		push(name, kw, KindType)
		inner := scopeOf()
		for _, tp := range tps {
			t.add(&Symbol{Name: tp, Kind: KindTypeParameter, Scope: inner, Line: line})
		}
		return

	case "delegate":
		// `Delegate Sub Handler(args)` declares a TYPE, and opens no block.
		_, tail := takeWord(after)
		name, generic, _ := parseTypeHead(tail)
		if name != "" {
			t.add(&Symbol{Name: name, Kind: KindType, TypeName: "delegate", Generic: generic, Scope: scope, Line: line})
		}
		return

	case "declare":
		// `Declare [Auto] Function X Lib "y" Alias "z" (args) As T`
		tail := after
		for {
			w, r := takeWord(tail)
			if w != "auto" && w != "ansi" && w != "unicode" {
				break
			}
			tail = r
		}
		_, tail = takeWord(tail)
		if name, _ := takeIdent(tail); name != "" {
			t.add(&Symbol{Name: name, Kind: KindMethod, Scope: scope, Line: line})
		}
		return

	case "sub", "function", "operator":
		// A MustOverride member, a Declare and an Interface member have no
		// body and therefore no `End Sub`. Pushing one would desynchronise the
		// container stack for the whole rest of the file.
		bodyless := mods["mustoverride"] || enclosingIsInterface(*stack)
		parseMethod(t, kw, after, scope, line, bodyless, push, scopeOf)
		return

	case "property":
		parseProperty(t, after, scope, line)
		return

	case "get", "set":
		// Accessors, not the property, are what open a block. An
		// auto-property (`Public Property Title As String`) has no
		// `End Property`, so pushing on Property would desynchronise the
		// container stack from the first auto-property in the file onward —
		// and auto-properties are ubiquitous. `End Property` therefore pops
		// nothing, which the tolerant pop already allows.
		push(kw, kw, KindProperty)
		if strings.HasPrefix(after, "(") {
			if end := matchBracket(after, 0); end > 0 {
				for _, p := range parseParams(after[1:end], scopeOf(), line) {
					t.add(p)
				}
			}
		}
		return

	case "event":
		name, tail := takeIdent(after)
		if name == "" {
			return
		}
		sym := &Symbol{Name: name, Kind: KindEvent, Scope: scope, Line: line}
		t.add(sym)
		if strings.HasPrefix(tail, "(") {
			if end := matchBracket(tail, 0); end > 0 {
				push(name, "event", KindEvent)
				for _, p := range parseParams(tail[1:end], scopeOf(), line) {
					t.add(p)
				}
				*stack = (*stack)[:len(*stack)-1]
			}
		}
		return
	}

	// Enum members: a bare name inside an Enum body.
	if innerKind() == KindType && enclosingIsEnum(*stack) && !sawModifier {
		if name, tail := takeIdent(body); name != "" && (tail == "" || strings.HasPrefix(tail, "=")) {
			t.add(&Symbol{Name: name, Kind: KindEnumMember, Scope: scope, Line: line})
		}
		return
	}

	// Variable declarators. Requires at least one modifier (Dim, Const, an
	// access modifier, ...) so that a bare call statement `Foo(1)` is never
	// mistaken for a declaration.
	if !sawModifier {
		return
	}
	kind := KindField
	switch {
	case mods["const"]:
		kind = KindConst
	case innerKind() == KindMethod || innerKind() == KindProperty:
		kind = KindLocal
	}
	for _, s := range parseDeclarators(body, kind, scope, line) {
		t.add(s)
	}
}

func enclosingIsInterface(stack []container) bool {
	if len(stack) == 0 {
		return false
	}
	return stack[len(stack)-1].ender == "interface"
}

func enclosingIsEnum(stack []container) bool {
	if len(stack) == 0 {
		return false
	}
	return stack[len(stack)-1].ender == "enum"
}

// parseImports records `Imports X = A.B.C` aliases. A plain
// `Imports System.Text` binds no new local name, so nothing is recorded.
func parseImports(t *Table, rest string, line int) {
	parts := splitTopLevel(rest, '=')
	if len(parts) != 2 {
		return
	}
	name, _ := takeIdent(parts[0])
	target := strings.TrimSpace(parts[1])
	if name == "" || target == "" {
		return
	}
	t.add(&Symbol{Name: name, Kind: KindImportAlias, Target: target, Line: line})
}

// parseTypeHead reads `Name(Of T, U)` and returns the name, whether it was
// generic, and the type-parameter names.
func parseTypeHead(s string) (name string, generic bool, typeParams []string) {
	name, rest := takeIdent(s)
	if name == "" || !strings.HasPrefix(rest, "(") {
		return name, false, nil
	}
	end := matchBracket(rest, 0)
	if end < 0 {
		return name, false, nil
	}
	inner := strings.TrimSpace(rest[1:end])
	w, tail := takeWord(inner)
	if w != "of" {
		return name, false, nil
	}
	for _, p := range splitTopLevel(tail, ',') {
		// `Of T As IComparable` — the constraint is not a parameter name.
		if tp, _ := takeIdent(p); tp != "" {
			typeParams = append(typeParams, tp)
		}
	}
	return name, true, typeParams
}

func parseMethod(
	t *Table, kw, after, scope string, line int, bodyless bool,
	push func(name, ender string, kind SymbolKind), scopeOf func() string,
) {
	after, _, _ = cutKeyword(after, "Handles")
	after, _, _ = cutKeyword(after, "Implements")

	name, rest := takeIdent(after)
	if name == "" {
		return
	}
	generic := false
	if strings.HasPrefix(rest, "(") {
		if end := matchBracket(rest, 0); end > 0 {
			if w, _ := takeWord(rest[1:end]); w == "of" {
				generic = true
				rest = strings.TrimSpace(rest[end+1:])
			}
		}
	}
	var params string
	if strings.HasPrefix(rest, "(") {
		if end := matchBracket(rest, 0); end > 0 {
			params = rest[1:end]
			rest = strings.TrimSpace(rest[end+1:])
		}
	}
	ret := ""
	if w, tail := takeWord(rest); w == "as" {
		ret = strings.TrimSpace(tail)
	}
	t.add(&Symbol{Name: name, Kind: KindMethod, TypeName: ret, Generic: generic, Scope: scope, Line: line})

	// A bodyless member opens no container, so scopeOf() would put its
	// parameters in the enclosing type. They belong to the method either way:
	// leaving them at type scope makes an interface's parameter names visible
	// to every other member of the type.
	paramScope := scope + "." + name
	if scope == "" {
		paramScope = name
	}
	if !bodyless {
		push(name, kw, KindMethod)
		paramScope = scopeOf()
	}
	for _, p := range parseParams(params, paramScope, line) {
		t.add(p)
	}
}

func parseProperty(t *Table, after, scope string, line int) {
	after, _, _ = cutKeyword(after, "Implements")
	name, rest := takeIdent(after)
	if name == "" {
		return
	}
	var params string
	if strings.HasPrefix(rest, "(") {
		if end := matchBracket(rest, 0); end > 0 {
			params = rest[1:end]
			rest = strings.TrimSpace(rest[end+1:])
		}
	}
	typeName := ""
	isArray := false
	if w, tail := takeWord(rest); w == "as" {
		typeName, isArray = readType(tail)
	}
	t.add(&Symbol{Name: name, Kind: KindProperty, TypeName: typeName, IsArray: isArray, Scope: scope, Line: line})
	// Parameters of an indexed property are scoped to the property by name
	// even though the property opens no container.
	propScope := name
	if scope != "" {
		propScope = scope + "." + name
	}
	for _, p := range parseParams(params, propScope, line) {
		t.add(p)
	}
}

// parseParams reads a parameter list body (no surrounding parentheses).
func parseParams(list, scope string, line int) []*Symbol {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil
	}
	var out []*Symbol
	for _, p := range splitTopLevel(list, ',') {
		if p == "" {
			continue
		}
		_, p = SplitAttributes(p)
		for {
			w, r := takeWord(p)
			if w != "byval" && w != "byref" && w != "optional" && w != "paramarray" {
				break
			}
			p = r
		}
		// Strip a default value; its own '=' must not be read as a declarator.
		if before, _, ok := cutKeyword(p, "="); ok {
			p = before
		} else if i := strings.IndexByte(p, '='); i >= 0 {
			p = strings.TrimSpace(p[:i])
		}
		name, rest := takeIdent(p)
		if name == "" {
			continue
		}
		sym := &Symbol{Name: name, Kind: KindParameter, Scope: scope, Line: line}
		if strings.HasPrefix(rest, "(") {
			if end := matchBracket(rest, 0); end > 0 {
				sym.IsArray = true
				rest = strings.TrimSpace(rest[end+1:])
			}
		}
		if w, tail := takeWord(rest); w == "as" {
			ty, arr := readType(tail)
			sym.TypeName = ty
			sym.IsArray = sym.IsArray || arr
		}
		out = append(out, sym)
	}
	return out
}

// parseDeclarators reads `a, b(10) As Integer, c As String`.
//
// VB.NET lets one `As` govern several names: in `Dim a, b As Integer` both are
// Integer. Declarators without their own As therefore inherit the type of the
// next declarator that has one.
func parseDeclarators(body string, kind SymbolKind, scope string, line int) []*Symbol {
	var out []*Symbol
	pending := []*Symbol{}
	for _, d := range splitTopLevel(body, ',') {
		if d == "" {
			continue
		}
		name, rest := takeIdent(d)
		if name == "" || declModifiers[FoldName(name)] {
			continue
		}
		sym := &Symbol{Name: name, Kind: kind, Scope: scope, Line: line}
		if strings.HasPrefix(rest, "(") {
			// `Dim a(10)` and `Dim a()` are both arrays.
			if end := matchBracket(rest, 0); end > 0 {
				sym.IsArray = true
				rest = strings.TrimSpace(rest[end+1:])
			}
		}
		if w, tail := takeWord(rest); w == "as" {
			ty, arr := readType(tail)
			sym.TypeName = ty
			sym.IsArray = sym.IsArray || arr
			for _, p := range pending {
				p.TypeName = ty
				p.IsArray = p.IsArray || arr
			}
			pending = pending[:0]
		} else {
			pending = append(pending, sym)
		}
		out = append(out, sym)
	}
	return out
}

// readType reads the type after `As`, dropping a `New`, an initialiser and any
// `With`/`From` block, and reports whether it is an array type.
//
// The array test is what separates `As Integer()` (array) from
// `As List(Of Integer)` (generic): an array suffix's parentheses hold nothing
// but commas.
func readType(s string) (typeName string, isArray bool) {
	s = strings.TrimSpace(s)
	sawNew := false
	if w, tail := takeWord(s); w == "new" {
		sawNew = true
		s = strings.TrimSpace(tail)
	}
	if before, _, ok := cutKeyword(s, "With"); ok {
		s = before
	}
	if before, _, ok := cutKeyword(s, "From"); ok {
		s = before
	}
	if i := strings.IndexByte(s, '='); i >= 0 {
		if before, _, ok := cutKeyword(s, "="); ok {
			s = before
		} else {
			s = strings.TrimSpace(s[:i])
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	// Walk trailing bracket groups; an array suffix contains only commas.
	for strings.HasSuffix(s, ")") {
		open := strings.LastIndexByte(s, '(')
		if open < 0 {
			break
		}
		end := matchBracket(s, open)
		if end != len(s)-1 {
			break
		}
		inner := strings.TrimSpace(s[open+1 : end])
		if inner != "" && strings.Trim(inner, ", ") != "" {
			// (Of T) is part of the type name and stays. A constructor's
			// argument list is not: `As New StreamReader(path)` names the type
			// StreamReader.
			if w, _ := takeWord(inner); sawNew && w != "of" {
				s = strings.TrimSpace(s[:open])
				continue
			}
			break // (Of T) or (bounds) — not an array suffix.
		}
		isArray = true
		s = strings.TrimSpace(s[:open])
	}
	return s, isArray
}

// parseForHeader records the loop variable of `For i As Integer = ...` and
// `For Each item As Foo In coll`.
func parseForHeader(t *Table, rest, scope string, line int) {
	if w, tail := takeWord(rest); w == "each" {
		rest = tail
	}
	if before, _, ok := cutKeyword(rest, "In"); ok {
		rest = before
	}
	if before, _, ok := cutKeyword(rest, "To"); ok {
		rest = before
	}
	parseAsClauseLocal(t, rest, scope, line)
}

// parseAsClauseLocal records `name As [New] Type` from a Using/Catch/For
// header. A header that redeclares nothing (`Using conn`) records nothing.
func parseAsClauseLocal(t *Table, rest, scope string, line int) {
	name, tail := takeIdent(rest)
	if name == "" {
		return
	}
	sym := &Symbol{Name: name, Kind: KindLocal, Scope: scope, Line: line}
	if strings.HasPrefix(tail, "(") {
		if end := matchBracket(tail, 0); end > 0 {
			sym.IsArray = true
			tail = strings.TrimSpace(tail[end+1:])
		}
	}
	w, after := takeWord(tail)
	if w != "as" {
		return
	}
	ty, arr := readType(after)
	sym.TypeName = ty
	sym.IsArray = sym.IsArray || arr
	t.add(sym)
}
