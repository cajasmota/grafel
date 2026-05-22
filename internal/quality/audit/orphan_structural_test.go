package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestAudit_StructuralOnlyCountsAsOrphan (Issue #1597) verifies that an entity
// whose ONLY relationship is a structural CONTAINS edge is reported as an
// orphan. Before the fix the audit treated any relationship (including the
// CONTAINS edge every file emits for its members) as connectivity, reporting
// 0 orphans on graphs that visibly render isolated nodes.
func TestAudit_StructuralOnlyCountsAsOrphan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARCHIGRAPH_ROOT", "") // force <repo>/.archigraph state layout
	stateDir := filepath.Join(dir, ".archigraph")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Repo: "t",
		Entities: []graph.Entity{
			{ID: "file", Kind: "File", Language: "go"},
			{ID: "lonely", Kind: "function", Language: "go"}, // only CONTAINS → orphan
			{ID: "caller", Kind: "function", Language: "go"}, // has a CALLS edge → not orphan
			{ID: "callee", Kind: "function", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "file", ToID: "lonely", Kind: "CONTAINS"},
			{ID: "r2", FromID: "file", ToID: "caller", Kind: "CONTAINS"},
			{ID: "r3", FromID: "file", ToID: "callee", Kind: "CONTAINS"},
			{ID: "r4", FromID: "caller", ToID: "callee", Kind: "CALLS"},
		},
	}
	if err := graph.WriteAtomic(filepath.Join(stateDir, "graph.json"), doc, false); err != nil {
		t.Fatal(err)
	}

	rep, err := AuditPath(dir, false)
	if err != nil {
		t.Fatalf("AuditPath: %v", err)
	}
	if len(rep.Repos) != 1 {
		t.Fatalf("want 1 repo report, got %d", len(rep.Repos))
	}
	rr := rep.Repos[0]
	// "file" (only outbound CONTAINS), and "lonely" (only inbound CONTAINS)
	// are both orphans. "caller" and "callee" share a CALLS edge.
	if rr.Orphans != 2 {
		t.Errorf("Orphans = %d, want 2 (file + lonely; CALLS pair excluded)", rr.Orphans)
	}
	if rr.Orphans == 0 {
		t.Error("regression: structural-only entities reported as 0 orphans (Issue #1597)")
	}
}
