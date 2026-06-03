package python

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_di_graph", &PyDIExtractor{})
}

// PyDIExtractor extracts dependency-injection edges for two Python DI styles
// (#3628 area #5), matching the FROM/TO contract of the NestJS / Angular
// emitters:
//
//	INJECTED_INTO : provider (dependency callable/class) → consumer (the
//	                handler/class that receives it). FromID=provider,
//	                ToID=consumer.
//	BINDS         : token → implementation. FromID=token, ToID=impl.
//
// Two frameworks are covered:
//
//   - FastAPI: a route/handler parameter `svc: Service = Depends(get_service)`
//     yields INJECTED_INTO(get_service → handler). When the Depends() argument
//     names a class (`Depends(SvcClass)`) the provider is that class. A bare
//     `Depends()` resolves to the parameter's type annotation. The consumer is
//     the enclosing `def`.
//
//   - dependency-injector: a DeclarativeContainer's provider attribute
//     `service = providers.Factory(Service, ...)` yields BINDS(service → Service)
//     (token = the container attribute name, impl = the first positional class).
//     An `@inject`-decorated function whose parameter defaults to
//     `Provide[Container.service]` yields INJECTED_INTO(service → function),
//     where the provider is the container attribute (the token).
//
// Honest-partial: dynamic dependencies whose callable cannot be resolved to a
// bare identifier (`Depends(getattr(mod, name))`, `Depends(make_dep())`) are
// skipped, never fabricated. Likewise a `Provide[...]` whose subscript is not a
// dotted attribute chain is skipped.
type PyDIExtractor struct{}

func (e *PyDIExtractor) Language() string { return "python_di_graph" }

var (
	// rePyDef captures a `def name(` head with the byte offset of the opening
	// paren so the signature can be balanced-scanned. Group 1 = function name.
	rePyDef = regexp.MustCompile(`(?m)^[ \t]*(?:async[ \t]+)?def[ \t]+(\w+)[ \t]*\(`)

	// rePyDependsParam matches a parameter assigned `= Depends(<arg>)`. Group 1
	// is the (possibly dotted) dependency identifier; empty when `Depends()` is
	// bare. The leading `:type` is parsed separately for the bare-Depends case.
	rePyDependsCall = regexp.MustCompile(`=[ \t]*Depends[ \t]*\(\s*([A-Za-z_][\w.]*)?\s*\)`)

	// rePyContainerClass captures a class deriving from a dependency-injector
	// DeclarativeContainer / DynamicContainer. Group 1 = container class name,
	// and the match index marks the class body start for the provider scan.
	rePyContainerClass = regexp.MustCompile(
		`(?m)^[ \t]*class[ \t]+(\w+)[ \t]*\([^)]*\b(?:DeclarativeContainer|DynamicContainer)\b[^)]*\)\s*:`)

	// rePyProvider captures a container provider attribute:
	//   name = providers.Factory(Impl, ...)  |  Singleton(...) | Callable(...) ...
	// Group 1 = attribute (token) name, group 2 = provider kind, group 3 = the
	// first positional argument (the implementation), best-effort.
	rePyProvider = regexp.MustCompile(
		`(?m)^[ \t]*(\w+)[ \t]*=[ \t]*(?:providers\.)?(Factory|Singleton|Callable|Resource|Object|Configuration|Selector|Dict|List|Aggregate|ThreadLocalSingleton|ContextLocalSingleton)[ \t]*\(\s*([A-Za-z_][\w.]*)?`)

	// rePyProvideDefault matches a parameter defaulting to a dependency-injector
	// wiring marker `Provide[Container.attr]` (or nested `Provide[C.sub.attr]`).
	// Group 1 = the dotted chain inside the subscript.
	rePyProvideMarker = regexp.MustCompile(`Provide\[\s*([A-Za-z_][\w.]*)\s*\]`)

	// rePyInjectDecorator detects an `@inject` decorator preceding a def.
	rePyInjectDecorator = regexp.MustCompile(`(?m)^[ \t]*@inject\b`)

	// reLitestarDepsKw locates a `dependencies = {` / `dependencies={` keyword
	// (litestar declares DI providers in a dict keyed by the handler param name).
	// The match index marks the position just past the `{` so the dict body can
	// be balanced-scanned.
	reLitestarDepsKw = regexp.MustCompile(`\bdependencies[ \t]*=[ \t]*\{`)

	// reLitestarProvideItem matches one dict item `"key": Provide(<callable>)`
	// inside a litestar `dependencies={...}` mapping. Group 1 = dependency key
	// (the handler param name / BINDS token), group 2 = the provider callable
	// passed as Provide()'s first positional argument (best-effort, may be empty
	// for a dynamic/kwarg-only Provide which is then skipped).
	reLitestarProvideItem = regexp.MustCompile(
		`["']([A-Za-z_]\w*)["']\s*:\s*(?:litestar\.di\.|di\.)?Provide[ \t]*\(\s*([A-Za-z_][\w.]*)?`)

	// reSanicDependency matches sanic's native DI registration
	// `app.ext.dependency(<impl>)` / `app.ext.add_dependency("name", <impl>)`.
	// Group 1 = the implementation/instance identifier (first positional).
	reSanicDependency = regexp.MustCompile(
		`\.ext\.(?:add_)?dependency[ \t]*\(\s*(?:["'][A-Za-z_]\w*["']\s*,\s*)?([A-Za-z_][\w.]*)`)
)

func (e *PyDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_di_graph")
	_, span := tracer.Start(ctx, "custom.python_di_graph")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	var out []types.EntityRecord
	edgeCount := 0

	// addEdge emits a thin owner entity carrying one DI relationship. The owner
	// Name is synthetic (di:<role>:<from>-><to>@<line>) so it never collides
	// with — and therefore never replaces, via MergeWithCustom — a rich base
	// entity of the same class/function name. The semantic provider/consumer/
	// token names live on the relationship's FromID/ToID, which the cross-file
	// resolver binds via its global byName index independent of the owner.
	addEdge := func(subtype string, line int, props map[string]string, rel types.RelationshipRecord) {
		owner := "di:" + rel.Kind + ":" + rel.FromID + "->" + rel.ToID + "@" + strconv.Itoa(line)
		ent := entity(owner, "SCOPE.Pattern", subtype, file.Path, line, props)
		ent.Relationships = append(ent.Relationships, rel)
		out = append(out, ent)
		edgeCount++
	}

	// ---- FastAPI Depends() constructor/param injection -------------------
	for _, fn := range pyFunctions(src) {
		sig := pyBalancedParen(src, fn.parenOpen)
		if sig == "" {
			continue
		}
		for _, p := range pySplitParams(sig) {
			provider := pyDependsProvider(p)
			if provider == "" {
				continue
			}
			addEdge("di_consumer", fn.line,
				map[string]string{
					"framework": "fastapi",
					"di_role":   "depends",
					"consumer":  fn.name,
					"provider":  provider,
					"via":       "fastapi_depends",
				},
				types.RelationshipRecord{
					FromID: provider,
					ToID:   fn.name,
					Kind:   string(types.RelationshipKindInjectedInto),
					Properties: map[string]string{
						"framework": "fastapi",
						"provider":  provider,
						"consumer":  fn.name,
						"via":       "fastapi_depends",
					},
				})
		}
	}

	// ---- dependency-injector container providers: BINDS token → impl -----
	for _, c := range pyContainers(src) {
		body := src[c.bodyStart:c.bodyEnd]
		bodyLineBase := lineOf(src, c.bodyStart)
		for _, m := range rePyProvider.FindAllStringSubmatchIndex(body, -1) {
			token := body[m[2]:m[3]]
			kind := body[m[4]:m[5]]
			impl := ""
			if m[6] >= 0 {
				impl = pyLeafIdent(body[m[6]:m[7]])
			}
			if impl == "" {
				// Provider with no resolvable positional class (e.g.
				// Configuration(), Object(literal)) — skip, no fabrication.
				continue
			}
			line := bodyLineBase + strings.Count(body[:m[0]], "\n")
			addEdge("di_provider", line,
				map[string]string{
					"framework":     "dependency-injector",
					"provider_kind": kind,
					"container":     c.name,
					"token":         token,
					"impl":          impl,
					"via":           "dependency_injector_provider",
				},
				types.RelationshipRecord{
					FromID: token,
					ToID:   impl,
					Kind:   string(types.RelationshipKindBinds),
					Properties: map[string]string{
						"framework":     "dependency-injector",
						"provider_kind": kind,
						"container":     c.name,
						"token":         token,
						"via":           "dependency_injector_provider",
					},
				})
		}
	}

	// ---- dependency-injector @inject + Provide[...]: INJECTED_INTO -------
	// Only consider Provide[...] markers that sit inside an @inject-decorated
	// function's signature, so we attribute the edge to the right consumer.
	for _, fn := range pyInjectFunctions(src) {
		sig := pyBalancedParen(src, fn.parenOpen)
		if sig == "" {
			continue
		}
		for _, pm := range rePyProvideMarker.FindAllStringSubmatch(sig, -1) {
			token := pyProvideToken(pm[1])
			if token == "" {
				continue
			}
			addEdge("di_consumer", fn.line,
				map[string]string{
					"framework": "dependency-injector",
					"di_role":   "inject",
					"consumer":  fn.name,
					"provider":  token,
					"via":       "dependency_injector_inject",
				},
				types.RelationshipRecord{
					FromID: token,
					ToID:   fn.name,
					Kind:   string(types.RelationshipKindInjectedInto),
					Properties: map[string]string{
						"framework": "dependency-injector",
						"provider":  token,
						"consumer":  fn.name,
						"via":       "dependency_injector_inject",
					},
				})
		}
	}

	// ---- litestar native DI: dependencies={"k": Provide(cb)} --------------
	// BINDS(key -> provider callable) for each dict item; the dependency key is
	// the token that a handler param resolves by name. Then INJECTED_INTO for any
	// handler param whose name matches a declared key (litestar resolves DI by
	// parameter name == dependency key).
	depKeyToProvider := map[string]string{}
	for _, ds := range pyLitestarDepsDicts(src) {
		body := src[ds.start:ds.end]
		lineBase := lineOf(src, ds.start)
		for _, m := range reLitestarProvideItem.FindAllStringSubmatchIndex(body, -1) {
			key := body[m[2]:m[3]]
			provider := ""
			if m[4] >= 0 && !pyIdentIsCalled(body, m[5]) {
				provider = pyLeafIdent(body[m[4]:m[5]])
			}
			if provider == "" {
				// Dynamic/kwarg-only/called Provide(make_dep()) — no resolvable
				// static callable; skip (honest-partial, never fabricate).
				continue
			}
			line := lineBase + strings.Count(body[:m[0]], "\n")
			depKeyToProvider[key] = provider
			addEdge("di_provider", line,
				map[string]string{
					"framework": "litestar",
					"token":     key,
					"impl":      provider,
					"via":       "litestar_provide",
				},
				types.RelationshipRecord{
					FromID: key,
					ToID:   provider,
					Kind:   string(types.RelationshipKindBinds),
					Properties: map[string]string{
						"framework": "litestar",
						"token":     key,
						"via":       "litestar_provide",
					},
				})
		}
	}
	// INJECTED_INTO: a handler param named like a declared dependency key →
	// INJECTED_INTO(provider -> handler). Only fires when the key was actually
	// bound via Provide() above, so the param-name match reflects a real binding.
	if len(depKeyToProvider) > 0 {
		for _, fn := range pyFunctions(src) {
			sig := pyBalancedParen(src, fn.parenOpen)
			if sig == "" {
				continue
			}
			for _, p := range pySplitParams(sig) {
				name := pyParamName(p)
				if name == "" || name == "self" || name == "cls" {
					continue
				}
				provider, ok := depKeyToProvider[name]
				if !ok {
					continue
				}
				addEdge("di_consumer", fn.line,
					map[string]string{
						"framework": "litestar",
						"di_role":   "provide",
						"consumer":  fn.name,
						"provider":  provider,
						"token":     name,
						"via":       "litestar_provide",
					},
					types.RelationshipRecord{
						FromID: provider,
						ToID:   fn.name,
						Kind:   string(types.RelationshipKindInjectedInto),
						Properties: map[string]string{
							"framework": "litestar",
							"provider":  provider,
							"consumer":  fn.name,
							"token":     name,
							"via":       "litestar_provide",
						},
					})
			}
		}
	}

	// ---- sanic native DI: app.ext.dependency(impl) -----------------------
	// Sanic registers a DI instance/type via app.ext.dependency(...); the
	// instance is then injected into handlers by type annotation. We can resolve
	// the registration (BINDS impl -> impl, i.e. the registered provider), but
	// not the per-handler annotation→type match reliably file-locally, so the
	// injection point stays honest-missing for sanic.
	for _, m := range reSanicDependency.FindAllStringSubmatchIndex(src, -1) {
		if pyIdentIsCalled(src, m[3]) {
			// dependency(SessionFactory()) — the registered impl is the result of
			// a call expression, not a static type; skip (honest-partial).
			continue
		}
		impl := pyLeafIdent(src[m[2]:m[3]])
		if impl == "" {
			continue
		}
		line := lineOf(src, m[0])
		addEdge("di_provider", line,
			map[string]string{
				"framework": "sanic",
				"token":     impl,
				"impl":      impl,
				"via":       "sanic_ext_dependency",
			},
			types.RelationshipRecord{
				FromID: impl,
				ToID:   impl,
				Kind:   string(types.RelationshipKindBinds),
				Properties: map[string]string{
					"framework": "sanic",
					"token":     impl,
					"via":       "sanic_ext_dependency",
				},
			})
	}

	span.SetAttributes(attribute.Int("di_edge_count", edgeCount))
	return out, nil
}

// pyLitestarDepsSpan describes the byte span of a litestar `dependencies={...}`
// dict body (between the braces).
type pyLitestarDepsSpan struct {
	start int
	end   int
}

// pyLitestarDepsDicts returns the body span of each `dependencies={...}` mapping
// in the source, balanced-scanning the braces so nested brackets are respected.
func pyLitestarDepsDicts(src string) []pyLitestarDepsSpan {
	var out []pyLitestarDepsSpan
	for _, loc := range reLitestarDepsKw.FindAllStringIndex(src, -1) {
		// loc[1]-1 is the '{'.
		open := loc[1] - 1
		inner := pyBalancedBrace(src, open)
		if inner < 0 {
			continue
		}
		out = append(out, pyLitestarDepsSpan{start: open + 1, end: inner})
	}
	return out
}

// pyBalancedBrace returns the byte offset of the matching '}' for the '{' at
// index open (the returned offset points AT the closing brace). Returns -1 on
// imbalance. Brackets/parens nested inside are balanced too.
func pyBalancedBrace(src string, open int) int {
	if open < 0 || open >= len(src) || src[open] != '{' {
		return -1
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// pyIdentIsCalled reports whether the identifier ending at byte offset `end`
// (exclusive) is immediately followed by a `(`, i.e. it is a call expression
// like `make_dep()` rather than a bare callable reference. Leading whitespace
// between the identifier and the paren is tolerated.
func pyIdentIsCalled(src string, end int) bool {
	for i := end; i < len(src); i++ {
		switch src[i] {
		case ' ', '\t':
			continue
		case '(':
			return true
		default:
			return false
		}
	}
	return false
}

// pyParamName returns the parameter name from a single parameter declaration,
// stripping any `:type`, `=default`, and leading `*`/`**`. Returns "" for an
// empty/positional-only marker (`*`, `/`).
func pyParamName(param string) string {
	p := strings.TrimSpace(param)
	for strings.HasPrefix(p, "*") {
		p = strings.TrimSpace(p[1:])
	}
	if p == "" || p == "/" {
		return ""
	}
	if i := strings.IndexAny(p, ":=)"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if c := p[0]; !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return ""
	}
	for _, c := range p {
		if !(c == '_' || (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return ""
		}
	}
	return p
}

// pyFuncInfo locates a `def` head: its name, source line, and the byte offset
// of the opening parenthesis of its parameter list.
type pyFuncInfo struct {
	name      string
	line      int
	parenOpen int
}

// pyFunctions returns every top-level/method `def` in the file.
func pyFunctions(src string) []pyFuncInfo {
	var out []pyFuncInfo
	for _, m := range rePyDef.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, pyFuncInfo{
			name:      src[m[2]:m[3]],
			line:      lineOf(src, m[0]),
			parenOpen: m[1] - 1, // m[1] is just past '('
		})
	}
	return out
}

// pyInjectFunctions returns each `def` that is immediately preceded (modulo
// other decorators) by an `@inject` decorator.
func pyInjectFunctions(src string) []pyFuncInfo {
	var out []pyFuncInfo
	for _, fn := range pyFunctions(src) {
		// Look back up to ~300 bytes before the def for an `@inject` line that
		// is part of the contiguous decorator block (no blank line between).
		lo := 0
		// Find start of the def's line.
		defLineStart := strings.LastIndexByte(src[:fn.parenOpen], '\n') + 1
		// Walk back to the def keyword line start.
		region := src[:defLineStart]
		if len(region) > 400 {
			lo = len(region) - 400
		}
		prefix := region[lo:]
		if pyHasContiguousInject(prefix) {
			out = append(out, fn)
		}
	}
	return out
}

// pyHasContiguousInject reports whether the trailing contiguous decorator block
// of prefix contains an `@inject` decorator.
func pyHasContiguousInject(prefix string) bool {
	lines := strings.Split(prefix, "\n")
	// Drop the final partial line (the def line itself isn't included; prefix
	// ends at the def line start, so the last element is "").
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			// A blank line breaks the contiguous decorator block only if we've
			// already seen a decorator; trailing empties are skipped.
			continue
		}
		if strings.HasPrefix(t, "@") {
			if rePyInjectDecorator.MatchString(lines[i]) {
				return true
			}
			continue
		}
		// First non-decorator, non-blank line ends the block.
		break
	}
	return false
}

// pyDependsProvider resolves the provider identifier from a single parameter
// declaration. Returns the dotted identifier inside `Depends(<id>)`; for a bare
// `Depends()` it falls back to the parameter's type annotation. Returns "" for
// non-Depends parameters or unresolved dynamic expressions.
func pyDependsProvider(param string) string {
	m := rePyDependsCall.FindStringSubmatch(param)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return pyLeafIdent(m[1])
	}
	// Bare Depends() → use the type annotation `name: Type = Depends()`.
	colon := strings.IndexByte(param, ':')
	if colon < 0 {
		return ""
	}
	ann := param[colon+1:]
	if eq := strings.IndexByte(ann, '='); eq >= 0 {
		ann = ann[:eq]
	}
	return pyLeafIdent(strings.TrimSpace(ann))
}

// pyProvideToken resolves the provider token from a `Provide[<chain>]` dotted
// chain, returning the leaf attribute (the container attribute name), which is
// the BINDS token. `Container.service` → service; `C.sub.service` → service.
func pyProvideToken(chain string) string {
	chain = strings.TrimSpace(chain)
	if chain == "" {
		return ""
	}
	parts := strings.Split(chain, ".")
	leaf := parts[len(parts)-1]
	// A subscripted / called leaf is dynamic — skip.
	if strings.ContainsAny(leaf, "[]() ") || leaf == "" {
		return ""
	}
	return leaf
}

// pyLeafIdent returns the leaf identifier of a dotted chain, rejecting dynamic
// expressions (parens, subscripts, operators) so only statically-resolvable
// names survive. `module.get_db` → get_db; `getattr(m,n)` → "".
func pyLeafIdent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, "()[]{}+*/\"'") {
		return ""
	}
	if i := strings.IndexAny(s, " \t,="); i >= 0 {
		s = s[:i]
	}
	if idx := strings.LastIndexByte(s, '.'); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if c := s[0]; !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return ""
	}
	return s
}

// pyContainerInfo describes a DeclarativeContainer class body span.
type pyContainerInfo struct {
	name      string
	bodyStart int
	bodyEnd   int
}

// pyContainers returns every dependency-injector container class with its body
// byte span (from just after the `:` to the next top-level dedent, approximated
// by the next class/def at column 0 or EOF).
func pyContainers(src string) []pyContainerInfo {
	var out []pyContainerInfo
	matches := rePyContainerClass.FindAllStringSubmatchIndex(src, -1)
	for _, m := range matches {
		name := src[m[2]:m[3]]
		bodyStart := m[1]
		bodyEnd := pyClassBodyEnd(src, bodyStart)
		out = append(out, pyContainerInfo{name: name, bodyStart: bodyStart, bodyEnd: bodyEnd})
	}
	return out
}

// rePyTopLevel matches a column-0 `class`/`def`/`@` that ends an indented class
// body.
var rePyTopLevel = regexp.MustCompile(`(?m)^(?:class|def|@)\b`)

// pyClassBodyEnd returns the byte offset where the class body that starts at
// `start` ends — the next column-0 class/def/decorator, or EOF.
func pyClassBodyEnd(src string, start int) int {
	rest := src[start:]
	loc := rePyTopLevel.FindStringIndex(rest)
	if loc == nil {
		return len(src)
	}
	return start + loc[0]
}

// pyBalancedParen returns the substring inside the balanced parenthesis pair
// whose '(' is at index open (src[open]=='('). Returns "" on imbalance.
func pyBalancedParen(src string, open int) string {
	if open < 0 || open >= len(src) || src[open] != '(' {
		return ""
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return ""
}

// pySplitParams splits a parameter list on top-level commas, respecting nested
// brackets so `Depends(x)` / `List[int]` aren't split mid-parameter.
func pySplitParams(params string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
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
