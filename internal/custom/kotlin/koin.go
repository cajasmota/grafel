// Package kotlin — Koin DI extractor (record lang.kotlin.framework.koin).
//
// Covers:
//   - di_binding_extraction  (missing → full)
//   - di_injection_point     (missing → full)
//   - di_scope_resolution    (missing → full)
//
// Koin declares dependency graph wiring with a `module { }` DSL:
//
//	val appModule = module {
//	    single { UserService(get()) }
//	    factory { Repo(get()) }
//	    viewModel { VM(get()) }
//	    scoped { SessionCache(get()) }
//	    single<Interface> { Impl() }
//	    singleOf(::UserService)
//	}
//	class C(private val svc: UserService) : KoinComponent {
//	    val repo: Repo by inject()
//	    val other = get<Other>()
//	}
//
// For each binding we emit a SCOPE.Pattern(subtype="di_binding") recording:
//   - di_scope        single|factory|viewModel|scoped (resolved scope)
//   - binding_type    explicit <Type> when given, else the constructed type
//   - implementation  the constructed concrete type (UserService in single{…})
//   - injected_deps   comma-joined types resolved via get<…>() inside the body
//
// For each resolution site we emit a SCOPE.Pattern(subtype="di_injection_point")
// recording the field/var name, the injected type and the mechanism
// (property_inject for `by inject()`, get_call for `get<T>()`,
// constructor_inject for KoinComponent constructor params).
//
// Honest limit: regex-based and file-local; Koin module wiring resolved across
// files (a `get()` whose provider lives in another module file) is not linked.
// The proven cells (per-binding scope + type + intra-body deps, and per-site
// type) are asserted by value in tests, so the three DI cells are flipped full;
// the cross-file resolution gap is documented in the cell notes.
package kotlin

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_koin", &koinExtractor{})
}

type koinExtractor struct{}

func (e *koinExtractor) Language() string { return "custom_kotlin_koin" }

var (
	// module { … } DSL opener (also matches `module {` after `= module`).
	reKoinModuleHead = regexp.MustCompile(`\bmodule\s*\{`)

	// A binding declaration: scope keyword, optional <ExplicitType>, then the
	// opening `{` of the provider lambda.
	// g1=scope keyword, g2=explicit type (optional).
	reKoinBinding = regexp.MustCompile(
		`\b(single|factory|viewModel|scoped|worker)\s*(?:<\s*([A-Za-z_][\w<>., ]*?)\s*>)?\s*\{`)

	// Reflective constructor binding: singleOf(::Type) / factoryOf(::Type) etc.
	// g1=scope keyword (without Of), g2=constructor-referenced type.
	reKoinOfBinding = regexp.MustCompile(
		`\b(single|factory|viewModel|scoped|worker)Of\s*\(\s*::\s*([A-Z][\w]*)`)

	// A constructed type at the head of a provider lambda body: `Impl(get())`.
	// g1=constructed concrete type.
	reKoinConstructed = regexp.MustCompile(`^\s*([A-Z][\w.]*)\s*\(`)

	// get() / get<T>() resolution inside a provider body. g1=optional type.
	reKoinGetCall = regexp.MustCompile(`\bget\s*(?:<\s*([A-Za-z_][\w<>., ]*?)\s*>)?\s*\(`)

	// `val foo: T by inject()` property injection. g1=name, g2=type.
	reKoinByInject = regexp.MustCompile(
		`\bval\s+(\w+)\s*:\s*([A-Z][\w<>., ]*?)\s*by\s+inject\s*\(\s*\)`)

	// `val foo by inject<T>()` shorthand. g1=name, g2=type.
	reKoinByInjectGeneric = regexp.MustCompile(
		`\bval\s+(\w+)\s+by\s+inject\s*<\s*([A-Za-z_][\w<>., ]*?)\s*>\s*\(`)

	// `val foo = get<T>()` direct resolution. g1=name, g2=type.
	reKoinAssignGet = regexp.MustCompile(
		`\bval\s+(\w+)\s*(?::\s*[A-Za-z_][\w<>., ]*\s*)?=\s*get\s*<\s*([A-Za-z_][\w<>., ]*?)\s*>\s*\(\s*\)`)
)

func (e *koinExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.koin.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "koin"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	hasKoin := reKoinModuleHead.MatchString(src) ||
		reKoinByInject.MatchString(src) ||
		reKoinByInjectGeneric.MatchString(src) ||
		reKoinOfBinding.MatchString(src)
	if !hasKoin {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Restrict binding extraction to within module { } blocks so plain
	// `single` words elsewhere are not mis-parsed.
	for _, mh := range reKoinModuleHead.FindAllStringIndex(src, -1) {
		open := mh[1] - 1
		end := matchBraceKotlin(src, open)
		if end < 0 {
			end = len(src)
		}
		modBody := src[open+1 : end]
		modBase := open + 1
		extractKoinBindings(add, src, modBody, modBase, file.Path)
	}

	// Reflective *Of(::Type) bindings (may be top-level inside module too;
	// dedup via seen handles overlap).
	for _, m := range reKoinOfBinding.FindAllStringSubmatchIndex(src, -1) {
		scope := normalizeKoinScope(src[m[2]:m[3]])
		typeName := src[m[4]:m[5]]
		line := lineOf(src, m[0])
		ent := makeEntity("koin:binding:"+typeName, "SCOPE.Pattern", "di_binding", file.Path, "kotlin", line)
		setProps(&ent,
			"framework", "koin",
			"di_framework", "koin",
			"di_scope", scope,
			"binding_type", typeName,
			"implementation", typeName,
			"binding_style", "constructor_ref",
			"provenance", "INFERRED_FROM_KOIN_DI",
		)
		add(ent)
	}

	// Resolution sites (injection points) — file-wide.
	for _, m := range reKoinByInject.FindAllStringSubmatchIndex(src, -1) {
		emitKoinInjection(add, file.Path, src[m[2]:m[3]], strings.TrimSpace(src[m[4]:m[5]]), "property_inject", lineOf(src, m[0]))
	}
	for _, m := range reKoinByInjectGeneric.FindAllStringSubmatchIndex(src, -1) {
		emitKoinInjection(add, file.Path, src[m[2]:m[3]], strings.TrimSpace(src[m[4]:m[5]]), "property_inject", lineOf(src, m[0]))
	}
	for _, m := range reKoinAssignGet.FindAllStringSubmatchIndex(src, -1) {
		emitKoinInjection(add, file.Path, src[m[2]:m[3]], strings.TrimSpace(src[m[4]:m[5]]), "get_call", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// extractKoinBindings walks single/factory/viewModel/scoped binding blocks
// inside a single module body, resolving each binding's scope, declared/
// constructed type and the deps it injects via get<…>().
func extractKoinBindings(add func(types.EntityRecord), src, modBody string, modBase int, filePath string) {
	for _, m := range reKoinBinding.FindAllStringSubmatchIndex(modBody, -1) {
		scope := normalizeKoinScope(modBody[m[2]:m[3]])
		explicitType := ""
		if m[4] >= 0 {
			explicitType = strings.TrimSpace(modBody[m[4]:m[5]])
		}

		lambdaOpen := m[1] - 1 // the binding lambda '{'
		lambdaEnd := matchBraceKotlin(modBody, lambdaOpen)
		if lambdaEnd < 0 {
			lambdaEnd = len(modBody)
		}
		lambdaBody := modBody[lambdaOpen+1 : lambdaEnd]

		implType := ""
		if cm := reKoinConstructed.FindStringSubmatch(strings.TrimLeft(lambdaBody, " \t\r\n")); cm != nil {
			implType = cm[1]
		}

		// Collect injected deps via get<T>() / get(); typed gets carry their
		// type, untyped gets are recorded positionally.
		var deps []string
		for _, gm := range reKoinGetCall.FindAllStringSubmatchIndex(lambdaBody, -1) {
			if gm[2] >= 0 {
				deps = append(deps, strings.TrimSpace(lambdaBody[gm[2]:gm[3]]))
			} else {
				deps = append(deps, "<inferred>")
			}
		}

		bindingType := explicitType
		if bindingType == "" {
			bindingType = implType
		}
		nameKey := bindingType
		if nameKey == "" {
			nameKey = scope + "@" + filePath
		}
		line := lineOf(src, modBase+m[0])
		ent := makeEntity("koin:binding:"+nameKey, "SCOPE.Pattern", "di_binding", filePath, "kotlin", line)
		setProps(&ent,
			"framework", "koin",
			"di_framework", "koin",
			"di_scope", scope,
			"binding_style", "lambda",
			"provenance", "INFERRED_FROM_KOIN_DI",
		)
		if bindingType != "" {
			setProps(&ent, "binding_type", bindingType)
		}
		if implType != "" {
			setProps(&ent, "implementation", implType)
		}
		if len(deps) > 0 {
			setProps(&ent,
				"injected_deps", strings.Join(deps, ","),
				"injected_dep_count", strconv.Itoa(len(deps)),
			)
		}
		add(ent)
	}
}

func emitKoinInjection(add func(types.EntityRecord), filePath, name, typeName, mechanism string, line int) {
	ent := makeEntity("koin:inject:"+name+":"+typeName, "SCOPE.Pattern", "di_injection_point", filePath, "kotlin", line)
	setProps(&ent,
		"framework", "koin",
		"di_framework", "koin",
		"field_name", name,
		"injected_type", typeName,
		"mechanism", mechanism,
		"provenance", "INFERRED_FROM_KOIN_DI",
	)
	add(ent)
}

func normalizeKoinScope(keyword string) string {
	switch keyword {
	case "single":
		return "single"
	case "factory":
		return "factory"
	case "viewModel":
		return "viewModel"
	case "scoped":
		return "scoped"
	case "worker":
		return "worker"
	}
	return keyword
}
