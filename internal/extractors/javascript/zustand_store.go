// zustand_store.go — issue #2590.
//
// Zustand store action CALLS edges
// ─────────────────────────────────
// Zustand stores are created with the `create` function from the "zustand"
// package. The store closure contains action functions (values that are
// arrow functions or function expressions) alongside plain state values.
//
//   import { create } from 'zustand'
//   export const useSyncQueueStore = create<State>((set, get) => ({
//     queue: [],                                      // plain state
//     enqueue: (item) => set(state => ({...})),       // action
//     process: async () => { /* ... */ },             // action
//   }))
//
// Call sites invoke actions via two patterns:
//
//   // Pattern A — getState() accessor (non-hook usage in plain TS):
//   const { enqueue, process } = useSyncQueueStore.getState()
//   process()
//   useSyncQueueStore.getState().markFailed(id, msg)
//
//   // Pattern B — selector hook (React component usage):
//   const process = useSyncQueueStore(s => s.process)
//   process()
//
// Both patterns are currently invisible to the graph: the extractor sees
// `process()` as a bare identifier call (landing in bug-extractor) and misses
// the CALLS edge to the action function inside the store closure.
//
// Fix: scan the file's import bindings for `create` from "zustand". When
// found, walk variable_declarator nodes at module scope to find
// `const <storeVar> = create<T>(...)` shapes and record which keys in the
// inner object literal have function values (the action set).
//
// When call sites subsequently do:
//   - `<storeVar>.getState().<action>(...)` — emit CALLS from caller to action
//   - `<storeVar>(s => s.<action>)` / selector usage — not directly a call, skip
//     (selectors only retrieve the function; the call site is a separate node)
//
// For member-expression access after getState() the trailing method name IS the
// action name, so we emit a CALLS edge with Properties["via"] = "zustand_store".
//
// All edges carry Properties["via"] = "zustand_store" so the resolver can
// classify them correctly.

package javascript

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
)

// PropViaZustandStore is the value for Properties["via"] stamped on CALLS
// edges emitted through Zustand store action detection.
const PropViaZustandStore = "zustand_store"

// zustandTracker holds the per-file state for Zustand store tracking.
//
// Built once per file by buildZustandTracker after importByLocal is populated.
// Nil when no zustand import is found (fast-path for non-Zustand files).
type zustandTracker struct {
	// storeActions maps a store variable name (e.g. "useSyncQueueStore") to
	// the set of action keys defined in its create() object literal.
	// Only function-valued keys are included; plain state values are excluded.
	storeActions map[string]map[string]bool
}

// buildZustandTracker constructs a zustandTracker from the already-populated
// importByLocal map and an AST walk to find create() assignments.
//
// Returns nil when no "zustand" import is found in the file (fast-path).
func (x *extractor) buildZustandTracker(root *sitter.Node) *zustandTracker {
	if x.importByLocal == nil {
		return nil
	}
	// Check whether any import binding comes from "zustand" with imported name
	// "create". We also accept namespace / wildcard imports of "zustand".
	hasZustandCreate := false
	zustandCreateLocals := make(map[string]bool) // local names bound to `create`
	for localName, b := range x.importByLocal {
		if b == nil {
			continue
		}
		if b.importPath != "zustand" {
			continue
		}
		// Named import `import { create } from 'zustand'` — importedName == "create"
		if b.importedName == "create" {
			hasZustandCreate = true
			zustandCreateLocals[localName] = true
		}
		// Namespace: `import * as zustand from 'zustand'` — we check member calls
		// like `zustand.create(...)` separately in the AST walk below.
		if b.importedName == "*" || b.importedName == "default" {
			hasZustandCreate = true
			zustandCreateLocals[localName] = true
		}
	}
	if !hasZustandCreate {
		return nil
	}

	t := &zustandTracker{
		storeActions: make(map[string]map[string]bool),
	}
	if root != nil {
		t.scanForStores(x, root, zustandCreateLocals)
	}
	if len(t.storeActions) == 0 {
		return nil
	}
	return t
}

// scanForStores walks the AST looking for:
//
//	const <storeVar> = create<T>((set, get) => ({ ... }))
//	const <storeVar> = zustand.create<T>((set, get) => ({ ... }))
//	export const <storeVar> = create(...)
//
// For each match it records the action keys (function-valued object properties)
// in storeActions[<storeVar>].
func (t *zustandTracker) scanForStores(x *extractor, root *sitter.Node, createLocals map[string]bool) {
	stack := make([]*sitter.Node, 0, 64)
	stack = append(stack, root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "variable_declarator" {
			t.processVariableDeclarator(x, n, createLocals)
		}
		count := int(n.ChildCount())
		for i := count - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
}

// processVariableDeclarator checks whether the declarator is a zustand create()
// call and records the action keys.
func (t *zustandTracker) processVariableDeclarator(x *extractor, n *sitter.Node, createLocals map[string]bool) {
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "identifier" {
		return
	}
	storeVar := x.nodeText(nameNode)
	if storeVar == "" {
		return
	}

	valueNode := n.ChildByFieldName("value")
	if valueNode == nil {
		return
	}

	// Unwrap call_expression — may be `create(...)` or `create<State>(...)`.
	// tree-sitter TypeScript parses `create<State>(...)` as a call_expression
	// where the function is either an identifier or a generic_call.
	callNode := unwrapToCallExpression(valueNode)
	if callNode == nil {
		return
	}
	if !t.isZustandCreateCall(x, callNode, createLocals) {
		return
	}

	// Extract the object literal from the factory arrow: create((set, get) => ({...}))
	// The first argument should be an arrow_function or function_expression.
	actions := extractStoreActions(x, callNode)
	if len(actions) == 0 {
		return
	}
	t.storeActions[storeVar] = actions
}

// unwrapToCallExpression unwraps TS generic call expressions like `create<T>(...)`
// which tree-sitter may parse as a call_expression wrapping a generic identifier.
// Returns the call_expression node or nil.
func unwrapToCallExpression(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "call_expression" {
		return n
	}
	// TypeScript: `create<State>(...)` parses as call_expression at top level.
	// Nothing to unwrap for other shapes.
	return nil
}

// isZustandCreateCall returns true when callNode is a call to a local `create`
// binding that originated from the "zustand" import.
func (t *zustandTracker) isZustandCreateCall(x *extractor, callNode *sitter.Node, createLocals map[string]bool) bool {
	fn := callNode.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	switch fn.Type() {
	case "identifier":
		// `create(...)` — direct named import.
		return createLocals[x.nodeText(fn)]
	case "member_expression":
		// `zustand.create(...)` — namespace import.
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if obj == nil || prop == nil {
			return false
		}
		if x.nodeText(prop) != "create" {
			return false
		}
		return createLocals[x.nodeText(obj)]
	case "call_expression":
		// TypeScript generic: `create<State>(...)` — some grammars parse this
		// as a subscript/generic call wrapping the base identifier.
		// Recurse once to handle: `create<T>(factory)` where outer is call.
		return t.isZustandCreateCall(x, fn, createLocals)
	}
	return false
}

// extractStoreActions extracts the action keys (function-valued properties)
// from the first argument of a create() call.
//
// Expected shape: create((set, get) => ({ key: fnVal, ... }))
// Also handles:   create((set, get) => { return { key: fnVal, ... } })
func extractStoreActions(x *extractor, createCall *sitter.Node) map[string]bool {
	args := createCall.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	// Find the first arrow_function or function_expression argument.
	var factoryNode *sitter.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "arrow_function" || ch.Type() == "function_expression" {
			factoryNode = ch
			break
		}
	}
	if factoryNode == nil {
		return nil
	}

	// The body of the factory arrow/function should be a parenthesized object
	// or contain a return statement with an object.
	body := factoryNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	// Find the object literal in the body.
	objNode := findObjectLiteral(body)
	if objNode == nil {
		return nil
	}

	return collectFunctionValuedKeys(x, objNode)
}

// findObjectLiteral searches for an object node in typical arrow-function body shapes:
//
//	(set, get) => ({ queue: [], enqueue: fn })   → parenthesized_expression → object
//	(set, get) => { return { ... } }             → statement_block → return → object
//	(set, get) => { return persist({ ... }) }    → NOT directly an object (ignored)
func findObjectLiteral(body *sitter.Node) *sitter.Node {
	if body == nil {
		return nil
	}
	switch body.Type() {
	case "object":
		return body
	case "parenthesized_expression":
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch != nil && ch.Type() == "object" {
				return ch
			}
		}
	case "statement_block":
		// Search for `return { ... }` as the first return statement.
		for i := 0; i < int(body.ChildCount()); i++ {
			stmt := body.Child(i)
			if stmt == nil || stmt.Type() != "return_statement" {
				continue
			}
			for j := 0; j < int(stmt.ChildCount()); j++ {
				ch := stmt.Child(j)
				if ch != nil && ch.Type() == "object" {
					return ch
				}
				// Parenthesized return: `return ({ ... })`
				if ch != nil && ch.Type() == "parenthesized_expression" {
					for k := 0; k < int(ch.ChildCount()); k++ {
						inner := ch.Child(k)
						if inner != nil && inner.Type() == "object" {
							return inner
						}
					}
				}
			}
		}
	}
	return nil
}

// collectFunctionValuedKeys returns the set of property keys in an object
// literal whose values are arrow functions or function expressions.
// Plain state values (arrays, primitives, identifiers that are not functions)
// are excluded so we don't emit spurious CALLS edges.
func collectFunctionValuedKeys(x *extractor, obj *sitter.Node) map[string]bool {
	actions := make(map[string]bool)
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		valNode := pair.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		if !isFunctionNode(valNode) {
			continue
		}
		key := x.nodeText(keyNode)
		key = strings.Trim(key, `"'`+"`")
		if key != "" {
			actions[key] = true
		}
	}
	if len(actions) == 0 {
		return nil
	}
	return actions
}

// isFunctionNode returns true when the node is an arrow_function or
// function_expression (i.e., a function-valued property → an action).
func isFunctionNode(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	return n.Type() == "arrow_function" || n.Type() == "function_expression"
}

// zustandGetStateActionEdges checks whether callNode is a chained
// `<storeVar>.getState().<action>(...)` call expression. When the trailing
// method matches an action in the store's action set, it returns a CALLS
// RelationshipRecord tagged with Properties["via"] = "zustand_store".
//
// Handled chain shapes:
//
//	useSyncQueueStore.getState().markFailed(id, msg)
//	useSyncQueueStore.getState().process()
//
// The outer call_expression has a function of type member_expression whose
// object is itself a call_expression (getState() call).
func (t *zustandTracker) zustandGetStateActionEdges(x *extractor, callNode *sitter.Node, callerName string) []types.RelationshipRecord {
	if t == nil {
		return nil
	}
	fn := callNode.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return nil
	}
	// The property is the action name.
	actionProp := fn.ChildByFieldName("property")
	if actionProp == nil {
		return nil
	}
	actionName := x.nodeText(actionProp)
	if actionName == "" {
		return nil
	}

	// The object of the member_expression should be the getState() call.
	objNode := fn.ChildByFieldName("object")
	if objNode == nil {
		return nil
	}

	// Resolve: is objNode a `<storeVar>.getState()` call?
	storeVar := t.resolveGetStateCall(x, objNode)
	if storeVar == "" {
		// Also try one level deeper — some code does:
		//   const store = useSyncQueueStore.getState()
		//   store.markFailed(...)
		// That pattern requires variable tracking we don't do here; skip.
		return nil
	}

	actions, ok := t.storeActions[storeVar]
	if !ok || !actions[actionName] {
		return nil
	}

	// Emit a CALLS edge from callerName → actionName.
	rel := types.RelationshipRecord{
		ToID: actionName,
		Kind: "CALLS",
		Properties: map[string]string{
			"via": PropViaZustandStore,
		},
	}
	return []types.RelationshipRecord{rel}
}

// resolveGetStateCall returns the store variable name when node is a call
// of the form `<storeVar>.getState()`. Returns "" otherwise.
func (t *zustandTracker) resolveGetStateCall(x *extractor, node *sitter.Node) string {
	if node == nil || node.Type() != "call_expression" {
		return ""
	}
	fn := node.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return ""
	}
	prop := fn.ChildByFieldName("property")
	if prop == nil || x.nodeText(prop) != "getState" {
		return ""
	}
	obj := fn.ChildByFieldName("object")
	if obj == nil || obj.Type() != "identifier" {
		return ""
	}
	candidate := x.nodeText(obj)
	if _, ok := t.storeActions[candidate]; ok {
		return candidate
	}
	return ""
}

// zustandSelectorActionEdges checks whether callNode is of the form
// `<storeVar>(s => s.<action>)()` — a selector call that extracts an action
// function and immediately invokes it. This pattern is:
//
//	const process = useSyncQueueStore(s => s.process)
//	process()   ← separate call — NOT detected here
//
//	useSyncQueueStore(s => s.process)()  ← immediately-invoked selector
//
// For the immediately-invoked form the outer call_expression has a function
// that is itself a call_expression (the selector hook call). We detect:
//
//	outer: `<inner>()`  where inner = `<storeVar>(s => s.<action>)`
//
// We emit a CALLS edge from callerName → actionName with via=zustand_store.
func (t *zustandTracker) zustandSelectorActionEdges(x *extractor, callNode *sitter.Node, callerName string) []types.RelationshipRecord {
	if t == nil {
		return nil
	}
	fn := callNode.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	// The function of the outer call must be a parenthesized call or call_expression
	// (the selector invocation).
	var innerCall *sitter.Node
	switch fn.Type() {
	case "call_expression":
		innerCall = fn
	case "parenthesized_expression":
		for i := 0; i < int(fn.ChildCount()); i++ {
			ch := fn.Child(i)
			if ch != nil && ch.Type() == "call_expression" {
				innerCall = ch
				break
			}
		}
	}
	if innerCall == nil {
		return nil
	}

	// innerCall should be `<storeVar>(s => s.<action>)`.
	selectorFn := innerCall.ChildByFieldName("function")
	if selectorFn == nil || selectorFn.Type() != "identifier" {
		return nil
	}
	storeVar := x.nodeText(selectorFn)
	actions, ok := t.storeActions[storeVar]
	if !ok {
		return nil
	}

	// Find the selector argument: should be an arrow function `s => s.<action>`.
	args := innerCall.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	actionName := ""
	for i := 0; i < int(args.ChildCount()); i++ {
		arg := args.Child(i)
		if arg == nil || arg.Type() != "arrow_function" {
			continue
		}
		// The body should be a member_expression `s.<action>`.
		body := arg.ChildByFieldName("body")
		if body == nil || body.Type() != "member_expression" {
			continue
		}
		propNode := body.ChildByFieldName("property")
		if propNode == nil {
			continue
		}
		candidate := x.nodeText(propNode)
		if actions[candidate] {
			actionName = candidate
			break
		}
	}
	if actionName == "" {
		return nil
	}

	rel := types.RelationshipRecord{
		ToID: actionName,
		Kind: "CALLS",
		Properties: map[string]string{
			"via": PropViaZustandStore,
		},
	}
	return []types.RelationshipRecord{rel}
}
