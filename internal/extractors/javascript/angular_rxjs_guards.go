// angular_rxjs_guards.go — Angular Internals (B) implementation cells
// (issue #2874): rxjs_pattern_detection + guard_interceptor_recognition.
//
// Both cells decorate EXISTING entity Kinds (SCOPE.Operation / SCOPE.Component)
// and reuse already-registered edge Kinds (SUBSCRIBES_TO, TRANSFORMS,
// IMPLEMENTS, CONTAINS) — NO new Kind is introduced, so internal/types stays
// exhaustive (the #2839 lesson).
//
//   - rxjs_pattern_detection — RxJS is Angular's reactive substrate. Three
//     idioms are recognised inside an Angular class body and surfaced as
//     SCOPE.Operation entities:
//     1. Observable pipelines: `source$.pipe(map(), switchMap(), filter())`
//     → subtype "rxjs_pipeline" + one TRANSFORMS edge per operator so the
//     operator chain is queryable.
//     2. Subscriptions: `obs$.subscribe(...)` / `obs$.subscribe({next})`
//     → subtype "rxjs_subscription" + a SUBSCRIBES_TO edge to the source.
//     3. Subjects: `new Subject()` / `new BehaviorSubject(v)` /
//     `new ReplaySubject()` / `new AsyncSubject()` → subtype "rxjs_subject"
//     (the multicast/state-stream primitives).
//     Inline-template `| async` pipe usage is recorded as a property flag on the
//     component (it consumes an Observable in the view).
//
//   - guard_interceptor_recognition — Angular route guards and HTTP
//     interceptors. Both class-based and functional forms are recognised:
//     1. Class guards: a class whose `implements` clause names a guard
//     interface (CanActivate / CanActivateChild / CanDeactivate / CanLoad
//     / CanMatch / Resolve) → SCOPE.Component subtype "angular_guard" with
//     an IMPLEMENTS edge to the interface.
//     2. Class interceptors: `implements HttpInterceptor`
//     → subtype "angular_interceptor" + IMPLEMENTS edge.
//     3. Functional guards: `export const x: CanActivateFn = (...) => ...`
//     (and the *Fn siblings) → subtype "angular_guard".
//     4. Functional interceptors: `export const x: HttpInterceptorFn = ...`
//     → subtype "angular_interceptor".
//     Class forms are detected from the decorated-class node in
//     handleAngularClass; functional forms from a program-level pass over
//     exported `const` arrow declarations (they are not class-decorated).
package javascript

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// reAngularAsyncPipe matches the RxJS `| async` pipe in an inline Angular
// template (`{{ data$ | async }}`, `*ngIf="data$ | async as d"`). It is the
// template-side RxJS subscription idiom (rxjs_pattern_detection).
var reAngularAsyncPipe = regexp.MustCompile(`\|\s*async\b`)

// angularRxjsOperators is the set of common RxJS pipeable operators we
// recognise as the transform stages of a `.pipe(...)` chain. The list covers
// the high-frequency operators; an unrecognised call inside pipe() is still
// counted as an operator stage (any call_expression in the pipe argument list
// is a transform), this set only drives the "known operator" classification.
var angularRxjsOperators = map[string]bool{
	"map": true, "switchMap": true, "mergeMap": true, "concatMap": true,
	"exhaustMap": true, "filter": true, "tap": true, "take": true,
	"takeUntil": true, "takeWhile": true, "debounceTime": true,
	"distinctUntilChanged": true, "catchError": true, "retry": true,
	"startWith": true, "scan": true, "reduce": true, "withLatestFrom": true,
	"combineLatestWith": true, "shareReplay": true, "share": true,
	"finalize": true, "delay": true, "throttleTime": true, "first": true,
	"last": true, "skip": true, "pluck": true, "mapTo": true,
	"switchMapTo": true, "mergeAll": true, "concatAll": true, "pairwise": true,
}

// angularRxjsSubjects is the set of RxJS Subject constructors. A `new <Subject>`
// expression where the constructor is one of these emits a "rxjs_subject"
// operation (Angular's in-component multicast / state-stream primitive).
var angularRxjsSubjects = map[string]bool{
	"Subject":         true,
	"BehaviorSubject": true,
	"ReplaySubject":   true,
	"AsyncSubject":    true,
}

// angularRxjsPatterns scans an Angular class body for RxJS idioms and returns
// SCOPE.Operation entities (with embedded edges) for pipelines, subscriptions
// and subjects (issue #2874 — rxjs_pattern_detection). Each entity carries the
// component name + framework so it is attributable to the class via the
// CONTAINS edges handleAngularClass appends.
func (x *extractor) angularRxjsPatterns(body ts.Node, className string) []types.EntityRecord {
	if body == nil {
		return nil
	}
	var out []types.EntityRecord
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
		recv := fn.ChildByFieldName("object")
		recvText := x.nodeText(recv)

		switch method {
		case "pipe":
			ops := x.angularPipeOperators(call)
			if len(ops) == 0 {
				continue
			}
			name := "pipe(" + strings.Join(ops, ",") + ")"
			key := "pipeline|" + recvText + "|" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			start, end := lines(call)
			rels := make([]types.RelationshipRecord, 0, len(ops))
			for _, op := range ops {
				known := "false"
				if angularRxjsOperators[op] {
					known = "true"
				}
				rels = append(rels, types.RelationshipRecord{
					ToID: "rxjs:operator:" + op,
					Kind: string(types.RelationshipKindTransforms),
					Properties: map[string]string{
						"operator":       op,
						"known_operator": known,
						"component":      className,
						"framework":      "angular",
						"rxjs":           "operator",
					},
				})
			}
			e := x.newAngularOp(className, "rxjs_pipeline",
				fmt.Sprintf("rxjs.pipe[%s]", strings.Join(ops, "|")),
				fmt.Sprintf("%s.pipe(%s)", recvText, strings.Join(ops, ", ")),
				start, end, map[string]string{
					"rxjs":      "pipeline",
					"source":    recvText,
					"operators": strings.Join(ops, ","),
				}, rels)
			out = append(out, e)

		case "subscribe":
			source := angularObservableSource(recvText)
			key := "subscribe|" + recvText
			if seen[key] {
				continue
			}
			seen[key] = true
			start, end := lines(call)
			rels := []types.RelationshipRecord{{
				ToID: "rxjs:observable:" + source,
				Kind: string(types.RelationshipKindSubscribesTo),
				Properties: map[string]string{
					"source":    source,
					"component": className,
					"framework": "angular",
					"rxjs":      "subscription",
				},
			}}
			e := x.newAngularOp(className, "rxjs_subscription",
				fmt.Sprintf("rxjs.subscribe(%s)", source),
				fmt.Sprintf("%s.subscribe(...)", recvText),
				start, end, map[string]string{
					"rxjs":   "subscription",
					"source": source,
				}, rels)
			out = append(out, e)
		}
	}

	// Subject construction: `new Subject()` / `new BehaviorSubject(v)`.
	for _, ne := range findAllNodes(body, "new_expression") {
		ctorNode := ne.ChildByFieldName("constructor")
		if ctorNode == nil {
			continue
		}
		ctor := x.nodeText(ctorNode)
		if idx := strings.IndexByte(ctor, '<'); idx >= 0 {
			ctor = ctor[:idx]
		}
		ctor = strings.TrimSpace(ctor)
		if !angularRxjsSubjects[ctor] {
			continue
		}
		key := "subject|" + ctor + "|" + fmt.Sprint(ne.StartByte())
		if seen[key] {
			continue
		}
		seen[key] = true
		start, end := lines(ne)
		e := x.newAngularOp(className, "rxjs_subject",
			fmt.Sprintf("rxjs.%s", ctor),
			fmt.Sprintf("new %s(...)", ctor),
			start, end, map[string]string{
				"rxjs":         "subject",
				"subject_kind": ctor,
			}, nil)
		out = append(out, e)
	}

	return out
}

// angularPipeOperators returns the operator names in a `.pipe(op1(), op2(), …)`
// call's argument list. Every call_expression argument is treated as an
// operator stage; its callee leaf name is the operator. Bare identifiers (a
// pre-built operator reference passed without a call) are also captured.
func (x *extractor) angularPipeOperators(call ts.Node) []string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var ops []string
	for i := 0; i < int(args.ChildCount()); i++ {
		arg := args.Child(i)
		if arg == nil {
			continue
		}
		switch arg.Type() {
		case "call_expression":
			fn := arg.ChildByFieldName("function")
			if fn == nil {
				continue
			}
			leaf := x.nodeText(fn)
			if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
				leaf = leaf[idx+1:]
			}
			if idx := strings.IndexByte(leaf, '<'); idx >= 0 {
				leaf = leaf[:idx]
			}
			leaf = strings.TrimSpace(leaf)
			if leaf != "" {
				ops = append(ops, leaf)
			}
		case "identifier":
			ops = append(ops, x.nodeText(arg))
		}
	}
	return ops
}

// angularObservableSource normalises a subscribe-receiver expression to a stable
// source label. `this.users$` → "users$"; `this.service.data$.pipe(...)` is the
// receiver of a chained subscribe so we keep the trailing member. Falls back to
// the raw text (trimmed) when it is a bare identifier.
func angularObservableSource(recvText string) string {
	recvText = strings.TrimSpace(recvText)
	if recvText == "" {
		return "<observable>"
	}
	// A chained `.pipe(...)` receiver ends in a call — strip a trailing paren
	// group so `a$.pipe(...)` collapses to the head `a$`.
	if idx := strings.IndexByte(recvText, '('); idx >= 0 {
		recvText = recvText[:idx]
		recvText = strings.TrimSuffix(strings.TrimSpace(recvText), ".pipe")
	}
	if idx := strings.LastIndexByte(recvText, '.'); idx >= 0 {
		recvText = recvText[idx+1:]
	}
	recvText = strings.TrimSpace(recvText)
	if recvText == "" {
		return "<observable>"
	}
	return recvText
}

// newAngularOp builds a SCOPE.Operation entity for an Angular idiom (RxJS /
// guard helper), filling the common Properties (kind/subtype/component/
// framework) and attaching any caller-supplied relationships.
func (x *extractor) newAngularOp(className, subtype, name, sig string, start, end int, props map[string]string, rels []types.RelationshipRecord) types.EntityRecord {
	if props == nil {
		props = map[string]string{}
	}
	props["kind"] = "SCOPE.Operation"
	props["subtype"] = subtype
	props["component"] = className
	props["framework"] = "angular"
	e := types.EntityRecord{
		Name:             name,
		QualifiedName:    x.qualify(fmt.Sprintf("%s.%s", className, name)),
		Kind:             "SCOPE.Operation",
		SourceFile:       x.filePath,
		StartLine:        start,
		EndLine:          end,
		Language:         x.language,
		Subtype:          subtype,
		Signature:        sig,
		Properties:       props,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Relationships:    rels,
	}
	e.ID = e.ComputeID()
	return e
}

// angularGuardInterfaces maps a route-guard interface name to a fixed label.
// A class whose `implements` clause names one of these is an Angular route
// guard (guard_interceptor_recognition).
var angularGuardInterfaces = map[string]bool{
	"CanActivate":      true,
	"CanActivateChild": true,
	"CanDeactivate":    true,
	"CanLoad":          true,
	"CanMatch":         true,
	"Resolve":          true,
}

// angularGuardFnTypes maps a functional-guard / functional-interceptor type
// annotation to the subtype it confers. Angular's functional guards are typed
// `const x: CanActivateFn = (...) => ...`; functional interceptors are typed
// `HttpInterceptorFn`.
var angularGuardFnTypes = map[string]string{
	"CanActivateFn":      "angular_guard",
	"CanActivateChildFn": "angular_guard",
	"CanDeactivateFn":    "angular_guard",
	"CanLoadFn":          "angular_guard",
	"CanMatchFn":         "angular_guard",
	"ResolveFn":          "angular_guard",
	"HttpInterceptorFn":  "angular_interceptor",
}

// angularClassGuardSubtype inspects a class_declaration's heritage clause and
// returns ("angular_guard"|"angular_interceptor", interfaceName) when the class
// implements an Angular guard or HttpInterceptor interface, else ("", "").
func (x *extractor) angularClassGuardSubtype(class ts.Node) (string, string) {
	if class == nil {
		return "", ""
	}
	for i := 0; i < int(class.ChildCount()); i++ {
		c := class.Child(i)
		if c == nil || c.Type() != "class_heritage" {
			continue
		}
		for j := 0; j < int(c.ChildCount()); j++ {
			impl := c.Child(j)
			if impl == nil || impl.Type() != "implements_clause" {
				continue
			}
			for k := 0; k < int(impl.ChildCount()); k++ {
				idn := impl.Child(k)
				if idn == nil {
					continue
				}
				if idn.Type() != "type_identifier" && idn.Type() != "generic_type" && idn.Type() != "identifier" {
					continue
				}
				name := angularLeafTypeName(x.nodeText(idn))
				if name == "" {
					name = strings.TrimSpace(x.nodeText(idn))
				}
				if angularGuardInterfaces[name] {
					return "angular_guard", name
				}
				if name == "HttpInterceptor" {
					return "angular_interceptor", name
				}
			}
		}
	}
	return "", ""
}

// angularGuardClassRels returns the normalised role ("guard"|"interceptor"),
// the implemented interface, and the IMPLEMENTS edge for an Angular class that
// is also a guard / interceptor. It is folded into the component entity emitted
// by handleAngularClass (the class is already an @Injectable, so it surfaces as
// angular_service; the guard role is recorded as an extra property + an
// IMPLEMENTS edge to the guard interface). The role vocabulary matches the
// functional form (angularFunctionalGuards) so guard/interceptor queries are
// uniform across class and functional shapes.
func (x *extractor) angularGuardClassRels(class ts.Node, className string) (role, iface string, rels []types.RelationshipRecord) {
	subtype, iface := x.angularClassGuardSubtype(class)
	if subtype == "" {
		return "", "", nil
	}
	role = "guard"
	if subtype == "angular_interceptor" {
		role = "interceptor"
	}
	rels = []types.RelationshipRecord{{
		ToID: iface,
		Kind: string(types.RelationshipKindImplements),
		Properties: map[string]string{
			"implementer":  className,
			"interface":    iface,
			"angular_role": role,
			"framework":    "angular",
		},
	}}
	return role, iface, rels
}

// angularFunctionalGuards scans the program root for exported functional guards
// / interceptors — `const name: CanActivateFn = (...) => ...` (and the *Fn
// siblings). It emits one SCOPE.Component entity per match (subtype
// angular_guard / angular_interceptor) so functional guards are first-class
// alongside the class form. Called as a program-level pass from Extract.
func (x *extractor) angularFunctionalGuards(root ts.Node) {
	if root == nil {
		return
	}
	for _, decl := range findAllNodes(root, "variable_declarator") {
		typeNode := decl.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		typeName := angularLeafTypeName(x.nodeText(typeNode))
		if typeName == "" {
			typeName = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(x.nodeText(typeNode)), ":"))
		}
		subtype, ok := angularGuardFnTypes[typeName]
		if !ok {
			continue
		}
		// The RHS must be a function (arrow / function expression) to be a
		// functional guard, not a re-typed value.
		val := decl.ChildByFieldName("value")
		if val == nil || (val.Type() != "arrow_function" && val.Type() != "function" && val.Type() != "function_expression") {
			continue
		}
		nameNode := decl.ChildByFieldName("name")
		name := x.nodeText(nameNode)
		if name == "" {
			continue
		}
		start, end := lines(decl)
		role := "guard"
		if subtype == "angular_interceptor" {
			role = "interceptor"
		}
		e := types.EntityRecord{
			Name:          name,
			QualifiedName: x.qualify(name),
			Kind:          "SCOPE.Component",
			SourceFile:    x.filePath,
			StartLine:     start,
			EndLine:       end,
			Language:      x.language,
			Subtype:       subtype,
			Signature:     fmt.Sprintf("const %s: %s", name, typeName),
			Properties: map[string]string{
				"kind":         "SCOPE.Component",
				"subtype":      subtype,
				"framework":    "angular",
				"angular_role": role,
				"guard_type":   typeName,
				"functional":   "true",
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
		}
		e.ID = e.ComputeID()
		x.entities = append(x.entities, e)
	}
}
