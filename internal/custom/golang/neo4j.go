package golang

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// neo4j.go: Cypher-DSL extractor for the official Neo4j Go driver
// (github.com/neo4j/neo4j-go-driver, v4 and v5).
//
// Neo4j is the one driver in this slice where relationships are first-class:
// they are declared inline in the Cypher query text. The honest coverage shape:
//
//   - Models / Schema — partial. Node labels in Cypher patterns ((n:Person),
//                    (:Movie)) are surfaced as SCOPE.Schema nodes. Labels are a
//                    soft schema (a node may carry any labels at runtime) and
//                    are recovered from a query string by regex, hence partial.
//   - Relationships— full (topology). Relationship types in Cypher patterns
//                    (-[:ACTED_IN]->, -[r:KNOWS]-) are surfaced as
//                    SCOPE.Schema entities with subtype "relationship" (there
//                    is no dedicated SCOPE.Relation kind). When BOTH endpoints
//                    of the pattern carry a static node label and the relation
//                    a static type — (a:Person)-[:ACTED_IN]->(m:Movie) — the
//                    graph-schema topology is additionally promoted to a
//                    traversable GRAPH_RELATES edge between the node-label
//                    entities (Person ─GRAPH_RELATES(ACTED_IN)→ Movie), the
//                    graph-DB analogue of Mongo's JOINS_COLLECTION and the Java
//                    SDN @Relationship edge (#3612, epic #3606). Parameterised
//                    / dynamic relations (no static type, or built by string
//                    concatenation) stay type-only — honest-partial.
//   - Queries      — partial. `session.Run("CYPHER")` / `ExecuteQuery(...,
//                    "CYPHER")` / `tx.Run("CYPHER")` call sites are captured
//                    with a coarse verb sniffed from the leading Cypher clause.
//                    Dynamically-built query strings are not fully recoverable,
//                    so partial.
//   - Migrations   — honesty-NA. Neo4j is schema-flexible / graph-native; there
//                    is no migration runner in the driver (constraints/indexes
//                    are applied via ad-hoc Cypher). Recorded not_applicable.
//
// The extractor gates on the neo4j-go-driver import actually being present.

func init() {
	extractor.Register("custom_go_neo4j", &neo4jExtractor{})
}

type neo4jExtractor struct{}

func (e *neo4jExtractor) Language() string { return "custom_go_neo4j" }

var (
	// Import marker for the official Neo4j Go driver (v4 and v5).
	reImportNeo4j = regexp.MustCompile(`"github\.com/neo4j/neo4j-go-driver(?:/v\d+)?/neo4j`)

	// Cypher query strings passed to session.Run / tx.Run / ExecuteQuery. Both
	// backtick and double-quoted literals are captured.
	reNeo4jRun = regexp.MustCompile(
		"(?s)\\.(?:Run|ExecuteQuery)\\([^`\"]*`([^`]*)`|\\.(?:Run|ExecuteQuery)\\([^\"]*\"((?:[^\"\\\\]|\\\\.)*)\"",
	)

	// A node label inside a Cypher pattern: (var:Label) or (:Label). Captures
	// the first label; chained labels (:A:B) capture A (the primary label).
	reCypherLabel = regexp.MustCompile(`\([A-Za-z_]\w*?\s*:\s*([A-Za-z_]\w*)|\(\s*:\s*([A-Za-z_]\w*)`)

	// A relationship type inside a Cypher pattern: -[:TYPE]-> / -[r:TYPE]-.
	reCypherRelType = regexp.MustCompile(`-\[\s*[A-Za-z_]\w*?\s*:\s*([A-Za-z_]\w*)|-\[\s*:\s*([A-Za-z_]\w*)`)

	// A full directed relationship triple inside a Cypher pattern:
	//
	//   (a:LeftLabel) -[ rvar :REL_TYPE ]-> (b:RightLabel)
	//   (a:LeftLabel) <-[ rvar :REL_TYPE ]-  (b:RightLabel)
	//
	// Both endpoints must carry a statically-resolvable node label and the
	// relationship must carry a statically-resolvable type for an edge to be
	// emitted. The arrow head (-> vs <-) determines GRAPH_RELATES direction:
	// the OUTGOING source is the label the arrow points away from.
	//
	// Captures:
	//   1 = left node label
	//   2 = optional left-arrow head ("<" when the relation points left)
	//   3 = relationship type
	//   4 = optional right-arrow head (">" when the relation points right)
	//   5 = right node label
	//
	// A bare relationship with no type (-[r]-> / -[]->) or a parameterised /
	// dynamic type (-[:$relType]-> built via string concat) does NOT match the
	// type group, so no edge is emitted — honest-partial.
	reCypherTriple = regexp.MustCompile(
		`\(\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)[^()]*\)\s*` +
			`(<)?-\[\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)[^\]]*\]-(>)?` +
			`\s*\(\s*(?:[A-Za-z_]\w*)?\s*:\s*([A-Za-z_]\w*)`,
	)
)

func (e *neo4jExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("grafel/custom/golang")
	_, span := tracer.Start(ctx, "indexer.neo4j_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "neo4j"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "go" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reImportNeo4j.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// nodeIdx maps a node label to the index, in `entities`, of its
	// SCOPE.Schema node entity. Used by the GRAPH_RELATES pass to hang the
	// graph-topology edge off the *source* node-label entity (the graph
	// "table"), mirroring the Java SDN @Node owner and the Mongo
	// JOINS_COLLECTION source collection.
	nodeIdx := make(map[string]int)

	for _, m := range reNeo4jRun.FindAllStringSubmatchIndex(src, -1) {
		cypher := submatch(src, m, 2) // backtick form
		if cypher == "" {
			cypher = submatch(src, m, 4) // double-quote form
		}
		line := lineOf(src, m[0])

		// Query operation, verb sniffed from the leading Cypher clause.
		verb := neo4jVerbKind(cypher)
		q := makeEntity("cypher:"+verb+":"+itoa(line), "SCOPE.Operation", "query", file.Path, file.Language, line)
		setProps(&q, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
			"query_type", verb)
		add(q)

		// Schema: node labels in the Cypher pattern.
		for _, lm := range reCypherLabel.FindAllStringSubmatch(cypher, -1) {
			label := lm[1]
			if label == "" {
				label = lm[2]
			}
			if label == "" {
				continue
			}
			n := makeEntity(extractor.Neo4jNodeName(label), "SCOPE.Schema", "", file.Path, file.Language, line)
			setProps(&n, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
				"node_label", label)
			before := len(entities)
			add(n)
			// Record the node entity's index the first time it is emitted.
			// add() dedupes, so only register when it was actually appended.
			if len(entities) == before+1 {
				nodeIdx[extractor.Neo4jNodeName(label)] = before
			}
		}

		// Relationships: relationship types in the Cypher pattern (first-class).
		for _, rm := range reCypherRelType.FindAllStringSubmatch(cypher, -1) {
			relType := rm[1]
			if relType == "" {
				relType = rm[2]
			}
			if relType == "" {
				continue
			}
			r := makeEntity("rel:"+relType, "SCOPE.Schema", "relationship", file.Path, file.Language, line)
			setProps(&r, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
				"rel_type", relType)
			add(r)
		}

		// --- GRAPH_RELATES edges (#3612, epic #3606) ---
		// Promote the graph-schema topology encoded in a Cypher relationship
		// pattern — (a:Person)-[:ACTED_IN]->(m:Movie) — into a traversable
		// edge between the node-label entities (the graph "tables"), the
		// graph-DB analogue of the Mongo JOINS_COLLECTION and the Java SDN
		// @Relationship GRAPH_RELATES. The edge hangs off the OUTGOING source
		// node entity; the arrow head decides which endpoint that is:
		//
		//   (a:A)-[:R]->(b:B)   →  A ─GRAPH_RELATES(R, OUTGOING)→ node:B
		//   (a:A)<-[:R]-(b:B)   →  B ─GRAPH_RELATES(R, OUTGOING)→ node:A
		//
		// The resolver fills FromID from the owning node entity and resolves
		// ToID (`scope:schema:<file>#node:<label>`, #6122) to the target node
		// entity by LOCATION — addressing it by its colon-bearing Name could
		// not work, see internal/extractor/neo4j_node.go. Only emitted
		// when BOTH endpoints carry a static label AND the relation a static
		// type; dynamic / parameterised relations don't match and stay
		// label/type-only — honest-partial.
		for _, tm := range reCypherTriple.FindAllStringSubmatch(cypher, -1) {
			leftLabel, relType, rightLabel := tm[1], tm[3], tm[5]
			pointsRight := tm[4] == ">"
			pointsLeft := tm[2] == "<"
			if relType == "" || leftLabel == "" || rightLabel == "" {
				continue
			}
			// Determine OUTGOING source / target from the arrow head. A
			// pattern with no head on either side is undirected; Neo4j stores
			// every relationship with a concrete direction, so we treat the
			// written left→right order as the source for the edge and record
			// direction=UNDIRECTED.
			srcLabel, dstLabel, direction := leftLabel, rightLabel, "OUTGOING"
			switch {
			case pointsRight && !pointsLeft:
				srcLabel, dstLabel = leftLabel, rightLabel
			case pointsLeft && !pointsRight:
				srcLabel, dstLabel = rightLabel, leftLabel
			default:
				direction = "UNDIRECTED"
			}

			// #6122 — address the target node entity by LOCATION, not by its
			// colon-bearing Name. `"node:"+dstLabel` reached byName through
			// splitStub, which cuts at the first colon, so it probed the BARE
			// label and bound to any code symbol of that name. "" means the
			// helper refused a colon-bearing path/label: emit NO edge, because
			// the alternative is not an honest dangle but a ref that PARSES as
			// Format A. See internal/extractor/neo4j_node.go.
			toID := extractor.Neo4jNodeTargetID(file.Path, dstLabel)
			if toID == "" {
				continue
			}
			ownerIdx, ok := nodeIdx[extractor.Neo4jNodeName(srcLabel)]
			if !ok {
				continue
			}
			entities[ownerIdx].Relationships = append(
				entities[ownerIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: string(types.RelationshipKindGraphRelates),
					Properties: types.Props{
						{K: "direction", V: direction},
						{K: "framework", V: "neo4j"},
						{K: "provenance", V: "INFERRED_FROM_NEO4J_CYPHER"},
						{K: "rel_type", V: relType},
					},
				},
			)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// neo4jVerbKind sniffs a coarse verb from the leading Cypher clause so
// query_type is comparable across the data-access extractors.
func neo4jVerbKind(cypher string) string {
	// Find the first non-space keyword.
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
	switch upperCypher(cypher[i:j]) {
	case "MATCH", "RETURN", "WITH", "UNWIND", "CALL":
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

// upperCypher upper-cases an ASCII keyword without allocating via strings.
func upperCypher(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
