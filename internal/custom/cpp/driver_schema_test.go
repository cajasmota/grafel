package cpp_test

// driver_schema_test.go — tests for the C++ raw database driver schema extractor.

import (
	"testing"
)

func TestCppDriverLibpqxx_CreateTable(t *testing.T) {
	src := `#include <pqxx/pqxx>
#include <string>

void setup(pqxx::connection& conn) {
	pqxx::work tx(conn);
	tx.exec("CREATE TABLE users (id SERIAL PRIMARY KEY, name VARCHAR(255) NOT NULL, age INT)");
	tx.commit();
}
`
	ents := extract(t, "custom_cpp_driver_schema", fi("setup.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Fatal("expected SCOPE.Schema entity for users table")
	}
	ormProp(t, ents, "SCOPE.Schema", "", "users", "table_name", "users")
	// Value assertions: specific columns + parsed SQL types.
	ormProp(t, ents, "SCOPE.Schema", "column", "users.id", "column_type", "SERIAL")
	ormProp(t, ents, "SCOPE.Schema", "column", "users.id", "parent_table", "users")
	ormProp(t, ents, "SCOPE.Schema", "column", "users.name", "column_type", "VARCHAR")
	ormProp(t, ents, "SCOPE.Schema", "column", "users.age", "column_type", "INT")
}

func TestCppDriverLibpqxx_QueryAttribution(t *testing.T) {
	src := `#include <pqxx/pqxx>

void query(pqxx::connection& conn, int id) {
	pqxx::work tx(conn);
	pqxx::result r = tx.exec("SELECT id, name FROM users WHERE id = " + std::to_string(id));
	tx.exec("INSERT INTO logs (msg) VALUES ('queried')");
	tx.commit();
}
`
	ents := extract(t, "custom_cpp_driver_schema", fi("query.cpp", "cpp", src))
	// Value assertions: SQL verbs classified specifically from exec() literals.
	var verbs []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			verbs = append(verbs, e.Props["sql_verb"])
		}
	}
	if !containsStr(verbs, "SELECT") {
		t.Errorf("expected SELECT verb, got %v", verbs)
	}
	if !containsStr(verbs, "INSERT") {
		t.Errorf("expected INSERT verb, got %v", verbs)
	}
}

func TestCppDriverMongocxx_Collection(t *testing.T) {
	src := `#include <mongocxx/client.hpp>
#include <mongocxx/instance.hpp>

void save(mongocxx::client& client) {
	auto db = client["mydb"];
	auto users = db["users"];
	auto logs = db.collection("audit_logs");
}
`
	ents := extract(t, "custom_cpp_driver_schema", fi("mongo.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Fatal("expected SCOPE.Schema entity for users collection")
	}
	ormProp(t, ents, "SCOPE.Schema", "", "users", "collection_name", "users")
	ormProp(t, ents, "SCOPE.Schema", "", "audit_logs", "collection_name", "audit_logs")
}

func TestCppDriverMongocxx_QueryAttribution(t *testing.T) {
	src := `#include <mongocxx/client.hpp>

void ops(mongocxx::client& client) {
	auto db = client["mydb"];
	auto users = db["users"];
	users.insert_one(make_document(kvp("name", "Alice")));
	auto cursor = users.find(make_document());
	users.update_one(filter, update);
	users.delete_many(filter);
	auto pipe = users.aggregate(pipeline);
}
`
	ents := extract(t, "custom_cpp_driver_schema", fi("ops.cpp", "cpp", src))
	var verbs []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			verbs = append(verbs, e.Props["mongo_verb"])
		}
	}
	for _, want := range []string{"INSERT", "FIND", "UPDATE", "DELETE", "AGGREGATE"} {
		if !containsStr(verbs, want) {
			t.Errorf("expected mongo verb %s, got %v", want, verbs)
		}
	}
}

func TestCppDriverMysql_Execute(t *testing.T) {
	src := `#include <mysql/mysql.h>
#include <string>

void setup(MYSQL* conn) {
	mysql_query(conn, "CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, total DECIMAL(10,2))");
}

void doQuery(MYSQL* conn) {
	mysql_query(conn, "SELECT * FROM orders WHERE id = 1");
}
`
	// mysql_query is a free function (not a .exec method); the free-function
	// regex picks up its SQL string literal.
	ents := extract(t, "custom_cpp_driver_schema", fi("mysql.cpp", "cpp", src))
	// CREATE TABLE → orders table + columns.
	ormProp(t, ents, "SCOPE.Schema", "", "orders", "table_name", "orders")
	ormProp(t, ents, "SCOPE.Schema", "column", "orders.id", "column_type", "INT")
	ormProp(t, ents, "SCOPE.Schema", "column", "orders.total", "column_type", "DECIMAL")
	// SELECT verb attributed from the second mysql_query.
	var verbs []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			verbs = append(verbs, e.Props["sql_verb"])
		}
	}
	if !containsStr(verbs, "SELECT") {
		t.Errorf("expected SELECT verb from mysql_query, got %v", verbs)
	}
	if !containsStr(verbs, "CREATE") {
		t.Errorf("expected CREATE verb from mysql_query, got %v", verbs)
	}
}

func TestCppDriverNoMatch_WrongLanguage(t *testing.T) {
	src := `#include <pqxx/pqxx>
class Foo {};`
	ents := extract(t, "custom_cpp_driver_schema", fi("foo.cpp", "python", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

func TestCppDriverNoMatch_NoInclude(t *testing.T) {
	src := `#include <iostream>
int main() {
	return 0;
}
`
	ents := extract(t, "custom_cpp_driver_schema", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("without driver include, expected no entities, got %d", len(ents))
	}
}
