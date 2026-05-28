package svelte_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2877 — Svelte Internals taxonomy: prove the seven idiom cells from a
// single hand-written fixture (Comp.svelte) against its golden artifact.
//
//	(A) recording: runes ($state/$derived/$effect), props (export let + $props),
//	    stores (writable), context (setContext/getContext), SFC blocks.
//	(B) implemented here: reactive `$:` statements + `use:` actions.
type goldenEntity struct {
	Name    string `json:"name"`
	Subtype string `json:"subtype"`
}

type svelteInternalsGolden struct {
	Component     string         `json:"component"`
	Entities      []goldenEntity `json:"entities"`
	ComponentUses []string       `json:"component_uses"`
}

func loadSvelteInternalsFixture(t *testing.T) ([]types.EntityRecord, svelteInternalsGolden) {
	t.Helper()
	dir := filepath.Join("..", "javascript", "testdata", "svelte_internals")
	src, err := os.ReadFile(filepath.Join(dir, "Comp.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	goldenBytes, err := os.ReadFile(filepath.Join(dir, "Comp.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden svelteInternalsGolden
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	recs := mustExtract(t, "Comp.svelte", string(src))
	return recs, golden
}

func hasEntity(recs []types.EntityRecord, name, subtype string) bool {
	for _, e := range recs {
		if e.Name == name && e.Subtype == subtype && e.Kind == "SCOPE.Operation" {
			return true
		}
	}
	return false
}

func TestIssue2877_SvelteInternals_Golden(t *testing.T) {
	recs, golden := loadSvelteInternalsFixture(t)

	comp := findByName(recs, golden.Component)
	if comp == nil {
		t.Fatalf("component %q not extracted", golden.Component)
	}

	for _, want := range golden.Entities {
		if !hasEntity(recs, want.Name, want.Subtype) {
			t.Errorf("missing entity name=%q subtype=%q; got: %s", want.Name, want.Subtype, dump(recs))
		}
	}

	for _, toID := range golden.ComponentUses {
		if !relTo(comp.Relationships, "USES", toID) {
			t.Errorf("component %q missing USES → %q", golden.Component, toID)
		}
	}
}

// TestIssue2877_ReactiveStatements proves (B) reactive_statements: a `$:`
// assignment declares a named reactive op with a DEPENDS_ON edge, and a `$:`
// block surfaces as a positional reactive op.
func TestIssue2877_ReactiveStatements(t *testing.T) {
	recs, _ := loadSvelteInternalsFixture(t)

	reactives := findBySubtype(recs, "reactive_statement")
	if len(reactives) < 2 {
		t.Fatalf("expected >=2 reactive_statement entities, got %d", len(reactives))
	}

	quad := findByName(recs, "quadrupled")
	if quad == nil || quad.Subtype != "reactive_statement" {
		t.Fatalf("expected reactive assignment 'quadrupled'")
	}
	if quad.Properties["reactive_kind"] != "assignment" {
		t.Errorf("quadrupled reactive_kind = %q, want assignment", quad.Properties["reactive_kind"])
	}
	if !relTo(quad.Relationships, "DEPENDS_ON", "state:quadrupled") {
		t.Errorf("quadrupled missing DEPENDS_ON → state:quadrupled; rels=%v", quad.Relationships)
	}

	block := findByName(recs, "$:_1")
	if block == nil || block.Properties["reactive_kind"] != "block" {
		t.Errorf("expected reactive block '$:_1' with reactive_kind=block")
	}
}

// TestIssue2877_Actions proves (B) actions: each distinct `use:` directive
// emits a deduplicated SCOPE.Operation subtype=action + a USES edge.
func TestIssue2877_Actions(t *testing.T) {
	recs, _ := loadSvelteInternalsFixture(t)

	actions := findBySubtype(recs, "action")
	names := map[string]bool{}
	for _, a := range actions {
		names[a.Name] = true
		if a.Properties["action"] == "" {
			t.Errorf("action %q missing action property", a.Name)
		}
	}
	for _, want := range []string{"use:tooltip", "use:clickOutside"} {
		if !names[want] {
			t.Errorf("missing action %q; got %v", want, names)
		}
	}
}
