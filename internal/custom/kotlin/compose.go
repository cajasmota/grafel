package kotlin

import (
	"context"
	"regexp"

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
		`\b(?:viewModel|hiltViewModel)\s*<([A-Z][A-Za-z0-9_]*)>\s*\(`,
	)
	reViewModelAssign = regexp.MustCompile(
		`val\s+\w+\s*:\s*([A-Z][A-Za-z0-9_]*)\s*=\s*(?:viewModel|hiltViewModel)\s*\(`,
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

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
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
