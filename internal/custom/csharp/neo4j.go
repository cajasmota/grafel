// neo4j.go — Cypher-DSL extractor for the C# Neo4j drivers
// (Neo4j.Driver — the official driver — and Neo4jClient — the fluent
// community client). #3616, epic #3606.
//
// C# is driver-based: like the Go driver, relationships are first-class but
// live inline in the Cypher *query text* rather than in OGM decorators. The
// official driver runs Cypher strings:
//
//	session.RunAsync("MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m")
//	await session.ExecuteWriteAsync(tx =>
//	    tx.RunAsync("CREATE (a:Person)-[:KNOWS]->(b:Person)"))
//
// Neo4jClient exposes a fluent builder whose .Match(...) / .Create(...) /
// .Merge(...) arguments are likewise Cypher pattern strings:
//
//	client.Cypher.Match("(p:Person)-[:ACTED_IN]->(m:Movie)").Return(...)
//
// Honest coverage shape (mirrors the Go extractor exactly):
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
//   - Queries — partial. RunAsync("CYPHER") / .Match("CYPHER") call sites are
//     captured with a coarse verb sniffed from the leading Cypher clause.
//     Dynamically-built query strings are not fully recoverable, so partial.
//   - Migrations — not_applicable. Neo4j is schema-flexible / graph-native;
//     the driver has no migration runner (constraints/indexes are applied via
//     ad-hoc Cypher).
//
// The extractor gates on a Neo4j.Driver / Neo4jClient using actually being
// present.
//
// Registration key: "custom_csharp_neo4j"
package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_neo4j", &neo4jExtractor{})
}

type neo4jExtractor struct{}

func (e *neo4jExtractor) Language() string { return "custom_csharp_neo4j" }

var (
	// Gate: a using of the official driver (Neo4j.Driver) or the fluent
	// community client (Neo4jClient).
	reCSNeo4jImport = regexp.MustCompile(`using\s+Neo4j(?:\.Driver|Client)\b`)

	// Cypher strings passed to the driver / fluent client. We capture the
	// contents of any C# string literal argument to a Cypher-running method:
	//
	//   RunAsync("...") / Run("...")            — official driver, sync + async
	//   .Match("...") / .Create("...") /        — Neo4jClient fluent builder
	//   .Merge("...") / .OptionalMatch("...")
	//
	// Verbatim (@"...") and interpolated ($"...") prefixes are tolerated by the
	// optional [@$]* before the quote. Interpolated strings still surface their
	// static label/type tokens; the dynamic ({var}) holes simply do not match
	// the node/rel regexes — honest-partial.
	reCSNeo4jRun = regexp.MustCompile(
		`(?:RunAsync|Run|Match|OptionalMatch|Create|Merge)\s*\(\s*[@$]*"((?:[^"\\]|\\.)*)"`,
	)

	// A node label inside a Cypher pattern: (var:Label) or (:Label). Captures
	// the first (primary) label; chained labels (:A:B) capture A.
	reCSCypherLabel = regexp.MustCompile(`\([A-Za-z_]\w*?\s*:\s*([A-Za-z_]\w*)|\(\s*:\s*([A-Za-z_]\w*)`)

	// A relationship type inside a Cypher pattern: -[:TYPE]-> / -[r:TYPE]-.
	reCSCypherRelType = regexp.MustCompile(`-\[\s*[A-Za-z_]\w*?\s*:\s*([A-Za-z_]\w*)|-\[\s*:\s*([A-Za-z_]\w*)`)

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
	reCSCypherTriple = regexp.MustCompile(
		`\(\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)[^()]*\)\s*` +
			`(<)?-\[\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)[^\]]*\]-(>)?` +
			`\s*\(\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)`,
	)
)

func (e *neo4jExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_neo4j_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "neo4j"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "csharp" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reCSNeo4jImport.MatchString(src) {
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
	// extractor and the Mongo JOINS_COLLECTION source collection.
	nodeIdx := make(map[string]int)

	for _, rm := range reCSNeo4jRun.FindAllStringSubmatchIndex(src, -1) {
		cypher := src[rm[2]:rm[3]]
		line := lineOf(src, rm[0])

		// Query operation, verb sniffed from the leading Cypher clause.
		verb := neo4jCSVerbKind(cypher)
		q := makeEntity("cypher:"+verb+":"+itoa(line), "SCOPE.Operation", "query", file.Path, "csharp", line)
		setProps(&q, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
			"query_type", verb)
		add(q)

		// Schema: node labels in the Cypher pattern.
		for _, lm := range reCSCypherLabel.FindAllStringSubmatch(cypher, -1) {
			label := lm[1]
			if label == "" {
				label = lm[2]
			}
			if label == "" {
				continue
			}
			n := makeEntity("node:"+label, "SCOPE.Schema", "", file.Path, "csharp", line)
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
		for _, relm := range reCSCypherRelType.FindAllStringSubmatch(cypher, -1) {
			relType := relm[1]
			if relType == "" {
				relType = relm[2]
			}
			if relType == "" {
				continue
			}
			r := makeEntity("rel:"+relType, "SCOPE.Schema", "relationship", file.Path, "csharp", line)
			setProps(&r, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
				"rel_type", relType)
			add(r)
		}

		// --- GRAPH_RELATES edges (#3616, epic #3606) ---
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
		// The resolver fills FromID from the owning node entity and resolves
		// ToID ("node:<label>") to the target node entity by name. Only emitted
		// when BOTH endpoints carry a static label AND the relation a static
		// type; dynamic / parameterised relations stay label/type-only —
		// honest-partial.
		for _, tm := range reCSCypherTriple.FindAllStringSubmatch(cypher, -1) {
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

// neo4jCSVerbKind sniffs a coarse verb from the leading Cypher clause so
// query_type is comparable across the C# data-access extractors.
func neo4jCSVerbKind(cypher string) string {
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
	switch upperCypherCS(cypher[i:j]) {
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

// upperCypherCS upper-cases an ASCII keyword without allocating via strings.
func upperCypherCS(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
