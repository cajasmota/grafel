// Package javascript — navigation edge extraction for issue #2655.
//
// This file detects navigation call sites across Expo Router, React Navigation,
// and Next.js patterns, emitting NAVIGATES_TO edges from the caller entity to a
// route stub (or to the matched destination entity if resolvable).
//
// Detected patterns (Phase 1):
//   - router.push('/path')                     → NAVIGATES_TO, route='/path'
//   - router.navigate('/path')                 → NAVIGATES_TO, route='/path'
//   - router.replace('/path')                  → NAVIGATES_TO, route='/path'
//   - router.back()                            → NAVIGATES_TO, route='<back>'
//   - router.push({pathname: '/x', params: {a, b}}) → route='/x', params=[a,b]
//   - navigation.navigate('Screen')            → NAVIGATES_TO, route='Screen'
//   - navigation.push('Screen')                → NAVIGATES_TO, route='Screen'
//   - Linking.openURL('https://...')           → NAVIGATES_TO (external)
//
// Phase 2 additions (#2658):
//   - Template-literal routes: router.push(`/users/${id}`) → route='/users/{*}'
//   - Hook-rename binding: const nav = useNavigation(); nav.navigate('X')
//     detected via hookVarToNavModule table scanned alongside hookVarToModule.
package javascript

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/types"
)

// templateExprRe matches JavaScript template-literal expression slots
// (${...}) and is used to normalise dynamic route strings to a stable
// pattern compatible with server-side route definitions (e.g. /users/{*}).
// Phase 2 of #2658 — mirrors the {*} sentinel used by normalizePathForIndex
// in internal/links/http_pass.go.
var templateExprRe = regexp.MustCompile(`\$\{[^}]*\}`)

// normalizeTemplateLiteralRoute converts a raw template-literal string
// (including surrounding backticks and any ${…} interpolations) into a
// stable route pattern by:
//  1. Stripping the surrounding backtick delimiters.
//  2. Replacing every ${…} slot with the {*} sentinel so the result can
//     be matched against server-side route definitions like /users/{id}.
//
// Example:  "`/users/${id}/profile`"  →  "/users/{*}/profile"
func normalizeTemplateLiteralRoute(raw string) string {
	// Strip surrounding backticks added by nodeText for template_string nodes.
	s := strings.Trim(raw, "`")
	// Replace every ${…} interpolation with the {*} placeholder.
	s = templateExprRe.ReplaceAllString(s, "{*}")
	return s
}

// navigationHookNames is the set of hook function names whose return value
// should be treated as a navigation object. When a variable is bound to one
// of these hooks (e.g. `const nav = useNavigation()`), any .navigate/.push
// call on that variable is recognised as a navigation call.
//
// Phase 2 of #2658 — hook-rename binding detection.
var navigationHookNames = map[string]bool{
	"useNavigation": true,
	"useRouter":     true,
	"useNavigate":   true,
}

// navigationMethodNames is the set of method names recognised as navigation
// calls. "push" appears both here (router.push / navigation.push) and in
// builtinMethodNames (Array.push). The navigation detector runs BEFORE the
// normal callTarget path in extractCallRelationships and gates on the full
// receiver.method shape to avoid misidentifying plain array .push() calls.
var navigationMethodNames = map[string]bool{
	"push":     true,
	"navigate": true,
	"replace":  true,
	"back":     true,
	"openURL":  true,
}

// navigationReceiverNames is the set of receiver identifiers whose method
// calls are treated as navigation, regardless of local variable name aliasing.
// This is a conservative allowlist: it covers the most common single-word
// aliases for the Expo Router / React Navigation / Linking APIs.
var navigationReceiverNames = map[string]bool{
	"router":     true,
	"navigation": true,
	"Linking":    true,
}

// extractNavigationCall inspects a single call_expression node and, if it
// matches a navigation pattern, returns the destination route, any param key
// names extracted from the `params:` object, and ok=true.
//
// Three argument shapes are handled:
//
//  1. String literal first arg:
//     router.push('/foo')  →  route='/foo', params=nil
//
//  2. Template literal first arg (dynamic — captured as-is):
//     router.push(`/foo/${id}`)  →  route='`/foo/${id}`', params=nil
//
//  3. Object-form first arg with pathname + optional params:
//     router.push({pathname: '/users/[id]', params: {id, type}})
//     →  route='/users/[id]', params=['id','type']
//
//  4. No first arg (e.g. router.back()):
//     →  route='<back>'
func extractNavigationCall(x *extractor, call *sitter.Node) (route string, params []string, ok bool) {
	if call == nil {
		return "", nil, false
	}

	// Must be a call_expression whose function child is a member_expression.
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return "", nil, false
	}

	// Resolve receiver and method names.
	objNode := fn.ChildByFieldName("object")
	propNode := fn.ChildByFieldName("property")
	if objNode == nil || propNode == nil {
		return "", nil, false
	}

	receiver := x.nodeText(objNode)
	method := x.nodeText(propNode)

	// Receiver must be in the static allowlist OR bound to a navigation hook
	// via the hookVarToNavModule table built in buildNavigationHookVarTable.
	// Phase 2 of #2658 — hook-rename binding detection.
	if !navigationReceiverNames[receiver] && !x.isNavigationHookVar(receiver) {
		return "", nil, false
	}
	// Method must be a navigation verb.
	if !navigationMethodNames[method] {
		return "", nil, false
	}

	// router.back() / navigation.back() — no argument, route stub.
	if method == "back" {
		return "<back>", nil, true
	}

	// Inspect the arguments list.
	args := call.ChildByFieldName("arguments")
	if args == nil {
		// No args node at all (shouldn't happen for these methods, but be safe).
		return "<" + method + ">", nil, true
	}

	// Find the first non-punctuation child of the arguments node.
	firstArg := firstMeaningfulArg(args)
	if firstArg == nil {
		// e.g. Linking.openURL() with no argument — skip.
		return "", nil, false
	}

	switch firstArg.Type() {
	case "string":
		// Quoted string literal: 'screen' or "/path".
		raw := x.nodeText(firstArg)
		route = strings.Trim(raw, `"'`+"`")
		return route, nil, true

	case "template_string":
		// Template literal — normalise ${…} slots to {*} so the route
		// can be matched against server-side definitions (Phase 2, #2658).
		route = normalizeTemplateLiteralRoute(x.nodeText(firstArg))
		return route, nil, true

	case "object", "object_expression":
		// Object-form: {pathname: '/x', params: {a, b, ...}}.
		return extractObjectFormRoute(x, firstArg)

	default:
		// Identifier or other expression — dynamic / unresolvable.
		return "", nil, false
	}
}

// firstMeaningfulArg returns the first child of an arguments node that is not
// a punctuation token ("(", ")", ",").
func firstMeaningfulArg(args *sitter.Node) *sitter.Node {
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		t := ch.Type()
		if t == "(" || t == ")" || t == "," {
			continue
		}
		return ch
	}
	return nil
}

// extractObjectFormRoute parses an object literal node for the `pathname` key
// and the `params` key, returning the route and param key names.
func extractObjectFormRoute(x *extractor, obj *sitter.Node) (route string, params []string, ok bool) {
	for i := 0; i < int(obj.ChildCount()); i++ {
		child := obj.Child(i)
		if child == nil {
			continue
		}
		// Both JS and TS grammars use "pair" for key: value inside objects.
		if child.Type() != "pair" {
			continue
		}
		keyNode := child.ChildByFieldName("key")
		valNode := child.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		key := strings.Trim(x.nodeText(keyNode), `"'`+"`")
		switch key {
		case "pathname":
			raw := x.nodeText(valNode)
			route = strings.Trim(raw, `"'`+"`")
		case "params":
			params = extractParamKeys(x, valNode)
		}
	}
	if route == "" {
		return "", nil, false
	}
	return route, params, true
}

// extractParamKeys returns the property/shorthand key names from an object
// expression used as the `params:` value in a navigation call.
//
// Handles:
//   - explicit `{id: value}` (pair)
//   - shorthand `{id}` (shorthand_property_identifier[_pattern])
//   - spread elements `{...rest}` — recorded as the sentinel "...spread" so
//     downstream queries know dynamic keys may exist (#2665)
//
// If the params value is not an object literal (e.g. a variable reference like
// `params: opts`), returns an empty slice — we deliberately do NOT perform any
// data-flow analysis here. Caller can distinguish "no params object" (nil)
// from "params object with non-static contents" (empty slice).
func extractParamKeys(x *extractor, obj *sitter.Node) []string {
	if obj == nil {
		return nil
	}
	// Unwrap through one level of parenthesisation.
	if obj.Type() == "parenthesized_expression" {
		for i := 0; i < int(obj.ChildCount()); i++ {
			ch := obj.Child(i)
			if ch != nil && (ch.Type() == "object" || ch.Type() == "object_expression") {
				obj = ch
				break
			}
		}
	}
	if obj.Type() != "object" && obj.Type() != "object_expression" {
		// #2665: caller passed e.g. `params: opts` — variable reference.
		// Return empty (non-nil) so emitNavigationEdge can record an empty
		// params_keys array (dynamic params, no static keys recoverable).
		return []string{}
	}

	var keys []string
	seen := make(map[string]bool)
	for i := 0; i < int(obj.ChildCount()); i++ {
		child := obj.Child(i)
		if child == nil {
			continue
		}
		var keyName string
		switch child.Type() {
		case "pair":
			if kn := child.ChildByFieldName("key"); kn != nil {
				keyName = strings.Trim(x.nodeText(kn), `"'`+"`")
			}
		case "shorthand_property_identifier", "shorthand_property_identifier_pattern":
			keyName = x.nodeText(child)
		case "spread_element":
			// #2665: record that a spread exists so consumers know dynamic
			// keys may be present beyond the static set.
			keyName = "...spread"
		}
		if keyName != "" && !seen[keyName] {
			seen[keyName] = true
			keys = append(keys, keyName)
		}
	}
	return keys
}

// emitNavigationEdge constructs a NAVIGATES_TO RelationshipRecord from a
// navigation call site. The ToID is the route string (prefixed with "route:")
// so it forms a stable synthetic stub ID that the linker can later match
// against declared route entities.
//
// Properties set:
//   - "route"       : the destination route/screen name
//   - "params"      : comma-separated param key names (omitted when empty)
//   - "params_keys" : JSON array string of sorted, deduped param keys (#2665).
//                     Empty array "[]" indicates the params object existed but
//                     contained no statically-extractable keys (e.g. variable
//                     reference). Property is omitted entirely when no params
//                     object was observed.
//   - "line"        : 1-indexed source line of the call site
//   - "via"         : "navigation_call" (traceability tag)
func emitNavigationEdge(route string, params []string, call *sitter.Node) types.RelationshipRecord {
	toID := "route:" + route
	props := map[string]string{
		"route": route,
		"line":  strconv.Itoa(int(call.StartPoint().Row) + 1),
		"via":   "navigation_call",
	}
	if len(params) > 0 {
		props["params"] = strings.Join(params, ", ")
	}
	// #2665: emit params_keys whenever a params object was observed (params != nil),
	// even when empty — distinguishes "no params arg" from "params: <dynamic>".
	if params != nil {
		// Dedupe + sort. extractParamKeys already dedupes, but normalise here
		// so callers that build the edge from other paths get the same shape.
		seen := make(map[string]struct{}, len(params))
		uniq := make([]string, 0, len(params))
		for _, k := range params {
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			uniq = append(uniq, k)
		}
		sort.Strings(uniq)
		if b, err := json.Marshal(uniq); err == nil {
			props["params_keys"] = string(b)
		}
	}
	return types.RelationshipRecord{
		ToID:       toID,
		Kind:       "NAVIGATES_TO",
		Properties: props,
	}
}

// ---------------------------------------------------------------------------
// Phase 2 (#2658) — Hook-rename binding helpers
// ---------------------------------------------------------------------------

// isNavigationHookVar reports whether varName is known to hold the result of a
// useNavigation() / useRouter() / useNavigate() hook call. The table is
// pre-built in Extract() via buildNavigationHookVarTable and stored on x.navHookVars.
func (x *extractor) isNavigationHookVar(varName string) bool {
	return x.navHookVars[varName]
}

// buildNavigationHookVarTable scans the AST for variable declarations of the
// form `const <varName> = <hookName>(...)` where <hookName> is in
// navigationHookNames. Returns a map from varName → true for every variable
// that should be treated as a navigation receiver.
//
// This is called once per file from Extract() (alongside buildHookVarToModule)
// and stored on x.navHookVars so extractNavigationCall can consult it via
// isNavigationHookVar without needing to re-traverse the AST. Phase 2 of #2658.
func buildNavigationHookVarTable(x *extractor, root *sitter.Node) map[string]bool {
	if root == nil {
		return nil
	}
	out := make(map[string]bool)

	// Walk the AST looking for:
	//   const <varName> = <hookName>(...)
	// where <hookName> ∈ navigationHookNames.
	stack := make([]*sitter.Node, 0, 64)
	stack = append(stack, root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "variable_declarator" {
			nameNode := n.ChildByFieldName("name")
			valNode := n.ChildByFieldName("value")
			if nameNode != nil && valNode != nil && valNode.Type() == "call_expression" {
				localName := x.nodeText(nameNode)
				if localName != "" && !strings.ContainsAny(localName, "{}[].,") {
					fnNode := valNode.ChildByFieldName("function")
					if fnNode != nil {
						hookName := x.nodeText(fnNode)
						if navigationHookNames[hookName] {
							out[localName] = true
						}
					}
				}
			}
		}
		count := int(n.ChildCount())
		for i := count - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
