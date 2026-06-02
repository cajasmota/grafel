package elixir_test

// neo4j_test.go — tests for the custom_elixir_neo4j extractor's GRAPH_RELATES
// graph-schema topology (#3618, epic #3606). Completes the Neo4j topology set
// alongside Java (#3663), Python+JS (#3670), Go (#3612), C# (#3616) and
// Ruby (#3614).
//
// Elixir's Neo4j access is driver-based (Bolt.Sips / boltx) with no OGM
// decorators, so the graph schema lives in the Cypher query text passed to
// query!/2. The extractor parses relationship patterns —
//   (a:Person)-[:ACTED_IN]->(m:Movie)
// — and promotes them to traversable GRAPH_RELATES edges between the
// node-label entities, the graph-DB analogue of Mongo's JOINS_COLLECTION.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/elixir"
	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractExNeo4j(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_elixir_neo4j")
	if !ok {
		t.Fatal("custom_elixir_neo4j not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "store.ex",
		Language: "elixir",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findExGraphRelates(ents []types.EntityRecord, fromLabel, toLabel string) *types.RelationshipRecord {
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

func anyExGraphRelates(ents []types.EntityRecord) bool {
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				return true
			}
		}
	}
	return false
}

const exNeo4jUse = "defmodule App.Store do\n  alias Bolt.Sips\n"

// Headline: MATCH (p:Person)-[:ACTED_IN]->(m:Movie) in query!/2 →
//
//	Person ─GRAPH_RELATES(ACTED_IN, OUTGOING)→ node:Movie
func TestExNeo4jGraphRelatesEdge(t *testing.T) {
	src := exNeo4jUse +
		"  def q(conn) do\n" +
		"    Bolt.Sips.query!(conn, \"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m\")\n" +
		"  end\nend\n"

	ents := extractExNeo4j(t, src)
	edge := findExGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Movie edge; got %+v", ents)
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

// Boltx.query/2 is also a Cypher carrier.
func TestExNeo4jBoltx(t *testing.T) {
	src := "defmodule App.Store do\n  alias Boltx\n" +
		"  def q(conn) do\n" +
		"    Boltx.query(conn, \"MATCH (u:User)-[:OWNS]->(o:Order) RETURN o\")\n" +
		"  end\nend\n"

	ents := extractExNeo4j(t, src)
	edge := findExGraphRelates(ents, "User", "Order")
	if edge == nil {
		t.Fatalf("expected User -GRAPH_RELATES-> node:Order; got %+v", ents)
	}
	if got := edge.Properties["rel_type"]; got != "OWNS" {
		t.Errorf("rel_type = %q, want OWNS", got)
	}
}

// Direction: a left-pointing arrow flips the OUTGOING source endpoint.
func TestExNeo4jLeftArrow(t *testing.T) {
	src := exNeo4jUse +
		"  def q(conn) do\n" +
		"    Bolt.Sips.query!(conn, \"MATCH (m:Movie)<-[:ACTED_IN]-(p:Person) RETURN p\")\n" +
		"  end\nend\n"

	ents := extractExNeo4j(t, src)
	edge := findExGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Movie (arrow flipped); got %+v", ents)
	}
	if got := edge.Properties["direction"]; got != "OUTGOING" {
		t.Errorf("direction = %q, want OUTGOING", got)
	}
	if rev := findExGraphRelates(ents, "Movie", "Person"); rev != nil {
		t.Errorf("unexpected reverse edge Movie->Person: %+v", rev)
	}
}

// Undirected pattern records direction=UNDIRECTED.
func TestExNeo4jUndirected(t *testing.T) {
	src := exNeo4jUse +
		"  def q(conn) do\n" +
		"    Bolt.Sips.query!(conn, \"MATCH (a:Person)-[:KNOWS]-(b:Person) RETURN a, b\")\n" +
		"  end\nend\n"

	ents := extractExNeo4j(t, src)
	edge := findExGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Person; got %+v", ents)
	}
	if got := edge.Properties["direction"]; got != "UNDIRECTED" {
		t.Errorf("direction = %q, want UNDIRECTED", got)
	}
}

// Negative: a single-node MATCH yields NO GRAPH_RELATES edge.
func TestExNeo4jSingleNodeNoEdge(t *testing.T) {
	src := exNeo4jUse +
		"  def q(conn) do\n" +
		"    Bolt.Sips.query!(conn, \"MATCH (p:Person) RETURN p\")\n" +
		"  end\nend\n"

	ents := extractExNeo4j(t, src)
	if anyExGraphRelates(ents) {
		t.Fatalf("expected NO GRAPH_RELATES edge for single-node MATCH; got %+v", ents)
	}
}

// Negative: without the Bolt.Sips / Boltx gate, nothing is extracted.
func TestExNeo4jNoImportNoExtraction(t *testing.T) {
	src := "defmodule App.Store do\n" +
		"  def q(conn) do\n" +
		"    SomethingElse.query!(conn, \"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p\")\n" +
		"  end\nend\n"
	ents := extractExNeo4j(t, src)
	if len(ents) != 0 {
		t.Fatalf("expected no entities without Bolt.Sips/Boltx; got %+v", ents)
	}
}
