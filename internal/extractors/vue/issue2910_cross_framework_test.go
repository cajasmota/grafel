package vue_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2910 — cross-framework reuse of the React-ecosystem TanStack Query +
// Redux/RTK detectors in Vue. @tanstack/vue-query (useQuery/useMutation/
// useInfiniteQuery) and framework-agnostic Redux Toolkit (createSlice/
// configureStore) used inside a Vue SFC are decorated as SCOPE.Operation
// entities with CONTAINS edges from the component.

func componentContains(ents []types.EntityRecord, toID string) bool {
	for i := range ents {
		if ents[i].Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "CONTAINS" && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

func TestIssue2910_VueCrossFrameworkQuery(t *testing.T) {
	src := loadVueFixture(t, "CrossFrameworkQuery.vue")
	ents := extract(t, "src/components/CrossFrameworkQuery.vue", src)

	// TanStack vue-query: one tanstack_query op per entry-point call kind.
	wantTanstack := []struct{ name, kind string }{
		{"tanstack:useQuery", "query"},
		{"tanstack:useMutation", "mutation"},
		{"tanstack:useInfiniteQuery", "infinite_query"},
	}
	for _, w := range wantTanstack {
		e := findByName(ents, w.name)
		if e == nil {
			t.Fatalf("missing tanstack_query entity %s; %s", w.name, dump(ents))
		}
		if e.Subtype != "tanstack_query" {
			t.Errorf("%s subtype = %q, want tanstack_query", w.name, e.Subtype)
		}
		if e.Properties["query_kind"] != w.kind {
			t.Errorf("%s query_kind = %q, want %q", w.name, e.Properties["query_kind"], w.kind)
		}
		if e.Properties["framework"] != "vue" || e.Properties["via"] != "tanstack_query" {
			t.Errorf("%s props = %v, want framework=vue via=tanstack_query", w.name, e.Properties)
		}
		if !componentContains(ents, w.name) {
			t.Errorf("component missing CONTAINS -> %s", w.name)
		}
	}

	// Redux Toolkit (framework-agnostic) used in a Vue app.
	wantRedux := []struct{ name, subtype string }{
		{"rtk:createSlice", "redux_slice"},
		{"rtk:configureStore", "redux_store"},
	}
	for _, w := range wantRedux {
		e := findByName(ents, w.name)
		if e == nil {
			t.Fatalf("missing redux entity %s; %s", w.name, dump(ents))
		}
		if e.Subtype != w.subtype {
			t.Errorf("%s subtype = %q, want %q", w.name, e.Subtype, w.subtype)
		}
		if e.Properties["via"] != "redux" {
			t.Errorf("%s via = %q, want redux", w.name, e.Properties["via"])
		}
		if !componentContains(ents, w.name) {
			t.Errorf("component missing CONTAINS -> %s", w.name)
		}
	}
}

// A Vue component that imports none of the cross-framework packages must not be
// decorated (the detector is gated on the import package, so a local function
// named useQuery does not get mis-tagged).
func TestIssue2910_VueNoFalsePositive(t *testing.T) {
	src := `<script setup>
function useQuery() { return null }
const r = useQuery()
const x = createSlice()
</script>
<template><div>{{ r }}</div></template>`
	ents := extract(t, "src/components/Plain.vue", src)
	for i := range ents {
		if ents[i].Subtype == "tanstack_query" || ents[i].Properties["via"] == "redux" {
			t.Errorf("unexpected cross-framework decoration without import: %s (%s)", ents[i].Name, ents[i].Subtype)
		}
	}
}
