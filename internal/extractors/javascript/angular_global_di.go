// angular_global_di.go — Angular global dependency-injection wiring (#4378).
//
// Angular registers cross-cutting providers app-wide through DI tokens in a
// module/bootstrap providers array, the same {provide, useClass/useExisting/
// useFactory, multi} provider-object shape NestJS uses (#4329). Previously the
// JS/TS extractor recognised the @NgModule class and its constructor injections
// but never read the `providers` array, so the bound classes — HTTP
// interceptors, APP_INITIALIZER factories, the global ErrorHandler, custom
// service tokens — had no edge from a real source entity and looked orphan /
// dead, and the app-wide scope was invisible. Standalone bootstrap
// (`bootstrapApplication(App, { providers: [...] })`) produced nothing at all.
//
// This pass generalises the NestJS global-DI fix (#4329 / #4380 convention) to
// Angular. It emits, reusing the existing USES Kind (NO new Kind):
//
//   - NgModule providers: for each entry in @NgModule({ providers: [...] }) the
//     declaring module emits a module → bound-class USES edge marked
//     global=true, di_token (HTTP_INTERCEPTORS / APP_INITIALIZER / ErrorHandler
//     / NG_VALIDATORS / a custom InjectionToken / the class itself for a bare
//     provider), di_role (interceptor / initializer / error_handler / validator
//     / service), and multi=true for multi-providers.
//
//   - Standalone bootstrap: `bootstrapApplication(App, { providers: [...] })`
//     binds the same provider shapes app-wide with no owning module, so the
//     edges hang off a synthetic `app` application entity (mirroring the NestJS
//     bootstrap `app` owner). `provideHttpClient(withInterceptors([fn]))`
//     functional interceptors and bare/object providers both yield app → target
//     USES edges marked global=true.
//
// Every edge target is a bare class / function identifier; it resolves to the
// real declaring entity through resolve.BuildIndex's symbol table, connecting
// the previously-orphan class. The pass is a program-level pass invoked from
// Extract after the AST walk, so the module/class entities it attaches to
// already exist.
//
// Route guards / resolvers ({ path, canActivate: [Guard], resolve: { x: Res } })
// are intentionally deferred to a tight follow-up (epic #4334) because route
// extraction lives on a separate path; see angular_global_di_test.go.
package javascript

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// angularAppEntityName is the synthetic owner name for app-level global wiring
// emitted from a standalone bootstrapApplication(...) call. Matches the NestJS
// bootstrap convention (#4329) so the two frameworks share the `app` owner.
const angularAppEntityName = "app"

// angularDITokenRole maps a well-known Angular global DI token to the di_role of
// the class/function it binds app-wide. Tokens not in this map are still emitted
// (di_role="service") so custom InjectionToken bindings are connected too.
var angularDITokenRole = map[string]string{
	"HTTP_INTERCEPTORS":       "interceptor",
	"APP_INITIALIZER":         "initializer",
	"ENVIRONMENT_INITIALIZER": "initializer",
	"ErrorHandler":            "error_handler",
	"NG_VALIDATORS":           "validator",
	"NG_ASYNC_VALIDATORS":     "validator",
	"NG_VALUE_ACCESSOR":       "value_accessor",
	"HTTP_INTERCEPTOR_FNS":    "interceptor",
	"ROUTES":                  "routes",
}

// reAngularProvideObject matches an object-literal provider entry
// {provide: TOKEN, useClass|useValue|useFactory|useExisting: Impl}. Group 1 is
// the raw token expression, group 2 the binding keyword, group 3 the impl
// expression (best-effort up to the next comma/brace). Mirrors the NestJS
// reNestProvideObject shape — Angular's provider object is identical.
var reAngularProvideObject = regexp.MustCompile(
	`provide\s*:\s*([^,]+?)\s*,\s*(useClass|useValue|useFactory|useExisting)\s*:\s*([^,}]+)`,
)

// reAngularMulti detects a `multi: true` flag anywhere in a provider object body.
var reAngularMulti = regexp.MustCompile(`\bmulti\s*:\s*true\b`)

// reAngularWithInterceptors matches `withInterceptors([a, b, ...])` — the
// standalone functional-interceptor registration passed to provideHttpClient.
// Group 1 is the array body.
var reAngularWithInterceptors = regexp.MustCompile(`withInterceptors\s*\(\s*\[([^\]]*)\]`)

// reAngularIdent matches a bare identifier (PascalCase class or camelCase
// function reference).
var reAngularIdent = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\b`)

// angularGlobalProviders is the program-level pass that links @NgModule and
// bootstrapApplication global providers into the graph (#4378). It mutates
// x.entities: it appends global USES edges onto the declaring @NgModule entity
// (already emitted by handleAngularClass) and, when a standalone bootstrap is
// present, emits a synthetic `app` entity owning the bootstrap USES edges.
func (x *extractor) angularGlobalProviders(root ts.Node) {
	if root == nil {
		return
	}
	x.angularModuleProviders(root)
	x.angularBootstrapProviders(root)
}

// angularProviderEdge is one resolved global provider binding.
type angularProviderEdge struct {
	target string // bound class / factory / interceptor-fn identifier
	token  string // di_token (the magic token, custom token, or the class itself)
	role   string // di_role (interceptor/initializer/error_handler/service/...)
	multi  bool
}

// angularParseProviders parses a `providers: [...]` array body and returns the
// global provider bindings it declares. It handles the two Angular provider
// shapes uniformly with NestJS:
//
//	bare class      : `AuthService`            → token=AuthService, role=service
//	object provider : `{ provide: TOKEN, useClass|useExisting|useFactory|
//	                     useValue: Impl, multi?: true }`
//
// For an object provider the di_role is derived from the token (interceptor for
// HTTP_INTERCEPTORS, initializer for APP_INITIALIZER, error_handler for
// ErrorHandler, …) and the bound target is the use* implementation. Bare class
// entries bind the class to itself (token=class, role=service).
func angularParseProviders(arrayBody string) []angularProviderEdge {
	var out []angularProviderEdge
	seen := map[string]bool{}
	add := func(e angularProviderEdge) {
		if e.target == "" {
			return
		}
		key := e.token + "|" + e.target + "|" + e.role
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, e)
	}

	// Object-form providers first; record their spans so the bare-class scan can
	// skip identifiers that live inside an object provider.
	type span struct{ lo, hi int }
	var objSpans []span
	for _, loc := range angularProviderObjectSpans(arrayBody) {
		objSpans = append(objSpans, span{loc[0], loc[1]})
		body := arrayBody[loc[0]:loc[1]]
		pm := reAngularProvideObject.FindStringSubmatch(body)
		if pm == nil {
			continue
		}
		token := angularNormaliseToken(pm[1])
		useKind := pm[2]
		impl := angularImplName(pm[3], useKind)
		if token == "" || impl == "" {
			continue
		}
		role, ok := angularDITokenRole[token]
		if !ok {
			role = "service"
		}
		add(angularProviderEdge{
			target: impl,
			token:  token,
			role:   role,
			multi:  reAngularMulti.MatchString(body),
		})
	}

	inObj := func(pos int) bool {
		for _, s := range objSpans {
			if pos >= s.lo && pos < s.hi {
				return true
			}
		}
		return false
	}

	// Bare class providers: a PascalCase identifier at array top level that is
	// not inside an object provider and not a provideX(...) helper call. Each
	// binds the class to itself (token=class, role=service).
	for _, m := range reAngularIdent.FindAllStringSubmatchIndex(arrayBody, -1) {
		id := arrayBody[m[2]:m[3]]
		if id == "" || id[0] < 'A' || id[0] > 'Z' {
			continue // only PascalCase class names; provideX()/withX() are camelCase
		}
		if inObj(m[2]) {
			continue
		}
		// Skip identifiers that are object-literal keys (followed by ':') — none
		// of the top-level bare entries are key:value, but guard anyway.
		rest := strings.TrimLeft(arrayBody[m[3]:], " \t")
		if strings.HasPrefix(rest, ":") {
			continue
		}
		add(angularProviderEdge{target: id, token: id, role: "service"})
	}
	return out
}

// angularProviderObjectSpans returns the [lo,hi) byte spans of each top-level
// `{...}` object literal inside a providers-array body, so object providers can
// be parsed and excluded from the bare-class scan.
func angularProviderObjectSpans(body string) [][2]int {
	var out [][2]int
	depth := 0
	start := -1
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, [2]int{start, i + 1})
				start = -1
			}
		}
	}
	return out
}

// angularModuleProviders scans every @NgModule-decorated class for its
// providers array and appends a module → bound-class USES edge (global=true)
// per provider onto the already-emitted module entity.
func (x *extractor) angularModuleProviders(root ts.Node) {
	for _, dec := range findAllNodes(root, "decorator") {
		name, call := x.decoratorIdent(dec)
		if name != "NgModule" || call == nil {
			continue
		}
		module := x.decoratedClassName(dec)
		if module == "" {
			continue
		}
		body := x.angularDecoratorArrayRaw(call, "providers")
		if body == "" {
			continue
		}
		idx := x.entityIndexByName(module)
		if idx < 0 {
			continue
		}
		for _, e := range angularParseProviders(body) {
			x.entities[idx].Relationships = append(x.entities[idx].Relationships,
				angularGlobalUsesEdge(module, e, "angular", "angular_ngmodule_provider", false))
		}
	}
}

// angularBootstrapProviders scans for a standalone bootstrapApplication(App,
// { providers: [...] }) call. When found it emits a synthetic `app` entity and
// hangs the app → target USES edges (global=true) off it, mirroring the NestJS
// bootstrap `app` owner (#4329). Functional interceptors registered via
// `provideHttpClient(withInterceptors([fn]))` also become app → fn edges.
func (x *extractor) angularBootstrapProviders(root ts.Node) {
	for _, call := range findAllNodes(root, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || x.nodeText(fn) != "bootstrapApplication" {
			continue
		}
		args := call.ChildByFieldName("arguments")
		if args == nil {
			continue
		}
		// The options object is the second argument; pull its providers array.
		raw := x.angularOptionsProvidersRaw(args)
		if raw == "" {
			continue
		}
		var edges []angularProviderEdge
		edges = append(edges, angularParseProviders(raw)...)
		edges = append(edges, angularFunctionalInterceptorEdges(raw)...)
		if len(edges) == 0 {
			continue
		}
		appIdx := x.ensureAngularAppEntity(call)
		for _, e := range edges {
			x.entities[appIdx].Relationships = append(x.entities[appIdx].Relationships,
				angularGlobalUsesEdge(angularAppEntityName, e, "angular", "angular_bootstrap_provider", true))
		}
	}
}

// angularFunctionalInterceptorEdges extracts `withInterceptors([fn1, fn2])`
// functional interceptors from a bootstrap providers body. Each function
// identifier becomes an interceptor binding (di_role=interceptor, multi=true —
// functional interceptors compose like multi-providers).
func angularFunctionalInterceptorEdges(body string) []angularProviderEdge {
	var out []angularProviderEdge
	seen := map[string]bool{}
	for _, m := range reAngularWithInterceptors.FindAllStringSubmatch(body, -1) {
		for _, idm := range reAngularIdent.FindAllStringSubmatch(m[1], -1) {
			id := idm[1]
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, angularProviderEdge{
				target: id,
				token:  "HTTP_INTERCEPTORS",
				role:   "interceptor",
				multi:  true,
			})
		}
	}
	return out
}

// angularGlobalUsesEdge builds the module/app → target USES edge for a global
// provider binding (#4378), tagged global=true + di_token/di_role + multi.
func angularGlobalUsesEdge(owner string, e angularProviderEdge, framework, via string, fromApp bool) types.RelationshipRecord {
	props := map[string]string{
		"framework": framework,
		"di_role":   e.role,
		"di_scope":  "global",
		"di_token":  e.token,
		"global":    "true",
		"via":       via,
	}
	if fromApp {
		props["owner"] = owner
	} else {
		props["module"] = owner
	}
	if e.multi {
		props["multi"] = "true"
	}
	return types.RelationshipRecord{
		FromID:     owner,
		ToID:       e.target,
		Kind:       string(types.RelationshipKindUses),
		Properties: props,
	}
}

// decoratedClassName returns the name of the class a decorator node decorates,
// by scanning the decorator's parent for the sibling class_declaration.
func (x *extractor) decoratedClassName(dec ts.Node) string {
	parent := dec.Parent()
	if parent == nil {
		return ""
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		c := parent.Child(i)
		if c != nil && c.Type() == "class_declaration" {
			return x.nodeText(c.ChildByFieldName("name"))
		}
	}
	return ""
}

// angularDecoratorArrayRaw returns the raw source text inside the `key: [...]`
// array of a decorator call's first object-literal argument (e.g. the
// `providers` array of @NgModule), or "" when absent. Raw text is used so the
// uniform provider-object / bare-class parsing (shared with NestJS) applies.
func (x *extractor) angularDecoratorArrayRaw(call ts.Node, key string) string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	var obj ts.Node
	for i := 0; i < int(args.ChildCount()); i++ {
		if args.Child(i).Type() == "object" {
			obj = args.Child(i)
			break
		}
	}
	if obj == nil {
		return ""
	}
	return x.angularObjectArrayRaw(obj, key)
}

// angularOptionsProvidersRaw returns the raw `providers: [...]` array body from
// the second (options) argument of a bootstrapApplication(App, {providers}) call.
func (x *extractor) angularOptionsProvidersRaw(args ts.Node) string {
	// Find object-literal arguments (skip the leading component identifier).
	for i := 0; i < int(args.ChildCount()); i++ {
		c := args.Child(i)
		if c == nil || c.Type() != "object" {
			continue
		}
		if raw := x.angularObjectArrayRaw(c, "providers"); raw != "" {
			return raw
		}
	}
	return ""
}

// angularObjectArrayRaw returns the raw text inside the `key: [...]` array value
// of an object-literal node, or "" when the key is absent / not an array.
func (x *extractor) angularObjectArrayRaw(obj ts.Node, key string) string {
	for i := 0; i < int(obj.ChildCount()); i++ {
		pair := obj.Child(i)
		if pair.Type() != "pair" {
			continue
		}
		k := strings.Trim(x.nodeText(pair.ChildByFieldName("key")), `"'`)
		if k != key {
			continue
		}
		val := pair.ChildByFieldName("value")
		if val == nil || val.Type() != "array" {
			return ""
		}
		raw := x.nodeText(val)
		// Strip the surrounding [ ] so the body parses identically to a NestJS
		// array body.
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(raw, "[")
		raw = strings.TrimSuffix(raw, "]")
		return raw
	}
	return ""
}

// entityIndexByName returns the index of the first emitted entity with the given
// Name, or -1. Used to attach module-provider USES edges to the @NgModule entity.
func (x *extractor) entityIndexByName(name string) int {
	for i := range x.entities {
		if x.entities[i].Name == name {
			return i
		}
	}
	return -1
}

// ensureAngularAppEntity returns the index of the synthetic `app` entity,
// emitting it (once) when absent. The entity owns the bootstrap global USES
// edges so the bound classes are retained and resolve through the symbol table.
func (x *extractor) ensureAngularAppEntity(anchor ts.Node) int {
	if idx := x.entityIndexByName(angularAppEntityName); idx >= 0 {
		return idx
	}
	start, end := lines(anchor)
	e := types.EntityRecord{
		Name:          angularAppEntityName,
		QualifiedName: x.qualify(angularAppEntityName),
		Kind:          "SCOPE.Component",
		SourceFile:    x.filePath,
		StartLine:     start,
		EndLine:       end,
		Language:      x.language,
		Subtype:       "application",
		Signature:     "bootstrapApplication(...)",
		Properties: map[string]string{
			"kind":       "SCOPE.Component",
			"subtype":    "application",
			"framework":  "angular",
			"provenance": "INFERRED_FROM_ANGULAR_BOOTSTRAP",
		},
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
	}
	e.ID = e.ComputeID()
	x.entities = append(x.entities, e)
	return len(x.entities) - 1
}

// angularNormaliseToken strips quotes/whitespace/generics from a DI token
// expression, mirroring nestNormaliseToken. A string-literal token 'CONFIG'
// becomes CONFIG; an identifier token (HTTP_INTERCEPTORS, a custom
// InjectionToken constant, ErrorHandler) keeps its leaf.
func angularNormaliseToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`")) {
		return strings.Trim(s, "'\"`")
	}
	if i := strings.IndexAny(s, " <([{"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// angularImplName extracts the implementation identifier bound to a token for
// the given use-kind, mirroring nestImplName. useClass/useExisting/useFactory
// yield a class/function leaf; useValue yields a PascalCase reference (else "").
func angularImplName(expr, useKind string) string {
	expr = strings.TrimSpace(expr)
	switch useKind {
	case "useClass", "useExisting", "useFactory":
		if i := strings.IndexAny(expr, " <([{,"); i >= 0 {
			expr = expr[:i]
		}
		expr = strings.TrimSpace(expr)
		return expr
	case "useValue":
		if i := strings.IndexAny(expr, " <([{,"); i >= 0 {
			expr = expr[:i]
		}
		expr = strings.TrimSpace(expr)
		if expr == "" || expr[0] < 'A' || expr[0] > 'Z' {
			return ""
		}
		return expr
	}
	return ""
}
