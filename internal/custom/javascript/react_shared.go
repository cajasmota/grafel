package javascript

import (
	"fmt"

	"github.com/cajasmota/archigraph/internal/types"
)

// react_shared.go — shared React component + hook recognition used by the
// React-based meta-frameworks (Next.js, Remix, Gatsby) so they extract page
// components and custom/builtin hook usage with the same fidelity as the
// standalone React extractor (issue #2857, Structure group).
//
// The regexes themselves live in react.go (reReactExportFunction,
// reReactExportConst, reReactClassComponent, reReactHook,
// reReactCreateContext, reReactUseContext, reReactJSXPresence,
// reReactUseHookCall). These helpers wrap them so each meta-framework
// extractor can tag the framework name + provenance while reusing the exact
// same detection logic — no duplicated patterns.

// reactComponentSink receives an extracted entity. Each meta-framework passes
// its own dedup-aware adder.
type reactComponentSink func(ent types.EntityRecord)

// extractReactStructure runs the shared React component + hook recognition over
// src and feeds every entity to add. framework tags the emitted entities so
// downstream queries can attribute them to the host meta-framework
// (react/next/remix/gatsby). It covers:
//
//   - function / arrow / class components (PascalCase, JSX-guarded)        → SCOPE.UIComponent subtype="component"
//   - custom hook definitions (export const/function useXxx)               → SCOPE.Operation   subtype="hook"
//   - hook call sites (useState, useEffect, useMyHook, ...)                → SCOPE.Operation   subtype="hook_call"
//   - createContext + useContext                                          → SCOPE.Component/Operation
//
// It returns the set of component / hook-definition names it emitted so the
// caller can decide route-component attribution.
func extractReactStructure(src, filePath, language, framework string, add reactComponentSink) {
	hasJSX := reReactJSXPresence.MatchString(src)

	if hasJSX {
		for _, m := range reReactExportFunction.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity(name, "SCOPE.UIComponent", "component", filePath, language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "component_type", "function",
				"provenance", "INFERRED_FROM_REACT_COMPONENT")
			add(ent)
		}
		for _, m := range reReactExportConst.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity(name, "SCOPE.UIComponent", "component", filePath, language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework, "component_type", "arrow",
				"provenance", "INFERRED_FROM_REACT_COMPONENT")
			add(ent)
		}
	}

	for _, m := range reReactClassComponent.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "component_type", "class",
			"provenance", "INFERRED_FROM_REACT_CLASS_COMPONENT")
		add(ent)
	}

	// Custom hook definitions.
	for _, m := range reReactHook.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "hook", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "provenance", "INFERRED_FROM_REACT_HOOK")
		add(ent)
	}

	// Hook call sites (useXxx(...)). Proves hook_recognition for components that
	// only *consume* hooks (the common case for a page component).
	for _, m := range reReactUseHookCall.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("call:"+name, "SCOPE.Operation", "hook_call", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "hook", name,
			"builtin", fmt.Sprintf("%t", reactBuiltinHooks[name]),
			"provenance", "INFERRED_FROM_REACT_HOOK_CALL")
		add(ent)
	}

	// createContext / useContext.
	for _, m := range reReactCreateContext.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "context", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "provenance", "INFERRED_FROM_REACT_CONTEXT")
		add(ent)
	}
	for _, m := range reReactUseContext.FindAllStringSubmatchIndex(src, -1) {
		ctxName := src[m[2]:m[3]]
		ent := makeEntity("useContext:"+ctxName, "SCOPE.Operation", "context_use", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "context_name", ctxName,
			"provenance", "INFERRED_FROM_REACT_USE_CONTEXT")
		add(ent)
	}
}

// reactBuiltinHooks is the set of React-core hooks; a call to one proves the
// enclosing file is a React component / custom hook.
var reactBuiltinHooks = map[string]bool{
	"useState": true, "useEffect": true, "useContext": true, "useReducer": true,
	"useCallback": true, "useMemo": true, "useRef": true, "useImperativeHandle": true,
	"useLayoutEffect": true, "useDebugValue": true, "useTransition": true,
	"useDeferredValue": true, "useId": true, "useSyncExternalStore": true,
	"useInsertionEffect": true, "useOptimistic": true, "useActionState": true,
	"useFormStatus": true,
}
