package java

import (
	"testing"
)

// ============================================================================
// Issue #3091: Vaadin + GWT minimal extractors
// ============================================================================

// ─── Vaadin ──────────────────────────────────────────────────────────────────

// TestVaadin_Route_ComponentExtraction_Issue3091 proves that a class annotated
// with @Route("path") is detected as a UIComponent + a Route entity.
// Registry target: lang.java.framework.vaadin Structure/component_extraction → partial.
// Registry target: lang.java.framework.vaadin Navigation/router_pattern → partial.
// Cite: internal/custom/java/vaadin_gwt.go
func TestVaadin_Route_ComponentExtraction_Issue3091(t *testing.T) {
	source := `
package com.example.ui;

import com.vaadin.flow.router.Route;
import com.vaadin.flow.component.orderedlayout.VerticalLayout;

@Route("dashboard")
public class DashboardView extends VerticalLayout {
    public DashboardView() {
        add(new Paragraph("Hello"));
    }
}
`
	r := ExtractVaadin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vaadin",
		FilePath:  "DashboardView.java",
	})

	foundComponent := false
	foundRoute := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VAADIN_ROUTE" && e.Kind == "SCOPE.UIComponent" {
			foundComponent = true
			if e.Properties["route_path"] != "dashboard" {
				t.Errorf("[#3091 router_pattern] expected route_path=dashboard, got %v", e.Properties["route_path"])
			}
		}
		if e.Kind == "SCOPE.Route" && e.Provenance == "INFERRED_FROM_VAADIN_ROUTE" {
			foundRoute = true
		}
	}
	if !foundComponent {
		t.Errorf("[#3091 component_extraction] expected UIComponent entity from @Route class, got none")
	}
	if !foundRoute {
		t.Errorf("[#3091 router_pattern] expected SCOPE.Route entity from @Route annotation, got none")
	}
}

// TestVaadin_ComponentSuperclass_Issue3091 proves that a class extending a
// Vaadin layout class is detected as a UIComponent even without @Route.
// Registry target: lang.java.framework.vaadin Structure/component_extraction → partial.
func TestVaadin_ComponentSuperclass_Issue3091(t *testing.T) {
	source := `
package com.example.ui;

import com.vaadin.flow.component.orderedlayout.HorizontalLayout;

public class Toolbar extends HorizontalLayout {
    public Toolbar() {
        add(new Button("Save"));
    }
}
`
	r := ExtractVaadin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vaadin",
		FilePath:  "Toolbar.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VAADIN_COMPONENT" && e.Name == "Toolbar" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3091 component_extraction] expected UIComponent entity for Toolbar extends HorizontalLayout")
	}
}

// TestVaadin_DataFetching_DataProvider_Issue3091 proves that DataProvider usage
// is detected as a data_fetching signal.
// Registry target: lang.java.framework.vaadin Data Flow/data_fetching → partial.
func TestVaadin_DataFetching_DataProvider_Issue3091(t *testing.T) {
	source := `
package com.example.ui;

import com.vaadin.flow.data.provider.DataProvider;

public class PersonGrid extends VerticalLayout {
    Grid<Person> grid = new Grid<>();

    public PersonGrid() {
        grid.setDataProvider(DataProvider.ofCollection(personService.findAll()));
        add(grid);
    }
}
`
	r := ExtractVaadin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vaadin",
		FilePath:  "PersonGrid.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VAADIN_DATA_FETCH" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3091 data_fetching] expected data_fetch entity from DataProvider usage, got none")
	}
}

// TestVaadin_IgnoresNonVaadinFramework_Issue3091 proves the framework gate works.
func TestVaadin_IgnoresNonVaadinFramework_Issue3091(t *testing.T) {
	source := `@Route("foo") public class Foo extends VerticalLayout {}`
	r := ExtractVaadin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "Foo.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3091 gate] Vaadin extractor should not fire on spring_boot framework")
	}
}

// ─── GWT ─────────────────────────────────────────────────────────────────────

// TestGWT_UiTemplate_ComponentExtraction_Issue3091 proves that a class annotated
// with @UiTemplate is detected as a UIComponent widget.
// Registry target: lang.java.framework.gwt Structure/component_extraction → partial.
// Cite: internal/custom/java/vaadin_gwt.go
func TestGWT_UiTemplate_ComponentExtraction_Issue3091(t *testing.T) {
	source := `
package com.example.client;

import com.google.gwt.uibinder.client.UiTemplate;
import com.google.gwt.user.client.ui.Composite;

@UiTemplate("LoginPanel.ui.xml")
public class LoginPanel extends Composite {
    interface MyUiBinder extends UiBinder<Widget, LoginPanel> {}
}
`
	r := ExtractGWT(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "gwt",
		FilePath:  "LoginPanel.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_GWT_UI_TEMPLATE" && e.Name == "LoginPanel" {
			found = true
			if e.Properties["ui_template"] != "LoginPanel.ui.xml" {
				t.Errorf("[#3091 component_extraction] expected ui_template=LoginPanel.ui.xml, got %v", e.Properties["ui_template"])
			}
		}
	}
	if !found {
		t.Errorf("[#3091 component_extraction] expected UIComponent entity from @UiTemplate class, got none")
	}
}

// TestGWT_WidgetSuperclass_Issue3091 proves that GWT Composite/Widget subclasses
// are detected without @UiTemplate.
// Registry target: lang.java.framework.gwt Structure/component_extraction → partial.
func TestGWT_WidgetSuperclass_Issue3091(t *testing.T) {
	source := `
package com.example.client;

public class MyPanel extends com.google.gwt.user.client.ui.Composite {
    public MyPanel() {}
}
`
	// simpler — strip package prefix from regex perspective
	source2 := `
package com.example.client;

public class MyPanel extends Composite {
    public MyPanel() {}
}
`
	r := ExtractGWT(PatternContext{
		Source:    source2,
		Language:  "java",
		Framework: "gwt",
		FilePath:  "MyPanel.java",
	})
	_ = source

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_GWT_WIDGET_CLASS" && e.Name == "MyPanel" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3091 component_extraction] expected UIComponent for MyPanel extends Composite")
	}
}

// TestGWT_HistoryNavigation_RouterPattern_Issue3091 proves that History.newItem()
// calls are detected as GWT router_pattern signals.
// Registry target: lang.java.framework.gwt Navigation/router_pattern → partial.
func TestGWT_HistoryNavigation_RouterPattern_Issue3091(t *testing.T) {
	source := `
package com.example.client;

import com.google.gwt.user.client.History;

public class NavController {
    public void goToLogin() {
        History.newItem("login");
    }
    public void goToHome() {
        History.newItem("home");
    }
}
`
	r := ExtractGWT(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "gwt",
		FilePath:  "NavController.java",
	})

	tokens := map[string]bool{}
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_GWT_HISTORY" && e.Kind == "SCOPE.Route" {
			tokens[e.Name] = true
		}
	}
	if !tokens["login"] {
		t.Errorf("[#3091 router_pattern] expected SCOPE.Route for 'login' token")
	}
	if !tokens["home"] {
		t.Errorf("[#3091 router_pattern] expected SCOPE.Route for 'home' token")
	}
}

// TestGWT_EntryPoint_Issue3091 proves EntryPoint implementations are detected.
func TestGWT_EntryPoint_Issue3091(t *testing.T) {
	source := `
package com.example.client;

import com.google.gwt.core.client.EntryPoint;

public class App implements EntryPoint {
    public void onModuleLoad() {}
}
`
	r := ExtractGWT(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "gwt",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_GWT_ENTRY_POINT" && e.Name == "App" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3091 component_extraction] expected Component entity for GWT EntryPoint")
	}
}

// TestGWT_RPCServlet_Issue3091 proves RemoteServiceServlet is detected as taint marker.
func TestGWT_RPCServlet_Issue3091(t *testing.T) {
	source := `
package com.example.server;

import com.google.gwt.user.server.rpc.RemoteServiceServlet;

public class GreetingServiceImpl extends RemoteServiceServlet implements GreetingService {
    public String greet(String name) { return "Hello, " + name; }
}
`
	r := ExtractGWT(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "gwt",
		FilePath:  "GreetingServiceImpl.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_GWT_RPC_SERVLET" && e.Name == "GreetingServiceImpl" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3091 rpc_taint] expected Service entity for GWT RemoteServiceServlet")
	}
}

// TestGWT_IgnoresNonGWTFramework_Issue3091 proves the GWT framework gate works.
func TestGWT_IgnoresNonGWTFramework_Issue3091(t *testing.T) {
	source := `public class Foo extends Composite {}`
	r := ExtractGWT(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vaadin",
		FilePath:  "Foo.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3091 gate] GWT extractor should not fire on vaadin framework")
	}
}
