// dataflow_react.go — React Data-Flow extraction for the JS/TS AST extractor
// (issue #2855, Data Flow group). Closes the prop_extraction and
// state_management partials.
//
// Prior to this file the Data-Flow surface for React was incomplete:
//
//   - prop_extraction was PARTIAL — only JSX *navigation* props (Link `to`,
//     Navigate `to`, etc., via navigation.go #2665) were recognised. Generic
//     component props (the destructured `{ title, count }` parameter of a
//     function component, or a typed `props: Props` parameter) were not.
//   - state_management was PARTIAL — only Zustand selector bindings
//     (zustand_store.go #2632) were cited; the React-core useState/useReducer
//     state-setter pairs (already emitted as subtype="state_setter" in
//     extractor.go) were not attributed to the Data-Flow cell.
//
// This file adds generic React component-prop extraction:
//
//	function Card({ title, count }: Props) { … }   → component_prop entities
//	const Card = (props) => …                      → component_prop (whole-object)
//
// Each prop becomes a SCOPE.Operation entity subtype="component_prop" and the
// component entity gains a CONTAINS edge to it (reusing existing Kinds — no new
// entity/edge Kind, so internal/types/ stays green). grafel_find can then
// filter `subtype:component_prop` to enumerate a component's prop surface, and
// the Data-Flow/prop_extraction cell is honestly backed by AST extraction
// rather than the navigation-only special case.
package javascript

import (
	"fmt"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// extractComponentProps inspects a React component's parameter list and returns
// (a) the SCOPE.Operation prop entities to append and (b) CONTAINS edges from
// the component to each prop. It only fires for PascalCase component names —
// utility functions and hooks do not have a "prop surface".
//
// Supported parameter shapes (the conventional React component signatures):
//
//	({ title, count })            object_pattern         → one prop per field
//	({ title, count }: Props)     object_pattern + type  → one prop per field
//	(props)                       identifier             → single whole-props bag
//	(props: Props)                identifier + type      → single whole-props bag
//
// A second `ref`/context parameter (forwardRef) is ignored — only the first
// parameter carries props in React.
func (x *extractor) extractComponentProps(params ts.Node, componentName string) ([]types.EntityRecord, []types.RelationshipRecord) {
	if params == nil || !isComponentName(componentName) {
		return nil, nil
	}
	first := firstFormalParameter(params)
	if first == nil {
		return nil, nil
	}

	var ents []types.EntityRecord
	var rels []types.RelationshipRecord

	emit := func(propName, sig string, node ts.Node) {
		if propName == "" {
			return
		}
		start, end := lines(node)
		qn := fmt.Sprintf("%s.%s", componentName, propName)
		e := types.EntityRecord{
			Name:          propName,
			QualifiedName: x.qualify(qn),
			Kind:          "SCOPE.Operation",
			SourceFile:    x.filePath,
			StartLine:     start,
			EndLine:       end,
			Language:      x.language,
			Subtype:       "component_prop",
			Signature:     sig,
			Properties: map[string]string{
				"kind":      "SCOPE.Operation",
				"subtype":   "component_prop",
				"component": componentName,
				"prop":      propName,
				"framework": "react",
			},
			EnrichmentStatus: types.StatusPending,
			QualityScore:     1.0,
		}
		e.ID = e.ComputeID()
		ents = append(ents, e)
		rels = append(rels, types.RelationshipRecord{
			ToID: e.ID,
			Kind: "CONTAINS",
			Properties: map[string]string{
				"component": componentName,
				"prop":      propName,
				"framework": "react",
			},
		})
	}

	// The formal parameter node is either a `required_parameter` /
	// `optional_parameter` (TS grammar) wrapping a pattern, or the bare pattern
	// (JS grammar). Unwrap to the binding pattern.
	pat := bindingPatternOf(first)
	if pat == nil {
		return nil, nil
	}

	switch pat.Type() {
	case "object_pattern":
		for _, field := range objectPatternFields(x, pat) {
			emit(field, fmt.Sprintf("prop %s.%s", componentName, field), pat)
		}
	case "identifier":
		name := x.nodeText(pat)
		// `(props)` — the whole-props bag. Emit a single prop entity named for
		// the bag so the cell is backed even when props are passed wholesale.
		emit(name, fmt.Sprintf("props %s(%s)", componentName, name), pat)
	}
	return ents, rels
}

// firstFormalParameter returns the first non-punctuation child of a
// formal_parameters node (the parameter list `( … )`).
func firstFormalParameter(params ts.Node) ts.Node {
	for i := 0; i < int(params.ChildCount()); i++ {
		c := params.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "(", ")", ",":
			continue
		}
		return c
	}
	return nil
}

// bindingPatternOf unwraps a TS required_parameter/optional_parameter to its
// `pattern` child, or returns the node directly when it is already a pattern
// (JS grammar shape).
func bindingPatternOf(param ts.Node) ts.Node {
	switch param.Type() {
	case "required_parameter", "optional_parameter":
		if p := param.ChildByFieldName("pattern"); p != nil {
			return p
		}
		// Fallback: first object_pattern / identifier child.
		for i := 0; i < int(param.ChildCount()); i++ {
			c := param.Child(i)
			if c != nil && (c.Type() == "object_pattern" || c.Type() == "identifier") {
				return c
			}
		}
		return nil
	default:
		return param
	}
}

// objectPatternFields returns the distinct destructured field names of an
// object_pattern, handling shorthand (`{ title }`), renamed (`{ title: t }`),
// and defaulted (`{ count = 0 }`) forms. The key (exported name) is captured —
// it is the prop the parent passes.
func objectPatternFields(x *extractor, pat ts.Node) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for i := 0; i < int(pat.ChildCount()); i++ {
		c := pat.Child(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "shorthand_property_identifier_pattern", "shorthand_property_identifier":
			add(x.nodeText(c))
		case "pair_pattern":
			if k := c.ChildByFieldName("key"); k != nil {
				add(x.nodeText(k))
			}
		case "object_assignment_pattern":
			// `{ count = 0 }` — the left side is the binding/key.
			if l := c.ChildByFieldName("left"); l != nil {
				add(x.nodeText(l))
			} else if c.NamedChildCount() > 0 {
				add(x.nodeText(c.NamedChild(0)))
			}
		case "rest_pattern":
			// `{ ...rest }` — capture the rest binding name.
			if c.NamedChildCount() > 0 {
				add(x.nodeText(c.NamedChild(0)))
			}
		}
	}
	return out
}
