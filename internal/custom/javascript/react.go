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
	extreg.Register("custom_js_react", &reactExtractor{})
}

type reactExtractor struct{}

func (e *reactExtractor) Language() string { return "custom_js_react" }

var (
	// Exported function component: export function FooBar( / export default function FooBar(
	reReactExportFunction = regexp.MustCompile(
		`export\s+(?:default\s+)?function\s+([A-Z][A-Za-z0-9_]*)\s*\(`,
	)
	// Exported arrow/const component: export const FooBar = (...)
	reReactExportConst = regexp.MustCompile(
		`export\s+const\s+([A-Z][A-Za-z0-9_]*)\s*=\s*(?:React\.memo\s*\(|React\.forwardRef\s*\()?(?:async\s+)?\(`,
	)
	// Class components: extends Component / extends React.Component / extends PureComponent
	reReactClassComponent = regexp.MustCompile(
		`class\s+([A-Z][A-Za-z0-9_]*)\s+extends\s+(?:React\.)?(?:Component|PureComponent)\b`,
	)
	// Custom hooks: export const useXxx / export function useXxx
	reReactHook = regexp.MustCompile(
		`export\s+(?:(?:const|function)\s+)(use[A-Z][A-Za-z0-9_]*)`,
	)
	// HOC: export default connect(...)(Component) / export default withRouter(Component)
	reReactHOC = regexp.MustCompile(
		`export\s+default\s+\w+\([^)]*\)\s*\(\s*([A-Z][A-Za-z0-9_]*)\s*\)`,
	)
	// createContext
	reReactCreateContext = regexp.MustCompile(
		`(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*(?:Context)?)\s*=\s*(?:React\.)?createContext\s*\(`,
	)
	// useContext(SomeContext) call sites
	reReactUseContext = regexp.MustCompile(
		`useContext\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`,
	)
	// JSX guard: presence of JSX in file
	// JSX guard: presence of JSX in file (matches component tags <Foo, HTML tags <div, or createElement).
	reReactJSXPresence = regexp.MustCompile(
		`(?:<[A-Za-z][A-Za-z0-9_]*[\s/>]|React\.createElement\s*\()`,
	)
)

func (e *reactExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.react_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "react"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	hasJSX := reReactJSXPresence.MatchString(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Functional components (requires JSX guard)
	if hasJSX {
		for _, m := range reReactExportFunction.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "react", "component_type", "function",
				"provenance", "INFERRED_FROM_REACT_COMPONENT")
			addEntity(ent)
		}
		for _, m := range reReactExportConst.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "react", "component_type", "arrow",
				"provenance", "INFERRED_FROM_REACT_COMPONENT")
			addEntity(ent)
		}
	}

	// Class components
	for _, m := range reReactClassComponent.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "react", "component_type", "class",
			"provenance", "INFERRED_FROM_REACT_CLASS_COMPONENT")
		addEntity(ent)
	}

	// Custom hooks
	for _, m := range reReactHook.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "hook", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "react", "provenance", "INFERRED_FROM_REACT_HOOK")
		addEntity(ent)
	}

	// HOC
	for _, m := range reReactHOC.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "hoc", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "react", "provenance", "INFERRED_FROM_REACT_HOC")
		addEntity(ent)
	}

	// createContext
	for _, m := range reReactCreateContext.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "context", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "react", "provenance", "INFERRED_FROM_REACT_CONTEXT")
		addEntity(ent)
	}

	// useContext call sites
	for _, m := range reReactUseContext.FindAllStringSubmatchIndex(src, -1) {
		ctxName := src[m[2]:m[3]]
		name := "useContext:" + ctxName
		ent := makeEntity(name, "SCOPE.Operation", "context_use", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "react", "context_name", ctxName,
			"provenance", "INFERRED_FROM_REACT_USE_CONTEXT")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
