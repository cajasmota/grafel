// react.go — ReScript-React (@rescript/react) decoration pass (#5378, epic
// #5360 Group A).
//
// ReScript-React (https://rescript-lang.org/docs/react/latest/introduction) is
// the idiomatic React binding for ReScript: a component is a `let` binding —
// conventionally named `make` — annotated with the `@react.component` decorator.
// The decorator's labelled arguments (`~name`, `~onClick`) are the component's
// props, and the body returns JSX (already surfaced as RENDERS edges by the base
// extractor). The base extractor (extractor.go) surfaces these as ordinary
// SCOPE.Operation `let` bindings — the fact that they are React components, and
// their prop set, is invisible.
//
// This pass recognises the pattern and re-kinds the annotated operation,
// mirroring the F# Elmish/Feliz bootstrap (internal/extractors/fsharp/
// elmish_feliz.go) and the Elm TEA pass (internal/extractors/elm/tea.go).
// ReScript compiles to JavaScript and @rescript/react binds the very same
// React runtime as the JS/TS ecosystem, so the JS-ecosystem React model is
// reused rather than re-implemented (the npm package_manager resolves the
// version; the RENDERS/component model is shared).
//
// What it produces (on top of the base extractor's entities):
//   - a `@react.component`-annotated `let` operation → re-kinded
//     SCOPE.UIComponent, subtype react_component, Properties[ui_framework]=
//     rescript-react, Properties[react_component]=true
//   - the labelled-argument prop names (~name, ~onClick) → Properties[props]
//     (comma-joined), so prop_extraction is queryable
//
// Honest scope: detection is heuristic (regex over the decorator + the binding's
// argument list) like the rest of the ReScript extractor. Prop TYPES, hooks
// (React.useState/useEffect), and context are not separately modelled here —
// prop_extraction records the prop NAME set only; the rest is deferred.
package rescript

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

var (
	// reactComponentDecoRE matches the @react.component decorator (optionally
	// parameterised, e.g. @react.component(~props=...)) at the start of a line.
	reactComponentDecoRE = regexp.MustCompile(`(?m)^[ \t]*@react\.component\b`)

	// reactLabelledArgRE matches one labelled argument (a ReScript-React prop)
	// in a binding's argument list: `~name`, `~onClick`, `~name: string`,
	// `~name=?` (optional). Group 1 is the prop name.
	reactLabelledArgRE = regexp.MustCompile(`~([a-z_][a-zA-Z0-9_']*)`)
)

// applyReScriptReact decorates the base entities in place when the file declares
// any @react.component component. It is a no-op when the file uses no
// @react.component decorator (a plain ReScript module is never mis-classified).
//
// src is the raw ReScript source; entities is the slice produced by
// extractReScript. An operation is re-kinded to SCOPE.UIComponent when a
// @react.component decorator sits on the line(s) immediately above its `let`
// binding (blank/comment lines tolerated between the decorator and the binding).
func applyReScriptReact(src string, entities []types.EntityRecord) {
	decoLines := reactComponentDecoratorLines(src)
	if len(decoLines) == 0 {
		return
	}
	lines := strings.Split(src, "\n")

	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" || e.Subtype != "let" {
			continue
		}
		if !decoratorPrecedes(decoLines, e.StartLine) {
			continue
		}
		e.Kind = "SCOPE.UIComponent"
		e.Subtype = "react_component"
		setReactProp(e, "ui_framework", "rescript-react")
		setReactProp(e, "react_component", "true")
		if props := reactComponentProps(lines, e); props != "" {
			setReactProp(e, "props", props)
		}
	}
}

// reactComponentDecoratorLines returns the 1-based line numbers carrying a
// @react.component decorator.
func reactComponentDecoratorLines(src string) []int {
	var out []int
	for _, m := range reactComponentDecoRE.FindAllStringIndex(src, -1) {
		line := strings.Count(src[:m[0]], "\n") + 1
		out = append(out, line)
	}
	return out
}

// decoratorPrecedes reports whether any decorator line sits immediately above
// the binding at bindingLine. A decorator at bindingLine-1 (the common case) or
// separated only by blank lines qualifies. We accept a small gap (≤2 lines) to
// tolerate a doc comment between the decorator and the `let`.
func decoratorPrecedes(decoLines []int, bindingLine int) bool {
	for _, d := range decoLines {
		if d < bindingLine && bindingLine-d <= 3 {
			return true
		}
	}
	return false
}

// reactComponentProps extracts the labelled-argument prop names from a
// component binding's `let make = (~a, ~b) => ...` argument list. It scans from
// the binding's `let` line up to the first `=>` (the body boundary) so props
// declared on continuation lines are still captured. Returns a comma-joined,
// deduplicated, declaration-ordered prop list, or "" when none.
func reactComponentProps(lines []string, e *types.EntityRecord) string {
	if e.StartLine <= 0 || e.StartLine > len(lines) {
		return ""
	}
	// Collect from the binding line up to the arrow (or a few lines, whichever
	// comes first) — the argument list lives between `(` and `) =>`.
	var b strings.Builder
	end := e.StartLine + 6
	if e.EndLine > 0 && e.EndLine < end {
		end = e.EndLine
	}
	if end > len(lines) {
		end = len(lines)
	}
	for i := e.StartLine - 1; i < end; i++ {
		line := lines[i]
		b.WriteString(line)
		b.WriteByte('\n')
		if strings.Contains(line, "=>") {
			break
		}
	}
	scope := b.String()
	// Cut at the first `=>` so body labelled-args (e.g. a nested callback) are
	// not mistaken for props.
	if idx := strings.Index(scope, "=>"); idx >= 0 {
		scope = scope[:idx]
	}

	var props []string
	seen := map[string]bool{}
	for _, m := range reactLabelledArgRE.FindAllStringSubmatch(scope, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		props = append(props, name)
	}
	return strings.Join(props, ",")
}

// setReactProp sets a Property on an entity, allocating the map on first use.
func setReactProp(e *types.EntityRecord, key, val string) {
	if e.Properties == nil {
		e.Properties = map[string]string{}
	}
	e.Properties[key] = val
}
