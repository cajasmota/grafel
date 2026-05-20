package mcp

// repair_verify implements ADR-0015 trust-model rules R1-R7 on the MCP
// submit path. The indexer's apply-side already validates on apply; this
// layer enforces the same rules at submit time so an agent gets immediate
// feedback rather than discovering a rejection only at the next index run.
//
// Rules mirror docs/specs/repair-trust-model.md exactly.
// Issue: #546 — ADR-0015 #3/8

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/graph"
)

// submitVerifyModuleRe is the R5 module-identifier regex from repair-trust-model.md.
// Same pattern as enrichment.moduleIdentRe; redeclared here to avoid exporting from enrichment.
var submitVerifyModuleRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_\-./]*$`)

// RepairVerifyResult is the structured outcome of trust-model verification on
// a proposed submit. ok=true means the repair passed all rules; ok=false means
// at least one rule rejected it and RejectedReason is set to a stable code.
type RepairVerifyResult struct {
	OK             bool   `json:"ok"`
	RejectedReason string `json:"rejected_reason,omitempty"`
}

// VerifyRepairSubmit runs R1-R7 against a proposed repair before it is
// written to repair.json. It mirrors the apply-side validateRepair in
// internal/enrichment/repair_apply.go but operates against the live
// loaded graph (via docEnts) and the current candidate set (via candidateEdgeIDs).
//
//   - R1  edge_id_unknown:               edgeID not present in candidateEdgeIDs
//   - R2  target_entity_not_found:       bind_to_entity target missing from docEnts
//   - R3  self_loop_disallowed:          bind_to_entity target == fromEntityID
//   - R4  contradicts_contains_hierarchy: target is a CONTAINS ancestor of fromEntityID
//   - R5  invalid_module_identifier:     reclassify_as_external module fails regex
//   - R6  missing_required_field:        conditional required field absent for resolution kind
//   - R7  reasoning_too_short:           reasoning is empty or whitespace-only
func VerifyRepairSubmit(
	rep enrichment.Repair,
	fromEntityID string,
	candidateEdgeIDs map[string]bool,
	docEnts map[string]*graph.Entity,
	containsParents map[string]map[string]bool,
) RepairVerifyResult {
	// R1 — edge_id must match a current candidate.
	if len(candidateEdgeIDs) > 0 && !candidateEdgeIDs[rep.EdgeID] {
		return RepairVerifyResult{RejectedReason: "edge_id_unknown"}
	}

	// R7 — reasoning must be substantive.
	if strings.TrimSpace(rep.Reasoning) == "" {
		return RepairVerifyResult{RejectedReason: "reasoning_too_short"}
	}

	// Resolution-specific R2/R3/R4/R5/R6.
	switch rep.Resolution {
	case enrichment.RepairBindToEntity:
		// R6 — required field.
		if rep.TargetEntityID == "" {
			return RepairVerifyResult{RejectedReason: "missing_required_field"}
		}
		// R2 — target must exist in graph.
		if docEnts != nil {
			if _, ok := docEnts[rep.TargetEntityID]; !ok {
				return RepairVerifyResult{RejectedReason: "target_entity_not_found"}
			}
		}
		// R3 — no self-loop.
		if fromEntityID != "" && rep.TargetEntityID == fromEntityID {
			return RepairVerifyResult{RejectedReason: "self_loop_disallowed"}
		}
		// R4 — target cannot be a CONTAINS ancestor.
		if containsParents != nil {
			if ancestors, ok := containsParents[fromEntityID]; ok {
				if ancestors[rep.TargetEntityID] {
					return RepairVerifyResult{RejectedReason: "contradicts_contains_hierarchy"}
				}
			}
		}
	case enrichment.RepairReclassifyAsExternal:
		// R6 — required field.
		if rep.Module == "" {
			return RepairVerifyResult{RejectedReason: "missing_required_field"}
		}
		// R5 — module must be a safe identifier.
		if !submitVerifyModuleRe.MatchString(rep.Module) ||
			strings.Contains(rep.Module, "..") ||
			strings.HasPrefix(rep.Module, "/") {
			return RepairVerifyResult{RejectedReason: "invalid_module_identifier"}
		}
	case enrichment.RepairReclassifyAsDynamic:
		// R6 — dynamic_reason required.
		if strings.TrimSpace(rep.DynamicReason) == "" {
			return RepairVerifyResult{RejectedReason: "missing_required_field"}
		}
	case enrichment.RepairReclassifyAsResolved:
		// R6 — new_target required.
		if strings.TrimSpace(rep.NewTarget) == "" {
			return RepairVerifyResult{RejectedReason: "missing_required_field"}
		}
	case enrichment.RepairAbandon:
		// R6 — abandon_reason required.
		if strings.TrimSpace(rep.AbandonReason) == "" {
			return RepairVerifyResult{RejectedReason: "missing_required_field"}
		}
	default:
		// Not in the allowlist — the caller checks this separately, but
		// guard here for completeness.
		return RepairVerifyResult{RejectedReason: fmt.Sprintf("resolution_kind_unsupported: %s", rep.Resolution)}
	}

	return RepairVerifyResult{OK: true}
}

// buildVerifyContext extracts the from_entity_id and the CONTAINS-parent
// map from the loaded graph document. Used by handleSubmitRepair to
// populate VerifyRepairSubmit's context arguments.
func buildVerifyContext(doc *graph.Document) (map[string]*graph.Entity, map[string]map[string]bool) {
	if doc == nil {
		return nil, nil
	}
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	// Build transitive CONTAINS-parent map for R4.
	directParents := make(map[string]map[string]bool)
	for _, r := range doc.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		if directParents[r.ToID] == nil {
			directParents[r.ToID] = map[string]bool{}
		}
		directParents[r.ToID][r.FromID] = true
	}
	out := make(map[string]map[string]bool, len(directParents))
	var walk func(id string, acc map[string]bool, seen map[string]bool)
	walk = func(id string, acc map[string]bool, seen map[string]bool) {
		if seen[id] {
			return
		}
		seen[id] = true
		for p := range directParents[id] {
			acc[p] = true
			walk(p, acc, seen)
		}
	}
	for id := range directParents {
		acc := map[string]bool{}
		walk(id, acc, map[string]bool{})
		out[id] = acc
	}
	return byID, out
}

// candidateEdgeIDSet builds the set of known edge_ids from a repo's current
// repair_edge candidates. R1 checks against this set.
func candidateEdgeIDSet(candidates []enrichment.Candidate) map[string]bool {
	s := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		if c.Context == nil {
			continue
		}
		if v, ok := c.Context["edge_id"].(string); ok && v != "" {
			s[v] = true
		}
	}
	return s
}

// fromEntityIDForEdge extracts the from_entity.id from a repair_edge
// candidate that matches edgeID. Returns "" if not found.
func fromEntityIDForEdge(candidates []enrichment.Candidate, edgeID string) string {
	for _, c := range candidates {
		if c.Context == nil {
			continue
		}
		eid, _ := c.Context["edge_id"].(string)
		if eid != edgeID {
			continue
		}
		if fe, ok := c.Context["from_entity"].(map[string]any); ok {
			if id, ok := fe["id"].(string); ok {
				return id
			}
		}
	}
	return ""
}
