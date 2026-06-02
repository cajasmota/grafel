package swift_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/swift"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Vapor
// ---------------------------------------------------------------------------

func TestVaporRoute(t *testing.T) {
	src := `
func routes(_ app: Application) throws {
    app.get("users") { req async throws -> [User] in
        return try await User.query(on: req.db).all()
    }
    app.post("orders") { req async throws -> Order in
        return try await Order.create(on: req.db)
    }
}
`
	ents := extract(t, "custom_swift_vapor", fi("routes.swift", "swift", src))
	// Vapor route entity = METHOD + " " + path (no leading slash)
	if !containsEntity(ents, "SCOPE.Operation", "GET users") {
		t.Error("expected GET users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST orders") {
		t.Error("expected POST orders route")
	}
}

func TestVaporModel(t *testing.T) {
	src := `
final class Todo: Model, Content {
    static let schema = "todos"

    @ID(key: .id)
    var id: UUID?

    @Field(key: "title")
    var title: String
}
`
	ents := extract(t, "custom_swift_vapor", fi("Todo.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Schema", "Todo") {
		t.Error("expected Todo model schema")
	}
}

func TestVaporRouteCollection(t *testing.T) {
	src := `struct UserController: RouteCollection {}`
	ents := extract(t, "custom_swift_vapor", fi("UserController.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Component", "UserController") {
		t.Error("expected UserController route collection component")
	}
}

func TestVaporNoMatch(t *testing.T) {
	src := `import Foundation\nlet x = 42`
	ents := extract(t, "custom_swift_vapor", fi("main.swift", "swift", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// SwiftUI
// ---------------------------------------------------------------------------

func TestSwiftUIView(t *testing.T) {
	src := `
struct ContentView: View {
    var body: some View {
        Text("Hello")
    }
}

struct UserListView: View {
    var body: some View { EmptyView() }
}
`
	ents := extract(t, "custom_swift_swiftui", fi("ContentView.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "ContentView") {
		t.Error("expected ContentView UIComponent")
	}
	if !containsEntity(ents, "SCOPE.UIComponent", "UserListView") {
		t.Error("expected UserListView UIComponent")
	}
}

func TestSwiftUIStateVar(t *testing.T) {
	src := `
struct Counter: View {
    @State var count: Int = 0
    @Binding var isVisible: Bool
    @ObservedObject var viewModel: CounterViewModel
}
`
	ents := extract(t, "custom_swift_swiftui", fi("Counter.swift", "swift", src))
	// @State → entity name = "state:" + propName
	if !containsEntity(ents, "SCOPE.Pattern", "state:count") {
		t.Error("expected state:count pattern")
	}
	// @Binding → entity name = "binding:" + propName
	if !containsEntity(ents, "SCOPE.Pattern", "binding:isVisible") {
		t.Error("expected binding:isVisible pattern")
	}
	// @ObservedObject → entity name = varName
	if !containsEntity(ents, "SCOPE.Component", "viewModel") {
		t.Error("expected viewModel ObservedObject component")
	}
}

func TestSwiftUINavigationLink(t *testing.T) {
	src := `
NavigationLink(destination: DetailView()) { Text("Go") }
`
	ents := extract(t, "custom_swift_swiftui", fi("Nav.swift", "swift", src))
	if !containsEntity(ents, "SCOPE.Operation", "nav:DetailView") {
		t.Error("expected nav:DetailView operation")
	}
}

func TestSwiftUIBuiltinSkipped(t *testing.T) {
	src := `struct Text: View { var body: some View { EmptyView() } }`
	ents := extract(t, "custom_swift_swiftui", fi("Builtins.swift", "swift", src))
	if containsEntity(ents, "SCOPE.UIComponent", "Text") {
		t.Error("builtin Text should be skipped")
	}
}

func TestSwiftUINoMatch(t *testing.T) {
	src := `import SwiftUI`
	ents := extract(t, "custom_swift_swiftui", fi("imports.swift", "swift", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// SwiftUI edges (navigation + state) — value-asserting
// ---------------------------------------------------------------------------

// extractFull returns the raw EntityRecords (with relationships) for edge tests.
func extractFull(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// hasEdge reports whether some entity named fromName emits an edge of kind to
// ToID. fromName "" matches any source.
func hasEdge(ents []types.EntityRecord, fromName, kind, toID string) bool {
	for _, e := range ents {
		if fromName != "" && e.Name != fromName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == kind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

func edgeProp(ents []types.EntityRecord, fromName, kind, toID, key string) (string, bool) {
	for _, e := range ents {
		if fromName != "" && e.Name != fromName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == kind && r.ToID == toID {
				v, ok := r.Properties[key]
				return v, ok
			}
		}
	}
	return "", false
}

func TestSwiftUINavigatesToEdge(t *testing.T) {
	src := `
struct ContentView: View {
    var body: some View {
        NavigationStack {
            NavigationLink(destination: DetailView()) { Text("Go") }
        }
    }
}
`
	ents := extractFull(t, "custom_swift_swiftui", fi("ContentView.swift", "swift", src))
	// ContentView NAVIGATES_TO DetailView (via the synthetic view: stub).
	if !hasEdge(ents, "ContentView", "NAVIGATES_TO", "view:DetailView") {
		t.Fatal("expected ContentView NAVIGATES_TO view:DetailView")
	}
	if v, _ := edgeProp(ents, "ContentView", "NAVIGATES_TO", "view:DetailView", "via"); v != "navigation_link" {
		t.Errorf("expected via=navigation_link, got %q", v)
	}
}

func TestSwiftUINavigatesValueEdge(t *testing.T) {
	src := `
struct ListView: View {
    var body: some View {
        NavigationStack {
            NavigationLink(value: route) { Text("Open") }
        }
    }
}
`
	ents := extractFull(t, "custom_swift_swiftui", fi("ListView.swift", "swift", src))
	if !hasEdge(ents, "ListView", "NAVIGATES_TO", "navvalue:route") {
		t.Fatal("expected ListView NAVIGATES_TO navvalue:route")
	}
	if v, _ := edgeProp(ents, "ListView", "NAVIGATES_TO", "navvalue:route", "via"); v != "navigation_link_value" {
		t.Errorf("expected via=navigation_link_value, got %q", v)
	}
}

func TestSwiftUIModalNavigatesEdge(t *testing.T) {
	src := `
struct HomeView: View {
    @State var showing = false
    var body: some View {
        Text("Home")
            .sheet(isPresented: $showing) { SettingsView() }
    }
}
`
	ents := extractFull(t, "custom_swift_swiftui", fi("HomeView.swift", "swift", src))
	if !hasEdge(ents, "HomeView", "NAVIGATES_TO", "view:SettingsView") {
		t.Fatal("expected HomeView NAVIGATES_TO view:SettingsView (sheet)")
	}
	if v, _ := edgeProp(ents, "HomeView", "NAVIGATES_TO", "view:SettingsView", "via"); v != "modal_sheet" {
		t.Errorf("expected via=modal_sheet, got %q", v)
	}
}

func TestSwiftUIFullScreenCoverNavigatesEdge(t *testing.T) {
	src := `
struct RootView: View {
    var body: some View {
        Text("Root")
            .fullScreenCover(isPresented: $flag) { OnboardingView() }
    }
}
`
	ents := extractFull(t, "custom_swift_swiftui", fi("RootView.swift", "swift", src))
	if !hasEdge(ents, "RootView", "NAVIGATES_TO", "view:OnboardingView") {
		t.Fatal("expected RootView NAVIGATES_TO view:OnboardingView (fullScreenCover)")
	}
	if v, _ := edgeProp(ents, "RootView", "NAVIGATES_TO", "view:OnboardingView", "via"); v != "modal_fullScreenCover" {
		t.Errorf("expected via=modal_fullScreenCover, got %q", v)
	}
}

func TestSwiftUIUsesViewModelEdge(t *testing.T) {
	src := `
struct ProfileView: View {
    @StateObject var vm = ProfileViewModel()
    @ObservedObject var settings: SettingsStore
    @EnvironmentObject var appState: AppState
    var body: some View { Text("Profile") }
}
`
	ents := extractFull(t, "custom_swift_swiftui", fi("ProfileView.swift", "swift", src))
	// ProfileView USES ProfileViewModel / SettingsStore / AppState.
	if !hasEdge(ents, "ProfileView", "USES", "type:SettingsStore") {
		t.Fatal("expected ProfileView USES type:SettingsStore")
	}
	if !hasEdge(ents, "ProfileView", "USES", "type:AppState") {
		t.Fatal("expected ProfileView USES type:AppState")
	}
	// @StateObject var vm = ProfileViewModel() — type comes from initializer; the
	// shared regex captures the RHS type token.
	if !hasEdge(ents, "ProfileView", "USES", "type:ProfileViewModel") {
		t.Fatal("expected ProfileView USES type:ProfileViewModel")
	}
	if v, _ := edgeProp(ents, "ProfileView", "USES", "type:AppState", "property_wrapper"); v != "@EnvironmentObject" {
		t.Errorf("expected property_wrapper=@EnvironmentObject, got %q", v)
	}
}

func TestSwiftUIStateNoEdge(t *testing.T) {
	// @State / @Binding are local value state — entity props, NOT edges.
	src := `
struct Counter: View {
    @State var count: Int = 0
    @Binding var on: Bool
    var body: some View { Text("\(count)") }
}
`
	ents := extractFull(t, "custom_swift_swiftui", fi("Counter.swift", "swift", src))
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "USES" {
				t.Errorf("@State/@Binding must not emit USES edge, got %s -> %s", e.Name, r.ToID)
			}
		}
	}
}
