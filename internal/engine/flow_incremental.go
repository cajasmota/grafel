// Package engine — flow_incremental.go implements the blast-radius-scoped
// incremental re-run of the per-repo flow passes (process-flow + event-flow)
// for #5309 layer 3.
//
// # Why this exists
//
// The full-rebuild path runs RunProcessFlow + RunEventFlow as Pass 7 / 7.5 over
// the finalized single-repo graph. The incremental path (internal/extractors/
// incremental.go) previously skipped both passes entirely, carrying the prior
// build's Process / EventFlow entities (and their ENTRY_POINT_OF /
// STEP_IN_PROCESS / SEED_OF_EVENT_FLOW / STEP_IN_EVENT_FLOW edges) forward
// verbatim — which a file change can staleify (a renamed entity in a chain, a
// new call that lengthens a flow, a deleted publisher that removes one).
//
// # The scoping
//
// Both flow walkers are pure, deterministic, bounded functions of a small slice
// of the graph: the CALLS / FETCHES / phantom-CALLS adjacency + the HTTP
// boundary edges (IMPLEMENTS / ROUTES_TO / SERVES) + http_endpoint entities for
// process-flow, and the PUBLISHES_TO / SUBSCRIBES_TO edges + channel entities
// for event-flow. Their output is keyed by deterministic content hashes over
// the chain (computeProcessID / the event-flow id), so a re-run over an
// unchanged input slice reproduces byte-identical flows.
//
// That gives a clean blast-radius gate: if the reindex's blast radius does not
// touch any flow-input edge or any flow-relevant entity, the prior flows are
// already exactly what a full rebuild would compute, so we keep them verbatim
// and skip the walkers. If the blast radius DOES touch a flow input, we strip
// the stale flows and re-run both walkers over the finalized graph — which is
// byte-equivalent to a full rebuild for the affected scope (and, because the
// walkers are global per-repo BFS with cross-flow ranking/caps, re-running both
// in full is the only partition that preserves strict parity; they are bounded
// by entry-point count × MaxDepth, not by graph size).
//
// The predicate is a deliberate over-approximation: re-running when nothing
// actually changed a flow is merely wasted (bounded) work; SKIPPING a re-run
// that a full rebuild would have changed is a correctness bug. So the predicate
// errs toward recompute.
package engine

import "github.com/cajasmota/grafel/internal/graph"

// flowEntityKinds are the entity kinds emitted by the flow walkers. They are
// stripped before a re-run and never count as a flow-input change themselves
// (the walkers regenerate them).
var flowEntityKinds = map[string]bool{
	EntityKindProcess:   true,
	EntityKindEventFlow: true,
}

// flowEdgeKinds are the relationship kinds emitted by the flow walkers. They are
// stripped before a re-run (the walkers regenerate them) and do not themselves
// count as a flow-input change.
var flowEdgeKinds = map[string]bool{
	RelationshipKindEntryPointOf:    true,
	RelationshipKindStepInProcess:   true,
	RelationshipKindSeedOfEventFlow: true,
	RelationshipKindStepInEventFlow: true,
}

// flowInputEdgeKinds are the relationship kinds the flow walkers consume.
// A new or removed edge of one of these kinds is a flow-input change.
//   - CALLS / FETCHES drive the process-flow BFS adjacency (FETCHES is also the
//     consumer-HTTP bridge).
//   - the phantom cross-repo CALLS edge is a CALLS edge carrying cross_repo=true,
//     so it is already covered by the CALLS entry below.
//   - IMPLEMENTS / ROUTES_TO / SERVES form the HTTP-boundary set used by entry
//     ranking + cross-stack detection.
//   - PUBLISHES_TO / SUBSCRIBES_TO drive the event-flow pub/sub adjacency.
var flowInputEdgeKinds = map[string]bool{
	RelationshipKindCalls:   true,
	RelationshipKindFetches: true,
	"IMPLEMENTS":            true,
	"ROUTES_TO":             true,
	"SERVES":                true,
	"PUBLISHES_TO":          true,
	"SUBSCRIBES_TO":         true,
}

// FlowsAffectedByDelta reports whether the blast radius of an incremental
// reindex could change the per-repo flow walkers' output, so the caller can
// decide between a cheap "keep prior flows verbatim" and a re-run.
//
// It is a conservative over-approximation. Flows are considered affected when
// the blast radius contains, ignoring the walker-emitted flow entities/edges
// themselves:
//
//   - any newly extracted entity (it may be a new entry point, call step, HTTP
//     endpoint, or channel — and entry ranking depends on the entity set), or
//   - any removed entity (it may have been a step / entry / channel), or
//   - any new relationship of a flow-input kind, or
//   - any removed relationship of a flow-input kind.
//
// removedEntityIDs is the set of entity IDs sourced from changed/deleted files
// that were pruned. newEntities / newRels are the freshly extracted set.
// removedRels is the set of relationships pruned during the incremental pass
// (outbound rels of removed entities + dangling inbound edges).
func FlowsAffectedByDelta(
	newEntities []graph.Entity,
	removedEntityIDs map[string]bool,
	newRels []graph.Relationship,
	removedRels []graph.Relationship,
) bool {
	// A removed real (non-flow) entity may have been a flow node.
	if len(removedEntityIDs) > 0 {
		return true
	}
	for i := range newEntities {
		if !flowEntityKinds[newEntities[i].Kind] {
			return true
		}
	}
	for i := range newRels {
		if flowInputEdgeKinds[newRels[i].Kind] {
			return true
		}
	}
	for i := range removedRels {
		if flowInputEdgeKinds[removedRels[i].Kind] {
			return true
		}
	}
	return false
}

// stripFlows removes every walker-emitted Process / EventFlow entity and every
// walker-emitted flow edge from doc in place, so a subsequent RunProcessFlow /
// RunEventFlow re-run does not double-emit. Returns the number of entities and
// relationships removed. Edges are stripped both by kind AND by endpoint: a
// flow entity's id can appear on either side of an edge whose kind is not in
// flowEdgeKinds only via the walker, but the kind filter is exhaustive for the
// walker's output; the endpoint filter is a belt-and-suspenders sweep for any
// stray edge touching a stripped flow node.
func stripFlows(doc *graph.Document) (entsRemoved, relsRemoved int) {
	if doc == nil {
		return 0, 0
	}
	flowIDs := make(map[string]bool)
	keptEnts := doc.Entities[:0]
	for _, e := range doc.Entities {
		if flowEntityKinds[e.Kind] {
			flowIDs[e.ID] = true
			entsRemoved++
			continue
		}
		keptEnts = append(keptEnts, e)
	}
	doc.Entities = keptEnts

	keptRels := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		if flowEdgeKinds[r.Kind] || flowIDs[r.FromID] || flowIDs[r.ToID] {
			relsRemoved++
			continue
		}
		keptRels = append(keptRels, r)
	}
	doc.Relationships = keptRels
	return entsRemoved, relsRemoved
}

// RunFlowsIncremental makes the per-repo process-flow + event-flow passes
// blast-radius-scoped on the incremental reindex path (#5309 layer 3).
//
//   - When the blast radius cannot affect any flow input (FlowsAffectedByDelta
//     is false — e.g. a docs-only or comment-only change, or a change that
//     touches no call/HTTP/pub-sub structure), the prior flows carried forward
//     in doc are already byte-equivalent to a full rebuild, so they are kept
//     verbatim and the walkers are skipped.
//   - Otherwise the stale flows are stripped and both walkers are re-run over
//     the finalized graph, reproducing exactly the Process / EventFlow entities
//   - edges a full rebuild would emit for this repo.
//
// It must be called BEFORE module-aggregation but over the otherwise-finalized
// graph (merged + scoped-resolved entities/edges), mirroring the full path where
// the flow passes (Pass 7 / 7.5) run just before module-agg (Pass 8).
//
// Returns:
//   - recomputed: true when the walkers re-ran, false when prior flows were kept;
//   - flowEnts / flowRels: the Process / EventFlow entities and edges freshly
//     emitted by this re-run (empty when prior flows were kept). The caller folds
//     these into the affected-module set so module-aggregation re-derives their
//     `_external` Module node + CONTAINS edges exactly as a full rebuild's Pass 8
//     would.
func RunFlowsIncremental(
	doc *graph.Document,
	newEntities []graph.Entity,
	removedEntityIDs map[string]bool,
	newRels []graph.Relationship,
	removedRels []graph.Relationship,
) (recomputed bool, flowEnts []graph.Entity, flowRels []graph.Relationship) {
	if doc == nil {
		return false, nil, nil
	}
	if !FlowsAffectedByDelta(newEntities, removedEntityIDs, newRels, removedRels) {
		return false, nil, nil
	}
	stripFlows(doc)
	entsBefore := len(doc.Entities)
	relsBefore := len(doc.Relationships)
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	RunEventFlow(doc, DefaultEventFlowConfig())
	// Copy out the freshly emitted flow artefacts; copies are stable even if doc's
	// backing arrays are later reallocated by the module-agg append churn.
	flowEnts = append(flowEnts, doc.Entities[entsBefore:]...)
	flowRels = append(flowRels, doc.Relationships[relsBefore:]...)
	return true, flowEnts, flowRels
}
