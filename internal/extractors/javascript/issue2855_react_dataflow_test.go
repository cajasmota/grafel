// Package javascript — issue #2855 React Data-Flow proving tests.
//
// Flips React prop_extraction (partial→full) and state_management
// (partial→full) by proving:
//   - generic component props (destructured object pattern + whole-bag) emit
//     SCOPE.Operation subtype="component_prop" with CONTAINS edges from the
//     component (dataflow_react.go), beyond the navigation-only #2665 case,
//   - useState / useReducer state-setter pairs emit SCOPE.Operation
//     subtype="state_setter" (extractor.go), backing state_management with the
//     React-core hooks, not just Zustand.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2855_ReactPropExtraction(t *testing.T) {
	src := []byte(`import { useState } from 'react';

interface Props {
  title: string;
  count: number;
}

export function Card({ title, count = 0, ...rest }: Props) {
  return <h1>{title}: {count}</h1>;
}

export const Banner = (props) => <div>{props.label}</div>;

export const Avatar = ({ src: imageSrc, alt }) => <img src={imageSrc} alt={alt} />;
`)

	ents := extractReact(t, "Card.tsx", src)

	// Card: destructured props → one component_prop per field.
	mustProp := func(component, prop string) {
		t.Helper()
		for i := range ents {
			e := &ents[i]
			if e.Subtype == "component_prop" && e.Name == prop && e.Properties["component"] == component {
				return
			}
		}
		t.Errorf("missing component_prop %s.%s; %s", component, prop, dumpKinds(ents))
	}
	mustProp("Card", "title")
	mustProp("Card", "count")
	mustProp("Card", "rest")

	// Renamed destructure: `{ src: imageSrc }` captures the KEY (`src`), the
	// prop the parent passes — not the local binding.
	mustProp("Avatar", "src")
	mustProp("Avatar", "alt")

	// Whole-bag form: `(props)` → single component_prop named for the bag.
	mustProp("Banner", "props")

	// CONTAINS edge from the component to its props.
	card := findByName(ents, "Card")
	if card == nil {
		t.Fatalf("Card component not extracted")
	}
	var titleProp *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype == "component_prop" && ents[i].Name == "title" {
			titleProp = &ents[i]
		}
	}
	if titleProp == nil {
		t.Fatalf("title prop entity missing")
	}
	if !hasRel(card.Relationships, "CONTAINS", titleProp.ID) {
		t.Errorf("Card missing CONTAINS → title prop (id=%s); rels=%v", titleProp.ID, card.Relationships)
	}
}

func TestIssue2855_ReactStateManagement(t *testing.T) {
	src := []byte(`import { useState, useReducer } from 'react';

export function Counter() {
  const [count, setCount] = useState(0);
  const [state, dispatch] = useReducer(reducer, initialState);
  return <button onClick={() => setCount(count + 1)}>{count}</button>;
}
`)

	ents := extractReact(t, "Counter.tsx", src)

	// state_management: useState / useReducer setters emit state_setter ops.
	mustSetter := func(name string) {
		t.Helper()
		for i := range ents {
			if ents[i].Subtype == "state_setter" && ents[i].Name == name {
				return
			}
		}
		t.Errorf("missing state_setter %q; %s", name, dumpKinds(ents))
	}
	mustSetter("setCount")
	mustSetter("dispatch")
}
