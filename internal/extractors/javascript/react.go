// react.go — React hook recognition for the JS/TS AST extractor (issue #2854,
// Structure group, hook_recognition capability).
//
// Background: component_extraction (PascalCase function/arrow/class components)
// and HOC/context recognition already live in extractor.go (issues #610, #611,
// the forwardRef/memo/connect wrapper set). The remaining partial was
// hook_recognition — the extractor recognised Zustand/React-Query hook *calls*
// (destructure_bindings.go) but did NOT:
//
//  1. classify a *custom hook definition* (`function useFoo()` / `const useBar
//     = () => …`) as a hook entity, and
//  2. emit a USES_HOOK edge from a component / custom hook to each `useXxx()`
//     it invokes.
//
// This file closes that gap:
//   - isReactHookName     — the `use` + Uppercase convention (the same rule the
//     React linter enforces for the Rules of Hooks).
//   - extractHookCalls    — scans a function body for `useXxx(…)` call sites and
//     returns USES_HOOK edges (deduplicated).
//
// hook definitions are tagged via the subtype set in handleFunctionDeclaration /
// handleVariableDeclarator (subtype="react_hook") so grafel_find can filter
// custom hooks, and the USES_HOOK edge wires the component→hook composition
// graph that powers the Structure/hook_recognition cell.
package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// isReactHookName reports whether name follows the React hook naming
// convention: the literal prefix "use" followed by an uppercase letter
// (useState, useEffect, useMyCustomHook). A bare "use" or "used"/"user" is NOT
// a hook — the 4th rune must be uppercase.
func isReactHookName(name string) bool {
	if len(name) < 4 {
		return false
	}
	if name[0] != 'u' || name[1] != 's' || name[2] != 'e' {
		return false
	}
	c := name[3]
	return c >= 'A' && c <= 'Z'
}

// builtinReactHooks is the set of React-core hooks. Calls to these are still
// emitted as USES_HOOK edges (they prove the enclosing function is a component
// / custom hook), but a *definition* named like one of these would be the
// user re-implementing it, which we still treat as a custom hook.
var builtinReactHooks = map[string]bool{
	"useState": true, "useEffect": true, "useContext": true,
	"useReducer": true, "useCallback": true, "useMemo": true,
	"useRef": true, "useImperativeHandle": true, "useLayoutEffect": true,
	"useDebugValue": true, "useTransition": true, "useDeferredValue": true,
	"useId": true, "useSyncExternalStore": true, "useInsertionEffect": true,
	"useOptimistic": true, "useActionState": true, "useFormStatus": true,
}

// extractHookCalls scans a function body for hook call sites (`useXxx(...)`)
// and returns USES_HOOK edges from callerName to each distinct hook. Only emits
// when callerName is itself a component (PascalCase) or a custom hook
// (use-prefixed) — the contexts in which React's Rules of Hooks permit hook
// calls — so utility functions don't pick up spurious edges.
func (x *extractor) extractHookCalls(body ts.Node, callerName string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	if !isComponentName(callerName) && !isReactHookName(callerName) {
		return nil
	}
	calls := findAllNodes(body, "call_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	var rels []types.RelationshipRecord
	for _, c := range calls {
		fn := c.ChildByFieldName("function")
		if fn == nil || fn.Type() != "identifier" {
			continue
		}
		hook := x.nodeText(fn)
		if !isReactHookName(hook) || hook == callerName || seen[hook] {
			continue
		}
		seen[hook] = true
		builtin := "false"
		if builtinReactHooks[hook] {
			builtin = "true"
		}
		rels = append(rels, types.RelationshipRecord{
			ToID: hook,
			Kind: string(types.RelationshipKindUsesHook),
			Properties: map[string]string{
				"consumer":  callerName,
				"hook":      hook,
				"builtin":   builtin,
				"framework": "react",
			},
		})
	}
	return rels
}
