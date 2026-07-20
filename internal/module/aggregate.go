// Package module — deterministic module-level graph aggregation (issue #1383).
//
// # Summary
//
// Aggregate materialises a module-level view of the entity graph. It reads
// Properties["module"] from every entity in the document and produces:
//
//  1. Module container nodes  — one synthetic Entity per distinct (repo, module)
//     pair.  Kind = "Module", Name = module path (e.g. "core/views").
//     Stable 16-char hex IDs derived from (repo, "Module", moduleName, "").
//
//  2. CONTAINS edges — Module → entity for every entity whose module matches.
//     FromID = module node ID, ToID = entity ID.
//
//  3. Aggregated DEPENDS_ON edges — for every entity-level relationship whose
//     source entity and target entity live in DIFFERENT modules, a weighted
//     edge is emitted between the two Module nodes.  The weight (stored as
//     Properties["weight"]) equals the number of distinct underlying entity
//     edges.  Self-edges (same module on both sides) are suppressed.
//
// # Storage strategy
//
// Module nodes and their edges are appended INTO the existing graph.Document
// so they are queryable via the standard loader, MCP, and API without any new
// format overhead.  All Module entities carry:
//
//	Properties["synthetic"] = "true"
//	Kind                     = "Module"
//
// Consumers that want only the entity-level graph can filter by
// entity.Kind != "Module".  The existing entity/relationship counts in
// Stats are updated to include the new nodes and edges.
//
// # Determinism
//
// Same input always produces the same output:
//   - Module IDs: graph.EntityID(repo, "Module", name, "").
//   - CONTAINS IDs: graph.RelationshipID(moduleID, entityID, "CONTAINS").
//   - DEPENDS_ON IDs: graph.RelationshipID(fromModuleID, toModuleID, "DEPENDS_ON").
//   - Entity iteration order is normalised by sorting before emitting.
//
// # Cross-repo edges
//
// Module IDs encode the repo tag so modules from different repos never
// collide.  A cross-repo entity relationship (fromEntity.Repo != toEntity.Repo)
// maps to a cross-repo Module→Module DEPENDS_ON edge, which is both valid and
// useful for multi-repo group views.
package module

import (
	"fmt"
	"sort"

	"github.com/cajasmota/grafel/internal/graph"
)

// KindModule is the entity Kind used for synthetic module container nodes.
const KindModule = "Module"

// KindContains is the relationship kind used for Module→entity edges.
const KindContains = "CONTAINS"

// KindDependsOn is the relationship kind used for Module→Module edges.
const KindDependsOn = "DEPENDS_ON"

// AggregateResult holds the counts produced by Aggregate.
type AggregateResult struct {
	// ModuleNodes is the number of synthetic Module entities created.
	ModuleNodes int
	// ContainsEdges is the number of Module→entity CONTAINS edges emitted.
	ContainsEdges int
	// DependsOnEdges is the number of Module→Module DEPENDS_ON edges emitted.
	DependsOnEdges int
}

// ModuleKey uniquely identifies a module node within the document.  Each
// entity carries a repo tag and a module label; cross-repo module nodes are
// fully distinct. Exported so the incremental path (AggregateIncremental) can
// describe the affected blast radius in the same terms.
type ModuleKey struct {
	repo string
	name string
}

// NewModuleKey builds a ModuleKey from a repo tag and module label. Used by the
// incremental path to assemble the affected-module set.
func NewModuleKey(repo, name string) ModuleKey { return ModuleKey{repo: repo, name: name} }

// moduleNodeID returns the synthetic Module node ID for a key.
func moduleNodeID(mk ModuleKey) string { return moduleNodeEntityID(mk.repo, mk.name) }

// Aggregate runs the module-level aggregation pass over doc.  It appends
// Module entities and their edges to doc.Entities / doc.Relationships and
// updates doc.Stats to reflect the additions.
//
// The pass is safe to call on any Document that has been produced by the
// standard indexer pipeline (all entities must have Properties["module"] set;
// entities without the key receive an implicit "_external" treatment and are
// placed in a module node named "_external").
//
// Aggregate is deterministic: calling it twice on the same Document (without
// re-running the indexer) is idempotent — duplicate Module nodes are skipped
// by the seenEntity gate in the caller, but for safety the function also
// checks for pre-existing Module entities and skips re-emitting them.
func Aggregate(doc *graph.Document) AggregateResult {
	if doc == nil {
		return AggregateResult{}
	}

	// ── Step 1: build entity-id → module key lookup ─────────────────────────
	// We need to know which module each entity belongs to so that we can
	// resolve both endpoints of every relationship.
	entityModule := make(map[string]ModuleKey, len(doc.Entities))
	// Use Document.Repo for same-repo entities. Cross-repo entity IDs
	// in a multi-repo group document carry a per-entity repo tag in
	// Properties["repo"]; single-repo documents use doc.Repo for all.
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == KindModule {
			continue
		}
		entityModule[e.ID] = moduleKeyForEntity(e, doc.Repo)
	}

	// emit for every module (the full-rebuild caller has no scoping filter).
	return aggregateModules(doc, entityModule, func(ModuleKey) bool { return true })
}

// aggregateModules is the shared emission core for both the full-rebuild
// Aggregate and the blast-radius-scoped AggregateIncremental. It (re-)emits
// Module nodes, CONTAINS edges, and Module→Module DEPENDS_ON edges for every
// module key satisfying include(mk), seeding its idempotency gates from the
// Module artifacts already present in doc so it never double-emits an edge that
// survived an incremental strip. entityModule maps every (non-Module) entity ID
// to its module key.
//
// DEPENDS_ON weights are computed over the FULL relationship set (the true
// cross-module fan-in/-out of an included module spans edges from any module),
// but an edge is only EMITTED when its from-module is included — so a scoped run
// re-derives exactly the pairs the strip removed, with the same weights a full
// rebuild computes.
func aggregateModules(doc *graph.Document, entityModule map[string]ModuleKey, include func(ModuleKey) bool) AggregateResult {
	// Distinct module keys.
	moduleSet := make(map[ModuleKey]struct{})
	for _, mk := range entityModule {
		moduleSet[mk] = struct{}{}
	}

	// Which Module nodes already exist (idempotency seed).
	existingModuleIDs := make(map[string]bool, len(doc.Entities))
	for k := range doc.Entities {
		if doc.Entities[k].Kind == KindModule {
			existingModuleIDs[doc.Entities[k].ID] = true
		}
	}

	// ── Emit Module container entities (sorted for stable output) ────────────
	sortedModuleKeys := make([]ModuleKey, 0, len(moduleSet))
	for mk := range moduleSet {
		sortedModuleKeys = append(sortedModuleKeys, mk)
	}
	sort.Slice(sortedModuleKeys, func(i, j int) bool {
		if sortedModuleKeys[i].repo != sortedModuleKeys[j].repo {
			return sortedModuleKeys[i].repo < sortedModuleKeys[j].repo
		}
		return sortedModuleKeys[i].name < sortedModuleKeys[j].name
	})

	newModuleCount := 0
	for _, mk := range sortedModuleKeys {
		if !include(mk) {
			continue
		}
		mid := moduleNodeID(mk)
		if existingModuleIDs[mid] {
			continue
		}
		existingModuleIDs[mid] = true
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:   mid,
			Name: mk.name,
			Kind: KindModule,
		}.WithProperties(map[string]string{
			"module":    mk.name,
			"repo":      mk.repo,
			"synthetic": "true",
		},
		))
		newModuleCount++
	}

	// ── Emit CONTAINS edges (Module → entity) ────────────────────────────────
	existingRels := make(map[string]bool, len(doc.Relationships))
	for k := range doc.Relationships {
		existingRels[doc.Relationships[k].ID] = true
	}

	sortedEntityIDs := make([]string, 0, len(entityModule))
	for eid := range entityModule {
		sortedEntityIDs = append(sortedEntityIDs, eid)
	}
	sort.Strings(sortedEntityIDs)

	containsCount := 0
	for _, eid := range sortedEntityIDs {
		mk := entityModule[eid]
		if !include(mk) {
			continue
		}
		mid := moduleNodeID(mk)
		rid := graph.RelationshipID(mid, eid, KindContains)
		if existingRels[rid] {
			continue
		}
		existingRels[rid] = true
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     rid,
			FromID: mid,
			ToID:   eid,
			Kind:   KindContains,
		})
		containsCount++
	}

	// ── Aggregate Module→Module DEPENDS_ON edges ─────────────────────────────
	type modPair struct{ from, to ModuleKey }
	edgeWeight := make(map[modPair]int)
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind == KindContains || r.Kind == KindDependsOn {
			continue
		}
		fromMK, fromOK := entityModule[r.FromID]
		toMK, toOK := entityModule[r.ToID]
		if !fromOK || !toOK {
			continue
		}
		if fromMK == toMK {
			continue
		}
		edgeWeight[modPair{from: fromMK, to: toMK}]++
	}

	sortedPairs := make([]modPair, 0, len(edgeWeight))
	for p := range edgeWeight {
		sortedPairs = append(sortedPairs, p)
	}
	sort.Slice(sortedPairs, func(i, j int) bool {
		pi, pj := sortedPairs[i], sortedPairs[j]
		if pi.from.repo != pj.from.repo {
			return pi.from.repo < pj.from.repo
		}
		if pi.from.name != pj.from.name {
			return pi.from.name < pj.from.name
		}
		if pi.to.repo != pj.to.repo {
			return pi.to.repo < pj.to.repo
		}
		return pi.to.name < pj.to.name
	})

	dependsOnCount := 0
	for _, p := range sortedPairs {
		// Emit iff the from-module is in scope. A scoped strip removed every
		// DEPENDS_ON whose from OR to is affected; re-emitting on the from side
		// for every affected module re-creates the strip's removals (the to-only
		// affected edges are re-emitted when their own from-module is iterated,
		// which it is because that from-module's outbound edge count changed only
		// if it too is affected — and a to-only change cannot alter a from-only
		// module's emitted set). Concretely: include(from) covers every pair the
		// scoped strip dropped, because the strip drops on from||to and we
		// recompute from the full edge set.
		if !include(p.from) && !include(p.to) {
			continue
		}
		fromMID := moduleNodeID(p.from)
		toMID := moduleNodeID(p.to)
		rid := graph.RelationshipID(fromMID, toMID, KindDependsOn)
		if existingRels[rid] {
			continue
		}
		existingRels[rid] = true
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     rid,
			FromID: fromMID,
			ToID:   toMID,
			Kind:   KindDependsOn,
		}.WithProperties(map[string]string{
			"weight": fmt.Sprintf("%d", edgeWeight[p]),
		},
		))
		dependsOnCount++
	}

	// ── Update document stats ────────────────────────────────────────────────
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)

	return AggregateResult{
		ModuleNodes:    newModuleCount,
		ContainsEdges:  containsCount,
		DependsOnEdges: dependsOnCount,
	}
}

// moduleNodeEntityID returns the stable 16-char hex entity ID for a Module
// container node.  The ID encodes (repo, "Module", name, "") to guarantee
// uniqueness across repos and avoid collisions with real entities (which
// always have a non-empty sourceFile).
func moduleNodeEntityID(repo, name string) string {
	return graph.EntityID(repo, KindModule, name, "")
}

// AggregateIncremental re-runs the module-aggregation pass on the blast radius
// of a change (issue #5309, layer 2), producing a graph byte-equivalent to what
// a full rebuild's Aggregate would, WITHOUT re-deriving the module layer for the
// whole graph.
//
// The full-rebuild path (cmd/grafel/index.go Pass 8) calls Aggregate on a fresh
// document that has no Module nodes / CONTAINS / DEPENDS_ON edges yet. The
// incremental path, by contrast, carries the previous build's module layer
// forward in doc; a file change can leave it stale:
//
//   - CONTAINS edges to removed entities (or from now-empty modules),
//   - Module nodes whose membership vanished,
//   - DEPENDS_ON edges whose cross-module weight changed or which no longer
//     have any underlying entity edge.
//
// affected is the set of module keys whose membership or dependency endpoints
// changed in this reindex (the union of the modules of removed and re-extracted
// entities, plus the modules on either side of any added/removed cross-module
// edge). The pass strips ONLY the module-layer artifacts touching an affected
// module, then re-derives them via the same logic Aggregate uses — so unchanged
// modules' nodes/edges are preserved untouched while the changed ones land
// exactly where a full rebuild would.
//
// When affected is empty the document's module layer is already correct and the
// function is a no-op. Passing the full module-key set degenerates to a complete
// module-layer rebuild (equivalent to stripping every Module artifact and
// calling Aggregate), which is the conservative fallback.
func AggregateIncremental(doc *graph.Document, affected map[ModuleKey]struct{}) AggregateResult {
	if doc == nil || len(affected) == 0 {
		return AggregateResult{}
	}

	// ── Step A: map every (current) entity to its module key. ────────────────
	// This is the post-merge membership the rebuilt layer must reflect.
	entityModule := make(map[string]ModuleKey, len(doc.Entities))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == KindModule {
			continue
		}
		entityModule[e.ID] = moduleKeyForEntity(e, doc.Repo)
	}

	// affectedModuleID is the set of synthetic Module node IDs that belong to an
	// affected module key — the only Module nodes we may drop/re-emit.
	affectedModuleID := make(map[string]bool, len(affected))
	for mk := range affected {
		affectedModuleID[moduleNodeID(mk)] = true
	}

	// ── Step B: strip stale module-layer artifacts touching an affected module.
	// A CONTAINS/DEPENDS_ON edge or a Module node is "stale" iff it references an
	// affected module; everything else is preserved verbatim so unchanged
	// modules stay byte-identical to the prior build.
	keptEntities := doc.Entities[:0]
	for _, e := range doc.Entities {
		if e.Kind == KindModule && affectedModuleID[e.ID] {
			continue // drop — will be re-emitted if it still has members
		}
		keptEntities = append(keptEntities, e)
	}
	doc.Entities = keptEntities

	keptRels := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		switch r.Kind {
		case KindContains:
			// FromID is the Module node; drop iff that module is affected.
			if affectedModuleID[r.FromID] {
				continue
			}
		case KindDependsOn:
			// Drop iff either endpoint module is affected (its weight may move).
			if affectedModuleID[r.FromID] || affectedModuleID[r.ToID] {
				continue
			}
		}
		keptRels = append(keptRels, r)
	}
	doc.Relationships = keptRels

	// ── Step C: re-derive the stripped artifacts. We reuse the same emission
	// logic as Aggregate but limit it to affected modules so the result is
	// byte-equivalent to a full rebuild over the affected blast radius. After
	// the strip the doc has NO Module/CONTAINS/DEPENDS_ON for affected modules,
	// so the idempotency gates inside the helpers below re-create exactly the
	// set a fresh Aggregate would for those modules.
	return aggregateModules(doc, entityModule, func(mk ModuleKey) bool {
		_, ok := affected[mk]
		return ok
	})
}

// moduleKeyForEntity computes the (repo, module) key for a single entity using
// the same rules as Aggregate's Step 1: Properties["module"] (default
// "_external") and Properties["repo"] (default docRepo).
func moduleKeyForEntity(e *graph.Entity, docRepo string) ModuleKey {
	mod := "_external"
	repo := docRepo
	if e.PropLen() > 0 {
		if v, ok := e.PropLookup("module"); ok && v != "" {
			mod = v
		}
		if v, ok := e.PropLookup("repo"); ok && v != "" {
			repo = v
		}
	}
	return ModuleKey{repo: repo, name: mod}
}
