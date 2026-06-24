// react.go — Reason-React ([@react.component]) decoration pass (#5379, epic
// #5360 Group A).
//
// Reason-React (https://reasonml.github.io/reason-react/) is the idiomatic React
// binding for ReasonML — the legacy predecessor of ReScript-React. A component
// is a `let` binding, conventionally named `make`, annotated with the
// `[@react.component]` attribute. ReasonML uses the BRACKET-ATTRIBUTE syntax
// `[@react.component]` (OCaml/Reason attributes), where ReScript uses the bare
// decorator `@react.component`; the React model is otherwise identical. The
// attribute's labelled arguments (`~name`, `~onClick`) are the component's
// props.
//
// The base extractor (extractor.go) surfaces these as ordinary SCOPE.Operation
// `let` bindings — the fact that they are React components, and their prop set,
// is invisible. This pass recognises the pattern and re-kinds the annotated
// operation, mirroring the ReScript-React bootstrap (internal/extractors/
// rescript/react.go), the F# Elmish/Feliz bootstrap, and the Elm TEA pass.
//
// Reason compiles to JavaScript and Reason-React binds the very same React
// runtime as the JS/TS ecosystem, so the JS-ecosystem React model is reused
// rather than re-implemented (the npm package_manager resolves the version via
// the sibling bsconfig.json / package.json).
//
// What it produces (on top of the base extractor's entities):
//   - a `[@react.component]`-annotated `let` operation → re-kinded
//     SCOPE.UIComponent, subtype react_component, Properties[ui_framework]=
//     reason-react, Properties[react_component]=true
//   - the labelled-argument prop names (~name, ~onClick) → Properties[props]
//     (comma-joined), so prop_extraction is queryable
//
// Honest scope: detection is heuristic (regex over the attribute + the binding's
// argument list) like the rest of the ReasonML extractor. Prop TYPES, hooks
// (React.useState/useReducer), context, and JSX RENDERS edges are not modelled
// here — prop_extraction records the prop NAME set only; the rest is deferred.
package reasonml

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

var (
	// reactComponentAttrRE matches the Reason `[@react.component]` attribute
	// (optionally parameterised, e.g. `[@react.component ~props=...]`) anywhere
	// on a line. Reason attributes use the bracket form `[@attr]`; the leading
	// `[@` may be preceded by other tokens (it is commonly on its own line above
	// the binding, but can also be inline).
	reactComponentAttrRE = regexp.MustCompile(`\[@react\.component\b`)

	// reactLabelledArgRE matches one labelled argument (a Reason-React prop) in a
	// binding's argument list: `~name`, `~onClick`, `~name: string`, `~name=?`
	// (optional). Group 1 is the prop name.
	reactLabelledArgRE = regexp.MustCompile(`~([a-z_][a-zA-Z0-9_']*)`)
)

// applyReasonReact decorates the base entities in place when the file declares
// any [@react.component] component. It is a no-op when the file uses no
// [@react.component] attribute (a plain ReasonML module is never mis-classified).
//
// src is the raw ReasonML source; entities is the slice produced by
// extractReasonML. An operation is re-kinded to SCOPE.UIComponent when a
// [@react.component] attribute sits on the line(s) immediately above its `let`
// binding (blank/comment lines tolerated), or inline on the binding line itself.
func applyReasonReact(src string, entities []types.EntityRecord) {
	attrLines := reactComponentAttrLines(src)
	if len(attrLines) == 0 {
		return
	}
	lines := strings.Split(src, "\n")

	for i := range entities {
		e := &entities[i]
		if e.Kind != "SCOPE.Operation" || e.Subtype != "let" {
			continue
		}
		if !attrPrecedes(attrLines, e.StartLine) {
			continue
		}
		e.Kind = "SCOPE.UIComponent"
		e.Subtype = "react_component"
		setReactProp(e, "ui_framework", "reason-react")
		setReactProp(e, "react_component", "true")
		if props := reactComponentProps(lines, e); props != "" {
			setReactProp(e, "props", props)
		}
	}
}

// reactComponentAttrLines returns the 1-based line numbers carrying a
// [@react.component] attribute.
func reactComponentAttrLines(src string) []int {
	var out []int
	for _, m := range reactComponentAttrRE.FindAllStringIndex(src, -1) {
		line := strings.Count(src[:m[0]], "\n") + 1
		out = append(out, line)
	}
	return out
}

// attrPrecedes reports whether any attribute line sits on or immediately above
// the binding at bindingLine. An attribute on the binding line itself (inline
// `[@react.component] let make = ...`), at bindingLine-1 (the common case), or
// separated only by blank/comment lines qualifies. We accept a small gap
// (≤2 lines above) to tolerate a doc comment between the attribute and the `let`.
func attrPrecedes(attrLines []int, bindingLine int) bool {
	for _, a := range attrLines {
		if a <= bindingLine && bindingLine-a <= 3 {
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
