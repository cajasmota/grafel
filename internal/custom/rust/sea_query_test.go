package rust_test

// sea_query_test.go — value-asserting tests for the custom_rust_sea_query
// extractor. Each test asserts the SPECIFIC table / column literal extracted
// from a SeaQuery statement, not merely that some entity was produced.

import "testing"

// entityProp returns the value of property `key` on the first entity matching
// (kind, name), and whether such an entity was found.
func entityProp(ents []entitySummary, kind, name, key string) (string, bool) {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return e.Props[key], true
		}
	}
	return "", false
}

func TestSeaQuery_SelectFromTable(t *testing.T) {
	src := `
use sea_query::{Query, Iden};

fn devices_query() -> sea_query::SelectStatement {
    Query::select()
        .columns([MDevices::Id, MDevices::Name])
        .from(MDevices)
        .to_owned()
}
`
	ents := extract(t, "custom_rust_sea_query", fi("query.rs", "rust", src))
	tbl, ok := entityProp(ents, "SCOPE.Pattern", "sea_query:query:select:MDevices", "table_name")
	if !ok {
		t.Fatal("expected sea_query:query:select:MDevices pattern")
	}
	if tbl != "MDevices" {
		t.Errorf("expected table_name=MDevices, got %q", tbl)
	}
	if kind, _ := entityProp(ents, "SCOPE.Pattern", "sea_query:query:select:MDevices", "statement_kind"); kind != "select" {
		t.Errorf("expected statement_kind=select, got %q", kind)
	}
	// Columns must be extracted as schema_column entities with the literal name.
	if !containsEntity(ents, "SCOPE.Component", "sea_query:column:MDevices.Id") {
		t.Error("expected schema_column sea_query:column:MDevices.Id")
	}
	if col, _ := entityProp(ents, "SCOPE.Component", "sea_query:column:MDevices.Name", "column_name"); col != "Name" {
		t.Errorf("expected column_name=Name, got %q", col)
	}
}

func TestSeaQuery_InsertIntoTable(t *testing.T) {
	src := `
let q = Query::insert()
    .into_table(Inspections)
    .columns([Inspections::Id, Inspections::Status])
    .to_owned();
`
	ents := extract(t, "custom_rust_sea_query", fi("insert.rs", "rust", src))
	tbl, ok := entityProp(ents, "SCOPE.Pattern", "sea_query:query:insert:Inspections", "table_name")
	if !ok {
		t.Fatal("expected sea_query:query:insert:Inspections pattern")
	}
	if tbl != "Inspections" {
		t.Errorf("expected table_name=Inspections, got %q", tbl)
	}
	if !containsEntity(ents, "SCOPE.Component", "sea_query:column:Inspections.Status") {
		t.Error("expected schema_column sea_query:column:Inspections.Status")
	}
}

func TestSeaQuery_UpdateTable(t *testing.T) {
	src := `
let q = Query::update()
    .table(Users)
    .value(Users::Name, "x")
    .to_owned();
`
	ents := extract(t, "custom_rust_sea_query", fi("update.rs", "rust", src))
	tbl, ok := entityProp(ents, "SCOPE.Pattern", "sea_query:query:update:Users", "table_name")
	if !ok {
		t.Fatal("expected sea_query:query:update:Users pattern")
	}
	if tbl != "Users" {
		t.Errorf("expected table_name=Users, got %q", tbl)
	}
}

func TestSeaQuery_DeleteFromTable(t *testing.T) {
	src := `
let q = Query::delete()
    .from_table(Sessions)
    .to_owned();
`
	ents := extract(t, "custom_rust_sea_query", fi("delete.rs", "rust", src))
	tbl, ok := entityProp(ents, "SCOPE.Pattern", "sea_query:query:delete:Sessions", "table_name")
	if !ok {
		t.Fatal("expected sea_query:query:delete:Sessions pattern")
	}
	if tbl != "Sessions" {
		t.Errorf("expected table_name=Sessions, got %q", tbl)
	}
}

func TestSeaQuery_IdenDeriveEnum(t *testing.T) {
	src := `
#[derive(Iden)]
enum MDevices {
    Table,
    Id,
    Name,
}
`
	ents := extract(t, "custom_rust_sea_query", fi("iden.rs", "rust", src))
	tbl, ok := entityProp(ents, "SCOPE.Component", "sea_query:iden:MDevices", "iden_enum")
	if !ok {
		t.Fatal("expected sea_query:iden:MDevices orm_model")
	}
	if tbl != "MDevices" {
		t.Errorf("expected iden_enum=MDevices, got %q", tbl)
	}
}
