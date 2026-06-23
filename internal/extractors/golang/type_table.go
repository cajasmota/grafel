// type_table.go — per-function local-variable type tracking for the
// Go references pass (#1840).
//
// Background. Issue #1839's disposition breakdown on grafel showed
// `same_package_unqualified` is the dominant remaining bucket — 24,697
// edges, 67% of all unresolved Go edges. The top-10 unresolved roots
// (Relationships, ToID, Properties, Get, Subtype, ID, Contains, Name,
// ...) are all struct *field names* referenced via a local variable
// whose type the extractor never tracked. Example:
//
//	func (s *Server) handle(entry EntityRecord) {
//	    fmt.Println(entry.ToID) // refs EntityRecord.ToID
//	}
//
// `entry` is a parameter typed `EntityRecord`, and `entry.ToID` should
// bind to the struct's ToID field. The existing references pass only
// recognised two LHS shapes for `<expr>.<field>` selectors:
//
//  1. `r.<field>` where `r` is the enclosing method's receiver var.
//  2. `T.<field>` where `T` is a PascalCase identifier (type / package).
//
// Anything else — a parameter, a `:=` short-var-decl, a `var x T` —
// missed entirely. The infrastructure to type those names already
// existed in extractor.go (`collectParamTypes`, `collectBodyVarTypes`,
// `mergeVarTypes`) but was consumed only by extractCallRelationships
// for CALLS edges. This file plumbs the same maps into the references
// walk so REFERENCES selectors can resolve `<localVar>.<field>` against
// dottedSymbols / structFields.
//
// Scope — v1 lightweight (per #1840 acceptance):
//   - Function & method PARAMETERS with explicit types.
//   - Method RECEIVER (`(s *Server)` → s is `Server`).
//   - Short-var-decl whose RHS is a struct literal (`x := Foo{...}`,
//     `x := &Foo{...}`).
//   - `var x T` / `var x = Foo{...}`.
//   - Short-var-decl from an allowlisted constructor (`r := chi.NewRouter()`).
//
// Out of scope (v2 ticket — TODO file separately if signal warrants):
//   - Return-type chains across user-defined functions (`x := myFunc()` →
//     resolve myFunc's declared return type). Requires a cross-function
//     return-type table; deferred.
//   - Interface dispatch / embedded type promotion.
//   - Generic type inference.
//   - Multi-LHS assignment (`a, b := f()`).
//   - Closure capture beyond the immediate enclosing function.
//
// Why a separate file: keeps the references pass narrow (walk + emit)
// and isolates the type-tracking concern so v2 can swap in a richer
// implementation without touching emission logic.

package golang

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// goVarTypes is a (varName → canonicalTypeName) map for a single
// function/method scope. Canonical form strips a leading `*` and any
// generic `[T]` suffix — the same form used by collectParamTypes /
// collectBodyVarTypes so the maps are mergeable.
//
// The map is consumed during selector_expression resolution in
// references.go. A miss leaves the selector unbound (graceful fallback
// to same_package_unqualified, matching the pre-#1840 behaviour) — we
// never crash and never emit a wrong edge.
type goVarTypes map[string]string

// buildFunctionVarTypes returns the var-type table for a function or
// method body. Composes three sources, in lexical-scope order (outer
// → inner), with mergeVarTypes' "drop on type conflict" semantics
// preventing spurious bindings on shadowed names:
//
//  1. The receiver parameter, if this is a method_declaration. Bound to
//     the receiver's value type (pointer stripped — pointer-vs-value
//     receivers share their type's field set).
//  2. Regular parameters from the function's parameter_list (via
//     collectParamTypes). Already canonical.
//  3. Local short-var-decls and var-decls inside the body (via
//     collectBodyVarTypes). Already canonical, ambiguous names dropped.
//
// Returns nil when no name can be typed — callers can no-op cheaply.
// All three sub-collectors are pre-existing infrastructure originally
// added for CALLS-edge resolution (issue #364); this function is the
// adapter that lets the references pass share them.
//
// Passing nil for funcOrMethodNode is safe and returns nil.
func buildFunctionVarTypes(funcOrMethodNode ts.Node, src []byte, ctorReturns map[string]string) goVarTypes {
	if funcOrMethodNode == nil {
		return nil
	}

	var receiverMap map[string]string
	if funcOrMethodNode.Type() == "method_declaration" {
		recvNode := funcOrMethodNode.ChildByFieldName("receiver")
		recvVar := receiverParamName(recvNode, src)
		recvType := receiverTypeName(recvNode, src)
		if recvVar != "" && recvType != "" {
			receiverMap = map[string]string{recvVar: recvType}
		}
	}

	paramsNode := funcOrMethodNode.ChildByFieldName("parameters")
	paramMap := collectParamTypes(paramsNode, src)

	bodyNode := funcOrMethodNode.ChildByFieldName("body")
	bodyMap := collectBodyVarTypes(bodyNode, src, ctorReturns)

	// Merge in scope order: receiver → params → body. mergeVarTypes
	// drops names with conflicting types (the conservative choice from
	// #364 — better to miss than to mis-bind).
	out := mergeVarTypes(receiverMap, paramMap)
	out = mergeVarTypes(out, bodyMap)
	if len(out) == 0 {
		return nil
	}
	return out
}

// lookupVarType returns the tracked type for varName, or "" when no
// binding is recorded. Always safe on a nil receiver. The "" miss is
// the graceful fallback path required by #1840 — callers proceed
// without emitting an edge, leaving the selector as the existing
// `same_package_unqualified` bucket.
func (vt goVarTypes) lookupVarType(varName string) string {
	if vt == nil || varName == "" {
		return ""
	}
	return vt[varName]
}
