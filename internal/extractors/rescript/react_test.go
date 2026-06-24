package rescript_test

import "testing"

// TestReScriptReact_ComponentReKinded verifies a @react.component-annotated
// `let make` binding is re-kinded SCOPE.UIComponent with the rescript-react
// framework tag.
func TestReScriptReact_ComponentReKinded(t *testing.T) {
	src := `
open React

@react.component
let make = (~name: string, ~onClick) => {
  <div>
    <Header title="My App" />
    <button onClick> {React.string(name)} </button>
  </div>
}
`
	ents := runReScript(t, src, "Card.res")

	// `make` is now a UIComponent, not a plain Operation.
	if op := rsFind(ents, "make", "SCOPE.Operation"); op != nil {
		t.Error("@react.component make should NOT remain SCOPE.Operation")
	}
	comp := rsFind(ents, "make", "SCOPE.UIComponent")
	if comp == nil {
		t.Fatal("expected 'make' as SCOPE.UIComponent")
	}
	if comp.Subtype != "react_component" {
		t.Errorf("subtype=%q want react_component", comp.Subtype)
	}
	if comp.Properties["ui_framework"] != "rescript-react" {
		t.Errorf("ui_framework=%q want rescript-react", comp.Properties["ui_framework"])
	}
	if comp.Properties["react_component"] != "true" {
		t.Errorf("react_component=%q want true", comp.Properties["react_component"])
	}
}

// TestReScriptReact_Props verifies the labelled-argument prop names are recorded.
func TestReScriptReact_Props(t *testing.T) {
	src := `
@react.component
let make = (~name: string, ~count: int, ~onClick) => {
  <div> {React.string(name)} </div>
}
`
	ents := runReScript(t, src, "Widget.res")
	comp := rsFind(ents, "make", "SCOPE.UIComponent")
	if comp == nil {
		t.Fatal("expected 'make' as SCOPE.UIComponent")
	}
	props := comp.Properties["props"]
	for _, want := range []string{"name", "count", "onClick"} {
		if !propListHas(props, want) {
			t.Errorf("props=%q missing %q", props, want)
		}
	}
}

// TestReScriptReact_NoDecoratorIsPlainOperation verifies a plain `let` binding
// with no @react.component decorator stays a SCOPE.Operation (no misclassify).
func TestReScriptReact_NoDecoratorIsPlainOperation(t *testing.T) {
	src := `
let helper = (x) => x + 1

let make = (~name) => {
  <div> {React.string(name)} </div>
}
`
	ents := runReScript(t, src, "Plain.res")
	if rsFind(ents, "make", "SCOPE.UIComponent") != nil {
		t.Error("make without @react.component should NOT be a UIComponent")
	}
	if rsFind(ents, "make", "SCOPE.Operation") == nil {
		t.Error("make without @react.component should remain SCOPE.Operation")
	}
	if rsFind(ents, "helper", "SCOPE.Operation") == nil {
		t.Error("helper should remain SCOPE.Operation")
	}
}

// TestReScriptReact_RendersPreserved verifies JSX RENDERS edges still flow on a
// re-kinded component (the JS-ecosystem React render model is reused).
func TestReScriptReact_RendersPreserved(t *testing.T) {
	src := `
@react.component
let make = (~name) => {
  <div>
    <Header title="t" />
    <Footer />
  </div>
}
`
	ents := runReScript(t, src, "Page.res")
	comp := rsFind(ents, "make", "SCOPE.UIComponent")
	if comp == nil {
		t.Fatal("expected 'make' as SCOPE.UIComponent")
	}
	for _, want := range []string{"Header", "Footer"} {
		found := false
		for _, r := range comp.Relationships {
			if r.Kind == "RENDERS" && r.ToID == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected RENDERS edge to %q", want)
		}
	}
}

func propListHas(csv, want string) bool {
	start := 0
	for i := 0; i <= len(csv); i++ {
		if i == len(csv) || csv[i] == ',' {
			if csv[start:i] == want {
				return true
			}
			start = i + 1
		}
	}
	return false
}
