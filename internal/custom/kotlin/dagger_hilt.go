// Package kotlin — Dagger / Hilt DI extractor (record
// lang.kotlin.framework.dagger-hilt).
//
// Covers:
//   - di_binding_extraction  (missing → full)
//   - di_injection_point     (missing → full)
//   - di_scope_resolution    (missing → full)
//
// Dagger/Hilt annotations are identical in Kotlin and Java; this extractor is
// gated to `kotlin` source so the Java pipeline is unaffected. It targets the
// canonical Hilt/Dagger DSL:
//
//	@Module
//	@InstallIn(SingletonComponent::class)
//	object NetworkModule {
//	    @Provides
//	    @Singleton
//	    fun provideRepo(api: ApiService): Repo = RepoImpl(api)
//
//	    @Binds
//	    abstract fun bindLogger(impl: ConsoleLogger): Logger
//	}
//
//	class UserService @Inject constructor(private val repo: Repo)
//
//	@HiltViewModel
//	class ProfileViewModel @Inject constructor(private val repo: Repo) : ViewModel()
//
//	@HiltAndroidApp class App : Application()
//	@AndroidEntryPoint class MainActivity : ComponentActivity()
//
// Bindings (di_binding):
//   - @Provides fun provideX(deps...): X      → provider binding, binding_type=
//     return type, implementation=return type, injected_deps=param types.
//   - @Binds fun bindX(impl: Impl): X         → bind binding, binding_type=
//     return type (the interface), implementation=parameter type (the impl).
//     The @InstallIn(Component::class) on the enclosing module sets di_scope to
//     the component (e.g. SingletonComponent → singleton); a method-level scope
//     annotation (@Singleton / @ActivityScoped / …) overrides it.
//
// Injection points (di_injection_point):
//   - @Inject constructor(p1: T1, p2: T2)     → one injection point per
//     constructor parameter, mechanism=constructor_inject.
//   - @Inject lateinit var field: T           → field injection,
//     mechanism=field_inject.
//
// Component / entry-point markers (di_scope_resolution):
//   - @HiltAndroidApp / @AndroidEntryPoint / @HiltViewModel on a class →
//     a di_component entity recording the Hilt entry-point kind and the
//     component scope it lives in.
//
// Honest limit: regex-based and file-local. Cross-module binding resolution
// (a @Provides whose return type is consumed by an @Inject in another file)
// is not linked here — that pairing is the engine's job. The proven cells
// (per-binding scope+type+impl+deps, per-parameter injection points, per-class
// entry points) are asserted by value in tests, so the three DI cells flip
// full; the cross-file resolution gap is documented in the cell notes.
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
	extractor.Register("custom_kotlin_dagger_hilt", &daggerHiltExtractor{})
}

type daggerHiltExtractor struct{}

func (e *daggerHiltExtractor) Language() string { return "custom_kotlin_dagger_hilt" }

var (
	// @InstallIn(SingletonComponent::class) — g1 = component class name.
	reHiltInstallIn = regexp.MustCompile(`@InstallIn\s*\(\s*([A-Za-z_][\w]*)\s*::\s*class`)

	// @Provides on a fun: matches the annotation; the method head follows.
	reDaggerProvides = regexp.MustCompile(`@Provides\b`)

	// @Binds on a fun.
	reDaggerBinds = regexp.MustCompile(`@Binds\b`)

	// Method head: `fun provideRepo(api: ApiService): Repo`.
	// g1 = name, g2 = raw params, g3 = return type (optional).
	reKtFunHead = regexp.MustCompile(
		`(?:abstract\s+|open\s+|override\s+|internal\s+|private\s+|public\s+|protected\s+|suspend\s+)*` +
			`fun\s+([A-Za-z_]\w*)\s*\(([^)]*)\)\s*(?::\s*([A-Za-z_][\w<>., ?]*?)\s*)?(?:=|\{|\n|\r|$)`)

	// @Inject constructor(...) — g1 = raw params.
	reHiltInjectConstructor = regexp.MustCompile(
		`@Inject\s+(?:internal\s+|private\s+|protected\s+|public\s+)?constructor\s*\(([^)]*)\)`)

	// @Inject lateinit var field: T  — g1 = field name, g2 = type.
	reHiltInjectField = regexp.MustCompile(
		`@Inject\s+(?:lateinit\s+)?(?:var|val)\s+([A-Za-z_]\w*)\s*:\s*([A-Za-z_][\w<>., ?]*?)\s*(?:=|$|\n)`)

	// Hilt entry-point markers on a class. g1 = marker, g2 = class name.
	reHiltEntryPoint = regexp.MustCompile(
		`@(HiltAndroidApp|AndroidEntryPoint|HiltViewModel)\b[\s\S]{0,160}?\bclass\s+([A-Za-z_]\w*)`)

	// Method-level scope annotation (overrides component scope).
	reDaggerMethodScope = regexp.MustCompile(
		`@(Singleton|ActivityScoped|ActivityRetainedScoped|FragmentScoped|ViewScoped|ViewModelScoped|ServiceScoped|Reusable)\b`)
)

func (e *daggerHiltExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.dagger_hilt.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "dagger-hilt"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	if !daggerHiltHasAny(src) {
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

	// Component scope from the (file-local) @InstallIn on the enclosing module.
	componentScope := ""
	componentName := ""
	if mm := reHiltInstallIn.FindStringSubmatch(src); len(mm) >= 2 {
		componentName = mm[1]
		componentScope = hiltComponentScope(componentName)
	}

	extractDaggerProviders(add, src, file.Path, componentScope, componentName)
	extractDaggerBinds(add, src, file.Path, componentScope, componentName)
	extractHiltInjections(add, src, file.Path)
	extractHiltEntryPoints(add, src, file.Path, componentScope)

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

func daggerHiltHasAny(src string) bool {
	return strings.Contains(src, "@Provides") ||
		strings.Contains(src, "@Binds") ||
		strings.Contains(src, "@Inject") ||
		strings.Contains(src, "@HiltAndroidApp") ||
		strings.Contains(src, "@AndroidEntryPoint") ||
		strings.Contains(src, "@HiltViewModel") ||
		strings.Contains(src, "@InstallIn")
}

// extractDaggerProviders handles `@Provides fun provideX(deps): X`.
func extractDaggerProviders(add func(types.EntityRecord), src, filePath, componentScope, componentName string) {
	for _, m := range reDaggerProvides.FindAllStringIndex(src, -1) {
		head, headStart, ok := nextKtFunHead(src, m[1])
		if !ok {
			continue
		}
		name := head[1]
		params := head[2]
		retType := strings.TrimSpace(head[3])
		if retType == "" {
			continue
		}
		scope := resolveDaggerScope(src, m[0], headStart, componentScope)
		deps := paramTypes(params)

		ent := makeEntity("dagger:binding:provides:"+name+":"+retType, "SCOPE.Pattern", "di_binding", filePath, "kotlin", lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dagger-hilt",
			"di_framework", "dagger-hilt",
			"di_scope", scope,
			"binding_style", "provides",
			"binding_type", retType,
			"implementation", retType,
			"provider_method", name,
			"provenance", "INFERRED_FROM_DAGGER_DI",
		)
		if componentName != "" {
			setProps(&ent, "component", componentName)
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

// extractDaggerBinds handles `@Binds abstract fun bindX(impl: Impl): Iface`.
func extractDaggerBinds(add func(types.EntityRecord), src, filePath, componentScope, componentName string) {
	for _, m := range reDaggerBinds.FindAllStringIndex(src, -1) {
		head, headStart, ok := nextKtFunHead(src, m[1])
		if !ok {
			continue
		}
		name := head[1]
		params := head[2]
		retType := strings.TrimSpace(head[3])
		if retType == "" {
			continue
		}
		// @Binds takes exactly one parameter: the implementation.
		impl := ""
		if pts := paramTypes(params); len(pts) > 0 {
			impl = pts[0]
		}
		scope := resolveDaggerScope(src, m[0], headStart, componentScope)

		ent := makeEntity("dagger:binding:binds:"+retType+":"+impl, "SCOPE.Pattern", "di_binding", filePath, "kotlin", lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dagger-hilt",
			"di_framework", "dagger-hilt",
			"di_scope", scope,
			"binding_style", "binds",
			"binding_type", retType,
			"bind_method", name,
			"provenance", "INFERRED_FROM_DAGGER_DI",
		)
		if impl != "" {
			setProps(&ent, "implementation", impl)
		}
		if componentName != "" {
			setProps(&ent, "component", componentName)
		}
		add(ent)
	}
}

// extractHiltInjections handles @Inject constructor(...) and @Inject fields.
func extractHiltInjections(add func(types.EntityRecord), src, filePath string) {
	for _, m := range reHiltInjectConstructor.FindAllStringSubmatchIndex(src, -1) {
		params := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		for _, p := range splitParams(params) {
			name, typ := paramNameType(p)
			if typ == "" {
				continue
			}
			ent := makeEntity("dagger:inject:ctor:"+name+":"+typ, "SCOPE.Pattern", "di_injection_point", filePath, "kotlin", line)
			setProps(&ent,
				"framework", "dagger-hilt",
				"di_framework", "dagger-hilt",
				"field_name", name,
				"injected_type", typ,
				"mechanism", "constructor_inject",
				"provenance", "INFERRED_FROM_DAGGER_DI",
			)
			add(ent)
		}
	}

	for _, m := range reHiltInjectField.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		typ := strings.TrimSpace(src[m[4]:m[5]])
		if typ == "" {
			continue
		}
		ent := makeEntity("dagger:inject:field:"+name+":"+typ, "SCOPE.Pattern", "di_injection_point", filePath, "kotlin", lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dagger-hilt",
			"di_framework", "dagger-hilt",
			"field_name", name,
			"injected_type", typ,
			"mechanism", "field_inject",
			"provenance", "INFERRED_FROM_DAGGER_DI",
		)
		add(ent)
	}
}

// extractHiltEntryPoints records @HiltAndroidApp / @AndroidEntryPoint /
// @HiltViewModel markers as di_component scope-resolution entities.
func extractHiltEntryPoints(add func(types.EntityRecord), src, filePath, componentScope string) {
	for _, m := range reHiltEntryPoint.FindAllStringSubmatchIndex(src, -1) {
		marker := src[m[2]:m[3]]
		className := src[m[4]:m[5]]
		scope := hiltEntryPointScope(marker)
		if scope == "" {
			scope = componentScope
		}
		ent := makeEntity("dagger:component:"+marker+":"+className, "SCOPE.Pattern", "di_scope_resolution", filePath, "kotlin", lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dagger-hilt",
			"di_framework", "dagger-hilt",
			"entry_point_kind", marker,
			"component_class", className,
			"di_scope", scope,
			"provenance", "INFERRED_FROM_DAGGER_DI",
		)
		add(ent)
	}
}

// ---------------------------------------------------------------------------
// Helpers (dagger-hilt namespaced)
// ---------------------------------------------------------------------------

// nextKtFunHead finds the first `fun <name>(params): Ret` head at or after pos
// (within a 400-byte window so annotation-stacked methods still resolve).
// Returns the submatch slice, the head's absolute start offset, and ok.
func nextKtFunHead(src string, pos int) ([]string, int, bool) {
	end := pos + 400
	if end > len(src) {
		end = len(src)
	}
	window := src[pos:end]
	loc := reKtFunHead.FindStringSubmatchIndex(window)
	if loc == nil {
		return nil, 0, false
	}
	sm := reKtFunHead.FindStringSubmatch(window)
	return sm, pos + loc[0], true
}

// resolveDaggerScope returns the method-level scope annotation if present
// between annStart and headStart, else the component scope, else "unscoped".
func resolveDaggerScope(src string, annStart, headStart int, componentScope string) string {
	if headStart > annStart && headStart <= len(src) {
		// Look back a little before the annotation too — scope annotations may
		// precede @Provides — by scanning the window around the method head.
		lo := annStart - 80
		if lo < 0 {
			lo = 0
		}
		window := src[lo:headStart]
		if mm := reDaggerMethodScope.FindStringSubmatch(window); len(mm) >= 2 {
			return daggerScopeName(mm[1])
		}
	}
	if componentScope != "" {
		return componentScope
	}
	return "unscoped"
}

// hiltComponentScope maps a Hilt component class to its scope name.
func hiltComponentScope(component string) string {
	switch component {
	case "SingletonComponent", "ApplicationComponent":
		return "singleton"
	case "ActivityComponent":
		return "activity"
	case "ActivityRetainedComponent":
		return "activity_retained"
	case "FragmentComponent":
		return "fragment"
	case "ViewComponent", "ViewWithFragmentComponent":
		return "view"
	case "ViewModelComponent":
		return "viewmodel"
	case "ServiceComponent":
		return "service"
	}
	return strings.ToLower(strings.TrimSuffix(component, "Component"))
}

// daggerScopeName normalizes a method-level scope annotation to a scope name.
func daggerScopeName(ann string) string {
	switch ann {
	case "Singleton":
		return "singleton"
	case "ActivityScoped":
		return "activity"
	case "ActivityRetainedScoped":
		return "activity_retained"
	case "FragmentScoped":
		return "fragment"
	case "ViewScoped":
		return "view"
	case "ViewModelScoped":
		return "viewmodel"
	case "ServiceScoped":
		return "service"
	case "Reusable":
		return "reusable"
	}
	return strings.ToLower(ann)
}

// hiltEntryPointScope maps an entry-point marker to its component scope.
func hiltEntryPointScope(marker string) string {
	switch marker {
	case "HiltAndroidApp":
		return "singleton"
	case "HiltViewModel":
		return "viewmodel"
	case "AndroidEntryPoint":
		return "activity"
	}
	return ""
}

// paramTypes returns the declared types of every `name: Type` parameter.
func paramTypes(params string) []string {
	var out []string
	for _, p := range splitParams(params) {
		if _, typ := paramNameType(p); typ != "" {
			out = append(out, typ)
		}
	}
	return out
}

// splitParams splits a Kotlin parameter list on commas at depth 0 (so generic
// args like Map<K, V> are not split).
func splitParams(params string) []string {
	params = strings.TrimSpace(params)
	if params == "" {
		return nil
	}
	var out []string
	depth := 0
	start := 0
	for i, r := range params {
		switch r {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, params[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, params[start:])
	return out
}

// paramNameType parses one `[modifiers] name: Type [= default]` parameter,
// returning the name and the type (default value and modifiers stripped).
func paramNameType(p string) (string, string) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", ""
	}
	// Drop a default value.
	if eq := strings.Index(p, "="); eq >= 0 {
		p = strings.TrimSpace(p[:eq])
	}
	colon := strings.Index(p, ":")
	if colon < 0 {
		return "", ""
	}
	left := strings.Fields(strings.TrimSpace(p[:colon])) // [val|var|private|…] name
	name := ""
	if len(left) > 0 {
		name = left[len(left)-1]
	}
	typ := strings.TrimSpace(p[colon+1:])
	return name, typ
}
