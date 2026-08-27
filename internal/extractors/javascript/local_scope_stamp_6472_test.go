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
	"strings"
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
		// noiseLocalScope bucket instead of hiding it from grafel_find. The
		// carve-out's comment names THREE facts as load-bearing: an addressable
		// "Component.prop" QualifiedName, a real StartLine/EndLine span, and a
		// ComputeID()-derived ID that grafel_inspect accepts. All three are
		// asserted here, so the justification cannot quietly stop being true.
		component := e.Properties["component"]
		if component == "" {
			t.Errorf("component_prop %q carries no \"component\" property; the addressable "+
				"name it is checked against below is derived from it", name)
		}
		wantSuffix := component + "." + name
		if !strings.HasSuffix(e.QualifiedName, wantSuffix) {
			t.Errorf("component_prop %q: QualifiedName = %q, want it to end in %q. "+
				"internal/mcp/denoise.go's carve-out is justified by the prop being "+
				"addressable AS \"Component.prop\" — a merely non-empty qualified name does "+
				"not carry that claim", name, e.QualifiedName, wantSuffix)
		}
		if e.StartLine == 0 {
			t.Errorf("component_prop %q has StartLine 0; the denoise carve-out is justified by "+
				"the prop having a real source span", name)
		}
		if e.EndLine == 0 {
			t.Errorf("component_prop %q has EndLine 0; the denoise carve-out cites a real "+
				"StartLine/EndLine span, so both ends have to exist", name)
		}
		if e.EndLine < e.StartLine {
			t.Errorf("component_prop %q has EndLine %d < StartLine %d — not a usable span",
				name, e.EndLine, e.StartLine)
		}
		// The ID half of the claim: grafel_inspect addresses the prop by an ID
		// the record computes from its own identity. Assert it is present AND
		// that it is genuinely that computed value, not an arbitrary string.
		if e.ID == "" {
			t.Errorf("component_prop %q has an empty ID; grafel_inspect addresses it by ID, "+
				"which is the third fact the denoise carve-out rests on", name)
		}
		if want := e.ComputeID(); e.ID != want {
			t.Errorf("component_prop %q: ID = %q, want the record's own ComputeID() %q — "+
				"the carve-out cites a ComputeID()-derived ID specifically", name, e.ID, want)
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

// TestAngularInputIsNotStampedLocalScope_6472 is the OTHER half of the
// justification, and until now it existed only in hand-built fixtures.
//
// The entire argument for stamping React props — and for deleting the
// framework-name clause from internal/resolve's isLocalBindingKind — rests on
// the claim that Angular's @Input() is a DIFFERENT kind of record: a
// component's public surface, addressable from a parent template as
// `<app-chart [Data]="…">`, which must keep the repository-wide byName slot.
// If the Angular emitter ever started stamping local_scope that distinction
// would collapse and #6470's regression would return — silently, because every
// test pinning it uses records typed out by hand.
//
// Both emitters live in this package, so the claim is cheap to assert against
// the real extractor. It is asserted here rather than argued in a comment.
//
// Non-vacuity: the test fails unless the Angular @Input props are actually
// extracted, and it asserts the React control in the SAME run — so "nothing is
// stamped" cannot pass by the stamp being broken everywhere.
func TestAngularInputIsNotStampedLocalScope_6472(t *testing.T) {
	src := []byte("import { Component, Input } from '@angular/core';\n" +
		"\n" +
		"@Component({ selector: 'app-chart', template: '<div></div>' })\n" +
		"export class ChartComponent {\n" +
		"  @Input() Data: string;\n" +
		"  @Input() label = 'x';\n" +
		"}\n")

	ents := extractReact(t, "chart.component.ts", src)

	angularProps := map[string]*types.EntityRecord{}
	for i := range ents {
		e := &ents[i]
		if e.Subtype == "component_prop" && e.Properties["framework"] == "angular" {
			angularProps[e.Name] = e
		}
	}
	// Non-vacuity: the @Input() props must actually exist.
	for _, want := range []string{"Data", "label"} {
		if angularProps[want] == nil {
			t.Fatalf("non-vacuity: Angular @Input() %q was not extracted as a component_prop; %s",
				want, dumpKinds(ents))
		}
	}

	for name, e := range angularProps {
		if e.Properties["local_scope"] == "true" {
			t.Errorf("angular @Input() %q is stamped local_scope=true. An @Input() is the "+
				"component's PUBLIC surface — addressable from a parent template, so it must "+
				"keep the repository-wide byName slot. Stamping it collides the declaration "+
				"into ambiguity against any same-named import placeholder in any language, "+
				"which is the #6470 regression that forced the framework gate #6472 removed; "+
				"Properties=%v", name, e.Properties)
		}
	}

	// The React control, in the same run: the stamp mechanism is working, so
	// the absence above is a real distinction rather than a dead code path.
	react := extractReact(t, "ChartR.tsx", []byte("export function ChartR({ Data }) {\n"+
		"  return <div>{Data}</div>;\n"+
		"}\n"))
	stamped := false
	for i := range react {
		e := &react[i]
		if e.Subtype == "component_prop" && e.Name == "Data" && e.Properties["local_scope"] == "true" {
			stamped = true
		}
	}
	if !stamped {
		t.Fatalf("non-vacuity: the React control prop was not stamped, so the Angular assertion "+
			"above proves nothing about a distinction between the two emitters; %s",
			dumpKinds(react))
	}
}
