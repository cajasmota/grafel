// django_admin.go — supplemental Python-extractor pass for Django admin
// modules (`admin.py`). Emits two complementary surfaces:
//
//   1. REFERENCES edges from the admin MODULE entity to every Model and
//      ModelAdmin class registered in the file via:
//
//         admin.site.register(M, A)
//         admin.site.register(M)              # bare register
//         @admin.register(M [, M2, …])        # decorator on ModelAdmin
//
//      The existing per-class REGISTERS edge (internal/custom/python/
//      django.go) wires the ModelAdmin → Model relationship, but leaves
//      the admin module itself disconnected. This pass emits module-level
//      REFERENCES so admin.py shows up as a connected island in the graph.
//
//   2. Property capture on every ModelAdmin class — the keys list_display,
//      list_filter, search_fields, readonly_fields, fieldsets, inlines,
//      ordering, date_hierarchy, actions. W4R4 evidence showed list_display
//      and list_filter present but search_fields missing. This pass
//      captures the complete set as flat string properties on the
//      ModelAdmin class entity so docgen can render the admin contract.
//
//   3. Custom admin actions: methods decorated with @admin.action(
//      description="…") on a ModelAdmin class are stamped with
//      Properties["admin_action"]="true" and Properties["description"]
//      so they surface as first-class operations in the admin doc page.
//
// Runs after walkNode + extractClassFields so we can mutate emitted
// SCOPE.Operation / SCOPE.Component entities in place.

package python

import (
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// adminModelAdminProps is the canonical list of Django ModelAdmin attributes
// the docgen surface cares about. Captured as flat key/value strings on the
// ModelAdmin class entity. Values are stored as the trimmed source-text of
// the RHS expression so a `list_display = ("a", "b")` survives without
// any value-shape normalisation here (downstream renderers can format it).
var adminModelAdminProps = []string{
	"list_display",
	"list_filter",
	"search_fields",
	"readonly_fields",
	"fieldsets",
	"inlines",
	"ordering",
	"date_hierarchy",
	"actions",
}

// emitDjangoAdminEdges is the entry point invoked from Extract. No-op for
// non-admin files.
func emitDjangoAdminEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	if !isDjangoAdminFile(file.Path) {
		return
	}

	// 1. Top-level admin.site.register(M [, A]) calls.
	registered := collectAdminSiteRegisterCalls(root, file.Content)
	// 2. @admin.register(M [, M2]) decorator → ModelAdmin class targets.
	decorRegistered := collectAdminDecoratorRegisterCalls(root, file.Content)

	// Emit REFERENCES edges on the file entity for each (model, admin) pair.
	fileEnt := &(*entities)[0]
	seen := map[string]bool{}
	emit := func(target string) {
		if target == "" {
			return
		}
		toID := buildAdminClassRef(file.Path, target)
		key := toID
		if seen[key] {
			return
		}
		seen[key] = true
		fileEnt.Relationships = append(fileEnt.Relationships,
			types.RelationshipRecord{
				ToID: toID,
				Kind: string(types.RelationshipKindReferences),
				Properties: map[string]string{
					"framework":    "django",
					"pattern_type": "admin_register",
				},
			})
	}
	for _, p := range registered {
		emit(p.model)
		emit(p.admin)
	}
	for _, p := range decorRegistered {
		for _, m := range p.models {
			emit(m)
		}
		emit(p.adminClass)
	}

	// 3. ModelAdmin property capture + custom admin actions.
	captureModelAdminProperties(root, file, entities)
}

// adminSiteRegisterPair captures one admin.site.register(M, A) call's args.
// admin may be empty when the bare 1-arg form is used.
type adminSiteRegisterPair struct {
	model string
	admin string
}

// collectAdminSiteRegisterCalls scans the module root for top-level
// `admin.site.register(...)` calls and returns the (model, admin) pairs.
//
// Both positional and dotted-identifier args survive intact. The first arg
// may itself be a list `[M1, M2]` (Django supports registering multiple
// models in one call); we expand into one pair per model.
func collectAdminSiteRegisterCalls(root ts.Node, src []byte) []adminSiteRegisterPair {
	var out []adminSiteRegisterPair
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "call" {
			fn := n.ChildByFieldName("function")
			if fn != nil && strings.TrimSpace(nodeText(fn, src)) == "admin.site.register" {
				args := n.ChildByFieldName("arguments")
				if args != nil && args.NamedChildCount() > 0 {
					first := args.NamedChild(0)
					var models []string
					if first != nil && (first.Type() == "list" || first.Type() == "tuple") {
						for i := 0; i < int(first.NamedChildCount()); i++ {
							ch := first.NamedChild(i)
							if ch == nil {
								continue
							}
							if t := strings.TrimSpace(nodeText(ch, src)); t != "" {
								models = append(models, t)
							}
						}
					} else if first != nil {
						if t := strings.TrimSpace(nodeText(first, src)); t != "" {
							models = []string{t}
						}
					}
					var adminCls string
					if args.NamedChildCount() > 1 {
						second := args.NamedChild(1)
						if second != nil && second.Type() != "keyword_argument" {
							adminCls = strings.TrimSpace(nodeText(second, src))
						}
					}
					for _, m := range models {
						out = append(out, adminSiteRegisterPair{model: m, admin: adminCls})
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

// adminDecoratorRegister captures one @admin.register(M, M2, …) decorator
// site and the class it precedes.
type adminDecoratorRegister struct {
	models     []string
	adminClass string
}

// collectAdminDecoratorRegisterCalls scans the module for class definitions
// whose decorator list includes `@admin.register(M [, M2, …])` and returns
// the captured model list + admin class name.
func collectAdminDecoratorRegisterCalls(root ts.Node, src []byte) []adminDecoratorRegister {
	var out []adminDecoratorRegister
	var walk func(n ts.Node)
	walk = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "decorated_definition" {
			inner := n.ChildByFieldName("definition")
			if inner != nil && inner.Type() == "class_definition" {
				nameNode := inner.ChildByFieldName("name")
				if nameNode != nil {
					adminClass := nodeText(nameNode, src)
					for i := 0; i < int(n.ChildCount()); i++ {
						ch := n.Child(i)
						if ch == nil || ch.Type() != "decorator" {
							continue
						}
						// Decorator first named child is `call` for the
						// `@admin.register(M)` form.
						for j := 0; j < int(ch.NamedChildCount()); j++ {
							call := ch.NamedChild(j)
							if call == nil || call.Type() != "call" {
								continue
							}
							fn := call.ChildByFieldName("function")
							if fn == nil || strings.TrimSpace(nodeText(fn, src)) != "admin.register" {
								continue
							}
							args := call.ChildByFieldName("arguments")
							if args == nil {
								continue
							}
							var models []string
							for k := 0; k < int(args.NamedChildCount()); k++ {
								arg := args.NamedChild(k)
								if arg == nil {
									continue
								}
								if arg.Type() == "keyword_argument" {
									continue
								}
								if t := strings.TrimSpace(nodeText(arg, src)); t != "" {
									models = append(models, t)
								}
							}
							if len(models) > 0 {
								out = append(out, adminDecoratorRegister{models: models, adminClass: adminClass})
							}
						}
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

// captureModelAdminProperties walks every class_definition and, for any class
// that extends ModelAdmin / admin.ModelAdmin / TabularInline / StackedInline,
// captures the canonical ModelAdmin attribute keys (list_display, etc.) as
// flat properties on the class entity. Also tags @admin.action methods.
func captureModelAdminProperties(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			handleAdminClass(n, parentClass, file, entities)
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
					walk(body.Child(i), childCls)
				}
			}
			return
		case "decorated_definition":
			inner := n.ChildByFieldName("definition")
			if inner != nil && inner.Type() == "class_definition" {
				handleAdminClass(inner, parentClass, file, entities)
				// Recurse into the inner class body too.
				nameNode := inner.ChildByFieldName("name")
				cls := ""
				if nameNode != nil {
					cls = nodeText(nameNode, file.Content)
				}
				childCls := cls
				if parentClass != "" && cls != "" {
					childCls = parentClass + "." + cls
				}
				body := inner.ChildByFieldName("body")
				if body != nil {
					for i := 0; i < int(body.ChildCount()); i++ {
						walk(body.Child(i), childCls)
					}
				}
				return
			}
			if inner != nil && inner.Type() == "function_definition" {
				// Method-level @admin.action handling.
				if parentClass != "" {
					stampAdminActionOnMethod(n, inner, parentClass, file, entities)
				}
				return
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")
}

// handleAdminClass captures ModelAdmin properties on the matching class entity.
func handleAdminClass(cd ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	nameNode := cd.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	clsLeaf := nodeText(nameNode, file.Content)
	if clsLeaf == "" {
		return
	}
	if !classExtendsAdmin(cd, file.Content) {
		return
	}
	emittedName := clsLeaf
	if parentClass != "" {
		emittedName = parentClass + "." + clsLeaf
	}
	cls := findClassByName(*entities, file.Path, emittedName)
	if cls == nil {
		return
	}
	if cls.Properties == nil {
		cls.Properties = map[string]string{}
	}
	cls.Properties["framework"] = "django"
	cls.Properties["component_kind"] = "model_admin"

	body := cd.ChildByFieldName("body")
	if body == nil {
		return
	}
	props := parseSimpleAssignments(body, file.Content)
	for _, key := range adminModelAdminProps {
		if v, ok := props[key]; ok {
			cls.Properties[key] = v
		}
	}
}

// classExtendsAdmin reports whether the class extends one of the Django
// admin base classes: ModelAdmin, admin.ModelAdmin, TabularInline,
// StackedInline, admin.TabularInline, admin.StackedInline.
func classExtendsAdmin(cd ts.Node, src []byte) bool {
	supers := cd.ChildByFieldName("superclasses")
	if supers == nil {
		return false
	}
	t := nodeText(supers, src)
	for _, base := range []string{
		"ModelAdmin",
		"TabularInline",
		"StackedInline",
	} {
		if strings.Contains(t, base) {
			return true
		}
	}
	return false
}

// stampAdminActionOnMethod looks for @admin.action(...) on the decorated
// method and, when found, stamps Properties["admin_action"]="true" plus the
// captured description / permissions kwargs on the matching Operation
// entity.
func stampAdminActionOnMethod(decorated, inner ts.Node, parentClass string, file extractor.FileInput, entities *[]types.EntityRecord) {
	methodNameNode := inner.ChildByFieldName("name")
	if methodNameNode == nil {
		return
	}
	methodName := nodeText(methodNameNode, file.Content)
	if methodName == "" {
		return
	}
	props := map[string]string{}
	found := false
	for i := 0; i < int(decorated.ChildCount()); i++ {
		ch := decorated.Child(i)
		if ch == nil || ch.Type() != "decorator" {
			continue
		}
		// Look for `@admin.action(...)` — a call whose function leaf is "action"
		// and whose receiver text contains "admin".
		for j := 0; j < int(ch.NamedChildCount()); j++ {
			cand := ch.NamedChild(j)
			if cand == nil || cand.Type() != "call" {
				continue
			}
			fn := cand.ChildByFieldName("function")
			if fn == nil {
				continue
			}
			fnText := strings.TrimSpace(nodeText(fn, file.Content))
			if fnText != "admin.action" && fnText != "action" {
				continue
			}
			if fnText == "action" && decoratorLeaf(fn, file.Content) != "action" {
				continue
			}
			found = true
			args := cand.ChildByFieldName("arguments")
			if args == nil {
				continue
			}
			for k := 0; k < int(args.NamedChildCount()); k++ {
				arg := args.NamedChild(k)
				if arg == nil || arg.Type() != "keyword_argument" {
					continue
				}
				nm := arg.ChildByFieldName("name")
				vl := arg.ChildByFieldName("value")
				if nm == nil || vl == nil {
					continue
				}
				switch nodeText(nm, file.Content) {
				case "description":
					if v := stripQuotes(strings.TrimSpace(nodeText(vl, file.Content))); v != "" {
						props["description"] = v
					}
				case "permissions":
					if v := strings.TrimSpace(nodeText(vl, file.Content)); v != "" {
						props["permissions"] = v
					}
				}
			}
		}
	}
	if !found {
		return
	}
	props["admin_action"] = "true"
	emittedName := parentClass + "." + methodName
	op := findOpByName(*entities, file.Path, emittedName)
	if op == nil {
		return
	}
	if op.Properties == nil {
		op.Properties = map[string]string{}
	}
	for k, v := range props {
		op.Properties[k] = v
	}
}

// isDjangoAdminFile reports whether path is a Django admin module. We accept
// the canonical `admin.py` and also `admin/` package files (admin/__init__.py
// and any sibling submodule like admin/users.py) so app-split admin layouts
// still get processed.
func isDjangoAdminFile(path string) bool {
	p := filepath.ToSlash(path)
	base := filepath.Base(p)
	if base == "admin.py" {
		return true
	}
	// admin/<anything>.py — match any segment "admin" in the dirname.
	dir := filepath.Dir(p)
	for _, seg := range strings.Split(dir, "/") {
		if seg == "admin" {
			return true
		}
	}
	return false
}

// buildAdminClassRef returns a structural-ref ToID for an admin REFERENCES
// edge target. The resolver's by-Name index across SCOPE.Component/class
// (Django Model) and SCOPE.Component/admin_class (#1374) picks the canonical
// target.
func buildAdminClassRef(filePath, name string) string {
	return "scope:component:ref:python:" + filepath.ToSlash(filePath) + ":" + name
}
