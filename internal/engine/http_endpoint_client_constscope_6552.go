// Scope-aware JS/TS constant resolution for the bare-identifier URL fold
// (#6551 / #6552).
//
// # WHY A SECOND TABLE, ALONGSIDE buildJSConstantSymbolTable
//
// buildJSConstantSymbolTable (http_endpoint_client_synthesis.go:741) is a regex
// table: jsConstStringRe runs over RAW file text, keeps the FIRST match per
// name, never strips comments, and its declarator alternation is literally
// `(?:const|let|var)`. Its five long-standing consumers — sse_edges.go:186,
// websocket_edges.go:660, and http_endpoint_client_synthesis.go:1387, :1445,
// :1707 — only ever REFINE a path that is already partly literal, so a wrong
// binding there degrades a path they were going to emit anyway.
//
// The bare-identifier arm is different in kind: it makes the table the SOLE
// source of truth for a call that would otherwise have been an honest
// `/{dynamic}` marker. A wrong binding is then published as a confident
// runtime_dynamic=false endpoint, and the cross-stack linker FETCHES-links it
// to a route the call never makes. Four measured shapes: a commented-out
// declaration wins; an identifier declared in an unrelated class's method is
// folded at a call where it is not in scope; `let url = '/a'; url = '/b'` folds
// the stale first value; a module const shadowed by a method parameter of the
// same name folds the const.
//
// So this file adds an AST-derived table ALONGSIDE the regex one and wires it
// to the new arm ONLY. The alternative — switching all six consumers — would
// change the behaviour of five passes that are not implicated in this defect,
// on a table whose looseness they have depended on for years (the `let`/`var`
// arm in particular is load-bearing for the template-literal folds). Scoping
// the new table to the one consumer that needs it keeps the other five
// byte-identical by construction rather than by measurement.
//
// The AST fixes all four shapes structurally rather than by patching a regex:
// tree-sitter never yields a `comment` node as a declaration, a
// `lexical_declaration` exposes `const` vs `let`, and every node carries byte
// positions, so an out-of-scope or shadowed binding is a lookup rather than a
// guess.
//
// FAIL SAFE: every ambiguity — no visible binding, two bindings tying for the
// innermost scope, a non-const kind, a non-string initialiser, any assignment
// to the name anywhere in the file — resolves to "decline". The call then stays
// `/{dynamic}`, which is what `main` emitted and is honest.
package engine

import (
	"context"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// jsScopeNodeTypes are the grammar nodes that introduce a lexical scope. A
// binding is governed by the innermost such node enclosing its declaration; a
// function node (rather than its body block) is the scope for its parameters,
// so a parameter correctly shadows a module const throughout the function.
var jsScopeNodeTypes = map[string]bool{
	"program":                        true,
	"statement_block":                true,
	"function_declaration":           true,
	"function_expression":            true,
	"generator_function":             true,
	"generator_function_declaration": true,
	"arrow_function":                 true,
	"method_definition":              true,
	"class_declaration":              true,
	"class_body":                     true,
	"class":                          true,
	"for_statement":                  true,
	"for_in_statement":               true,
	"catch_clause":                   true,
	"switch_body":                    true,
}

// jsConstBinding is one name bound in one scope.
type jsConstBinding struct {
	// value is the folded string, valid only when foldable is true.
	value string
	// foldable is true only for `const NAME = '<plain string literal>'`.
	// Every other binding shape (let, var, parameter, import, destructuring,
	// function/class name, non-literal initialiser) is recorded as
	// non-foldable so it can still SHADOW an outer const.
	foldable bool
	// scopeStart/scopeEnd are the byte span of the scope this binding governs.
	scopeStart, scopeEnd uint32
}

// jsScopedConstTable is a per-file, scope-aware view of the bindings the
// bare-identifier URL fold may consult.
type jsScopedConstTable struct {
	bindings map[string][]jsConstBinding
	// assigned records every name that is the target of an assignment or an
	// update expression anywhere in the file. It is file-global on purpose: it
	// carries no scope span, so it cannot tell an assignment to THIS binding
	// from one to a same-named binding elsewhere. It adds no safety the
	// const-only rule does not already provide — only a `const` is ever
	// foldable, and valid JS cannot assign to a `const` — so it can only ever
	// suppress folds it need not have suppressed. See resolve for why it stays.
	assigned map[string]bool
}

// buildJSScopedConstTable parses content and returns the scope-aware binding
// table, or nil when the source cannot be parsed. A nil table declines every
// lookup, so a parse failure degrades to the pre-#6551 `/{dynamic}` behaviour.
func buildJSScopedConstTable(content, lang string) *jsScopedConstTable {
	tsLang := lang
	if tsLang != "javascript" && tsLang != "typescript" {
		tsLang = "typescript" // safe superset for any JS/TS-shaped input
	}
	factory := treesitter.NewParserFactory(nil)
	pr, err := factory.Parse(context.Background(), []byte(content), tsLang)
	if err != nil || pr == nil || pr.TSTree == nil {
		return nil
	}
	defer pr.TSTree.Close()

	root := pr.TSTree.RootNode()
	if root == nil {
		return nil
	}
	t := &jsScopedConstTable{
		bindings: make(map[string][]jsConstBinding),
		assigned: make(map[string]bool),
	}
	t.walk(root, content, [2]uint32{root.StartByte(), root.EndByte()})
	return t
}

// walk is a depth-first traversal that carries the enclosing scope span
// explicitly, so no Parent() chasing is needed and a function's parameters land
// in the function's own scope rather than its body block's.
func (t *jsScopedConstTable) walk(n ts.Node, src string, scope [2]uint32) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "lexical_declaration":
		t.recordDeclarators(n, src, scope, isConstDeclaration(n, src))
	case "variable_declaration":
		// `var` is function-scoped and reassignable — recorded so it can
		// shadow, never folded.
		t.recordDeclarators(n, src, scope, false)
	case "formal_parameters":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			t.recordPatternNames(n.NamedChild(i), src, scope)
		}
	case "import_specifier", "namespace_import":
		// An imported name is bound from ANOTHER file; the table is per-file,
		// so it must never fold. Recording it keeps that decline explicit.
		t.recordPatternNames(n, src, scope)
	case "function_declaration", "generator_function_declaration", "class_declaration":
		if name := n.ChildByFieldName("name"); name != nil && name.Type() == "identifier" {
			t.add(nodeTextJS(name, src), jsConstBinding{scopeStart: scope[0], scopeEnd: scope[1]})
		}
	case "assignment_expression", "augmented_assignment_expression":
		if left := n.ChildByFieldName("left"); left != nil {
			t.markAssigned(left, src)
		}
	case "update_expression":
		if arg := n.ChildByFieldName("argument"); arg != nil {
			t.markAssigned(arg, src)
		}
	}

	childScope := scope
	if jsScopeNodeTypes[n.Type()] {
		childScope = [2]uint32{n.StartByte(), n.EndByte()}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		t.walk(n.Child(i), src, childScope)
	}
}

// isConstDeclaration reports whether a lexical_declaration's keyword is `const`
// rather than `let`. The keyword is an anonymous first child of the node, so
// tree-sitter answers a question jsConstStringRe's `(?:const|let|var)`
// alternation deliberately erased.
func isConstDeclaration(n ts.Node, src string) bool {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c == nil || c.IsNamed() {
			continue
		}
		switch strings.TrimSpace(nodeTextJS(c, src)) {
		case "const":
			return true
		case "let", "var":
			return false
		}
	}
	return false
}

// recordDeclarators records every name bound by a declaration node. A name is
// foldable only when the declaration is a `const`, the bound target is a plain
// identifier (not a destructuring pattern), and the initialiser is a plain
// string literal — a `template_string` is excluded even when it has no
// substitutions, because folding it is the template arm's job, not this one's.
func (t *jsScopedConstTable) recordDeclarators(n ts.Node, src string, scope [2]uint32, isConst bool) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		d := n.NamedChild(i)
		if d == nil || d.Type() != "variable_declarator" {
			continue
		}
		name := d.ChildByFieldName("name")
		if name == nil {
			continue
		}
		if name.Type() != "identifier" {
			// Destructuring: bind every name in the pattern, none foldable.
			t.recordPatternNames(name, src, scope)
			continue
		}
		b := jsConstBinding{scopeStart: scope[0], scopeEnd: scope[1]}
		if val := d.ChildByFieldName("value"); isConst && val != nil && val.Type() == "string" {
			b.value = jsStringLiteralValue(nodeTextJS(val, src))
			b.foldable = b.value != ""
		}
		t.add(nodeTextJS(name, src), b)
	}
}

// recordPatternNames binds every identifier appearing in a binding pattern
// (parameter, destructuring target, import clause) as non-foldable, so it can
// shadow an outer const without ever supplying a value itself.
func (t *jsScopedConstTable) recordPatternNames(n ts.Node, src string, scope [2]uint32) {
	if n == nil {
		return
	}
	if n.Type() == "identifier" || n.Type() == "shorthand_property_identifier_pattern" {
		t.add(nodeTextJS(n, src), jsConstBinding{scopeStart: scope[0], scopeEnd: scope[1]})
		return
	}
	// Do not descend into a default-value expression — `f(x = PEOPLE)` binds
	// only x; the PEOPLE there is a reference, not a binding.
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		switch n.FieldNameForChild(i) {
		case "value", "right", "type":
			continue
		}
		t.recordPatternNames(c, src, scope)
	}
}

// markAssigned flags a name as reassigned. Only a bare identifier target
// matters here; a member or subscript assignment (`obj.x = …`) rebinds nothing
// this table tracks.
func (t *jsScopedConstTable) markAssigned(target ts.Node, src string) {
	if target != nil && target.Type() == "identifier" {
		t.assigned[nodeTextJS(target, src)] = true
	}
}

func (t *jsScopedConstTable) add(name string, b jsConstBinding) {
	if name == "" {
		return
	}
	t.bindings[name] = append(t.bindings[name], b)
}

// resolve returns the folded string value of name as seen from byte offset pos,
// or ("", false) when the binding cannot be established beyond doubt.
//
// The rule is deliberately narrow, and every branch that is not certain
// declines:
//
//   - no binding of the name is visible at pos → decline (the name is either
//     undefined here or declared in a sibling scope: shape D);
//   - the innermost visible scope holds more than one binding of the name →
//     decline (ambiguous, and a duplicate declaration in one scope is not
//     something a symbol table should arbitrate);
//   - the innermost visible binding is not a `const` bound to a string literal
//     → decline (shapes C and H: a `let`, a `var`, a parameter, an import —
//     shape C's `let url` is declined here, by !best.foldable, before the
//     reassignment is ever consulted);
//   - the name is assigned anywhere in the file → decline. This one is a
//     file-GLOBAL guard, deliberately not scope-aware, and it is strictly
//     redundant: `foldable` is set only for a `const`, and valid JS cannot
//     assign to a `const`. Its only reachable effect is to OVER-decline — a
//     module-level `const base` stops folding because some unrelated function
//     assigns its own `let base`. It is kept as belt-and-braces: an
//     over-decline costs a `/{dynamic}` path, which is honest, whereas a
//     mis-fold invents an endpoint that does not exist.
//
// Because it is redundant, dropping the `t.assigned` check leaves this
// package's suite green — it is an equivalent mutant under the current tests.
// That is a property of the check being a fail-safe, not evidence it is dead
// weight; do not delete it on a green run alone.
//
// Shape A needs no rule at all: a commented-out declaration is a `comment`
// node, so it never enters the table.
func (t *jsScopedConstTable) resolve(name string, pos uint32) (string, bool) {
	if t == nil || name == "" || t.assigned[name] {
		return "", false
	}
	var best jsConstBinding
	bestSpan := uint32(0)
	found, tied := false, false
	for _, b := range t.bindings[name] {
		if pos < b.scopeStart || pos >= b.scopeEnd {
			continue
		}
		span := b.scopeEnd - b.scopeStart
		switch {
		case !found || span < bestSpan:
			best, bestSpan, found, tied = b, span, true, false
		case span == bestSpan:
			tied = true
		}
	}
	if !found || tied || !best.foldable {
		return "", false
	}
	return best.value, true
}

// jsBareIdentifier reports whether expr is exactly one JS/TS identifier — no
// member access, no call, no concatenation, no whitespace-separated operator.
// `base + '/things'` and `this.url` both fail here, which is why neither
// reaches the fold. Trimming a `this.` prefix to make the latter resolve is
// precisely the permissive move this guard exists to prevent: a class field and
// a module const of the same name are different bindings.
func jsBareIdentifier(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '$':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
