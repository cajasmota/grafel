package golang_test

// neo4j_test.go — tests for the custom_go_neo4j extractor's GRAPH_RELATES
// graph-schema topology (#3612, epic #3606). Completes the Neo4j topology set
// alongside Java (#3663) and Python+JS (#3670).
//
// Go's Neo4j access is driver-based (github.com/neo4j/neo4j-go-driver) with no
// OGM decorators, so the graph schema lives in the Cypher query text. The
// extractor parses relationship patterns —
//   (a:Person)-[:ACTED_IN]->(m:Movie)
// — and promotes them to traversable GRAPH_RELATES edges between the
// node-label entities, the graph-DB analogue of Mongo's JOINS_COLLECTION.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/grafel/internal/custom/golang"
	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractNeo4jRaw runs the custom_go_neo4j extractor and returns the raw
// EntityRecords (with their Relationships intact, which the entitySummary
// helper strips).
func extractNeo4jRaw(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_go_neo4j")
	if !ok {
		t.Fatal("custom_go_neo4j not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "store.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// findGoGraphRelates returns the GRAPH_RELATES edge from the node entity named
// "node:<fromLabel>" to ToID "node:<toLabel>", or nil if absent.
func findGoGraphRelates(ents []types.EntityRecord, fromLabel, toLabel string) *types.RelationshipRecord {
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

// anyGraphRelates reports whether ANY GRAPH_RELATES edge exists in the set.
func anyGraphRelates(ents []types.EntityRecord) bool {
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				return true
			}
		}
	}
	return false
}

// neo4jImport is the driver import line every fixture carries so the extractor
// gate fires.
const neo4jImport = "\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n"

// ---------------------------------------------------------------------------
// Headline: MATCH (p:Person)-[:ACTED_IN]->(m:Movie) →
//           Person ─GRAPH_RELATES(ACTED_IN, OUTGOING)→ node:Movie
// ---------------------------------------------------------------------------

func TestNeo4jGoGraphRelatesEdge(t *testing.T) {
	src := "package store\n\nimport (\n" + neo4jImport + ")\n\n" +
		"func q(session neo4j.Session) {\n" +
		"\tsession.Run(`MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m`, nil)\n" +
		"}\n"

	ents := extractNeo4jRaw(t, src)

	edge := findGoGraphRelates(ents, "Person", "Movie")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES→ node:Movie edge, got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "ACTED_IN" {
		t.Errorf("rel_type: want ACTED_IN, got %q", edge.Properties.Get("rel_type"))
	}
	if edge.Properties.Get("direction") != "OUTGOING" {
		t.Errorf("direction: want OUTGOING, got %q", edge.Properties.Get("direction"))
	}
	if edge.Properties.Get("framework") != "neo4j" {
		t.Errorf("framework: want neo4j, got %q", edge.Properties.Get("framework"))
	}

	// The OUTGOING source node is the edge owner; the reverse edge must NOT
	// exist (direction is a property, not an endpoint swap).
	if findGoGraphRelates(ents, "Movie", "Person") != nil {
		t.Error("did not expect a reverse Movie ─GRAPH_RELATES→ Person edge")
	}
}

// ---------------------------------------------------------------------------
// Arrow direction: (m:Movie)<-[:DIRECTED]-(d:Director) — the head points LEFT,
// so the OUTGOING source is the RIGHT endpoint (Director), not the written
// left endpoint.
// ---------------------------------------------------------------------------

func TestNeo4jGoGraphRelatesIncomingArrow(t *testing.T) {
	src := "package store\n\nimport (\n" + neo4jImport + ")\n\n" +
		"func q(session neo4j.Session) {\n" +
		"\tsession.Run(`MATCH (m:Movie)<-[:DIRECTED]-(d:Director) RETURN m`, nil)\n" +
		"}\n"

	ents := extractNeo4jRaw(t, src)

	edge := findGoGraphRelates(ents, "Director", "Movie")
	if edge == nil {
		t.Fatalf("expected Director ─GRAPH_RELATES→ node:Movie edge (left-arrow), got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "DIRECTED" {
		t.Errorf("rel_type: want DIRECTED, got %q", edge.Properties.Get("rel_type"))
	}
	if edge.Properties.Get("direction") != "OUTGOING" {
		t.Errorf("direction: want OUTGOING, got %q", edge.Properties.Get("direction"))
	}
	// The written-left endpoint must NOT be the source.
	if findGoGraphRelates(ents, "Movie", "Director") != nil {
		t.Error("did not expect Movie ─GRAPH_RELATES→ Director for a left-pointing arrow")
	}
}

// ---------------------------------------------------------------------------
// CREATE / MERGE patterns carry the same topology as MATCH.
// ---------------------------------------------------------------------------

func TestNeo4jGoGraphRelatesCreateMerge(t *testing.T) {
	src := "package store\n\nimport (\n" + neo4jImport + ")\n\n" +
		"func q(session neo4j.Session) {\n" +
		"\tsession.Run(`CREATE (u:User)-[:FOLLOWS]->(t:Team)`, nil)\n" +
		"\tsession.Run(`MERGE (a:Account)-[:OWNS]->(w:Wallet)`, nil)\n" +
		"}\n"

	ents := extractNeo4jRaw(t, src)

	if e := findGoGraphRelates(ents, "User", "Team"); e == nil || e.Properties.Get("rel_type") != "FOLLOWS" {
		t.Errorf("expected User ─GRAPH_RELATES(FOLLOWS)→ Team from CREATE, got %+v", ents)
	}
	if e := findGoGraphRelates(ents, "Account", "Wallet"); e == nil || e.Properties.Get("rel_type") != "OWNS" {
		t.Errorf("expected Account ─GRAPH_RELATES(OWNS)→ Wallet from MERGE, got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// Undirected pattern (a:A)-[:R]-(b:B): Neo4j stores a concrete direction, so
// the written left→right order seeds the edge but direction=UNDIRECTED.
// ---------------------------------------------------------------------------

func TestNeo4jGoGraphRelatesUndirected(t *testing.T) {
	src := "package store\n\nimport (\n" + neo4jImport + ")\n\n" +
		"func q(session neo4j.Session) {\n" +
		"\tsession.Run(`MATCH (a:Person)-[:KNOWS]-(b:Person) RETURN a, b`, nil)\n" +
		"}\n"

	ents := extractNeo4jRaw(t, src)

	edge := findGoGraphRelates(ents, "Person", "Person")
	if edge == nil {
		t.Fatalf("expected Person ─GRAPH_RELATES(KNOWS)→ Person self-edge, got %+v", ents)
	}
	if edge.Properties.Get("rel_type") != "KNOWS" {
		t.Errorf("rel_type: want KNOWS, got %q", edge.Properties.Get("rel_type"))
	}
	if edge.Properties.Get("direction") != "UNDIRECTED" {
		t.Errorf("direction: want UNDIRECTED, got %q", edge.Properties.Get("direction"))
	}
}

// ---------------------------------------------------------------------------
// Negatives / honest-partial
// ---------------------------------------------------------------------------

// A single-node MATCH (no relationship) emits the node entity but NO edge.
func TestNeo4jGoSingleNodeNoEdge(t *testing.T) {
	src := "package store\n\nimport (\n" + neo4jImport + ")\n\n" +
		"func q(session neo4j.Session) {\n" +
		"\tsession.Run(`MATCH (p:Person) RETURN p`, nil)\n" +
		"}\n"

	ents := extractNeo4jRaw(t, src)
	if anyGraphRelates(ents) {
		t.Errorf("single-node MATCH must not emit GRAPH_RELATES, got %+v", ents)
	}
}

// A relationship whose type is dynamic/parameterised (-[r]-> with no static
// type) is honest-partial: no GRAPH_RELATES edge, the labels stay as node
// schema only.
func TestNeo4jGoDynamicRelTypeNoEdge(t *testing.T) {
	src := "package store\n\nimport (\n" + neo4jImport + ")\n\n" +
		"func q(session neo4j.Session) {\n" +
		"\tsession.Run(`MATCH (p:Person)-[r]->(m:Movie) RETURN type(r)`, nil)\n" +
		"}\n"

	ents := extractNeo4jRaw(t, src)
	if anyGraphRelates(ents) {
		t.Errorf("untyped relationship must not emit GRAPH_RELATES (honest-partial), got %+v", ents)
	}
	// The node labels are still surfaced as schema (partial coverage holds).
	foundPerson := false
	for i := range ents {
		if ents[i].Name == "node:Person" {
			foundPerson = true
		}
	}
	if !foundPerson {
		t.Errorf("expected node:Person schema entity to survive, got %+v", ents)
	}
}

// Gate: a file without the neo4j-go-driver import produces nothing.
func TestNeo4jGoImportGate(t *testing.T) {
	src := "package store\n\nconst q = `MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p`\n"
	ents := extractNeo4jRaw(t, src)
	if len(ents) != 0 {
		t.Errorf("neo4j extractor must not fire without the driver import, got %+v", ents)
	}
}
