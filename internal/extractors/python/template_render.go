// template_render.go — supplemental pass that emits RENDERS edges from Python
// request handlers / views to a shared SCOPE.Template node (epic #3628). It
// lets the graph answer "what does this view render?" (outbound RENDERS) and
// "who renders users/list.html?" (inbound RENDERS).
//
// Detected shapes (static only — honest-partial, precision-first):
//
//	render_template('users/list.html', ...)   → RENDERS users/list.html   (Flask)
//	render(request, 'home.html', ...)          → RENDERS home.html          (Django)
//	class X(TemplateView): template_name = 'd.html'  → RENDERS d.html       (Django CBV)
//
// Intentionally DROPPED (would mislead view-layer analysis):
//
//	render_template(name_var)                  (dynamic / variable name)
//	render_template(f"{x}.html")               (f-string interpolation)
//	render(request, build_name())              (computed name)
//
// All node/edge construction (convergence on one node per template name via a
// synthetic SourceFile) lives in extractor.EmitTemplateEdges.

package python

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// emitTemplateRenderEdges scans every function / method body for Flask
// render_template / Django render call shapes and class bodies for the Django
// TemplateView.template_name attribute, appending template entities + RENDERS
// edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input. Renders at module scope attach to the file entity.
func emitTemplateRenderEdges(root *sitter.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.TemplateEdge

	var stack []string // enclosing entity-Name stack; top = current scope
	current := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1]
	}

	var walk func(n *sitter.Node, parentClass string)
	walk = func(n *sitter.Node, parentClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			cls := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				cls = nodeText(nn, src)
			}
			childCls := cls
			if parentClass != "" && cls != "" {
				childCls = parentClass + "." + cls
			}
			stack = append(stack, childCls)
			if body := n.ChildByFieldName("body"); body != nil {
				// Django CBV: template_name = 'x.html' at class scope binds the
				// template to the class entity itself.
				if tpl := pyTemplateNameAttr(body, src); tpl != "" {
					edges = append(edges, extractor.TemplateEdge{
						Name: tpl, FromName: childCls, Pattern: "template_name",
					})
				}
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "function_definition":
			leaf := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				leaf = nodeText(nn, src)
			}
			emitted := leaf
			if parentClass != "" && leaf != "" {
				emitted = parentClass + "." + leaf
			}
			stack = append(stack, emitted)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "decorated_definition":
			if inner := n.ChildByFieldName("definition"); inner != nil {
				walk(inner, parentClass)
			}
			return
		case "call":
			if name, pat := pyRenderCallTemplate(n, src); name != "" {
				edges = append(edges, extractor.TemplateEdge{
					Name: name, FromName: current(), Pattern: pat,
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")

	extractor.EmitTemplateEdges(entities, "python", edges)
}

// pyRenderCallTemplate inspects a call node and, if it is a Flask
// render_template(...) or Django render(request, '...') call whose template
// argument is a plain string literal, returns the (raw) template name and the
// detector pattern label. Returns "", "" otherwise (including dynamic names —
// a non-string template argument yields nothing, so EmitTemplateEdges drops it).
func pyRenderCallTemplate(call *sitter.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", ""
	}
	callee := ""
	switch fn.Type() {
	case "identifier":
		callee = nodeText(fn, src)
	case "attribute":
		// flask.render_template / shortcuts.render → trailing attribute leaf.
		if a := fn.ChildByFieldName("attribute"); a != nil {
			callee = nodeText(a, src)
		}
	}

	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", ""
	}

	switch callee {
	case "render_template":
		// Flask: first positional arg is the template name.
		if s := pyFirstStringArg(args, src); s != "" {
			return s, "render_template"
		}
	case "render":
		// Django shortcut: render(request, 'template.html', context). The
		// template name is the SECOND positional argument. We require the first
		// positional arg to be present (request) and read the second string.
		if s := pyNthStringArg(args, src, 1); s != "" {
			return s, "django_render"
		}
	}
	return "", ""
}

// pyFirstStringArg returns the unquoted value of the first positional string
// literal argument in an argument_list, or "" if the first positional argument
// is not a plain string literal (dynamic name → drop).
func pyFirstStringArg(args *sitter.Node, src []byte) string {
	return pyNthStringArg(args, src, 0)
}

// pyNthStringArg returns the unquoted value of the n-th positional argument if
// it is a plain string literal; "" otherwise. Keyword arguments are skipped
// when counting positionals. A non-string positional at index n yields "" so
// dynamic template names never fabricate an edge.
func pyNthStringArg(args *sitter.Node, src []byte, n int) string {
	pos := -1
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "keyword_argument", "comment":
			continue // not positional
		}
		pos++
		if pos == n {
			if c.Type() == "string" {
				return stripQuotes(nodeText(c, src))
			}
			return "" // n-th positional is not a plain string literal → dynamic
		}
	}
	return ""
}

// pyTemplateNameAttr scans a class body for a `template_name = '...'`
// assignment (Django class-based views) and returns the unquoted literal, or ""
// if absent / dynamic.
func pyTemplateNameAttr(body *sitter.Node, src []byte) string {
	for i := 0; i < int(body.NamedChildCount()); i++ {
		stmt := body.NamedChild(i)
		if stmt == nil {
			continue
		}
		// expression_statement → assignment
		var assign *sitter.Node
		if stmt.Type() == "expression_statement" && stmt.NamedChildCount() > 0 {
			assign = stmt.NamedChild(0)
		} else if stmt.Type() == "assignment" {
			assign = stmt
		}
		if assign == nil || assign.Type() != "assignment" {
			continue
		}
		left := assign.ChildByFieldName("left")
		right := assign.ChildByFieldName("right")
		if left == nil || right == nil {
			continue
		}
		if left.Type() == "identifier" && nodeText(left, src) == "template_name" && right.Type() == "string" {
			return stripQuotes(nodeText(right, src))
		}
	}
	return ""
}
