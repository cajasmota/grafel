package java

// dynamodb_java.go — DynamoDB Enhanced Client Java extractor.
//
// Detects AWS SDK v2 DynamoDB Enhanced Client annotations from
// software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.*:
//
//   - @DynamoDbBean          → model entity (SCOPE.Schema / "model")
//   - @DynamoDbPartitionKey  → schema attribute (SCOPE.Component / "partition_key")
//   - @DynamoDbSortKey       → schema attribute (SCOPE.Component / "sort_key")
//   - @DynamoDbAttribute     → schema attribute (SCOPE.Component / "attribute")
//   - @DynamoDbIgnore        → ignored field (SCOPE.Component / "ignored_field")
//
// DynamoDB is a key-value/document store. It has no relational FK,
// lazy-loading, or ORM association concepts; those cells are not_applicable.
//
// The extractor self-gates on the presence of the awssdk enhanced-dynamodb
// import or a @DynamoDbBean annotation so it does not fire on generic Java
// sources.

import (
	"context"
	"regexp"
	"strings"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_java_dynamodb", &dynamoDBExtractor{})
}

type dynamoDBExtractor struct{}

func (d *dynamoDBExtractor) Language() string { return "custom_java_dynamodb" }

var (
	// Gate: must have the awssdk enhanced dynamodb import or a @DynamoDbBean
	// annotation (handles cases where star-imports are used).
	dynamoDBMarkerRE = regexp.MustCompile(
		`software\.amazon\.awssdk\.enhanced\.dynamodb|@DynamoDbBean\b`)

	// @DynamoDbBean class — captures the class name.
	// Handles optional modifiers between the annotation and the class keyword.
	dynamoDBBeanClassRE = regexp.MustCompile(
		`@DynamoDbBean\b[^{]*?class\s+(\w+)`)

	// @DynamoDbPartitionKey getter/field — captures the method/field name
	// on the line following the annotation.
	dynamoDBPartitionKeyRE = regexp.MustCompile(
		`@DynamoDbPartitionKey\b[^\n]*\n\s*(?:(?:public|protected|private|static|final)\s+)*\w+\s+(\w+)\s*[\(\{;]`)

	// @DynamoDbSortKey getter/field.
	dynamoDBSortKeyRE = regexp.MustCompile(
		`@DynamoDbSortKey\b[^\n]*\n\s*(?:(?:public|protected|private|static|final)\s+)*\w+\s+(\w+)\s*[\(\{;]`)

	// @DynamoDbAttribute("column_name") — captures the alias value.
	dynamoDBAttributeRE = regexp.MustCompile(
		`@DynamoDbAttribute\s*\(\s*"([^"]+)"\s*\)`)

	// @DynamoDbIgnore getter/field.
	dynamoDBIgnoreRE = regexp.MustCompile(
		`@DynamoDbIgnore\b[^\n]*\n\s*(?:(?:public|protected|private|static|final)\s+)*\w+\s+(\w+)\s*[\(\{;]`)
)

func (d *dynamoDBExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	if strings.ToLower(file.Language) != "java" {
		return nil, nil
	}
	src := string(file.Content)
	if !dynamoDBMarkerRE.MatchString(src) {
		return nil, nil
	}

	fp := file.Path
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

	// --- @DynamoDbBean class → model entity ---
	for _, m := range dynamoDBBeanClassRE.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		ent := makeEntity(className, "SCOPE.Schema", "model", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dynamodb_enhanced",
			"provenance", "INFERRED_FROM_DYNAMODB_BEAN",
		)
		add(ent)
	}

	// --- @DynamoDbPartitionKey → partition_key attribute ---
	for _, m := range dynamoDBPartitionKeyRE.FindAllStringSubmatchIndex(src, -1) {
		memberName := src[m[2]:m[3]]
		ent := makeEntity("pk:"+memberName, "SCOPE.Component", "partition_key", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dynamodb_enhanced",
			"member_name", memberName,
			"provenance", "INFERRED_FROM_DYNAMODB_PARTITION_KEY",
		)
		add(ent)
	}

	// --- @DynamoDbSortKey → sort_key attribute ---
	for _, m := range dynamoDBSortKeyRE.FindAllStringSubmatchIndex(src, -1) {
		memberName := src[m[2]:m[3]]
		ent := makeEntity("sk:"+memberName, "SCOPE.Component", "sort_key", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dynamodb_enhanced",
			"member_name", memberName,
			"provenance", "INFERRED_FROM_DYNAMODB_SORT_KEY",
		)
		add(ent)
	}

	// --- @DynamoDbAttribute("alias") → attribute ---
	for _, m := range dynamoDBAttributeRE.FindAllStringSubmatchIndex(src, -1) {
		alias := src[m[2]:m[3]]
		ent := makeEntity("attr:"+alias, "SCOPE.Component", "attribute", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dynamodb_enhanced",
			"alias", alias,
			"provenance", "INFERRED_FROM_DYNAMODB_ATTRIBUTE",
		)
		add(ent)
	}

	// --- @DynamoDbIgnore → ignored_field ---
	for _, m := range dynamoDBIgnoreRE.FindAllStringSubmatchIndex(src, -1) {
		memberName := src[m[2]:m[3]]
		ent := makeEntity("ignore:"+memberName, "SCOPE.Component", "ignored_field", fp, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"framework", "dynamodb_enhanced",
			"member_name", memberName,
			"provenance", "INFERRED_FROM_DYNAMODB_IGNORE",
		)
		add(ent)
	}

	return entities, nil
}
