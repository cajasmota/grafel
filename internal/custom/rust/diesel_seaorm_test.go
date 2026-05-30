package rust_test

// diesel_seaorm_test.go — tests for custom_rust_diesel and custom_rust_seaorm
// extractors (issue #3269).
//
// Uses the fi/extract/containsEntity helpers from extractors_test.go.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Diesel — schema extraction (table! macro)
// ---------------------------------------------------------------------------

func TestDiesel_TableMacro(t *testing.T) {
	src := readFixture(t, "testdata/diesel_schema.rs")
	ents := extract(t, "custom_rust_diesel", fi("schema.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Component", "diesel:schema:users") {
		t.Error("expected diesel:schema:users from table! macro")
	}
	if !containsEntity(ents, "SCOPE.Component", "diesel:schema:posts") {
		t.Error("expected diesel:schema:posts from table! macro")
	}
}

// ---------------------------------------------------------------------------
// Diesel — model extraction (#[derive(Queryable/Insertable/...)])
// ---------------------------------------------------------------------------

func TestDiesel_QueryableModel(t *testing.T) {
	src := readFixture(t, "testdata/diesel_models.rs")
	ents := extract(t, "custom_rust_diesel", fi("models.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Component", "diesel:model:User") {
		t.Error("expected diesel:model:User (Queryable)")
	}
	if !containsEntity(ents, "SCOPE.Component", "diesel:model:NewUser") {
		t.Error("expected diesel:model:NewUser (Insertable)")
	}
	if !containsEntity(ents, "SCOPE.Component", "diesel:model:Post") {
		t.Error("expected diesel:model:Post (Queryable+Associations)")
	}
	if !containsEntity(ents, "SCOPE.Component", "diesel:model:UpdatePost") {
		t.Error("expected diesel:model:UpdatePost (AsChangeset)")
	}
}

func TestDiesel_QueryableModelInline(t *testing.T) {
	src := `
use diesel::prelude::*;

#[derive(Queryable)]
pub struct Product {
    pub id: i32,
    pub name: String,
}
`
	ents := extract(t, "custom_rust_diesel", fi("product.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "diesel:model:Product") {
		t.Error("expected diesel:model:Product")
	}
}

// ---------------------------------------------------------------------------
// Diesel — relationship extraction (joinable! + belongs_to)
// ---------------------------------------------------------------------------

func TestDiesel_JoinableMacro(t *testing.T) {
	src := readFixture(t, "testdata/diesel_schema.rs")
	ents := extract(t, "custom_rust_diesel", fi("schema.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Pattern", "diesel:joinable:posts->users") {
		t.Error("expected diesel:joinable:posts->users from joinable! macro")
	}
}

func TestDiesel_BelongsTo(t *testing.T) {
	src := readFixture(t, "testdata/diesel_models.rs")
	ents := extract(t, "custom_rust_diesel", fi("models.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Pattern", "diesel:belongs_to:User") {
		t.Error("expected diesel:belongs_to:User from #[belongs_to(User)]")
	}
}

func TestDiesel_BelongsToInline(t *testing.T) {
	src := `
use diesel::prelude::*;

#[derive(Queryable, Associations)]
#[belongs_to(Category, foreign_key = "category_id")]
pub struct Article {
    pub id: i32,
    pub category_id: i32,
}
`
	ents := extract(t, "custom_rust_diesel", fi("article.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "diesel:belongs_to:Category") {
		t.Error("expected diesel:belongs_to:Category")
	}
}

// ---------------------------------------------------------------------------
// Diesel — non-rust file is ignored
// ---------------------------------------------------------------------------

func TestDiesel_IgnoresNonRust(t *testing.T) {
	src := `table! { users (id) { id -> Integer, } }`
	ents := extract(t, "custom_rust_diesel", fi("schema.ts", "typescript", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-rust file, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// SeaORM — entity model extraction (#[derive(DeriveEntityModel)])
// ---------------------------------------------------------------------------

func TestSeaORM_EntityModel(t *testing.T) {
	src := readFixture(t, "testdata/seaorm_entity.rs")
	ents := extract(t, "custom_rust_seaorm", fi("user.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Component", "seaorm:model:users") {
		t.Error("expected seaorm:model:users (from table_name attribute)")
	}
}

func TestSeaORM_EntityModelInline(t *testing.T) {
	src := `
use sea_orm::entity::prelude::*;

#[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
#[sea_orm(table_name = "products")]
pub struct Model {
    #[sea_orm(primary_key)]
    pub id: i32,
    pub name: String,
}
`
	ents := extract(t, "custom_rust_seaorm", fi("product.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "seaorm:model:products") {
		t.Error("expected seaorm:model:products")
	}
}

// ---------------------------------------------------------------------------
// SeaORM — relationship extraction (DeriveRelation enum)
// ---------------------------------------------------------------------------

func TestSeaORM_RelationHasMany(t *testing.T) {
	src := readFixture(t, "testdata/seaorm_entity.rs")
	ents := extract(t, "custom_rust_seaorm", fi("user.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Pattern", "seaorm:relation:Relation:has_many:Entity") {
		t.Error("expected seaorm:relation:Relation:has_many:Entity")
	}
}

func TestSeaORM_RelationBelongsTo(t *testing.T) {
	src := `
use sea_orm::entity::prelude::*;

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(belongs_to = "super::user::Entity", from = "Column::UserId", to = "super::user::Column::Id")]
    User,
}
`
	ents := extract(t, "custom_rust_seaorm", fi("post.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "seaorm:relation:Relation:belongs_to:Entity") {
		t.Error("expected seaorm:relation:Relation:belongs_to:Entity")
	}
}

// ---------------------------------------------------------------------------
// SeaORM — migration extraction (impl MigrationTrait)
// ---------------------------------------------------------------------------

func TestSeaORM_Migration(t *testing.T) {
	src := readFixture(t, "testdata/seaorm_migration.rs")
	ents := extract(t, "custom_rust_seaorm", fi("migration.rs", "rust", src))

	if !containsEntity(ents, "SCOPE.Component", "seaorm:migration:Migration") {
		t.Error("expected seaorm:migration:Migration from impl MigrationTrait")
	}
}

func TestSeaORM_MigrationInline(t *testing.T) {
	src := `
use sea_orm_migration::prelude::*;

pub struct CreateUsersTable;

impl MigrationTrait for CreateUsersTable {
    fn name(&self) -> &str { "m20220101_create_users" }
}
`
	ents := extract(t, "custom_rust_seaorm", fi("mig.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "seaorm:migration:CreateUsersTable") {
		t.Error("expected seaorm:migration:CreateUsersTable")
	}
}

// ---------------------------------------------------------------------------
// SeaORM — non-rust file is ignored
// ---------------------------------------------------------------------------

func TestSeaORM_IgnoresNonRust(t *testing.T) {
	src := `
#[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
pub struct Model { pub id: i32 }
`
	ents := extract(t, "custom_rust_seaorm", fi("entity.py", "python", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-rust file, got %d", len(ents))
	}
}
