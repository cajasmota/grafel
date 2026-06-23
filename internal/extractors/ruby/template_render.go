// template_render.go — supplemental pass that emits RENDERS edges from Rails
// controller actions to a shared SCOPE.Template node (epic #3628). It lets the
// graph answer "what view does this action render?" (outbound RENDERS) and
// "who renders users/show?" (inbound RENDERS).
//
// Detected EXPLICIT render shapes (static only — honest-partial, precision-first):
//
//	render 'users/show'           → RENDERS users/show       (explicit template path)
//	render template: 'x/y'        → RENDERS x/y              (explicit :template)
//	render partial: 'list'        → RENDERS list             (explicit :partial)
//
// Intentionally DROPPED / honest-skip:
//
//	render @user / render(view)    (variable / object arg — dynamic)
//	render :index                  (SYMBOL → implicit-convention action view; the
//	                                target file action.html.erb is resolved by
//	                                Rails at runtime from the *controller+action*,
//	                                NOT a literal template path. Recording it as a
//	                                template-NAME node would fabricate a path, so
//	                                we skip the bare symbol form.)
//	(no render call → implicit render of action view)  — convention-only,
//	                                no literal path in source → honest-skip.
//
// All node/edge construction (convergence on one node per template name) lives
// in extractor.EmitTemplateEdges.

package ruby

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitTemplateRenderEdges scans every method body for explicit Rails `render`
// calls with a literal template path and appends template entities + RENDERS
// edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitTemplateRenderEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var edges []extractor.TemplateEdge

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "method", "singleton_method":
			name := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), name)
			}
			return
		case "call":
			if name, pat := rubyRenderTemplate(n, src); name != "" {
				edges = append(edges, extractor.TemplateEdge{Name: name, FromName: enclosing, Pattern: pat})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extractor.EmitTemplateEdges(entities, "ruby", edges)
}

// rubyRenderTemplate returns the literal template path + detector label for an
// explicit Rails `render` call, or ("","") otherwise. Accepted forms:
//
//	render 'users/show'        → ("users/show", "render_string")
//	render template: 'x/y'     → ("x/y", "render_template")
//	render partial: 'list'     → ("list", "render_partial")
//
// A bare receiver (`obj.render`) is NOT a Rails controller render and is
// rejected. Symbol args (render :index) and variable/object args are dropped.
func rubyRenderTemplate(call ts.Node, src []byte) (string, string) {
	// Must be a receiver-less `render ...` call (Rails controller DSL).
	if recv := call.ChildByFieldName("receiver"); recv != nil {
		return "", ""
	}
	method := call.ChildByFieldName("method")
	if method == nil || strings.TrimSpace(rubyNodeText(method, src)) != "render" {
		return "", ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", ""
	}

	// Inspect the first argument: a bare string (explicit path) or a pair with
	// a :template / :partial key.
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "string":
			if s := rubyStringContent(a, src); s != "" {
				return s, "render_string"
			}
			return "", "" // interpolated → dynamic
		case "pair":
			key, val := rubyPairParts(a, src)
			switch key {
			case "template":
				if val != "" {
					return val, "render_template"
				}
			case "partial":
				if val != "" {
					return val, "render_partial"
				}
			}
			// keep scanning later pairs (e.g. render layout:..., template:...)
			continue
		default:
			// First positional arg is a symbol (render :index), a variable, or an
			// object (render @user) — dynamic / convention-only, drop.
			return "", ""
		}
	}
	return "", ""
}

// rubyPairParts returns the hash_key_symbol name and, if the value is a literal
// string, its content — for a `key: 'value'` pair. A non-string value yields
// ("key", "").
func rubyPairParts(pair ts.Node, src []byte) (string, string) {
	var key, val string
	if k := pair.ChildByFieldName("key"); k != nil {
		key = strings.TrimSpace(rubyNodeText(k, src))
		key = strings.TrimSuffix(key, ":")
		key = strings.TrimPrefix(key, ":")
	}
	if v := pair.ChildByFieldName("value"); v != nil && v.Type() == "string" {
		val = rubyStringContent(v, src)
	}
	return key, val
}
