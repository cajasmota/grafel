package javascript

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// NestJS dependency-injection extraction (#3628 area #5).
//
// The base nestjsExtractor emits entities (controllers, services, modules,
// guards, …) but no DI edges, so the rewrite-parity oracle cannot resolve
// "what provider satisfies this constructor param" or "what guard protects this
// endpoint". This file adds the DI graph by emitting:
//
//   INJECTED_INTO : provider/token → the class whose constructor injects it.
//                   Mirrors the Angular DI convention (angular.go): FromID is
//                   the injected provider (type name or @Inject token), ToID is
//                   the consuming class. Property via=nestjs_constructor.
//
//   BINDS         : a NestJS @Module → each provider/controller it declares,
//                   each module it imports, and each provider it exports; and a
//                   DI token → its implementation for the object-form provider
//                   shapes {provide: TOKEN, useClass/useValue/useFactory/
//                   useExisting: Impl}. Mirrors the Helm BINDS shape (token →
//                   impl). Property binding_kind distinguishes the sub-shape.
//
//   USES          : a controller class (class-level @UseGuards/@UseInterceptors/
//                   @UsePipes) or a route handler operation (method-level) → the
//                   guard / interceptor / pipe it applies. This is what the
//                   oracle needs to resolve auth on an endpoint. Property
//                   di_role records guard|interceptor|pipe.
//
// All edges use bare class / token names for FromID/ToID, matching the Angular
// path; the cross-file resolver binds them to the declaring entity. Token →
// impl bindings whose token is a string literal resolve cross-file only when an
// {provide:} site names the same token, so the coverage record marks
// module-wiring/di-binding "partial" for cross-file token resolution.

var (
	// reNestInjectableScope captures the Scope.X enum from
	// @Injectable({ scope: Scope.REQUEST }).
	reNestInjectableScope = regexp.MustCompile(
		`@Injectable\s*\(\s*\{[^}]*\bscope\s*:\s*Scope\.(\w+)[^}]*\}\s*\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)

	// reNestClassDecl captures a (possibly decorated) class declaration head so
	// the constructor / @UseGuards scan can be anchored to its class name. The
	// class name is group 1. The decorator block preceding the class is consumed
	// loosely so @UseGuards at class level falls within the matched span when we
	// re-scan from the class start.
	reNestClassDecl = regexp.MustCompile(`class\s+([A-Z][A-Za-z0-9_]*)`)

	// reNestConstructor captures the parameter list of a class constructor.
	reNestConstructor = regexp.MustCompile(`constructor\s*\(`)

	// reNestModuleDecorator captures a @Module({...}) decorator's object body
	// (group 1) and the class it decorates (group 2).
	reNestModuleDecorator = regexp.MustCompile(
		`@Module\s*\(\s*(\{[\s\S]*?\})\s*\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)

	// reNestUseDecorator captures class- or method-level @UseGuards /
	// @UseInterceptors / @UsePipes and their argument list (group 2).
	reNestUseDecorator = regexp.MustCompile(
		`@Use(Guards|Interceptors|Pipes)\s*\(([^)]*)\)`,
	)

	// reNestIdent matches a bare PascalCase identifier (a class reference).
	reNestIdent = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)\b`)

	// reNestProvideObject matches an object-literal provider entry
	// {provide: TOKEN, useClass|useValue|useFactory|useExisting: Impl}. Group 1
	// is the raw token expression, group 2 the binding keyword, group 3 the impl
	// expression (best-effort up to the next comma/brace).
	reNestProvideObject = regexp.MustCompile(
		`provide\s*:\s*([^,]+?)\s*,\s*(useClass|useValue|useFactory|useExisting)\s*:\s*([^,}]+)`,
	)
)

var nestUseRoleMap = map[string]string{
	"Guards":       "guard",
	"Interceptors": "interceptor",
	"Pipes":        "pipe",
}

// nestAppTokenRole maps a NestJS global cross-cutting DI token (from
// '@nestjs/core') to the di_role of the class it binds app-wide. These tokens
// register a guard/interceptor/filter/pipe globally via the object-form
// provider shape { provide: APP_*, useClass|useExisting|useFactory: Impl } in a
// module's providers array (#4329). The bound class otherwise looks unused
// because the only inbound edge would dangle from the phantom magic token.
var nestAppTokenRole = map[string]string{
	"APP_GUARD":       "guard",
	"APP_INTERCEPTOR": "interceptor",
	"APP_FILTER":      "filter",
	"APP_PIPE":        "pipe",
}

// nestGlobalMethodRole maps an app.useGlobal*() bootstrap call (typically in
// main.ts) to the di_role of the class it binds app-wide (#4329).
var nestGlobalMethodRole = map[string]string{
	"useGlobalGuards":       "guard",
	"useGlobalInterceptors": "interceptor",
	"useGlobalFilters":      "filter",
	"useGlobalPipes":        "pipe",
}

// nestAppEntityName is the synthetic owner name for app-level global wiring
// emitted from a bootstrap file's app.useGlobal*() calls.
const nestAppEntityName = "app"

// reNestUseGlobal captures the head of an app.useGlobal*( call. Group 1 is the
// method name; the argument list is read with balanced-paren scanning from the
// captured '(' so nested calls like new ValidationPipe({...}) are not truncated.
var reNestUseGlobal = regexp.MustCompile(
	`\.\s*(useGlobalGuards|useGlobalInterceptors|useGlobalFilters|useGlobalPipes)\s*\(`,
)

// extractNestDIEdges scans a NestJS source file and returns the DI edges grouped
// by the bare entity name they should attach to (the FromID side for
// INJECTED_INTO/USES on a class, the module name for BINDS). The caller merges
// these onto the matching emitted entity's Relationships slice.
//
// edgesByOwner key is the bare Name of the file-local entity the edge should
// hang off (so the resolver always finds a real source entity). The edge's
// FromID/ToID still carry the semantic provider/consumer/token names, mirroring
// angular.go where INJECTED_INTO has FromID=provider, ToID=consumer but the
// edge is attached to the consumer class entity present in the file.
//
//	INJECTED_INTO : owner = consumer class (FromID=provider, ToID=consumer)
//	USES (class)  : owner = controller class
//	USES (handler): owner = "<HTTP_METHOD> <method>" operation
//	BINDS (module): owner = module class
//	BINDS (token) : owner = module class (token entity may be cross-file)
func extractNestDIEdges(src string) map[string][]types.RelationshipRecord {
	out := map[string][]types.RelationshipRecord{}
	add := func(owner string, r types.RelationshipRecord) {
		out[owner] = append(out[owner], r)
	}

	// ---- Constructor injection: provider INJECTED_INTO consumer ----------
	for _, c := range nestClasses(src) {
		params := nestConstructorParams(src, c.bodyStart)
		for _, p := range params {
			provider := p.token
			if provider == "" {
				continue
			}
			add(c.name, types.RelationshipRecord{
				FromID: provider,
				ToID:   c.name,
				Kind:   string(types.RelationshipKindInjectedInto),
				Properties: map[string]string{
					"consumer":  c.name,
					"provider":  provider,
					"framework": "nestjs",
					"via":       "nestjs_constructor",
					"di_role":   p.role,
				},
			})
		}
		// ---- Class-level @UseGuards/@UseInterceptors/@UsePipes ----------
		for _, u := range nestUseDecoratorsIn(src[c.declStart:c.bodyStart]) {
			add(c.name, types.RelationshipRecord{
				FromID: c.name,
				ToID:   u.target,
				Kind:   string(types.RelationshipKindUses),
				Properties: map[string]string{
					"framework": "nestjs",
					"di_role":   u.role,
					"di_scope":  "class",
					"via":       "nestjs_use_decorator",
				},
			})
		}
	}

	// ---- Method-level @UseGuards on route handlers -----------------------
	// The route handler operation entity is named "<HTTP_METHOD> <method>" by
	// the base extractor; we attach the USES edge to that operation so the
	// oracle can answer "what guard protects this endpoint". We re-scan each
	// HTTP-verb decorator block and look backwards for a sibling @UseGuards
	// applied to the same handler.
	for _, h := range nestRouteHandlers(src) {
		for _, u := range nestUseDecoratorsIn(h.decoratorBlock) {
			owner := h.opName
			add(owner, types.RelationshipRecord{
				FromID: owner,
				ToID:   u.target,
				Kind:   string(types.RelationshipKindUses),
				Properties: map[string]string{
					"framework":   "nestjs",
					"di_role":     u.role,
					"di_scope":    "handler",
					"method_name": h.methodName,
					"via":         "nestjs_use_decorator",
				},
			})
		}
	}

	// ---- @Module wiring: BINDS -------------------------------------------
	for _, mm := range reNestModuleDecorator.FindAllStringSubmatch(src, -1) {
		body := mm[1]
		module := mm[2]
		sections := nestModuleSections(body)
		// Collapse to one BINDS edge per (module → target): a member that is
		// both a provider and an export yields a single edge tagged
		// binding_kind=provider with exported=true, rather than two
		// (module,target)-identical edges. Precedence: controller > provider >
		// import (each names a distinct member kind; export is a flag).
		type bindInfo struct {
			kind     string
			exported bool
			order    int // emission order to keep output stable
		}
		binds := map[string]*bindInfo{}
		var order []string
		ensure := func(name string) *bindInfo {
			b, ok := binds[name]
			if !ok {
				b = &bindInfo{}
				binds[name] = b
				order = append(order, name)
			}
			return b
		}
		rank := map[string]int{"controller": 3, "provider": 2, "import": 1}
		for kind, names := range sections {
			for _, n := range names {
				b := ensure(n)
				if kind == "export" {
					b.exported = true
					if b.kind == "" {
						// Exported-but-not-locally-declared (re-export): record
						// the export membership so it is still queryable.
						b.kind = "export"
					}
					continue
				}
				if rank[kind] > rank[b.kind] {
					b.kind = kind
				}
			}
		}
		for _, n := range order {
			b := binds[n]
			props := map[string]string{
				"framework":    "nestjs",
				"binding_kind": b.kind, // provider|controller|import|export
				"module":       module,
				"via":          "nestjs_module",
			}
			if b.exported {
				props["exported"] = "true"
			}
			add(module, types.RelationshipRecord{
				FromID:     module,
				ToID:       n,
				Kind:       string(types.RelationshipKindBinds),
				Properties: props,
			})
		}
		// Object-form provider tokens: {provide: TOKEN, useClass: Impl} → the
		// token BINDS its implementation (token → impl), mirroring Helm BINDS.
		for _, pm := range reNestProvideObject.FindAllStringSubmatch(body, -1) {
			token := nestNormaliseToken(pm[1])
			useKind := pm[2]
			impl := nestImplName(pm[3], useKind)
			if token == "" || impl == "" {
				continue
			}
			add(module, types.RelationshipRecord{
				FromID: token,
				ToID:   impl,
				Kind:   string(types.RelationshipKindBinds),
				Properties: map[string]string{
					"framework":    "nestjs",
					"binding_kind": strings.ToLower(useKind[:1]) + useKind[1:], // useClass etc.
					"token":        token,
					"module":       module,
					"via":          "nestjs_provider_token",
				},
			})

			// Global cross-cutting DI mounts: { provide: APP_*, useClass|…: Impl }
			// binds the impl class app-wide (#4329). The token-form BINDS above
			// dangles from the phantom magic token, so without this the impl class
			// (guard/interceptor/filter/pipe) looks orphan / dead. Emit a
			// module → impl USES edge marked global so the app-wide scope is
			// queryable and the class is connected to a real source entity.
			if role, ok := nestAppTokenRole[token]; ok {
				add(module, types.RelationshipRecord{
					FromID: module,
					ToID:   impl,
					Kind:   string(types.RelationshipKindUses),
					Properties: map[string]string{
						"framework": "nestjs",
						"di_role":   role,
						"di_scope":  "global",
						"di_token":  token,
						"global":    "true",
						"module":    module,
						"via":       "nestjs_app_token",
					},
				})
			}
		}
	}

	// ---- Bootstrap global wiring: app.useGlobal*() -----------------------
	// In main.ts the app instance binds guards/interceptors/filters/pipes
	// app-wide via app.useGlobalGuards(...) etc. These have no owning class or
	// module, so the edge is hung off a synthetic `app` entity (emitted by the
	// caller when global wiring is present) and points at the bound class.
	for owner, r := range extractNestGlobalWiring(src) {
		out[owner] = append(out[owner], r...)
	}

	return out
}

// extractNestGlobalWiring returns app.useGlobal*() USES edges keyed by the
// synthetic app-owner name (#4329). Each call arg that names a class — bare
// `Foo` or `new Foo(...)` — yields an `app` USES <class> edge marked global.
func extractNestGlobalWiring(src string) map[string][]types.RelationshipRecord {
	out := map[string][]types.RelationshipRecord{}
	for _, m := range reNestUseGlobal.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		role := nestGlobalMethodRole[method]
		open := m[1] - 1 // index of the '(' captured at the end of the match
		args := nestBalanced(src, open, '(', ')')
		if args == "" {
			continue
		}
		seen := map[string]bool{}
		for _, cls := range nestGlobalArgClasses(args) {
			if seen[cls] {
				continue
			}
			seen[cls] = true
			out[nestAppEntityName] = append(out[nestAppEntityName], types.RelationshipRecord{
				FromID: nestAppEntityName,
				ToID:   cls,
				Kind:   string(types.RelationshipKindUses),
				Properties: map[string]string{
					"framework": "nestjs",
					"di_role":   role,
					"di_scope":  "global",
					"global":    "true",
					"via":       "nestjs_use_global",
				},
			})
		}
	}
	return out
}

// nestGlobalArgClasses returns the PascalCase class identifiers referenced in an
// app.useGlobal*() argument list. It handles `Foo`, `new Foo(...)`, and
// multiple comma-separated args, ignoring config-object option keys by only
// taking the leading identifier of each top-level argument.
func nestGlobalArgClasses(args string) []string {
	var out []string
	for _, raw := range nestSplitParams(args) {
		a := strings.TrimSpace(raw)
		if a == "" {
			continue
		}
		a = strings.TrimPrefix(a, "new ")
		a = strings.TrimSpace(a)
		// Leaf identifier before any (, <, ., space.
		if i := strings.IndexAny(a, " <([{.,"); i >= 0 {
			a = a[:i]
		}
		a = strings.TrimSpace(a)
		if a == "" || a[0] < 'A' || a[0] > 'Z' {
			continue
		}
		out = append(out, a)
	}
	return out
}

// nestHasGlobalWiring reports whether src contains any app.useGlobal*() call,
// so the caller knows to emit the synthetic `app` owner entity.
func nestHasGlobalWiring(src string) bool {
	return reNestUseGlobal.MatchString(src)
}

// nestClassInfo describes a class declaration span within a file.
type nestClassInfo struct {
	name      string
	declStart int // index of the `class` keyword (after decorators are scanned back)
	bodyStart int // index of the opening `{` of the class body
}

// nestClasses returns every PascalCase class in the file with the byte offsets
// needed to scope the constructor / decorator scans.
func nestClasses(src string) []nestClassInfo {
	var out []nestClassInfo
	for _, m := range reNestClassDecl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		declStart := m[0]
		// Walk back over a preceding decorator block so class-level @UseGuards
		// is included in [declStart, bodyStart). We move declStart back to the
		// start of the contiguous decorator/export prefix.
		ds := declStart
		// Find the body open brace.
		brace := strings.IndexByte(src[m[1]:], '{')
		if brace < 0 {
			continue
		}
		bodyStart := m[1] + brace
		// Expand declStart backwards across a decorator span: scan up to ~400
		// bytes before the class for the nearest preceding blank line / `}` /
		// `;` boundary so class-level decorators are captured.
		lo := ds - 600
		if lo < 0 {
			lo = 0
		}
		prefix := src[lo:ds]
		if idx := nestDecoratorPrefixStart(prefix); idx >= 0 {
			ds = lo + idx
		}
		out = append(out, nestClassInfo{name: name, declStart: ds, bodyStart: bodyStart})
	}
	return out
}

// nestDecoratorPrefixStart returns the offset within prefix where the contiguous
// trailing decorator block begins, or -1 when the text immediately before the
// class is not a decorator. It walks back over lines that are blank, `export`,
// or start with `@`, stopping at the first non-decorator code line.
func nestDecoratorPrefixStart(prefix string) int {
	lines := strings.Split(prefix, "\n")
	// Find the index of the last line that is a decorator/export so we can keep
	// the contiguous trailing run.
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" || t == "export" || t == "export default" {
			continue
		}
		if strings.HasPrefix(t, "@") {
			start = i
			continue
		}
		break
	}
	if start < 0 {
		return -1
	}
	// Compute byte offset of line `start`.
	off := 0
	for i := 0; i < start; i++ {
		off += len(lines[i]) + 1
	}
	return off
}

// nestCtorParam is one constructor injection target.
type nestCtorParam struct {
	token string // injected provider: the @Inject token, else the param type
	role  string // "token" when @Inject(...) custom token, else "type"
}

// nestConstructorParams parses the constructor parameter list of the class
// whose body opens at bodyStart and returns the injected providers. It handles:
//
//	constructor(private readonly userService: UserService)        → UserService
//	constructor(@Inject('CONFIG') private cfg: ConfigShape)       → CONFIG (token)
//	constructor(@Inject(TOKEN) private x: X)                      → TOKEN  (token)
func nestConstructorParams(src string, bodyStart int) []nestCtorParam {
	// Search for the constructor within the class body.
	body := src[bodyStart:]
	loc := reNestConstructor.FindStringIndex(body)
	if loc == nil {
		return nil
	}
	open := bodyStart + loc[1] - 1 // index of '('
	params := nestBalanced(src, open, '(', ')')
	if params == "" {
		return nil
	}
	var out []nestCtorParam
	for _, raw := range nestSplitParams(params) {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		// @Inject(token) custom token wins over the declared type.
		if tok := nestInjectToken(p); tok != "" {
			out = append(out, nestCtorParam{token: tok, role: "token"})
			continue
		}
		if t := nestParamType(p); t != "" {
			out = append(out, nestCtorParam{token: t, role: "type"})
		}
	}
	return out
}

var reNestInject = regexp.MustCompile(`@Inject\s*\(\s*([^)]*?)\s*\)`)

// nestInjectToken extracts the token from an @Inject(...) parameter decorator,
// normalising a string-literal token to its bare value.
func nestInjectToken(param string) string {
	m := reNestInject.FindStringSubmatch(param)
	if m == nil {
		return ""
	}
	return nestNormaliseToken(m[1])
}

// nestParamType returns the leaf type identifier of a constructor parameter
// (the part after the last `:`), rejecting primitives and lowercase shapes.
func nestParamType(param string) string {
	idx := strings.LastIndex(param, ":")
	if idx < 0 {
		return ""
	}
	t := strings.TrimSpace(param[idx+1:])
	// Strip default value / trailing tokens.
	if eq := strings.IndexByte(t, '='); eq >= 0 {
		t = strings.TrimSpace(t[:eq])
	}
	// Take the leaf identifier before any generic / union / array marker.
	if i := strings.IndexAny(t, "<|&[ \t"); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(t)
	switch t {
	case "", "string", "number", "boolean", "any", "void", "object",
		"unknown", "never", "symbol", "bigint":
		return ""
	}
	if t[0] < 'A' || t[0] > 'Z' {
		return ""
	}
	return t
}

// nestUse is a resolved @Use* decorator application.
type nestUse struct {
	role   string // guard|interceptor|pipe
	target string // the referenced class name
}

// nestUseDecoratorsIn returns every guard/interceptor/pipe referenced by a
// @UseGuards/@UseInterceptors/@UsePipes decorator within the given text span.
// Each PascalCase identifier in the decorator's argument list becomes a target
// (NestJS allows multiple per decorator, e.g. @UseGuards(A, B)).
func nestUseDecoratorsIn(span string) []nestUse {
	var out []nestUse
	seen := map[string]bool{}
	for _, m := range reNestUseDecorator.FindAllStringSubmatch(span, -1) {
		role := nestUseRoleMap[m[1]]
		for _, idm := range reNestIdent.FindAllStringSubmatch(m[2], -1) {
			target := idm[1]
			key := role + ":" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, nestUse{role: role, target: target})
		}
	}
	return out
}

// nestRouteHandler describes a decorated HTTP route handler method.
type nestRouteHandler struct {
	opName         string // matches the base extractor's "<HTTP_METHOD> <method>" entity name
	methodName     string
	decoratorBlock string // the contiguous decorator text preceding the handler
}

var reNestRouteHandler = regexp.MustCompile(
	`@(Get|Post|Put|Delete|Patch|Options|Head|All)\s*\(([^)]*)\)\s*(?:async\s+)?(\w+)\s*\(`,
)

// nestRouteHandlers returns each HTTP route handler with the decorator block
// immediately preceding it (so method-level @UseGuards can be associated).
func nestRouteHandlers(src string) []nestRouteHandler {
	var out []nestRouteHandler
	for _, m := range reNestRouteHandler.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		methodName := src[m[6]:m[7]]
		httpMethod := nestHTTPVerbMap[verb]
		// Decorator block: walk back from the @Verb decorator across the
		// contiguous decorator run (other @Use*/@Verb decorators on the same
		// handler appear directly above or below; NestJS allows either order).
		lo := m[0] - 400
		if lo < 0 {
			lo = 0
		}
		// Extend forward to the handler's opening paren so a @UseGuards placed
		// *between* @Get and the method name is captured too.
		hi := m[1]
		block := src[lo:hi]
		if idx := nestDecoratorPrefixStart(src[lo:m[0]]); idx >= 0 {
			block = src[lo+idx : hi]
		} else {
			block = src[m[0]:hi]
		}
		out = append(out, nestRouteHandler{
			opName:         httpMethod + " " + methodName,
			methodName:     methodName,
			decoratorBlock: block,
		})
	}
	return out
}

// nestModuleSections parses a @Module({...}) object body and returns the
// PascalCase identifiers declared under providers/controllers/imports/exports.
// The returned map keys are binding_kind values (singular): provider,
// controller, import, export. Object-form provider entries ({provide: …}) are
// skipped here (handled separately as token→impl BINDS) but their useClass impl
// is still surfaced as a provider membership so module→impl is queryable.
func nestModuleSections(body string) map[string][]string {
	out := map[string][]string{}
	for key, kind := range map[string]string{
		"providers":   "provider",
		"controllers": "controller",
		"imports":     "import",
		"exports":     "export",
	} {
		arr := nestArrayFor(body, key)
		if arr == "" {
			continue
		}
		seen := map[string]bool{}
		// Bare identifiers (UsersService) and object-form impls (useClass: X).
		for _, idm := range reNestIdent.FindAllStringSubmatch(arr, -1) {
			id := idm[1]
			// Skip object-literal keyword identifiers.
			switch id {
			case "Inject", "Scope":
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out[kind] = append(out[kind], id)
		}
	}
	return out
}

// nestArrayFor returns the raw text inside the `[...]` value of `key:` within a
// module-decorator object body, or "" when the key is absent.
func nestArrayFor(body, key string) string {
	re := regexp.MustCompile(`\b` + key + `\s*:\s*\[`)
	loc := re.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	open := loc[1] - 1 // index of '['
	return nestBalanced(body, open, '[', ']')
}

// nestBalanced returns the substring strictly inside the balanced bracket pair
// that opens at index `open` (src[open]==openCh). Returns "" on imbalance.
func nestBalanced(src string, open int, openCh, closeCh byte) string {
	if open < 0 || open >= len(src) || src[open] != openCh {
		return ""
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return ""
}

// nestSplitParams splits a constructor parameter list on top-level commas,
// respecting nested (), [], {}, <> so generics and @Inject(...) args don't
// break a parameter in two.
func nestSplitParams(params string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, params[start:i])
				start = i + 1
			}
		}
	}
	if start < len(params) {
		out = append(out, params[start:])
	}
	return out
}

// nestNormaliseToken strips quotes/whitespace from a DI token expression. A
// string-literal token 'CONFIG' becomes CONFIG; an identifier token TOKEN is
// returned as-is. Returns "" for empty/unparseable tokens.
func nestNormaliseToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// String literal → bare value.
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`")) {
		return strings.Trim(s, "'\"`")
	}
	// Identifier token (possibly InjectionToken constant) — keep leaf.
	if i := strings.IndexAny(s, " <([{"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// nestImplName extracts the implementation identifier bound to a token for the
// given use-kind. For useClass/useExisting it is a class identifier; for
// useValue it may be any expression (we keep the leaf identifier when it is
// PascalCase, else ""); for useFactory it is a function reference.
func nestImplName(expr, useKind string) string {
	expr = strings.TrimSpace(expr)
	switch useKind {
	case "useClass", "useExisting", "useFactory":
		if i := strings.IndexAny(expr, " <([{,"); i >= 0 {
			expr = expr[:i]
		}
		expr = strings.TrimSpace(expr)
		if expr == "" {
			return ""
		}
		return expr
	case "useValue":
		// A useValue may be a class/const reference (PascalCase) or a literal.
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
