// dotnet_di.go — .NET dependency-injection GRAPH extractor (#3699, parent
// #3628 area #5).
//
// aspnet_core.go already records service registrations as standalone
// SCOPE.Pattern entities ("di:Scoped:IFoo"), but it does NOT emit the DI GRAPH:
// no interface→implementation BINDS edge and no constructor INJECTED_INTO edge.
// The rewrite-parity oracle needs both to resolve "what impl backs this
// interface token" and "what service does this class depend on". This extractor
// adds that graph for the Microsoft.Extensions.DependencyInjection container,
// mirroring the NestJS DI work (#3649) and the MAUI DI edges (mobile_platform.go)
// edge-for-edge:
//
//	BINDS         : services.AddSingleton<IFoo, Foo>() / AddScoped / AddTransient
//	                → IFoo BINDS Foo, with lifetime=Singleton|Scoped|Transient.
//	                FromID = the interface/token, ToID = the implementation,
//	                matching the {provide: TOKEN, useClass: Impl} NestJS shape and
//	                the MAUI iface→impl edge. The two-type-arg form is the binding;
//	                a single-type-arg AddScoped<Foo>() is a self-registration
//	                (token == impl), still emitted as BINDS with
//	                binding_kind=self.
//
//	INJECTED_INTO : a constructor parameter of a DI-managed class (a controller,
//	                a registered implementation, or any class taking interface/
//	                service-typed ctor params) → the param type INJECTED_INTO the
//	                class. FromID = injected service, ToID = consumer class,
//	                mirroring nestjs_di.go. Primitive / option / config params are
//	                rejected so no spurious edge is produced.
//
// Honest-partial: the BINDS impl and the INJECTED_INTO provider resolve to the
// concrete class only via the cross-file resolver pass (the registration site
// and the implementation class usually live in different files); factory-style
// registrations (AddScoped<IFoo>(sp => new Foo(...))) and open-generic binds
// keep the di_binding cell at "partial".
package csharp

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_csharp_dotnet_di", &dotnetDIExtractor{})
}

type dotnetDIExtractor struct{}

func (e *dotnetDIExtractor) Language() string { return "custom_csharp_dotnet_di" }

var (
	// reDotnetDITwoArg matches services.AddSingleton<IFoo, Foo>() and the
	// keyed/try variants (TryAddScoped, AddKeyedSingleton). Group 1 = lifetime,
	// group 2 = interface/token, group 3 = implementation.
	reDotnetDITwoArg = regexp.MustCompile(
		`(?:Try)?Add(?:Keyed)?(Singleton|Scoped|Transient)\s*<\s*([\w.]+)\s*,\s*([\w.]+)\s*>`)

	// reDotnetDIOneArg matches the self-registration form AddScoped<Foo>().
	// Group 1 = lifetime, group 2 = service type.
	reDotnetDIOneArg = regexp.MustCompile(
		`(?:Try)?Add(?:Keyed)?(Singleton|Scoped|Transient)\s*<\s*([\w.]+)\s*>`)

	// reDotnetDIRuntimeType matches the typeof()-based runtime form
	// services.AddScoped(typeof(IFoo), typeof(Foo)). Group 1 = lifetime,
	// group 2 = interface, group 3 = implementation.
	reDotnetDIRuntimeType = regexp.MustCompile(
		`(?:Try)?Add(?:Keyed)?(Singleton|Scoped|Transient)\s*\(\s*typeof\s*\(\s*([\w.]+)\s*\)\s*,\s*typeof\s*\(\s*([\w.]+)\s*\)`)

	// reDotnetClassDecl matches a class declaration with an optional base list.
	// Group 1 = class name, group 2 = base list (after ':'), may be empty.
	reDotnetClassDecl = regexp.MustCompile(
		`(?m)^\s*(?:public|internal|sealed|abstract|partial|static|\s)*` +
			`class\s+([A-Za-z_]\w*)(?:\s*:\s*([\w.,<>\s]+?))?\s*(?:\{|where|$)`)
)

// dotnetDIPrimitiveParam reports whether a constructor parameter type should not
// produce an injection edge. Covers primitives, framework option/config types,
// and logging generics that are infrastructure rather than app services.
func dotnetDIPrimitiveParam(t string) bool {
	if csharpPrimitives[t] {
		return true
	}
	switch t {
	case "IConfiguration", "IServiceProvider", "CancellationToken",
		"IHostEnvironment", "IWebHostEnvironment", "IMemoryCache":
		return true
	}
	// IOptions<...> / ILogger<...> / Lazy<...> infrastructure wrappers.
	for _, w := range []string{"IOptions", "IOptionsMonitor", "ILogger", "Lazy"} {
		if strings.HasPrefix(t, w+"<") || t == w {
			return true
		}
	}
	return false
}

func (e *dotnetDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "csharp" {
		return nil, nil
	}
	src := string(file.Content)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// ── 1. Two-type-arg binds: IFoo BINDS Foo (with lifetime) ───────────────
	// Track spans already consumed by the two-arg form so the one-arg regex
	// (a prefix-superset) does not re-emit them as self-registrations.
	twoArgSpans := map[int]bool{}
	emitBind := func(iface, impl, lifetime, kind string, line int) {
		iface = leafType(iface)
		impl = leafType(impl)
		if iface == "" || impl == "" {
			return
		}
		ent := makeEntity("di:"+iface+"->"+impl, "SCOPE.Component", "di_binding",
			file.Path, "csharp", line)
		setProps(&ent, "framework", "dotnet_di", "provenance", "INFERRED_FROM_DOTNET_DI_BINDING",
			"interface", iface, "implementation", impl, "lifetime", lifetime, "binding_kind", kind)
		ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
			ToID: "impl:" + impl,
			Kind: string(types.RelationshipKindBinds),
			Properties: types.Props{
				{K: "binding_kind", V: kind},
				{K: "framework", V: "dotnet_di"},
				{K: "implementation", V: impl},
				{K: "interface", V: iface},
				{K: "lifetime", V: lifetime},
				{K: "line", V: itoa(line)},
			},
		})
		add(ent)
	}

	for _, m := range reDotnetDITwoArg.FindAllStringSubmatchIndex(src, -1) {
		twoArgSpans[m[0]] = true
		emitBind(src[m[4]:m[5]], src[m[6]:m[7]], src[m[2]:m[3]], "interface_impl", lineOf(src, m[0]))
	}
	for _, m := range reDotnetDIRuntimeType.FindAllStringSubmatchIndex(src, -1) {
		emitBind(src[m[4]:m[5]], src[m[6]:m[7]], src[m[2]:m[3]], "typeof", lineOf(src, m[0]))
	}

	// ── 2. Self-registration binds: AddScoped<Foo>() → Foo BINDS Foo ─────────
	for _, m := range reDotnetDIOneArg.FindAllStringSubmatchIndex(src, -1) {
		// Skip if this match is the prefix of a consumed two-arg registration
		// (same start offset) — re-check for a comma in the type-arg list.
		seg := src[m[0]:]
		if commaInDotnetTypeArgs(seg) {
			continue
		}
		svc := leafType(src[m[4]:m[5]])
		lifetime := src[m[2]:m[3]]
		if svc == "" {
			continue
		}
		emitBind(svc, svc, lifetime, "self", lineOf(src, m[0]))
	}

	// ── 3. Constructor injection: service INJECTED_INTO the class ────────────
	for _, c := range dotnetClasses(src) {
		params, ok := dotnetConstructorParams(src, c.name, c.bodyStart, c.bodyEnd)
		if !ok {
			continue
		}
		for _, p := range splitDotnetParams(params) {
			t := dotnetParamType(p)
			if t == "" || dotnetDIPrimitiveParam(t) {
				continue
			}
			provider := leafType(t)
			if provider == "" || provider == c.name {
				continue
			}
			// Carrier = provider (FromID); ToID = consumer class, matching the
			// NestJS FromID=provider/ToID=consumer convention.
			ent := makeEntity(provider, "SCOPE.Class", "", file.Path, "csharp", c.line)
			setProps(&ent, "framework", "dotnet_di", "provenance", "INFERRED_FROM_DOTNET_DI_PROVIDER",
				"di_role", "provider")
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID: "consumer:" + c.name,
				Kind: string(types.RelationshipKindInjectedInto),
				Properties: types.Props{
					{K: "consumer", V: c.name},
					{K: "framework", V: "dotnet_di"},
					{K: "line", V: itoa(c.line)},
					{K: "provider", V: provider},
					{K: "via", V: "dotnet_constructor"},
				},
			})
			add(ent)
		}
	}

	return entities, nil
}

// ── parsing helpers ────────────────────────────────────────────────────────────

// dotnetClass is a class declaration span.
type dotnetClass struct {
	name      string
	line      int
	bodyStart int
	bodyEnd   int
}

// dotnetClasses returns every class declaration with its body span.
func dotnetClasses(src string) []dotnetClass {
	var out []dotnetClass
	for _, m := range reDotnetClassDecl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Scan for the body brace from just after the class NAME (m[3]) rather
		// than the full match end (m[1]): the decl regex consumes the opening
		// `{` itself, so starting at m[1] would skip past the class body.
		bs := indexBrace(src, m[3])
		if bs < 0 {
			continue
		}
		be := matchBrace(src, bs)
		out = append(out, dotnetClass{name: name, line: lineOf(src, m[0]), bodyStart: bs, bodyEnd: be})
	}
	return out
}

// dotnetConstructorParams finds the parameter list of a constructor named cls
// within [bodyStart,bodyEnd). Returns (params, true) when found.
func dotnetConstructorParams(src, cls string, bodyStart, bodyEnd int) (string, bool) {
	body := src[bodyStart:bodyEnd]
	re := regexp.MustCompile(`(?:public|internal|protected|private)\s+` + regexp.QuoteMeta(cls) + `\s*\(`)
	loc := re.FindStringIndex(body)
	if loc == nil {
		return "", false
	}
	open := bodyStart + loc[1] - 1
	return balancedParens(src, open), true
}

// dotnetParamType returns the service type of a single C# constructor parameter
// chunk (e.g. "IUserRepo repo" → IUserRepo, "[FromKeyedServices(\"x\")] IFoo f"
// → IFoo). Returns "" when the chunk has no usable type token.
func dotnetParamType(param string) string {
	p := strings.TrimSpace(param)
	if p == "" {
		return ""
	}
	// Strip leading [Attribute(...)] blocks.
	for strings.HasPrefix(p, "[") {
		depth := 0
		i := 0
		for ; i < len(p); i++ {
			if p[i] == '[' {
				depth++
			} else if p[i] == ']' {
				depth--
				if depth == 0 {
					i++
					break
				}
			}
		}
		p = strings.TrimSpace(p[i:])
	}
	// Strip 'in'/'ref'/'out'/'params' modifiers.
	for _, mod := range []string{"in ", "ref ", "out ", "params ", "readonly "} {
		p = strings.TrimPrefix(p, mod)
	}
	p = strings.TrimSpace(p)
	// Drop a default value (= ...).
	if i := strings.IndexByte(p, '='); i >= 0 {
		p = strings.TrimSpace(p[:i])
	}
	// type is everything up to the last space (the param name follows).
	idx := strings.LastIndexByte(p, ' ')
	if idx < 0 {
		return ""
	}
	t := strings.TrimSpace(p[:idx])
	return t
}

// leafType strips a namespace qualifier (a.b.Foo → Foo) and a nullable marker.
func leafType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimSuffix(t, "?")
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		t = t[i+1:]
	}
	// Drop generic params for the leaf identifier.
	if i := strings.IndexByte(t, '<'); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// splitDotnetParams splits a C# parameter list on top-level commas, respecting
// nested <>, (), [], {}.
func splitDotnetParams(params string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case '<', '(', '[', '{':
			depth++
		case '>', ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, params[start:i])
				start = i + 1
			}
		}
	}
	if strings.TrimSpace(params[start:]) != "" {
		out = append(out, params[start:])
	}
	return out
}

// indexBrace returns the index of the next '{' at or after from, or -1 if a ';'
// terminates the declaration first.
func indexBrace(src string, from int) int {
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '{':
			return i
		case ';':
			return -1
		}
	}
	return -1
}

// matchBrace returns the index just past the '}' matching the '{' at open.
func matchBrace(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

// balancedParens returns the substring strictly inside the balanced () pair that
// opens at open (src[open]=='('), or "" on imbalance.
func balancedParens(src string, open int) string {
	if open < 0 || open >= len(src) || src[open] != '(' {
		return ""
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return ""
}

// commaInDotnetTypeArgs reports whether the matched Add...<...>( segment carries
// a top-level comma in its first <...> type-arg list (the two-arg form).
func commaInDotnetTypeArgs(seg string) bool {
	lt := strings.IndexByte(seg, '<')
	if lt < 0 {
		return false
	}
	depth := 0
	for i := lt; i < len(seg); i++ {
		switch seg[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return false
			}
		case ',':
			if depth == 1 {
				return true
			}
		}
	}
	return false
}
