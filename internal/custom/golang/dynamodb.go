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

// dynamodb.go: item-shape + query-DSL extractor for the AWS DynamoDB Go SDK
// (aws-sdk-go-v2/service/dynamodb, and the v1 aws-sdk-go path).
//
// DynamoDB is schema-less at the attribute level (only the key schema is
// declared), so the honest coverage shape is:
//
//   - Models / Schema — partial. Go structs carrying `dynamodbav:"attr"` tags
//                    are recognised as item shapes with their attributes
//                    enumerated, and `TableName: aws.String("x")` /
//                    `TableName: "x"` literals name SCOPE.Schema tables. A
//                    `dynamodbav` tag is a marshalling hint (it does not prove
//                    the struct maps to a table) and an item has no enforced
//                    schema beyond the key, hence partial.
//   - Queries      — partial. Client method call sites (GetItem/PutItem/Query/
//                    Scan/UpdateItem/DeleteItem/BatchGetItem/BatchWriteItem/
//                    TransactWriteItems/...) are captured with the operation
//                    verb. Binding a call to a concrete table from a regex is
//                    not reliable, so this stays partial.
//   - Relationships— honesty-NA. DynamoDB has no foreign keys or joins. GSIs /
//                    LSIs are access-path indexes, not relations, and are not
//                    modelled as relationships here. Recorded not_applicable.
//   - Migrations   — honesty-NA. The SDK ships no migration runner; tables are
//                    created via CreateTable/IaC out-of-band.
//
// The extractor gates on the dynamodb SDK import actually being present, so a
// file that merely mentions "dynamodbav" without the SDK is not poached.

func init() {
	extractor.Register("custom_go_dynamodb", &dynamoExtractor{})
}

type dynamoExtractor struct{}

func (e *dynamoExtractor) Language() string { return "custom_go_dynamodb" }

var (
	// Import marker for the DynamoDB SDK (v2 and v1 service paths).
	reImportDynamo = regexp.MustCompile(`"github\.com/aws/aws-sdk-go(?:-v2)?/(?:service|aws)/dynamodb`)

	// A struct field carrying a `dynamodbav:"attr"` tag.
	//   ID   string `dynamodbav:"id"`
	//   Name string `dynamodbav:"name,omitempty"`
	reDynamoavField = regexp.MustCompile("(?m)^\\s*(\\w+)\\s+([\\w\\.\\[\\]\\*]+)\\s+`[^`]*\\bdynamodbav:\"([^\"]*)\"[^`]*`")

	// TableName: aws.String("x") / TableName: "x" => names a table.
	reDynamoTableName = regexp.MustCompile(`TableName:\s*(?:aws\.String\(\s*)?"([^"]+)"`)

	// Client method call sites. Verb captured for query_type stamping.
	reDynamoQueryCall = regexp.MustCompile(
		`(?m)\.(BatchGetItem|BatchWriteItem|TransactGetItems|TransactWriteItems|GetItem|PutItem|UpdateItem|DeleteItem|Query|Scan)\s*\(`,
	)
)

func (e *dynamoExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.dynamodb_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "dynamodb"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if file.Language != "go" || len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	if !reImportDynamo.MatchString(src) {
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

	// 1. Schema: TableName: "x" literals => tables.
	for _, m := range reDynamoTableName.FindAllStringSubmatchIndex(src, -1) {
		table := src[m[2]:m[3]]
		ent := makeEntity("table:"+table, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_TABLE",
			"table_name", table)
		add(ent)
	}

	// 2. Models / Schema: structs with `dynamodbav:"attr"` field tags.
	for _, sm := range reDBStruct.FindAllStringSubmatchIndex(src, -1) {
		structName := src[sm[2]:sm[3]]
		body := src[sm[4]:sm[5]]
		structLine := lineOf(src, sm[0])
		fields := reDynamoavField.FindAllStringSubmatch(body, -1)
		if len(fields) == 0 {
			continue
		}
		ent := makeEntity(structName, "SCOPE.Schema", "", file.Path, file.Language, structLine)
		setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_ITEM_STRUCT")
		add(ent)

		for _, fm := range fields {
			fieldName := fm[1]
			fieldType := fm[2]
			attr := strings.TrimSpace(fm[3])
			if attr == "" || attr == "-" {
				continue
			}
			if i := strings.IndexByte(attr, ','); i >= 0 {
				attr = attr[:i]
			}
			fieldEnt := makeEntity("field:"+structName+"."+fieldName, "SCOPE.Component", "field", file.Path, file.Language, structLine)
			setProps(&fieldEnt, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_ITEM_STRUCT",
				"model_name", structName, "field_name", fieldName, "dynamodb_attr", attr, "go_type", fieldType)
			add(fieldEnt)
		}
	}

	// 3. Queries: client method call sites.
	for _, m := range reDynamoQueryCall.FindAllStringSubmatchIndex(src, -1) {
		verb := src[m[2]:m[3]]
		ent := makeEntity("dynamo:"+verb+":"+itoa(lineOf(src, m[0])), "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_QUERY",
			"query_type", dynamoVerbKind(verb), "call_verb", verb)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// dynamoVerbKind classifies a DynamoDB client method into a coarse CRUD verb so
// query_type is comparable across the data-access extractors.
func dynamoVerbKind(verb string) string {
	switch verb {
	case "GetItem", "BatchGetItem", "Query", "Scan", "TransactGetItems":
		return "select"
	case "PutItem", "BatchWriteItem":
		return "insert"
	case "UpdateItem", "TransactWriteItems":
		return "update"
	case "DeleteItem":
		return "delete"
	default:
		return "query"
	}
}
