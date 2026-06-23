// template_render.go — supplemental pass that emits RENDERS edges from
// JS/TS request handlers to a shared SCOPE.Template node (epic #3628). It lets
// the graph answer "what view does this route render?" (outbound RENDERS) and
// "who renders profile?" (inbound RENDERS).
//
// Detected shapes (static only — honest-partial, precision-first):
//
//	res.render('profile', {...})   → RENDERS profile        (Express)
//	res.render("users/list")        → RENDERS users/list      (Express)
//	ctx.render('home')              → RENDERS home            (Koa, koa-views)
//
// Intentionally DROPPED (would mislead view-layer analysis):
//
//	res.render(viewName)            (variable / dynamic name)
//	res.render(`p/${id}`)           (template-literal interpolation)
//	res.json(...) / res.send(...)   (not a view render — REST, not MVC)
//
// The receiver heuristic requires the method to be `render` on a `res` / `ctx`
// /-suffixed response object (res, ctx, response) so component-composition or
// unrelated `.render()` calls don't false-positive. All node/edge construction
// (convergence on one node per template name) lives in
// extractor.EmitTemplateEdges.

package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// emitTemplateRenderEdges scans the AST for Express/Koa `<res>.render('view')`
// calls and appends template entities + RENDERS edges to x.entities.
// x.entities[0] MUST be the file entity. Safe with an empty tree.
func (x *extractor) emitTemplateRenderEdges(root ts.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}

	var edges []extreg.TemplateEdge

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration", "generator_function_declaration":
			name := x.nodeText(n.ChildByFieldName("name"))
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, name)
			}
			return
		case "method_definition":
			name := x.nodeText(n.ChildByFieldName("name"))
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, name)
			}
			return
		case "variable_declarator":
			nameNode := n.ChildByFieldName("name")
			valNode := n.ChildByFieldName("value")
			if nameNode != nil && valNode != nil {
				switch valNode.Type() {
				case "arrow_function", "function", "function_expression":
					name := x.nodeText(nameNode)
					if body := valNode.ChildByFieldName("body"); body != nil {
						walk(body, name)
						return
					}
				}
			}
		case "call_expression":
			if tpl := x.jsRenderCallTemplate(n); tpl != "" {
				edges = append(edges, extreg.TemplateEdge{
					Name: tpl, FromName: enclosing, Pattern: "res_render",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extreg.EmitTemplateEdges(&x.entities, x.language, edges)
}

// jsRenderCallTemplate returns the raw (unquoted) template name from a
// `<res>.render('view', ...)` call where the receiver is a response-like object
// (res / ctx / response, case-insensitive) and the first argument is a plain
// string literal. Returns "" otherwise (non-render method, non-response
// receiver, or dynamic first argument → drop).
func (x *extractor) jsRenderCallTemplate(call ts.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return ""
	}
	prop := fn.ChildByFieldName("property")
	if prop == nil || x.nodeText(prop) != "render" {
		return ""
	}
	obj := fn.ChildByFieldName("object")
	if obj == nil || !isResponseReceiver(x.nodeText(obj)) {
		return ""
	}

	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	// First positional argument must be a plain string literal.
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil || a.Type() == "comment" {
			continue
		}
		if a.Type() == "string" {
			return jsStripString(x.nodeText(a))
		}
		return "" // first arg is dynamic / template-literal → drop
	}
	return ""
}

// isResponseReceiver reports whether an object expression text names a typical
// HTTP-response object: exactly "res", "ctx", "response", or "reply" (Fastify),
// case-insensitive. Conservative on purpose — a random `widget.render('x')`
// must not be mistaken for a server-side view render.
func isResponseReceiver(obj string) bool {
	switch obj {
	case "res", "ctx", "response", "reply",
		"Res", "Ctx", "Response", "Reply":
		return true
	}
	return false
}
