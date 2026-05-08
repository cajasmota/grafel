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
