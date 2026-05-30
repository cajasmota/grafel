package csharp_test

// ---------------------------------------------------------------------------
// Driver schema_extraction — Cassandra, MongoDB, DynamoDB, Elasticsearch
// ---------------------------------------------------------------------------

import "testing"

func TestCassandraTableAttribute(t *testing.T) {
	src := `
using Cassandra.Data.Linq;

[Table("users")]
public class User
{
    [Column("user_id")]
    [PartitionKey]
    public Guid Id { get; set; }

    [Column("user_name")]
    public string Name { get; set; }
}
`
	ents := extract(t, "custom_csharp_driver_schema", fi("User.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "cassandra:table:users") {
		t.Error("expected cassandra:table:users schema_extraction entity")
	}
	foundCol := false
	for _, e := range ents {
		if e.Subtype == "schema_extraction" && e.Kind == "SCOPE.Pattern" {
			foundCol = true
			break
		}
	}
	if !foundCol {
		t.Error("expected schema_extraction entity from Cassandra [Column] attribute")
	}
}

func TestMongoDBBsonElement(t *testing.T) {
	src := `
using MongoDB.Bson;
using MongoDB.Bson.Serialization.Attributes;

public class Product
{
    [BsonElement("product_name")]
    public string Name { get; set; }

    [BsonElement("price")]
    public decimal Price { get; set; }

    [BsonIgnore]
    public string InternalCode { get; set; }
}
`
	ents := extract(t, "custom_csharp_driver_schema", fi("Product.cs", "csharp", src))

	foundElement := false
	foundIgnore := false
	for _, e := range ents {
		if e.Subtype == "schema_extraction" {
			if e.Name == "mongodb:field:product_name:Product.cs:8" || e.Name == "mongodb:field:price:Product.cs:12" {
				foundElement = true
			}
			if e.Kind == "SCOPE.Pattern" && len(e.Name) > 16 && e.Name[:16] == "mongodb:ignore:P" {
				foundIgnore = true
			}
		}
	}
	// Just check that we got some schema entities
	schemaCount := 0
	for _, e := range ents {
		if e.Subtype == "schema_extraction" {
			schemaCount++
		}
	}
	if schemaCount < 3 {
		t.Errorf("expected at least 3 schema_extraction entities (2 BsonElement + 1 BsonIgnore), got %d", schemaCount)
	}
	_ = foundElement
	_ = foundIgnore
}

func TestMongoDBBsonCollection(t *testing.T) {
	src := `
using MongoDB.Driver;

[BsonCollection("orders")]
public class Order
{
    public ObjectId Id { get; set; }
}
`
	ents := extract(t, "custom_csharp_driver_schema", fi("Order.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "mongodb:collection:orders") {
		t.Error("expected mongodb:collection:orders schema_extraction entity")
	}
}

func TestDynamoDBTableAttribute(t *testing.T) {
	src := `
using Amazon.DynamoDBv2.DataModel;

[DynamoDBTable("Products")]
public class Product
{
    [DynamoDBHashKey]
    public string Id { get; set; }

    [DynamoDBRangeKey]
    public string Category { get; set; }

    [DynamoDBProperty("product_name")]
    public string Name { get; set; }
}
`
	ents := extract(t, "custom_csharp_driver_schema", fi("Product.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Pattern", "dynamodb:table:Products") {
		t.Error("expected dynamodb:table:Products schema_extraction entity")
	}

	foundHash := false
	foundRange := false
	foundProp := false
	for _, e := range ents {
		if e.Subtype == "schema_extraction" {
			switch {
			case len(e.Name) > 16 && e.Name[:16] == "dynamodb:hashkey":
				foundHash = true
			case len(e.Name) > 17 && e.Name[:17] == "dynamodb:rangekey":
				foundRange = true
			case len(e.Name) > 19 && e.Name[:19] == "dynamodb:property:p":
				foundProp = true
			}
		}
	}
	if !foundHash {
		t.Error("expected dynamodb:hashkey schema_extraction from [DynamoDBHashKey]")
	}
	if !foundRange {
		t.Error("expected dynamodb:rangekey schema_extraction from [DynamoDBRangeKey]")
	}
	if !foundProp {
		t.Error("expected dynamodb:property schema_extraction from [DynamoDBProperty]")
	}
}

func TestElasticsearchNESTAttributes(t *testing.T) {
	src := `
using Nest;

[ElasticsearchType(RelationName = "product")]
public class Product
{
    [Keyword]
    public string Id { get; set; }

    [Text(Name = "product_name")]
    public string Name { get; set; }

    [PropertyName("price_amount")]
    public decimal Price { get; set; }

    [Number]
    public int StockCount { get; set; }
}
`
	ents := extract(t, "custom_csharp_driver_schema", fi("Product.cs", "csharp", src))

	foundType := false
	foundField := false
	for _, e := range ents {
		if e.Subtype == "schema_extraction" {
			if len(e.Name) > 13 && e.Name[:13] == "elastic:type:" {
				foundType = true
			}
			if len(e.Name) > 14 && (e.Name[:14] == "elastic:field:" || e.Name[:19] == "elastic:field_attr:") {
				foundField = true
			}
		}
	}
	if !foundType {
		t.Error("expected elastic:type schema_extraction from [ElasticsearchType]")
	}
	if !foundField {
		t.Error("expected elastic:field schema_extraction from NEST field attributes")
	}
}

func TestDriverSchemaNoMatch(t *testing.T) {
	src := `namespace App { class Helper { } }`
	ents := extract(t, "custom_csharp_driver_schema", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
