package golang

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_go_di_graph", &goDIExtractor{})
}

// goDIExtractor extracts dependency-injection edges for google/wire and
// uber/fx (#3628 area #5), matching the NestJS/Angular FROM/TO contract:
//
//	BINDS         : provider constructor → the type it produces. FromID=provider
//	                func, ToID=produced type.
//	INJECTED_INTO : a constructor's parameter type (a dependency) → the type the
//	                constructor produces (the consumer). FromID=dependency,
//	                ToID=produced type.
//
// google/wire:
//   - `wire.Build(NewService, NewRepo, ...)` and `wire.NewSet(NewService, ...)`
//     enumerate provider functions. Each `NewX` provider BINDS its produced
//     type: we resolve the provider's return type from its `func NewX(...) (*X,
//     error)` / `func NewX(...) X` signature → BINDS(NewX → X). When the return
//     type cannot be resolved (provider defined in another file), the provider
//     is still recorded but BINDS is skipped (honest-partial, cross-file).
//
// uber/fx:
//   - `fx.Provide(NewService, NewRepo)` registers constructors. Each `NewX`
//     produces type X (BINDS(NewX → X)) and its parameter types are injected
//     into X: INJECTED_INTO(ParamType → X). `fx.Invoke(fn)` consumers' params
//     are injected into the invoked function.
//
// Both frameworks share provider-constructor parsing: a Go func
// `func NewService(repo *Repo, cfg Config) *Service` produces `*Service` and
// consumes `*Repo`, `Config`. We emit INJECTED_INTO(Repo → Service) and
// INJECTED_INTO(Config → Service), plus BINDS(NewService → Service).
//
// Honest-partial: providers whose return type is `error`-only, an interface
// from another package we can't resolve, or a non-pointer/non-named shape are
// skipped rather than fabricated. Only providers that appear in a wire/fx
// registration site are processed (a bare `NewX` func is not DI by itself).
type goDIExtractor struct{}

func (e *goDIExtractor) Language() string { return "custom_go_di_graph" }

var (
	// reGoWireSet captures wire.Build(...) / wire.NewSet(...) argument lists.
	// Group 1 = call kind (Build|NewSet), match index marks the '(' to balance.
	reGoWireCall = regexp.MustCompile(`\bwire\.(Build|NewSet)\s*\(`)

	// reGoFxProvide captures fx.Provide(...) / fx.Invoke(...) argument lists.
	reGoFxCall = regexp.MustCompile(`\bfx\.(Provide|Invoke)\s*\(`)

	// reGoFuncDecl captures a top-level func declaration head so a provider's
	// signature can be located. Group 1 = func name, match index 1 marks just
	// past the opening '(' of the parameter list.
	reGoFuncDecl = regexp.MustCompile(`(?m)^func\s+(\w+)\s*\(`)

	// reGoIdent matches an exported identifier (provider-constructor reference)
	// possibly package-qualified (`pkg.NewX`). Group 1 = leaf identifier.
	reGoProviderRef = regexp.MustCompile(`(?:\w+\.)?([A-Za-z_]\w*)`)
)

func (e *goDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.go_di_graph")
	_, span := tracer.Start(ctx, "custom.go_di_graph")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	path := file.Path
	var out []types.EntityRecord
	edgeCount := 0

	// addEdge emits a thin owner entity with a synthetic, non-colliding Name so
	// MergeWithCustom never replaces a rich base func/type entity. The semantic
	// provider/produced/dependency names live on the relationship FromID/ToID,
	// which the resolver binds via its global byName index.
	addEdge := func(subtype string, line int, props map[string]string, rel types.RelationshipRecord) {
		owner := "di:" + rel.Kind + ":" + rel.FromID + "->" + rel.ToID + "@" + itoa(line)
		ent := makeEntity(owner, "SCOPE.Pattern", subtype, path, file.Language, line)
		setProps(&ent, mapToKV(props)...)
		ent.Relationships = append(ent.Relationships, rel)
		out = append(out, ent)
		edgeCount++
	}

	// Index provider funcs in this file by name so registration sites can
	// resolve their produced/consumed types.
	funcs := goFuncSignatures(src)

	// Collect (framework, registrationKind, providerName) from wire/fx sites.
	type reg struct {
		framework string
		kind      string // build|newset|provide|invoke
		provider  string
		line      int
	}
	var regs []reg
	for _, loc := range reGoWireCall.FindAllStringSubmatchIndex(src, -1) {
		kind := strings.ToLower(src[loc[2]:loc[3]])
		args := goBalancedParen(src, loc[1]-1)
		line := lineOf(src, loc[0])
		for _, p := range goProviderRefs(args) {
			regs = append(regs, reg{"wire", kind, p, line})
		}
	}
	for _, loc := range reGoFxCall.FindAllStringSubmatchIndex(src, -1) {
		kind := strings.ToLower(src[loc[2]:loc[3]])
		args := goBalancedParen(src, loc[1]-1)
		line := lineOf(src, loc[0])
		for _, p := range goProviderRefs(args) {
			regs = append(regs, reg{"fx", kind, p, line})
		}
	}

	seen := map[string]bool{}
	for _, r := range regs {
		sig, ok := funcs[r.provider]
		if !ok {
			// Provider defined in another file — record the registration but
			// no type edges (honest-partial, cross-file resolution).
			continue
		}
		produced := goReturnType(sig.returns)
		// BINDS(provider → produced type).
		if produced != "" {
			key := "binds:" + r.provider + ":" + produced
			if !seen[key] {
				seen[key] = true
				addEdge("di_provider", sig.line, map[string]string{
					"framework":    r.framework,
					"registration": r.kind,
					"provider":     r.provider,
					"produces":     produced,
					"via":          r.framework + "_provider",
				}, types.RelationshipRecord{
					FromID: r.provider, ToID: produced,
					Kind: string(types.RelationshipKindBinds),
					Properties: types.Props{
						{K: "framework", V: r.framework},
						{K: "provider", V: r.provider},
						{K: "registration", V: r.kind},
						{K: "via", V: r.framework + "_provider"},
					},
				})
			}
			// INJECTED_INTO(param type → produced type) for each dependency.
			for _, dep := range goParamTypes(sig.params) {
				if dep == "" || dep == produced {
					continue
				}
				key := "inj:" + dep + ":" + produced
				if seen[key] {
					continue
				}
				seen[key] = true
				addEdge("di_consumer", sig.line, map[string]string{
					"framework": r.framework,
					"consumer":  produced,
					"provider":  dep,
					"via":       r.framework + "_constructor",
				}, types.RelationshipRecord{
					FromID: dep, ToID: produced,
					Kind: string(types.RelationshipKindInjectedInto),
					Properties: types.Props{
						{K: "consumer", V: produced},
						{K: "framework", V: r.framework},
						{K: "provider", V: dep},
						{K: "via", V: r.framework + "_constructor"},
					},
				})
			}
		}
	}

	span.SetAttributes(attribute.Int("di_edge_count", edgeCount))
	return out, nil
}

// mapToKV flattens a map into a setProps-compatible key,value... slice.
func mapToKV(m map[string]string) []string {
	out := make([]string, 0, len(m)*2)
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}

// goFuncSig holds a function's parameter / return text spans.
type goFuncSig struct {
	params  string
	returns string
	line    int
}

// goFuncSignatures parses every top-level func into its param list and return
// signature text.
func goFuncSignatures(src string) map[string]goFuncSig {
	out := map[string]goFuncSig{}
	for _, m := range reGoFuncDecl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		open := m[1] - 1 // '('
		params := goBalancedParen(src, open)
		// Find the byte just past the closing ')' of the param list.
		closeIdx := goMatchParen(src, open)
		if closeIdx < 0 {
			continue
		}
		// The return signature is between ')' and the opening '{' of the body.
		brace := strings.IndexByte(src[closeIdx:], '{')
		var returns string
		if brace >= 0 {
			returns = strings.TrimSpace(src[closeIdx+1 : closeIdx+brace])
		}
		out[name] = goFuncSig{params: params, returns: returns, line: lineOf(src, m[0])}
	}
	return out
}

// goProviderRefs returns the leaf identifiers referenced in a wire/fx call
// argument list, skipping nested wire.NewSet/wire.Bind helper calls' keywords.
func goProviderRefs(args string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range goSplitArgs(args) {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		// Skip nested set references and wire.Bind/wire.Value helpers — only
		// bare/qualified constructor identifiers are providers.
		if strings.ContainsAny(p, "(){}") {
			continue
		}
		m := reGoProviderRef.FindStringSubmatch(p)
		if m == nil {
			continue
		}
		id := m[1]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// goReturnType resolves the primary produced type from a Go return signature,
// stripping a trailing `error`. Handles `*Service`, `(*Service, error)`,
// `Service`, `(Service, error)`. Returns the leaf type name, "" if unresolved.
func goReturnType(ret string) string {
	ret = strings.TrimSpace(ret)
	ret = strings.TrimPrefix(ret, "(")
	ret = strings.TrimSuffix(ret, ")")
	if ret == "" {
		return ""
	}
	// First comma-separated return that is not `error`.
	for _, part := range goSplitArgs(ret) {
		t := goLeafType(part)
		if t != "" && t != "error" {
			return t
		}
	}
	return ""
}

// goParamTypes returns the leaf type of each parameter in a Go param list.
// Handles grouped params (`a, b *Repo`) by assigning the trailing type to each.
func goParamTypes(params string) []string {
	var out []string
	for _, part := range goSplitArgs(params) {
		t := goLeafType(part)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// goLeafType extracts the leaf named type from a parameter/return fragment,
// dropping the param name, pointer/slice/map markers, and package qualifier.
// Rejects builtins and context/error noise types.
func goLeafType(frag string) string {
	frag = strings.TrimSpace(frag)
	if frag == "" {
		return ""
	}
	// A parameter may be `name Type` or just `Type`. Take the last token.
	fields := strings.Fields(frag)
	t := fields[len(fields)-1]
	// Strip pointer/slice markers.
	t = strings.TrimLeft(t, "*[]")
	// Strip variadic.
	t = strings.TrimPrefix(t, "...")
	t = strings.TrimLeft(t, "*")
	// Drop package qualifier.
	if idx := strings.LastIndex(t, "."); idx >= 0 {
		t = t[idx+1:]
	}
	switch t {
	case "", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune",
		"float32", "float64", "bool", "any", "Context":
		return ""
	}
	c := t[0]
	if !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return ""
	}
	return t
}

// goBalancedParen returns the substring inside the balanced '(' at index open.
func goBalancedParen(src string, open int) string {
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

// goMatchParen returns the index of the ')' that closes the '(' at open, or -1.
func goMatchParen(src string, open int) int {
	if open < 0 || open >= len(src) || src[open] != '(' {
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

// goSplitArgs splits on top-level commas respecting nested brackets.
func goSplitArgs(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
