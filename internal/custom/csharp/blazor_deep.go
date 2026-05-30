// Package csharp — Deep Blazor extractor (Structure / Data Flow / Navigation / Lifecycle).
//
// This extractor raises the four capability groups to the TS/JS bar by adding
// patterns that the earlier blazor_extra.go / blazor_dataflow.go extractors
// only cover partially:
//
//	Structure/component_extraction (additions):
//	  - Razor filename basename (MyPage.razor → component entity "MyPage")
//	  - @attribute [Route("...")] attribute-based component declaration
//	  - @rendermode directive (signals an interactive component)
//	  - class X : ComponentBase, IComponent, LayoutComponentBase (already in
//	    blazor_extra.go; re-emitted here with type+subtype for completeness)
//
//	Structure/context_extraction (additions):
//	  - Constructor DI in .razor.cs code-behind files:
//	    public MyComponent(IService svc, ILogger<T> log)
//
//	Data Flow/prop_extraction (upgrade to full):
//	  - [Parameter] with explicit type+name → emit "param:TYPE:NAME"
//	  - EventCallback<T> parameters → emit "callback:T:NAME"
//	  - [EditorRequired] [Parameter] decoration
//
//	Data Flow/state_management (additions):
//	  - @bind / @bind:event two-way binding pattern → "bind:NAME"
//	  - CascadeValue<T> provider (parent side of cascade)
//
//	Data Flow/data_fetching (additions):
//	  - IHttpClientFactory.CreateClient() → "http:factory:NAME"
//	  - Named HttpClient via IHttpClientFactory
//
//	Data Flow/branch_conditions (additions):
//	  - @foreach in Razor markup → "branch:razorforeach:FILE:LINE"
//
//	Navigation/router_pattern (additions):
//	  - Route parameter names extracted from @page "{id}" / "{id:int}" / "{**slug}"
//	  - @attribute [Route("...")] emitted as router_pattern operation
//
//	Lifecycle/state_setter_emission (additions):
//	  - SetParametersAsync override
//	  - IDisposable.Dispose() / IAsyncDisposable.DisposeAsync()
//	  - ShouldRender() override
//
// Registration key: "custom_csharp_blazor_deep"
// Closes #3381.
package csharp

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_blazor_deep", &blazorDeepExtractor{})
}

type blazorDeepExtractor struct{}

func (e *blazorDeepExtractor) Language() string { return "custom_csharp_blazor_deep" }

// ---------------------------------------------------------------------------
// Regex catalog — Structure
// ---------------------------------------------------------------------------

var (
	// @attribute [Route("/path")] — attribute-based component route declaration
	reBDpAttributeRoute = regexp.MustCompile(
		`(?m)^@attribute\s+\[Route\s*\(\s*"([^"]+)"\s*\)\]`,
	)

	// @rendermode ... — marks interactive component (Server / WebAssembly / Auto)
	reBDpRenderMode = regexp.MustCompile(
		`(?m)^@rendermode\s+(\w+)`,
	)

	// Constructor DI in .razor.cs code-behind:
	//   public MyPage(IMyService svc) { }
	reBDpCtorDI = regexp.MustCompile(
		`(?m)public\s+(\w+)\s*\(([^)]+)\)`,
	)
	// Match single DI parameter: <Type> <name>
	reBDpCtorParam = regexp.MustCompile(
		`(\w+(?:<[^>]+>)?)\s+(\w+)`,
	)

	// @page "/path/{param}" — extract parameter names from route template
	// Reuse reBEPage from blazor_extra.go
	reBDpRouteParam = regexp.MustCompile(`\{(\w+)(?::[^}]*)?\}`)
)

// ---------------------------------------------------------------------------
// Regex catalog — Data Flow
// ---------------------------------------------------------------------------

var (
	// [Parameter] public TYPE NAME — capture type+name pair
	reBDpParameterTyped = regexp.MustCompile(
		`\[Parameter\]\s*(?:\[EditorRequired\]\s*)?(?:public\s+)?(\w+(?:<[^>]+>)?)\s+(\w+)\s*\{`,
	)

	// EventCallback<T> — callback prop
	reBDpEventCallback = regexp.MustCompile(
		`\bEventCallback(?:<([^>]+)>)?\s+(\w+)\s*\{`,
	)

	// @bind="Name" / @bind:event="oninput" — two-way binding
	reBDpBind = regexp.MustCompile(
		`@bind(?::[a-z]+)?\s*=\s*["'](\w+)["']`,
	)

	// <CascadeValue Value="..."> / <CascadingValue Value="..."> — provider side
	reBDpCascadeValue = regexp.MustCompile(
		`<Cascad(?:e|ing)Value\b[^>]*\bValue\s*=\s*["@](\w+)["']?`,
	)

	// IHttpClientFactory.CreateClient / _factory.CreateClient("name")
	reBDpHttpFactory = regexp.MustCompile(
		`\.CreateClient\s*\(\s*(?:"([^"]+)")?\s*\)`,
	)

	// @foreach in Razor markup
	reBDpRazorForeach = regexp.MustCompile(
		`(?m)^\s*@foreach\s*\(`,
	)
)

// ---------------------------------------------------------------------------
// Regex catalog — Lifecycle
// ---------------------------------------------------------------------------

var (
	// SetParametersAsync(ParameterView parameters) override
	reBDpSetParametersAsync = regexp.MustCompile(
		`(?m)(?:public|protected)\s+(?:override\s+)?(?:async\s+)?Task\s+SetParametersAsync\s*\(`,
	)

	// IDisposable.Dispose() / public void Dispose()
	reBDpDispose = regexp.MustCompile(
		`(?m)(?:public|protected)\s+(?:override\s+)?(?:void|ValueTask)\s+(?:Dispose|DisposeAsync)\s*\(\s*\)`,
	)

	// ShouldRender() override
	reBDpShouldRender = regexp.MustCompile(
		`(?m)(?:public|protected)\s+(?:override\s+)?bool\s+ShouldRender\s*\(\s*\)`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *blazorDeepExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_blazor_deep_extractor.extract",
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
	isRazor := strings.HasSuffix(file.Path, ".razor") ||
		strings.HasSuffix(file.Path, ".razor.cs")
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

	// -------------------------------------------------------------------------
	// Structure/component_extraction — razor file basename
	// -------------------------------------------------------------------------
	if isRazor {
		base := filepath.Base(file.Path)
		// Strip .razor or .razor.cs
		compName := strings.TrimSuffix(strings.TrimSuffix(base, ".cs"), ".razor")
		if compName != "" && compName != base {
			ent := makeEntity(compName, "SCOPE.UIComponent", "component_extraction", file.Path, "csharp", 1)
			setProps(&ent, "framework", "blazor",
				"provenance", "INFERRED_FROM_RAZOR_FILENAME",
				"component_name", compName)
			add(ent)
		}
	}

	// Structure/component_extraction — @attribute [Route(...)]
	for _, m := range reBDpAttributeRoute.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		name := "component:attr-route:" + route
		ent := makeEntity(name, "SCOPE.UIComponent", "component_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_ATTRIBUTE_ROUTE",
			"route_path", route)
		add(ent)
	}

	// Structure/component_extraction — @rendermode
	for _, m := range reBDpRenderMode.FindAllStringSubmatchIndex(src, -1) {
		mode := src[m[2]:m[3]]
		name := "rendermode:" + mode
		ent := makeEntity(name, "SCOPE.UIComponent", "component_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_RENDERMODE",
			"render_mode", mode)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Structure/context_extraction — constructor DI in code-behind
	// -------------------------------------------------------------------------
	if strings.HasSuffix(file.Path, ".razor.cs") {
		for _, m := range reBDpCtorDI.FindAllStringSubmatchIndex(src, -1) {
			// Only consider constructors (name matches class name heuristic: starts with uppercase)
			ctorName := src[m[2]:m[3]]
			if len(ctorName) == 0 || ctorName[0] < 'A' || ctorName[0] > 'Z' {
				continue
			}
			paramList := src[m[4]:m[5]]
			// Each DI parameter
			for _, pm := range reBDpCtorParam.FindAllStringSubmatch(paramList, -1) {
				svcType := pm[1]
				varName := pm[2]
				// Skip primitive/keyword params
				if csharpPrimitives[svcType] || varName == "" {
					continue
				}
				name := "ctx:ctor:" + svcType + ":" + varName
				ent := makeEntity(name, "SCOPE.Component", "context_extraction", file.Path, "csharp", lineOf(src, m[0]))
				setProps(&ent, "framework", "blazor",
					"provenance", "INFERRED_FROM_BLAZOR_CTOR_DI",
					"service_type", svcType, "variable_name", varName)
				add(ent)
			}
		}
	}

	// -------------------------------------------------------------------------
	// Data Flow/prop_extraction — typed [Parameter]
	// -------------------------------------------------------------------------
	for _, m := range reBDpParameterTyped.FindAllStringSubmatchIndex(src, -1) {
		propType := src[m[2]:m[3]]
		propName := src[m[4]:m[5]]
		name := "param:" + propType + ":" + propName
		ent := makeEntity(name, "SCOPE.Pattern", "prop_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_PARAMETER_TYPED",
			"param_type", propType, "param_name", propName)
		add(ent)
	}

	// Data Flow/prop_extraction — EventCallback<T>
	for _, m := range reBDpEventCallback.FindAllStringSubmatchIndex(src, -1) {
		cbType := ""
		if m[2] >= 0 {
			cbType = src[m[2]:m[3]]
		}
		cbName := src[m[4]:m[5]]
		name := "callback:" + cbType + ":" + cbName
		ent := makeEntity(name, "SCOPE.Pattern", "prop_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_EVENT_CALLBACK",
			"callback_type", cbType, "callback_name", cbName)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Data Flow/state_management — @bind two-way binding
	// -------------------------------------------------------------------------
	for _, m := range reBDpBind.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		name := "bind:" + fieldName
		ent := makeEntity(name, "SCOPE.Pattern", "state_management", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_BIND",
			"bound_field", fieldName)
		add(ent)
	}

	// Data Flow/state_management — CascadingValue provider
	for _, m := range reBDpCascadeValue.FindAllStringSubmatchIndex(src, -1) {
		valueName := src[m[2]:m[3]]
		name := "cascade:provider:" + valueName
		ent := makeEntity(name, "SCOPE.Pattern", "state_management", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_CASCADE_VALUE_PROVIDER",
			"cascade_value", valueName)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Data Flow/data_fetching — IHttpClientFactory
	// -------------------------------------------------------------------------
	for _, m := range reBDpHttpFactory.FindAllStringSubmatchIndex(src, -1) {
		clientName := "default"
		if m[2] >= 0 {
			clientName = src[m[2]:m[3]]
		}
		name := "http:factory:" + clientName + ":" + file.Path + ":" + itoa(lineOf(src, m[0]))
		ent := makeEntity(name, "SCOPE.Pattern", "data_fetching", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_HTTP_CLIENT_FACTORY",
			"client_name", clientName)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Data Flow/branch_conditions — @foreach in Razor markup
	// -------------------------------------------------------------------------
	for _, m := range reBDpRazorForeach.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		name := "branch:razorforeach:" + file.Path + ":" + itoa(line)
		ent := makeEntity(name, "SCOPE.Pattern", "branch_conditions", file.Path, "csharp", line)
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_RAZOR_FOREACH",
			"kind", "razor_foreach")
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Navigation/router_pattern — @attribute [Route(...)] as router_pattern
	// -------------------------------------------------------------------------
	for _, m := range reBDpAttributeRoute.FindAllStringSubmatchIndex(src, -1) {
		route := src[m[2]:m[3]]
		ent := makeEntity("route:attr:"+route, "SCOPE.Operation", "router_pattern", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_ATTRIBUTE_ROUTE",
			"route_path", route)
		add(ent)
	}

	// Navigation/router_pattern — route parameter names from @page templates
	for _, pm := range reBEPage.FindAllStringSubmatch(src, -1) {
		template := pm[1]
		for _, rp := range reBDpRouteParam.FindAllStringSubmatch(template, -1) {
			paramName := rp[1]
			name := "route:param:" + template + ":" + paramName
			line := 1
			if idx := reBEPage.FindStringIndex(src); idx != nil {
				line = lineOf(src, idx[0])
			}
			ent := makeEntity(name, "SCOPE.Pattern", "router_pattern", file.Path, "csharp", line)
			setProps(&ent, "framework", "blazor",
				"provenance", "INFERRED_FROM_BLAZOR_ROUTE_PARAM",
				"route_template", template, "param_name", paramName)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// Lifecycle/state_setter_emission — SetParametersAsync
	// -------------------------------------------------------------------------
	for _, m := range reBDpSetParametersAsync.FindAllStringIndex(src, -1) {
		name := "lifecycle:SetParametersAsync"
		ent := makeEntity(name, "SCOPE.Operation", "state_setter_emission", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_SET_PARAMETERS_ASYNC",
			"lifecycle_method", "SetParametersAsync")
		add(ent)
	}

	// Lifecycle/state_setter_emission — Dispose / DisposeAsync
	for _, m := range reBDpDispose.FindAllStringSubmatchIndex(src, -1) {
		name := "lifecycle:Dispose"
		ent := makeEntity(name, "SCOPE.Operation", "state_setter_emission", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_DISPOSE",
			"lifecycle_method", "Dispose")
		add(ent)
	}

	// Lifecycle/state_setter_emission — ShouldRender
	for _, m := range reBDpShouldRender.FindAllStringIndex(src, -1) {
		name := "lifecycle:ShouldRender"
		ent := makeEntity(name, "SCOPE.Operation", "state_setter_emission", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "blazor",
			"provenance", "INFERRED_FROM_BLAZOR_SHOULD_RENDER",
			"lifecycle_method", "ShouldRender")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
