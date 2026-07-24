// Package fsharp: Fable + Elmish/Feliz frontend extraction (#5129, follow-up
// #5049).
//
// Fable compiles F# to JavaScript; the dominant Fable UI stack is Elmish (an
// Elm-style Model-View-Update architecture) rendered through Feliz (a typed
// React DSL). Neither was modelled by the base extractor — an Elmish app's
// `Model`/`Msg`/`update`/`view`/`init` all surfaced as undifferentiated
// SCOPE.Component / SCOPE.Operation entities, and the Feliz component functions
// looked like ordinary `let` bindings. This file recognises the framework and
// decorates the already-extracted entities so the MVU triad, the Feliz
// component tree, and command dispatch are queryable.
//
// Framework detection is import-gated (`open Elmish` / `open Feliz` /
// `open Fable.*`) so a plain F# file is never mis-classified. When detection
// fails the pass is a no-op — every base entity is returned unchanged.
//
// What it produces (all on top of the base extractor's entities):
//   - the `Model` type            → re-kinded SCOPE.Model, subtype elmish_model
//   - the `Msg` discriminated union → re-kinded SCOPE.Event, subtype elmish_msg
//     (its DU-case sub-entities are the individual messages, already emitted by
//     #4942; they inherit the Msg's role via Properties)
//   - `init` / `update` / `view`   → Properties["elmish_role"] = init|update|view
//   - the operation containing `Program.mkProgram` → Properties["elmish_program"]
//   - Feliz component functions (body uses the `Html.`/`prop.`/`React.` DSL or
//     returns a `ReactElement`) → re-kinded SCOPE.UIComponent, subtype
//     feliz_component, with a RENDERS edge to each nested component it calls
//   - command dispatch (`Cmd.ofMsg`/`Cmd.OfAsync`/`Cmd.batch`/`Cmd.none`) inside
//     update/init → a USES edge stamped Properties["dispatch"] = the Cmd helper
//
// Honest scope: detection and decoration are structural (regex/heuristic) like
// the rest of the F# extractor. Feliz attribute props, Elmish subscriptions, and
// `Program.withReactSynchronous`/hydration wiring are recognised only insofar as
// the program operation is flagged; per-prop extraction is deferred.
package fsharp

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

var (
	// Elmish/Feliz/Fable framework import markers. Any one of these `open`
	// targets (prefix match) flips the file into frontend mode.
	fableImportPrefixes = []string{
		"Elmish",
		"Feliz",
		"Fable.Core",
		"Fable.React",
		"Fable.Import",
		"Fable.Browser",
	}

	// Program.mkProgram / Program.mkSimple — the Elmish bootstrap that wires the
	// init/update/view triad into a running program.
	mkProgramRE = regexp.MustCompile(`\bProgram\.(?:mkProgram|mkSimple)\b`)

	// Cmd dispatch helpers inside update/init bodies.
	cmdDispatchRE = regexp.MustCompile(`\bCmd\.(ofMsg|OfAsync\.\w+|OfPromise\.\w+|OfFunc\.\w+|batch|none|map)\b`)

	// Feliz/Fable.React DSL markers that identify a function as a view/component:
	// `Html.div [...]`, `prop.children`, `React.functionComponent`, an explicit
	// `ReactElement` return annotation, or a JSX-ish `Html.text`.
	felizDSLRE = regexp.MustCompile(`\b(?:Html|Bulma|Daisy|Mui|Bind|Feliz)\.\w+|\bprop\.\w+|\bReact\.\w+|\bReactElement\b`)

	// A call to another PascalCase component function inside a Feliz body, e.g.
	// `Counter.view model dispatch` or `navbar props`. The head identifier is the
	// rendered child component.
	felizChildRE = regexp.MustCompile(`(?m)(?:^[ \t]*|[\[(]\s*|\|>\s*)([A-Za-z_][A-Za-z0-9_']*)[ \t]+(?:"|'|[0-9]|\(|\[|[a-z_])`)
)

// fileUsesFableFrontend reports whether any import marks the file as a Fable
// Elmish/Feliz frontend module.
func fileUsesFableFrontend(imports []string) bool {
	for _, imp := range imports {
		for _, p := range fableImportPrefixes {
			if imp == p || strings.HasPrefix(imp, p+".") {
				return true
			}
		}
	}
	return false
}

// applyElmishFeliz decorates the base entities in place when the file is a Fable
// Elmish/Feliz frontend module. It is a no-op for non-frontend files (wrong
// language never reaches here; no-match files have no Fable imports).
//
// src is the raw F# source, filePath the repo-relative path, imports the
// collected `open` targets, and entities the slice produced by extractFSharp.
func applyElmishFeliz(src, filePath string, imports []string, entities []types.EntityRecord) {
	if !fileUsesFableFrontend(imports) {
		return
	}

	// Pass 1: re-kind the MVU data types (Model / Msg) and tag the triad
	// operations. We index by name so the second component pass can skip them.
	mvuOps := map[string]string{} // operation name → elmish role (init/update/view)
	for i := range entities {
		e := &entities[i]
		switch {
		case e.Kind == "SCOPE.Component" && e.Name == "Model" && e.Subtype == "record":
			e.Kind = "SCOPE.Model"
			e.Subtype = "elmish_model"
			setProp(e, "elmish_role", "model")
			setProp(e, "fable_frontend", "true")
		case e.Kind == "SCOPE.Component" && e.Name == "Msg" && e.Subtype == "discriminated_union":
			e.Kind = "SCOPE.Event"
			e.Subtype = "elmish_msg"
			setProp(e, "elmish_role", "msg")
			setProp(e, "fable_frontend", "true")
		case e.Kind == "SCOPE.Operation":
			if role, ok := elmishTriadRole(e.Name); ok {
				mvuOps[e.Name] = role
				setProp(e, "elmish_role", role)
				setProp(e, "fable_frontend", "true")
			}
		}
	}

	// Pass 2: flag the Elmish program bootstrap and wire Feliz components +
	// command dispatch. Both need the per-operation body, which we re-derive
	// from the entity's line span (the base extractor already computed it).
	lines := strings.Split(src, "\n")
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" && e.Kind != "SCOPE.UIComponent" {
			continue
		}
		body := entityBody(lines, e)
		if body == "" {
			continue
		}

		// Program.mkProgram bootstrap → flag the host operation.
		if mkProgramRE.MatchString(body) {
			setProp(e, "elmish_program", "true")
			setProp(e, "fable_frontend", "true")
		}

		// Command dispatch (init/update return `Model * Cmd<Msg>`).
		for _, m := range cmdDispatchRE.FindAllStringSubmatch(body, -1) {
			helper := "Cmd." + m[1]
			addDispatchEdge(e, helper)
		}

		// Feliz/Fable.React component detection: a `let`-bound function whose body
		// uses the Feliz DSL is a UI component. The MVU `view` is itself a Feliz
		// component, so it is re-kinded too (its elmish_role=view survives).
		if e.Subtype == "let" || mvuOps[e.Name] == "view" {
			if felizDSLRE.MatchString(body) {
				e.Kind = "SCOPE.UIComponent"
				if e.Subtype == "let" {
					e.Subtype = "feliz_component"
				}
				setProp(e, "fable_frontend", "true")
				setProp(e, "ui_framework", "feliz")
				// RENDERS edges to nested component functions.
				addFelizRendersEdges(e, body, filePath)
			}
		}
	}
}

// elmishTriadRole classifies an operation name as part of the Elmish MVU triad.
// The Elmish convention names these `init`, `update`, and `view`; a trailing or
// leading qualifier is tolerated (`initModel`, `updateState`, `viewMain`) so a
// component module's locally-scoped triad is still recognised.
func elmishTriadRole(name string) (string, bool) {
	switch name {
	case "init":
		return "init", true
	case "update":
		return "update", true
	case "view", "render":
		return "view", true
	}
	switch {
	case strings.HasPrefix(name, "init"):
		return "init", true
	case strings.HasPrefix(name, "update"):
		return "update", true
	case strings.HasPrefix(name, "view"):
		return "view", true
	}
	return "", false
}

// addFelizRendersEdges scans a Feliz component body for calls to other
// PascalCase / lower-case component functions and emits a RENDERS edge to each
// unique child, mirroring the React component-composition convention (#610).
func addFelizRendersEdges(e *types.EntityRecord, body, filePath string) {
	seen := map[string]bool{}
	scrubbed := scrubKeepingQuote(body)
	for _, m := range felizChildRE.FindAllStringSubmatch(scrubbed, -1) {
		child := m[1]
		if child == "" || child == e.Name || seen[child] {
			continue
		}
		if fsharpKeywords[child] || isFelizDSLHead(child) {
			continue
		}
		// A child component name is a binding head; skip obvious non-components
		// (single letters, the `prop`/`style`/`Html` DSL roots).
		if len(child) < 2 {
			continue
		}
		seen[child] = true
		e.Relationships = append(e.Relationships, types.RelationshipRecord{
			ToID: extractor.BuildOperationStructuralRef("fsharp", filePath, child),
			Kind: "RENDERS",
		})
	}
}

// isFelizDSLHead reports whether tok is a Feliz/React DSL root that must not be
// mistaken for a child component.
func isFelizDSLHead(tok string) bool {
	switch tok {
	case "Html", "prop", "style", "React", "Feliz", "Bulma", "Daisy", "Mui",
		"Bind", "color", "length", "text", "str", "ofInt", "ofFloat", "ofList":
		return true
	}
	return false
}

// addDispatchEdge records a USES edge stamped with the Cmd helper that produced
// it, de-duplicated per (helper) so a body that dispatches the same helper twice
// emits one edge.
func addDispatchEdge(e *types.EntityRecord, helper string) {
	for _, r := range e.Relationships {
		if r.Kind == "USES" && r.Properties.Get("dispatch") == helper {
			return
		}
	}
	e.Relationships = append(e.Relationships, types.RelationshipRecord{
		ToID: "Cmd",
		Kind: "USES",
		Properties: types.Props{
			{K: "dispatch", V: helper},
			{K: "elmish_command", V: "true"},
		},
	})
}

// entityBody re-derives the source body for an entity from its [StartLine,
// EndLine] span (1-based, inclusive). Returns "" when the span is degenerate.
func entityBody(lines []string, e *types.EntityRecord) string {
	if e.StartLine <= 0 || e.EndLine < e.StartLine || e.EndLine > len(lines) {
		return ""
	}
	return strings.Join(lines[e.StartLine-1:e.EndLine], "\n")
}

// setProp sets a Property on an entity, allocating the map on first use.
func setProp(e *types.EntityRecord, key, val string) {
	if e.Properties == nil {
		e.Properties = map[string]string{}
	}
	e.Properties[key] = val
}
