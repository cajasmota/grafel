package swift_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

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
