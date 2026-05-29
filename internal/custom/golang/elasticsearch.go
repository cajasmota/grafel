package golang

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// elasticsearch.go: index-mapping + query-DSL extractor for the two common Go
// Elasticsearch clients: the official go-elasticsearch
// (github.com/elastic/go-elasticsearch) and olivere/elastic.
//
// Elasticsearch indexes are schema-ful via mappings, but mappings are usually
// applied as JSON DSL rather than declared in Go types, so the honest coverage
// shape is:
//
//   - Models / Schema — partial. Index names are recovered from
//                    `Index("name")` / `Index: "name"` / `Indices("name")`
//                    call sites and surfaced as SCOPE.Schema indices, and Go
//                    structs carrying `json:"field"` tags adjacent to the
//                    client are treated as document shapes (a heuristic — a
//                    json tag does not prove the struct is an ES document).
//                    Hence partial.
//   - Queries      — partial. Request-builder / DSL call sites (Search/Index/
//                    Get/Update/Delete/Bulk/Count/Msearch/...) are captured
//                    with the operation verb. Binding a query to a concrete
//                    index from a regex is not reliable, so this stays partial.
//   - Migrations   — partial. `CreateIndex("name")` /
//                    `indices.Create(...)` call sites are index-creation
//                    operations: the closest analogue to a migration ES has.
//                    There is no ordered/versioned migration runner, so this is
//                    partial, not full.
//   - Relationships— honesty-NA. Elasticsearch is a document store; join/nested
//                    types are denormalisation, not relations. Recorded
//                    not_applicable.
//
// The extractor gates on an ES client import actually being present.

func init() {
	extractor.Register("custom_go_elasticsearch", &elasticExtractor{})
}

type elasticExtractor struct{}

func (e *elasticExtractor) Language() string { return "custom_go_elasticsearch" }

var (
	// Import marker for the two common Go ES clients.
	reImportElastic = regexp.MustCompile(`"github\.com/(?:elastic/go-elasticsearch(?:/v\d+)?|olivere/elastic(?:/v\d+)?)`)

	// Index identification: Index("name") / Indices("name") / Index: "name".
	reESIndexCall = regexp.MustCompile(`\b(?:Index|Indices)\s*[(:]\s*"([^"]+)"`)

	// Index-creation (the migration analogue): CreateIndex("name") or
	// indices.Create / .Create("name") on an index API. The index name, when a
	// string literal, is captured.
	reESCreateIndex = regexp.MustCompile(`\b(?:CreateIndex|(?:[Ii]ndices\.)?Create)\(\s*"([^"]+)"`)

	// A struct field carrying a `json:"field"` tag (ES documents are marshalled
	// as JSON). Heuristic document-shape detection.
	reESJSONField = regexp.MustCompile("(?m)^\\s*(\\w+)\\s+([\\w\\.\\[\\]\\*]+)\\s+`[^`]*\\bjson:\"([^\"]*)\"[^`]*`")

	// Query/operation call sites on an ES client or request builder.
	reESQueryCall = regexp.MustCompile(
		`(?m)\.(MultiSearch|Msearch|SearchScroll|Search|IndexDoc|Index|Bulk|MultiGet|Mget|Get|Update|Delete|Count|Scroll|Reindex)\s*\(`,
	)
)

func (e *elasticExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.elasticsearch_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "elasticsearch"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "go" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reImportElastic.MatchString(src) {
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

	// 1. Schema: index name call sites => indices.
	for _, m := range reESIndexCall.FindAllStringSubmatchIndex(src, -1) {
		idx := src[m[2]:m[3]]
		ent := makeEntity("index:"+idx, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elasticsearch", "provenance", "INFERRED_FROM_ES_INDEX",
			"index_name", idx)
		add(ent)
	}

	// 2. Models / Schema: structs with `json:"field"` field tags => document
	//    shapes (heuristic).
	for _, sm := range reDBStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])
		fields := reESJSONField.FindAllStringSubmatch(body, -1)
		if len(fields) == 0 {
			continue
		}
		ent := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&ent, "framework", "elasticsearch", "provenance", "INFERRED_FROM_ES_DOC_STRUCT")
		add(ent)

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			field := strings.TrimSpace(fm[3])
			if field == "" || field == "-" {
				continue
			}
			if i := strings.IndexByte(field, ','); i >= 0 {
				field = field[:i]
			}
			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", "elasticsearch", "provenance", "INFERRED_FROM_ES_DOC_STRUCT",
				"model_name", structName, "field_name", fieldName, "es_field", field, "go_type", fieldType)
			add(fieldEnt)
		}
	}

	// 3. Migrations (partial): index-creation call sites.
	for _, m := range reESCreateIndex.FindAllStringSubmatchIndex(src, -1) {
		idx := src[m[2]:m[3]]
		ent := makeEntity("create_index:"+idx, "SCOPE.Operation", "migration", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elasticsearch", "provenance", "INFERRED_FROM_ES_CREATE_INDEX",
			"index_name", idx, "migration_kind", "index_creation")
		add(ent)
	}

	// 4. Queries: client / request-builder call sites.
	for _, m := range reESQueryCall.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("es:"+verb+":"+itoa(lineOf(src, m[0])), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "elasticsearch", "provenance", "INFERRED_FROM_ES_QUERY",
			"query_type", esVerbKind(verb), "call_verb", verb)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// esVerbKind classifies an ES client/builder method into a coarse CRUD verb so
// query_type is comparable across the data-access extractors.
func esVerbKind(verb string) string {
	switch verb {
	case "Search", "SearchScroll", "MultiSearch", "Msearch", "Get", "MultiGet",
		"Mget", "Count", "Scroll":
		return "select"
	case "Index", "IndexDoc", "Bulk", "Reindex":
		return "insert"
	case "Update":
		return "update"
	case "Delete":
		return "delete"
	default:
		return "query"
	}
}
