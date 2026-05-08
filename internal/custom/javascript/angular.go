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
	extreg.Register("custom_js_angular", &angularExtractor{})
}

type angularExtractor struct{}

func (e *angularExtractor) Language() string { return "custom_js_angular" }

var (
	reAngularNgModule = regexp.MustCompile(
		`@NgModule\s*\([^)]*(?:\([^)]*\)[^)]*)*\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reAngularComponent = regexp.MustCompile(
		`@Component\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reAngularSelector = regexp.MustCompile(
		`selector\s*:\s*['"]([^'"]+)['"]`,
	)
	reAngularDirective = regexp.MustCompile(
		`@Directive\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reAngularInjectableService = regexp.MustCompile(
		`@Injectable\s*\([^)]*\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reAngularPipe = regexp.MustCompile(
		`@Pipe\s*\([^@]*?\)\s*(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	reAngularInput = regexp.MustCompile(
		`@Input\s*\([^)]*\)\s+(\w+)`,
	)
	reAngularOutput = regexp.MustCompile(
		`@Output\s*\([^)]*\)\s+(\w+)`,
	)
	reAngularRoute = regexp.MustCompile(
		`path\s*:\s*['"]([^'"]*?)['"]`,
	)
	reAngularRouteComponent = regexp.MustCompile(
		`component\s*:\s*([A-Z][A-Za-z0-9_]*)`,
	)
	reAngularCanActivate = regexp.MustCompile(
		`(?:export\s+)?class\s+([A-Z][A-Za-z0-9_]*)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bCanActivate\b`,
	)
)

func (e *angularExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.angular_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "angular"),
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

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Subtype, ent.Name)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// @NgModule
	for _, m := range reAngularNgModule.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "module", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_NGMODULE")
		addEntity(ent)
	}

	// @Component
	for _, m := range reAngularComponent.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		// Try to extract selector from surrounding decorator text
		decoratorEnd := m[2]
		decoratorStart := strings.LastIndex(src[:decoratorEnd], "@Component")
		if decoratorStart >= 0 {
			decoratorBlock := src[decoratorStart:decoratorEnd]
			if sm := reAngularSelector.FindStringSubmatch(decoratorBlock); sm != nil {
				setProps(&ent, "selector", sm[1])
			}
		}
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_COMPONENT")
		addEntity(ent)
	}

	// @Directive
	for _, m := range reAngularDirective.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.UIComponent", "directive", file.Path, file.Language, lineOf(src, m[0]))
		decoratorEnd := m[2]
		decoratorStart := strings.LastIndex(src[:decoratorEnd], "@Directive")
		if decoratorStart >= 0 {
			decoratorBlock := src[decoratorStart:decoratorEnd]
			if sm := reAngularSelector.FindStringSubmatch(decoratorBlock); sm != nil {
				setProps(&ent, "selector", sm[1])
			}
		}
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_DIRECTIVE")
		addEntity(ent)
	}

	// @Injectable services
	for _, m := range reAngularInjectableService.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "service", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_INJECTABLE")
		addEntity(ent)
	}

	// @Pipe
	for _, m := range reAngularPipe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "pipe", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_PIPE")
		addEntity(ent)
	}

	// @Input() properties
	for _, m := range reAngularInput.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "input_property", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_INPUT")
		addEntity(ent)
	}

	// @Output() properties
	for _, m := range reAngularOutput.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "output_property", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_OUTPUT")
		addEntity(ent)
	}

	// Router routes (path: 'xxx', component: Xxx)
	routePaths := reAngularRoute.FindAllStringSubmatchIndex(src, -1)
	routeComponents := reAngularRouteComponent.FindAllStringSubmatchIndex(src, -1)
	// Pair up adjacent path+component definitions
	compIdx := 0
	for _, pm := range routePaths {
		routePath := src[pm[2]:pm[3]]
		// Find nearest component after this path definition
		for compIdx < len(routeComponents) && routeComponents[compIdx][0] < pm[0] {
			compIdx++
		}
		var compName string
		if compIdx < len(routeComponents) && routeComponents[compIdx][0]-pm[0] < 200 {
			compName = src[routeComponents[compIdx][2]:routeComponents[compIdx][3]]
		}
		name := routePath
		if compName != "" {
			name = fmt.Sprintf("%s->%s", routePath, compName)
		}
		ent := makeEntity(name, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, pm[0]))
		setProps(&ent, "framework", "angular", "route_path", routePath,
			"component", compName, "provenance", "INFERRED_FROM_ANGULAR_ROUTE")
		addEntity(ent)
	}

	// Route guards (CanActivate)
	for _, m := range reAngularCanActivate.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "guard", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "angular", "provenance", "INFERRED_FROM_ANGULAR_GUARD")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
