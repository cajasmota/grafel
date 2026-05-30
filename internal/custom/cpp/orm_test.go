package cpp_test

// orm_test.go — tests for ODB, SOCI, and sqlpp11 C++ ORM extractors.

import (
	"testing"
)

// containsStr reports whether want appears in xs (local test helper).
func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// ============================================================================
// ODB
// ============================================================================

func TestODBModelExtraction(t *testing.T) {
	src := `
#pragma db object table("people")
class Person {
public:
	std::string name;
	int age;
};
`
	ents := extract(t, "custom_cpp_odb", fi("person.hxx", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "Person") {
		t.Fatal("expected SCOPE.Schema entity for Person")
	}
	// Value assertion: explicit table("people") mapping captured.
	ormProp(t, ents, "SCOPE.Schema", "", "Person", "table_name", "people")
	ormProp(t, ents, "SCOPE.Schema", "", "Person", "class_name", "Person")
}

func TestODBModelExtraction_DefaultTable(t *testing.T) {
	src := `
#pragma db object
class Account {};
`
	ents := extract(t, "custom_cpp_odb", fi("account.hxx", "cpp", src))
	// No explicit table() → ODB defaults the table name to the class name.
	ormProp(t, ents, "SCOPE.Schema", "", "Account", "table_name", "Account")
}

func TestODBMemberExtraction(t *testing.T) {
	src := `
#pragma db member(Person::name) column("person_name") type("VARCHAR(128)")
#pragma db member(Person::id) id
#pragma db member(Person::age)
`
	ents := extract(t, "custom_cpp_odb", fi("person.hxx", "cpp", src))
	// Value assertions: explicit column("…") + type("…") resolved, not raw text.
	ormProp(t, ents, "SCOPE.Schema", "column", "member:name", "column_name", "person_name")
	ormProp(t, ents, "SCOPE.Schema", "column", "member:name", "column_type", "VARCHAR(128)")
	ormProp(t, ents, "SCOPE.Schema", "column", "member:name", "field_name", "name")
	// PK marker from the `id` annotation.
	ormProp(t, ents, "SCOPE.Schema", "column", "member:id", "is_primary_key", "true")
	// Member with no column() → column name defaults to the field name.
	ormProp(t, ents, "SCOPE.Schema", "column", "member:age", "column_name", "age")
}

func TestODBRelationshipExtraction(t *testing.T) {
	src := `
#pragma db member(Employee::employer) one_to_many inverse(employees)
`
	ents := extract(t, "custom_cpp_odb", fi("employee.hxx", "cpp", src))
	// Value assertions: relationship kind + inverse target are specific.
	ormProp(t, ents, "SCOPE.Pattern", "relationship", "one_to_many:employer", "relationship_kind", "one_to_many")
	ormProp(t, ents, "SCOPE.Pattern", "relationship", "one_to_many:employer", "field_name", "employer")
	ormProp(t, ents, "SCOPE.Pattern", "relationship", "one_to_many:employer", "target_type", "employees")
}

func TestODBLazyPtrExtraction(t *testing.T) {
	src := `
odb::lazy_ptr<Employer> employer_;
odb::lazy_shared_ptr<Department> dept_;
`
	ents := extract(t, "custom_cpp_odb", fi("employee.hxx", "cpp", src))
	// Value assertions: each lazy_ptr names its specific target type.
	ormProp(t, ents, "SCOPE.Pattern", "relationship", "lazy_ptr:Employer", "target_type", "Employer")
	ormProp(t, ents, "SCOPE.Pattern", "relationship", "lazy_ptr:Employer", "relationship_kind", "lazy_ptr")
	ormProp(t, ents, "SCOPE.Pattern", "relationship", "lazy_ptr:Department", "target_type", "Department")
}

func TestODBQueryExtraction(t *testing.T) {
	src := `
odb::result<Person> r = db.query<Person>(odb::query<Person>::age > 18);
`
	ents := extract(t, "custom_cpp_odb", fi("main.cpp", "cpp", src))
	// Value assertion: query attributed to the specific model type.
	ormProp(t, ents, "SCOPE.Operation", "query", "query:Person", "model_type", "Person")
}

func TestODBNoMatch(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }
`
	ents := extract(t, "custom_cpp_odb", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestODBWrongLanguage(t *testing.T) {
	src := `#pragma db object
class Foo {};`
	// python language → should not match
	ents := extract(t, "custom_cpp_odb", fi("foo.hxx", "python", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ============================================================================
// SOCI
// ============================================================================

func TestSOCITypeConversionModel(t *testing.T) {
	src := `
template<> struct type_conversion<Person> {
	typedef values base_type;
	static void from_base(values const& v, indicator ind, Person& p) {
		p.name = v.get<string>("name");
	}
};
`
	ents := extract(t, "custom_cpp_soci", fi("person.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "Person") {
		t.Fatal("expected SCOPE.Schema entity for type_conversion<Person>")
	}
	ormProp(t, ents, "SCOPE.Schema", "", "Person", "class_name", "Person")
}

func TestSOCIIntoBinding(t *testing.T) {
	src := `
soci::session sql("postgresql://dbname=mydb");
int count;
sql << "SELECT COUNT(*) FROM persons", into(count);
`
	ents := extract(t, "custom_cpp_soci", fi("query.cpp", "cpp", src))
	// Value assertions: the bound variable + direction are specific.
	ormProp(t, ents, "SCOPE.Schema", "column", "binding:count", "variable", "count")
	ormProp(t, ents, "SCOPE.Schema", "column", "binding:count", "direction", "into")
}

func TestSOCIQueryExtraction(t *testing.T) {
	src := `
soci::session sql("sqlite3://mydb.db");
sql << "SELECT * FROM users WHERE id = :id", use(id), into(user);
sql << "INSERT INTO logs (msg) VALUES (:msg)", use(msg);
`
	ents := extract(t, "custom_cpp_soci", fi("query.cpp", "cpp", src))
	// Value assertions: each SQL string is classified by its specific verb.
	var verbs []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			verbs = append(verbs, e.Props["sql_verb"])
		}
	}
	if !containsStr(verbs, "SELECT") {
		t.Errorf("expected a SELECT query verb, got %v", verbs)
	}
	if !containsStr(verbs, "INSERT") {
		t.Errorf("expected an INSERT query verb, got %v", verbs)
	}
}

func TestSOCINoMatch(t *testing.T) {
	src := `#include <vector>
void foo() { std::vector<int> v; }`
	ents := extract(t, "custom_cpp_soci", fi("foo.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ============================================================================
// sqlpp11
// ============================================================================

func TestSQLPP11TableExtraction(t *testing.T) {
	src := `
struct TabPerson : sqlpp::table<TabPerson, TabPerson::Id, TabPerson::Name> {
	struct id_ {
		struct _alias_t { static constexpr const char _literal[] = "id"; };
		using _traits = sqlpp::make_traits<sqlpp::integer, sqlpp::tag::must_not_insert>;
	};
	struct name_ {
		struct _alias_t { static constexpr const char _literal[] = "name"; };
		using _traits = sqlpp::make_traits<sqlpp::varchar>;
	};
	id_   id;
	name_ name;
};
`
	ents := extract(t, "custom_cpp_sqlpp11", fi("tables.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Schema", "TabPerson") {
		t.Fatal("expected SCOPE.Schema entity for TabPerson")
	}
	ormProp(t, ents, "SCOPE.Schema", "", "TabPerson", "class_name", "TabPerson")
	// Value assertions: column structs (id_, name_) resolve to specific columns
	// parented to the table.
	ormProp(t, ents, "SCOPE.Schema", "column", "TabPerson.id", "parent_table", "TabPerson")
	ormProp(t, ents, "SCOPE.Schema", "column", "TabPerson.id", "col_struct", "id_")
	ormProp(t, ents, "SCOPE.Schema", "column", "TabPerson.name", "parent_table", "TabPerson")
}

func TestSQLPP11AliasExtraction(t *testing.T) {
	src := `SQLPP_ALIAS_PROVIDER(left)
SQLPP_ALIAS_PROVIDER(right)
`
	ents := extract(t, "custom_cpp_sqlpp11", fi("tables.h", "cpp", src))
	// Value assertions: each alias is captured by its specific name.
	ormProp(t, ents, "SCOPE.Schema", "alias", "alias:left", "alias_name", "left")
	ormProp(t, ents, "SCOPE.Schema", "alias", "alias:right", "alias_name", "right")
}

func TestSQLPP11QueryExtraction(t *testing.T) {
	src := `
auto result = db(select(tab.id, tab.name).from(tab).where(tab.id == id));
db(insert_into(tab).set(tab.name = "alice"));
db(update(tab).set(tab.name = "bob").where(tab.id == 1));
db(remove_from(tab).where(tab.id == 2));
`
	ents := extract(t, "custom_cpp_sqlpp11", fi("queries.cpp", "cpp", src))
	// Value assertions: every DSL verb is classified specifically.
	var verbs []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			verbs = append(verbs, e.Props["sql_verb"])
		}
	}
	for _, want := range []string{"SELECT", "INSERT_INTO", "UPDATE", "REMOVE_FROM"} {
		if !containsStr(verbs, want) {
			t.Errorf("expected %s query verb, got %v", want, verbs)
		}
	}
}

func TestSQLPP11NoMatch(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }
`
	ents := extract(t, "custom_cpp_sqlpp11", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-sqlpp11 file, got %d", len(ents))
	}
}
