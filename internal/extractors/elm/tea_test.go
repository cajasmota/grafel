package elm_test

import "testing"

// teaCounter is a canonical The-Elm-Architecture counter app: a Model alias, a
// Msg custom type, the init/update/view triad, and the Browser.sandbox program
// entry wired through `main`.
const teaCounter = `module Main exposing (main)

import Browser
import Html exposing (Html, button, div, text)
import Html.Events exposing (onClick)


type alias Model =
    { count : Int }


type Msg
    = Increment
    | Decrement
    | Reset


init : Model
init =
    { count = 0 }


update : Msg -> Model -> Model
update msg model =
    case msg of
        Increment ->
            { model | count = model.count + 1 }

        Decrement ->
            { model | count = model.count - 1 }

        Reset ->
            { model | count = 0 }


view : Model -> Html Msg
view model =
    div []
        [ button [ onClick Decrement ] [ text "-" ]
        , text (String.fromInt model.count)
        , button [ onClick Increment ] [ text "+" ]
        ]


main : Program () Model Msg
main =
    Browser.sandbox { init = init, update = update, view = view }
`

func TestTEA_ModelRekind(t *testing.T) {
	ents := runElm(t, teaCounter, "src/Main.elm")
	m := elmFind(ents, "Model", "SCOPE.Model")
	if m == nil {
		t.Fatal("Model not re-kinded to SCOPE.Model")
	}
	if m.Subtype != "tea_model" {
		t.Errorf("Model subtype=%q want tea_model", m.Subtype)
	}
	if m.Properties["tea_role"] != "model" {
		t.Errorf("Model tea_role=%q want model", m.Properties["tea_role"])
	}
}

func TestTEA_MsgRekindWithVariants(t *testing.T) {
	ents := runElm(t, teaCounter, "src/Main.elm")
	msg := elmFind(ents, "Msg", "SCOPE.Event")
	if msg == nil {
		t.Fatal("Msg not re-kinded to SCOPE.Event")
	}
	if msg.Subtype != "tea_msg" {
		t.Errorf("Msg subtype=%q want tea_msg", msg.Subtype)
	}
	if got := msg.Properties["tea_variants"]; got != "Increment,Decrement,Reset" {
		t.Errorf("Msg tea_variants=%q want Increment,Decrement,Reset", got)
	}
}

func TestTEA_TriadRoles(t *testing.T) {
	ents := runElm(t, teaCounter, "src/Main.elm")
	want := map[string]string{"init": "init", "update": "update", "view": "view"}
	for name, role := range want {
		op := elmFind(ents, name, "SCOPE.Operation")
		if op == nil {
			t.Fatalf("%s operation missing", name)
		}
		if op.Properties["tea_role"] != role {
			t.Errorf("%s tea_role=%q want %q", name, op.Properties["tea_role"], role)
		}
	}
}

func TestTEA_ProgramFlagged(t *testing.T) {
	ents := runElm(t, teaCounter, "src/Main.elm")
	main := elmFind(ents, "main", "SCOPE.Operation")
	if main == nil {
		t.Fatal("main operation missing")
	}
	if main.Properties["tea_program"] != "true" {
		t.Errorf("main tea_program=%q want true", main.Properties["tea_program"])
	}
	if main.Properties["tea_program_kind"] != "sandbox" {
		t.Errorf("main tea_program_kind=%q want sandbox", main.Properties["tea_program_kind"])
	}
}

// TestTEA_NonFrontendNoop confirms a plain Elm helper module (no Browser/Html
// import) is NOT decorated — Model/Msg stay plain SCOPE.Component entities.
func TestTEA_NonFrontendNoop(t *testing.T) {
	const helper = `module Util exposing (clamp)

import String


type alias Model =
    { x : Int }


clamp : Int -> Int
clamp n =
    String.length (String.fromInt n)
`
	ents := runElm(t, helper, "src/Util.elm")
	if m := elmFind(ents, "Model", "SCOPE.Model"); m != nil {
		t.Error("Model should NOT be re-kinded in a non-frontend module")
	}
	if m := elmFind(ents, "Model", "SCOPE.Component"); m == nil {
		t.Error("Model should remain a plain SCOPE.Component in a non-frontend module")
	}
}
