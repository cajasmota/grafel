// Package csharp — schema_extraction extractor for C# NoSQL / document
// database driver attribute annotations.
//
// Raw ADO.NET drivers (MySQL, Npgsql, SQLite) do not provide schema-mapping
// attributes — schema_extraction for those records is not_applicable.
//
// Drivers with attribute-based schema mapping that this extractor covers:
//
//	cassandra  — Cassandra.Data.Linq [Table("ks.tbl")] / [Column("col")] POCO
//	             attributes used with the Cassandra LINQ driver.
//
//	mongodb    — MongoDB.Bson [BsonCollection("coll")] / [BsonElement("field")]
//	             / [BsonIgnore] attribute annotations on POCO documents.
//
//	dynamodb   — AWSSDK.DynamoDBv2 [DynamoDBTable("tbl")] /
//	             [DynamoDBHashKey] / [DynamoDBRangeKey] /
//	             [DynamoDBProperty("attr")] attribute annotations.
//
//	elastic    — NEST / Elastic.Clients.Elasticsearch [ElasticsearchType(...)],
//	             [PropertyName("field")], [Text], [Keyword], [Number] attribute
//	             annotations on POCO documents.
//
// Registration key: "custom_csharp_driver_schema"
// Emitted entity kind: SCOPE.Pattern with subtype "schema_extraction"
//
//	and SCOPE.Component with subtype "model_extraction" for document types.
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
	extractor.Register("custom_csharp_driver_schema", &driverSchemaExtractor{})
}

type driverSchemaExtractor struct{}

func (e *driverSchemaExtractor) Language() string { return "custom_csharp_driver_schema" }

// ---------------------------------------------------------------------------
// Namespace / framework detection
// ---------------------------------------------------------------------------

var (
	reDSCassandra = regexp.MustCompile(`using\s+Cassandra(?:\.Data(?:\.Linq)?)?\b`)
	reDSMongoDB   = regexp.MustCompile(`using\s+MongoDB\.(?:Bson|Driver)\b`)
	reDSDynamoDB  = regexp.MustCompile(`using\s+Amazon\.DynamoDBv2\b`)
	reDSElastic   = regexp.MustCompile(`using\s+(?:Nest|Elastic\.Clients\.Elasticsearch)\b`)
)

// ---------------------------------------------------------------------------
// Cassandra LINQ POCO attributes
// ---------------------------------------------------------------------------

var (
	// [Table("keyspace.tableName")] — Cassandra LINQ class attribute
	reCassTable = regexp.MustCompile(
		`\[Table\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
	// [Column("colName")] — Cassandra LINQ property attribute (shared with Dapper regex)
	reCassColumn = regexp.MustCompile(
		`\[Column\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
)

// ---------------------------------------------------------------------------
// MongoDB BSON attributes
// ---------------------------------------------------------------------------

var (
	// [BsonCollection("collectionName")] (MongoDB.Driver.Linq style custom attr)
	reBsonCollection = regexp.MustCompile(
		`\[BsonCollection\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
	// [BsonElement("fieldName")] on a property
	reBsonElement = regexp.MustCompile(
		`\[BsonElement\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
	// [BsonIgnore] on a property
	reBsonIgnore = regexp.MustCompile(
		`\[BsonIgnore\]`,
	)
	// Class names that are MongoDB documents — any class in a file with Bson usings
	reBsonDocClass = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*(?::\s*[\w\s,<>]+)?\s*\{`,
	)
)

// ---------------------------------------------------------------------------
// DynamoDB attributes
// ---------------------------------------------------------------------------

var (
	// [DynamoDBTable("tableName")] on a class
	reDDBTable = regexp.MustCompile(
		`\[DynamoDBTable\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
	// [DynamoDBHashKey] on a property
	reDDBHashKey = regexp.MustCompile(
		`\[DynamoDBHashKey(?:\s*\([^)]*\))?\s*\]`,
	)
	// [DynamoDBRangeKey] on a property
	reDDBRangeKey = regexp.MustCompile(
		`\[DynamoDBRangeKey(?:\s*\([^)]*\))?\s*\]`,
	)
	// [DynamoDBProperty("attrName")] on a property
	reDDBProperty = regexp.MustCompile(
		`\[DynamoDBProperty\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
)

// ---------------------------------------------------------------------------
// Elasticsearch / NEST attributes
// ---------------------------------------------------------------------------

var (
	// [ElasticsearchType(RelationName = "typename")] on a class
	reESType = regexp.MustCompile(
		`\[ElasticsearchType\s*\([^)]*\)\s*\]`,
	)
	// [PropertyName("fieldName")] on a property
	reESPropertyName = regexp.MustCompile(
		`\[PropertyName\s*\(\s*["']([^"']+)["']\s*\)\s*\]`,
	)
	// [Text] / [Keyword] / [Number] / [Date] field type attributes
	reESFieldAttr = regexp.MustCompile(
		`\[(?:Text|Keyword|Number|Date|Boolean|Binary|Object|Nested|GeoPoint|GeoShape|Completion|SearchAsYouType|RankFeature|Flattened)(?:\s*\([^)]*\))?\s*\]`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *driverSchemaExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_driver_schema_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)
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

	isCassandra := reDSCassandra.MatchString(src)
	isMongoDB := reDSMongoDB.MatchString(src)
	isDynamoDB := reDSDynamoDB.MatchString(src)
	isElastic := reDSElastic.MatchString(src)

	// -------------------------------------------------------------------------
	// Cassandra LINQ POCO schema
	// -------------------------------------------------------------------------

	if isCassandra || reCassTable.MatchString(src) {
		// [Table("...")] → schema_extraction
		for _, m := range reCassTable.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "cassandra:table:" + tableName
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "cassandra", "provenance", "INFERRED_FROM_CASSANDRA_TABLE",
				"table_name", tableName)
			add(ent)
		}

		// [Column("...")] → schema_extraction
		for _, m := range reCassColumn.FindAllStringSubmatchIndex(src, -1) {
			colName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "cassandra:column:" + colName + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "cassandra", "provenance", "INFERRED_FROM_CASSANDRA_COLUMN",
				"column_name", colName)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// MongoDB BSON schema
	// -------------------------------------------------------------------------

	if isMongoDB || reBsonElement.MatchString(src) || reBsonCollection.MatchString(src) {
		// [BsonCollection("...")] → schema_extraction (collection name)
		for _, m := range reBsonCollection.FindAllStringSubmatchIndex(src, -1) {
			collName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "mongodb:collection:" + collName
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "mongodb", "provenance", "INFERRED_FROM_BSON_COLLECTION",
				"collection_name", collName)
			add(ent)
		}

		// [BsonElement("...")] → schema_extraction (field name)
		for _, m := range reBsonElement.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "mongodb:field:" + fieldName + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "mongodb", "provenance", "INFERRED_FROM_BSON_ELEMENT",
				"field_name", fieldName)
			add(ent)
		}

		// [BsonIgnore] → schema_extraction (excluded field)
		for _, m := range reBsonIgnore.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "mongodb:ignore:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "mongodb", "provenance", "INFERRED_FROM_BSON_IGNORE")
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// DynamoDB attribute schema
	// -------------------------------------------------------------------------

	if isDynamoDB || reDDBTable.MatchString(src) || reDDBHashKey.MatchString(src) {
		// [DynamoDBTable("...")] → schema_extraction
		for _, m := range reDDBTable.FindAllStringSubmatchIndex(src, -1) {
			tableName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "dynamodb:table:" + tableName
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_TABLE",
				"table_name", tableName)
			add(ent)
		}

		// [DynamoDBHashKey] → schema_extraction
		for _, m := range reDDBHashKey.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "dynamodb:hashkey:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_HASH_KEY")
			add(ent)
		}

		// [DynamoDBRangeKey] → schema_extraction
		for _, m := range reDDBRangeKey.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "dynamodb:rangekey:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_RANGE_KEY")
			add(ent)
		}

		// [DynamoDBProperty("...")] → schema_extraction
		for _, m := range reDDBProperty.FindAllStringSubmatchIndex(src, -1) {
			attrName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "dynamodb:property:" + attrName + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "dynamodb", "provenance", "INFERRED_FROM_DYNAMODB_PROPERTY",
				"attribute_name", attrName)
			add(ent)
		}
	}

	// -------------------------------------------------------------------------
	// Elasticsearch / NEST schema
	// -------------------------------------------------------------------------

	if isElastic || reESType.MatchString(src) || reESFieldAttr.MatchString(src) {
		// [ElasticsearchType(...)] → schema_extraction
		for _, m := range reESType.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "elastic:type:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "elastic", "provenance", "INFERRED_FROM_ELASTICSEARCH_TYPE")
			add(ent)
		}

		// [PropertyName("...")] → schema_extraction
		for _, m := range reESPropertyName.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			name := "elastic:field:" + fieldName + ":" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "elastic", "provenance", "INFERRED_FROM_ES_PROPERTY_NAME",
				"field_name", fieldName)
			add(ent)
		}

		// [Text] / [Keyword] / [Number] etc. → schema_extraction
		for _, m := range reESFieldAttr.FindAllStringIndex(src, -1) {
			line := lineOf(src, m[0])
			name := "elastic:field_attr:" + file.Path + ":" + itoa(line)
			ent := makeEntity(name, "SCOPE.Pattern", "schema_extraction", file.Path, "csharp", line)
			setProps(&ent, "framework", "elastic", "provenance", "INFERRED_FROM_ES_FIELD_ATTR")
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
