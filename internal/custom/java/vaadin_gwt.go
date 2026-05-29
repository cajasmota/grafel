package java

import (
	"regexp"
	"strings"
)

// Vaadin + GWT custom extractor.
//
// Vaadin is a server-side Java UI framework where components are plain Java
// objects — there is no client-side hydration, SSR, or static generation.
// GWT (Google Web Toolkit) compiles Java to client-side JavaScript; the Java
// source lives on the server side but the output runs in the browser. GWT RPC
// is a Java-to-Java call protocol.
//
// Coverage cells delivered (#3091):
//
// Vaadin:
//   - component_extraction  → partial  (@Route classes, component superclasses)
//   - router_pattern        → partial  (@Route("path") value extraction)
//   - data_fetching         → partial  (DataProvider, @GridColumn, addColumn)
//
// GWT:
//   - component_extraction  → partial  (@UiTemplate / UiBinder widget classes)
//   - router_pattern        → partial  (PlaceController, History.newItem())
//
// Not-applicable cells are recorded in the registry; see C-flip shell loop
// in the commit that introduced this file.

// ─── framework gates ────────────────────────────────────────────────────────

var vaadinFrameworks = map[string]bool{
	"vaadin": true,
}

var gwtFrameworks = map[string]bool{
	"gwt": true,
}

func vaadinFrameworkMatches(fw string) bool {
	return vaadinFrameworks[fw] || strings.HasPrefix(fw, "vaadin")
}

func gwtFrameworkMatches(fw string) bool {
	return gwtFrameworks[fw] || strings.HasPrefix(fw, "gwt")
}

// ─── Vaadin regexps ─────────────────────────────────────────────────────────

var (
	// @Route("path") annotation on a class — router_pattern + component_extraction.
	vaadinRouteAnnotationRE = regexp.MustCompile(
		`(?s)@Route\s*\(\s*(?:value\s*=\s*)?"([^"]*)"\s*\)` +
			`[^{]*?class\s+(\w+)`)

	// @Route with no explicit path (default = "") on a class.
	vaadinRouteNoPathRE = regexp.MustCompile(
		`(?s)@Route\s*\(\s*\)[^{]*?class\s+(\w+)`)

	// Class extending standard Vaadin component superclasses.
	vaadinComponentClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(?:VerticalLayout|HorizontalLayout|Div|Composite|Component|` +
			`FormLayout|Grid|TextField|Button|Dialog|Notification|` +
			`PolymerTemplate|LitTemplate|RouterLayout)\b`)

	// DataProvider reference — data_fetching.
	vaadinDataProviderRE = regexp.MustCompile(
		`(?m)DataProvider\s*\.\s*(?:of|ofCollection|fromCallbacks|fromItems|` +
			`withConfigurableFilter)\s*\(`)

	// @GridColumn annotation on a field — data_fetching.
	vaadinGridColumnRE = regexp.MustCompile(
		`(?m)@GridColumn\b`)

	// grid.addColumn / grid.setItems — data_fetching.
	vaadinGridAddColumnRE = regexp.MustCompile(
		`(?m)\b(?:addColumn|setDataProvider|setItems)\s*\(`)
)

// ─── GWT regexps ────────────────────────────────────────────────────────────

var (
	// @UiTemplate("Foo.ui.xml") on a widget class — component_extraction.
	gwtUiTemplateRE = regexp.MustCompile(
		`(?s)@UiTemplate\s*\(\s*"([^"]*)"\s*\)[^{]*?class\s+(\w+)`)

	// Class extending Composite or Widget (UiBinder pattern) without @UiTemplate.
	gwtWidgetClassRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`(?:Composite|Widget|SimplePanel|DockLayoutPanel|` +
			`VerticalPanel|HorizontalPanel|FlowPanel|HTMLPanel|` +
			`DialogBox|PopupPanel|TabPanel|StackPanel)\b`)

	// interface Foo extends EntryPoint — GWT module entry points.
	gwtEntryPointRE = regexp.MustCompile(
		`(?m)(?:public\s+)?class\s+(\w+)\s+implements\s+[^{]*\bEntryPoint\b`)

	// History.newItem("token") — router_pattern.
	gwtHistoryNewItemRE = regexp.MustCompile(
		`(?m)History\s*\.\s*newItem\s*\(\s*"([^"]*)"\s*`)

	// PlaceController.goTo — router_pattern.
	gwtPlaceControllerRE = regexp.MustCompile(
		`(?m)\bplaceController\s*\.\s*goTo\s*\(`)

	// RemoteServiceServlet implementation — RPC taint marker.
	gwtRPCServletRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+extends\s+` +
			`RemoteServiceServlet\b`)
)

// ─── ExtractVaadin ───────────────────────────────────────────────────────────

// ExtractVaadin runs the Vaadin extractor.
// Delivers partial coverage for: component_extraction, router_pattern, data_fetching.
func ExtractVaadin(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !vaadinFrameworkMatches(ctx.Framework) {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// --- @Route("path") classes → component + route entities ---
	for _, m := range vaadinRouteAnnotationRE.FindAllStringSubmatchIndex(source, -1) {
		path := source[m[2]:m[3]]
		clsName := source[m[4]:m[5]]
		compRef := "scope:uicomponent:vaadin_route:" + fp + ":" + clsName
		routeRef := "scope:route:vaadin:" + fp + ":" + clsName

		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.UIComponent", Subtype: "component",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_VAADIN_ROUTE", Ref: compRef,
			Properties: map[string]any{
				"component_kind": "route_view", "route_path": path,
				"framework": "vaadin",
			},
		}) {
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: path, Kind: "SCOPE.Route", Subtype: "page",
				SourceFile: fp,
				LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_VAADIN_ROUTE", Ref: routeRef,
				Properties: map[string]any{
					"http_method": "GET", "route_path": path,
					"component_class": clsName, "framework": "vaadin",
				},
			})
			addRel(&result, seenRels, Relationship{
				SourceRef: compRef, TargetRef: routeRef, RelationshipType: "ROUTES_TO",
				Properties: map[string]string{"route_path": path},
			})
		}
	}

	// --- @Route() with no path (empty default) ---
	for _, m := range vaadinRouteNoPathRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		compRef := "scope:uicomponent:vaadin_route:" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.UIComponent", Subtype: "component",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_VAADIN_ROUTE", Ref: compRef,
			Properties: map[string]any{
				"component_kind": "route_view", "route_path": "",
				"framework": "vaadin",
			},
		})
	}

	// --- Component superclass extension → component_extraction ---
	for _, m := range vaadinComponentClassRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		compRef := "scope:uicomponent:vaadin_component:" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.UIComponent", Subtype: "component",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_VAADIN_COMPONENT", Ref: compRef,
			Properties: map[string]any{
				"component_kind": "layout_or_widget", "framework": "vaadin",
			},
		})
	}

	// --- DataProvider / addColumn / setItems → data_fetching ---
	dataFetchHit := false
	if vaadinDataProviderRE.MatchString(source) {
		dataFetchHit = true
	}
	if vaadinGridColumnRE.MatchString(source) {
		dataFetchHit = true
	}
	if vaadinGridAddColumnRE.MatchString(source) {
		dataFetchHit = true
	}
	if dataFetchHit {
		hostClass := findEnclosingClass(source, 0)
		if hostClass == "" {
			hostClass = "unknown"
		}
		ref := "scope:operation:vaadin_data_fetch:" + fp + ":" + hostClass
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: hostClass + "::data_fetch", Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  1, LineEnd: 1,
			Provenance: "INFERRED_FROM_VAADIN_DATA_FETCH", Ref: ref,
			Properties: map[string]any{"framework": "vaadin"},
		})
	}

	_ = seenRels
	return result
}

// ─── ExtractGWT ──────────────────────────────────────────────────────────────

// ExtractGWT runs the GWT extractor.
// Delivers partial coverage for: component_extraction, router_pattern.
func ExtractGWT(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !gwtFrameworkMatches(ctx.Framework) {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// --- @UiTemplate widget classes → component_extraction ---
	for _, m := range gwtUiTemplateRE.FindAllStringSubmatchIndex(source, -1) {
		template := source[m[2]:m[3]]
		clsName := source[m[4]:m[5]]
		ref := "scope:uicomponent:gwt_widget:" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.UIComponent", Subtype: "widget",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_GWT_UI_TEMPLATE", Ref: ref,
			Properties: map[string]any{
				"component_kind": "uibinder_widget", "ui_template": template,
				"framework": "gwt",
			},
		})
	}

	// --- Widget superclass extensions → component_extraction ---
	for _, m := range gwtWidgetClassRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		ref := "scope:uicomponent:gwt_widget:" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.UIComponent", Subtype: "widget",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_GWT_WIDGET_CLASS", Ref: ref,
			Properties: map[string]any{
				"component_kind": "gwt_widget", "framework": "gwt",
			},
		})
	}

	// --- EntryPoint implementations ---
	for _, m := range gwtEntryPointRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		ref := "scope:component:gwt_entrypoint:" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Component", Subtype: "entrypoint",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_GWT_ENTRY_POINT", Ref: ref,
			Properties: map[string]any{
				"component_kind": "entry_point", "framework": "gwt",
			},
		})
	}

	// --- History.newItem("token") → router_pattern ---
	for _, m := range gwtHistoryNewItemRE.FindAllStringSubmatchIndex(source, -1) {
		token := source[m[2]:m[3]]
		hostClass := findEnclosingClass(source, m[0])
		if hostClass == "" {
			hostClass = "unknown"
		}
		routeRef := "scope:route:gwt:" + fp + ":" + token
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: token, Kind: "SCOPE.Route", Subtype: "history_token",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_GWT_HISTORY", Ref: routeRef,
			Properties: map[string]any{
				"route_path": token, "host_class": hostClass, "framework": "gwt",
			},
		})
	}

	// --- PlaceController.goTo → router_pattern (presence marker) ---
	if gwtPlaceControllerRE.MatchString(source) {
		hostClass := findEnclosingClass(source, 0)
		if hostClass == "" {
			hostClass = "unknown"
		}
		ref := "scope:operation:gwt_place_nav:" + fp + ":" + hostClass
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: hostClass + "::placeController.goTo", Kind: "SCOPE.Operation", Subtype: "navigation",
			SourceFile: fp,
			LineStart:  1, LineEnd: 1,
			Provenance: "INFERRED_FROM_GWT_PLACE_CONTROLLER", Ref: ref,
			Properties: map[string]any{"framework": "gwt"},
		})
	}

	// --- RemoteServiceServlet → RPC taint marker ---
	for _, m := range gwtRPCServletRE.FindAllStringSubmatchIndex(source, -1) {
		clsName := source[m[2]:m[3]]
		ref := "scope:service:gwt_rpc:" + fp + ":" + clsName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: clsName, Kind: "SCOPE.Service", Subtype: "rpc_servlet",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_GWT_RPC_SERVLET", Ref: ref,
			Properties: map[string]any{
				"component_kind": "gwt_rpc_servlet", "framework": "gwt",
			},
		})
	}

	_ = seenRels
	return result
}
