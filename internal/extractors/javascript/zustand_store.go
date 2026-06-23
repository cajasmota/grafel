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
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
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

	// storeActionNodes maps a store variable name to a map from action key to
	// the AST node of the function value. Populated alongside storeActions so
	// that emitStoreActionEntities can derive source-line ranges for each action
	// method entity (issue #2626).
	storeActionNodes map[string]map[string]ts.Node

	// storePartializeFields maps a store variable name to the list of field
	// names that appear in the partialize config (issue #2626).
	// Populated by processVariableDeclarator when a second argument to create()
	// contains a "partialize" key.
	storePartializeFields map[string][]string
}

// buildZustandTracker constructs a zustandTracker from the already-populated
// importByLocal map and an AST walk to find create() assignments.
//
// Returns nil when no "zustand" import is found in the file (fast-path).
func (x *extractor) buildZustandTracker(root ts.Node) *zustandTracker {
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
		storeActions:          make(map[string]map[string]bool),
		storeActionNodes:      make(map[string]map[string]ts.Node),
		storePartializeFields: make(map[string][]string),
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
func (t *zustandTracker) scanForStores(x *extractor, root ts.Node, createLocals map[string]bool) {
	stack := make([]ts.Node, 0, 64)
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
func (t *zustandTracker) processVariableDeclarator(x *extractor, n ts.Node, createLocals map[string]bool) {
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
	actions, actionNodes := extractStoreActionsWithNodes(x, callNode)
	if len(actions) == 0 {
		return
	}
	t.storeActions[storeVar] = actions
	t.storeActionNodes[storeVar] = actionNodes

	// Issue #2626 — extract partialize fields from the optional second argument
	// to create(): create(factory, { name: 'auth', partialize: (s) => ({ user: s.user }) })
	if fields := extractPartializeFields(x, callNode); len(fields) > 0 {
		t.storePartializeFields[storeVar] = fields
	}
}

// unwrapToCallExpression unwraps TS generic call expressions like `create<T>(...)`
// which tree-sitter may parse as a call_expression wrapping a generic identifier.
// Returns the call_expression node or nil.
func unwrapToCallExpression(n ts.Node) ts.Node {
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
func (t *zustandTracker) isZustandCreateCall(x *extractor, callNode ts.Node, createLocals map[string]bool) bool {
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

// extractStoreActionsWithNodes extracts the action keys (function-valued
// properties) from the first argument of a create() call, returning both the
// bool set (used by call-site detection) and a map from action name to the
// function-value AST node (used by emitStoreActionEntities to derive line
// ranges). Issue #2626.
//
// Expected shape: create((set, get) => ({ key: fnVal, ... }))
// Also handles:   create((set, get) => { return { key: fnVal, ... } })
//
// Issue #2646 — also handles the Zustand middleware (curried) pattern:
//
//	create<State>()(persist((set, get) => ({ ... })))
//	create<State>()(devtools((set, get) => ({ ... }), opts))
//	create<State>()(immer((set) => ({ ... })))
//
// When the first arg to the create() call is itself a call_expression
// (a middleware wrapper like persist/devtools/immer), we unwrap one level
// to find the factory arrow/function_expression inside the middleware call.
func extractStoreActionsWithNodes(x *extractor, createCall ts.Node) (map[string]bool, map[string]ts.Node) {
	args := createCall.ChildByFieldName("arguments")
	if args == nil {
		return nil, nil
	}
	// Find the first arrow_function or function_expression argument.
	// If the first non-punctuation arg is a call_expression (middleware wrapper),
	// unwrap one level to find the factory inside that middleware call.
	var factoryNode ts.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "arrow_function" || ch.Type() == "function_expression" {
			factoryNode = ch
			break
		}
		// Issue #2646 — Zustand middleware pattern:
		// create()(persist(factory, config)) or create()(devtools(factory))
		// The first arg is a call_expression wrapping the real factory.
		if ch.Type() == "call_expression" {
			factoryNode = extractFactoryFromMiddlewareCall(x, ch)
			if factoryNode != nil {
				break
			}
		}
	}
	if factoryNode == nil {
		return nil, nil
	}

	// The body of the factory arrow/function should be a parenthesized object
	// or contain a return statement with an object.
	body := factoryNode.ChildByFieldName("body")
	if body == nil {
		return nil, nil
	}

	// Find the object literal in the body.
	objNode := findObjectLiteral(body)
	if objNode == nil {
		return nil, nil
	}

	return collectFunctionValuedKeysWithNodes(x, objNode)
}

// extractFactoryFromMiddlewareCall handles the Zustand middleware pattern
// (issue #2646):
//
//	persist((set, get) => ({ ... }), config)
//	devtools((set, get) => ({ ... }), opts)
//	immer((set) => ({ ... }))
//	subscribeWithSelector(immer((set) => ({ ... })))  ← nested middleware
//
// It looks at the first argument of the middleware call_expression for an
// arrow_function or function_expression (the factory). When the first arg is
// itself a call_expression (nested middleware), it recurses one level.
//
// Returns the factory node or nil when the shape is not recognised.
func extractFactoryFromMiddlewareCall(_ *extractor, middlewareCall ts.Node) ts.Node {
	if middlewareCall == nil || middlewareCall.Type() != "call_expression" {
		return nil
	}
	args := middlewareCall.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "arrow_function" || ch.Type() == "function_expression" {
			return ch
		}
		// Nested middleware: persist(devtools(factory)) — recurse once.
		if ch.Type() == "call_expression" {
			if inner := extractFactoryFromMiddlewareCall(nil, ch); inner != nil {
				return inner
			}
		}
	}
	return nil
}

// extractPartializeFields extracts the field names from the partialize function
// in the optional second argument of create():
//
//	create(factory, { name: 'auth', partialize: (s) => ({ user: s.user, token: s.token }) })
//
// Returns nil when no partialize config is found. Issue #2626.
func extractPartializeFields(x *extractor, createCall ts.Node) []string {
	args := createCall.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	// The second non-comma, non-factory argument should be an object literal
	// containing the store config (name, partialize, etc.).
	factorySkipped := false
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "," {
			continue
		}
		if ch.Type() == "arrow_function" || ch.Type() == "function_expression" {
			factorySkipped = true
			continue
		}
		if !factorySkipped {
			continue
		}
		if ch.Type() != "object" {
			continue
		}
		// Found the config object — look for a "partialize" key.
		for j := 0; j < int(ch.ChildCount()); j++ {
			pair := ch.Child(j)
			if pair == nil || pair.Type() != "pair" {
				continue
			}
			keyNode := pair.ChildByFieldName("key")
			if keyNode == nil {
				continue
			}
			key := strings.Trim(x.nodeText(keyNode), `"'`+"`")
			if key != "partialize" {
				continue
			}
			// The value should be an arrow_function or function_expression
			// returning an object with the fields to keep.
			valNode := pair.ChildByFieldName("value")
			if valNode == nil {
				continue
			}
			return collectPartializeReturnKeys(x, valNode)
		}
		break
	}
	return nil
}

// collectPartializeReturnKeys collects the object keys returned by the
// partialize arrow: (s) => ({ user: s.user, token: s.token }) → ["user", "token"].
func collectPartializeReturnKeys(x *extractor, fnNode ts.Node) []string {
	if fnNode == nil {
		return nil
	}
	if fnNode.Type() != "arrow_function" && fnNode.Type() != "function_expression" {
		return nil
	}
	body := fnNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	objNode := findObjectLiteral(body)
	if objNode == nil {
		return nil
	}
	var fields []string
	for i := 0; i < int(objNode.ChildCount()); i++ {
		pair := objNode.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		keyNode := pair.ChildByFieldName("key")
		if keyNode == nil {
			continue
		}
		field := strings.Trim(x.nodeText(keyNode), `"'`+"`")
		if field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

// findObjectLiteral searches for an object node in typical arrow-function body shapes:
//
//	(set, get) => ({ queue: [], enqueue: fn })   → parenthesized_expression → object
//	(set, get) => { return { ... } }             → statement_block → return → object
//	(set, get) => { return persist({ ... }) }    → NOT directly an object (ignored)
func findObjectLiteral(body ts.Node) ts.Node {
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

// collectFunctionValuedKeysWithNodes returns the set of property keys in an
// object literal whose values are arrow functions or function expressions,
// alongside a map from key to the function-value AST node (for line ranges).
// Plain state values (arrays, primitives, identifiers that are not functions)
// are excluded so we don't emit spurious CALLS edges. Issue #2626.
func collectFunctionValuedKeysWithNodes(x *extractor, obj ts.Node) (map[string]bool, map[string]ts.Node) {
	actions := make(map[string]bool)
	nodes := make(map[string]ts.Node)
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
			nodes[key] = valNode
		}
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions, nodes
}

// emitStoreActionEntities emits a SCOPE.Operation entity for every action
// method found in every Zustand store in this file. This is the fix for
// issue #2626: without standalone entities for store actions, the graph's
// CALLS adjacency has no outgoing edges FROM the store actions, so BFS
// (grafel_traces action=follow) terminates at useAuthStore instead of
// entering the closure body.
//
// Each entity gets:
//   - Kind = "SCOPE.Operation", Subtype = "method"
//   - SourceFile / StartLine / EndLine from the function-value node
//   - Properties["via"] = "zustand_store" to flag origin
//   - Properties["store"] = storeVar so callers can group by store
//
// partialize_fields are stamped on the store-variable entity via a synthetic
// property on the first emitted action (there is no separate store entity in
// the current architecture; the property lives on the tracker and is wired
// via Properties["partialize_fields"] on each action entity of that store).
func (t *zustandTracker) emitStoreActionEntities(x *extractor) {
	if t == nil {
		return
	}
	for storeVar, actionNames := range t.storeActions {
		nodeMap := t.storeActionNodes[storeVar]
		partFields := strings.Join(t.storePartializeFields[storeVar], ",")
		for actionName := range actionNames {
			var fnNode ts.Node
			if nodeMap != nil {
				fnNode = nodeMap[actionName]
			}
			// Issue #2631 — qualify the entity name with the store variable so
			// two stores sharing an action name (e.g. useAuthStore.logout and
			// useAdminStore.logout) produce distinct entity IDs.
			// Format: <storeVar>::<actionName>  (mirrors class method convention).
			qualifiedName := storeVar + "::" + actionName
			props := map[string]string{
				"kind":        "SCOPE.Operation",
				"subtype":     "method",
				"via":         PropViaZustandStore,
				"store":       storeVar,
				"action_name": actionName,
			}
			if partFields != "" {
				props["partialize_fields"] = partFields
			}
			sig := storeVar + "." + actionName
			if fnNode != nil {
				x.emitWithProps(qualifiedName, "SCOPE.Operation", fnNode, "method", sig, props, nil)
			}
			// When fnNode is nil (should not happen in practice; storeActionNodes
			// is always co-populated with storeActions), skip silently: the
			// CALLS edge still resolves if another extractor pass emits the action.
		}
	}
}

// isFunctionNode returns true when the node is an arrow_function or
// function_expression (i.e., a function-valued property → an action).
func isFunctionNode(n ts.Node) bool {
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
func (t *zustandTracker) zustandGetStateActionEdges(x *extractor, callNode ts.Node, callerName string) []types.RelationshipRecord {
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

	// Issue #2631 — use the qualified ID (<storeVar>::<actionName>) so the CALLS
	// edge resolves to the correct entity when multiple stores share an action name.
	rel := types.RelationshipRecord{
		ToID: storeVar + "::" + actionName,
		Kind: "CALLS",
		Properties: map[string]string{
			"via":  PropViaZustandStore,
			"line": strconv.Itoa(int(callNode.StartPoint().Row) + 1),
		},
	}
	return []types.RelationshipRecord{rel}
}

// resolveGetStateCall returns the store variable name when node is a call
// of the form `<storeVar>.getState()`. Returns "" otherwise.
func (t *zustandTracker) resolveGetStateCall(x *extractor, node ts.Node) string {
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
func (t *zustandTracker) zustandSelectorActionEdges(x *extractor, callNode ts.Node, callerName string) []types.RelationshipRecord {
	if t == nil {
		return nil
	}
	fn := callNode.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	// The function of the outer call must be a parenthesized call or call_expression
	// (the selector invocation).
	var innerCall ts.Node
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

	// Issue #2631 — qualify the ToID with the store name to match the entity ID
	// emitted by emitStoreActionEntities.
	rel := types.RelationshipRecord{
		ToID: storeVar + "::" + actionName,
		Kind: "CALLS",
		Properties: map[string]string{
			"via":  PropViaZustandStore,
			"line": strconv.Itoa(int(callNode.StartPoint().Row) + 1),
		},
	}
	return []types.RelationshipRecord{rel}
}
