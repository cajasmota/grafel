// getattr_dispatch.go — resolve indirect method dispatch via
// `getattr(self, name)()` where `name` is a string literal bound earlier in the
// same function body (issue #5158).
//
// Background
// ----------
// A common reflective-dispatch shape leaves a CALLS edge pointing at nothing
// resolvable, because the invoked method name is computed:
//
//	class OrderHandler:
//	    def run(self, ev):
//	        action = "handle_order"        # literal binding
//	        getattr(self, action)(ev)      # → self.handle_order(ev)
//
//	        getattr(self, "handle_stock")()  # inline literal form
//
// The normal call-extraction path can't connect `getattr(self, action)(...)` to
// `OrderHandler.handle_order` without tracing the literal assigned to `action`.
// This pass does exactly that, reusing the cross-language literal-binding
// resolver (extractor.LiteralBindingResolver): last-write-wins, and a non-literal
// reassignment (`action = pick()`) taints the binding so a stale literal never
// resolves.
//
// Scope / conservatism
// --------------------
//   - Receiver must be `self` or `cls` (an in-class method dispatch) so the
//     recovered target is `<parentClass>.<method>`. getattr on an arbitrary
//     object can't be statically typed here and is left alone.
//   - The name argument must be either a bare string literal (inline) or a
//     simple identifier resolvable to a string literal via the binder. A binary
//     expression (`"on_" + ev`) or any other shape is non-static ⇒ skipped.
//   - Best-effort straight-line trace; conditional/branch reassignment is
//     approximated by taint, like the COBOL/shell wirings.
package python

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractGetattrDispatchCalls scans a function body for
// `getattr(self|cls, <name>)(...)` calls and emits a CALLS edge to
// `<parentClass>.<method>` when <name> resolves to a static string literal.
// parentClass must be non-empty (the dispatch is method-scoped). seen is the
// (target,alias) dedup set bridged from the primary call pass so we never
// double-emit an edge the direct path already produced.
func extractGetattrDispatchCalls(
	body ts.Node,
	src []byte,
	parentClass, callerName string,
	seen map[seenKeyDD]bool,
) []types.RelationshipRecord {
	if body == nil || parentClass == "" {
		return nil
	}

	// binder tracks `<name> = "<literal>"` bindings in document order across the
	// function body. Python identifiers are case-sensitive ⇒ identity keyFn. The
	// whole function body is one scope here (we do not descend into nested
	// def/lambda for binding state — those open their own scope and are rare for
	// this pattern); a single straight-line pass is sufficient and conservative.
	binder := extractor.NewLiteralBindingResolver(nil)
	var rels []types.RelationshipRecord

	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "assignment":
			recordPyAssignment(n, src, binder)
		case "call":
			if method, dynVar, ok := getattrDispatchTarget(n, src, binder); ok {
				target := parentClass + "." + method
				if target == parentClass+"."+callerName {
					// self-recursion — drop, matching the primary path.
					break
				}
				key := seenKeyDD{target: target, alias: ""}
				if !seen[key] {
					seen[key] = true
					props := map[string]string{
						"line":         strconv.Itoa(int(n.StartPoint().Row) + 1),
						"resolved_via": extractor.ResolvedViaLiteralBinding,
						"pattern_type": "getattr_dispatch",
					}
					if dynVar != "" {
						props["dynamic_target"] = dynVar
					} else {
						props["dynamic_target"] = "getattr-literal"
					}
					rels = append(rels, types.RelationshipRecord{
						ToID:       target,
						Kind:       "CALLS",
						Properties: props,
					})
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(body)
	return rels
}

// recordPyAssignment feeds a single-target `<id> = <rhs>` assignment into the
// binder: a bare string-literal RHS Binds; any other RHS Taints. Multi-target /
// augmented / annotated assignments and non-identifier targets are ignored
// (they neither establish nor reliably clear a simple-name binding).
func recordPyAssignment(n ts.Node, src []byte, binder *extractor.LiteralBindingResolver) {
	// assignment children: identifier, "=", <rhs>  (simple form).
	if n.ChildCount() < 3 {
		return
	}
	lhs := n.Child(0)
	if lhs == nil || lhs.Type() != "identifier" {
		return
	}
	name := nodeText(lhs, src)
	rhs := n.Child(int(n.ChildCount()) - 1)
	if rhs == nil {
		binder.Taint(name)
		return
	}
	if rhs.Type() == "string" {
		if lit := pyStringLiteralValue(rhs, src); lit != "" {
			binder.Bind(name, lit)
			return
		}
	}
	// Any non-literal RHS (call, binary_operator, attribute, identifier, …).
	binder.Taint(name)
}

// getattrDispatchTarget inspects an outer `call` node and, when it has the
// shape `getattr(self|cls, <name>)(...)` with <name> resolvable to a static
// string literal, returns (method, dynVar, true). dynVar is the source variable
// name for the binder-resolved form, or "" for the inline-string form. Returns
// ("","",false) for any other call.
func getattrDispatchTarget(outer ts.Node, src []byte, binder *extractor.LiteralBindingResolver) (string, string, bool) {
	// Outer call's function child must itself be the `getattr(self, X)` call.
	fn := outer.ChildByFieldName("function")
	if fn == nil || fn.Type() != "call" {
		return "", "", false
	}
	gattr := fn.ChildByFieldName("function")
	if gattr == nil || gattr.Type() != "identifier" || nodeText(gattr, src) != "getattr" {
		return "", "", false
	}
	args := fn.ChildByFieldName("arguments")
	if args == nil {
		return "", "", false
	}
	// Collect the two positional argument nodes (named children, comma-separated).
	var pos []ts.Node
	for i := 0; i < int(args.NamedChildCount()); i++ {
		ch := args.NamedChild(i)
		if ch != nil {
			pos = append(pos, ch)
		}
	}
	if len(pos) < 2 {
		return "", "", false
	}
	recv := pos[0]
	if recv.Type() != "identifier" {
		return "", "", false
	}
	if r := nodeText(recv, src); r != "self" && r != "cls" {
		return "", "", false
	}
	nameArg := pos[1]
	switch nameArg.Type() {
	case "string":
		if lit := pyStringLiteralValue(nameArg, src); lit != "" {
			return lit, "", true
		}
	case "identifier":
		v := nodeText(nameArg, src)
		if lit, ok := binder.Resolve(v); ok {
			return lit, v, true
		}
	}
	return "", "", false
}
