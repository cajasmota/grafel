package mcp

// Tests for ADR-0015 trust-model R1-R7 verification on the MCP submit path.
// Issue: #546 — ADR-0015 #3/8

import (
	"testing"

	"github.com/cajasmota/grafel/internal/enrichment"
	"github.com/cajasmota/grafel/internal/graph"
)

// baseRepair returns a valid bind_to_entity repair for tests to mutate.
func baseRepair() enrichment.Repair {
	return enrichment.Repair{
		EdgeID:         "er:aabbccddeeff0011",
		Resolution:     enrichment.RepairBindToEntity,
		TargetEntityID: "bbbbbbbbbbbbbbbb",
		Confidence:     0.9,
		Reasoning:      "imported from b.py per import line",
		Source:         "generate-docs/pass-1a",
	}
}

func baseDocEnts() map[string]*graph.Entity {
	return map[string]*graph.Entity{
		"aaaaaaaaaaaaaaaa": {ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "caller"},
		"bbbbbbbbbbbbbbbb": {ID: "bbbbbbbbbbbbbbbb", Kind: "Function", Name: "target"},
		"cccccccccccccccc": {ID: "cccccccccccccccc", Kind: "Class", Name: "Wrapper"},
	}
}

func baseCandidateEdgeIDs() map[string]bool {
	return map[string]bool{"er:aabbccddeeff0011": true}
}

// TestVerifyRepairSubmit_Happy verifies a valid repair passes all rules.
func TestVerifyRepairSubmit_Happy(t *testing.T) {
	vr := VerifyRepairSubmit(baseRepair(), "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
	if !vr.OK {
		t.Fatalf("expected ok, got rejected_reason=%q", vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R1_EdgeIDUnknown — edge_id not in candidate set.
func TestVerifyRepairSubmit_R1_EdgeIDUnknown(t *testing.T) {
	rep := baseRepair()
	rep.EdgeID = "er:0000000000000000" // not in candidate set
	vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
	if vr.OK || vr.RejectedReason != "edge_id_unknown" {
		t.Fatalf("R1: got ok=%v reason=%q", vr.OK, vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R1_SkippedWhenCandidateSetEmpty — R1 is skipped when
// the candidate set is empty (repo hasn't been indexed yet; agent passes
// edge_id from a cached candidate list).
func TestVerifyRepairSubmit_R1_SkippedWhenCandidateSetEmpty(t *testing.T) {
	rep := baseRepair()
	vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", nil, baseDocEnts(), nil)
	if !vr.OK {
		t.Fatalf("R1 should be skipped on empty candidate set: %q", vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R2_TargetEntityNotFound — bind_to_entity to unknown entity.
func TestVerifyRepairSubmit_R2_TargetEntityNotFound(t *testing.T) {
	rep := baseRepair()
	rep.TargetEntityID = "deadbeefdeadbeef" // not in docEnts
	vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
	if vr.OK || vr.RejectedReason != "target_entity_not_found" {
		t.Fatalf("R2: got ok=%v reason=%q", vr.OK, vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R3_SelfLoopDisallowed — bind_to_entity to own entity.
func TestVerifyRepairSubmit_R3_SelfLoopDisallowed(t *testing.T) {
	rep := baseRepair()
	rep.TargetEntityID = "aaaaaaaaaaaaaaaa" // == fromEntityID
	vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
	if vr.OK || vr.RejectedReason != "self_loop_disallowed" {
		t.Fatalf("R3: got ok=%v reason=%q", vr.OK, vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R4_ContradictsContainsHierarchy — target is a CONTAINS ancestor.
func TestVerifyRepairSubmit_R4_ContradictsContainsHierarchy(t *testing.T) {
	rep := baseRepair()
	rep.TargetEntityID = "cccccccccccccccc" // Wrapper CONTAINS caller
	// Build containsParents so Wrapper is a known ancestor of caller.
	containsParents := map[string]map[string]bool{
		"aaaaaaaaaaaaaaaa": {"cccccccccccccccc": true},
	}
	vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), containsParents)
	if vr.OK || vr.RejectedReason != "contradicts_contains_hierarchy" {
		t.Fatalf("R4: got ok=%v reason=%q", vr.OK, vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R5_InvalidModuleIdentifier — reclassify_as_external with bad module.
func TestVerifyRepairSubmit_R5_InvalidModuleIdentifier(t *testing.T) {
	for _, badModule := range []string{"../etc/passwd", "/absolute", "has spaces", "a..b"} {
		rep := baseRepair()
		rep.Resolution = enrichment.RepairReclassifyAsExternal
		rep.TargetEntityID = ""
		rep.Module = badModule
		vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
		if vr.OK || vr.RejectedReason != "invalid_module_identifier" {
			t.Fatalf("R5 bad module=%q: ok=%v reason=%q", badModule, vr.OK, vr.RejectedReason)
		}
	}
	// Valid module should pass.
	rep := baseRepair()
	rep.Resolution = enrichment.RepairReclassifyAsExternal
	rep.TargetEntityID = ""
	rep.Module = "django.db.models"
	vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
	if !vr.OK {
		t.Fatalf("R5 valid module rejected: %q", vr.RejectedReason)
	}
}

// TestVerifyRepairSubmit_R6_MissingRequiredField — each resolution kind checked.
func TestVerifyRepairSubmit_R6_MissingRequiredField(t *testing.T) {
	cases := []struct {
		resolution string
		mutate     func(*enrichment.Repair)
	}{
		{enrichment.RepairBindToEntity, func(r *enrichment.Repair) { r.TargetEntityID = "" }},
		{enrichment.RepairReclassifyAsExternal, func(r *enrichment.Repair) { r.Module = "" }},
		{enrichment.RepairReclassifyAsDynamic, func(r *enrichment.Repair) { r.DynamicReason = "" }},
		{enrichment.RepairReclassifyAsResolved, func(r *enrichment.Repair) { r.NewTarget = "" }},
		{enrichment.RepairAbandon, func(r *enrichment.Repair) { r.AbandonReason = "" }},
	}
	for _, tc := range cases {
		rep := baseRepair()
		rep.Resolution = tc.resolution
		tc.mutate(&rep)
		vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
		if vr.OK || vr.RejectedReason != "missing_required_field" {
			t.Errorf("R6 resolution=%s: ok=%v reason=%q", tc.resolution, vr.OK, vr.RejectedReason)
		}
	}
}

// TestVerifyRepairSubmit_R7_ReasoningTooShort — empty/whitespace reasoning.
func TestVerifyRepairSubmit_R7_ReasoningTooShort(t *testing.T) {
	for _, bad := range []string{"", "   ", "\t"} {
		rep := baseRepair()
		rep.Reasoning = bad
		vr := VerifyRepairSubmit(rep, "aaaaaaaaaaaaaaaa", baseCandidateEdgeIDs(), baseDocEnts(), nil)
		if vr.OK || vr.RejectedReason != "reasoning_too_short" {
			t.Fatalf("R7 reasoning=%q: ok=%v reason=%q", bad, vr.OK, vr.RejectedReason)
		}
	}
}

// TestBuildVerifyContext_BasicGraph confirms entity map and CONTAINS parent
// map are populated from a simple document.
func TestBuildVerifyContext_BasicGraph(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "caller"},
			{ID: "bbbbbbbbbbbbbbbb", Kind: "Class", Name: "Wrapper"},
		},
		Relationships: []graph.Relationship{
			{FromID: "bbbbbbbbbbbbbbbb", ToID: "aaaaaaaaaaaaaaaa", Kind: "CONTAINS"},
		},
	}
	ents, parents := buildVerifyContext(&LoadedRepo{Doc: doc})
	if len(ents) != 2 {
		t.Fatalf("ents len=%d want 2", len(ents))
	}
	if !parents["aaaaaaaaaaaaaaaa"]["bbbbbbbbbbbbbbbb"] {
		t.Fatalf("CONTAINS parent not found: %+v", parents)
	}
}

// TestCandidateEdgeIDSet confirms the set is populated from context.edge_id.
func TestCandidateEdgeIDSet(t *testing.T) {
	cands := []enrichment.Candidate{
		{Context: map[string]any{"edge_id": "er:001122334455aabb"}},
		{Context: map[string]any{"edge_id": "er:aabbccddeeff0011"}},
		{Context: nil},
	}
	s := candidateEdgeIDSet(cands)
	if !s["er:001122334455aabb"] || !s["er:aabbccddeeff0011"] {
		t.Fatalf("set=%v", s)
	}
	if len(s) != 2 {
		t.Fatalf("len=%d want 2", len(s))
	}
}

// TestFromEntityIDForEdge confirms from_entity.id is extracted from the
// matching candidate.
func TestFromEntityIDForEdge(t *testing.T) {
	cands := []enrichment.Candidate{
		{
			Context: map[string]any{
				"edge_id":     "er:aabbccddeeff0011",
				"from_entity": map[string]any{"id": "aaaaaaaaaaaaaaaa"},
			},
		},
	}
	got := fromEntityIDForEdge(cands, "er:aabbccddeeff0011")
	if got != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("got=%q want aaaaaaaaaaaaaaaa", got)
	}
	// Missing edge returns empty string.
	got2 := fromEntityIDForEdge(cands, "er:0000000000000000")
	if got2 != "" {
		t.Fatalf("missing edge: got=%q", got2)
	}
}
