// neo4j.go — Cypher-DSL extractor for the PHP Neo4j driver
// (laudis/neo4j-php-client). #3618, epic #3606.
//
// PHP is driver-based: like the Go and C# drivers, Neo4j relationships are
// first-class but live inline in the Cypher *query text* rather than in OGM
// decorators. The laudis client runs Cypher strings through ->run(...):
//
//	$client->run("MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m");
//	$session->run('CREATE (a:Person)-[:KNOWS]->(b:Person)');
//	$tsx->run("MATCH (u:User)-[:OWNS]->(o:Order) RETURN o");
//	Statement::create("MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p");
//
// Honest coverage shape (mirrors the Go / C# extractors exactly):
//
//   - Models / Schema — partial. Node labels in Cypher patterns ((n:Person),
//     (:Movie)) are surfaced as SCOPE.Schema nodes. Labels are a soft schema
//     recovered from a query string by regex, hence partial.
//   - Relationships — full (topology). Relationship types in Cypher patterns
//     (-[:ACTED_IN]->, -[r:KNOWS]-) are surfaced as SCOPE.Schema entities with
//     subtype "relationship". When BOTH endpoints of the pattern carry a
//     static node label and the relation a static type —
//     (a:Person)-[:ACTED_IN]->(m:Movie) — the graph-schema topology is
//     additionally promoted to a traversable GRAPH_RELATES edge between the
//     node-label entities (Person ─GRAPH_RELATES(ACTED_IN)→ Movie), the
//     graph-DB analogue of Mongo's JOINS_COLLECTION and the Java SDN
//     @Relationship edge. Parameterised / dynamic relations (no static type,
//     or built by string concatenation / interpolation) stay type-only —
//     honest-partial.
//   - Queries — partial. ->run("CYPHER") / Statement::create("CYPHER") call
//     sites are captured with a coarse verb sniffed from the leading Cypher
//     clause. Dynamically-built query strings are not fully recoverable, so
//     partial.
//   - Migrations — not_applicable. Neo4j is schema-flexible / graph-native;
//     the driver has no migration runner (constraints/indexes are applied via
//     ad-hoc Cypher).
//
// The extractor gates on a laudis/neo4j-php-client `use Laudis\Neo4j…`
// statement actually being present.
//
// Registration key: "custom_php_neo4j"
package php

import (
	"context"
	"regexp"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_php_neo4j", &neo4jExtractor{})
}

type neo4jExtractor struct{}

func (e *neo4jExtractor) Language() string { return "custom_php_neo4j" }

var (
	// Gate: a `use Laudis\Neo4j…` import of the laudis/neo4j-php-client driver.
	reNeo4jPHPImport = regexp.MustCompile(`use\s+Laudis\\Neo4j\b`)

	// Cypher strings passed to the driver: $client->run("…") /
	// $session->run('…') / $tsx->run("…") / Statement::create("…"). Both
	// double-quoted and single-quoted PHP string literals are captured.
	//
	// Interpolated double-quoted strings still surface their static
	// label/type tokens; the dynamic ({$var}) holes simply do not match the
	// node/rel regexes — honest-partial.
	reNeo4jPHPRun = regexp.MustCompile(
		`(?:->run|::create|::run)\s*\(\s*"((?:[^"\\]|\\.)*)"|` +
			`(?:->run|::create|::run)\s*\(\s*'((?:[^'\\]|\\.)*)'`,
	)

	// A node label inside a Cypher pattern: (var:Label) or (:Label). Captures
	// the first (primary) label; chained labels (:A:B) capture A.
	reNeo4jPHPCypherLabel = regexp.MustCompile(`\([A-Za-z_]\w*?\s*:\s*([A-Za-z_]\w*)|\(\s*:\s*([A-Za-z_]\w*)`)

	// A relationship type inside a Cypher pattern: -[:TYPE]-> / -[r:TYPE]-.
	reNeo4jPHPCypherRelType = regexp.MustCompile(`-\[\s*[A-Za-z_]\w*?\s*:\s*([A-Za-z_]\w*)|-\[\s*:\s*([A-Za-z_]\w*)`)

	// A full directed relationship triple inside a Cypher pattern:
	//
	//   (a:LeftLabel) -[ rvar :REL_TYPE ]-> (b:RightLabel)
	//   (a:LeftLabel) <-[ rvar :REL_TYPE ]-  (b:RightLabel)
	//
	// Both endpoints must carry a statically-resolvable node label and the
	// relationship a statically-resolvable type for an edge to be emitted. The
	// arrow head (-> vs <-) decides GRAPH_RELATES direction.
	//
	// Captures:
	//   1 = left node label
	//   2 = optional left-arrow head ("<")
	//   3 = relationship type
	//   4 = optional right-arrow head (">")
	//   5 = right node label
	//
	// A bare relationship with no type (-[r]-> / -[]->) or a dynamic type does
	// NOT match the type group, so no edge is emitted — honest-partial.
	reNeo4jPHPCypherTriple = regexp.MustCompile(
		`\(\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)[^()]*\)\s*` +
			`(<)?-\[\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)[^\]]*\]-(>)?` +
			`\s*\(\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)`,
	)
)

func (e *neo4jExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.php_neo4j_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "neo4j"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "php" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reNeo4jPHPImport.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// nodeIdx maps "node:<label>" to the index, in `entities`, of its
	// SCOPE.Schema node entity. The GRAPH_RELATES pass hangs the topology edge
	// off the *source* node-label entity (the graph "table"), mirroring the Go
	// / C# extractors and the Mongo JOINS_COLLECTION source collection.
	nodeIdx := make(map[string]int)

	for _, rm := range reNeo4jPHPRun.FindAllStringSubmatchIndex(src, -1) {
		// Group 1 = double-quoted literal, group 2 = single-quoted literal.
		var cypher string
		if rm[2] >= 0 {
			cypher = src[rm[2]:rm[3]]
		} else if rm[4] >= 0 {
			cypher = src[rm[4]:rm[5]]
		}
		if cypher == "" {
			continue
		}
		line := lineOf(src, rm[0])

		// Query operation, verb sniffed from the leading Cypher clause.
		verb := neo4jPHPVerbKind(cypher)
		q := makeEntity("cypher:"+verb+":"+strconv.Itoa(line), "SCOPE.Operation", "query", file.Path, "php", line)
		setProps(&q, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
			"query_type", verb)
		add(q)

		// Schema: node labels in the Cypher pattern.
		for _, lm := range reNeo4jPHPCypherLabel.FindAllStringSubmatch(cypher, -1) {
			label := lm[1]
			if label == "" {
				label = lm[2]
			}
			if label == "" {
				continue
			}
			n := makeEntity("node:"+label, "SCOPE.Schema", "", file.Path, "php", line)
			setProps(&n, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
				"node_label", label)
			before := len(entities)
			add(n)
			// add() dedupes; only register the index when actually appended.
			if len(entities) == before+1 {
				nodeIdx["node:"+label] = before
			}
		}

		// Relationships: relationship types in the Cypher pattern (first-class).
		for _, relm := range reNeo4jPHPCypherRelType.FindAllStringSubmatch(cypher, -1) {
			relType := relm[1]
			if relType == "" {
				relType = relm[2]
			}
			if relType == "" {
				continue
			}
			r := makeEntity("rel:"+relType, "SCOPE.Schema", "relationship", file.Path, "php", line)
			setProps(&r, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
				"rel_type", relType)
			add(r)
		}

		// --- GRAPH_RELATES edges (#3618, epic #3606) ---
		// Promote the graph-schema topology encoded in a Cypher relationship
		// pattern — (a:Person)-[:ACTED_IN]->(m:Movie) — into a traversable edge
		// between the node-label entities (the graph "tables"), the graph-DB
		// analogue of the Mongo JOINS_COLLECTION and the Java SDN @Relationship
		// GRAPH_RELATES. The edge hangs off the OUTGOING source node entity; the
		// arrow head decides which endpoint that is:
		//
		//   (a:A)-[:R]->(b:B)   →  A ─GRAPH_RELATES(R, OUTGOING)→ node:B
		//   (a:A)<-[:R]-(b:B)   →  B ─GRAPH_RELATES(R, OUTGOING)→ node:A
		//
		// Only emitted when BOTH endpoints carry a static label AND the relation
		// a static type; dynamic / parameterised relations stay label/type-only
		// — honest-partial.
		for _, tm := range reNeo4jPHPCypherTriple.FindAllStringSubmatch(cypher, -1) {
			leftLabel, relType, rightLabel := tm[1], tm[3], tm[5]
			pointsRight := tm[4] == ">"
			pointsLeft := tm[2] == "<"
			if relType == "" || leftLabel == "" || rightLabel == "" {
				continue
			}
			// Determine OUTGOING source / target from the arrow head. A pattern
			// with no head on either side is undirected; Neo4j stores every
			// relationship with a concrete direction, so we treat the written
			// left→right order as the source and record direction=UNDIRECTED.
			srcLabel, dstLabel, direction := leftLabel, rightLabel, "OUTGOING"
			switch {
			case pointsRight && !pointsLeft:
				srcLabel, dstLabel = leftLabel, rightLabel
			case pointsLeft && !pointsRight:
				srcLabel, dstLabel = rightLabel, leftLabel
			default:
				direction = "UNDIRECTED"
			}

			ownerIdx, ok := nodeIdx["node:"+srcLabel]
			if !ok {
				continue
			}
			entities[ownerIdx].Relationships = append(
				entities[ownerIdx].Relationships,
				types.RelationshipRecord{
					ToID: "node:" + dstLabel,
					Kind: string(types.RelationshipKindGraphRelates),
					Properties: map[string]string{
						"framework":  "neo4j",
						"rel_type":   relType,
						"direction":  direction,
						"provenance": "INFERRED_FROM_NEO4J_CYPHER",
					},
				},
			)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// neo4jPHPVerbKind sniffs a coarse verb from the leading Cypher clause so
// query_type is comparable across the PHP data-access extractors.
func neo4jPHPVerbKind(cypher string) string {
	i := 0
	for i < len(cypher) && (cypher[i] == ' ' || cypher[i] == '\t' || cypher[i] == '\n' || cypher[i] == '\r') {
		i++
	}
	j := i
	for j < len(cypher) {
		c := cypher[j]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			j++
			continue
		}
		break
	}
	switch upperCypherPHP(cypher[i:j]) {
	case "MATCH", "RETURN", "WITH", "UNWIND", "CALL", "OPTIONAL":
		return "read"
	case "CREATE", "MERGE":
		return "create"
	case "SET":
		return "update"
	case "DELETE", "REMOVE", "DETACH":
		return "delete"
	default:
		return "query"
	}
}

// upperCypherPHP upper-cases an ASCII keyword without allocating via strings.
func upperCypherPHP(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
