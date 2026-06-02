package csharp_test

// neo4j_test.go — tests for the custom_csharp_neo4j extractor's GRAPH_RELATES
// graph-schema topology (#3616, epic #3606). Completes the Neo4j topology set
// alongside Java (#3663), Python+JS (#3670) and Go (#3612).
//
// C#'s Neo4j access is driver-based (Neo4j.Driver, Neo4jClient) with no OGM
// decorators, so the graph schema lives in the Cypher query text. The
// extractor parses relationship patterns —
//   (a:Person)-[:ACTED_IN]->(m:Movie)
// — and promotes them to traversable GRAPH_RELATES edges between the
// node-label entities, the graph-DB analogue of Mongo's JOINS_COLLECTION.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractCSNeo4j runs the custom_csharp_neo4j extractor and returns the raw
// EntityRecords (with their Relationships intact).
func extractCSNeo4j(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_csharp_neo4j")
	if !ok {
		t.Fatal("custom_csharp_neo4j not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "Store.cs",
		Language: "csharp",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// findCSGraphRelates returns the GRAPH_RELATES edge from the node entity named
// "node:<fromLabel>" to ToID "node:<toLabel>", or nil if absent.
func findCSGraphRelates(ents []types.EntityRecord, fromLabel, toLabel string) *types.RelationshipRecord {
	for i := range ents {
		if !(ents[i].Kind == "SCOPE.Schema" && ents[i].Name == "node:"+fromLabel) {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == string(types.RelationshipKindGraphRelates) && r.ToID == "node:"+toLabel {
				return r
			}
		}
	}
	return nil
}

// anyCSGraphRelates reports whether ANY GRAPH_RELATES edge exists in the set.
func anyCSGraphRelates(ents []types.EntityRecord) bool {
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				return true
			}
		}
	}
	return false
}

const csNeo4jUsing = "using Neo4j.Driver;\n"

func wrapCS(body string) string {
	return csNeo4jUsing + "namespace App {\n  class Store {\n" + body + "\n  }\n}\n"
}

// ---------------------------------------------------------------------------
// Headline: MATCH (p:Person)-[:ACTED_IN]->(m:Movie) in a C# string →
//           Person ─GRAPH_RELATES(ACTED_IN, OUTGOING)→ node:Movie
// ---------------------------------------------------------------------------

func TestCSNeo4jGraphRelatesEdge(t *testing.T) {
	src := wrapCS(`    async Task Q(IAsyncSession session) {
      await session.RunAsync("MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m");
    }`)

	ents := extractCSNeo4j(t, src)

	edge := findCSGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Movie edge; got entities %+v", ents)
	}
	if got := edge.Properties["rel_type"]; got != "ACTED_IN" {
		t.Errorf("rel_type = %q, want ACTED_IN", got)
	}
	if got := edge.Properties["direction"]; got != "OUTGOING" {
		t.Errorf("direction = %q, want OUTGOING", got)
	}
	if got := edge.Properties["framework"]; got != "neo4j" {
		t.Errorf("framework = %q, want neo4j", got)
	}
}

// ---------------------------------------------------------------------------
// Direction: a left-pointing arrow flips the OUTGOING source endpoint.
//   (m:Movie)<-[:ACTED_IN]-(p:Person) → Person ─ACTED_IN→ node:Movie
// ---------------------------------------------------------------------------

func TestCSNeo4jGraphRelatesLeftArrow(t *testing.T) {
	src := wrapCS(`    async Task Q(IAsyncSession session) {
      await session.RunAsync("MATCH (m:Movie)<-[:ACTED_IN]-(p:Person) RETURN p");
    }`)

	ents := extractCSNeo4j(t, src)

	// Source is Person (the arrow points away from Person).
	edge := findCSGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Movie (arrow flipped); got %+v", ents)
	}
	if got := edge.Properties["direction"]; got != "OUTGOING" {
		t.Errorf("direction = %q, want OUTGOING", got)
	}
	// And there must be NO edge in the written left→right order.
	if rev := findCSGraphRelates(ents, "Movie", "Person"); rev != nil {
		t.Errorf("unexpected reverse edge Movie->Person: %+v", rev)
	}
}

// ---------------------------------------------------------------------------
// Undirected pattern records direction=UNDIRECTED, written order as source.
//   (a:Person)-[:KNOWS]-(b:Person)
// ---------------------------------------------------------------------------

func TestCSNeo4jGraphRelatesUndirected(t *testing.T) {
	src := wrapCS(`    async Task Q(IAsyncSession session) {
      await session.RunAsync("MATCH (a:Person)-[:KNOWS]-(b:Person) RETURN a, b");
    }`)

	ents := extractCSNeo4j(t, src)

	edge := findCSGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Person; got %+v", ents)
	}
	if got := edge.Properties["rel_type"]; got != "KNOWS" {
		t.Errorf("rel_type = %q, want KNOWS", got)
	}
	if got := edge.Properties["direction"]; got != "UNDIRECTED" {
		t.Errorf("direction = %q, want UNDIRECTED", got)
	}
}

// ---------------------------------------------------------------------------
// Neo4jClient fluent .Match("...") is also a Cypher carrier.
// ---------------------------------------------------------------------------

func TestCSNeo4jClientFluentMatch(t *testing.T) {
	src := "using Neo4jClient;\nnamespace App {\n  class Store {\n" +
		`    void Q(IGraphClient client) {
      client.Cypher.Match("(u:User)-[:FOLLOWS]->(o:Org)").Return(x => x).Results.ToList();
    }` + "\n  }\n}\n"

	ents := extractCSNeo4j(t, src)

	edge := findCSGraphRelates(ents, "User", "Org")
	if edge == nil {
		t.Fatalf("expected User -GRAPH_RELATES-> node:Org via .Match; got %+v", ents)
	}
	if got := edge.Properties["rel_type"]; got != "FOLLOWS" {
		t.Errorf("rel_type = %q, want FOLLOWS", got)
	}
}

// ---------------------------------------------------------------------------
// CREATE / MERGE write patterns also yield topology.
// ---------------------------------------------------------------------------

func TestCSNeo4jCreateWritePattern(t *testing.T) {
	src := wrapCS(`    async Task W(IAsyncTransaction tx) {
      await tx.RunAsync("CREATE (a:Author)-[:WROTE]->(b:Book)");
    }`)

	ents := extractCSNeo4j(t, src)

	edge := findCSGraphRelates(ents, "Author", "Book")
	if edge == nil {
		t.Fatalf("expected Author -GRAPH_RELATES-> node:Book; got %+v", ents)
	}
	if got := edge.Properties["rel_type"]; got != "WROTE" {
		t.Errorf("rel_type = %q, want WROTE", got)
	}
	// The query op verb should be sniffed as "create".
	var sawCreate bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Properties["query_type"] == "create" {
			sawCreate = true
		}
	}
	if !sawCreate {
		t.Errorf("expected a query op with query_type=create; got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// Negative: a single-node MATCH yields NO GRAPH_RELATES edge.
// ---------------------------------------------------------------------------

func TestCSNeo4jSingleNodeNoEdge(t *testing.T) {
	src := wrapCS(`    async Task Q(IAsyncSession session) {
      await session.RunAsync("MATCH (p:Person) RETURN p");
    }`)

	ents := extractCSNeo4j(t, src)

	if anyCSGraphRelates(ents) {
		t.Fatalf("single-node MATCH must not emit a GRAPH_RELATES edge; got %+v", ents)
	}
	// But the node label must still be surfaced as schema.
	var sawNode bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Name == "node:Person" {
			sawNode = true
		}
	}
	if !sawNode {
		t.Errorf("expected node:Person schema entity; got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// Negative: a dynamic / parameterised relationship type is honest-partial —
// no edge, but endpoints still surface as node schema.
// ---------------------------------------------------------------------------

func TestCSNeo4jDynamicRelHonestPartial(t *testing.T) {
	// Untyped relationship -[r]-> : no static type, so no topology edge.
	src := wrapCS(`    async Task Q(IAsyncSession session, string rel) {
      await session.RunAsync($"MATCH (p:Person)-[r]->(m:Movie) RETURN r");
    }`)

	ents := extractCSNeo4j(t, src)

	if anyCSGraphRelates(ents) {
		t.Fatalf("untyped relationship must not emit a GRAPH_RELATES edge; got %+v", ents)
	}
	// Both endpoint labels still surface (honest-partial).
	if findNodeSchema(ents, "Person") == nil || findNodeSchema(ents, "Movie") == nil {
		t.Errorf("expected both Person and Movie node schema entities; got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// Negative: no Neo4j using → extractor is a no-op (gate).
// ---------------------------------------------------------------------------

func TestCSNeo4jGateNoImport(t *testing.T) {
	src := "namespace App {\n  class Store {\n" +
		`    void Q() {
      var s = "MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p";
    }` + "\n  }\n}\n"

	ents := extractCSNeo4j(t, src)
	if len(ents) != 0 {
		t.Fatalf("expected no entities without a Neo4j using; got %+v", ents)
	}
}

func findNodeSchema(ents []types.EntityRecord, label string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Name == "node:"+label {
			return &ents[i]
		}
	}
	return nil
}
