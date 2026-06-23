// angular_route_guards.go — Angular route-config guard/resolver wiring
// (issue #4415, follow-up to the global-DI pass #4378; epic #4334).
//
// Angular binds guard and resolver CLASSES to a route declaratively in the
// route-config array:
//
//	const routes: Routes = [{
//	  path: 'admin',
//	  component: AdminComponent,
//	  canActivate: [AuthGuard, RoleGuard],   // array-of-class form
//	  canActivateChild: [ChildGuard],
//	  canDeactivate: [UnsavedGuard],
//	  canMatch: [FeatureGuard],
//	  canLoad: [LazyGuard],
//	  resolve: { user: UserResolver },        // object-map form
//	}];
//
// #4378 covered NgModule / standalone-bootstrap providers but explicitly
// deferred route guards because route extraction lives on a separate path. This
// pass closes that gap: for each route object it emits a route → guard/resolver
// CLASS USES edge tagged di_role=guard|resolver. The guard interfaces
// (CanActivate / Resolve / …) already give the class an IMPLEMENTS edge via
// guard_interceptor_recognition (#2874); the NEW edge here is the
// route→guard USES wiring that connects the route declaration to the class.
//
// Edge target is a bare class identifier; it resolves to the declaring entity
// through resolve.BuildIndex's symbol table (the same convention the global-DI
// USES edges use). The edge hangs off the entity that owns the route table —
// the enclosing class entity when the route array sits inside a class body
// (e.g. an @NgModule with RouterModule.forRoot([...])), otherwise the file
// entity (entities[0]) for a top-level `const routes: Routes = [...]`.
//
// The modern FUNCTIONAL-guard form (`canActivate: [() => inject(Auth).ok()]`)
// is handled best-effort: when the arrow body statically references an injected
// service via `inject(ServiceClass)`, a USES edge to that service is emitted
// (di_role=guard, functional=true). When no service is statically recoverable
// (e.g. a free function reference resolved elsewhere) the entry is skipped
// honestly rather than guessed.
//
// No new entity Kind or relationship Kind is introduced — USES is reused, so
// internal/types stays exhaustive (the #2839 lesson).
package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// angularGuardArrayKeys are the route-config keys whose value is an array of
// guard classes (or functional guards). Each referenced class becomes a
// route → guard USES edge with di_role=guard.
var angularGuardArrayKeys = map[string]bool{
	"canActivate":      true,
	"canActivateChild": true,
	"canDeactivate":    true,
	"canMatch":         true,
	"canLoad":          true,
}

// angularRouteGuards is a program-level pass (invoked from Extract after the AST
// walk) that scans every route-config object literal for guard/resolver bindings
// and emits route → guard/resolver CLASS USES edges. It is a no-op on files with
// no Angular route config.
func (x *extractor) angularRouteGuards(root ts.Node) {
	if root == nil {
		return
	}
	for _, obj := range findAllNodes(root, "object") {
		if !x.angularLooksLikeRoute(obj) {
			continue
		}
		route := x.angularRoutePathLabel(obj)
		owner, ownerIdx := x.angularRouteOwner(obj)
		for _, edge := range x.angularRouteGuardEdges(obj, route, owner) {
			x.entities[ownerIdx].Relationships = append(x.entities[ownerIdx].Relationships, edge)
		}
	}
}

// angularLooksLikeRoute reports whether an object literal is an Angular route
// config: it must carry at least one guard/resolver key (canActivate / resolve /
// …). Requiring a guard key — not merely `path` — keeps the pass tightly scoped
// to routes that actually bind a guard, so a plain `{ path, component }` route
// emits no spurious USES edge (the regression invariant).
func (x *extractor) angularLooksLikeRoute(obj ts.Node) bool {
	for _, key := range x.angularObjectKeys(obj) {
		if angularGuardArrayKeys[key] || key == "resolve" {
			return true
		}
	}
	return false
}

// angularObjectKeys returns the property-key names of an object literal's direct
// `pair` children (shorthand/spread entries are skipped).
func (x *extractor) angularObjectKeys(obj ts.Node) []string {
	var keys []string
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		k := strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`)
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// angularRoutePathLabel returns a stable label for the route — its `path`
// segment, normalised so an empty default path renders as "<index>". When the
// route object has no `path` (a pathless layout route) it falls back to
// "<route>" so the edge still carries an anchor.
func (x *extractor) angularRoutePathLabel(obj ts.Node) string {
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		if strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`) != "path" {
			continue
		}
		val := strings.Trim(strings.TrimSpace(x.nodeText(pair.ChildByFieldName("value"))), `"'`)
		if val == "" {
			return "<index>"
		}
		return val
	}
	return "<route>"
}

// angularRouteOwner returns the name and entity index of the entity that should
// own the route's guard USES edges: the nearest enclosing class entity when the
// route array sits in a class body, otherwise the file entity (index 0). The
// index is guaranteed valid (the file entity always exists).
func (x *extractor) angularRouteOwner(obj ts.Node) (string, int) {
	for n := obj.Parent(); n != nil; n = n.Parent() {
		if n.Type() != "class_declaration" {
			continue
		}
		name := x.nodeText(n.ChildByFieldName("name"))
		if name == "" {
			break
		}
		if idx := x.entityIndexByName(name); idx >= 0 {
			return name, idx
		}
		break
	}
	return x.entities[0].Name, 0
}

// angularRouteGuardEdges builds the route → guard/resolver USES edges for one
// route object: the array-of-class guard keys (di_role=guard), the resolve
// object-map (di_role=resolver) and best-effort functional guards (the
// inject(Service) target). Duplicate (target, role) pairs within a route are
// de-duplicated.
func (x *extractor) angularRouteGuardEdges(obj ts.Node, route, owner string) []types.RelationshipRecord {
	var edges []types.RelationshipRecord
	seen := map[string]bool{}

	add := func(target, role, key string) {
		if target == "" {
			return
		}
		dedup := target + "|" + role
		if seen[dedup] {
			return
		}
		seen[dedup] = true
		edges = append(edges, types.RelationshipRecord{
			FromID: owner,
			ToID:   target,
			Kind:   string(types.RelationshipKindUses),
			Properties: map[string]string{
				"framework": "angular",
				"di_role":   role,
				"route":     route,
				"route_key": key,
				"owner":     owner,
				"via":       "angular_route_config",
			},
		})
	}

	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		key := strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`)
		val := pair.ChildByFieldName("value")
		if val == nil {
			continue
		}
		switch {
		case angularGuardArrayKeys[key]:
			for _, t := range x.angularGuardArrayTargets(val) {
				add(t.name, "guard", key)
			}
		case key == "resolve":
			// resolve: { dataKey: ResolverClass, ... } — object-map form.
			if val.Type() != "object" {
				continue
			}
			for j := 0; j < int(val.ChildCount()); j++ {
				rp := val.Child(j)
				if rp == nil || rp.Type() != "pair" {
					continue
				}
				resolver := angularLeafTypeName(strings.TrimSpace(x.nodeText(rp.ChildByFieldName("value"))))
				add(resolver, "resolver", key)
			}
		}
	}
	return edges
}

// angularGuardTarget is one resolved entry of a guard array: either a class
// identifier (functional=false) or the injected service of a functional guard
// (functional=true). A functional guard with no statically recoverable service
// yields no target and is skipped by the caller.
type angularGuardTarget struct {
	name       string
	functional bool
}

// angularGuardArrayTargets returns the guard targets of an array value. A bare
// identifier element is a class guard. An arrow-function element is a functional
// guard — best-effort resolved to the first `inject(Service)` class referenced
// in its body; if none is statically recoverable the element is dropped.
func (x *extractor) angularGuardArrayTargets(arr ts.Node) []angularGuardTarget {
	if arr == nil || arr.Type() != "array" {
		return nil
	}
	var out []angularGuardTarget
	for i := 0; i < int(arr.ChildCount()); i++ {
		el := arr.Child(i)
		if el == nil {
			continue
		}
		switch el.Type() {
		case "identifier":
			name := strings.TrimSpace(x.nodeText(el))
			if name != "" {
				out = append(out, angularGuardTarget{name: name})
			}
		case "arrow_function", "function", "function_expression":
			if svc := x.angularFunctionalGuardService(el); svc != "" {
				out = append(out, angularGuardTarget{name: svc, functional: true})
			}
		}
	}
	return out
}

// angularFunctionalGuardService best-effort recovers the injected service class
// of an inline functional guard: the first-argument identifier of an
// `inject(ServiceClass)` call inside the arrow body. Returns "" when no static
// inject(...) target is present (the honest-skip path).
func (x *extractor) angularFunctionalGuardService(fn ts.Node) string {
	for _, call := range findAllNodes(fn, "call_expression") {
		callee := call.ChildByFieldName("function")
		if callee == nil || x.nodeText(callee) != "inject" {
			continue
		}
		args := call.ChildByFieldName("arguments")
		if args == nil {
			continue
		}
		for i := 0; i < int(args.ChildCount()); i++ {
			a := args.Child(i)
			if a != nil && a.Type() == "identifier" {
				return strings.TrimSpace(x.nodeText(a))
			}
		}
	}
	return ""
}
