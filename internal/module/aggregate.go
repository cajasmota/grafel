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

	"github.com/cajasmota/archigraph/internal/graph"
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

// moduleKey uniquely identifies a module node within the document.  Each
// entity carries a repo tag and a module label; cross-repo module nodes are
// fully distinct.
type moduleKey struct {
	repo string
	name string
}

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
	entityModule := make(map[string]moduleKey, len(doc.Entities))
	// Use Document.Repo for same-repo entities. Cross-repo entity IDs
	// in a multi-repo group document carry a per-entity repo tag in
	// Properties["repo"]; single-repo documents use doc.Repo for all.
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == KindModule {
			continue
		}
		mod := "_external"
		if e.Properties != nil {
			if v, ok := e.Properties["module"]; ok && v != "" {
				mod = v
			}
		}
		repo := doc.Repo
		if e.Properties != nil {
			if v, ok := e.Properties["repo"]; ok && v != "" {
				repo = v
			}
		}
		entityModule[e.ID] = moduleKey{repo: repo, name: mod}
	}

	// ── Step 2: collect distinct module keys ────────────────────────────────
	// Use a sorted slice for deterministic iteration order later.
	moduleSet := make(map[moduleKey]struct{})
	for _, mk := range entityModule {
		moduleSet[mk] = struct{}{}
	}

	// ── Step 3: build module-key → stable module node ID ────────────────────
	moduleNodeID := make(map[moduleKey]string, len(moduleSet))
	for mk := range moduleSet {
		moduleNodeID[mk] = moduleNodeEntityID(mk.repo, mk.name)
	}

	// ── Step 4: check which Module nodes already exist (idempotency) ────────
	existingModuleIDs := make(map[string]bool, len(doc.Entities))
	for k := range doc.Entities {
		if doc.Entities[k].Kind == KindModule {
			existingModuleIDs[doc.Entities[k].ID] = true
		}
	}

	// ── Step 5: emit Module container entities ───────────────────────────────
	// Collect and sort for stable output.
	sortedModuleKeys := make([]moduleKey, 0, len(moduleSet))
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
		mid := moduleNodeID[mk]
		if existingModuleIDs[mid] {
			continue
		}
		existingModuleIDs[mid] = true
		props := map[string]string{
			"module":    mk.name,
			"repo":      mk.repo,
			"synthetic": "true",
		}
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         mid,
			Name:       mk.name,
			Kind:       KindModule,
			Properties: props,
		})
		newModuleCount++
	}

	// ── Step 6: emit CONTAINS edges (Module → entity) ────────────────────────
	// Build a dedup set seeded with existing CONTAINS edges so re-runs are
	// idempotent.
	existingRels := make(map[string]bool, len(doc.Relationships))
	for k := range doc.Relationships {
		existingRels[doc.Relationships[k].ID] = true
	}

	// Sort entity IDs for stable CONTAINS emission order.
	sortedEntityIDs := make([]string, 0, len(entityModule))
	for eid := range entityModule {
		sortedEntityIDs = append(sortedEntityIDs, eid)
	}
	sort.Strings(sortedEntityIDs)

	containsCount := 0
	for _, eid := range sortedEntityIDs {
		mk := entityModule[eid]
		mid := moduleNodeID[mk]
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

	// ── Step 7: aggregate Module→Module DEPENDS_ON edges ─────────────────────
	// Walk every entity-level relationship; if the from/to entities are in
	// different modules, accumulate the count into a from-module→to-module
	// pair.
	type modPair struct{ from, to moduleKey }
	edgeWeight := make(map[modPair]int)
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind == KindContains || r.Kind == KindDependsOn {
			// Skip meta-edges we are generating ourselves.
			continue
		}
		fromMK, fromOK := entityModule[r.FromID]
		toMK, toOK := entityModule[r.ToID]
		if !fromOK || !toOK {
			continue
		}
		if fromMK == toMK {
			// Same module — self-edge: skip.
			continue
		}
		edgeWeight[modPair{from: fromMK, to: toMK}]++
	}

	// Sort pairs for deterministic emission.
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
		fromMID := moduleNodeID[p.from]
		toMID := moduleNodeID[p.to]
		rid := graph.RelationshipID(fromMID, toMID, KindDependsOn)
		if existingRels[rid] {
			continue
		}
		existingRels[rid] = true
		w := edgeWeight[p]
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     rid,
			FromID: fromMID,
			ToID:   toMID,
			Kind:   KindDependsOn,
			Properties: map[string]string{
				"weight": fmt.Sprintf("%d", w),
			},
		})
		dependsOnCount++
	}

	// ── Step 8: update document stats ────────────────────────────────────────
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
