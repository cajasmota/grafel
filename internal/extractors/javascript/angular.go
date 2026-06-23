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

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// angularClassDecorators maps a recognised class decorator identifier to the
// SCOPE.Component subtype emitted for the decorated class. The map name is
// historical (#2854 was Angular-only); it now also carries the NestJS
// constructor-DI decorators so the base AST extractor routes those classes
// through handleAngularClass — which emits the provider→consumer INJECTED_INTO
// edges from the constructor parameter list.
//
// Why the NestJS controller/resolver/gateway decorators belong here (#3970):
// @Injectable was already recognised, so NestJS *service* classes went through
// the DI-aware path and kept their INJECTED_INTO edges. @Controller / @Resolver
// / @WebSocketGateway were NOT, so a NestJS controller fell through to the
// generic class path and emitted an edge-LESS SCOPE.Component/class node. That
// node shares an entity ID (Kind+Name+SourceFile) with the NestJS custom
// extractor's edge-bearing controller entity, and the edge-less AST node won the
// same-id assembly dedup (first-writer-wins), dropping the controller's
// INJECTED_INTO edges. Routing @Controller/@Resolver/@WebSocketGateway through
// handleAngularClass makes the surviving AST node ALSO carry the INJECTED_INTO
// edges, so they are reachable on the controller via inspect.di_edges
// regardless of which same-id node wins. The constructor-injection scan
// (angularConstructorInjections) is framework-agnostic, so it captures the
// `constructor(private readonly svc: DevicesService)` providers identically.
var angularClassDecorators = map[string]string{
	"Component":  "angular_component",
	"Directive":  "angular_directive",
	"Injectable": "angular_service",
	"Pipe":       "angular_pipe",
	"NgModule":   "angular_module",
	// NestJS constructor-DI decorators (#3970). Subtypes match the
	// classLikeComponentSubtypes fold-source allowlist (controller/resolver/
	// gateway) so the node folds/dedups cleanly with the custom extractor's
	// same-named entity.
	"Controller":       "controller",
	"Resolver":         "resolver",
	"WebSocketGateway": "gateway",
}

// nestClassDecorators is the subset of angularClassDecorators that belongs to
// NestJS rather than Angular (#3970). It is used only to stamp the correct
// `framework` property/edge label on the emitted entity — the structural
// handling (subtype emission + constructor-DI INJECTED_INTO edges) is shared.
var nestClassDecorators = map[string]bool{
	"Controller":       true,
	"Resolver":         true,
	"WebSocketGateway": true,
}

// frameworkForDecorator returns the framework label for a recognised class
// decorator from the decorator name alone: "nestjs" for the NestJS-only DI
// decorators, else "angular". This is the name-only fallback used when no
// import context is available; the ambiguous @Injectable decorator (shared by
// Angular and NestJS) is disambiguated by frameworkForClass via import origin.
func frameworkForDecorator(decorator string) string {
	if nestClassDecorators[decorator] {
		return "nestjs"
	}
	return "angular"
}

// nestDecoratorModulePrefixes are the dotted import-source prefixes that mark a
// decorator as coming from NestJS (e.g. `@nestjs/common`, `@nestjs/core`,
// `@nestjs/graphql`, `@nestjs/websockets`). importBinding.sourceModule is
// dotted (slashes → dots), so `@nestjs/common` becomes `@nestjs.common`.
var nestDecoratorModulePrefixes = []string{"@nestjs.", "@nestjs/"}

// angularDecoratorModulePrefixes are the dotted import-source prefixes that mark
// a decorator as coming from Angular (`@angular/core`, `@angular/common`, …).
var angularDecoratorModulePrefixes = []string{"@angular.", "@angular/"}

// hasModulePrefix reports whether the dotted import source begins with any of
// the given prefixes (matching both the dotted `@nestjs.` and raw `@nestjs/`
// shapes so it is robust to how the source was canonicalised).
func hasModulePrefix(source string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(source, p) {
			return true
		}
	}
	return false
}

// frameworkForClass returns the framework label for a decorated class,
// disambiguating the @Injectable decorator (shared by Angular's @angular/core
// and NestJS's @nestjs/common) by the import origin of the decorator and the
// file's surrounding framework markers (#4503).
//
// Angular and NestJS both spell a DI provider `@Injectable()` with a
// constructor-injected providers list, so the bare decorator name is
// insufficient: classifying every @Injectable as Angular mis-tags an entire
// NestJS codebase as Angular (the upvate-v3 /di "121 angular" bug). The
// disambiguation is by import-origin/markers, not the bare decorator:
//
//   - The NestJS-only decorators (@Controller / @Resolver / @WebSocketGateway)
//     and the Angular-only decorators (@Component / @Directive / @Pipe /
//     @NgModule) are unambiguous from the name; defer to frameworkForDecorator.
//   - For @Injectable, look at where THIS decorator was imported from: a local
//     name imported from `@nestjs/*` → nestjs; from `@angular/*` → angular.
//   - When the decorator import is unresolved (re-exported through a barrel,
//     aliased, etc.), fall back to file-level markers: any `@nestjs/*` import
//     present → nestjs; any `@angular/*` import → angular.
//   - With no markers at all, keep the historical default (angular) so genuine
//     Angular projects without an explicit core import are unaffected.
func (x *extractor) frameworkForClass(decorator string) string {
	// Unambiguous decorators: name alone decides.
	if decorator != "Injectable" {
		return frameworkForDecorator(decorator)
	}

	// 1) Origin of the @Injectable decorator import itself.
	if b, ok := x.importByLocal[decorator]; ok && b != nil {
		if hasModulePrefix(b.sourceModule, nestDecoratorModulePrefixes) ||
			hasModulePrefix(b.importPath, nestDecoratorModulePrefixes) {
			return "nestjs"
		}
		if hasModulePrefix(b.sourceModule, angularDecoratorModulePrefixes) ||
			hasModulePrefix(b.importPath, angularDecoratorModulePrefixes) {
			return "angular"
		}
	}

	// 2) Fall back to file-level framework markers.
	var sawNest, sawAngular bool
	for i := range x.imports {
		src := x.imports[i].sourceModule
		path := x.imports[i].importPath
		if hasModulePrefix(src, nestDecoratorModulePrefixes) ||
			hasModulePrefix(path, nestDecoratorModulePrefixes) {
			sawNest = true
		}
		if hasModulePrefix(src, angularDecoratorModulePrefixes) ||
			hasModulePrefix(path, angularDecoratorModulePrefixes) {
			sawAngular = true
		}
	}
	switch {
	case sawNest && !sawAngular:
		return "nestjs"
	case sawAngular && !sawNest:
		return "angular"
	case sawNest && sawAngular:
		// Mixed file (rare). Prefer the decorator-origin signal already tried;
		// with neither matching, prefer nestjs only if the file has no Angular
		// class decorators is hard to know here — default to angular to avoid
		// regressing genuine Angular hybrids.
		return "angular"
	}

	// 3) No framework markers: keep the historical default.
	return "angular"
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
func (x *extractor) angularDecoratorFor(class ts.Node) (string, ts.Node) {
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
func (x *extractor) decoratorIdent(dec ts.Node) (string, ts.Node) {
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
func (x *extractor) handleAngularClass(n ts.Node, decorator string, call ts.Node) bool {
	subtype, ok := angularClassDecorators[decorator]
	if !ok {
		return false
	}
	nameNode := n.ChildByFieldName("name")
	className := x.nodeText(nameNode)
	if className == "" {
		return false
	}

	framework := x.frameworkForClass(decorator)
	props := map[string]string{
		"framework":          framework,
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
			// Angular Internals (issue #2874) — `| async` pipe in the inline
			// template subscribes to an Observable in the view; record it as a
			// flag so rxjs_pattern_detection covers the template-side idiom.
			if reAngularAsyncPipe.MatchString(tmpl) {
				props["rxjs_async_pipe"] = "true"
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
					"framework": framework,
				},
			})
		}
	}

	// Data Flow group (issue #2855) — collect data-flow signal entities for
	// the component before emitting it so the class can CONTAIN them:
	//   prop_extraction    : @Input()/@Output() decorated fields
	//   data_fetching      : HttpClient.get/post/… call sites
	//   branch_conditions  : *ngIf / @if / [ngSwitch] template branches
	// state_management is emitted as edges (CALLS to ngrx select/dispatch) so
	// it is attached to the component entity's relationship slice below.
	var dfEnts []types.EntityRecord
	if body := n.ChildByFieldName("body"); body != nil {
		ioEnts := x.angularInputOutputProps(body, className)
		fetchEnts, fetchRels := x.angularDataFetching(body, className)
		stateEnts, stateRels := x.angularStateManagement(body, className)
		dfEnts = append(dfEnts, ioEnts...)
		dfEnts = append(dfEnts, fetchEnts...)
		dfEnts = append(dfEnts, stateEnts...)
		rels = append(rels, fetchRels...)
		rels = append(rels, stateRels...)

		// Navigation group (issue #2856) — imperative Router navigation
		// (this.router.navigate([...]) / navigateByUrl) and RouterModule
		// route-table declarations both emit NAVIGATES_TO edges.
		rels = append(rels, x.angularNavigationRels(body, className)...)
		rels = append(rels, x.angularRouteTableRels(body, className)...)

		// Lifecycle group (issue #2856) — state-setter emission: signal
		// .set/.update and ngrx dispatch each emit a state_setter operation
		// plus a WRITES_TO edge to the state it mutates.
		setterEnts := x.angularStateSetterEmission(body, className)
		dfEnts = append(dfEnts, setterEnts...)

		// Angular Internals (issue #2874) — RxJS idioms inside the class body:
		// .pipe(operators) pipelines, .subscribe(...) subscriptions and
		// new Subject/BehaviorSubject construction (rxjs_pattern_detection).
		rxjsEnts := x.angularRxjsPatterns(body, className)
		dfEnts = append(dfEnts, rxjsEnts...)

		// Cross-framework TanStack Query (issue #2910) — the Angular adapter
		// (@tanstack/angular-query-experimental) injectQuery/injectMutation/
		// injectInfiniteQuery calls inside the class body emit a decorated
		// tanstack_query SCOPE.Operation + CONTAINS edge. No-op unless the
		// adapter is imported.
		tsqEnts, tsqRels := x.angularTanstackQuery(body, className)
		dfEnts = append(dfEnts, tsqEnts...)
		rels = append(rels, tsqRels...)
	}

	// Angular Internals (issue #2874) — class-based route guards / HTTP
	// interceptors (guard_interceptor_recognition). A class that implements a
	// guard interface (CanActivate/…/Resolve) or HttpInterceptor records its
	// role on the entity and emits an IMPLEMENTS edge to the interface.
	if role, _, guardRels := x.angularGuardClassRels(n, className); role != "" {
		props["angular_role"] = role
		rels = append(rels, guardRels...)
	}

	// Issue #4322 — generic class heritage. Beyond the narrow Angular guard /
	// interceptor interfaces handled above, an Angular-decorated class can still
	// `extends BaseComponent` / `implements OnInit, OnDestroy` (Angular
	// lifecycle interfaces are a classic orphan source). Emit the generic
	// EXTENDS / IMPLEMENTS edges, deduped against any guard IMPLEMENTS already
	// added, so these classes are connected to their base/interface.
	rels = appendHeritageDeduped(rels, x.classHeritageRels(n, className))
	if call != nil {
		meta := x.angularDecoratorMeta(call)
		if tmpl := meta["template"]; tmpl != "" {
			brEnts := x.angularBranchConditions(tmpl, className, n)
			dfEnts = append(dfEnts, brEnts...)
			// Navigation group (issue #2856) — routerLink directives in the
			// inline template emit NAVIGATES_TO edges.
			rels = append(rels, x.angularRouterLinkRels(tmpl, className, n)...)
		}
	}

	sig := fmt.Sprintf("@%s class %s", decorator, className)

	// CONTAINS edges from the component to each data-flow entity (props,
	// data-fetch call sites, branch conditions).
	for i := range dfEnts {
		rels = append(rels, types.RelationshipRecord{
			ToID: dfEnts[i].ID,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": className,
				"framework": "angular",
			},
		})
	}

	// Emit the Angular class entity, then attribute its body operations via
	// CONTAINS (mirrors handleClassDeclaration).
	classIdx := len(x.entities)
	x.emitWithProps(className, "SCOPE.Component", n, subtype, sig, props, rels)
	x.entities = append(x.entities, dfEnts...)

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
func (x *extractor) angularDecoratorMeta(call ts.Node) map[string]string {
	out := map[string]string{}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return out
	}
	var obj ts.Node
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
func (x *extractor) angularConstructorInjections(body ts.Node) []string {
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

// angularTitle uppercases the first rune of s (a small replacement for the
// deprecated strings.Title for our ASCII "input"/"output" inputs).
func angularTitle(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// angularInputOutputDecorators maps an Angular member decorator to the
// component_prop direction it represents. @Input() is an inbound prop (parent →
// child); @Output() is an outbound event emitter (child → parent). Both are
// part of a component's prop surface (Data Flow / prop_extraction).
var angularInputOutputDecorators = map[string]string{
	"Input":  "input",
	"Output": "output",
}

// angularInputOutputProps scans a class body for @Input()/@Output() decorated
// fields and returns one SCOPE.Operation subtype="component_prop" per field.
// The Angular grammar shape is a `public_field_definition` whose first child is
// a `decorator` (see the AST dump in this package's tests).
func (x *extractor) angularInputOutputProps(body ts.Node, className string) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}
	for i := 0; i < int(body.ChildCount()); i++ {
		field := body.Child(i)
		if field == nil {
			continue
		}
		if field.Type() != "public_field_definition" && field.Type() != "field_definition" {
			continue
		}
		dir := ""
		for j := 0; j < int(field.ChildCount()); j++ {
			c := field.Child(j)
			if c == nil || c.Type() != "decorator" {
				continue
			}
			name, _ := x.decoratorIdent(c)
			if d, ok := angularInputOutputDecorators[name]; ok {
				dir = d
				break
			}
		}
		if dir == "" {
			continue
		}
		nameNode := field.ChildByFieldName("name")
		if nameNode == nil {
			nameNode = field.ChildByFieldName("property")
		}
		propName := x.nodeText(nameNode)
		if propName == "" || seen[propName] {
			continue
		}
		seen[propName] = true
		start, end := lines(field)
		e := types.EntityRecord{
			Name:          propName,
			QualifiedName: x.qualify(fmt.Sprintf("%s.%s", className, propName)),
			Kind:          "SCOPE.Operation",
			SourceFile:    x.filePath,
			StartLine:     start,
			EndLine:       end,
			Language:      x.language,
			Subtype:       "component_prop",
			Signature:     fmt.Sprintf("@%s %s", angularTitle(dir), propName),
			Properties: map[string]string{
				"kind":           "SCOPE.Operation",
				"subtype":        "component_prop",
				"component":      className,
				"prop":           propName,
				"prop_direction": dir,
				"framework":      "angular",
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
		}
		e.ID = e.ComputeID()
		out = append(out, e)
	}
	return out
}

// reAngularHTTPMethod matches the HttpClient verb methods that constitute a
// data-fetch call site.
var angularHTTPMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true,
	"delete": true, "head": true, "options": true, "request": true,
}

// angularDataFetching scans method bodies for `this.<http>.get(...)`-style
// HttpClient call sites (Data Flow / data_fetching). It returns one
// SCOPE.Operation subtype="data_fetch" per distinct (method, url) site plus a
// CALLS edge from the component to the HTTP verb. The receiver name is not
// fixed to "http" — any member_expression `this.X.<verb>(...)` where <verb> is
// an HttpClient method is treated as a fetch site (the field's declared type is
// HttpClient by Angular convention).
func (x *extractor) angularDataFetching(body ts.Node, className string) ([]types.EntityRecord, []types.RelationshipRecord) {
	var ents []types.EntityRecord
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
		verb := x.nodeText(propNode)
		if !angularHTTPMethods[verb] {
			continue
		}
		// Receiver must be `this.<field>` (or `<field>`) — confirm the object is
		// a member_expression rooted at `this` or a plain identifier, to avoid
		// matching unrelated `.get`/`.delete` calls (Map.get, etc.) we cannot
		// type-check. We require the receiver to look like an injected service.
		recv := fn.ChildByFieldName("object")
		if recv == nil || (recv.Type() != "member_expression" && recv.Type() != "identifier") {
			continue
		}
		recvText := x.nodeText(recv)
		// URL argument (first string literal), best-effort.
		url := angularFirstStringArg(x, call)
		key := verb + "|" + url
		if seen[key] {
			continue
		}
		seen[key] = true
		start, end := lines(call)
		name := fmt.Sprintf("http.%s", verb)
		e := types.EntityRecord{
			Name:          name,
			QualifiedName: x.qualify(fmt.Sprintf("%s.%s", className, name)),
			Kind:          "SCOPE.Operation",
			SourceFile:    x.filePath,
			StartLine:     start,
			EndLine:       end,
			Language:      x.language,
			Subtype:       "data_fetch",
			Signature:     fmt.Sprintf("%s.%s(%s)", recvText, verb, url),
			Properties: map[string]string{
				"kind":        "SCOPE.Operation",
				"subtype":     "data_fetch",
				"component":   className,
				"http_method": verb,
				"url":         url,
				"framework":   "angular",
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
		}
		e.ID = e.ComputeID()
		ents = append(ents, e)
		rels = append(rels, types.RelationshipRecord{
			ToID: e.ID,
			Kind: "CALLS",
			Properties: map[string]string{
				"caller":      className,
				"http_method": verb,
				"framework":   "angular",
			},
		})
	}
	return ents, rels
}

// angularFirstStringArg returns the first string-literal argument of a call
// expression, stripped of quotes, or "" when the first argument is not a
// string literal.
func angularFirstStringArg(x *extractor, call ts.Node) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c != nil && c.Type() == "string" {
			return stringLiteralValue(x.nodeText(c))
		}
	}
	return ""
}

// angularStateMethods are the ngrx Store methods that constitute state access.
var angularStateMethods = map[string]bool{
	"select": true, "dispatch": true, "pipe": true, "selectSignal": true,
}

// angularStateContainerFactories are the call-expression factories whose return
// value is a piece of reactive state in modern Angular. signal/computed are the
// Angular signals primitives (the default state idiom since v16); signalStore/
// withState are the ngrx **signal** store builders. Each declaration becomes a
// state_store SCOPE.Operation so the Data Flow/state_management cell fires on
// the dominant modern idiom, not only the (rarely-used) ngrx Redux Store.
var angularStateContainerFactories = map[string]string{
	"signal":      "signal",
	"computed":    "computed",
	"signalStore": "ngrx_signal_store",
	"withState":   "ngrx_signal_store",
}

// angularStateManagement scans an Angular class body for state containers and
// store interactions, returning state_store SCOPE.Operation entities plus CALLS
// edges to ngrx Store methods (Data Flow / state_management). Three families are
// recognised — mirroring how React/Vue/Svelte model state_management as a
// declared state container plus its mutation points:
//
//	signals : count = signal(0); total = computed(...) ; signalStore/withState
//	          → state_store entity (the modern Angular default, no ngrx needed)
//	rxjs    : private user$ = new BehaviorSubject<User|null>(null)
//	          → state_store entity (BehaviorSubject is Angular's classic
//	            service-held state stream — coordinated with the #2893 rxjs
//	            subject recognition, which records the same construction as an
//	            rxjs_subject operation for the Internals cell)
//	ngrx    : this.store.select(...) / this.store.dispatch(...)
//	          → CALLS edge to the Store method (unchanged; kept so the Redux
//	            store path does not regress)
func (x *extractor) angularStateManagement(body ts.Node, className string) ([]types.EntityRecord, []types.RelationshipRecord) {
	if body == nil {
		return nil, nil
	}
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	seenStore := map[string]bool{}
	seenState := map[string]bool{}

	emitContainer := func(name, lib, primitive, sig string, node ts.Node) {
		if name == "" || seenState[name] {
			return
		}
		seenState[name] = true
		start, end := lines(node)
		ents = append(ents, x.newAngularOp(className, "state_store", name, sig, start, end,
			map[string]string{"state_lib": lib, "primitive": primitive}, nil))
	}

	// State-container declarations: `<name> = <factory>(...)` (class field) or
	// `const <name> = <factory>(...)` (local) for signal/computed/signalStore,
	// and `<name> = new BehaviorSubject(...)` for RxJS service state.
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
			if idx := strings.IndexByte(callee, '<'); idx >= 0 {
				callee = callee[:idx]
			}
			// signalStore(...) returns a class — accept it when the callee is
			// the bare factory or a `withState`/`withMethods` builder chain.
			if prim, ok := angularStateContainerFactories[callee]; ok {
				emitContainer(name, "angular-signals", prim,
					fmt.Sprintf("%s = %s(...)", name, callee), decl)
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
			ctor = strings.TrimSpace(ctor)
			if angularRxjsSubjects[ctor] {
				emitContainer(name, "rxjs-subject", ctor,
					fmt.Sprintf("%s = new %s(...)", name, ctor), decl)
			}
		}
	}

	// ngrx Redux Store interactions (this.store.select/dispatch/...): CALLS
	// edge per store method (unchanged behaviour, no regression).
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
		if !angularStateMethods[method] {
			continue
		}
		// Require the receiver to mention a store-like identifier so `pipe`
		// (which is also an RxJS operator) is only counted when chained off a
		// store. We accept `store`/`Store`-suffixed receivers.
		recv := fn.ChildByFieldName("object")
		recvText := x.nodeText(recv)
		if !angularLooksLikeStore(recvText) {
			continue
		}
		if seenStore[method] {
			continue
		}
		seenStore[method] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: "Store." + method,
			Kind: "CALLS",
			Properties: map[string]string{
				"caller":       className,
				"store_method": method,
				"framework":    "angular",
				"state_lib":    "ngrx",
			},
		})
	}
	return ents, rels
}

// angularLooksLikeStore reports whether a receiver expression text references a
// ngrx store (a `store` identifier or a `.store` member access).
func angularLooksLikeStore(recvText string) bool {
	lower := strings.ToLower(recvText)
	return strings.HasSuffix(lower, "store") || strings.HasSuffix(lower, ".store")
}

// reAngularBranch matches Angular template control-flow branches: the
// structural directive `*ngIf`/`*ngFor`/`*ngSwitchCase`, the new (v17) control
// flow blocks `@if`/`@for`/`@switch`, and `[ngSwitch]`. Each is a
// branch_conditions signal.
var reAngularBranch = regexp.MustCompile(`(\*ngIf|\*ngFor|\*ngSwitchCase|\*ngSwitchDefault|\[ngSwitch\]|@if\b|@else\b|@for\b|@switch\b)`)

// angularBranchConditions scans an inline template for conditional-rendering
// constructs and returns one SCOPE.Operation subtype="branch_condition" per
// distinct directive kind (Data Flow / branch_conditions). The component's
// source node provides the location anchor (inline templates do not carry
// independent line numbers in this extractor).
func (x *extractor) angularBranchConditions(template, className string, anchor ts.Node) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}
	start, end := lines(anchor)
	for _, m := range reAngularBranch.FindAllString(template, -1) {
		kind := strings.TrimSpace(m)
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		safe := strings.NewReplacer("*", "", "[", "", "]", "", "@", "at_").Replace(kind)
		e := types.EntityRecord{
			Name:          safe,
			QualifiedName: x.qualify(fmt.Sprintf("%s.%s", className, safe)),
			Kind:          "SCOPE.Operation",
			SourceFile:    x.filePath,
			StartLine:     start,
			EndLine:       end,
			Language:      x.language,
			Subtype:       "branch_condition",
			Signature:     fmt.Sprintf("template %s", kind),
			Properties: map[string]string{
				"kind":        "SCOPE.Operation",
				"subtype":     "branch_condition",
				"component":   className,
				"branch_kind": kind,
				"framework":   "angular",
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
		}
		e.ID = e.ComputeID()
		out = append(out, e)
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
