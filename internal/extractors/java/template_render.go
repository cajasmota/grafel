// template_render.go — supplemental pass that emits RENDERS edges from Spring
// MVC controller methods to a shared SCOPE.Template node (epic #3628). It lets
// the graph answer "what view does this handler render?" (outbound RENDERS) and
// "who renders users/list?" (inbound RENDERS).
//
// Detected shapes (static only — honest-partial, precision-first):
//
//	@Controller class X {
//	  @GetMapping("/users") public String list() { return "users/list"; }   → RENDERS users/list
//	  @GetMapping("/u")     public ModelAndView u() { return new ModelAndView("users/show"); }  → RENDERS users/show
//	}
//
// REST-vs-MVC honesty boundary (the crux of precision here): a String return
// value is a Spring *view name* ONLY in an MVC controller. It is NOT a view
// when the class is @RestController, or when the class/method carries
// @ResponseBody — in those cases the String is a serialized HTTP response body
// (REST), not a template. We therefore emit NO template edge for:
//
//	- @RestController classes
//	- methods (or whole classes) annotated @ResponseBody
//	- methods returning a computed/variable String (dynamic view name)
//	- methods with no Spring request-mapping annotation (not a handler)
//
// All node/edge construction (convergence on one node per view name) lives in
// extractor.EmitTemplateEdges.

package java

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// springMappingAnnotations are the Spring MVC request-mapping annotations whose
// presence marks a method as an HTTP handler whose String return is a view name.
var springMappingAnnotations = map[string]bool{
	"RequestMapping": true,
	"GetMapping":     true,
	"PostMapping":    true,
	"PutMapping":     true,
	"DeleteMapping":  true,
	"PatchMapping":   true,
}

// emitTemplateRenderEdges scans Spring MVC controllers for handler methods that
// return a static view name and appends template entities + RENDERS edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitTemplateRenderEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.TemplateEdge

	var walk func(n ts.Node, enclosingClass string)
	walk = func(n ts.Node, enclosingClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration":
			cls := childFieldText(n, "name", src)
			childCls := cls
			if enclosingClass != "" && cls != "" {
				childCls = enclosingClass + "." + cls
			}
			anns := annotationNames(n, src)
			// @RestController, or a class-level @ResponseBody, makes EVERY method
			// a REST endpoint — String returns are response bodies, not views.
			classIsRest := anns["RestController"] || anns["ResponseBody"]
			classIsMVC := anns["Controller"] && !classIsRest
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					ch := body.Child(i)
					if ch != nil && ch.Type() == "method_declaration" {
						if classIsMVC {
							if name, pat := javaSpringViewName(ch, childCls, src); name != "" {
								edges = append(edges, extractor.TemplateEdge{
									Name: name, FromName: childCls + "." + childFieldText(ch, "name", src), Pattern: pat,
								})
							}
						}
					}
					// Recurse for nested classes.
					walk(ch, childCls)
				}
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass)
		}
	}
	walk(root, "")

	extractor.EmitTemplateEdges(entities, "java", edges)
}

// javaSpringViewName returns the static view name a Spring MVC handler method
// renders, plus the detector pattern, or "", "" if the method is not a
// view-returning handler. Honesty guards:
//   - the method MUST carry a Spring request-mapping annotation (it is a handler)
//   - the method MUST NOT carry @ResponseBody (else String is a REST body)
//   - the returned value MUST be a String literal, or `new ModelAndView("...")`
//     with a literal first argument; anything else (variable, computed) → drop.
func javaSpringViewName(method ts.Node, enclosingClass string, src []byte) (string, string) {
	anns := annotationNames(method, src)
	if anns["ResponseBody"] {
		return "", "" // REST response body, not a view name
	}
	mapped := false
	for a := range anns {
		if springMappingAnnotations[a] {
			mapped = true
			break
		}
	}
	if !mapped {
		return "", "" // not an HTTP handler — no view inference
	}

	body := method.ChildByFieldName("body")
	if body == nil {
		return "", ""
	}
	// Find a `return <expr>;` whose expr is a String literal or ModelAndView(...).
	var found, pat string
	var scan func(n ts.Node)
	scan = func(n ts.Node) {
		if n == nil || found != "" {
			return
		}
		// Do not descend into nested type/lambda bodies that could carry their
		// own returns; Spring handler bodies are flat enough that a shallow scan
		// over return_statement nodes is correct, and nested lambdas returning
		// strings are not the handler's view. We still recurse generally but the
		// first matching literal return wins.
		if n.Type() == "return_statement" {
			if name, p := javaReturnViewName(n, src); name != "" {
				found, pat = name, p
				return
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			scan(n.Child(i))
		}
	}
	scan(body)
	if found == "" {
		return "", ""
	}
	_ = enclosingClass
	return found, pat
}

// javaReturnViewName extracts a static view name from a return_statement:
//
//	return "users/list";                 → "users/list", "spring_view"
//	return new ModelAndView("users/x");  → "users/x",     "model_and_view"
//
// Returns "", "" for a non-literal return (variable / concatenation / computed),
// so dynamic view names never fabricate a node.
func javaReturnViewName(ret ts.Node, src []byte) (string, string) {
	var expr ts.Node
	for i := 0; i < int(ret.NamedChildCount()); i++ {
		c := ret.NamedChild(i)
		if c != nil {
			expr = c
			break
		}
	}
	if expr == nil {
		return "", ""
	}
	switch expr.Type() {
	case "string_literal":
		return javaStripString(nodeText(expr, src)), "spring_view"
	case "object_creation_expression":
		typeNode := expr.ChildByFieldName("type")
		if typeNode == nil || lastIdent(strings.TrimSpace(nodeText(typeNode, src))) != "ModelAndView" {
			return "", ""
		}
		args := expr.ChildByFieldName("arguments")
		if args == nil {
			return "", ""
		}
		for i := 0; i < int(args.NamedChildCount()); i++ {
			a := args.NamedChild(i)
			if a == nil {
				continue
			}
			if a.Type() == "string_literal" {
				return javaStripString(nodeText(a, src)), "model_and_view"
			}
			return "", "" // first ctor arg is dynamic → drop
		}
	}
	return "", ""
}

// annotationNames returns the set of simple annotation names on a declaration's
// immediate modifiers (e.g. {"Controller": true, "GetMapping": true}).
func annotationNames(decl ts.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	for _, a := range findAnnotations(decl, src) {
		if a.name != "" {
			out[a.name] = true
		}
	}
	return out
}
