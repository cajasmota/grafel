package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Issue #6122 — the neo4j `node:` ref cluster.
//
// THE DEFECT. The Cypher-string neo4j extractors in five languages
// (csharp/elixir/golang/php/rust) mint a synthetic node-label entity whose Name
// is `node:<label>` and then emit the GRAPH_RELATES edge with
// `ToID: "node:" + dstLabel`. The target carries no QualifiedName, so
// LookupStatusHint reaches byName through splitStub
// (internal/resolve/refs.go:2658), which cuts at the FIRST colon and probes with
// the REMAINDER — the BARE LABEL. A ref intending the entity `node:Movie` can
// therefore only ever probe `byName["Movie"]`, which the intended target does
// not carry.
//
// That leaves two fates, and the second is the one that matters: dangle, or bind
// to whatever unrelated entity happens to be named `Movie`. A Cypher label is a
// database label recovered from a query string; a repo that stores Movie nodes
// very often also declares a Go struct / C# class called `Movie`. So the
// mis-bind is the LIKELY outcome, not the corner case — and a mis-bind IMPROVES
// the dangling-count metric while making the graph wrong. Every assertion below
// is on which entity the edge lands on, never on a count.
//
// THE FIXTURE IS BUILT TO DETECT THE MIS-BIND. `store.go` declares a real Go
// struct named `Movie` — the collider — alongside the Cypher that produces the
// `node:Movie` schema entity. Without the collider present this test could not
// tell "bound correctly" from "found nothing to mis-bind to", so its presence is
// asserted separately (non-vacuity) before the edge itself is checked.
func writeNeo4jFixture6122(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"store/store.go": "package store\n\n" +
			"import \"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n\n" +
			"// Movie is THE COLLIDER: a real Go struct whose name equals the Cypher\n" +
			"// node label below. `node:Movie` splits to a bare `Movie` byName probe,\n" +
			"// which this struct answers.\n" +
			"type Movie struct {\n" +
			"\tTitle string\n" +
			"}\n\n" +
			"func Cast(s neo4j.Session) {\n" +
			"\t_, _ = s.Run(`MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m`, nil)\n" +
			"}\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// TestNeo4jGraphRelatesBindsTheNodeEntityNotTheCollider6122 is the behavioural
// gate for the fix. The GRAPH_RELATES edge must join the two SCOPE.Schema
// node-label entities the Cypher pattern names — NOT the same-named Go struct.
func TestNeo4jGraphRelatesBindsTheNodeEntityNotTheCollider6122(t *testing.T) {
	fixture := writeNeo4jFixture6122(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	doc := persistAndReload(t, runIndexerOn(t, fixture, "neo4j6122", nil))

	// (a) NON-VACUITY, both halves. The collider must exist, or a mis-bind is
	// undetectable; the intended target must exist, or "does not bind to the
	// collider" would be satisfied by an edge that simply has nothing to hit.
	var collider, target *graph.Entity
	for i := range doc.Entities {
		e := &doc.Entities[i]
		switch {
		case e.Name == "Movie" && e.Kind != "SCOPE.Schema":
			collider = e
		case e.Kind == "SCOPE.Schema" && e.Name == "node:Movie":
			target = e
		}
	}
	if collider == nil {
		t.Fatalf("fixture no longer contains a non-schema entity named `Movie` — the " +
			"collider is what makes a mis-bind visible here; rebuild it, do not delete " +
			"the assertion")
	}
	if target == nil {
		t.Fatalf("no SCOPE.Schema entity named `node:Movie` — the neo4j extractor did " +
			"not run or no longer mints the node-label entity")
	}

	// (b) CONTENT, BOTH DIRECTIONS. The edge names the entities it joins by
	// kind and name, so binding to the wrong node fails exactly as loudly as
	// binding to nothing.
	got := edgeSet6105(doc, "GRAPH_RELATES")
	want := map[string]int{
		"GRAPH_RELATES: SCOPE.Schema:node:Person -> SCOPE.Schema:node:Movie": 1,
	}
	if len(got) != len(want) {
		t.Errorf("GRAPH_RELATES edge set changed size: got %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("GRAPH_RELATES %q: got %d, want %d (full set %v)", k, got[k], n, got)
		}
	}
	for k, n := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected GRAPH_RELATES edge %q (%d) — full set %v", k, n, got)
		}
	}

	// (c) The specific wrong answer, named. Redundant with (b) but it fails with
	// the diagnosis rather than a set diff.
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind == "GRAPH_RELATES" && r.ToID == collider.ID {
			t.Errorf("GRAPH_RELATES bound to the Go struct %s:%s (%s) instead of the "+
				"SCOPE.Schema node entity — this is the #6122 mis-bind",
				collider.Kind, collider.Name, collider.SourceFile)
		}
	}
}
