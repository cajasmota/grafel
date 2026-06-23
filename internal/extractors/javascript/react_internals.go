// react_internals.go — React framework-specific idiom recognition for the
// JS/TS AST extractor (issue #2875, framework_specific["React Internals"]).
//
// Scope: the three genuine React idioms that no generic capability column
// expresses. hooks (Structure/hook_recognition), context + HOC
// (context_extraction + hoc_wrapper_recognition) are deliberately NOT handled
// here — they are already measured by the generic Structure group, and adding
// them again would double-count. See the React Internals group in
// docs/coverage/registry.json.
//
//  1. lazy_code_splitting — `const X = lazy(() => import('./mod'))`. The lazy
//     wrapper is already classified as a SCOPE.Operation by
//     isFunctionWrapperCall (extractor.go). Here we additionally decorate that
//     entity with react_lazy="true" and lazy_module="<import specifier>" so the
//     code-split point (the dynamically imported module) is queryable.
//
//  2. suspense_error_boundary — a component that renders <Suspense> is a
//     suspense boundary; a class component that declares componentDidCatch or
//     a static getDerivedStateFromError is an error boundary. We decorate the
//     enclosing component entity with react_suspense="true" /
//     react_error_boundary="true".
//
//  3. portal_recognition — a component whose body calls createPortal(...) (or
//     ReactDOM.createPortal(...)) renders a portal. We decorate the component
//     entity with react_portal="true".
//
// All three DECORATE existing entities (SCOPE.Operation function/arrow
// components, SCOPE.Component classes) rather than emitting a new entity Kind —
// per the #2839 prefer-decorate discipline. No new EntityKind / RelationshipKind
// is introduced.
package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// stampProp sets a property on the most-recently-emitted entity (the one a
// component/wrapper emit site just appended). It is a no-op when no entity was
// appended (name=="" / "?"), keeping the call sites unconditional.
func (x *extractor) stampLastEntityProp(key, val string) {
	if len(x.entities) == 0 {
		return
	}
	e := &x.entities[len(x.entities)-1]
	if e.Properties == nil {
		e.Properties = map[string]string{}
	}
	e.Properties[key] = val
}

// decorateReactComponentInternals inspects a component function body for the
// Suspense / portal idioms and stamps the matching properties on the entity at
// the supplied index. idx must point at the SCOPE.Operation entity the caller
// just emitted for this component.
func (x *extractor) decorateReactComponentInternals(body ts.Node, idx int) {
	if body == nil || idx < 0 || idx >= len(x.entities) {
		return
	}
	suspense := x.bodyRendersSuspense(body)
	portal := x.bodyUsesCreatePortal(body)
	if !suspense && !portal {
		return
	}
	e := &x.entities[idx]
	if e.Properties == nil {
		e.Properties = map[string]string{}
	}
	if suspense {
		e.Properties["react_suspense"] = "true"
	}
	if portal {
		e.Properties["react_portal"] = "true"
	}
}

// bodyRendersSuspense reports whether the function body contains a JSX
// <Suspense> element (opening or self-closing). React.Suspense (member
// expression) is also matched.
func (x *extractor) bodyRendersSuspense(body ts.Node) bool {
	jsxNodes := findAllNodes(body, "jsx_opening_element", "jsx_self_closing_element")
	for _, jx := range jsxNodes {
		nameNode := jx.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		tag := x.nodeText(nameNode)
		// Match both bare `Suspense` and member `React.Suspense`.
		if tag == "Suspense" || tag == "React.Suspense" {
			return true
		}
	}
	return false
}

// bodyUsesCreatePortal reports whether the function body calls createPortal —
// either bare `createPortal(...)` (named import) or member
// `ReactDOM.createPortal(...)` / `reactDom.createPortal(...)`.
func (x *extractor) bodyUsesCreatePortal(body ts.Node) bool {
	calls := findAllNodes(body, "call_expression")
	for _, c := range calls {
		if x.calleeLeaf(c) == "createPortal" {
			return true
		}
	}
	return false
}

// calleeLeaf returns the leaf identifier of a call_expression's callee: the
// bare identifier, or the property of a member-expression callee. Returns ""
// for shapes it cannot reduce (e.g. computed-member callees).
func (x *extractor) calleeLeaf(call ts.Node) string {
	if call == nil || call.Type() != "call_expression" {
		return ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "type_identifier":
		return x.nodeText(fn)
	case "member_expression":
		if prop := fn.ChildByFieldName("property"); prop != nil {
			return x.nodeText(prop)
		}
	}
	return ""
}

// lazyImportModule returns the import specifier of a React.lazy split point —
// the module string inside `lazy(() => import('./mod'))`. Returns "" when
// valueNode is not a lazy(...) wrapper or the dynamic import specifier cannot be
// recovered as a static or template-literal specifier. Used to decorate the
// lazy wrapper entity with its code-split target (lazy_code_splitting).
//
// Supported specifier forms (issue #2958):
//   - String literal:      import('./SettingsPanel')  → "./SettingsPanel"
//   - Pure template:       import(`./SettingsPanel`)  → "./SettingsPanel"
//   - Interpolated tmpl:   import(`./panels/${name}`) → "./panels/{*}"
//   - Computed/call expr:  import(getPath())          → "" (unresolvable)
func (x *extractor) lazyImportModule(valueNode ts.Node) string {
	if x.calleeLeaf(valueNode) != "lazy" {
		return ""
	}
	// Find the dynamic import call inside the lazy() argument: tree-sitter
	// models `import('m')` as a call_expression whose function child is the
	// `import` keyword node.
	for _, c := range findAllNodes(valueNode, "call_expression") {
		fn := c.ChildByFieldName("function")
		if fn == nil || fn.Type() != "import" {
			continue
		}
		args := c.ChildByFieldName("arguments")
		if args == nil {
			continue
		}
		for i := 0; i < int(args.ChildCount()); i++ {
			arg := args.Child(i)
			if arg == nil {
				continue
			}
			switch arg.Type() {
			case "string":
				return trimStringQuotes(x.nodeText(arg))
			case "template_string", "template_literal":
				// Normalize ${…} interpolations to {*} sentinel — same pattern
				// used by normalizeTemplateLiteralRoute in navigation.go.
				return normalizeTemplateLiteralRoute(x.nodeText(arg))
			}
		}
	}
	return ""
}

// isLazyWrapper reports whether valueNode is a React.lazy(…) call — i.e. the
// callee leaf is "lazy". Used by extractor.go to unconditionally stamp
// react_lazy=true on lazy wrapper entities regardless of whether the import
// specifier is resolvable (issue #2958).
func (x *extractor) isLazyWrapper(valueNode ts.Node) bool {
	return x.calleeLeaf(valueNode) == "lazy"
}

// classIsErrorBoundary reports whether a class body declares the React error
// boundary contract: an instance componentDidCatch method or a static
// getDerivedStateFromError method.
func (x *extractor) classIsErrorBoundary(body ts.Node) bool {
	if body == nil {
		return false
	}
	for _, m := range findAllNodes(body, "method_definition") {
		nameNode := m.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		switch x.nodeText(nameNode) {
		case "componentDidCatch", "getDerivedStateFromError":
			return true
		}
	}
	return false
}

// trimStringQuotes strips a single pair of surrounding ' or " or ` quotes.
func trimStringQuotes(s string) string {
	if len(s) >= 2 {
		first := s[0]
		last := s[len(s)-1]
		if (first == '\'' || first == '"' || first == '`') && last == first {
			return s[1 : len(s)-1]
		}
	}
	return s
}
