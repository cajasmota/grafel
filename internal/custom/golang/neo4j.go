package golang

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
//   - Relationships— partial. Relationship types in Cypher patterns
//                    (-[:ACTED_IN]->, -[r:KNOWS]-) are surfaced as
//                    SCOPE.Schema entities with subtype "relationship" (there
//                    is no dedicated SCOPE.Relation kind). This is the
//                    first-class graph case — but the endpoints of the relation
//                    are not always statically resolvable from the query text,
//                    so the relation type is recorded without proven endpoint
//                    binding: partial.
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
)

func (e *neo4jExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
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
			n := makeEntity("node:"+label, "SCOPE.Schema", "", file.Path, file.Language, line)
			setProps(&n, "framework", "neo4j", "provenance", "INFERRED_FROM_NEO4J_CYPHER",
				"node_label", label)
			add(n)
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
