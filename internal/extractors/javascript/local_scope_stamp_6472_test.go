// Package javascript — #6472: the React props parameter carries the
// local_scope stamp.
//
// This is the ONLY test in the tree that observes the stamp reaching a real
// record. Everything downstream — internal/resolve's isLocalBindingKind and
// internal/mcp's classifyNoise carve-out — is exercised against hand-built
// fixtures, which by construction cannot fail when the emitter changes. Delete
// the `"local_scope": "true"` line from dataflow_react.go's prop Properties map
// and this file is what goes red.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestReactPropsCarryLocalScopeStamp_6472 asserts that every component_prop the
// React dataflow pass emits carries Properties["local_scope"]="true".
//
// A props parameter is a formal parameter of the component function: it exists
// only inside that callable, so it must not take the repository-wide byName
// slot against a same-named import placeholder (#6467). Before #6472 that was
// enforced in the resolver by a `framework == "react"` name check, which could
// not tell this record apart from angular's @Input() or vue's defineProps —
// both of which ARE a component's public surface and DO deserve the slot. The
// stamp is what replaced that check.
//
// Non-vacuity: the test fails if no component_prop is found at all, and it
// asserts the exact prop set the fixture should produce before asserting
// anything about properties.
//
// Scope control: the component function itself, and a module-scope helper, are
// asserted NOT to carry the stamp. Without those, a mutant stamping
// local_scope on every JS/TS entity would pass.
func TestReactPropsCarryLocalScopeStamp_6472(t *testing.T) {
	src := []byte(`interface Props { title: string; count: number }

export function Card({ title, count }: Props) {
  return <h1>{title}: {count}</h1>;
}

export const Banner = (props) => <div>{props.label}</div>;

export function formatTitle(s: string) { return s.trim(); }
`)

	ents := extractReact(t, "Card.tsx", src)

	props := map[string]*types.EntityRecord{}
	for i := range ents {
		if ents[i].Subtype == "component_prop" {
			props[ents[i].Name] = &ents[i]
		}
	}

	// Non-vacuity / positive control: the emitter ran and produced the props
	// this fixture is built to produce. Every assertion below is meaningless
	// without this.
	if len(props) == 0 {
		t.Fatalf("non-vacuity: no component_prop entities extracted at all; %s", dumpKinds(ents))
	}
	for _, want := range []string{"title", "count", "props"} {
		if props[want] == nil {
			t.Fatalf("non-vacuity: expected component_prop %q was not extracted; got %v", want, keysOfProps(props))
		}
	}

	// The assertion under test.
	for name, e := range props {
		if e.Properties["local_scope"] != "true" {
			t.Errorf("component_prop %q: Properties[\"local_scope\"] = %q, want \"true\" — "+
				"a React props parameter exists only inside the component callable and must be "+
				"stamped so internal/resolve's isLocalBindingKind keeps it out of the "+
				"repository-wide byName slot (#6472/#6467); Properties=%v",
				name, e.Properties["local_scope"], e.Properties)
		}
		// The record must stay independently inspectable — that is the reason
		// internal/mcp's classifyNoise carves component_prop out of the
		// noiseLocalScope bucket instead of hiding it from grafel_find. If the
		// emitter ever drops the qualified name or the line span, the carve-out
		// loses its justification and the prop SHOULD be hidden.
		if e.QualifiedName == "" {
			t.Errorf("component_prop %q has no QualifiedName; internal/mcp/denoise.go's "+
				"component_prop carve-out is justified by the prop being addressable", name)
		}
		if e.StartLine == 0 {
			t.Errorf("component_prop %q has StartLine 0; the denoise carve-out is justified by "+
				"the prop having a real source span", name)
		}
	}

	// Scope negative controls: module-scope declarations are NOT locals.
	for _, name := range []string{"Card", "formatTitle"} {
		e := findByName(ents, name)
		if e == nil {
			t.Fatalf("non-vacuity: module-scope declaration %q not extracted; %s", name, dumpKinds(ents))
		}
		if e.Properties["local_scope"] == "true" {
			t.Errorf("module-scope declaration %q must NOT be stamped local_scope — it is "+
				"addressable repo-wide and owns its byName slot; Properties=%v", name, e.Properties)
		}
	}
}

func keysOfProps(m map[string]*types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
