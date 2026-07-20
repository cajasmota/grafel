package mcp

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

func writeRichGraph(t *testing.T, path string) {
	t.Helper()
	doc := &graph.Document{
		Repo:        "demo",
		GeneratedAt: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC),
		Entities: []graph.Entity{
			{ID: "demo::a", QualifiedName: "pkg.A", Kind: "function", Name: "A"},
			{ID: "demo::b", QualifiedName: "pkg.B", Kind: "struct", Name: "B"},
			{ID: "demo::c", QualifiedName: "pkg.C", Kind: "function", Name: "C"},
		},
		Relationships: []graph.Relationship{
			// inbound to b: 2 edges
			{FromID: "demo::a", ToID: "demo::b", Kind: "CALLS"},
			{FromID: "demo::c", ToID: "demo::b", Kind: "CALLS"},
			// residual: bug_extractor disposition
			graph.Relationship{
				FromID: "demo::a",
				ToID:   "demo::missing",
				Kind:   "CALLS",
			}.WithProperties(map[string]string{"disposition": "bug_extractor"}),
			// non-residual
			graph.Relationship{
				FromID: "demo::c",
				ToID:   "demo::a",
				Kind:   "REFERENCES",
			}.WithProperties(map[string]string{"disposition": "resolved"}),
		},
	}
	if err := fbwriter.WriteAtomic(path, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestQueryServiceEndToEnd(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "graph.fb")
	writeRichGraph(t, p)

	c := NewCache(4)
	defer c.Close()
	q := NewQueryService(c)

	// ReadEntity hit + miss
	ev, err := q.ReadEntity(p, "demo::a")
	if err != nil || ev == nil || ev.Name != "A" {
		t.Fatalf("ReadEntity hit: %v / %+v", err, ev)
	}
	ev, err = q.ReadEntity(p, "demo::nope")
	if err != nil || ev != nil {
		t.Fatalf("ReadEntity miss: %v / %+v", err, ev)
	}

	// FindReferences (inbound to b)
	refs, err := q.FindReferences(p, "demo::b")
	if err != nil || len(refs) != 2 {
		t.Fatalf("FindReferences: %v / %d", err, len(refs))
	}

	// ListEntitiesByKind
	funcs, err := q.ListEntitiesByKind(p, "function")
	if err != nil || len(funcs) != 2 {
		t.Fatalf("ListEntitiesByKind: %v / %d", err, len(funcs))
	}

	// ListResiduals: exactly one bug_extractor edge
	res, err := q.ListResiduals(p, 10, 0)
	if err != nil || len(res) != 1 {
		t.Fatalf("ListResiduals: %v / %d", err, len(res))
	}

	// SubmitRepair: bind_to_entity with valid target
	_, err = q.SubmitRepair(p, RepairResolution{
		EdgeID:         res[0].ID,
		Kind:           "bind_to_entity",
		TargetEntityID: "demo::b",
	})
	if err != nil {
		t.Fatalf("SubmitRepair valid: %v", err)
	}
	// SubmitRepair: invalid target
	_, err = q.SubmitRepair(p, RepairResolution{
		EdgeID:         res[0].ID,
		Kind:           "bind_to_entity",
		TargetEntityID: "demo::ghost",
	})
	if err == nil {
		t.Fatalf("SubmitRepair invalid target should error")
	}
	// SubmitRepair: unknown edge
	_, err = q.SubmitRepair(p, RepairResolution{
		EdgeID: "er:deadbeefdeadbeef",
		Kind:   "abandon",
	})
	if err == nil {
		t.Fatalf("SubmitRepair unknown edge should error")
	}
}
