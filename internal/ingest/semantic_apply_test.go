package ingest

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// placingSection returns the "Placing an order" prompt (which has grounded
// targets placeOrder + validateOrder).
func placingSection(t *testing.T, b SemanticBundle) SemanticSectionPrompt {
	t.Helper()
	for _, sp := range b.Sections {
		if sp.Heading == "Placing an order" {
			return sp
		}
	}
	t.Fatal("missing 'Placing an order' section")
	return SemanticSectionPrompt{}
}

func TestApplySemanticResult_ValidCreatesNodesAndEdges(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/repo")
	code := codeEntitiesFixture("repo")
	bundles := EmitSemanticBundles(repoRoot, "repo", []string{"docs/orders.md"}, code)
	b := bundles[0]
	placing := placingSection(t, b)

	result := SemanticRunResult{
		Version:    b.Version,
		PromptHash: b.PromptHash,
		DocumentID: b.DocumentID,
		SectionResults: []SemanticSectionResult{
			{
				SectionID:          placing.SectionID,
				Class:              SemanticClassDesignDecision,
				Summary:            "placeOrder validates the cart via validateOrder before persisting.",
				RationaleTargetIDs: targetIDs(placing.MentionTargets),
			},
		},
	}

	out, err := ApplySemanticResult(b, result, code)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if out.Stats.DecisionsCreated != 1 {
		t.Fatalf("decisions = %d, want 1", out.Stats.DecisionsCreated)
	}
	if len(out.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(out.Entities))
	}
	dec := out.Entities[0]
	if dec.Kind != string(types.EntityKindDesignDecision) {
		t.Errorf("decision kind = %q, want %q", dec.Kind, types.EntityKindDesignDecision)
	}
	if dec.PropGet("section_id") != placing.SectionID {
		t.Errorf("decision not anchored to section: %q", dec.PropGet("section_id"))
	}

	// Edges: 1 anchor (Section→Decision CONTAINS) + 2 rationale (Decision→target).
	var anchors, rationale int
	for _, rel := range out.Relationships {
		switch rel.Kind {
		case string(types.RelationshipKindContains):
			anchors++
			if rel.FromID != placing.SectionID || rel.ToID != dec.ID {
				t.Errorf("anchor edge mis-linked: %s -> %s", rel.FromID, rel.ToID)
			}
		case string(types.RelationshipKindRationaleFor):
			rationale++
			if rel.FromID != dec.ID {
				t.Errorf("rationale edge not from decision: from=%s", rel.FromID)
			}
		}
	}
	if anchors != 1 {
		t.Errorf("anchor edges = %d, want 1", anchors)
	}
	if rationale != 2 {
		t.Errorf("rationale edges = %d, want 2 (placeOrder + validateOrder)", rationale)
	}
	if out.Stats.RejectedTargets != 0 {
		t.Errorf("rejected_targets = %d, want 0", out.Stats.RejectedTargets)
	}
}

func TestApplySemanticResult_NoneProducesNothing(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/repo")
	code := codeEntitiesFixture("repo")
	b := EmitSemanticBundles(repoRoot, "repo", []string{"docs/orders.md"}, code)[0]
	placing := placingSection(t, b)

	result := SemanticRunResult{
		Version:    b.Version,
		PromptHash: b.PromptHash,
		SectionResults: []SemanticSectionResult{
			{SectionID: placing.SectionID, Class: SemanticClassNone},
		},
	}
	out, err := ApplySemanticResult(b, result, code)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(out.Entities) != 0 || len(out.Relationships) != 0 {
		t.Errorf("None must produce no nodes/edges, got %d/%d", len(out.Entities), len(out.Relationships))
	}
	if out.Stats.SectionsNone != 1 {
		t.Errorf("sections_none = %d, want 1", out.Stats.SectionsNone)
	}
}

func TestApplySemanticResult_RejectsStaleAndMalformed(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/repo")
	code := codeEntitiesFixture("repo")
	b := EmitSemanticBundles(repoRoot, "repo", []string{"docs/orders.md"}, code)[0]
	placing := placingSection(t, b)

	cases := []struct {
		name   string
		result SemanticRunResult
	}{
		{
			name: "version mismatch",
			result: SemanticRunResult{Version: "999", PromptHash: b.PromptHash,
				SectionResults: []SemanticSectionResult{{SectionID: placing.SectionID, Class: SemanticClassNone}}},
		},
		{
			name: "stale prompt hash",
			result: SemanticRunResult{Version: b.Version, PromptHash: "deadbeef",
				SectionResults: []SemanticSectionResult{{SectionID: placing.SectionID, Class: SemanticClassNone}}},
		},
		{
			name: "unknown section id",
			result: SemanticRunResult{Version: b.Version, PromptHash: b.PromptHash,
				SectionResults: []SemanticSectionResult{{SectionID: "not-a-section", Class: SemanticClassSpec, Summary: "x"}}},
		},
		{
			name: "invalid class",
			result: SemanticRunResult{Version: b.Version, PromptHash: b.PromptHash,
				SectionResults: []SemanticSectionResult{{SectionID: placing.SectionID, Class: "Hallucinated", Summary: "x"}}},
		},
		{
			name: "non-None without summary",
			result: SemanticRunResult{Version: b.Version, PromptHash: b.PromptHash,
				SectionResults: []SemanticSectionResult{{SectionID: placing.SectionID, Class: SemanticClassSpec}}},
		},
		{
			name: "duplicate section",
			result: SemanticRunResult{Version: b.Version, PromptHash: b.PromptHash,
				SectionResults: []SemanticSectionResult{
					{SectionID: placing.SectionID, Class: SemanticClassNone},
					{SectionID: placing.SectionID, Class: SemanticClassNone},
				}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ApplySemanticResult(b, tc.result, code)
			if err == nil {
				t.Fatalf("expected rejection, got nil error")
			}
			// No partial corruption: a rejected result produces nothing.
			if len(out.Entities) != 0 || len(out.Relationships) != 0 {
				t.Errorf("rejected result must produce nothing, got %d/%d", len(out.Entities), len(out.Relationships))
			}
		})
	}
}

func TestApplySemanticResult_UnfoundedTargetIsHonestPartial(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/repo")
	code := codeEntitiesFixture("repo")
	b := EmitSemanticBundles(repoRoot, "repo", []string{"docs/orders.md"}, code)[0]
	placing := placingSection(t, b)

	// One valid grounded target + one invented (not a mention-target) ID.
	valid := placing.MentionTargets[0].ID
	result := SemanticRunResult{
		Version:    b.Version,
		PromptHash: b.PromptHash,
		SectionResults: []SemanticSectionResult{
			{
				SectionID:          placing.SectionID,
				Class:              SemanticClassRationale,
				Summary:            "why",
				RationaleTargetIDs: []string{valid, "ffffffffffffffff"},
			},
		},
	}
	out, err := ApplySemanticResult(b, result, code)
	if err != nil {
		t.Fatalf("apply should not fail on a bad target (honest partial): %v", err)
	}
	if out.Stats.DecisionsCreated != 1 {
		t.Errorf("decision still created, got %d", out.Stats.DecisionsCreated)
	}
	if out.Stats.RationaleEdges != 1 {
		t.Errorf("rationale edges = %d, want 1 (only the grounded target)", out.Stats.RationaleEdges)
	}
	if out.Stats.RejectedTargets != 1 {
		t.Errorf("rejected_targets = %d, want 1", out.Stats.RejectedTargets)
	}
}

func TestApplySemanticResult_Idempotent(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/repo")
	code := codeEntitiesFixture("repo")
	b := EmitSemanticBundles(repoRoot, "repo", []string{"docs/orders.md"}, code)[0]
	placing := placingSection(t, b)

	result := SemanticRunResult{
		Version:    b.Version,
		PromptHash: b.PromptHash,
		SectionResults: []SemanticSectionResult{
			{
				SectionID:          placing.SectionID,
				Class:              SemanticClassDesignDecision,
				Summary:            "stable summary",
				RationaleTargetIDs: targetIDs(placing.MentionTargets),
			},
		},
	}
	a1, err := ApplySemanticResult(b, result, code)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := ApplySemanticResult(b, result, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(a1.Entities) != len(a2.Entities) || len(a1.Relationships) != len(a2.Relationships) {
		t.Fatalf("non-idempotent counts: %d/%d vs %d/%d",
			len(a1.Entities), len(a1.Relationships), len(a2.Entities), len(a2.Relationships))
	}
	for i := range a1.Entities {
		if a1.Entities[i].ID != a2.Entities[i].ID {
			t.Errorf("entity ID drift at %d: %q vs %q", i, a1.Entities[i].ID, a2.Entities[i].ID)
		}
	}
	for i := range a1.Relationships {
		if a1.Relationships[i].ID != a2.Relationships[i].ID {
			t.Errorf("rel ID drift at %d: %q vs %q", i, a1.Relationships[i].ID, a2.Relationships[i].ID)
		}
	}
}

func targetIDs(ts []SemanticTarget) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}
