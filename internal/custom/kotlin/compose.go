package kotlin

import (
	"context"
	"regexp"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_compose", &composeExtractor{})
}

type composeExtractor struct{}

func (e *composeExtractor) Language() string { return "custom_kotlin_compose" }

var (
	reComposableFun = regexp.MustCompile(
		`@Composable\s+(?:(?:private|internal|public)\s+)?fun\s+([A-Z][A-Za-z0-9_]*)\s*\(`,
	)
	reComposableScreen = regexp.MustCompile(
		`@Composable\s+(?:(?:private|internal|public)\s+)?fun\s+([A-Z][A-Za-z0-9_]*Screen)\s*\(`,
	)
	reNavHostStart  = regexp.MustCompile(`(?m)\bNavHost\s*\(`)
	reNavComposable = regexp.MustCompile(
		`composable\s*\(\s*(?:route\s*=\s*)?["']([^"']+)["']\s*(?:,|\))`,
	)
	reNavNestedGraph = regexp.MustCompile(
		`navigation\s*\(\s*(?:route\s*=\s*)?["']([^"']+)["']`,
	)
	reViewModelGeneric = regexp.MustCompile(
		`\b(?:viewModel|hiltViewModel|koinViewModel)\s*<([A-Z][A-Za-z0-9_]*)>\s*\(`,
	)
	reViewModelAssign = regexp.MustCompile(
		`val\s+\w+\s*:\s*([A-Z][A-Za-z0-9_]*)\s*=\s*(?:viewModel|hiltViewModel|koinViewModel)\s*\(`,
	)

	// Navigation transition: navController.navigate("route") /
	// navController.navigate("detail/42") / navController.navigate(Screen.Detail.route).
	// Group 1 = string-literal route (in-file resolvable); group 2 = bare
	// expression route (e.g. Screen.Detail.route — cross-file indirection).
	reNavNavigateLiteral = regexp.MustCompile(
		`\.navigate\s*\(\s*["']([^"']+)["']`,
	)
	reNavNavigateExpr = regexp.MustCompile(
		`\.navigate\s*\(\s*([A-Z][A-Za-z0-9_.]*\.route)\b`,
	)
	// Enclosing @Composable function header (name capture) used to attribute
	// navigate(...) / viewModel() call sites to the screen that contains them.
	reComposableHeader = regexp.MustCompile(
		`@Composable\s+(?:(?:private|internal|public)\s+)?fun\s+([A-Z][A-Za-z0-9_]*)\s*\(`,
	)

	// State management: StateFlow<T>, MutableStateFlow<T>, collectAsState, collectAsStateWithLifecycle
	reStateFlow = regexp.MustCompile(
		`\b(?:Mutable)?StateFlow\s*<([A-Za-z0-9_?<>, ]+)>`,
	)
	// remember { } and rememberSaveable { } calls
	reRemember = regexp.MustCompile(
		`\b(remember(?:Saveable)?)\s*(?:<[^>]*>)?\s*\{`,
	)
	// mutableStateOf / mutableStateListOf / mutableStateMapOf
	reMutableStateOf = regexp.MustCompile(
		`\b(mutableState(?:Of|ListOf|MapOf))\s*\(`,
	)
	// collectAsState() / collectAsStateWithLifecycle()
	reCollectAsState = regexp.MustCompile(
		`\.collect(?:AsState|AsStateWithLifecycle)\s*\(`,
	)

	// KMP expect/actual declarations
	reKmpExpect = regexp.MustCompile(
		`(?m)^\s*expect\s+(?:fun|class|val|var|object|interface)\s+(\w+)`,
	)
	reKmpActual = regexp.MustCompile(
		`(?m)^\s*actual\s+(?:fun|class|val|var|object|interface)\s+(\w+)`,
	)
)

// builtinComposables are Compose framework-owned composables not emitted as entities.
var builtinComposables = map[string]bool{
	"Column": true, "Row": true, "Box": true, "Scaffold": true, "Surface": true,
	"Text": true, "Image": true, "Icon": true, "Button": true, "IconButton": true,
	"FloatingActionButton": true, "TextField": true, "OutlinedTextField": true,
	"Card": true, "LazyColumn": true, "LazyRow": true, "LazyVerticalGrid": true,
	"Spacer": true, "Divider": true, "CircularProgressIndicator": true,
	"LinearProgressIndicator": true, "AlertDialog": true, "DropdownMenu": true,
	"DropdownMenuItem": true, "TopAppBar": true, "BottomNavigation": true,
	"BottomNavigationItem": true, "NavigationBar": true, "NavigationBarItem": true,
	"ModalBottomSheet": true, "Checkbox": true, "RadioButton": true,
	"Switch": true, "Slider": true, "Tab": true, "TabRow": true,
	"AnimatedVisibility": true, "AnimatedContent": true, "Crossfade": true,
	"BackHandler": true, "LaunchedEffect": true, "DisposableEffect": true,
	"SideEffect": true, "CompositionLocalProvider": true, "NavHost": true,
	"BottomSheetScaffold": true, "ModalNavigationDrawer": true, "SnackbarHost": true,
	"SwipeToDismiss": true, "PullRefreshIndicator": true,
}

func (e *composeExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.compose_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "compose"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	lang := file.Language
	if lang != "kotlin" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// 1. @Composable functions -> SCOPE.UIComponent/component
	for _, m := range reComposableFun.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if builtinComposables[name] {
			continue
		}
		key := "compose:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COMPOSE_ANNOTATION")
		entities = append(entities, ent)
	}

	// 1b. @Composable XxxScreen functions -> SCOPE.UIComponent/screen (screen_detection)
	for _, m := range reComposableScreen.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "compose:screen:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.UIComponent", "screen", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COMPOSE_SCREEN",
			"screen_name", name)
		entities = append(entities, ent)
	}

	// 2. NavHost routes -> SCOPE.Operation/endpoint
	for _, nm := range reNavHostStart.FindAllStringIndex(src, -1) {
		// Extract brace block after NavHost(
		block := extractBraceBlock(src, nm[0])
		if block == "" {
			continue
		}
		for _, rm := range reNavComposable.FindAllStringSubmatch(block, -1) {
			route := rm[1]
			key := "nav_route:" + route
			if seen[key] {
				continue
			}
			seen[key] = true
			ent := makeEntity(route, "SCOPE.Operation", "endpoint", file.Path, lang, lineOf(src, nm[0]))
			setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COMPOSE_NAVHOST",
				"navigation_type", "navhost")
			entities = append(entities, ent)
		}
		for _, rm := range reNavNestedGraph.FindAllStringSubmatch(block, -1) {
			route := rm[1]
			key := "nav_nested:" + route
			if seen[key] {
				continue
			}
			seen[key] = true
			ent := makeEntity(route, "SCOPE.Operation", "endpoint", file.Path, lang, lineOf(src, nm[0]))
			setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COMPOSE_NAVHOST",
				"navigation_type", "nested_graph")
			entities = append(entities, ent)
		}
	}

	// 3. ViewModel injection -> SCOPE.Component (dependency node)
	for _, m := range reViewModelGeneric.FindAllStringSubmatchIndex(src, -1) {
		vmType := src[m[2]:m[3]]
		key := "viewmodel:" + vmType
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(vmType, "SCOPE.Component", "", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COMPOSE_VIEWMODEL",
			"injection_kind", "viewmodel")
		entities = append(entities, ent)
	}
	for _, m := range reViewModelAssign.FindAllStringSubmatchIndex(src, -1) {
		vmType := src[m[2]:m[3]]
		key := "viewmodel:" + vmType
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(vmType, "SCOPE.Component", "", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COMPOSE_VIEWMODEL",
			"injection_kind", "viewmodel_assign")
		entities = append(entities, ent)
	}

	// 4. StateFlow<T> / MutableStateFlow<T> -> SCOPE.Pattern/state_management
	for _, m := range reStateFlow.FindAllStringSubmatchIndex(src, -1) {
		typeParam := src[m[2]:m[3]]
		key := "stateflow:" + typeParam
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity("StateFlow<"+typeParam+">", "SCOPE.Pattern", "state_management", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_STATEFLOW",
			"state_type", typeParam)
		entities = append(entities, ent)
	}

	// 5. remember{} / rememberSaveable{} -> SCOPE.Pattern/state_management
	for _, m := range reRemember.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		key := "remember:" + fnName + ":" + lang + ":" + src[m[0]:m[1]]
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(fnName, "SCOPE.Pattern", "state_management", file.Path, lang, line)
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_REMEMBER",
			"remember_kind", fnName)
		entities = append(entities, ent)
	}

	// 6. mutableStateOf / mutableStateListOf / mutableStateMapOf -> SCOPE.Pattern/state_setter
	for _, m := range reMutableStateOf.FindAllStringSubmatchIndex(src, -1) {
		fnName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		key := "mutable_state:" + fnName + ":" + lang + ":" + src[m[0]:m[1]]
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity(fnName, "SCOPE.Pattern", "state_setter", file.Path, lang, line)
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_MUTABLE_STATE",
			"setter_kind", fnName)
		entities = append(entities, ent)
	}

	// 7. collectAsState() / collectAsStateWithLifecycle() -> SCOPE.Pattern/state_management
	for _, m := range reCollectAsState.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		key := "collect_as_state:" + src[m[0]:m[1]]
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity("collectAsState", "SCOPE.Pattern", "state_management", file.Path, lang, line)
		setProps(&ent, "framework", "compose", "provenance", "INFERRED_FROM_COLLECT_AS_STATE")
		entities = append(entities, ent)
	}

	// 8. KMP expect declarations -> SCOPE.Pattern/platform_branching
	for _, m := range reKmpExpect.FindAllStringSubmatchIndex(src, -1) {
		declName := src[m[2]:m[3]]
		key := "kmp:expect:" + declName
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity("expect:"+declName, "SCOPE.Pattern", "platform_branching", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "kmp", "provenance", "INFERRED_FROM_KMP_EXPECT",
			"declaration_name", declName, "branching_kind", "expect")
		entities = append(entities, ent)
	}

	// 9. KMP actual declarations -> SCOPE.Pattern/platform_branching
	for _, m := range reKmpActual.FindAllStringSubmatchIndex(src, -1) {
		declName := src[m[2]:m[3]]
		key := "kmp:actual:" + declName
		if seen[key] {
			continue
		}
		seen[key] = true
		ent := makeEntity("actual:"+declName, "SCOPE.Pattern", "platform_branching", file.Path, lang, lineOf(src, m[0]))
		setProps(&ent, "framework", "kmp", "provenance", "INFERRED_FROM_KMP_ACTUAL",
			"declaration_name", declName, "branching_kind", "actual")
		entities = append(entities, ent)
	}

	// ---------------------------------------------------------------------
	// Edges (issue #3576). Both NAVIGATES_TO and USES are emitted as embedded
	// relationships with an empty FromID so the resolver substitutes the host
	// entity ID (the enclosing @Composable) at edge-assembly time. ToID is a
	// bare name that the name-keyed resolver matches against the declared
	// route / ViewModel entity.
	//
	// relsByComposable groups every edge by the name of the @Composable that
	// owns the call site; attached to that composable's entity record below.
	spans := composableSpans(src)
	relsByComposable := make(map[string][]types.RelationshipRecord)
	edgeSeen := make(map[string]bool)

	// 10. Navigation transitions -> NAVIGATES_TO (screen -> route).
	// navController.navigate("detail/42") inside HomeScreen{} emits
	// HomeScreen -NAVIGATES_TO-> route:detail/{id}.
	emitNav := func(rawRoute, via string, off int) {
		from := enclosingComposable(spans, off)
		if from == "" {
			return // navigate() outside any @Composable — unattributable
		}
		route := normalizeRoute(rawRoute)
		key := "nav|" + from + "|" + route + "|" + via
		if edgeSeen[key] {
			return
		}
		edgeSeen[key] = true
		props := map[string]string{
			"route":     route,
			"via":       via,
			"caller":    from,
			"framework": "compose",
			"line":      strconv.Itoa(lineOf(src, off)),
		}
		if via == "navigate_route_const" {
			// Sealed-class Screen.X.route indirection: the literal route string
			// lives in another file, so the target is partial/unresolved here.
			props["unresolved"] = "true"
		}
		relsByComposable[from] = append(relsByComposable[from], types.RelationshipRecord{
			ToID:       "route:" + route,
			Kind:       "NAVIGATES_TO",
			Properties: props,
		})
	}
	for _, m := range reNavNavigateLiteral.FindAllStringSubmatchIndex(src, -1) {
		emitNav(src[m[2]:m[3]], "navigate_call", m[0])
	}
	for _, m := range reNavNavigateExpr.FindAllStringSubmatchIndex(src, -1) {
		emitNav(src[m[2]:m[3]], "navigate_route_const", m[0])
	}

	// 11. view -> viewmodel -> USES (composable -> ViewModel type).
	// val vm: MyViewModel = viewModel() inside HomeScreen{} emits
	// HomeScreen -USES-> MyViewModel.
	emitUses := func(vmType, via string, off int) {
		from := enclosingComposable(spans, off)
		if from == "" {
			return
		}
		key := "uses|" + from + "|" + vmType
		if edgeSeen[key] {
			return
		}
		edgeSeen[key] = true
		relsByComposable[from] = append(relsByComposable[from], types.RelationshipRecord{
			ToID: vmType,
			Kind: "USES",
			Properties: map[string]string{
				"viewmodel": vmType,
				"via":       via,
				"caller":    from,
				"framework": "compose",
				"line":      strconv.Itoa(lineOf(src, off)),
			},
		})
	}
	for _, m := range reViewModelGeneric.FindAllStringSubmatchIndex(src, -1) {
		emitUses(src[m[2]:m[3]], "viewmodel", m[0])
	}
	for _, m := range reViewModelAssign.FindAllStringSubmatchIndex(src, -1) {
		emitUses(src[m[2]:m[3]], "viewmodel_assign", m[0])
	}

	// Attach collected edges to their owning composable entity record.
	if len(relsByComposable) > 0 {
		for i := range entities {
			if rels, ok := relsByComposable[entities[i].Name]; ok &&
				entities[i].Kind == "SCOPE.UIComponent" {
				entities[i].Relationships = append(entities[i].Relationships, rels...)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// composableSpan records the byte range owned by one @Composable function so
// call sites (navigate(...), viewModel()) can be attributed to the enclosing
// screen. The span runs from the function header to the close of its body's
// top-level brace block.
type composableSpan struct {
	name       string
	start, end int
}

// composableSpans returns the byte ranges of every @Composable function in src,
// in source order. Used to attribute navigate(...) / viewModel() call sites to
// their enclosing composable.
func composableSpans(src string) []composableSpan {
	var spans []composableSpan
	for _, m := range reComposableHeader.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Body block starts at the first '{' after the header's '('.
		bodyStart := -1
		for i := m[1]; i < len(src); i++ {
			if src[i] == '{' {
				bodyStart = i
				break
			}
			if src[i] == '=' { // single-expression composable, no brace body
				break
			}
		}
		end := len(src)
		if bodyStart != -1 {
			depth := 0
			for i := bodyStart; i < len(src); i++ {
				switch src[i] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						end = i + 1
						i = len(src) // break outer
					}
				}
			}
		}
		spans = append(spans, composableSpan{name: name, start: m[0], end: end})
	}
	return spans
}

// enclosingComposable returns the name of the composable whose span contains
// off, or "" if none. Inner (later-starting) spans win so nested call sites are
// attributed to the closest enclosing screen.
func enclosingComposable(spans []composableSpan, off int) string {
	best := ""
	bestStart := -1
	for _, s := range spans {
		if off >= s.start && off < s.end && s.start > bestStart {
			best = s.name
			bestStart = s.start
		}
	}
	return best
}

// reRouteParamSeg matches a single concrete path segment that is a navigation
// argument value (a bare number, a $var, or a ${expr} interpolation) so it can
// be normalised back to the declared {id}-style template param.
var reRouteParamSeg = regexp.MustCompile(`/(?:\d+|\$\{[^}]*\}|\$[A-Za-z_][A-Za-z0-9_]*)`)

// normalizeRoute rewrites a concrete navigate("detail/42") destination into the
// declared route template "detail/{id}" by replacing value segments with {id}.
// Already-templated routes (detail/{id}) and constant routes (home) pass
// through unchanged.
func normalizeRoute(route string) string {
	return reRouteParamSeg.ReplaceAllString(route, "/{id}")
}

// extractBraceBlock returns the content of the balanced brace block starting at or after start.
func extractBraceBlock(src string, start int) string {
	idx := -1
	for i := start; i < len(src); i++ {
		if src[i] == '{' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ""
	}
	depth := 0
	for i := idx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[idx : i+1]
			}
		}
	}
	return src[idx:]
}
