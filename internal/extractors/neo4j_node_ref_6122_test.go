package extractors

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6122 — the neo4j node-label ref, tested across ALL FIVE Cypher-string
// extractors at once.
//
// WHY THIS FILE EXISTS, AND WHY IT LIVES HERE. The per-language `neo4j_test.go`
// files each test one package, and the refusal arm — `if toID == "" { continue }`
// in the GRAPH_RELATES pass — was covered in golang ONLY. Deleting that guard
// from rust, csharp, elixir AND php simultaneously left all four packages at
// exit 0. That is the "protected zero times" variant of the shape this cycle has
// hit repeatedly, and it is the same class as the golang mutant that survived
// until the arm was found to be unreachable from any fixture.
//
// internal/extractors is the one package that blank-imports every custom
// language package (custom_registry.go), so the five extractors are all
// registered here and one table can drive them.
//
// THE ARM IS UNREACHABLE FROM A REAL REPO — deliberately. Every label reaches
// the ref through a regex bounded to [A-Za-z_]\w*, and no path in any fixture in
// this repo contains ':'. The only way to exercise the guard is to hand the
// extractor a colon-bearing FileInput.Path, which is exactly what the hazard arm
// below does; ':' is legal in a POSIX filename.
//
// WHY DROPPING THE EDGE IS RIGHT, and why the usual "an honest dangle beats
// nothing" rule does not apply here: at two colons in the path the stub reaches
// stubScopeSegments = 6, where lookupStructural stops REJECTING and parses it as
// Format A with parts[4] as a file path and parts[5] as an entity name
// (internal/resolve/refs.go:2037). The alternative to dropping the edge is not a
// dangle — it is a ref that PARSES, and therefore mis-binds. The resolver half
// is the "six segments" subtest of
// internal/resolve.TestNeo4jNodeLocationRefBindsTheNodeEntity6122.

// neo4jCase6122 is one Cypher-string extractor and a source fixture carrying the
// same triple — (p:Person)-[:ACTED_IN]->(m:Movie) — in that language's host
// syntax, past that language's import gate.
type neo4jCase6122 struct {
	id   string // registry ID
	lang string // FileInput.Language the extractor gates on
	path string // colon-free control path
	src  string
}

func neo4jCases6122() []neo4jCase6122 {
	return []neo4jCase6122{
		{
			id: "custom_go_neo4j", lang: "go", path: "store/store.go",
			src: "package store\n\nimport (\n\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n)\n\n" +
				"func q(session neo4j.Session) {\n" +
				"\tsession.Run(`MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m`, nil)\n" +
				"}\n",
		},
		{
			id: "custom_csharp_neo4j", lang: "csharp", path: "src/Store.cs",
			src: "using Neo4j.Driver;\nnamespace App {\n  class Store {\n" +
				"    async Task Q(IAsyncSession session) {\n" +
				"      await session.RunAsync(\"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m\");\n" +
				"    }\n  }\n}\n",
		},
		{
			id: "custom_elixir_neo4j", lang: "elixir", path: "lib/store.ex",
			src: "defmodule App.Store do\n  alias Bolt.Sips\n" +
				"  def q(conn) do\n" +
				"    Bolt.Sips.query!(conn, \"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m\")\n" +
				"  end\nend\n",
		},
		{
			id: "custom_php_neo4j", lang: "php", path: "src/Store.php",
			src: "<?php\nuse Laudis\\Neo4j\\ClientBuilder;\n" +
				"$client->run(\"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m\");\n",
		},
		{
			id: "custom_rust_neo4j", lang: "rust", path: "src/store.rs",
			src: "use neo4rs::*;\n" +
				"async fn q(graph: &Graph) {\n" +
				"  graph.execute(query(\"MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m\")).await.unwrap();\n" +
				"}\n",
		},
	}
}

// extractNeo4j6122 runs one registered extractor over one fixture at one path.
func extractNeo4j6122(t *testing.T, c neo4jCase6122, path string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get(c.id)
	if !ok {
		t.Fatalf("%s not registered — internal/extractors/custom_registry.go is the "+
			"package that wires every custom language; if the ID changed, fix the table",
			c.id)
	}
	ents, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Language: c.lang,
		Content:  []byte(c.src),
	})
	if err != nil {
		t.Fatalf("%s extract: %v", c.id, err)
	}
	return ents
}

// graphRelates6122 returns every GRAPH_RELATES ToID in the record set.
func graphRelates6122(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		for j := range ents[i].Relationships {
			if r := &ents[i].Relationships[j]; r.Kind == string(types.RelationshipKindGraphRelates) {
				out = append(out, r.ToID)
			}
		}
	}
	return out
}

// countNodeEntities6122 counts SCOPE.Schema entities carrying the node-label
// Name for `label`.
func countNodeEntities6122(ents []types.EntityRecord, label string) int {
	n := 0
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Name == extractor.Neo4jNodeName(label) {
			n++
		}
	}
	return n
}

// TestNeo4jNodeRefIsLocationAddressedInEveryLanguage6122 is the CONTROL arm, and
// it is what makes the refusal arm below non-vacuous: each of the five
// extractors must emit exactly one GRAPH_RELATES edge, addressed by LOCATION at
// the file the ref actually names.
//
// Asserted on CONTENT — the exact ToID string, derived from the same helper the
// producer uses — never on a count of dangles, which reads a mis-bind as an
// improvement.
func TestNeo4jNodeRefIsLocationAddressedInEveryLanguage6122(t *testing.T) {
	for _, c := range neo4jCases6122() {
		t.Run(c.id, func(t *testing.T) {
			ents := extractNeo4j6122(t, c, c.path)
			want := extractor.Neo4jNodeTargetID(c.path, "Movie")
			if want == "" {
				t.Fatalf("Neo4jNodeTargetID refused the colon-free control path %q", c.path)
			}
			got := graphRelates6122(ents)
			if len(got) != 1 || got[0] != want {
				t.Fatalf("GRAPH_RELATES ToIDs = %v, want exactly [%q]. The ref must address "+
					"the node entity by LOCATION; `node:Movie` reaches byName through "+
					"splitStub and binds to any code symbol of that name (#6122)", got, want)
			}
			if n := countNodeEntities6122(ents, "Movie"); n != 1 {
				t.Errorf("node-label entity for `Movie` emitted %d times, want 1 — the ref "+
					"above addresses it by Name, so the two must agree", n)
			}
		})
	}
}

// TestNeo4jNodeRefRefusalDropsTheEdgeInEveryLanguage6122 is the HAZARD arm: the
// `if toID == "" { continue }` guard, driven in all five extractors.
//
// Before this test the guard existed in five places and was covered in one.
// Deleting it from the other four simultaneously left those packages green.
func TestNeo4jNodeRefRefusalDropsTheEdgeInEveryLanguage6122(t *testing.T) {
	for _, c := range neo4jCases6122() {
		t.Run(c.id, func(t *testing.T) {
			// Two colons is exactly the six-segment hazard.
			hazard := "a:b:c/" + c.path
			if extractor.Neo4jNodeTargetID(hazard, "Movie") != "" {
				t.Fatalf("Neo4jNodeTargetID accepted the colon-bearing path %q — this "+
					"test has nothing to observe unless the helper refuses", hazard)
			}
			ents := extractNeo4j6122(t, c, hazard)

			if got := graphRelates6122(ents); len(got) != 0 {
				t.Errorf("GRAPH_RELATES emitted with ToIDs %v from a file path containing "+
					"':' — the helper refused, so the caller must emit NO edge. A "+
					"six-segment stub PARSES as Format A instead of dangling, so shipping "+
					"the edge is a mis-bind, not an honest dangle", got)
			}
			// The refusal withholds the EDGE only, never the entities. Without
			// this the test would also pass if the extractor had simply failed
			// to parse the fixture at the odd path.
			if n := countNodeEntities6122(ents, "Movie"); n != 1 {
				t.Errorf("node-label entity for `Movie` emitted %d times, want 1 — the "+
					"refusal must drop the edge and nothing else", n)
			}
		})
	}
}
