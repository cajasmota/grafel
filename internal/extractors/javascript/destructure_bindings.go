// destructure_bindings.go — issue #2625.
//
// CALLS edges via destructured store/query bindings
// ──────────────────────────────────────────────────
// PR #2595 (Zustand store CALLS edges) handles the chained-access form:
//   useSyncQueueStore.getState().markFailed(id, msg)
//
// But the more common React/Zustand pattern is destructuring first, then calling:
//
//   // Form 1: destructured from hook selector
//   const { softLogout, login } = useAuthStore();
//   softLogout();   ← must produce CALLS edge to softLogout
//
//   // Form 2: destructured from getState()
//   const { markFailed } = useSyncQueueStore.getState();
//   markFailed();   ← same problem
//
//   // Form 3: destructured from useQuery / useMutation
//   const { mutate } = useMutation({ mutationFn: ... });
//   mutate(payload); ← needs edge to mutate target
//
//   // Form 4: destructured from imported function (verify)
//   import { foo } from './bar';
//   foo();   ← may already work via import resolution
//
// Fix: build a per-file destructureBindings table at extraction time. For each
// const/let with an object_pattern LHS, record:
//   - localName  — the identifier introduced into scope (e.g. "softLogout")
//   - sourceTarget — the resolved ToID for CALLS edges (e.g. "softLogout")
//   - via        — the classification tag for Properties["via"]
//
// When extractCallRelationships encounters a call_expression whose callee is a
// bare identifier matching a destructured binding, it emits a CALLS edge to
// sourceTarget with Properties["via"]=via.

package javascript

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
)

// PropViaDestructuredBinding is the Properties["via"] value stamped on CALLS
// edges resolved through the destructure-binding table.
const PropViaDestructuredBinding = "destructured_binding"

// destructureBinding describes one local binding introduced by an object-pattern
// destructuring statement.
type destructureBinding struct {
	// localName is the identifier bound in the current file scope (e.g. "softLogout").
	localName string

	// sourceTarget is the ToID to use for a CALLS edge when localName() is called.
	// For Zustand hooks this is the action key (e.g. "softLogout").
	// For React Query this is the result-field name (e.g. "mutate").
	// For imported functions it mirrors the exported symbol name.
	sourceTarget string

	// via is the Properties["via"] classification tag.
	via string
}

// buildDestructureBindings scans the given AST node (file root OR a function
// body) for const/let declarations with an object_pattern LHS and builds a map
// from localName to the resolved binding.  Only declarations whose RHS matches a
// recognised source pattern are recorded; everything else is skipped to avoid
// spurious edges.
//
// Recognised source patterns:
//  1. useXxxStore() / useXxxStore(s => ...) — Zustand hook selector
//  2. useXxxStore.getState() — Zustand store getState() destructuring
//  3. useQuery(...) / useMutation(...) / useInfiniteQuery(...) — TanStack / React Query
//  4. Bare identifier call where the callee is in importByLocal — generic imported fn
//
// The scan is a full BFS over the node tree so it finds bindings inside nested
// blocks (if/try/catch/for) within a function body.  It does NOT recurse into
// nested function bodies (arrow_function / function_expression / function_declaration)
// to avoid collecting bindings that are not visible at the caller's scope level.
//
// Returns nil when no relevant bindings are found (fast-path).
func (x *extractor) buildDestructureBindings(scope *sitter.Node) map[string]*destructureBinding {
	if scope == nil {
		return nil
	}

	result := make(map[string]*destructureBinding)

	stack := make([]*sitter.Node, 0, 32)
	stack = append(stack, scope)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		// Scan variable declarations for destructured bindings.
		if n.Type() == "lexical_declaration" || n.Type() == "variable_declaration" {
			for j := 0; j < int(n.ChildCount()); j++ {
				decl := n.Child(j)
				if decl == nil || decl.Type() != "variable_declarator" {
					continue
				}
				x.scanDestructureDeclarator(decl, result)
			}
		}
		// Push children, skipping nested function bodies so we don't
		// collect bindings that are out-of-scope for the current function.
		nodeType := n.Type()
		isNestedFn := (nodeType == "arrow_function" || nodeType == "function_expression" ||
			nodeType == "function_declaration") && n != scope
		if isNestedFn {
			continue
		}
		for i := int(n.ChildCount()) - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// scanDestructureDeclarator processes a single variable_declarator node.
// If the LHS is an object_pattern and the RHS matches a known source pattern,
// it registers one destructureBinding per property in the object pattern.
func (x *extractor) scanDestructureDeclarator(decl *sitter.Node, out map[string]*destructureBinding) {
	nameNode := decl.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "object_pattern" {
		return
	}
	valueNode := decl.ChildByFieldName("value")
	if valueNode == nil {
		return
	}

	via, calleeBase := x.classifyDestructureRHS(valueNode)
	if via == "" {
		return
	}

	// Collect local names from the object_pattern.
	// Handles:
	//   { softLogout, login }       — shorthand_property_identifier_pattern: localName == exportedName
	//   { mutate: doCreate }        — pair_pattern: localName = "doCreate", original = "mutate"
	//   { mutate: createAddr }      — pair_pattern with identifier value
	for i := 0; i < int(nameNode.ChildCount()); i++ {
		prop := nameNode.Child(i)
		if prop == nil {
			continue
		}
		switch prop.Type() {
		case "shorthand_property_identifier_pattern", "shorthand_property_identifier", "identifier":
			// { softLogout } — local name == source name
			localName := x.nodeText(prop)
			if localName == "" {
				continue
			}
			target := localName
			if calleeBase != "" {
				target = calleeBase + "." + localName
			}
			out[localName] = &destructureBinding{
				localName:    localName,
				sourceTarget: target,
				via:          via,
			}
		case "pair_pattern":
			// { mutate: doCreate } — rename: key is original name, value is local name
			keyNode := prop.ChildByFieldName("key")
			valNode := prop.ChildByFieldName("value")
			if keyNode == nil || valNode == nil {
				continue
			}
			originalName := x.nodeText(keyNode)
			originalName = strings.Trim(originalName, `"'`+"`")
			localName := x.nodeText(valNode)
			if originalName == "" || localName == "" {
				continue
			}
			target := originalName
			if calleeBase != "" {
				target = calleeBase + "." + originalName
			}
			out[localName] = &destructureBinding{
				localName:    localName,
				sourceTarget: target,
				via:          via,
			}
		case "pair":
			// Legacy shape in some JS grammars
			keyNode := prop.ChildByFieldName("key")
			valNode := prop.ChildByFieldName("value")
			if keyNode == nil || valNode == nil {
				continue
			}
			originalName := strings.Trim(x.nodeText(keyNode), `"'`+"`")
			localName := x.nodeText(valNode)
			if originalName == "" || localName == "" {
				continue
			}
			target := originalName
			if calleeBase != "" {
				target = calleeBase + "." + originalName
			}
			out[localName] = &destructureBinding{
				localName:    localName,
				sourceTarget: target,
				via:          via,
			}
		}
	}
}

// classifyDestructureRHS examines the RHS of a destructuring declaration and
// returns (via, calleeBase). via is empty when the RHS is not a recognised pattern.
//
// calleeBase is a non-empty prefix to prepend to each bound name (e.g. the store
// variable name for zustand) when synthesising the sourceTarget.  For Zustand we
// return calleeBase="" so the action name is used directly (it is already unique
// within the store namespace in practice).  For React Query fields like "mutate",
// the local field name is itself the canonical target.
func (x *extractor) classifyDestructureRHS(value *sitter.Node) (via, calleeBase string) {
	if value == nil {
		return "", ""
	}

	switch value.Type() {
	case "call_expression":
		fnNode := value.ChildByFieldName("function")
		if fnNode == nil {
			return "", ""
		}

		// Check for <storeVar>.getState() pattern:
		//   const { markFailed } = useSyncQueueStore.getState();
		// fnNode is a member_expression: obj=useSyncQueueStore, prop=getState
		if fnNode.Type() == "member_expression" {
			propNode := fnNode.ChildByFieldName("property")
			objNode := fnNode.ChildByFieldName("object")
			if propNode != nil && objNode != nil && x.nodeText(propNode) == "getState" {
				objName := x.nodeText(objNode)
				if isZustandHookName(objName) {
					return PropViaZustandStore, ""
				}
			}
		}

		callee := x.calleeLeafName(fnNode)
		// Zustand hook selector: useXxxStore(...) where name starts with "use" + capital + "Store"
		if isZustandHookName(callee) {
			return PropViaZustandStore, ""
		}
		// React Query hooks
		if isReactQueryHook(callee) {
			return "react_query", ""
		}
		// Generic: any imported call whose callee is in importByLocal — treat as
		// destructured_binding so call sites get linked.
		if _, ok := x.importByLocal[callee]; ok && callee != "" {
			return PropViaDestructuredBinding, ""
		}
		return "", ""
	}

	return "", ""
}

// calleeLeafName extracts the trailing identifier name from a function expression node.
// For `identifier` nodes returns the name directly.
// For `member_expression` nodes returns the property name.
func (x *extractor) calleeLeafName(fn *sitter.Node) string {
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "type_identifier", "property_identifier":
		return x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			return x.nodeText(prop)
		}
	}
	return ""
}

// isZustandHookName returns true when name looks like a Zustand store hook:
// starts with "use" followed by an uppercase letter and contains "Store" or ends with "Store".
// We also accept the general use* pattern when it matches a known store variable from the
// zustandTracker.  This is a heuristic; exact matching via zustandTracker is the primary path.
func isZustandHookName(name string) bool {
	if len(name) <= 3 {
		return false
	}
	if !strings.HasPrefix(name, "use") || name[3] < 'A' || name[3] > 'Z' {
		return false
	}
	// Must contain "Store" to distinguish from generic hooks (useQuery, useMutation, etc.)
	return strings.Contains(name, "Store")
}

// isReactQueryHook returns true when name is a TanStack / React Query hook that
// returns a result object containing callable fields (mutate, refetch, fetchNextPage, etc.).
func isReactQueryHook(name string) bool {
	switch name {
	case "useQuery", "useMutation", "useInfiniteQuery",
		"useSuspenseQuery", "useSuspenseInfiniteQuery",
		"usePrefetchQuery", "useIsFetching", "useIsMutating":
		return true
	}
	return false
}

// resolveCalleeViaBindings looks up a bare callee identifier in the
// destructure-binding table. On a hit it returns the resolved sourceTarget
// and a CALLS RelationshipRecord. Returns ("", false) on a miss.
func resolveCalleeViaBindings(callee string, bindings map[string]*destructureBinding) (*destructureBinding, bool) {
	if bindings == nil {
		return nil, false
	}
	b, ok := bindings[callee]
	return b, ok
}

// destructureBindingCallEdge synthesises a CALLS RelationshipRecord for a
// bare callee that matches a destructured binding. Returns nil when there
// is no match.
func destructureBindingCallEdge(callee string, bindings map[string]*destructureBinding) *types.RelationshipRecord {
	b, ok := resolveCalleeViaBindings(callee, bindings)
	if !ok {
		return nil
	}
	rel := types.RelationshipRecord{
		ToID: b.sourceTarget,
		Kind: "CALLS",
		Properties: map[string]string{
			"via": b.via,
		},
	}
	return &rel
}
