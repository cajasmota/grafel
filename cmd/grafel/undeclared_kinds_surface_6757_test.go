package main

// #6757 arm C — the undeclared-relationship-kind tally must be SURFACED, not
// merely counted.
//
// The precedent is RenameDetectTruncated and UnsupportedExtensions in the same
// sidecar: "the stderr warning is invisible to every programmatic consumer:
// MCP, the dashboard and `grafel doctor` read the graph, not the indexer's
// log." A log line nobody reads is not a report, so the tally lands in
// graph-stats.json alongside them.
//
// The second test is the NON-VACUITY proof for the whole arm: it runs a REAL
// index over a real repo and asserts a known undeclared kind reaches the
// report. A counter that only ever sees test doubles proves nothing.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/algorithms"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/types"
)

func TestStatsSidecarCarriesTheUndeclaredKindReport(t *testing.T) {
	rep := fbwriter.UndeclaredKindReport{
		Edges:         9,
		DistinctKinds: 3,
		Kinds: []fbwriter.UndeclaredKind{
			{Kind: "OWNS", Edges: 6},
			{Kind: "INDEXES", Edges: 2},
			{Kind: "REGISTERED_ON", Edges: 1},
		},
	}
	doc := &graph.Document{}
	side := buildStatsSidecar(doc, 0, nil, false, nil, time.Unix(0, 0).UTC(), algorithms.RenameStats{}, nil, rep)

	if side.UndeclaredRelationshipEdges != 9 {
		t.Errorf("UndeclaredRelationshipEdges = %d, want 9", side.UndeclaredRelationshipEdges)
	}
	if side.UndeclaredRelationshipKindCount != 3 {
		t.Errorf("UndeclaredRelationshipKindCount = %d, want 3", side.UndeclaredRelationshipKindCount)
	}
	// The NAMES, not just a total — a bare count says something is wrong, the
	// names say what.
	want := map[string]int{"OWNS": 6, "INDEXES": 2, "REGISTERED_ON": 1}
	if len(side.UndeclaredRelationshipKinds) != len(want) {
		t.Fatalf("UndeclaredRelationshipKinds = %v, want %v", side.UndeclaredRelationshipKinds, want)
	}
	for k, n := range want {
		if side.UndeclaredRelationshipKinds[k] != n {
			t.Errorf("UndeclaredRelationshipKinds[%q] = %d, want %d", k, side.UndeclaredRelationshipKinds[k], n)
		}
	}

	// A clean run must carry NO key at all, so a healthy repo's sidecar is
	// byte-identical to the pre-#6757 shape (omitempty).
	clean := buildStatsSidecar(doc, 0, nil, false, nil, time.Unix(0, 0).UTC(), algorithms.RenameStats{}, nil,
		fbwriter.UndeclaredKindReport{})
	blob, err := json.Marshal(clean)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"undeclared_relationship_kinds", "undeclared_relationship_edges", "undeclared_relationship_kind_count"} {
		if strings.Contains(string(blob), "\""+key+"\"") {
			t.Errorf("a clean index emitted %q; the report must be omitted entirely when nothing is undeclared", key)
		}
	}
}

// TestRealIndexReportsUndeclaredKindsReachingTheWritePath is the non-vacuity
// proof. INDEXES is emitted by the SQL extractor (internal/extractors/sql/
// sql.go:456) and is NOT in types.AllRelationshipKinds(), so a real index over
// a repo containing one CREATE INDEX must report it — from the write path, not
// from a fixture.
func TestRealIndexReportsUndeclaredKindsReachingTheWritePath(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	if types.IsValidRelationshipKind("INDEXES") {
		t.Skip("INDEXES has since been declared; pick another undeclared kind for this probe")
	}
	useInProcessIndex(t) // no forked child: this test drives the indexer directly
	repo := fallbackManifestRepo(t, "undeclaredkinds", "main")
	writeFixtureFile(t, repo, "schema.sql",
		"CREATE TABLE users (id INT PRIMARY KEY, email TEXT);\n"+
			"CREATE INDEX users_email_idx ON users (email);\n")
	seedGitRun(t, repo, "add", "-A")
	seedGitRun(t, repo, "commit", "-q", "-m", "sql")

	if err := daemonSchedulerIndex(context.Background(), repo, ""); err != nil {
		t.Fatalf("daemonSchedulerIndex: %v", err)
	}

	stateDir := daemon.StateDirForRepo(repo)
	blob, err := os.ReadFile(filepath.Join(stateDir, "graph-stats.json"))
	if err != nil {
		t.Fatalf("read graph-stats.json: %v", err)
	}
	var side graph.GraphStatsSidecar
	if err := json.Unmarshal(blob, &side); err != nil {
		t.Fatalf("decode sidecar: %v", err)
	}
	if side.TotalRelationships == 0 {
		t.Fatal("fixture is inert: the index wrote no relationships at all, so the write path observed nothing")
	}
	if side.UndeclaredRelationshipKinds["INDEXES"] == 0 {
		t.Fatalf("a real index of a repo with a CREATE INDEX did not report the undeclared kind INDEXES "+
			"— the counter is not observing the real write path (sidecar: edges=%d kinds=%v, %d relationships written)",
			side.UndeclaredRelationshipEdges, side.UndeclaredRelationshipKinds, side.TotalRelationships)
	}
	if side.UndeclaredRelationshipEdges < side.UndeclaredRelationshipKindCount {
		t.Errorf("edges=%d < distinct kinds=%d — impossible", side.UndeclaredRelationshipEdges, side.UndeclaredRelationshipKindCount)
	}
	// And nothing was dropped: the edge is in the graph as well as in the report.
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	var found bool
	for _, r := range doc.Relationships {
		if r.Kind == "INDEXES" {
			found = true
			break
		}
	}
	if !found {
		t.Error("the INDEXES edge was reported but is absent from the written graph — arm C counts, it never drops")
	}
}
