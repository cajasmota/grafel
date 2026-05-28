package javascript_test

import "testing"

// issue2858_metafw_state_setter_test.go — proving fixture for the
// meta-framework Lifecycle/state_setter_emission cells (issue #2858) on the
// React-based meta-frameworks (Next.js / Remix / Gatsby). Their page/route
// modules are ordinary React+TSX, so the shared AST extractor lifts useState /
// useReducer setters as SCOPE.Operation subtype="state_setter" (issue #513)
// exactly as it does for any React file. This test proves that path fires on a
// representative meta-framework client page component.
//
// Nuxt (Vue) and SvelteKit (Svelte) state-setter emission is covered by the
// vue / svelte extractors (vue ref/reactive writes, svelte $state runes / store
// .set/.update) — see internal/extractors/{vue,svelte}/issue2856_test.go.

func TestMetaFw2858ReactStateSetterEmission(t *testing.T) {
	// A Next.js App-Router client page component ('use client') with useState +
	// useReducer — the same shape Remix/Gatsby page components take.
	src := []byte(`'use client'
import { useState, useReducer } from 'react'

export default function ProfilePage() {
  const [open, setOpen] = useState(false)
  const [state, dispatch] = useReducer(reducer, init)
  return <button onClick={() => setOpen(!open)}>{state.label}</button>
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	for _, setter := range []string{"setOpen", "dispatch"} {
		e := findByName(entities, setter)
		if e == nil {
			t.Errorf("setter %q not found; names: %v", setter, entityNames(entities))
			continue
		}
		if e.Subtype != "state_setter" {
			t.Errorf("%q: subtype=%q, want state_setter (state_setter_emission)", setter, e.Subtype)
		}
	}
}
