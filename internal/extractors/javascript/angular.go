// angular.go — Angular component/structure recognition for the JS/TS AST
// extractor (issue #2854, Structure group).
//
// Angular declares its building blocks via TypeScript class decorators, NOT
// via React-style function components or HOCs:
//
//	@Component({selector, template/templateUrl})  → UI component
//	@Directive({selector})                        → attribute/structural directive
//	@Injectable()                                 → DI service (provider)
//	@Pipe({name})                                 → template transform
//	@NgModule({declarations, imports, providers}) → module
//
// In the tree-sitter TS/TSX grammar a decorated class surfaces as a
// `decorator` node that is a *previous sibling* of the `class_declaration`
// (either directly at the program level, or inside an `export_statement`).
// handleClassDeclaration consults angularDecoratorFor to discover that sibling.
//
// Capability mapping (Structure group, lang.jsts.framework.angular):
//   - component_extraction : @Component / @Directive classes emit
//     SCOPE.Component subtype="angular_component" / "angular_directive".
//   - context_extraction   : @Injectable services are Angular's dependency-
//     injection "context" providers; constructor-injected services emit
//     INJECTS edges (provider→consumer) — the Angular analogue of React
//     context provide/consume.
//   - hoc_wrapper_recognition : not applicable to Angular (no higher-order
//     component pattern) — recorded as not_applicable in the registry.
//
// Decorator argument metadata (selector, template inline child tags) is parsed
// best-effort from the decorator object literal so template composition emits
// RENDERS edges, mirroring the React (#610) and Vue/Svelte SFC extractors.
package javascript

import (
	"fmt"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// angularClassDecorators maps a recognised Angular decorator identifier to the
// SCOPE.Component subtype emitted for the decorated class.
var angularClassDecorators = map[string]string{
	"Component":  "angular_component",
	"Directive":  "angular_directive",
	"Injectable": "angular_service",
	"Pipe":       "angular_pipe",
	"NgModule":   "angular_module",
}

// reAngularPascalTag matches PascalCase / kebab custom-element tags in an inline
// Angular template string. Angular component selectors are conventionally
// kebab-case custom elements (e.g. `<app-child>`), so we capture tags that
// contain a hyphen or start uppercase and are not bare HTML built-ins.
var reAngularPascalTag = regexp.MustCompile(`<([a-z][a-z0-9]*-[a-z0-9-]*|[A-Z][A-Za-z0-9]*)\b`)

// angularDecoratorFor returns the Angular decorator identifier (e.g.
// "Component") and the decorator's call_expression node for a class_declaration
// node, or ("", nil) when the class is not Angular-decorated.
//
// The decorator is located by scanning previous siblings of the class node
// (and, when the class is inside an export_statement, the export_statement's
// previous siblings are folded in because the decorator is a child of the
// export_statement in that grammar shape).
func (x *extractor) angularDecoratorFor(class *sitter.Node) (string, *sitter.Node) {
	if class == nil {
		return "", nil
	}
	// Decorators are siblings within the same parent (export_statement or
	// program/statement_block). Walk previous siblings looking for a
	// `decorator` node.
	parent := class.Parent()
	if parent == nil {
		return "", nil
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		c := parent.Child(i)
		if c == nil || c.Type() != "decorator" {
			continue
		}
		name, call := x.decoratorIdent(c)
		if sub, ok := angularClassDecorators[name]; ok && sub != "" {
			return name, call
		}
	}
	return "", nil
}

// decoratorIdent returns the decorator's identifier name and its underlying
// call_expression (when the decorator is a call like `@Component({...})`).
func (x *extractor) decoratorIdent(dec *sitter.Node) (string, *sitter.Node) {
	for i := 0; i < int(dec.ChildCount()); i++ {
		c := dec.Child(i)
		switch c.Type() {
		case "identifier", "type_identifier":
			return x.nodeText(c), nil
		case "call_expression":
			fn := c.ChildByFieldName("function")
			if fn != nil {
				return x.nodeText(fn), c
			}
		}
	}
	return "", nil
}

// handleAngularClass emits an Angular class entity (component/directive/
// service/pipe/module) for a decorated class. It returns true when the class
// was Angular-decorated and fully handled (the generic class path should be
// skipped). The decorator name + call node come from angularDecoratorFor.
func (x *extractor) handleAngularClass(n *sitter.Node, decorator string, call *sitter.Node) bool {
	subtype, ok := angularClassDecorators[decorator]
	if !ok {
		return false
	}
	nameNode := n.ChildByFieldName("name")
	className := x.nodeText(nameNode)
	if className == "" {
		return false
	}

	props := map[string]string{
		"framework":          "angular",
		"angular_decorator":  decorator,
		"angular_class_kind": subtype,
	}

	var rels []types.RelationshipRecord

	// Parse the decorator object-literal metadata (selector / inline template
	// child tags / providers) best-effort.
	if call != nil {
		meta := x.angularDecoratorMeta(call)
		if sel := meta["selector"]; sel != "" {
			props["selector"] = sel
		}
		if tmpl := meta["template"]; tmpl != "" {
			for _, tag := range angularTemplateTags(tmpl, meta["selector"]) {
				rels = append(rels, types.RelationshipRecord{
					ToID: tag,
					Kind: "RENDERS",
					Properties: map[string]string{
						"renderer":  className,
						"framework": "angular",
					},
				})
			}
		}
	}

	// context_extraction: constructor-injected services → INJECTED_INTO edges
	// (Angular DI is the framework's context provide/consume mechanism). The
	// edge convention matches the framework DI rules (fastapi/quarkus/axum):
	// provider INJECTED_INTO consumer, so FromID is the injected service and
	// ToID is the decorated class.
	if body := n.ChildByFieldName("body"); body != nil {
		for _, dep := range x.angularConstructorInjections(body) {
			rels = append(rels, types.RelationshipRecord{
				FromID: dep,
				ToID:   className,
				Kind:   string(types.RelationshipKindInjectedInto),
				Properties: map[string]string{
					"consumer":  className,
					"provider":  dep,
					"framework": "angular",
				},
			})
		}
	}

	sig := fmt.Sprintf("@%s class %s", decorator, className)

	// Emit the Angular class entity, then attribute its body operations via
	// CONTAINS (mirrors handleClassDeclaration).
	classIdx := len(x.entities)
	x.emitWithProps(className, "SCOPE.Component", n, subtype, sig, props, rels)

	body := n.ChildByFieldName("body")
	if body != nil {
		cb := &classBindings{className: className, fields: map[string]string{}}
		x.collectClassFields(body, cb.fields)
		before := len(x.entities)
		x.walkChildren(body, className, cb)
		after := len(x.entities)
		for k := before; k < after; k++ {
			child := &x.entities[k]
			if child.Kind != "SCOPE.Operation" {
				continue
			}
			toID := extreg.BuildOperationStructuralRef(x.language, x.filePath, child.Name)
			x.entities[classIdx].Relationships = append(x.entities[classIdx].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		}
	}
	return true
}

// angularDecoratorMeta extracts string values for the keys we care about
// (selector, template) from a decorator call's first object-literal argument.
// templateUrl is recorded under "template_url" but does not yield RENDERS edges
// (the markup lives in a separate file the extractor does not parse here).
func (x *extractor) angularDecoratorMeta(call *sitter.Node) map[string]string {
	out := map[string]string{}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return out
	}
	var obj *sitter.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		if args.Child(i).Type() == "object" {
			obj = args.Child(i)
			break
		}
	}
	if obj == nil {
		return out
	}
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair.Type() != "pair" {
			continue
		}
		key := pair.ChildByFieldName("key")
		val := pair.ChildByFieldName("value")
		if key == nil || val == nil {
			continue
		}
		k := strings.Trim(x.nodeText(key), `"'`)
		switch k {
		case "selector":
			out["selector"] = stringLiteralValue(x.nodeText(val))
		case "template":
			out["template"] = stringLiteralValue(x.nodeText(val))
		case "templateUrl":
			out["template_url"] = stringLiteralValue(x.nodeText(val))
		case "name":
			out["name"] = stringLiteralValue(x.nodeText(val))
		}
	}
	return out
}

// angularConstructorInjections returns the injected service type names found in
// the class constructor's parameter list, e.g. `constructor(private http:
// HttpClient, store: Store)` → ["HttpClient", "Store"]. These are Angular's DI
// "context" dependencies (context_extraction capability).
func (x *extractor) angularConstructorInjections(body *sitter.Node) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < int(body.ChildCount()); i++ {
		m := body.Child(i)
		if m.Type() != "method_definition" {
			continue
		}
		nameNode := m.ChildByFieldName("name")
		if nameNode == nil || x.nodeText(nameNode) != "constructor" {
			continue
		}
		params := m.ChildByFieldName("parameters")
		if params == nil {
			continue
		}
		for j := 0; j < int(params.ChildCount()); j++ {
			p := params.Child(j)
			// required_parameter / optional_parameter carry a type annotation.
			tn := p.ChildByFieldName("type")
			if tn == nil {
				continue
			}
			typeName := angularLeafTypeName(x.nodeText(tn))
			if typeName == "" || seen[typeName] {
				continue
			}
			seen[typeName] = true
			out = append(out, typeName)
		}
	}
	return out
}

// angularLeafTypeName normalises a type-annotation string ("`: HttpClient`",
// "`: Store<AppState>`") to its leaf identifier ("HttpClient", "Store").
func angularLeafTypeName(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), ":"))
	if idx := strings.IndexAny(s, "<|& "); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	// Reject primitives / bare structural shapes.
	switch s {
	case "", "string", "number", "boolean", "any", "void", "object", "unknown", "never":
		return ""
	}
	// Must look like a type identifier (starts uppercase by Angular convention).
	if s[0] < 'A' || s[0] > 'Z' {
		return ""
	}
	return s
}

// angularTemplateTags returns the distinct custom-element / component tags
// referenced inside an inline Angular template string, excluding the
// component's own selector.
func angularTemplateTags(template, selfSelector string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range reAngularPascalTag.FindAllStringSubmatch(template, -1) {
		tag := m[1]
		if tag == "" || tag == selfSelector || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

// stringLiteralValue strips surrounding quotes / backticks from a string-literal
// node's raw text.
func stringLiteralValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' || first == '"' || first == '`') && first == last {
			return s[1 : len(s)-1]
		}
	}
	return s
}
