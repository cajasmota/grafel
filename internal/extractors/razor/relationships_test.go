package razor_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/razor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func relsByKind(entities []types.EntityRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
	}
	return out
}

func hasRel(entities []types.EntityRecord, kind, toContains string) bool {
	for _, e := range entities {
		for _, r := range e.Relationships {
			if r.Kind == kind && strings.Contains(r.ToID, toContains) {
				return true
			}
		}
	}
	return false
}

// ---- IMPORTS ---------------------------------------------------------------

func TestRelationships_Imports_SingleUsing(t *testing.T) {
	src := `@using Microsoft.AspNetCore.Components

<h1>Hello</h1>`
	entities := extract(t, "Foo.razor", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Fatalf("IMPORTS count = %d, want 1", len(rels))
	}
	if rels[0].ToID != "Microsoft.AspNetCore.Components" {
		t.Errorf("IMPORTS ToID = %q, want %q", rels[0].ToID, "Microsoft.AspNetCore.Components")
	}
	if rels[0].FromID != "Foo.razor" {
		t.Errorf("IMPORTS FromID = %q, want Foo.razor", rels[0].FromID)
	}
}

func TestRelationships_Imports_Multiple(t *testing.T) {
	src := `@using System
@using System.Linq
@using Microsoft.AspNetCore.Components.Web

<h1>Hi</h1>`
	entities := extract(t, "Multi.razor", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 3 {
		t.Fatalf("IMPORTS count = %d, want 3", len(rels))
	}
	wants := map[string]bool{
		"System":                                 false,
		"System.Linq":                            false,
		"Microsoft.AspNetCore.Components.Web":    false,
	}
	for _, r := range rels {
		if _, ok := wants[r.ToID]; ok {
			wants[r.ToID] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing IMPORTS edge to %q", k)
		}
	}
}

func TestRelationships_Imports_NoneWhenAbsent(t *testing.T) {
	src := `<h1>Hi</h1>`
	entities := extract(t, "Plain.razor", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 0 {
		t.Errorf("IMPORTS count = %d, want 0", len(rels))
	}
}

func TestRelationships_Imports_DedupesIdentical(t *testing.T) {
	src := `@using System
@using System

<h1>Hi</h1>`
	entities := extract(t, "Dup.razor", src)
	rels := relsByKind(entities, "IMPORTS")
	if len(rels) != 1 {
		t.Errorf("IMPORTS dedup count = %d, want 1", len(rels))
	}
}

// ---- CALLS -----------------------------------------------------------------

func TestRelationships_Calls_FromEventHandler(t *testing.T) {
	src := `@code {
    private void IncrementCount()
    {
        DoSomething();
        StateHasChanged();
    }
}`
	entities := extract(t, "Counter.razor", src)
	h := findByName(entities, "IncrementCount")
	if h == nil {
		t.Fatal("IncrementCount handler not found")
	}
	var callTargets []string
	for _, r := range h.Relationships {
		if r.Kind == "CALLS" {
			callTargets = append(callTargets, r.ToID)
		}
	}
	if len(callTargets) < 2 {
		t.Fatalf("CALLS count on IncrementCount = %d, want >= 2 (got %v)", len(callTargets), callTargets)
	}
	wants := map[string]bool{"DoSomething": false, "StateHasChanged": false}
	for _, c := range callTargets {
		if _, ok := wants[c]; ok {
			wants[c] = true
		}
	}
	for k, seen := range wants {
		if !seen {
			t.Errorf("missing CALLS edge to %q (got %v)", k, callTargets)
		}
	}
}

func TestRelationships_Calls_DedupedPerHandler(t *testing.T) {
	src := `@code {
    private void Runner()
    {
        Foo();
        Foo();
        Foo();
    }
}`
	entities := extract(t, "Dedup.razor", src)
	h := findByName(entities, "Runner")
	if h == nil {
		t.Fatal("Runner not found")
	}
	count := 0
	for _, r := range h.Relationships {
		if r.Kind == "CALLS" && r.ToID == "Foo" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("CALLS to Foo count = %d, want 1 (deduped)", count)
	}
}

func TestRelationships_Calls_SkipsKeywords(t *testing.T) {
	src := `@code {
    private void Run()
    {
        if (true) { return; }
        while (false) { }
        for (int i = 0; i < 1; i++) { }
    }
}`
	entities := extract(t, "Kw.razor", src)
	h := findByName(entities, "Run")
	if h == nil {
		t.Fatal("Run not found")
	}
	for _, r := range h.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "if", "while", "for", "switch", "return", "foreach":
			t.Errorf("CALLS edge to keyword %q should not exist", r.ToID)
		}
	}
}

// ---- CONTAINS --------------------------------------------------------------

func TestRelationships_Contains_ComponentToEventHandler(t *testing.T) {
	src := `@code {
    private void OnClick() { }
}`
	entities := extract(t, "Btn.razor", src)
	if len(entities) == 0 {
		t.Fatal("no entities")
	}
	comp := entities[0]
	if comp.Name != "Btn" {
		t.Fatalf("first entity = %q, want Btn", comp.Name)
	}
	wantRef := extractor.BuildOperationStructuralRef("razor", "Btn.razor", "OnClick")
	found := false
	for _, r := range comp.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == wantRef {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CONTAINS edge from component → %q\ngot rels: %+v", wantRef, comp.Relationships)
	}
}

func TestRelationships_Contains_MultipleHandlers(t *testing.T) {
	src := `@code {
    private void OnA() { }
    private void OnB() { }
    private async Task OnC() { }
}`
	entities := extract(t, "Multi.razor", src)
	comp := entities[0]
	count := 0
	for _, r := range comp.Relationships {
		if r.Kind == "CONTAINS" {
			count++
		}
	}
	if count < 3 {
		t.Errorf("CONTAINS count = %d, want >= 3", count)
	}
}

func TestRelationships_Contains_UsesStructuralRef(t *testing.T) {
	src := `@code {
    private void OnClick() { }
}`
	entities := extract(t, "Pages/Foo.razor", src)
	comp := entities[0]
	if !hasRel(entities, "CONTAINS", "scope:operation:method:razor:Pages/Foo.razor:OnClick") {
		t.Errorf("expected CONTAINS structural-ref to scope:operation:method:razor:Pages/Foo.razor:OnClick\ngot: %+v", comp.Relationships)
	}
}

// ---- Combined fixture ------------------------------------------------------

func TestRelationships_Combined_AllThreeKinds(t *testing.T) {
	src := `@using System
@using Microsoft.AspNetCore.Components

<h1>Hello</h1>

@code {
    [Parameter]
    public int Count { get; set; }

    private void IncrementCount()
    {
        Count++;
        StateHasChanged();
    }
}`
	entities := extract(t, "Counter.razor", src)
	if len(relsByKind(entities, "IMPORTS")) < 2 {
		t.Errorf("expected >= 2 IMPORTS")
	}
	if len(relsByKind(entities, "CALLS")) < 1 {
		t.Errorf("expected >= 1 CALLS")
	}
	if len(relsByKind(entities, "CONTAINS")) < 1 {
		t.Errorf("expected >= 1 CONTAINS")
	}
}
