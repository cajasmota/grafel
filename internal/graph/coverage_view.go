package graph

// coverage_view.go — mmap-backed coverage totals (#5954).
//
// ComputeCoverage takes a fully materialised *Document: ~608 MB of heap for a
// 325 MB graph.fb, all of it retained for the duration of the call. The daemon's
// post-rebuild analytics batch reads exactly TWO numbers out of the resulting
// CoverageReport — TotalProduction and CoveredProduction — and throws the rest
// (the uncovered list, the per-file and per-directory rollups, the percentages)
// away. Paying a full materialisation for two integers is the single largest
// avoidable allocation on that path.
//
// CoverageTotalsFromView computes those two numbers straight off the mmap. It
// runs the SAME four crediting phases in the SAME order as ComputeCoverage and
// shares phases 3 and 4 verbatim (attributeByNameAffinity /
// creditEndpointsViaHandlers are called unchanged), so the numbers cannot
// drift — see TestCoverageTotalsFromView_MatchesComputeCoverage.
//
// What it does NOT retain, and why that is safe:
//
//   - Entities that are neither production nor test are classified and dropped.
//     Every downstream consumer of entByID (phases 3 and 4) only ever reads
//     entries whose id is in prodIDs or testIDs: phase 3 iterates those two sets
//     directly, and phase 4 can only credit an edge when BOTH endpoints are in
//     prodIDs (covered's key set is a subset of prodIDs by construction, so a
//     non-production handler can never have covered[handlerID] > 0).
//   - For the entities it does retain it keeps only the four fields those two
//     phases read (ID, Name, Kind, SourceFile) rather than the whole row with
//     its properties, tags, metadata and algorithm overlays.
//   - Relationships are never materialised in bulk. Phase 1 streams them, and
//     phase 4 retains only the handler↔endpoint edges (IMPLEMENTS / ROUTES_TO /
//     SERVES), which are a rounding error next to the full edge vector.
//
// Phase 5 (the contract-covered band) is deliberately absent: it mutates
// contractRef only and never touches `covered`, so it cannot change either
// number returned here.

import (
	"strings"

	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// CoverageTotalsFromView returns the TotalProduction and CoveredProduction
// counts ComputeCoverage would report for the same graph, without
// materialising a Document.
func CoverageTotalsFromView(v fbreader.GraphView) (totalProduction, coveredProduction int) {
	if v == nil {
		return 0, 0
	}

	// ── pass 1: classify entities ────────────────────────────────────────────
	// Only production and test entities are retained, and only the fields the
	// later phases read. Everything else is decoded, inspected and dropped.
	entByID := make(map[string]*Entity)
	prodIDs := make(map[string]bool)
	testIDs := make(map[string]bool)
	contractTestIDs := make(map[string]bool)

	// A FRESH interner per row, deliberately. The Document path shares one
	// across the whole load because it retains every string anyway; here the
	// decoded row is dropped immediately, and a shared interner would pin every
	// unique string in the graph — reintroducing the retention this function
	// exists to avoid.
	v.IterateEntities(func(fbEnt *fb.Entity) bool {
		if fbEnt == nil {
			return true
		}
		e := fbEntityToGraphEntity(fbEnt, newStringInterner())
		switch {
		case isProductionEntity(&e):
			prodIDs[e.ID] = true
		case isTestEntity(&e):
			testIDs[e.ID] = true
			if isContractSpecFile(e.SourceFile) {
				contractTestIDs[e.ID] = true
			}
		default:
			return true // neither: nothing downstream can read it
		}
		entByID[e.ID] = &Entity{
			ID:         e.ID,
			Name:       e.Name,
			Kind:       e.Kind,
			SourceFile: e.SourceFile,
		}
		return true
	})
	totalProduction = len(prodIDs)

	// ── phase 1 + 2: TESTS edges, then synthetic TESTS from test CALLS ───────
	covered := make(map[string]int, len(prodIDs))
	testCallsTo := make(map[string][]string)
	// Handler↔endpoint edges are the only edges phase 4 looks at; collect them
	// on this single pass so phase 4 does not need a second scan.
	var handlerEdges []Relationship

	v.IterateRelationships(func(fbRel *fb.Relationship) bool {
		if fbRel == nil {
			return true
		}
		from := string(fbRel.FromId())
		to := string(fbRel.ToId())
		kind := strings.ToUpper(string(fbRel.Kind()))

		if handlerEndpointEdgeKinds[kind] {
			handlerEdges = append(handlerEdges, Relationship{FromID: from, ToID: to, Kind: kind})
		}

		switch kind {
		case kindTests:
			if !prodIDs[to] {
				return true
			}
			if contractTestIDs[from] {
				// Offline contract spec: a shape assertion, never a reach
				// signal. ComputeCoverage records it in contractRef, which
				// does not feed either total returned here.
				return true
			}
			covered[to]++
		case kindCalls:
			if testIDs[from] {
				testCallsTo[from] = append(testCallsTo[from], to)
			}
		}
		return true
	})

	for _, targets := range testCallsTo {
		for _, toID := range targets {
			if prodIDs[toID] && covered[toID] == 0 {
				covered[toID]++
			}
		}
	}

	// ── phases 3 and 4: shared verbatim with ComputeCoverage ─────────────────
	attributeByNameAffinity(entByID, testIDs, prodIDs, covered)
	creditEndpointsViaHandlers(entByID, handlerEdges, prodIDs, covered)

	return totalProduction, len(covered)
}
