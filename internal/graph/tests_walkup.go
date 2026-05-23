// Package graph — tests_walkup.go implements the TESTS edge walk-up pass.
//
// Problem: the testmap extractor emits TESTS edges that point at the helper
// Operation a test directly calls (e.g. `test_create_order → _calculate_total`).
// The helper is not a meaningful coverage target; the real subject is the
// viewset / handler / controller that wraps it.  Coverage metrics therefore
// severely undercount TESTS coverage — 0.1% (87 edges / 8 709 production
// entities) was measured on a real corpus before this pass.
//
// Algorithm (DeriveTestsWalkUp):
//
//  1. Build an inbound-CALLS index: helperID → []callerIDs for ALL production
//     entities (not just helpers — we filter per-entity below).
//
//  2. Scan every TESTS edge.  For each target that is a production entity,
//     check whether it has inbound CALLS edges from other production entities
//     (i.e., it is called by viewsets / handlers / controllers and not just
//     directly tested).
//
//  3. If the caller set is non-empty:
//     a. Filter to at most maxCallersPerHelper callers (configurable, default 5).
//        When the caller set is larger than the limit the helper is almost
//        certainly a widely-shared utility — emitting derived edges for every
//        caller would inflate coverage; skip instead.
//     b. For each selected caller, emit a derived TESTS relationship:
//           test_fn → caller  (Kind="TESTS")
//        with properties:
//           derived=helper:<helperID>
//           confidence=0.7    (below 1.0 to distinguish from explicit edges)
//           source=tests-walkup
//
//  4. Deduplicate: if an explicit TESTS edge already covers the caller, skip.
//
// The derived edges are appended to doc.Relationships so all downstream
// consumers (ComputeCoverage, MCP tools, dashboard) pick them up automatically.
//
// OTel span: graph.DeriveTestsWalkUp
// Issue: tests-edge-walk-up from helpers (follow-up to #1653).
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// WalkUpStats records the output of a DeriveTestsWalkUp run.
type WalkUpStats struct {
	// HelperTargets is the number of TESTS-edge targets that have inbound CALLS.
	HelperTargets int
	// DerivedEdges is the number of new derived TESTS edges emitted.
	DerivedEdges int
	// SkippedHighFanIn is the number of helpers skipped because their caller
	// count exceeded maxCallersPerHelper.
	SkippedHighFanIn int
	// DuplicatesSuppressed is the number of derived edges skipped because an
	// explicit TESTS edge already covered the caller.
	DuplicatesSuppressed int
}

// maxCallersPerHelper is the fan-in threshold above which a helper is
// considered a "wide utility" and walk-up is skipped.  Callers > this limit
// indicate the helper is a generic utility, not a domain-specific entry point.
const maxCallersPerHelper = 5

// DeriveTestsWalkUp walks inbound CALLS edges from each TESTS-edge helper
// target and emits derived TESTS edges to the callers.  It mutates
// doc.Relationships in-place and returns diagnostic statistics.
func DeriveTestsWalkUp(doc *Document) WalkUpStats {
	stats := WalkUpStats{}

	// ── index entities ───────────────────────────────────────────────────────
	entByID := make(map[string]*Entity, len(doc.Entities))
	for i := range doc.Entities {
		entByID[doc.Entities[i].ID] = &doc.Entities[i]
	}

	// ── inbound-CALLS index ──────────────────────────────────────────────────
	// inboundCalls maps toID → []fromID for CALLS edges where the from entity
	// is a production entity (non-test file, production kind).
	inboundCalls := make(map[string][]string)
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if !strings.EqualFold(rel.Kind, "CALLS") {
			continue
		}
		callerEnt, ok := entByID[rel.FromID]
		if !ok {
			continue
		}
		if !isProductionEntity(callerEnt) {
			continue
		}
		inboundCalls[rel.ToID] = append(inboundCalls[rel.ToID], rel.FromID)
	}

	// ── existing TESTS edges ─────────────────────────────────────────────────
	// Collect (testID, targetID) pairs already covered by explicit TESTS edges
	// so we can suppress duplicates.
	existingTests := make(map[[2]string]bool)
	// Also collect: per TESTS edge, testID → []helperID
	testToHelpers := make(map[string][]string)
	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		if !strings.EqualFold(rel.Kind, "TESTS") {
			continue
		}
		existingTests[[2]string{rel.FromID, rel.ToID}] = true
		testToHelpers[rel.FromID] = append(testToHelpers[rel.FromID], rel.ToID)
	}

	// ── derive walk-up edges ─────────────────────────────────────────────────
	var derived []Relationship

	for testID, helpers := range testToHelpers {
		for _, helperID := range helpers {
			callers, hasCalls := inboundCalls[helperID]
			if !hasCalls || len(callers) == 0 {
				continue
			}
			stats.HelperTargets++

			if len(callers) > maxCallersPerHelper {
				stats.SkippedHighFanIn++
				continue
			}

			for _, callerID := range callers {
				key := [2]string{testID, callerID}
				if existingTests[key] {
					stats.DuplicatesSuppressed++
					continue
				}
				// Mark as covered so we don't emit the same derived edge twice
				// (multiple helpers may share the same caller).
				existingTests[key] = true

				relID := derivedTestsEdgeID(testID, callerID, helperID)
				derived = append(derived, Relationship{
					ID:     relID,
					FromID: testID,
					ToID:   callerID,
					Kind:   "TESTS",
					Properties: map[string]string{
						"derived":    "helper:" + helperID,
						"confidence": "0.7",
						"source":     "tests-walkup",
					},
				})
				stats.DerivedEdges++
			}
		}
	}

	doc.Relationships = append(doc.Relationships, derived...)
	return stats
}

// derivedTestsEdgeID produces a stable 16-char edge id for a derived TESTS
// edge so repeated index runs produce the same graph.json.
func derivedTestsEdgeID(testID, callerID, helperID string) string {
	h := sha256.New()
	h.Write([]byte("derived-tests-walkup"))
	h.Write([]byte{0})
	h.Write([]byte(testID))
	h.Write([]byte{0})
	h.Write([]byte(callerID))
	h.Write([]byte{0})
	h.Write([]byte(helperID))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
