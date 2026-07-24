package fsharp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/fsharp"
	"github.com/cajasmota/grafel/internal/types"
)

// fsHasProp reports whether the named entity (matched by name) carries the
// given Property value.
func fsHasProp(ents []types.EntityRecord, name, key, val string) bool {
	for i := range ents {
		if ents[i].Name == name && ents[i].Properties[key] == val {
			return true
		}
	}
	return false
}

// fsByName returns the first entity with the given name, or nil.
func fsByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// fsHasDispatch reports whether the named operation carries a Cmd-dispatch USES
// edge for the given helper.
func fsHasDispatch(ents []types.EntityRecord, name, helper string) bool {
	for i := range ents {
		if ents[i].Name != name {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "USES" && r.Properties.Get("dispatch") == helper {
				return true
			}
		}
	}
	return false
}

// TestElmishFeliz_HappyPath runs the canonical MVU+Feliz fixture and asserts the
// full set of #5129 decorations: re-kinded Model/Msg, tagged init/update/view,
// the Feliz components, command dispatch, the program bootstrap, and the
// view→child RENDERS edge.
func TestElmishFeliz_HappyPath(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "elmish_counter.fs"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := runFSharp(t, string(src), "Counter.fs")

	// Model → SCOPE.Model / elmish_model.
	if m := fsFind(ents, "Model", "SCOPE.Model"); m == nil {
		t.Error("Model not re-kinded to SCOPE.Model")
	} else {
		if m.Subtype != "elmish_model" {
			t.Errorf("Model subtype = %q, want elmish_model", m.Subtype)
		}
		if m.Properties["elmish_role"] != "model" {
			t.Errorf("Model elmish_role = %q, want model", m.Properties["elmish_role"])
		}
	}

	// Msg → SCOPE.Event / elmish_msg.
	if m := fsFind(ents, "Msg", "SCOPE.Event"); m == nil {
		t.Error("Msg not re-kinded to SCOPE.Event")
	} else if m.Subtype != "elmish_msg" {
		t.Errorf("Msg subtype = %q, want elmish_msg", m.Subtype)
	}

	// init/update/view triad roles.
	if !fsHasProp(ents, "init", "elmish_role", "init") {
		t.Error("init not tagged elmish_role=init")
	}
	if !fsHasProp(ents, "update", "elmish_role", "update") {
		t.Error("update not tagged elmish_role=update")
	}
	if !fsHasProp(ents, "view", "elmish_role", "view") {
		t.Error("view not tagged elmish_role=view")
	}

	// view + counterButton are Feliz components.
	if v := fsFind(ents, "view", "SCOPE.UIComponent"); v == nil {
		t.Error("view not re-kinded to SCOPE.UIComponent")
	} else if v.Properties["ui_framework"] != "feliz" {
		t.Errorf("view ui_framework = %q, want feliz", v.Properties["ui_framework"])
	}
	if c := fsFind(ents, "counterButton", "SCOPE.UIComponent"); c == nil {
		t.Error("counterButton not re-kinded to SCOPE.UIComponent")
	} else if c.Subtype != "feliz_component" {
		t.Errorf("counterButton subtype = %q, want feliz_component", c.Subtype)
	}

	// Command dispatch on init (Cmd.ofMsg) and update (Cmd.none / Cmd.OfAsync).
	if !fsHasDispatch(ents, "init", "Cmd.ofMsg") {
		t.Error("init missing Cmd.ofMsg dispatch edge")
	}
	if !fsHasDispatch(ents, "update", "Cmd.OfAsync.perform") {
		t.Error("update missing Cmd.OfAsync.perform dispatch edge")
	}

	// Program bootstrap on main.
	if !fsHasProp(ents, "main", "elmish_program", "true") {
		t.Error("main not flagged elmish_program")
	}

	// view RENDERS counterButton (nested component composition).
	if !fsHasRel(ents, "view", "SCOPE.UIComponent", "RENDERS",
		extractor.BuildOperationStructuralRef("fsharp", "Counter.fs", "counterButton")) {
		t.Error("view missing RENDERS edge to counterButton")
	}
}

// TestElmishFeliz_NoMatchNoOp asserts that an ordinary F# file with no Fable
// imports is left entirely undecorated — no re-kinding, no elmish props.
func TestElmishFeliz_NoMatchNoOp(t *testing.T) {
	src := `module Plain

type Model =
    { Count: int }

type Msg =
    | Increment

let init () = { Count = 0 }

let update msg model = model

let view model dispatch = "no feliz here"
`
	ents := runFSharp(t, src, "Plain.fs")

	// Model/Msg stay as plain SCOPE.Component — no frontend re-kinding.
	if fsFind(ents, "Model", "SCOPE.Model") != nil {
		t.Error("Model wrongly re-kinded in a non-Fable file")
	}
	if fsFind(ents, "Msg", "SCOPE.Event") != nil {
		t.Error("Msg wrongly re-kinded in a non-Fable file")
	}
	if m := fsByName(ents, "Model"); m == nil || m.Kind != "SCOPE.Component" {
		t.Error("Model should remain SCOPE.Component")
	}
	// No elmish_role anywhere.
	for i := range ents {
		if ents[i].Properties["elmish_role"] != "" || ents[i].Properties["fable_frontend"] != "" {
			t.Errorf("entity %q carries elmish props in a non-Fable file", ents[i].Name)
		}
	}
}

// TestElmishFeliz_WrongLanguageNoOp asserts the decoration pass never fires for
// a non-F# input: a C#-shaped source routed through the F# extractor must not
// produce Elmish entities (and in practice another language's extractor owns
// the file). We assert no frontend re-kinding occurs.
func TestElmishFeliz_WrongLanguageNoOp(t *testing.T) {
	// C# source — no F# `open Elmish`, no F# `type Model = { ... }`. Routed
	// through the F# extractor it must not yield Elmish/Feliz decorations.
	src := `using Elmish;

public class Model {
    public int Count { get; set; }
}

public enum Msg { Increment, Decrement }
`
	ext, ok := extractor.Get("fsharp")
	if !ok {
		t.Fatal("fsharp extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Model.cs",
		Content:  []byte(src),
		Language: "csharp",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for i := range ents {
		if ents[i].Kind == "SCOPE.Model" || ents[i].Kind == "SCOPE.Event" ||
			ents[i].Properties["fable_frontend"] == "true" {
			t.Errorf("entity %q wrongly decorated as Fable frontend from C# input", ents[i].Name)
		}
	}
}
