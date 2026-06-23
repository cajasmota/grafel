// decorators_generic.go — issue #2016: generalize Python decorator capture
// beyond DRF @action / Celery @shared_task.
//
// For every decorator on a function_definition (top-level or method inside a
// class body), stamp a `decorator_<name>` property on the matching
// SCOPE.Operation entity. The value is the raw decorator source snippet
// (capped) when the decorator is a call form, or the literal "true" for a
// bare decorator.
//
// Recognised forms:
//
//	@property                           → decorator_property = "true"
//	@cached_property                    → decorator_cached_property = "true"
//	@staticmethod                       → decorator_staticmethod = "true"
//	@classmethod                        → decorator_classmethod = "true"
//	@contextmanager                     → decorator_contextmanager = "true"
//	@functools.wraps(other)             → decorator_wraps = "@functools.wraps(other)"
//	@cache(ttl=300)                     → decorator_cache = "@cache(ttl=300)"
//	@<name>.setter / @<name>.getter     → decorator_setter = "<name>" / decorator_getter = "<name>"
//	@<name>.deleter                     → decorator_deleter = "<name>"
//
// The pass runs AFTER the primary walk (and after the DRF / Celery passes),
// so:
//   - Operation entities exist and are looked up by SourceFile + Name.
//   - Existing `drf_action` / `is_task` / kwarg-derived properties are
//     preserved (this pass only writes keys that don't already exist).
//
// The pass is intentionally simple and never modifies existing properties.
// Decorator-specific passes that already know how to parse their kwargs
// (DRF @action, Celery @shared_task) continue to own their structured
// extraction; this pass adds the COVERAGE so every decorator becomes a
// queryable graph signal, satisfying the W8 evidence in #2016.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitGenericDecoratorProperties walks every decorated_definition in root,
// resolves the wrapped operation entity by SourceFile + Name, and stamps
// `decorator_<name>` properties on it for each decorator on the definition.
//
// Mutates *entities in place. Safe to call with nil/empty input.
func emitGenericDecoratorProperties(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	walkDecoratedDefs(root, "", file, entities)
}

func walkDecoratedDefs(n ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
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
			for i := 0; i < int(body.ChildCount()); i++ {
				walkDecoratedDefs(body.Child(i), childCls, file, entities)
			}
		}
		return
	case "decorated_definition":
		inner := n.ChildByFieldName("definition")
		if inner != nil && inner.Type() == "function_definition" {
			stampDecoratorsOnOperation(n, inner, parentClass, file, entities)
		}
		// Recurse so a decorated class wrapping methods is visited.
		if inner != nil {
			walkDecoratedDefs(inner, parentClass, file, entities)
		}
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkDecoratedDefs(n.Child(i), parentClass, file, entities)
	}
}

// stampDecoratorsOnOperation scans every decorator child of decoratedNode and
// stamps a `decorator_<leaf>` property on the matching SCOPE.Operation entity
// found by SourceFile + qualified Name.
func stampDecoratorsOnOperation(decoratedNode, fnNode ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	methodNameNode := fnNode.ChildByFieldName("name")
	if methodNameNode == nil {
		return
	}
	methodName := nodeText(methodNameNode, file.Content)
	if methodName == "" {
		return
	}
	emittedName := methodName
	if parentClass != "" {
		emittedName = parentClass + "." + methodName
	}
	op := findOpByName(*entities, file.Path, emittedName)
	if op == nil {
		return
	}

	for i := 0; i < int(decoratedNode.ChildCount()); i++ {
		ch := decoratedNode.Child(i)
		if ch == nil || ch.Type() != "decorator" {
			continue
		}
		// Walk named children of the decorator to find either:
		//   - a call    → `@foo(...)`           leaf = "foo"
		//   - attribute → `@a.b.c` (rare) or `@<name>.setter`
		//   - identifier→ `@foo` bare
		// We capture the LEAF identifier as the decorator name (matching the
		// existing convention in django_drf_actions.go decoratorLeaf).
		var leaf string
		var snippet string
		var isCall bool
		// setterTarget captures the LHS of `@<name>.setter` and friends so we
		// can stamp `decorator_setter = "<name>"` instead of just "true".
		var setterTarget string
		var setterKind string // "setter" | "getter" | "deleter"
		for j := 0; j < int(ch.NamedChildCount()); j++ {
			inner := ch.NamedChild(j)
			if inner == nil {
				continue
			}
			switch inner.Type() {
			case "call":
				isCall = true
				fn := inner.ChildByFieldName("function")
				leaf = decoratorLeaf(fn, file.Content)
				snippet = strings.TrimSpace(nodeText(ch, file.Content))
				if len(snippet) > 200 {
					snippet = snippet[:200] + "…"
				}
			case "attribute":
				// Could be `@x.setter` / `@x.getter` / `@x.deleter` (property
				// triad), or a dotted bare reference like `@a.b`.
				if attr := inner.ChildByFieldName("attribute"); attr != nil {
					attrText := nodeText(attr, file.Content)
					switch attrText {
					case "setter", "getter", "deleter":
						setterKind = attrText
						if obj := inner.ChildByFieldName("object"); obj != nil {
							setterTarget = strings.TrimSpace(nodeText(obj, file.Content))
						}
						leaf = attrText
					default:
						leaf = attrText
					}
				}
			case "identifier":
				leaf = nodeText(inner, file.Content)
			case "dotted_name":
				if c := inner.NamedChild(int(inner.NamedChildCount()) - 1); c != nil {
					leaf = nodeText(c, file.Content)
				}
			}
		}
		if leaf == "" {
			continue
		}
		if op.Properties == nil {
			op.Properties = map[string]string{}
		}
		key := "decorator_" + leaf
		// Don't overwrite a value already set by a specialised pass
		// (e.g. DRF @action stamped a structured kwarg snippet onto
		// `decorator_<methodName>` on the parent class; that's different,
		// scoped to the class. Here we write per-Operation keys which the
		// specialised passes don't touch.)
		if _, exists := op.Properties[key]; exists {
			continue
		}
		switch {
		case setterKind != "" && setterTarget != "":
			// `@x.setter` → decorator_setter = "x"
			op.Properties[key] = setterTarget
		case isCall && snippet != "":
			op.Properties[key] = snippet
		default:
			op.Properties[key] = "true"
		}
	}
}
