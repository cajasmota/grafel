package svelte_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2910 — cross-framework reuse of the React-ecosystem TanStack Query +
// Redux/RTK detectors in Svelte. @tanstack/svelte-query (createQuery/
// createMutation/createInfiniteQuery) and framework-agnostic Redux Toolkit
// (createSlice/configureStore) used inside a Svelte component are decorated as
// SCOPE.Operation entities with CONTAINS edges from the component.

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

func TestIssue2910_SvelteCrossFrameworkQuery(t *testing.T) {
	dir := filepath.Join("..", "javascript", "testdata", "svelte_internals")
	src, err := os.ReadFile(filepath.Join(dir, "CrossFrameworkQuery.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := mustExtract(t, "src/lib/CrossFrameworkQuery.svelte", string(src))

	wantTanstack := []struct{ name, kind string }{
		{"tanstack:createQuery", "query"},
		{"tanstack:createMutation", "mutation"},
		{"tanstack:createInfiniteQuery", "infinite_query"},
	}
	for _, w := range wantTanstack {
		e := findByName(ents, w.name)
		if e == nil {
			t.Fatalf("missing tanstack_query entity %s", w.name)
		}
		if e.Subtype != "tanstack_query" {
			t.Errorf("%s subtype = %q, want tanstack_query", w.name, e.Subtype)
		}
		if e.Properties["query_kind"] != w.kind {
			t.Errorf("%s query_kind = %q, want %q", w.name, e.Properties["query_kind"], w.kind)
		}
		if e.Properties["framework"] != "svelte" {
			t.Errorf("%s framework = %q, want svelte", w.name, e.Properties["framework"])
		}
		if !componentContains(ents, w.name) {
			t.Errorf("component missing CONTAINS -> %s", w.name)
		}
	}

	wantRedux := []struct{ name, subtype string }{
		{"rtk:createSlice", "redux_slice"},
		{"rtk:configureStore", "redux_store"},
	}
	for _, w := range wantRedux {
		e := findByName(ents, w.name)
		if e == nil {
			t.Fatalf("missing redux entity %s", w.name)
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

// No import → no decoration.
func TestIssue2910_SvelteNoFalsePositive(t *testing.T) {
	src := `<script>
  function createQuery() { return null }
  const r = createQuery()
</script>
<div>{r}</div>`
	ents := mustExtract(t, "src/lib/Plain.svelte", src)
	for i := range ents {
		if ents[i].Subtype == "tanstack_query" || ents[i].Properties["via"] == "redux" {
			t.Errorf("unexpected cross-framework decoration without import: %s", ents[i].Name)
		}
	}
}
