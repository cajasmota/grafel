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

// mongo_driver.go: schema-inference + query-DSL extractor for the official
// MongoDB Go driver (go.mongodb.org/mongo-driver).
//
// MongoDB is schema-less, so the honest coverage shape is:
//
//   - Models / Schema — partial. A `client.Database(..).Collection("x")`
//                    call site is recognised as a SCOPE.Schema collection,
//                    and Go structs carrying `bson:"field"` tags are
//                    recognised as document shapes with their fields
//                    enumerated. A `bson:` tag is a heuristic (it does not
//                    prove the struct maps to a collection) and a collection
//                    has no enforced schema, hence partial not full.
//   - Queries      — partial. Collection method call sites
//                    (Find/FindOne/InsertOne/InsertMany/UpdateOne/UpdateMany/
//                    DeleteOne/DeleteMany/Aggregate/CountDocuments/...) are
//                    captured with the operation verb. Binding a query to a
//                    concrete collection variable from a regex is not
//                    reliable, so this stays partial.
//   - Relationships— honesty-NA. MongoDB has no relational layer; references
//                    between documents are an application convention, not a
//                    driver concept. Recorded as not_applicable in the
//                    registry (no code claim).
//   - Migrations   — honesty-NA. The driver ships no migration runner and
//                    collections are created lazily on first write.
//
// The extractor gates on the mongo-driver import actually being present in
// the file, so a file that merely mentions "bson" without the driver is not
// poached.

func init() {
	extractor.Register("custom_go_mongo_driver", &mongoDriverExtractor{})
}

type mongoDriverExtractor struct{}

func (e *mongoDriverExtractor) Language() string { return "custom_go_mongo_driver" }

var (
	// Import marker for the official MongoDB Go driver. Presence gates the
	// whole extractor. Matches the mongo package and its bson/options subpkgs.
	reImportMongo = regexp.MustCompile(`"go\.mongodb\.org/mongo-driver(?:/v\d+)?/(?:mongo|bson)`)

	// .Collection("name") — names a collection => a SCOPE.Schema entity.
	// Only a string-literal argument is captured (a variable name is not a
	// stable collection identity).
	reMongoCollection = regexp.MustCompile(`\.Collection\(\s*"([^"]+)"`)

	// A struct field carrying a `bson:"field"` tag.
	//   ID   primitive.ObjectID `bson:"_id"`
	//   Name string             `bson:"name"`
	reBSONField = regexp.MustCompile("(?m)^\\s*(\\w+)\\s+([\\w\\.\\[\\]\\*]+)\\s+`[^`]*\\bbson:\"([^\"]*)\"[^`]*`")

	// Collection query method call sites. The verb is captured so query_type
	// can be stamped. Ordered longest-first within alternations is not needed
	// because Go's regexp is leftmost-longest for alternation? (RE2 is
	// leftmost, not longest) — each alternative is anchored by the trailing
	// `(`, so no prefix can shadow a longer verb here.
	reMongoQueryCall = regexp.MustCompile(
		`(?m)\.(FindOneAndUpdate|FindOneAndReplace|FindOneAndDelete|InsertMany|InsertOne|UpdateMany|UpdateByID|UpdateOne|ReplaceOne|DeleteMany|DeleteOne|CountDocuments|EstimatedDocumentCount|Distinct|Aggregate|BulkWrite|FindOne|Find)\s*\(`,
	)
)

func (e *mongoDriverExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.mongo_driver_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "mongodb"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "go" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reImportMongo.MatchString(src) {
		// No mongo-driver import: not our file.
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

	// 1. Schema: .Collection("name") call sites => collections.
	for _, m := range reMongoCollection.FindAllStringSubmatchIndex(src, -1) {
		coll := src[m[2]:m[3]]
		ent := makeEntity("collection:"+coll, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongodb", "provenance", "INFERRED_FROM_MONGO_COLLECTION",
			"collection_name", coll)
		add(ent)
	}

	// 2. Models / Schema: structs with `bson:"field"` field tags.
	for _, sm := range reDBStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])
		fields := reBSONField.FindAllStringSubmatch(body, -1)
		if len(fields) == 0 {
			continue
		}
		ent := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&ent, "framework", "mongodb", "provenance", "INFERRED_FROM_BSON_STRUCT_TAGS")
		add(ent)

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			field := strings.TrimSpace(fm[3])
			// "-" means "omit this field"; skip it.
			if field == "" || field == "-" {
				continue
			}
			// Strip option suffixes such as `bson:"name,omitempty"`.
			if i := strings.IndexByte(field, ','); i >= 0 {
				field = field[:i]
			}
			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", "mongodb", "provenance", "INFERRED_FROM_BSON_STRUCT_TAGS",
				"model_name", structName, "field_name", fieldName, "bson_field", field, "go_type", fieldType)
			add(fieldEnt)
		}
	}

	// 3. Queries: collection method call sites. Heuristic — captures the verb
	//    but cannot bind to a concrete collection from a regex.
	for _, m := range reMongoQueryCall.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("mongo:"+verb+":"+lineToken(src, m[0]), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongodb", "provenance", "INFERRED_FROM_MONGO_CALL",
			"query_type", mongoVerbKind(verb), "call_verb", verb)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// mongoVerbKind classifies a collection method into a coarse CRUD verb so the
// query_type property is comparable with the SQL-driver extractor's verbs.
func mongoVerbKind(verb string) string {
	switch {
	case strings.HasPrefix(verb, "Insert"):
		return "insert"
	case strings.HasPrefix(verb, "Update"), verb == "ReplaceOne", verb == "FindOneAndUpdate", verb == "FindOneAndReplace", verb == "BulkWrite":
		return "update"
	case strings.HasPrefix(verb, "Delete"), verb == "FindOneAndDelete":
		return "delete"
	default:
		// Find/FindOne/Aggregate/CountDocuments/Distinct/... are reads.
		return "select"
	}
}
