package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReachabilityBasicBFS exercises the BFS over CALLS + CONTAINS
// edges. The seeded http_endpoint_definition lights up its CALLS
// target; the orphan function stays unreachable.
func TestReachabilityBasicBFS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/handler.go", `package h

func main() {}

func Helper() {}

func orphan() {}
`)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: []entityNode{
			// Seeded by framework_entry_kinds (http_endpoint_definition).
			{ID: "ep1", Name: "GET /x", Kind: "http_endpoint_definition", SourceFile: "src/handler.go"},
			// Reached via CALLS from ep1.
			{ID: "handlerFn", Name: "HandleX", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
			// Reached via CONTAINS from handlerFn.
			{ID: "helperFn", Name: "Helper", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
			// CLI main — seeded via the Go sniffer (sniff:cli_main:main).
			{ID: "mainFn", Name: "main", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
			// Orphan — not reached.
			{ID: "orphanFn", Name: "orphan", Kind: "SCOPE.Function", SourceFile: "src/handler.go"},
		},
		Edges: []edgeRef{
			{FromID: "ep1", ToID: "handlerFn", Kind: "CALLS"},
			{FromID: "handlerFn", ToID: "helperFn", Kind: "CONTAINS"},
		},
	}}

	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}
	if res.LinksAdded < 4 {
		t.Errorf("expected at least 4 reachable (ep1, handlerFn, helperFn, mainFn); got %d", res.LinksAdded)
	}
	if res.Candidates < 1 {
		t.Errorf("expected at least 1 unreachable (orphan); got %d", res.Candidates)
	}

	// orphanFn must be flagged unreachable.
	got := map[string]string{}
	for _, e := range graphs[0].Entities {
		got[e.ID] = e.Properties["reachable"]
	}
	if got["orphanFn"] != "false" {
		t.Errorf("orphanFn should be reachable=false, got %q", got["orphanFn"])
	}
	if got["mainFn"] != "true" {
		t.Errorf("mainFn (CLI entry) should be reachable=true, got %q", got["mainFn"])
	}
	if got["handlerFn"] != "true" {
		t.Errorf("handlerFn (CALLS from ep1) should be reachable=true, got %q", got["handlerFn"])
	}
	if got["helperFn"] != "true" {
		t.Errorf("helperFn (CONTAINS from handlerFn) should be reachable=true, got %q", got["helperFn"])
	}

	// Sidecar exists with expected schema.
	sidecar := strings.TrimSuffix(paths.Links, ".json") + "-reachability.json"
	buf, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("sidecar: %v", err)
	}
	var doc reachabilityDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	if doc.Group != "g" {
		t.Errorf("group: want g, got %q", doc.Group)
	}
	if doc.TotalEntities != 5 {
		t.Errorf("total_entities: want 5, got %d", doc.TotalEntities)
	}
	if doc.Reachable < 4 {
		t.Errorf("reachable: want >=4, got %d", doc.Reachable)
	}
	foundOrphan := false
	for _, e := range doc.Entries {
		if e.Name == "orphan" && !e.Reachable {
			foundOrphan = true
		}
	}
	if !foundOrphan {
		t.Errorf("orphan entity not in sidecar entries as unreachable")
	}
}

// TestReachabilityNoSniffableFiles passes when only graph-encoded
// entry-points are present (no on-disk language source).
func TestReachabilityNoSniffableFiles(t *testing.T) {
	graphs := []repoGraph{{
		Repo:     "repo-b",
		FileRoot: t.TempDir(), // empty
		Entities: []entityNode{
			{ID: "ep", Name: "GET /y", Kind: "http_endpoint_definition", SourceFile: "src/x.unknown"},
			{ID: "h", Name: "h", Kind: "SCOPE.Function", SourceFile: "src/x.unknown"},
			{ID: "z", Name: "z", Kind: "SCOPE.Function", SourceFile: "src/x.unknown"},
		},
		Edges: []edgeRef{
			{FromID: "ep", ToID: "h", Kind: "CALLS"},
		},
	}}
	paths := Paths{Links: filepath.Join(t.TempDir(), "g-links.json")}
	res, err := runReachabilityPass("g", graphs, paths)
	if err != nil {
		t.Fatalf("runReachabilityPass: %v", err)
	}
	if res.Candidates < 1 {
		t.Errorf("expected at least 1 unreachable (z)")
	}
}
