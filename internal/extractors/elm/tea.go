// The Elm Architecture (TEA) decoration pass (#5375, epic #5360 Group A).
//
// Every non-trivial Elm app is structured as The Elm Architecture: an immutable
// `Model` carrying the app state, a `Msg` custom type enumerating every event,
// an `update : Msg -> Model -> ...` reducer, a `view : Model -> Html Msg`
// renderer, and a `main` value wired through `Browser.sandbox/element/document/
// application`. The base extractor (extractor.go) surfaces all of these as
// undifferentiated SCOPE.Component / SCOPE.Operation entities — the state/event/
// render triad is invisible. This pass recognises the pattern and decorates the
// already-extracted entities so the MVU flow is queryable, mirroring the F#
// Elmish/Feliz bootstrap (internal/extractors/fsharp/elmish_feliz.go) — Elmish is
// the F# port of TEA, so the model is directly reusable.
//
// Detection is import-gated (`import Browser` / `import Html`) so a plain Elm
// module (a helper library with no UI) is never mis-classified. When detection
// fails the pass is a no-op — every base entity is returned unchanged.
//
// What it produces (all on top of the base extractor's entities):
//   - the `Model` type alias       → re-kinded SCOPE.Model, subtype tea_model,
//     Properties[tea_role]=model
//   - the `Msg` custom type         → re-kinded SCOPE.Event, subtype tea_msg,
//     Properties[tea_role]=msg (its `|`-separated variants are recorded on
//     Properties[tea_variants] as the event set)
//   - `init` / `update` / `view`    → Properties[tea_role] = init|update|view
//   - the operation wiring `Browser.sandbox/element/document/application`
//     (idiomatically `main`) → Properties[tea_program]=true +
//     Properties[tea_program_kind] = sandbox|element|document|application
//
// Honest scope: detection and decoration are structural (regex/heuristic) like
// the rest of the Elm extractor. Subscriptions, ports, and Cmd/Sub effect
// threading are recognised only insofar as the program operation is flagged;
// per-effect extraction is deferred.
package elm

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

var (
	// teaImportPrefixes are the import markers that flip a file into TEA mode.
	// `Browser` hosts every program entry point; `Html` is present in every
	// view. Either one (prefix match) is sufficient.
	teaImportPrefixes = []string{"Browser", "Html"}

	// teaProgramRE matches the Browser program entry that wires the MVU triad
	// into a running program: Browser.sandbox / element / document / application.
	// Group 1 is the program kind.
	teaProgramRE = regexp.MustCompile(`\bBrowser\.(sandbox|element|document|application)\b`)

	// teaMsgVariantsRE captures the right-hand side of a `type Msg = ...`
	// custom-type declaration so its `|`-separated constructor names can be
	// recorded as the event set. Group 1 is the body up to the next top-level
	// declaration (a line starting at column 0).
	teaMsgVariantsRE = regexp.MustCompile(`(?sm)^type\s+Msg\b[^=]*=\s*(.*?)(?:\n\S|\z)`)

	// teaVariantHeadRE pulls the leading constructor name from one `|`-separated
	// custom-type alternative (e.g. "Increment", "SetName String").
	teaVariantHeadRE = regexp.MustCompile(`^\s*([A-Z][A-Za-z0-9_]*)`)
)

// fileUsesTEA reports whether any import marks the file as a TEA frontend module.
func fileUsesTEA(imports []string) bool {
	for _, imp := range imports {
		for _, p := range teaImportPrefixes {
			if imp == p || strings.HasPrefix(imp, p+".") {
				return true
			}
		}
	}
	return false
}

// applyTEA decorates the base entities in place when the file is a TEA frontend
// module. It is a no-op for non-TEA files (wrong language never reaches here;
// no-match files have no Browser/Html imports).
//
// src is the raw Elm source, imports the collected import targets, and entities
// the slice produced by extractElm.
func applyTEA(src string, imports []string, entities []types.EntityRecord) {
	if !fileUsesTEA(imports) {
		return
	}

	msgVariants := teaMsgVariants(src)

	// Pass 1: re-kind the MVU data types (Model / Msg) and tag the triad
	// operations.
	for i := range entities {
		e := &entities[i]
		switch {
		case e.Kind == "SCOPE.Component" && e.Name == "Model" &&
			(e.Subtype == "typealias" || e.Subtype == "type"):
			e.Kind = "SCOPE.Model"
			e.Subtype = "tea_model"
			setTEAProp(e, "tea_role", "model")
			setTEAProp(e, "tea_frontend", "true")
		case e.Kind == "SCOPE.Component" && e.Name == "Msg" && e.Subtype == "type":
			e.Kind = "SCOPE.Event"
			e.Subtype = "tea_msg"
			setTEAProp(e, "tea_role", "msg")
			setTEAProp(e, "tea_frontend", "true")
			if msgVariants != "" {
				setTEAProp(e, "tea_variants", msgVariants)
			}
		case e.Kind == "SCOPE.Operation":
			if role, ok := teaTriadRole(e.Name); ok {
				setTEAProp(e, "tea_role", role)
				setTEAProp(e, "tea_frontend", "true")
			}
		}
	}

	// Pass 2: flag the Browser program bootstrap. The program operation
	// (idiomatically `main`) is the one whose body wires sandbox/element/
	// document/application. We re-derive the body from the entity's line span.
	lines := strings.Split(src, "\n")
	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		body := teaEntityBody(lines, e)
		if body == "" {
			continue
		}
		if m := teaProgramRE.FindStringSubmatch(body); m != nil {
			setTEAProp(e, "tea_program", "true")
			setTEAProp(e, "tea_program_kind", m[1])
			setTEAProp(e, "tea_frontend", "true")
		}
	}
}

// teaTriadRole classifies an operation name as part of the TEA MVU triad. The
// convention names these `init`, `update`, and `view`; a trailing qualifier is
// tolerated (`updateForm`, `viewMain`) so a component module's locally-scoped
// triad is still recognised.
func teaTriadRole(name string) (string, bool) {
	switch name {
	case "init":
		return "init", true
	case "update":
		return "update", true
	case "view":
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

// teaMsgVariants returns a comma-separated list of the `Msg` custom type's
// constructor names (the event set), or "" when no `type Msg = ...` is present.
func teaMsgVariants(src string) string {
	m := teaMsgVariantsRE.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	var variants []string
	seen := map[string]bool{}
	for _, alt := range strings.Split(m[1], "|") {
		hm := teaVariantHeadRE.FindStringSubmatch(alt)
		if hm == nil {
			continue
		}
		name := hm[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		variants = append(variants, name)
	}
	return strings.Join(variants, ",")
}

// teaEntityBody re-derives the source body for an entity from its [StartLine,
// EndLine] span (1-based, inclusive). Returns "" when the span is degenerate.
func teaEntityBody(lines []string, e *types.EntityRecord) string {
	if e.StartLine <= 0 || e.EndLine < e.StartLine || e.EndLine > len(lines) {
		return ""
	}
	return strings.Join(lines[e.StartLine-1:e.EndLine], "\n")
}

// setTEAProp sets a Property on an entity, allocating the map on first use.
func setTEAProp(e *types.EntityRecord, key, val string) {
	if e.Properties == nil {
		e.Properties = map[string]string{}
	}
	e.Properties[key] = val
}
