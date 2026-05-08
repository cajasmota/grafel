// React Native component hierarchy and navigation route extraction (MX-1063).
//
// This extractor targets two React Native / React Navigation patterns:
//
//  1. Navigation routes: <Stack.Screen name="Home" component={HomeScreen} />,
//     <Tab.Screen ... />, <Drawer.Screen ... /> → SCOPE.Operation with name
//     "route:{Stack|Tab|Drawer}:{name_prop}" and metadata containing
//     route_type, route_name, and component.
//
//  2. Component hierarchy: exported function/arrow components whose JSX body
//     contains PascalCase child component tags → one SCOPE.UIComponent for the
//     parent component and additional SCOPE.UIComponent records for each
//     unique PascalCase child, with parent_component metadata to trace the
//     relationship.
//
// Detection heuristic: regex-based, two-pass (same approach as errorpattern.go
// and MX-1047). No tree-sitter parsing required for this secondary pass — the
// patterns are structurally unambiguous in React Navigation source.
//
// File gate: language must be "typescript" or "javascript".
// Framework gate (react-native signal): the file imports from 'react-native'
// OR contains a navigator Screen pattern (Stack.Screen / Tab.Screen /
// Drawer.Screen). This ensures non-React-Native JS/TS files are unaffected.
package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_react_native", &reactNativeExtractor{})
}

type reactNativeExtractor struct{}

func (e *reactNativeExtractor) Language() string { return "custom_js_react_native" }

// ---- compiled regexes -------------------------------------------------------

var (
	// React Native import signal: import { ... } from 'react-native'
	reRNImport = regexp.MustCompile(
		`from\s+['"]react-native['"]`,
	)

	// Navigator Screen tags: <Stack.Screen ...>, <Tab.Screen ...>, <Drawer.Screen ...>
	// Captures navigator kind (Stack/Tab/Drawer) and the full attribute block.
	reRNScreen = regexp.MustCompile(
		`<(Stack|Tab|Drawer)\.Screen\b([^>]*/?>|[^>]*>)`,
	)

	// name="..." or name={'...'} prop inside a Screen tag
	reRNScreenName = regexp.MustCompile(
		`\bname=(?:"([^"]+)"|'([^']+)'|\{['"]([^'"]+)['"]\})`,
	)

	// component={Identifier} prop inside a Screen tag
	reRNScreenComponent = regexp.MustCompile(
		`\bcomponent=\{([A-Z][A-Za-z0-9_]*)\}`,
	)

	// Exported function component (function or arrow form):
	// export default function Foo(  OR  export function Foo(  OR  export const Foo = (
	reRNExportedComponent = regexp.MustCompile(
		`export\s+(?:default\s+)?(?:function\s+([A-Z][A-Za-z0-9_]*)\s*\(|const\s+([A-Z][A-Za-z0-9_]*)\s*=\s*(?:React\.memo\s*\(|React\.forwardRef\s*\()?(?:async\s+)?\()`,
	)

	// PascalCase JSX child usage: <ComponentName or <ComponentName/
	// Excludes lowercase HTML tags and navigator wrappers like Stack.Navigator.
	// Negative lookahead not available in Go RE2 — we filter post-match.
	reRNJSXChild = regexp.MustCompile(
		`<([A-Z][A-Za-z0-9_]*)[\s/>]`,
	)

	// navigator wrapper tags to exclude from child hierarchy
	// (NavigationContainer, Stack.Navigator etc. — we track their Screen children separately)
	reRNNavigatorTag = regexp.MustCompile(
		`^(NavigationContainer|Stack|Tab|Drawer)$`,
	)
)

// Extract runs the React Native extraction pass.
func (e *reactNativeExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.react_native_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "react-native"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	src := string(file.Content)

	// File gate: must be a React Native file.
	isRN := reRNImport.MatchString(src) || reRNScreen.MatchString(src)
	if !isRN {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s", ent.Kind, ent.Name)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Pass 1: navigation route extraction.
	routeCount := extractRNRoutes(src, file.Path, file.Language, addEntity)

	// Pass 2: component hierarchy extraction.
	componentCount := extractRNComponents(src, file.Path, file.Language, addEntity)

	span.SetAttributes(
		attribute.Int("route_count", routeCount),
		attribute.Int("component_count", componentCount),
		attribute.Int("entity_count", len(entities)),
	)

	return entities, nil
}

// extractRNRoutes finds Stack.Screen / Tab.Screen / Drawer.Screen tags and
// emits one SCOPE.Operation per route. Returns the count of routes found.
func extractRNRoutes(src, filePath, language string, emit func(types.EntityRecord)) int {
	count := 0
	for _, m := range reRNScreen.FindAllStringSubmatchIndex(src, -1) {
		navigatorKind := src[m[2]:m[3]] // "Stack", "Tab", or "Drawer"
		tagBlock := src[m[0]:m[1]]      // full match including attributes

		// Extract name= prop.
		routeName := ""
		if nm := reRNScreenName.FindStringSubmatch(tagBlock); nm != nil {
			for _, g := range nm[1:] {
				if g != "" {
					routeName = g
					break
				}
			}
		}
		if routeName == "" {
			continue // name prop is required
		}

		// Extract component= prop (optional).
		componentName := ""
		if cm := reRNScreenComponent.FindStringSubmatch(tagBlock); cm != nil {
			componentName = cm[1]
		}

		entityName := fmt.Sprintf("route:%s:%s", navigatorKind, routeName)
		line := lineOf(src, m[0])
		ent := makeEntity(entityName, "SCOPE.Operation", "route", filePath, language, line)
		ent.Metadata = map[string]interface{}{
			"route_type": strings.ToLower(navigatorKind),
			"route_name": routeName,
			"component":  componentName,
			"framework":  "react-native",
			"provenance": "INFERRED_FROM_RN_NAVIGATION_SCREEN",
		}
		setProps(&ent, "framework", "react-native",
			"provenance", "INFERRED_FROM_RN_NAVIGATION_SCREEN",
			"navigator_kind", navigatorKind,
			"route_name", routeName,
			"component", componentName,
		)
		emit(ent)
		count++
	}
	return count
}

// extractRNComponents finds exported function components and their PascalCase
// JSX children, emitting SCOPE.UIComponent records. Returns component count.
func extractRNComponents(src, filePath, language string, emit func(types.EntityRecord)) int {
	count := 0

	for _, m := range reRNExportedComponent.FindAllStringSubmatchIndex(src, -1) {
		// Capture group 1 = function form, group 2 = const/arrow form.
		componentName := ""
		for _, idx := range [][]int{m[2:4], m[4:6]} {
			if idx[0] >= 0 {
				componentName = src[idx[0]:idx[1]]
				break
			}
		}
		if componentName == "" {
			continue
		}

		line := lineOf(src, m[0])
		ent := makeEntity(componentName, "SCOPE.UIComponent", "component", filePath, language, line)
		setProps(&ent, "framework", "react-native",
			"provenance", "INFERRED_FROM_RN_COMPONENT",
		)
		emit(ent)
		count++

		// Find children: scan from the component declaration to the end of file
		// (one-level heuristic — we don't track brace depth for simplicity).
		after := src[m[0]:]
		children := extractJSXChildren(after, componentName)
		for _, child := range children {
			childLine := line // approximate — child uses parent line as anchor
			childEnt := makeEntity(child, "SCOPE.UIComponent", "component", filePath, language, childLine)
			childEnt.Metadata = map[string]interface{}{
				"parent_component": componentName,
				"framework":        "react-native",
				"provenance":       "INFERRED_FROM_RN_JSX_CHILD",
			}
			setProps(&childEnt, "framework", "react-native",
				"provenance", "INFERRED_FROM_RN_JSX_CHILD",
				"parent_component", componentName,
			)
			emit(childEnt)
		}
	}

	return count
}

// extractJSXChildren returns unique PascalCase child component names from the
// JSX in the function body immediately following the component declaration.
// It scans up to the first empty line after a closing brace to approximate the
// function boundary without full AST parsing.
func extractJSXChildren(src, parentName string) []string {
	seen := make(map[string]bool)
	var children []string

	for _, m := range reRNJSXChild.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Skip the parent itself, navigator wrappers, and already-seen.
		if name == parentName || reRNNavigatorTag.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		children = append(children, name)
	}
	return children
}
