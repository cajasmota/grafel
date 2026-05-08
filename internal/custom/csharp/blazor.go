package csharp

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
	extractor.Register("custom_csharp_blazor", &blazorExtractor{})
}

type blazorExtractor struct{}

func (e *blazorExtractor) Language() string { return "custom_csharp_blazor" }

var (
	reBlazorPage = regexp.MustCompile(
		`(?m)^@page\s+"([^"]+)"`,
	)
	reBlazorInject = regexp.MustCompile(
		`(?m)^@inject\s+(\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
	reBlazorCodeMethod = regexp.MustCompile(
		`(?m)(?:private|protected|public|internal)?\s+(?:async\s+)?(?:Task|void|[\w<>\[\]]+)\s+(\w+)\s*\(`,
	)
	reBlazorComponentTag = regexp.MustCompile(
		`<([A-Z][A-Za-z0-9_]*)(?:\s+[^>]*)?>`,
	)
	reBlazorParameter = regexp.MustCompile(
		`\[Parameter\]\s*(?:public\s+)?(?:\w+(?:<[^>]+>)?)\s+(\w+)`,
	)
	reBlazorLayout = regexp.MustCompile(
		`(?m)^@layout\s+(\w+)`,
	)
	reBlazorInherits = regexp.MustCompile(
		`(?m)^@inherits\s+(\w+)`,
	)
)

// blazorBuiltinComponents are Blazor/HTML built-in components not emitted as entities.
var blazorBuiltinComponents = map[string]bool{
	"EditForm": true, "InputText": true, "InputNumber": true, "InputDate": true,
	"InputCheckbox": true, "InputSelect": true, "InputFile": true,
	"ValidationSummary": true, "ValidationMessage": true,
	"NavLink": true, "NavMenu": true, "AuthorizeView": true,
	"CascadingAuthenticationState": true, "Router": true, "RouteView": true,
	"FocusOnNavigate": true, "Virtualize": true, "DynamicComponent": true,
}

func (e *blazorExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.blazor_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "blazor"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. @page routes -> SCOPE.Operation/endpoint
	for _, m := range reBlazorPage.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		ent := makeEntity(route, "SCOPE.Operation", "endpoint", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_PAGE",
			"route_path", route)
		add(ent)
	}

	// 2. @inject -> SCOPE.Component (injected service)
	for _, m := range reBlazorInject.FindAllStringSubmatchIndex(src, -1) {
		serviceType := src[m[2]:m[3]]
		ent := makeEntity(serviceType, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_INJECT",
			"service_type", serviceType)
		add(ent)
	}

	// 3. @code block methods -> SCOPE.Operation/function
	for _, m := range reBlazorCodeMethod.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_CODE_METHOD")
		add(ent)
	}

	// 4. PascalCase component tags -> SCOPE.UIComponent
	for _, m := range reBlazorComponentTag.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if blazorBuiltinComponents[name] {
			continue
		}
		ent := makeEntity(name, "SCOPE.UIComponent", "component", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_COMPONENT_REF")
		add(ent)
	}

	// 5. [Parameter] properties -> SCOPE.Pattern
	for _, m := range reBlazorParameter.FindAllStringSubmatchIndex(src, -1) {
		paramName := src[m[2]:m[3]]
		ent := makeEntity("param:"+paramName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_PARAMETER",
			"parameter_name", paramName)
		add(ent)
	}

	// 6. @layout -> SCOPE.Component
	for _, m := range reBlazorLayout.FindAllStringSubmatchIndex(src, -1) {
		layoutName := src[m[2]:m[3]]
		ent := makeEntity("layout:"+layoutName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_LAYOUT")
		add(ent)
	}

	// 7. @inherits -> SCOPE.Component
	for _, m := range reBlazorInherits.FindAllStringSubmatchIndex(src, -1) {
		baseName := src[m[2]:m[3]]
		ent := makeEntity("inherits:"+baseName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor", "provenance", "INFERRED_FROM_BLAZOR_INHERITS")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
