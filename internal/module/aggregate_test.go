package module_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// makeEntity builds a minimal graph.Entity for test fixtures.
func makeEntity(id, kind, name, sourceFile, mod, repo string) graph.Entity {
	props := map[string]string{"module": mod}
	if repo != "" {
		props["repo"] = repo
	}
	return graph.Entity{
		ID:         id,
		Kind:       kind,
		Name:       name,
		SourceFile: sourceFile,
	}.WithProperties(props)
}

// makeRel builds a minimal graph.Relationship for test fixtures.
func makeRel(fromID, toID, kind string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(fromID, toID, kind),
		FromID: fromID,
		ToID:   toID,
		Kind:   kind,
	}
}

// moduleNodeID returns the expected ID for a synthetic Module node.
func moduleNodeID(repo, name string) string {
	return graph.EntityID(repo, module.KindModule, name, "")
}

// countKind counts entities/relationships of a given kind in a document.
func countEntityKind(doc *graph.Document, kind string) int {
	n := 0
	for k := range doc.Entities {
		if doc.Entities[k].Kind == kind {
			n++
		}
	}
	return n
}

func countRelKind(doc *graph.Document, kind string) int {
	n := 0
	for k := range doc.Relationships {
		if doc.Relationships[k].Kind == kind {
			n++
		}
	}
	return n
}

// findRelWeight returns the integer weight of a DEPENDS_ON edge between two
// module names, or -1 if the edge is absent.
func findRelWeight(doc *graph.Document, repo, fromMod, toMod string) int {
	fmid := moduleNodeID(repo, fromMod)
	tmid := moduleNodeID(repo, toMod)
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind == module.KindDependsOn && r.FromID == fmid && r.ToID == tmid {
			if r.PropLen() == 0 {
				return 0
			}
			if w, err := strconv.Atoi(r.PropGet("weight")); err == nil {
				return w
			}
			return 0
		}
	}
	return -1
}

// ─── TestAggregate_NilDoc ─────────────────────────────────────────────────────

func TestAggregate_NilDoc(t *testing.T) {
	res := module.Aggregate(nil)
	if res.ModuleNodes != 0 || res.ContainsEdges != 0 || res.DependsOnEdges != 0 {
		t.Errorf("expected zero result on nil doc, got %+v", res)
	}
}

// ─── TestAggregate_EmptyDoc ───────────────────────────────────────────────────

func TestAggregate_EmptyDoc(t *testing.T) {
	doc := &graph.Document{Repo: "myrepo"}
	res := module.Aggregate(doc)
	if res.ModuleNodes != 0 {
		t.Errorf("empty doc: want 0 module nodes, got %d", res.ModuleNodes)
	}
}

// ─── TestAggregate_ModuleNodeCount ───────────────────────────────────────────
//
// Two distinct modules (core/views, core/models) → exactly 2 Module nodes.

func TestAggregate_ModuleNodeCount(t *testing.T) {
	doc := &graph.Document{
		Repo: "myrepo",
		Entities: []graph.Entity{
			makeEntity("e1", "Function", "view_a", "core/views/a.py", "core/views", ""),
			makeEntity("e2", "Function", "view_b", "core/views/b.py", "core/views", ""),
			makeEntity("e3", "Function", "model_a", "core/models/a.py", "core/models", ""),
		},
	}
	res := module.Aggregate(doc)
	if res.ModuleNodes != 2 {
		t.Errorf("want 2 module nodes, got %d", res.ModuleNodes)
	}
	if countEntityKind(doc, module.KindModule) != 2 {
		t.Errorf("want 2 Module entities in doc, got %d", countEntityKind(doc, module.KindModule))
	}
}

// ─── TestAggregate_ContainsCoverage ──────────────────────────────────────────
//
// Every non-Module entity must have exactly one CONTAINS edge pointing to it.

func TestAggregate_ContainsCoverage(t *testing.T) {
	e1 := makeEntity("e1", "Function", "view_a", "core/views/a.py", "core/views", "")
	e2 := makeEntity("e2", "Function", "view_b", "core/views/b.py", "core/views", "")
	e3 := makeEntity("e3", "Function", "model_a", "core/models/a.py", "core/models", "")

	doc := &graph.Document{
		Repo:     "myrepo",
		Entities: []graph.Entity{e1, e2, e3},
	}
	res := module.Aggregate(doc)
	if res.ContainsEdges != 3 {
		t.Errorf("want 3 CONTAINS edges, got %d", res.ContainsEdges)
	}
	if countRelKind(doc, module.KindContains) != 3 {
		t.Errorf("want 3 CONTAINS rels in doc, got %d", countRelKind(doc, module.KindContains))
	}

	// Each entity should appear exactly once as ToID in a CONTAINS edge.
	containsTarget := make(map[string]int)
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind == module.KindContains {
			containsTarget[r.ToID]++
		}
	}
	for _, e := range []graph.Entity{e1, e2, e3} {
		if containsTarget[e.ID] != 1 {
			t.Errorf("entity %q: want 1 CONTAINS edge, got %d", e.ID, containsTarget[e.ID])
		}
	}
}

// ─── TestAggregate_DependsOnWeight ───────────────────────────────────────────
//
// 14 entity-level CALLS from core/views to core/models → one DEPENDS_ON with
// weight=14.

func TestAggregate_DependsOnWeight(t *testing.T) {
	const nCalls = 14
	var ents []graph.Entity
	var rels []graph.Relationship

	// 14 distinct view entities each call one model entity.
	modelID := fmt.Sprintf("model%04d", 0)
	ents = append(ents, makeEntity(modelID, "Function", fmt.Sprintf("model_%d", 0),
		"core/models/a.py", "core/models", ""))
	for i := 0; i < nCalls; i++ {
		viewID := fmt.Sprintf("view%04d", i)
		ents = append(ents, makeEntity(viewID, "Function", fmt.Sprintf("view_%d", i),
			"core/views/a.py", "core/views", ""))
		rels = append(rels, makeRel(viewID, modelID, "CALLS"))
	}

	doc := &graph.Document{
		Repo:          "myrepo",
		Entities:      ents,
		Relationships: rels,
	}
	module.Aggregate(doc)

	w := findRelWeight(doc, "myrepo", "core/views", "core/models")
	if w != nCalls {
		t.Errorf("want DEPENDS_ON weight=%d, got %d", nCalls, w)
	}
}

// ─── TestAggregate_NoSelfEdge ─────────────────────────────────────────────────
//
// Relationships between entities in the SAME module must not produce a
// Module→Module DEPENDS_ON self-edge.

func TestAggregate_NoSelfEdge(t *testing.T) {
	doc := &graph.Document{
		Repo: "myrepo",
		Entities: []graph.Entity{
			makeEntity("e1", "Function", "view_a", "core/views/a.py", "core/views", ""),
			makeEntity("e2", "Function", "view_b", "core/views/b.py", "core/views", ""),
		},
		Relationships: []graph.Relationship{
			makeRel("e1", "e2", "CALLS"),
		},
	}
	module.Aggregate(doc)

	if countRelKind(doc, module.KindDependsOn) != 0 {
		t.Errorf("expected 0 DEPENDS_ON edges (same-module), got %d",
			countRelKind(doc, module.KindDependsOn))
	}
}

// ─── TestAggregate_CrossRepo ──────────────────────────────────────────────────
//
// Entity from repo-A calls entity from repo-B in different modules → cross-repo
// DEPENDS_ON edge is emitted.

func TestAggregate_CrossRepo(t *testing.T) {
	// e1 is from repo-a, e2 from repo-b (simulated via Properties["repo"]).
	e1 := makeEntity("ea1", "Function", "svc_call", "svc/client.py", "svc", "repo-a")
	e2 := makeEntity("eb1", "Function", "handler", "api/handler.py", "api", "repo-b")
	// Manually set doc.Repo to repo-a; e2 has Properties["repo"] = "repo-b".
	doc := &graph.Document{
		Repo:          "repo-a",
		Entities:      []graph.Entity{e1, e2},
		Relationships: []graph.Relationship{makeRel("ea1", "eb1", "CALLS")},
	}
	module.Aggregate(doc)

	found := false
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind == module.KindDependsOn {
			fromMID := moduleNodeID("repo-a", "svc")
			toMID := moduleNodeID("repo-b", "api")
			if r.FromID == fromMID && r.ToID == toMID {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected cross-repo DEPENDS_ON edge (repo-a:svc → repo-b:api) not found")
	}
}

// ─── TestAggregate_StableIDs ──────────────────────────────────────────────────
//
// Running Aggregate twice on the same document must not duplicate nodes/edges.

func TestAggregate_Idempotent(t *testing.T) {
	doc := &graph.Document{
		Repo: "myrepo",
		Entities: []graph.Entity{
			makeEntity("e1", "Function", "view_a", "core/views/a.py", "core/views", ""),
			makeEntity("e2", "Function", "model_a", "core/models/a.py", "core/models", ""),
		},
		Relationships: []graph.Relationship{makeRel("e1", "e2", "CALLS")},
	}
	r1 := module.Aggregate(doc)
	entities1 := len(doc.Entities)
	rels1 := len(doc.Relationships)

	// Second call — all nodes/edges already exist; nothing should be added.
	r2 := module.Aggregate(doc)
	if len(doc.Entities) != entities1 {
		t.Errorf("idempotency: entity count changed from %d to %d on second call", entities1, len(doc.Entities))
	}
	if len(doc.Relationships) != rels1 {
		t.Errorf("idempotency: relationship count changed from %d to %d on second call", rels1, len(doc.Relationships))
	}
	if r2.ModuleNodes != 0 || r2.ContainsEdges != 0 || r2.DependsOnEdges != 0 {
		t.Errorf("idempotency: expected zero new artifacts on second run, got %+v", r2)
	}
	_ = r1
}

// ─── TestAggregate_DeterministicEdgeIDs ───────────────────────────────────────
//
// Module node IDs and edge IDs must be the same across two independent Aggregate
// calls on independently constructed but identical documents.

func TestAggregate_DeterministicEdgeIDs(t *testing.T) {
	build := func() *graph.Document {
		return &graph.Document{
			Repo: "myrepo",
			Entities: []graph.Entity{
				makeEntity("e1", "Function", "view_a", "core/views/a.py", "core/views", ""),
				makeEntity("e2", "Function", "model_a", "core/models/a.py", "core/models", ""),
			},
			Relationships: []graph.Relationship{makeRel("e1", "e2", "CALLS")},
		}
	}

	doc1 := build()
	doc2 := build()
	module.Aggregate(doc1)
	module.Aggregate(doc2)

	// Collect IDs from both and compare.
	ids := func(doc *graph.Document) map[string]bool {
		m := make(map[string]bool)
		for _, e := range doc.Entities {
			m[e.ID] = true
		}
		for _, r := range doc.Relationships {
			m[r.ID] = true
		}
		return m
	}

	ids1, ids2 := ids(doc1), ids(doc2)
	for id := range ids1 {
		if !ids2[id] {
			t.Errorf("determinism: ID %q in run-1 but not run-2", id)
		}
	}
	for id := range ids2 {
		if !ids1[id] {
			t.Errorf("determinism: ID %q in run-2 but not run-1", id)
		}
	}
}

// ─── TestAggregate_StatsUpdated ───────────────────────────────────────────────

func TestAggregate_StatsUpdated(t *testing.T) {
	doc := &graph.Document{
		Repo: "myrepo",
		Entities: []graph.Entity{
			makeEntity("e1", "Function", "view_a", "core/views/a.py", "core/views", ""),
			makeEntity("e2", "Function", "model_a", "core/models/a.py", "core/models", ""),
		},
		Relationships: []graph.Relationship{makeRel("e1", "e2", "CALLS")},
		Stats:         graph.Stats{Entities: 2, Relationships: 1},
	}
	module.Aggregate(doc)

	if doc.Stats.Entities != len(doc.Entities) {
		t.Errorf("Stats.Entities not updated: got %d, want %d", doc.Stats.Entities, len(doc.Entities))
	}
	if doc.Stats.Relationships != len(doc.Relationships) {
		t.Errorf("Stats.Relationships not updated: got %d, want %d", doc.Stats.Relationships, len(doc.Relationships))
	}
}

// ─── TestAggregate_ThreeModulesFixture ────────────────────────────────────────
//
// Comprehensive fixture: 3 modules, multiple cross-module edges.
//   - core/views  → core/models  (3 CALLS)
//   - core/views  → core/utils   (1 CALLS)
//   - core/models → core/utils   (2 CALLS)
//   - core/views  → core/views   (5 CALLS, intra — should NOT produce DEPENDS_ON)

func TestAggregate_ThreeModulesFixture(t *testing.T) {
	entities := []graph.Entity{
		makeEntity("v1", "Function", "v1", "core/views/a.py", "core/views", ""),
		makeEntity("v2", "Function", "v2", "core/views/b.py", "core/views", ""),
		makeEntity("m1", "Function", "m1", "core/models/a.py", "core/models", ""),
		makeEntity("m2", "Function", "m2", "core/models/b.py", "core/models", ""),
		makeEntity("m3", "Function", "m3", "core/models/c.py", "core/models", ""),
		makeEntity("u1", "Function", "u1", "core/utils/a.py", "core/utils", ""),
		makeEntity("u2", "Function", "u2", "core/utils/b.py", "core/utils", ""),
	}
	rels := []graph.Relationship{
		// core/views → core/models (3)
		makeRel("v1", "m1", "CALLS"),
		makeRel("v1", "m2", "CALLS"),
		makeRel("v2", "m3", "CALLS"),
		// core/views → core/utils (1)
		makeRel("v1", "u1", "CALLS"),
		// core/models → core/utils (2)
		makeRel("m1", "u1", "CALLS"),
		makeRel("m2", "u2", "CALLS"),
		// intra-module (should NOT produce DEPENDS_ON)
		makeRel("v1", "v2", "CALLS"),
		makeRel("v1", "v2", "IMPORTS"),
		makeRel("m1", "m2", "CALLS"),
		makeRel("m1", "m3", "CALLS"),
		makeRel("m2", "m3", "CALLS"),
	}
	doc := &graph.Document{
		Repo:          "myrepo",
		Entities:      entities,
		Relationships: rels,
	}
	res := module.Aggregate(doc)

	// 3 distinct modules → 3 Module nodes.
	if res.ModuleNodes != 3 {
		t.Errorf("want 3 Module nodes, got %d", res.ModuleNodes)
	}

	// Every entity should have exactly 1 CONTAINS edge.
	if res.ContainsEdges != len(entities) {
		t.Errorf("want %d CONTAINS edges, got %d", len(entities), res.ContainsEdges)
	}

	// 3 cross-module pairs → 3 DEPENDS_ON edges.
	if res.DependsOnEdges != 3 {
		t.Errorf("want 3 DEPENDS_ON edges, got %d", res.DependsOnEdges)
	}

	// Validate weights.
	if w := findRelWeight(doc, "myrepo", "core/views", "core/models"); w != 3 {
		t.Errorf("core/views→core/models: want weight 3, got %d", w)
	}
	if w := findRelWeight(doc, "myrepo", "core/views", "core/utils"); w != 1 {
		t.Errorf("core/views→core/utils: want weight 1, got %d", w)
	}
	if w := findRelWeight(doc, "myrepo", "core/models", "core/utils"); w != 2 {
		t.Errorf("core/models→core/utils: want weight 2, got %d", w)
	}

	// No self-edges.
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		if r.Kind == module.KindDependsOn && r.FromID == r.ToID {
			t.Errorf("unexpected DEPENDS_ON self-edge: %+v", r)
		}
	}
}
