package mcp

// inspect_discriminators_2666_test.go — verifies that handleGetNode renders a
// discriminators[] section when the inspected entity has DISCRIMINATES_ON
// edges (#2666). Each row carries file_line, line, literal, and other_side.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestInspect_RendersDiscriminatorsSection seeds an entity that participates
// in two DISCRIMINATES_ON edges (checklistType=2 on line 17 and type=periodic
// on line 23) and verifies that archigraph_inspect surfaces a discriminators[]
// array with both rows correctly populated.
func TestInspect_RendersDiscriminatorsSection(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "fn1", Name: "processChecklist",
				Kind:       "SCOPE.Operation",
				SourceFile: "src/checklist.ts",
				StartLine:  15,
			},
		},
		Relationships: []graph.Relationship{
			{
				ID: "d1", FromID: "fn1", ToID: "var:checklistType",
				Kind: "DISCRIMINATES_ON",
				Properties: map[string]string{
					"line":    "17",
					"literal": "2",
				},
			},
			{
				ID: "d2", FromID: "fn1", ToID: "var:type",
				Kind: "DISCRIMINATES_ON",
				Properties: map[string]string{
					"line":    "23",
					"literal": "periodic",
				},
			},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "fn1")

	rawDiscs, ok := out["discriminators"]
	if !ok {
		t.Fatalf("inspect response missing 'discriminators' key; got keys: %v", mapKeys(out))
	}
	arr, ok := rawDiscs.([]any)
	if !ok {
		t.Fatalf("'discriminators' is %T, want []any", rawDiscs)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 discriminator rows, got %d: %v", len(arr), arr)
	}

	byLiteral := map[string]map[string]any{}
	for _, r := range arr {
		m := r.(map[string]any)
		lit, _ := m["literal"].(string)
		byLiteral[lit] = m
	}

	row, ok := byLiteral["2"]
	if !ok {
		t.Fatalf("missing row for literal=2; got %v", byLiteral)
	}
	if v, _ := row["other_side"].(string); v != "var:checklistType" {
		t.Errorf("row[literal=2].other_side=%q, want %q", v, "var:checklistType")
	}
	if v, _ := row["line"].(float64); int(v) != 17 {
		t.Errorf("row[literal=2].line=%v, want 17", row["line"])
	}
	if v, _ := row["file_line"].(string); v != "src/checklist.ts:17" {
		t.Errorf("row[literal=2].file_line=%q, want %q", v, "src/checklist.ts:17")
	}

	row, ok = byLiteral["periodic"]
	if !ok {
		t.Fatalf("missing row for literal=periodic; got %v", byLiteral)
	}
	if v, _ := row["other_side"].(string); v != "var:type" {
		t.Errorf("row[literal=periodic].other_side=%q, want %q", v, "var:type")
	}
	if v, _ := row["line"].(float64); int(v) != 23 {
		t.Errorf("row[literal=periodic].line=%v, want 23", row["line"])
	}
}

// TestInspect_OmitsDiscriminatorsWhenAbsent verifies that an entity with no
// DISCRIMINATES_ON edges produces an inspect envelope WITHOUT a discriminators
// key (omitted entirely rather than empty array, to keep the response lean).
func TestInspect_OmitsDiscriminatorsWhenAbsent(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "fn1", Name: "noDiscriminator",
				Kind: "SCOPE.Operation", SourceFile: "a.ts", StartLine: 1,
			},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "fn1")
	if _, present := out["discriminators"]; present {
		t.Errorf("expected 'discriminators' key to be omitted; got %v", out["discriminators"])
	}
}
