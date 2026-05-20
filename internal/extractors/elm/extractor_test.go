package elm_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/elm"
	"github.com/cajasmota/archigraph/internal/types"
)

// runElm runs the extractor on raw source and returns entity records.
func runElm(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("elm")
	if !ok {
		t.Fatal("elm extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "elm",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func elmFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func elmHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// TestElm_Registered verifies the extractor is in the registry.
func TestElm_Registered(t *testing.T) {
	_, ok := extractor.Get("elm")
	if !ok {
		t.Fatal("elm extractor not registered")
	}
}

// TestElm_EmptyInput returns zero entities for empty content.
func TestElm_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("elm")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.elm",
		Content:  []byte{},
		Language: "elm",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestElm_ModuleDeclaration — module declaration extracted as SCOPE.Component(module).
func TestElm_ModuleDeclaration(t *testing.T) {
	src := `module Main exposing (..)

main : Program () Model Msg
main =
    Browser.sandbox { init = init, update = update, view = view }
`
	ents := runElm(t, src, "Main.elm")

	mod := elmFind(ents, "Main", "SCOPE.Component")
	if mod == nil {
		t.Fatal("expected Main module component")
	}
	if mod.Subtype != "module" {
		t.Errorf("expected subtype=module, got %q", mod.Subtype)
	}
}

// TestElm_FunctionDiscovery — top-level functions extracted as SCOPE.Operation.
func TestElm_FunctionDiscovery(t *testing.T) {
	src := `module Counter exposing (..)

type alias Model =
    { count : Int }

init : Model
init =
    { count = 0 }

increment : Model -> Model
increment model =
    { model | count = model.count + 1 }

decrement : Model -> Model
decrement model =
    { model | count = model.count - 1 }
`
	ents := runElm(t, src, "Counter.elm")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "elm" {
				t.Errorf("entity %q: expected Language=elm, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"init", "increment", "decrement"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted, got ops=%v", want, ops)
		}
	}
}

// TestElm_TypeAliasDiscovery — type alias declarations extracted as SCOPE.Component(typealias).
func TestElm_TypeAliasDiscovery(t *testing.T) {
	src := `module Model exposing (..)

type alias Model =
    { count : Int
    , name : String
    }

type alias Flags =
    { initialCount : Int }
`
	ents := runElm(t, src, "Model.elm")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	checks := map[string]string{
		"Model": "typealias",
		"Flags": "typealias",
	}
	for name, wantSubtype := range checks {
		got, ok := comps[name]
		if !ok {
			t.Errorf("expected type alias %q to be extracted; got comps=%v", name, comps)
		} else if got != wantSubtype {
			t.Errorf("type %q: expected subtype=%q, got %q", name, wantSubtype, got)
		}
	}
}

// TestElm_CustomTypeDiscovery — custom type declarations extracted as SCOPE.Component(type).
func TestElm_CustomTypeDiscovery(t *testing.T) {
	src := `module Msg exposing (..)

type Msg
    = Increment
    | Decrement
    | Reset

type Status
    = Loading
    | Loaded String
    | Failed String
`
	ents := runElm(t, src, "Msg.elm")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Msg", "Status"} {
		if subtype, ok := comps[name]; !ok {
			t.Errorf("expected custom type %q to be extracted; comps=%v", name, comps)
		} else if subtype != "type" {
			t.Errorf("type %q: expected subtype=type, got %q", name, subtype)
		}
	}
}

// TestElm_TypeAliasNotCustomType — "type alias" is NOT extracted as custom type.
func TestElm_TypeAliasNotCustomType(t *testing.T) {
	src := `module Foo exposing (..)

type alias Model = { x : Int }

type Msg = Click | Hover
`
	ents := runElm(t, src, "Foo.elm")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	if subtype, ok := comps["Model"]; !ok {
		t.Error("expected Model to be extracted as typealias")
	} else if subtype != "typealias" {
		t.Errorf("Model: expected subtype=typealias, got %q", subtype)
	}

	if subtype, ok := comps["Msg"]; !ok {
		t.Error("expected Msg to be extracted as type")
	} else if subtype != "type" {
		t.Errorf("Msg: expected subtype=type, got %q", subtype)
	}
}

// TestElm_ImportEdges — import statements emit IMPORTS edges.
func TestElm_ImportEdges(t *testing.T) {
	src := `module App exposing (..)

import Browser
import Browser.Navigation as Nav
import Html exposing (Html, div, text, button)
import Html.Attributes exposing (class, style)
import Html.Events exposing (onClick)

main : Program () Model Msg
main =
    Browser.sandbox { init = init, update = update, view = view }
`
	ents := runElm(t, src, "App.elm")

	wantImports := map[string]bool{
		"Browser":            false,
		"Browser.Navigation": false,
		"Html":               false,
		"Html.Attributes":    false,
		"Html.Events":        false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "App.elm" {
						t.Errorf("IMPORTS %q: expected FromID=App.elm, got %q", r.ToID, r.FromID)
					}
				}
			}
		}
	}
	for mod, found := range wantImports {
		if !found {
			t.Errorf("expected IMPORTS edge for %q", mod)
		}
	}
}

// TestElm_CallsEdges — function invocations emit CALLS edges.
func TestElm_CallsEdges(t *testing.T) {
	src := `module App exposing (..)

import List

helper : Int -> Int
helper x =
    x * 2

process : List Int -> List Int
process xs =
    List.map helper (List.filter isEven xs)

isEven : Int -> Bool
isEven n =
    modBy 2 n == 0
`
	ents := runElm(t, src, "App.elm")

	if !elmHasRel(ents, "process", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS process→helper")
	}
}

// TestElm_CallsDeduped — duplicate call targets are emitted once.
func TestElm_CallsDeduped(t *testing.T) {
	src := `module Dedup exposing (..)

worker : Int -> Int
worker x =
    x + 1

runner : Int
runner =
    worker (worker (worker 0))
`
	ents := runElm(t, src, "Dedup.elm")
	count := 0
	for _, e := range ents {
		if e.Name == "runner" && e.Kind == "SCOPE.Operation" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && r.ToID == "worker" {
					count++
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("expected 1 CALLS runner→worker (deduped), got %d", count)
	}
}

// TestElm_LanguageTagged — all relationships carry language=elm.
func TestElm_LanguageTagged(t *testing.T) {
	src := `module Tagged exposing (..)

import Html

type alias Model = { x : Int }

view : Model -> Html.Html msg
view model =
    Html.text (String.fromInt model.x)
`
	ents := runElm(t, src, "Tagged.elm")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "elm" {
				t.Errorf("rel %s→%s missing language=elm (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestElm_ModuleContainsEdges — module entity has CONTAINS edges for top-level functions.
func TestElm_ModuleContainsEdges(t *testing.T) {
	src := `module Counter exposing (..)

type alias Model = { count : Int }

init : Model
init = { count = 0 }

update : Msg -> Model -> Model
update msg model =
    case msg of
        Increment -> { model | count = model.count + 1 }
        Decrement -> { model | count = model.count - 1 }
`
	ents := runElm(t, src, "Counter.elm")

	var mod *types.EntityRecord
	for i := range ents {
		if ents[i].Name == "Counter" && ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "module" {
			mod = &ents[i]
			break
		}
	}
	if mod == nil {
		t.Fatal("expected Counter module component")
	}

	containsTargets := make(map[string]bool)
	for _, r := range mod.Relationships {
		if r.Kind == "CONTAINS" {
			containsTargets[r.ToID] = true
		}
	}

	if len(containsTargets) == 0 {
		t.Error("expected CONTAINS edges from module Counter, got none")
	}
}

// TestElm_TEAFixture — Elm The Architecture (TEA) pattern fixture for recall.
// This is the synthetic fixture required by the acceptance criteria.
func TestElm_TEAFixture(t *testing.T) {
	src := `module Main exposing (..)

import Browser
import Html exposing (Html, button, div, text)
import Html.Attributes exposing (class, style)
import Html.Events exposing (onClick)
import Json.Decode as Decode
import Json.Encode as Encode

-- MODEL

type alias Model =
    { count : Int
    , name : String
    , items : List String
    }

type alias Flags =
    { initialCount : Int }

-- MSG

type Msg
    = Increment
    | Decrement
    | Reset
    | SetName String
    | AddItem String
    | RemoveItem Int

-- INIT

init : Flags -> ( Model, Cmd Msg )
init flags =
    ( { count = flags.initialCount
      , name = "Counter"
      , items = []
      }
    , Cmd.none
    )

-- UPDATE

update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Increment ->
            ( { model | count = model.count + 1 }, Cmd.none )

        Decrement ->
            ( { model | count = model.count - 1 }, Cmd.none )

        Reset ->
            ( { model | count = 0 }, Cmd.none )

        SetName name ->
            ( { model | name = name }, Cmd.none )

        AddItem item ->
            ( { model | items = item :: model.items }, Cmd.none )

        RemoveItem _ ->
            ( model, Cmd.none )

-- VIEW

view : Model -> Html Msg
view model =
    div [ class "container" ]
        [ viewHeader model.name
        , viewCounter model.count
        , viewItems model.items
        ]

viewHeader : String -> Html Msg
viewHeader name =
    div [ class "header" ]
        [ text name ]

viewCounter : Int -> Html Msg
viewCounter count =
    div [ class "counter" ]
        [ button [ onClick Decrement, class "btn" ] [ text "-" ]
        , div [] [ text (String.fromInt count) ]
        , button [ onClick Increment, class "btn" ] [ text "+" ]
        ]

viewItems : List String -> Html Msg
viewItems items =
    div [ class "items" ]
        (List.map viewItem items)

viewItem : String -> Html Msg
viewItem item =
    div [ class "item" ] [ text item ]

-- MAIN

main : Program Flags Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , view = view
        , subscriptions = always Sub.none
        }
`
	ents := runElm(t, src, "Main.elm")

	wantOps := []string{"init", "update", "view", "viewHeader", "viewCounter", "viewItems", "viewItem", "main"}
	wantComps := []string{"Model", "Flags", "Msg", "Main"}
	wantImports := []string{"Browser", "Html", "Html.Attributes", "Html.Events", "Json.Decode", "Json.Encode"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundImports := make(map[string]bool)

	for _, e := range ents {
		switch e.Kind {
		case "SCOPE.Operation":
			foundOps[e.Name] = true
		case "SCOPE.Component":
			foundComps[e.Name] = true
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" {
					foundImports[r.ToID] = true
				}
			}
		}
	}

	opHits := 0
	for _, name := range wantOps {
		if foundOps[name] {
			opHits++
		} else {
			t.Logf("missing operation: %s", name)
		}
	}
	compHits := 0
	for _, name := range wantComps {
		if foundComps[name] {
			compHits++
		} else {
			t.Logf("missing component: %s", name)
		}
	}
	importHits := 0
	for _, mod := range wantImports {
		if foundImports[mod] {
			importHits++
		} else {
			t.Logf("missing import: %s", mod)
		}
	}

	totalWant := len(wantOps) + len(wantComps) + len(wantImports)
	totalFound := opHits + compHits + importHits
	recall := float64(totalFound) / float64(totalWant) * 100

	t.Logf("TEA fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}
