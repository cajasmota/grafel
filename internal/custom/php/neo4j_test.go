package php_test

// neo4j_test.go — tests for the custom_php_neo4j extractor's GRAPH_RELATES
// graph-schema topology (#3618, epic #3606). Completes the Neo4j topology set
// alongside Java (#3663), Python+JS (#3670), Go (#3612), C# (#3616) and
// Ruby (#3614).
//
// PHP's Neo4j access is driver-based (laudis/neo4j-php-client) with no OGM
// decorators, so the graph schema lives in the Cypher query text passed to
// ->run(...). The extractor parses relationship patterns —
//   (a:Person)-[:ACTED_IN]->(m:Movie)
// — and promotes them to traversable GRAPH_RELATES edges between the
// node-label entities, the graph-DB analogue of Mongo's JOINS_COLLECTION.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/php"
	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractPHPNeo4j(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_php_neo4j")
	if !ok {
		t.Fatal("custom_php_neo4j not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "Store.php",
		Language: "php",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findPHPGraphRelates(ents []types.EntityRecord, fromLabel, toLabel string) *types.RelationshipRecord {
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

func anyPHPGraphRelates(ents []types.EntityRecord) bool {
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				return true
			}
		}
	}
	return false
}

const phpNeo4jUse = "<?php\nuse Laudis\\Neo4j\\ClientBuilder;\n"

// Headline: MATCH (p:Person)-[:ACTED_IN]->(m:Movie) in a ->run() string →
//
//	Person ─GRAPH_RELATES(ACTED_IN, OUTGOING)→ node:Movie
func TestPHPNeo4jGraphRelatesEdge(t *testing.T) {
	src := phpNeo4jUse +
		"$client->run(\"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m\");\n"

	ents := extractPHPNeo4j(t, src)

	edge := findPHPGraphRelates(ents, "Person", "Movie")
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

// Single-quoted string is also a Cypher carrier.
func TestPHPNeo4jSingleQuoted(t *testing.T) {
	src := phpNeo4jUse +
		"$session->run('MATCH (u:User)-[:OWNS]->(o:Order) RETURN o');\n"

	ents := extractPHPNeo4j(t, src)
	edge := findPHPGraphRelates(ents, "User", "Order")
	if edge == nil {
		t.Fatalf("expected User -GRAPH_RELATES-> node:Order; got %+v", ents)
	}
	if got := edge.Properties["rel_type"]; got != "OWNS" {
		t.Errorf("rel_type = %q, want OWNS", got)
	}
}

// Direction: a left-pointing arrow flips the OUTGOING source endpoint.
//
//	(m:Movie)<-[:ACTED_IN]-(p:Person) → Person ─ACTED_IN→ node:Movie
func TestPHPNeo4jLeftArrow(t *testing.T) {
	src := phpNeo4jUse +
		"$client->run(\"MATCH (m:Movie)<-[:ACTED_IN]-(p:Person) RETURN p\");\n"

	ents := extractPHPNeo4j(t, src)
	edge := findPHPGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Movie (arrow flipped); got %+v", ents)
	}
	if got := edge.Properties["direction"]; got != "OUTGOING" {
		t.Errorf("direction = %q, want OUTGOING", got)
	}
	if rev := findPHPGraphRelates(ents, "Movie", "Person"); rev != nil {
		t.Errorf("unexpected reverse edge Movie->Person: %+v", rev)
	}
}

// Undirected pattern records direction=UNDIRECTED, written order as source.
func TestPHPNeo4jUndirected(t *testing.T) {
	src := phpNeo4jUse +
		"$client->run(\"MATCH (a:Person)-[:KNOWS]-(b:Person) RETURN a, b\");\n"

	ents := extractPHPNeo4j(t, src)
	edge := findPHPGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person -GRAPH_RELATES-> node:Person; got %+v", ents)
	}
	if got := edge.Properties["direction"]; got != "UNDIRECTED" {
		t.Errorf("direction = %q, want UNDIRECTED", got)
	}
}

// Negative: a single-node MATCH yields NO GRAPH_RELATES edge (honest-partial).
func TestPHPNeo4jSingleNodeNoEdge(t *testing.T) {
	src := phpNeo4jUse +
		"$client->run(\"MATCH (p:Person) RETURN p\");\n"

	ents := extractPHPNeo4j(t, src)
	if anyPHPGraphRelates(ents) {
		t.Fatalf("expected NO GRAPH_RELATES edge for single-node MATCH; got %+v", ents)
	}
}

// Negative: without the laudis import gate, nothing is extracted.
func TestPHPNeo4jNoImportNoExtraction(t *testing.T) {
	src := "<?php\n$client->run(\"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p\");\n"
	ents := extractPHPNeo4j(t, src)
	if len(ents) != 0 {
		t.Fatalf("expected no entities without laudis import; got %+v", ents)
	}
}
