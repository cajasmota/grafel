package reasonml_test

import (
	"strings"
	"testing"
)

// TestReasonReact_ComponentReKinded verifies a [@react.component]-annotated
// `let make` binding is re-kinded SCOPE.UIComponent with the reason-react
// framework tag.
func TestReasonReact_ComponentReKinded(t *testing.T) {
	src := `
open React;

[@react.component]
let make = (~name: string, ~onClick) => {
  <div>
    <Header title="My App" />
    <button onClick> {React.string(name)} </button>
  </div>;
};
`
	ents := runReasonML(t, src, "Card.re")

	// `make` is now a UIComponent, not a plain Operation.
	if op := reFind(ents, "make", "SCOPE.Operation"); op != nil {
		t.Error("[@react.component] make should NOT remain SCOPE.Operation")
	}
	comp := reFind(ents, "make", "SCOPE.UIComponent")
	if comp == nil {
		t.Fatal("expected 'make' as SCOPE.UIComponent")
	}
	if comp.Subtype != "react_component" {
		t.Errorf("subtype=%q want react_component", comp.Subtype)
	}
	if comp.Properties["ui_framework"] != "reason-react" {
		t.Errorf("ui_framework=%q want reason-react", comp.Properties["ui_framework"])
	}
	if comp.Properties["react_component"] != "true" {
		t.Errorf("react_component=%q want true", comp.Properties["react_component"])
	}
}

// TestReasonReact_Props verifies the labelled-argument prop names are recorded.
func TestReasonReact_Props(t *testing.T) {
	src := `
[@react.component]
let make = (~name: string, ~count: int, ~onClick) => {
  <div> {React.string(name)} </div>;
};
`
	ents := runReasonML(t, src, "Widget.re")
	comp := reFind(ents, "make", "SCOPE.UIComponent")
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

// TestReasonReact_MultilineProps verifies props declared on continuation lines
// (up to the `=>`) are captured.
func TestReasonReact_MultilineProps(t *testing.T) {
	src := `
[@react.component]
let make =
  (
    ~title: string,
    ~subtitle: string,
    ~onSelect,
  ) => {
  <section> {React.string(title)} </section>;
};
`
	ents := runReasonML(t, src, "Panel.re")
	comp := reFind(ents, "make", "SCOPE.UIComponent")
	if comp == nil {
		t.Fatal("expected 'make' as SCOPE.UIComponent")
	}
	props := comp.Properties["props"]
	for _, want := range []string{"title", "subtitle", "onSelect"} {
		if !propListHas(props, want) {
			t.Errorf("props=%q missing %q", props, want)
		}
	}
}

// TestReasonReact_AttrWithDocComment verifies an attribute separated from the
// `let` binding by a doc comment (within the small tolerated gap) is recognised.
func TestReasonReact_AttrWithDocComment(t *testing.T) {
	src := `
[@react.component]
/* A labelled badge. */
let make = (~label) => <span> {React.string(label)} </span>;
`
	ents := runReasonML(t, src, "Badge.re")
	comp := reFind(ents, "make", "SCOPE.UIComponent")
	if comp == nil {
		t.Fatal("expected [@react.component] make (doc-comment gap) as SCOPE.UIComponent")
	}
	if !propListHas(comp.Properties["props"], "label") {
		t.Errorf("props=%q missing label", comp.Properties["props"])
	}
}

// TestReasonReact_NoAttrIsPlainOperation verifies a plain `let` binding with no
// [@react.component] attribute stays a SCOPE.Operation (no misclassify).
func TestReasonReact_NoAttrIsPlainOperation(t *testing.T) {
	src := `
let helper = (x) => x + 1;

let make = (~name) => {
  <div> {React.string(name)} </div>;
};
`
	ents := runReasonML(t, src, "Plain.re")
	if reFind(ents, "make", "SCOPE.UIComponent") != nil {
		t.Error("make without [@react.component] should NOT be a UIComponent")
	}
	if reFind(ents, "make", "SCOPE.Operation") == nil {
		t.Error("make without [@react.component] should remain SCOPE.Operation")
	}
	if reFind(ents, "helper", "SCOPE.Operation") == nil {
		t.Error("helper should remain SCOPE.Operation")
	}
}

// TestReasonReact_OnlyAnnotatedReKinded verifies that in a file with a mix of
// components and helpers, only the [@react.component]-annotated binding is
// re-kinded.
func TestReasonReact_OnlyAnnotatedReKinded(t *testing.T) {
	src := `
let formatName = (n) => "Hello " ++ n;

[@react.component]
let make = (~name) => {
  <div> {React.string(formatName(name))} </div>;
};
`
	ents := runReasonML(t, src, "Mixed.re")
	if reFind(ents, "make", "SCOPE.UIComponent") == nil {
		t.Error("annotated make should be a UIComponent")
	}
	if reFind(ents, "formatName", "SCOPE.UIComponent") != nil {
		t.Error("formatName (no attribute) should NOT be a UIComponent")
	}
	if reFind(ents, "formatName", "SCOPE.Operation") == nil {
		t.Error("formatName should remain SCOPE.Operation")
	}
}

func propListHas(csv, want string) bool {
	for _, p := range strings.Split(csv, ",") {
		if p == want {
			return true
		}
	}
	return false
}
