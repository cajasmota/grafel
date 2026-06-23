// django_drf_actions.go — supplemental pass that captures DRF `@action(...)`
// decorator kwargs and surfaces them as Properties on the per-method
// Operation entity, plus exposes a class-level summary so the ClassManifest
// builder (#1861, internal/docgen) can render per-action decorators.
//
// Issue #1967 — Wave 1 docgen smoke showed methods decorated with
//
//	@action(detail=False, methods=["get"], serializer_class=FooSerializer)
//
// lose ALL the decorator metadata: the class_manifest method entry has no
// `decorators` field, and the per-action Operation has no `http_method`,
// `is_detail`, `serializer_class`, `url_path`, `permission_classes` props.
// Without that surface the endpoint detail page can't tell list-actions
// from detail-actions, can't show the per-action request/response shape
// (composes with #1935 ShapeTree), and falls back to HTTP-verb guessing.
//
// This pass runs AFTER walkNode + extractClassFields so it can find the
// emitted SCOPE.Operation entity by Name ("<Class>.<method>") and stamp
// properties onto it in-place. It also writes a per-method decorator
// summary onto the parent class's Properties map under a stable key
// (`decorator_<method>`) so the docgen ClassManifest builder can read the
// per-method decoration without re-parsing source.
//
// Recognised decorator forms:
//
//	@action(...)
//	@rest_framework.decorators.action(...)
//	@decorators.action(...)
//
// Recognised kwargs (all optional):
//
//	detail=<bool>
//	methods=[<str>, ...]
//	serializer_class=<Identifier>
//	url_path="<str>"
//	url_name="<str>"
//	permission_classes=[<Identifier>, ...]
//
// Multi-method actions stamp the first verb as `http_method` AND the full
// comma-joined list under `http_methods`. permission_classes are stored as
// a comma-joined identifier list (no source-text noise) so downstream
// queries don't have to re-parse a Python list literal.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitDRFActionProperties walks every class_definition in root and, for each
// method wrapped in a `decorated_definition` whose decorator list includes an
// `@action(...)` call, parses the call's kwargs and stamps them onto the
// matching Operation entity by Name ("<Class>.<method>").
//
// Mutates *entities in place. Safe to call with nil/empty input.
func emitDRFActionProperties(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	// Walk every class_definition (top-level or nested) once. For each method
	// declared inside the class body that carries an @action decorator,
	// parse the kwargs and stamp them on the matching Operation entity.
	walkDRFAction(root, "", file, entities)
}

func walkDRFAction(n ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "class_definition":
		nameNode := n.ChildByFieldName("name")
		cls := ""
		if nameNode != nil {
			cls = nodeText(nameNode, file.Content)
		}
		childCls := cls
		if parentClass != "" && cls != "" {
			childCls = parentClass + "." + cls
		}
		body := n.ChildByFieldName("body")
		if body != nil {
			scanClassBodyForActions(body, childCls, file, entities)
			// Recurse so nested classes (rare for DRF, but possible) are scanned too.
			for i := 0; i < int(body.ChildCount()); i++ {
				walkDRFAction(body.Child(i), childCls, file, entities)
			}
		}
		return
	case "decorated_definition":
		inner := n.ChildByFieldName("definition")
		if inner != nil {
			walkDRFAction(inner, parentClass, file, entities)
		}
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkDRFAction(n.Child(i), parentClass, file, entities)
	}
}

// scanClassBodyForActions iterates a class body and, for every
// decorated_definition wrapping a function_definition, scans the decorator
// list for an `@action(...)` call. When found, the kwargs are parsed and
// stamped on the matching SCOPE.Operation entity.
func scanClassBodyForActions(body ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil || ch.Type() != "decorated_definition" {
			continue
		}
		inner := ch.ChildByFieldName("definition")
		if inner == nil || inner.Type() != "function_definition" {
			continue
		}
		methodNameNode := inner.ChildByFieldName("name")
		if methodNameNode == nil {
			continue
		}
		methodName := nodeText(methodNameNode, file.Content)
		if methodName == "" {
			continue
		}
		actionCall := findActionDecoratorCall(ch, file.Content)
		if actionCall == nil {
			continue
		}
		props := parseActionKwargs(actionCall, file.Content)
		if len(props) == 0 {
			// Even a bare @action() still surfaces as a DRF action endpoint;
			// stamp at least the marker so consumers know.
			props = map[string]string{}
		}
		props["drf_action"] = "true"

		// Stamp on the matching Operation entity by Name.
		emittedName := parentClass + "." + methodName
		op := findOpByName(*entities, file.Path, emittedName)
		if op != nil {
			if op.Properties == nil {
				op.Properties = make(map[string]string, len(props))
			}
			for k, v := range props {
				op.Properties[k] = v
			}
			// #3628 area #6 — endpoint protection. Normalise the @action's
			// permission_classes into the cross-framework auth contract so the
			// parity oracle / grafel_auth_coverage reads DRF action guards the
			// same way as Spring/FastAPI/Express.
			stampDRFActionAuth(op.Properties)
		}

		// Also stamp on the parent class as a `decorator_<method>` property so
		// the ClassManifest builder can surface per-method decorator info
		// without re-parsing the source window. The value is the raw
		// "@action(...)" source snippet (capped) for human readability.
		cls := findClassByName(*entities, file.Path, parentClass)
		if cls != nil {
			if cls.Properties == nil {
				cls.Properties = make(map[string]string)
			}
			snippet := strings.TrimSpace(nodeText(actionCall.Parent(), file.Content))
			if len(snippet) > 200 {
				snippet = snippet[:200] + "…"
			}
			cls.Properties["decorator_"+methodName] = snippet
		}
	}
}

// findActionDecoratorCall scans the decorator children of decoratedNode and
// returns the call node whose callee leaf is "action" (e.g. `@action(...)`,
// `@rest_framework.decorators.action(...)`, `@decorators.action(...)`).
// Returns nil when no action decorator is present or the decorator is bare
// (`@action` without parens).
func findActionDecoratorCall(decoratedNode ts.Node, src []byte) ts.Node {
	for i := 0; i < int(decoratedNode.ChildCount()); i++ {
		ch := decoratedNode.Child(i)
		if ch == nil || ch.Type() != "decorator" {
			continue
		}
		// A decorator's first named child is either an identifier, an
		// attribute (dotted), or a call (parenthesised form). We only care
		// about the call form because `@action` without args carries no
		// kwargs.
		for j := 0; j < int(ch.NamedChildCount()); j++ {
			inner := ch.NamedChild(j)
			if inner == nil || inner.Type() != "call" {
				continue
			}
			fn := inner.ChildByFieldName("function")
			if fn == nil {
				continue
			}
			if decoratorLeaf(fn, src) == "action" {
				return inner
			}
		}
	}
	return nil
}

// decoratorLeaf returns the leaf identifier of a decorator callee.
//
//	identifier            → "action"
//	attribute (a.b.c)     → "c"
//	dotted_name (a.b.c)   → "c"
func decoratorLeaf(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier":
		return nodeText(n, src)
	case "attribute":
		if attr := n.ChildByFieldName("attribute"); attr != nil {
			return nodeText(attr, src)
		}
	case "dotted_name":
		if c := n.NamedChild(int(n.NamedChildCount()) - 1); c != nil {
			return nodeText(c, src)
		}
	}
	return ""
}

// parseActionKwargs reads the keyword arguments of an `@action(...)` call
// node and returns the canonical property map.
func parseActionKwargs(call ts.Node, src []byte) map[string]string {
	out := map[string]string{}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return out
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		nameNode := arg.ChildByFieldName("name")
		valueNode := arg.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			continue
		}
		key := nodeText(nameNode, src)
		switch key {
		case "detail":
			v := strings.TrimSpace(nodeText(valueNode, src))
			if strings.EqualFold(v, "True") {
				out["is_detail"] = "true"
			} else if strings.EqualFold(v, "False") {
				out["is_detail"] = "false"
			}
		case "methods":
			methods := parseListLiteralStrings(valueNode, src)
			if len(methods) > 0 {
				// First verb wins for `http_method`; full list under
				// `http_methods` (comma-joined, lowercased) so consumers
				// can render multi-method actions accurately.
				out["http_method"] = strings.ToLower(methods[0])
				lowered := make([]string, len(methods))
				for i, m := range methods {
					lowered[i] = strings.ToLower(m)
				}
				out["http_methods"] = strings.Join(lowered, ",")
			}
		case "serializer_class":
			if v := strings.TrimSpace(nodeText(valueNode, src)); v != "" {
				out["serializer_class"] = v
			}
		case "url_path":
			if v := stripQuotes(strings.TrimSpace(nodeText(valueNode, src))); v != "" {
				out["url_path"] = v
			}
		case "url_name":
			if v := stripQuotes(strings.TrimSpace(nodeText(valueNode, src))); v != "" {
				out["url_name"] = v
			}
		case "permission_classes":
			perms := parseListLiteralIdentifiers(valueNode, src)
			if len(perms) > 0 {
				out["permission_classes"] = strings.Join(perms, ",")
			}
		}
	}
	return out
}

// parseListLiteralStrings reads a Python `[ "a", "b" ]` or `( "a", "b" )`
// node and returns the unquoted string literals it contains. Non-string
// elements are skipped silently.
func parseListLiteralStrings(n ts.Node, src []byte) []string {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "list", "tuple", "set":
		// fall through
	default:
		// A bare string literal like methods="get" is non-conformant DRF
		// but try to recover.
		if n.Type() == "string" {
			return []string{stripQuotes(nodeText(n, src))}
		}
		return nil
	}
	var out []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		ch := n.NamedChild(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "string" {
			out = append(out, stripQuotes(nodeText(ch, src)))
		}
	}
	return out
}

// parseListLiteralIdentifiers reads a Python `[ A, B.C ]` node and returns
// the identifier-shaped elements as their full source text (so a dotted
// attribute like `permissions.IsAdmin` survives intact).
func parseListLiteralIdentifiers(n ts.Node, src []byte) []string {
	if n == nil {
		return nil
	}
	if n.Type() != "list" && n.Type() != "tuple" && n.Type() != "set" {
		// Single identifier? Still surface it.
		t := strings.TrimSpace(nodeText(n, src))
		if t == "" {
			return nil
		}
		return []string{t}
	}
	var out []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		ch := n.NamedChild(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "identifier", "attribute", "dotted_name":
			t := strings.TrimSpace(nodeText(ch, src))
			if t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// findOpByName returns a pointer to the first SCOPE.Operation entity whose
// SourceFile + Name matches, or nil when not found. Pointer is into the
// shared slice so callers can mutate Properties in place.
func findOpByName(entities []types.EntityRecord, filePath, name string) *types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.SourceFile != filePath {
			continue
		}
		if e.Kind == "SCOPE.Operation" && e.Name == name {
			return e
		}
	}
	return nil
}

// findClassByName returns a pointer to the SCOPE.Component/class entity
// whose SourceFile + Name matches, or nil when not found.
func findClassByName(entities []types.EntityRecord, filePath, name string) *types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.SourceFile != filePath {
			continue
		}
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" && e.Name == name {
			return e
		}
	}
	return nil
}
