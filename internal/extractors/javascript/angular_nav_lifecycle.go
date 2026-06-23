// angular_nav_lifecycle.go — Angular Navigation + Lifecycle extraction for the
// JS/TS AST extractor (issue #2856).
//
// This file closes two Angular capability cells, mirroring the React reference
// implementation (navigation.go for router_pattern; the useState/useReducer
// setter lift in extractor.go for state_setter_emission):
//
//   - Navigation / router_pattern   — Angular client-side routing. Three
//     idioms are recognised:
//     1. RouterModule.forRoot([{path:'x', component:Y}]) / forChild([...])
//     route table declarations → one NAVIGATES_TO edge per declared path.
//     2. Imperative navigation: this.router.navigate(['/path', id]) and
//     this.router.navigateByUrl('/path') → NAVIGATES_TO edge.
//     3. Template directives: routerLink="/path" and [routerLink]="['/path']"
//     inside an inline @Component template → NAVIGATES_TO edge.
//
//   - Lifecycle / state_setter_emission — Angular state mutation points. Two
//     idioms are recognised, each emitting a SCOPE.Operation subtype
//     "state_setter" plus a WRITES_TO edge from the setter call site to the
//     state it mutates (the same "edge from setter → state" semantic React
//     realises through its [value, setter] tuple lift):
//     1. Angular signals: `const count = signal(0)` declares the state;
//     `count.set(v)` / `count.update(fn)` are the setters. WRITES_TO
//     targets the signal binding name.
//     2. ngrx dispatch: `this.store.dispatch(loadUser())` mutates the ngrx
//     store; WRITES_TO targets the dispatched action type.
//
// All edges/entities reuse existing Kinds (NAVIGATES_TO, WRITES_TO,
// SCOPE.Operation) so internal/types/ stays green — no new Kind is introduced.
package javascript

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// angularRouterNavMethods are the imperative @angular/router Router methods that
// constitute a client-side navigation.
var angularRouterNavMethods = map[string]bool{
	"navigate":      true,
	"navigateByUrl": true,
}

// reAngularRouterLink matches a routerLink attribute in an inline Angular
// template, in either the string form (routerLink="/path") or the property-
// binding form ([routerLink]="['/path', id]"). Capture group 1 is the raw
// attribute value (without the surrounding quotes).
var reAngularRouterLink = regexp.MustCompile(`\[?routerLink\]?\s*=\s*"([^"]*)"`)

// reAngularRouterModuleRoutes matches RouterModule.forRoot([...]) /
// RouterModule.forChild([...]) so the bracketed route array can be scanned for
// path declarations.
var reAngularRouterModuleRoutes = regexp.MustCompile(`RouterModule\s*\.\s*for(?:Root|Child)\s*\(`)

// reAngularRoutePath matches a `path: 'segment'` entry inside a route config
// object literal. Capture group 1 is the path string (without quotes).
var reAngularRoutePath = regexp.MustCompile(`\bpath\s*:\s*['"]([^'"]*)['"]`)

// angularNavigationRels scans a class/method body for imperative Angular Router
// navigation calls (this.router.navigate([...]) / navigateByUrl('/x')) and
// returns one NAVIGATES_TO edge per distinct route. The receiver must look like
// a Router (a `router`-suffixed identifier or `.router` member access) so plain
// array .navigate-like calls are not misread.
func (x *extractor) angularNavigationRels(body ts.Node, className string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, call := range findAllNodes(body, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			continue
		}
		propNode := fn.ChildByFieldName("property")
		if propNode == nil {
			continue
		}
		method := x.nodeText(propNode)
		if !angularRouterNavMethods[method] {
			continue
		}
		recv := fn.ChildByFieldName("object")
		if recv == nil || !angularLooksLikeRouter(x.nodeText(recv)) {
			continue
		}
		route := angularNavRouteArg(x, call, method)
		if route == "" {
			continue
		}
		if seen[route] {
			continue
		}
		seen[route] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "route:" + route,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: map[string]string{
				"route":     route,
				"via":       "angular_router",
				"method":    method,
				"caller":    className,
				"framework": "angular",
				"line":      strconv.Itoa(int(call.StartPoint().Row) + 1),
			},
		})
	}
	return rels
}

// angularLooksLikeRouter reports whether a receiver expression text references
// an @angular/router Router instance (a `router` identifier or a `.router`
// member access, e.g. `this.router`).
func angularLooksLikeRouter(recvText string) bool {
	lower := strings.ToLower(recvText)
	return lower == "router" || strings.HasSuffix(lower, ".router")
}

// angularNavRouteArg extracts the destination route from the first argument of
// an Angular Router navigation call. navigate() takes an array of URL commands
// (`['/users', id]`); navigateByUrl() takes a string. Array commands are joined
// with '/' and dynamic (non-literal) segments are normalised to the {*}
// sentinel so the route matches server-side definitions.
func angularNavRouteArg(x *extractor, call ts.Node, method string) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	first := firstMeaningfulArg(args)
	if first == nil {
		return ""
	}
	switch first.Type() {
	case "string":
		return strings.Trim(x.nodeText(first), `"'`+"`")
	case "template_string":
		return normalizeTemplateLiteralRoute(x.nodeText(first))
	case "array":
		return angularURLCommandsToRoute(x, first)
	}
	return ""
}

// angularURLCommandsToRoute joins an Angular URL-command array (`['/users', id,
// 'edit']`) into a single route string. String-literal segments are kept;
// dynamic segments (identifiers, member expressions) become the {*} sentinel.
func angularURLCommandsToRoute(x *extractor, arr ts.Node) string {
	var segs []string
	for i := 0; i < int(arr.ChildCount()); i++ {
		c := arr.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "[", "]", ",":
			continue
		case "string":
			segs = append(segs, strings.Trim(x.nodeText(c), `"'`+"`"))
		case "template_string":
			segs = append(segs, strings.Trim(normalizeTemplateLiteralRoute(x.nodeText(c)), "/"))
		default:
			segs = append(segs, "{*}")
		}
	}
	if len(segs) == 0 {
		return ""
	}
	joined := strings.Join(segs, "/")
	// Collapse accidental double slashes from a leading '/' segment.
	joined = strings.ReplaceAll(joined, "//", "/")
	return joined
}

// angularRouterLinkRels scans an inline Angular template string for routerLink
// directives and returns one NAVIGATES_TO edge per distinct destination. Both
// the string form (routerLink="/x") and binding form ([routerLink]="['/x']")
// are recognised. The component's source node anchors the line number.
func (x *extractor) angularRouterLinkRels(template, className string, anchor ts.Node) []types.RelationshipRecord {
	if template == "" {
		return nil
	}
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	line := 1
	if anchor != nil {
		line = int(anchor.StartPoint().Row) + 1
	}
	for _, m := range reAngularRouterLink.FindAllStringSubmatch(template, -1) {
		route := angularRouterLinkValue(m[1])
		if route == "" || seen[route] {
			continue
		}
		seen[route] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "route:" + route,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: map[string]string{
				"route":     route,
				"via":       "router_link",
				"caller":    className,
				"framework": "angular",
				"line":      strconv.Itoa(line),
			},
		})
	}
	return rels
}

// angularRouterLinkValue normalises a routerLink attribute value into a route
// string. The binding form carries an array literal (`['/users', id]`) which is
// joined like a URL-command array; the string form is a plain path.
func angularRouterLinkValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		// Array-literal binding: ['/users', id]. Split on commas, keep string
		// literals, collapse dynamic segments to {*}.
		inner := strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
		var segs []string
		for _, part := range strings.Split(inner, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if len(part) >= 2 && (part[0] == '\'' || part[0] == '"') {
				segs = append(segs, strings.Trim(part, `'"`))
			} else {
				segs = append(segs, "{*}")
			}
		}
		if len(segs) == 0 {
			return ""
		}
		return strings.ReplaceAll(strings.Join(segs, "/"), "//", "/")
	}
	return raw
}

// angularRouteTableRels scans a class/program body for RouterModule.forRoot /
// forChild route-table declarations and returns one NAVIGATES_TO edge per
// declared `path:` segment. This captures the route *definitions* (the
// declarative half of router_pattern) distinct from imperative navigation.
func (x *extractor) angularRouteTableRels(body ts.Node, className string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	text := x.nodeText(body)
	if !reAngularRouterModuleRoutes.MatchString(text) {
		return nil
	}
	var rels []types.RelationshipRecord
	seen := map[string]bool{}
	for _, m := range reAngularRoutePath.FindAllStringSubmatch(text, -1) {
		route := strings.TrimSpace(m[1])
		// Normalise an empty path (the default child route) to a stable label.
		display := route
		if display == "" {
			display = "<index>"
		}
		if seen[display] {
			continue
		}
		seen[display] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "route:" + display,
			Kind: string(types.RelationshipKindNavigatesTo),
			Properties: map[string]string{
				"route":     display,
				"via":       "route_table",
				"caller":    className,
				"framework": "angular",
			},
		})
	}
	return rels
}

// angularStateSetterEmission scans a class body for Angular state-setter idioms
// and returns SCOPE.Operation subtype="state_setter" entities plus WRITES_TO
// edges from each setter to the state it mutates (issue #2856 —
// Lifecycle/state_setter_emission). Two idioms are recognised:
//
//	signal:   const count = signal(0); count.set(v); count.update(fn)
//	          → setter operation "count.set" WRITES_TO state "count"
//	ngrx:     this.store.dispatch(loadUser())
//	          → setter operation "dispatch:loadUser" WRITES_TO state "loadUser"
//
// The signal bindings declared in the body are collected first so a `.set`/
// `.update` call is only treated as a state setter when its receiver is a known
// signal (avoiding false positives on Set.add-style methods).
func (x *extractor) angularStateSetterEmission(body ts.Node, className string) []types.EntityRecord {
	if body == nil {
		return nil
	}
	signals, subjects := x.angularSignalBindings(body)

	var ents []types.EntityRecord
	seen := map[string]bool{}

	emit := func(name, stateName, sig string, props map[string]string, node ts.Node) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		start, end := lines(node)
		props["kind"] = "SCOPE.Operation"
		props["subtype"] = "state_setter"
		props["component"] = className
		props["framework"] = "angular"
		props["state"] = stateName
		e := types.EntityRecord{
			Name:             name,
			QualifiedName:    x.qualify(fmt.Sprintf("%s.%s", className, name)),
			Kind:             "SCOPE.Operation",
			SourceFile:       x.filePath,
			StartLine:        start,
			EndLine:          end,
			Language:         x.language,
			Subtype:          "state_setter",
			Signature:        sig,
			Properties:       props,
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
			// WRITES_TO edge from the setter to the state it mutates lives on
			// the setter entity so a setter→state query resolves directly.
			Relationships: []types.RelationshipRecord{{
				ToID: "state:" + stateName,
				Kind: string(types.RelationshipKindWritesTo),
				Properties: map[string]string{
					"setter":    name,
					"state":     stateName,
					"component": className,
					"framework": "angular",
				},
			}},
		}
		e.ID = e.ComputeID()
		ents = append(ents, e)
	}

	for _, call := range findAllNodes(body, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			continue
		}
		propNode := fn.ChildByFieldName("property")
		recv := fn.ChildByFieldName("object")
		if propNode == nil || recv == nil {
			continue
		}
		method := x.nodeText(propNode)
		recvText := x.nodeText(recv)

		// Signal setter: <signal>.set(...) / <signal>.update(...) / .mutate(...).
		if method == "set" || method == "update" || method == "mutate" {
			sigName := angularSignalLeafName(recvText)
			if sigName != "" && signals[sigName] {
				name := sigName + "." + method
				emit(name, sigName,
					fmt.Sprintf("%s.%s(...)", recvText, method),
					map[string]string{"setter_kind": "signal", "method": method}, call)
			}
			continue
		}

		// RxJS subject setter: <subject$>.next(...). A BehaviorSubject/Subject
		// service member pushed via .next() is Angular's classic state-mutation
		// idiom; the receiver must be a known subject binding so .next on other
		// objects (e.g. an iterator) is not miscounted.
		if method == "next" {
			subName := angularSignalLeafName(recvText)
			if subName != "" && subjects[subName] {
				name := subName + ".next"
				emit(name, subName,
					fmt.Sprintf("%s.next(...)", recvText),
					map[string]string{"setter_kind": "rxjs_subject", "method": method}, call)
			}
			continue
		}

		// ngrx dispatch: <store>.dispatch(actionCreator(...)).
		if method == "dispatch" && angularLooksLikeStore(recvText) {
			action := angularDispatchActionName(x, call)
			if action == "" {
				action = "<action>"
			}
			name := "dispatch:" + action
			emit(name, action,
				fmt.Sprintf("%s.dispatch(%s)", recvText, action),
				map[string]string{"setter_kind": "ngrx_dispatch", "action": action}, call)
		}
	}
	return ents
}

// angularSignalBindings collects the names of writable signal bindings and RxJS
// subject bindings declared in a class body. Signals come from
// `x = signal(...)` / `const x = signal<...>(...)` (and the `model` writable
// primitive); subjects from `x = new BehaviorSubject(...)` (and the other RxJS
// Subject ctors). The two name sets gate `.set`/`.update`/`.mutate` (signals)
// and `.next` (subjects) setter detection so those methods are only treated as
// state mutations when their receiver is a known reactive container.
func (x *extractor) angularSignalBindings(body ts.Node) (signals, subjects map[string]bool) {
	signals = map[string]bool{}
	subjects = map[string]bool{}
	for _, decl := range findAllNodes(body, "variable_declarator", "public_field_definition", "field_definition") {
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = decl.ChildByFieldName("property")
		}
		valNode := decl.ChildByFieldName("value")
		if nameNode == nil || valNode == nil {
			continue
		}
		name := x.nodeText(nameNode)
		if name == "" || strings.ContainsAny(name, "{}[].,") {
			continue
		}
		switch valNode.Type() {
		case "call_expression":
			fn := valNode.ChildByFieldName("function")
			if fn == nil {
				continue
			}
			callee := x.nodeText(fn)
			// Strip a generic type-argument suffix: signal<number>(0).
			if idx := strings.IndexByte(callee, '<'); idx >= 0 {
				callee = callee[:idx]
			}
			if angularSignalFactories[callee] {
				signals[name] = true
			}
		case "new_expression":
			ctorNode := valNode.ChildByFieldName("constructor")
			if ctorNode == nil {
				continue
			}
			ctor := x.nodeText(ctorNode)
			if idx := strings.IndexByte(ctor, '<'); idx >= 0 {
				ctor = ctor[:idx]
			}
			if angularRxjsSubjects[strings.TrimSpace(ctor)] {
				subjects[name] = true
			}
		}
	}
	return signals, subjects
}

// angularSignalFactories are the Angular reactivity factory functions whose
// return value is a writable signal (carries .set/.update setters).
var angularSignalFactories = map[string]bool{
	"signal": true,
	"model":  true,
}

// angularSignalLeafName returns the trailing identifier of a signal receiver
// expression. `this.count` → "count"; `count` → "count". Returns "" when the
// receiver is not a simple identifier / this-field access.
func angularSignalLeafName(recvText string) string {
	recvText = strings.TrimSpace(recvText)
	if recvText == "" {
		return ""
	}
	if idx := strings.LastIndexByte(recvText, '.'); idx >= 0 {
		recvText = recvText[idx+1:]
	}
	if recvText == "" || strings.ContainsAny(recvText, "()[]{} ") {
		return ""
	}
	return recvText
}

// angularDispatchActionName returns the name of the action creator passed to
// store.dispatch(...). For `dispatch(loadUser())` → "loadUser"; for
// `dispatch({ type: '[User] Load' })` → the type string. Returns "" when the
// shape is unrecognised.
func angularDispatchActionName(x *extractor, call ts.Node) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	first := firstMeaningfulArg(args)
	if first == nil {
		return ""
	}
	switch first.Type() {
	case "call_expression":
		fn := first.ChildByFieldName("function")
		if fn != nil {
			leaf := x.nodeText(fn)
			if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
				leaf = leaf[idx+1:]
			}
			return leaf
		}
	case "object", "object_expression":
		// { type: '[Feature] Action' } — extract the type literal.
		for i := 0; i < int(first.ChildCount()); i++ {
			pair := first.Child(i)
			if pair == nil || pair.Type() != "pair" {
				continue
			}
			k := pair.ChildByFieldName("key")
			v := pair.ChildByFieldName("value")
			if k == nil || v == nil {
				continue
			}
			if strings.Trim(x.nodeText(k), `"'`) == "type" {
				return strings.Trim(x.nodeText(v), `"'`+"`")
			}
		}
	case "identifier":
		return x.nodeText(first)
	}
	return ""
}
