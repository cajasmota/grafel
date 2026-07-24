package php

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_php_di_graph", &phpDIExtractor{})
}

// phpDIExtractor extracts dependency-injection edges for Laravel and Symfony
// (#3628 area #5), matching the NestJS/Angular FROM/TO contract:
//
//	BINDS         : token (interface/abstract) → implementation. FromID=token,
//	                ToID=impl.
//	INJECTED_INTO : a constructor-injected type → the class that declares the
//	                constructor. FromID=injected type, ToID=consumer class.
//
// Laravel:
//   - `$this->app->bind(PaymentInterface::class, StripePayment::class)` and the
//     `singleton(...)` / `app()->bind(...)` / `App::bind(...)` variants →
//     BINDS(PaymentInterface → StripePayment). A closure-form binding
//     (`bind(X::class, function () { ... })`) has no statically-resolvable impl,
//     so it is skipped (honest-partial).
//   - A class constructor with type-hinted parameters
//     (`public function __construct(PaymentInterface $p)`) → INJECTED_INTO(
//     PaymentInterface → <enclosing class>). Laravel autowires these from the
//     container.
//
// Symfony:
//   - `services.yaml`: an explicit service alias `App\Foo\Bar: '@App\Foo\Baz'`
//     or `alias: ...` → BINDS(Bar → Baz). With `autowire: true`, constructor
//     type-hints are resolved the same way as Laravel.
//   - Constructor type-hints in PHP classes → INJECTED_INTO (autowiring).
//
// Honest-partial: scalar/built-in type-hints (string, int, array, …) and
// untyped parameters yield no edge; closures and dynamic bindings are skipped.
type phpDIExtractor struct{}

func (e *phpDIExtractor) Language() string { return "custom_php_di_graph" }

var (
	// reLaravelBindClass captures container bindings where both arguments are
	// `Interface::class` / `Impl::class`. Group 1 = bind kind, 2 = abstract
	// (token), 3 = concrete (impl).
	reLaravelBindClass = regexp.MustCompile(
		`(?:\$this->app|app\(\)|App|\$app)->(bind|singleton|scoped)\s*\(\s*([A-Za-z_\\][\w\\]*)::class\s*,\s*([A-Za-z_\\][\w\\]*)::class`)

	// rePhpClassDecl captures a class declaration head; group 1 = class name.
	rePhpClassDecl = regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?class\s+(\w+)`)

	// rePhpConstructor captures the `__construct(` head; the byte just past the
	// open paren is used to balanced-scan the parameter list.
	rePhpConstructor = regexp.MustCompile(`function\s+__construct\s*\(`)

	// reSymServiceAlias captures a YAML service alias line:
	//   App\Service\Bar: '@App\Service\Baz'
	// Group 1 = service id (token), group 2 = referenced service (impl).
	reSymServiceAlias = regexp.MustCompile(
		`(?m)^\s{2,}([A-Za-z_\\][\w\\]*)\s*:\s*['"]?@([A-Za-z_\\][\w\\]*)['"]?\s*$`)

	// reSymAliasKey captures the explicit `alias:` form under a service id.
	reSymAliasKey = regexp.MustCompile(`(?m)^\s+alias\s*:\s*['"]?@?([A-Za-z_\\][\w\\]*)['"]?`)
)

func (e *phpDIExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.php_di_graph")
	_, span := tracer.Start(ctx, "custom.php_di_graph")
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
	// MergeWithCustom never replaces a rich base class/service entity. The
	// semantic token/impl/consumer names live on the relationship FromID/ToID.
	addEdge := func(subtype string, line int, props map[string]string, rel types.RelationshipRecord) {
		owner := "di:" + rel.Kind + ":" + rel.FromID + "->" + rel.ToID + "@" + strconv.Itoa(line)
		ent := makeEntity(owner, "SCOPE.Pattern", subtype, path, file.Language, line)
		setPropsMap(&ent, props)
		ent.Relationships = append(ent.Relationships, rel)
		out = append(out, ent)
		edgeCount++
	}

	lower := strings.ToLower(path)
	isYAML := strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")

	if isYAML {
		// ---- Symfony services.yaml: alias → impl BINDS -------------------
		for _, m := range reSymServiceAlias.FindAllStringSubmatchIndex(src, -1) {
			token := phpLeafType(src[m[2]:m[3]])
			impl := phpLeafType(src[m[4]:m[5]])
			if token == "" || impl == "" || token == impl {
				continue
			}
			line := lineOf(src, m[0])
			addEdge("di_binding", line, map[string]string{
				"framework": "symfony",
				"token":     token,
				"impl":      impl,
				"via":       "symfony_services_yaml",
			}, types.RelationshipRecord{
				FromID: token, ToID: impl,
				Kind: string(types.RelationshipKindBinds),
				Properties: types.Props{
					{K: "framework", V: "symfony"},
					{K: "token", V: token},
					{K: "via", V: "symfony_services_yaml"},
				},
			})
		}
		span.SetAttributes(attribute.Int("di_edge_count", edgeCount))
		return out, nil
	}

	// ---- Laravel container bindings: abstract → concrete BINDS -----------
	for _, m := range reLaravelBindClass.FindAllStringSubmatchIndex(src, -1) {
		bindKind := src[m[2]:m[3]]
		token := phpLeafType(src[m[4]:m[5]])
		impl := phpLeafType(src[m[6]:m[7]])
		if token == "" || impl == "" {
			continue
		}
		line := lineOf(src, m[0])
		addEdge("di_binding", line, map[string]string{
			"framework": "laravel",
			"bind_kind": bindKind,
			"token":     token,
			"impl":      impl,
			"via":       "laravel_container_bind",
		}, types.RelationshipRecord{
			FromID: token, ToID: impl,
			Kind: string(types.RelationshipKindBinds),
			Properties: types.Props{
				{K: "bind_kind", V: bindKind},
				{K: "framework", V: "laravel"},
				{K: "token", V: token},
				{K: "via", V: "laravel_container_bind"},
			},
		})
	}

	// ---- Constructor injection (Laravel + Symfony autowiring) ------------
	// Resolve the enclosing class for each __construct and emit INJECTED_INTO
	// for every class-typed parameter.
	for _, cls := range phpClasses(src) {
		ctorOpen := phpConstructorParen(src, cls.bodyStart, cls.bodyEnd)
		if ctorOpen < 0 {
			continue
		}
		params := phpBalancedParen(src, ctorOpen)
		if params == "" {
			continue
		}
		for _, p := range phpSplitParams(params) {
			typ := phpParamType(p)
			if typ == "" {
				continue
			}
			line := lineOf(src, ctorOpen)
			addEdge("di_consumer", line, map[string]string{
				"framework": "php-di",
				"consumer":  cls.name,
				"provider":  typ,
				"via":       "php_constructor",
			}, types.RelationshipRecord{
				FromID: typ, ToID: cls.name,
				Kind: string(types.RelationshipKindInjectedInto),
				Properties: types.Props{
					{K: "consumer", V: cls.name},
					{K: "framework", V: "php-di"},
					{K: "provider", V: typ},
					{K: "via", V: "php_constructor"},
				},
			})
		}
	}

	span.SetAttributes(attribute.Int("di_edge_count", edgeCount))
	return out, nil
}

// setPropsMap copies a map of properties onto an entity.
func setPropsMap(e *types.EntityRecord, props map[string]string) {
	for k, v := range props {
		e.Properties[k] = v
	}
}

// phpClassInfo is a class declaration with its body byte span.
type phpClassInfo struct {
	name      string
	bodyStart int
	bodyEnd   int
}

// phpClasses returns each class with the byte span of its `{...}` body so the
// constructor scan stays inside the owning class.
func phpClasses(src string) []phpClassInfo {
	var out []phpClassInfo
	for _, m := range rePhpClassDecl.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		brace := strings.IndexByte(src[m[1]:], '{')
		if brace < 0 {
			continue
		}
		bodyStart := m[1] + brace
		bodyEnd := phpMatchBrace(src, bodyStart)
		out = append(out, phpClassInfo{name: name, bodyStart: bodyStart, bodyEnd: bodyEnd})
	}
	return out
}

// phpConstructorParen returns the byte index of the '(' of a __construct found
// within [bodyStart, bodyEnd), or -1.
func phpConstructorParen(src string, bodyStart, bodyEnd int) int {
	if bodyStart < 0 || bodyEnd > len(src) || bodyStart >= bodyEnd {
		return -1
	}
	region := src[bodyStart:bodyEnd]
	loc := rePhpConstructor.FindStringIndex(region)
	if loc == nil {
		return -1
	}
	return bodyStart + loc[1] - 1 // index of '('
}

// phpParamType extracts the class type-hint of a single constructor parameter,
// rejecting scalar/built-in hints and untyped parameters. Handles nullable
// (`?Foo`), promoted-property modifiers (`private Foo $f`), and namespaces
// (`App\Foo\Bar $b` → Bar).
func phpParamType(param string) string {
	p := strings.TrimSpace(param)
	if p == "" {
		return ""
	}
	// A parameter must have a `$var`; the type is the token immediately before
	// the first `$`.
	dollar := strings.IndexByte(p, '$')
	if dollar < 0 {
		return ""
	}
	head := strings.TrimSpace(p[:dollar])
	if head == "" {
		return ""
	}
	fields := strings.Fields(head)
	// The type hint is the LAST whitespace-separated token before `$var`
	// (visibility/readonly modifiers precede it).
	typeTok := fields[len(fields)-1]
	typeTok = strings.TrimPrefix(typeTok, "?")
	// Union/intersection types — take the first member.
	if i := strings.IndexAny(typeTok, "|&"); i >= 0 {
		typeTok = typeTok[:i]
	}
	return phpLeafType(typeTok)
}

// phpLeafType normalises a (possibly namespaced) type to its leaf class name,
// rejecting scalar/built-in types and non-class shapes.
func phpLeafType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "\\")
	t = strings.TrimPrefix(t, "?")
	if t == "" {
		return ""
	}
	if idx := strings.LastIndex(t, "\\"); idx >= 0 {
		t = t[idx+1:]
	}
	switch strings.ToLower(t) {
	case "string", "int", "integer", "float", "bool", "boolean", "array",
		"object", "mixed", "void", "callable", "iterable", "self", "static",
		"parent", "null", "false", "true", "never":
		return ""
	}
	if t == "" {
		return ""
	}
	c := t[0]
	if !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return ""
	}
	return t
}

// phpMatchBrace returns the index just past the '}' that closes the '{' at
// `open`, or len(src) on imbalance.
func phpMatchBrace(src string, open int) int {
	if open < 0 || open >= len(src) || src[open] != '{' {
		return len(src)
	}
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

// phpBalancedParen returns the substring inside the balanced '(' at `open`.
func phpBalancedParen(src string, open int) string {
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

// phpSplitParams splits a parameter list on top-level commas.
func phpSplitParams(params string) []string {
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
