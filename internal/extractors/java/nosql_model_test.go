package java_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4283 — Spring Data NoSQL schema/model extraction.
//
// A @Document / @Table / @RedisHash annotated class must produce a SCOPE.Schema
// model entity (Subtype "schema") carrying the collection/table/keyspace name +
// field-membership CONTAINS edges. A plain class must NOT.

func nosqlModel(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "schema" {
			return &ents[i]
		}
	}
	return nil
}

func modelContainsField(model *types.EntityRecord, file, className, field string) bool {
	want := extractor.BuildSchemaFieldStructuralRef("java", file, className+"."+field)
	for _, r := range model.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == want {
			return true
		}
	}
	return false
}

func TestNoSQLModel_MongoDocument(t *testing.T) {
	src := `package com.example;
import org.springframework.data.mongodb.core.mapping.Document;
import org.springframework.data.mongodb.core.mapping.Field;
import org.springframework.data.annotation.Id;

@Document(collection="users")
public class User {
    @Id
    private String id;
    @Field("e")
    private String email;
    private String name;
}`
	ents := runJava(t, src)
	m := nosqlModel(ents, "User")
	if m == nil {
		t.Fatal("expected SCOPE.Schema/schema model entity for @Document class User")
	}
	if m.Properties["store"] != "mongodb" {
		t.Errorf("store: want mongodb, got %q", m.Properties["store"])
	}
	if m.Properties["collection"] != "users" {
		t.Errorf("collection: want users, got %q", m.Properties["collection"])
	}
	if m.QualifiedName != "com.example.User" {
		t.Errorf("qn: want com.example.User, got %q", m.QualifiedName)
	}
	for _, f := range []string{"id", "email", "name"} {
		if !modelContainsField(m, "Test.java", "User", f) {
			t.Errorf("expected CONTAINS edge to field %q", f)
		}
	}
	// @Field("e") override name reflected.
	if m.Properties["field.email.column"] != "e" {
		t.Errorf("field.email.column: want e, got %q", m.Properties["field.email.column"])
	}
	if m.Properties["field.id.id"] != "true" {
		t.Errorf("field.id.id: want true, got %q", m.Properties["field.id.id"])
	}
}

func TestNoSQLModel_MongoPositional(t *testing.T) {
	src := `package com.example;
import org.springframework.data.mongodb.core.mapping.Document;
@Document("books")
public class Book {
    private String title;
}`
	ents := runJava(t, src)
	m := nosqlModel(ents, "Book")
	if m == nil {
		t.Fatal("expected model for @Document(\"books\")")
	}
	if m.Properties["collection"] != "books" {
		t.Errorf("collection: want books, got %q", m.Properties["collection"])
	}
}

func TestNoSQLModel_CassandraTable(t *testing.T) {
	src := `package com.example;
import org.springframework.data.cassandra.core.mapping.Table;
import org.springframework.data.cassandra.core.mapping.PrimaryKey;
import org.springframework.data.cassandra.core.mapping.Column;

@Table("users")
public class User {
    @PrimaryKey
    private String id;
    @Column("full_name")
    private String name;
}`
	ents := runJava(t, src)
	m := nosqlModel(ents, "User")
	if m == nil {
		t.Fatal("expected model for @Table class")
	}
	if m.Properties["store"] != "cassandra" {
		t.Errorf("store: want cassandra, got %q", m.Properties["store"])
	}
	if m.Properties["table"] != "users" {
		t.Errorf("table: want users, got %q", m.Properties["table"])
	}
	if !modelContainsField(m, "Test.java", "User", "id") {
		t.Error("expected CONTAINS edge to field id")
	}
	if m.Properties["field.name.column"] != "full_name" {
		t.Errorf("field.name.column: want full_name, got %q", m.Properties["field.name.column"])
	}
}

func TestNoSQLModel_RedisHash(t *testing.T) {
	src := `package com.example;
import org.springframework.data.redis.core.RedisHash;
import org.springframework.data.annotation.Id;
import org.springframework.data.redis.core.index.Indexed;

@RedisHash("people")
public class Person {
    @Id
    private String id;
    @Indexed
    private String email;
}`
	ents := runJava(t, src)
	m := nosqlModel(ents, "Person")
	if m == nil {
		t.Fatal("expected model for @RedisHash class")
	}
	if m.Properties["store"] != "redis" {
		t.Errorf("store: want redis, got %q", m.Properties["store"])
	}
	if m.Properties["keyspace"] != "people" {
		t.Errorf("keyspace: want people, got %q", m.Properties["keyspace"])
	}
	if m.Properties["field.email.indexed"] != "true" {
		t.Errorf("field.email.indexed: want true, got %q", m.Properties["field.email.indexed"])
	}
	if m.Properties["field.id.id"] != "true" {
		t.Errorf("field.id.id: want true, got %q", m.Properties["field.id.id"])
	}
}

func TestNoSQLModel_JPAEntityTableNotNoSQL(t *testing.T) {
	// JPA @Entity + @Table is a relational model, owned by the jpa/hibernate
	// model_extraction arm — this NoSQL pass must NOT claim it as Cassandra.
	src := `package com.example;
import javax.persistence.Entity;
import javax.persistence.Table;
@Entity
@Table(name="orders")
public class Order {
    private Long id;
}`
	ents := runJava(t, src)
	if m := nosqlModel(ents, "Order"); m != nil {
		t.Fatalf("JPA @Entity @Table class must NOT emit a NoSQL model, got store=%q", m.Properties["store"])
	}
}

func TestNoSQLModel_PlainClassNoModel(t *testing.T) {
	src := `package com.example;
public class PlainPojo {
    private String id;
    private String name;
}`
	ents := runJava(t, src)
	if m := nosqlModel(ents, "PlainPojo"); m != nil {
		t.Fatal("plain non-annotated class must NOT emit a SCOPE.Schema model entity")
	}
}
