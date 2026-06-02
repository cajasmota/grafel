package engine

import (
	"fmt"
	"math"

	"github.com/cajasmota/archigraph/internal/graph"
)

// StructuralCoupling is the engine pass that annotates synthetic Module nodes
// with afferent coupling (Ca), efferent coupling (Ce), and instability
// (I = Ce / (Ca + Ce)) derived from the module-level dependency graph.
//
// This restores the previously-orphaned coupling_score enricher (issue #3634,
// epic #3625): a port existed at internal/enrichers/coupling_score_enricher.go
// but was imported by zero production code, so no `ca`/`ce`/`instability`
// property was ever set on any graph node.
//
// AXIS — STRUCTURAL, NOT TEMPORAL. This metric is computed from the
// import/dependency graph (DEPENDS_ON edges between Module nodes, produced by
// the module-aggregation pass). It is deliberately distinct from
// commit_coupling_edges.go, which derives COMMIT_COUPLED soft edges from git
// co-change history (a temporal/VCS axis). The two coexist and answer
// different questions: structural coupling measures architectural dependency
// fan-in/fan-out (useful for rewrite-boundary planning), while commit coupling
// measures which files change together over time.
//
// INPUT. The pass consumes the Module→Module DEPENDS_ON edges emitted by
// internal/module.Aggregate. It does NOT re-parse source or re-derive
// dependencies; it reuses the already-materialized aggregate edges. Therefore
// it MUST run after the module-aggregation pass.
//
// Definitions (Robert C. Martin, "Agile Software Development"):
//   - Ce (efferent coupling)  = number of distinct modules this module depends
//     ON   (outgoing DEPENDS_ON edges).
//   - Ca (afferent coupling)  = number of distinct modules that depend on THIS
//     module (incoming DEPENDS_ON edges).
//   - I  (instability)        = Ce / (Ca + Ce), in [0.0, 1.0]. I = 1.0 means
//     maximally unstable (depends on others, nothing depends on it); I = 0.0
//     means maximally stable (depended upon, depends on nothing). An isolated
//     module with no edges (Ca = Ce = 0) is assigned I = 0.0 by convention.

// CouplingStats summarises a StructuralCoupling run.
type CouplingStats struct {
	// ModulesAnnotated is the number of Module entities that received
	// ca/ce/instability properties.
	ModulesAnnotated int
	// DependsOnEdges is the number of Module→Module DEPENDS_ON edges consumed.
	DependsOnEdges int
	// Skipped is true when there were no Module entities to annotate (e.g. the
	// module-aggregation pass did not run, or the graph has no dependency
	// structure). Honest: no node is stamped when the input is absent.
	Skipped bool
}

// coupling key constants — the canonical Module dependency edge kind.
const (
	couplingModuleKind = "Module"
	couplingDependsOn  = "DEPENDS_ON"
)

// ApplyStructuralCoupling computes Ca/Ce/instability for every Module entity in
// doc from the Module→Module DEPENDS_ON edges and writes them as string
// Properties (`ca`, `ce`, `instability`, `coupling_computed`). It returns
// stats describing the run. The pass is deterministic and idempotent: a second
// call recomputes identical values.
func ApplyStructuralCoupling(doc *graph.Document) CouplingStats {
	if doc == nil {
		return CouplingStats{Skipped: true}
	}

	// Index Module entities by ID so we only count edges between real modules.
	moduleIdx := make(map[string]int)
	for i := range doc.Entities {
		if doc.Entities[i].Kind == couplingModuleKind {
			moduleIdx[doc.Entities[i].ID] = i
		}
	}
	if len(moduleIdx) == 0 {
		return CouplingStats{Skipped: true}
	}

	// Tally distinct outgoing (Ce) and incoming (Ca) DEPENDS_ON edges per
	// module. The module-aggregation pass already deduplicates parallel
	// edges between the same ordered module pair, so each edge counts once.
	ce := make(map[int]int, len(moduleIdx))
	ca := make(map[int]int, len(moduleIdx))
	consumed := 0
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind != couplingDependsOn {
			continue
		}
		fromIdx, fromOK := moduleIdx[r.FromID]
		toIdx, toOK := moduleIdx[r.ToID]
		if !fromOK || !toOK {
			// Only module-to-module dependency edges participate.
			continue
		}
		if fromIdx == toIdx {
			// Defensive: a self-dependency is not coupling.
			continue
		}
		ce[fromIdx]++
		ca[toIdx]++
		consumed++
	}

	// Stamp properties on each Module entity (including isolated modules with
	// Ca = Ce = 0, so the absence of coupling is explicit, not unknown).
	annotated := 0
	for _, idx := range moduleIdx {
		e := &doc.Entities[idx]
		ceVal := ce[idx]
		caVal := ca[idx]
		total := caVal + ceVal
		instability := 0.0
		if total > 0 {
			// Round to 2 decimals for stable, comparable output.
			instability = math.Round(float64(ceVal)/float64(total)*100) / 100
		}
		if e.Properties == nil {
			e.Properties = make(map[string]string)
		}
		e.Properties["ca"] = fmt.Sprintf("%d", caVal)
		e.Properties["ce"] = fmt.Sprintf("%d", ceVal)
		e.Properties["instability"] = fmt.Sprintf("%.2f", instability)
		e.Properties["coupling_computed"] = "true"
		annotated++
	}

	return CouplingStats{
		ModulesAnnotated: annotated,
		DependsOnEdges:   consumed,
		Skipped:          false,
	}
}
