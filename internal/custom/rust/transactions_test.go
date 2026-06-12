package rust_test

import "testing"

// Helper: find an entity by kind+name and return its props.
func txEntity(ents []entitySummary, name string) (entitySummary, bool) {
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "transaction_boundary" && e.Name == name {
			return e, true
		}
	}
	return entitySummary{}, false
}

func TestRustTx_DieselClosure(t *testing.T) {
	src := `
use diesel::prelude::*;

fn transfer_funds(conn: &mut PgConnection) -> QueryResult<()> {
    conn.transaction(|c| {
        diesel::update(accounts).execute(c)?;
        Ok(())
    })
}
`
	ents := extract(t, "custom_rust_transactions", fi("svc.rs", "rust", src))
	e, ok := txEntity(ents, "transfer_funds.transaction")
	if !ok {
		t.Fatalf("expected transfer_funds.transaction boundary; got %+v", ents)
	}
	if e.Props["framework"] != "diesel" {
		t.Errorf("framework = %q, want diesel", e.Props["framework"])
	}
	if e.Props["transactional"] != "true" {
		t.Errorf("transactional = %q, want true", e.Props["transactional"])
	}
	if e.Props["function"] != "transfer_funds" {
		t.Errorf("function = %q, want transfer_funds", e.Props["function"])
	}
	if e.Props["db_handle"] != "conn" {
		t.Errorf("db_handle = %q, want conn", e.Props["db_handle"])
	}
}

func TestRustTx_SqlxBegin(t *testing.T) {
	src := `
use sqlx::PgPool;

async fn create_order(pool: &PgPool) -> Result<(), sqlx::Error> {
    let mut tx = pool.begin().await?;
    sqlx::query("INSERT INTO orders ...").execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}
`
	ents := extract(t, "custom_rust_transactions", fi("orders.rs", "rust", src))
	e, ok := txEntity(ents, "create_order.transaction")
	if !ok {
		t.Fatalf("expected create_order.transaction boundary; got %+v", ents)
	}
	if e.Props["framework"] != "sqlx" {
		t.Errorf("framework = %q, want sqlx", e.Props["framework"])
	}
	if e.Props["function"] != "create_order" {
		t.Errorf("function = %q, want create_order", e.Props["function"])
	}
	if e.Props["transaction_api"] != "sqlx_begin" {
		t.Errorf("transaction_api = %q, want sqlx_begin", e.Props["transaction_api"])
	}
}

func TestRustTx_SeaOrmBegin(t *testing.T) {
	src := `
use sea_orm::{DatabaseConnection, TransactionTrait};

async fn book_seat(db: &DatabaseConnection) -> Result<(), DbErr> {
    let txn = db.begin().await?;
    seat::Entity::update(active).exec(&txn).await?;
    txn.commit().await?;
    Ok(())
}
`
	ents := extract(t, "custom_rust_transactions", fi("booking.rs", "rust", src))
	e, ok := txEntity(ents, "book_seat.transaction")
	if !ok {
		t.Fatalf("expected book_seat.transaction boundary; got %+v", ents)
	}
	if e.Props["framework"] != "sea_orm" {
		t.Errorf("framework = %q, want sea_orm", e.Props["framework"])
	}
	if e.Props["transaction_api"] != "sea_orm_begin" {
		t.Errorf("transaction_api = %q, want sea_orm_begin", e.Props["transaction_api"])
	}
}

func TestRustTx_SeaOrmClosure(t *testing.T) {
	src := `
use sea_orm::{DatabaseConnection, TransactionTrait};

async fn batch_update(db: &DatabaseConnection) -> Result<(), DbErr> {
    db.transaction(|txn| Box::pin(async move {
        item::Entity::delete_many().exec(txn).await?;
        Ok(())
    })).await
}
`
	ents := extract(t, "custom_rust_transactions", fi("batch.rs", "rust", src))
	e, ok := txEntity(ents, "batch_update.transaction")
	if !ok {
		t.Fatalf("expected batch_update.transaction boundary; got %+v", ents)
	}
	if e.Props["framework"] != "sea_orm" {
		t.Errorf("framework = %q, want sea_orm", e.Props["framework"])
	}
	if e.Props["transaction_api"] != "sea_orm_transaction_closure" {
		t.Errorf("transaction_api = %q, want sea_orm_transaction_closure", e.Props["transaction_api"])
	}
}

func TestRustTx_Rusqlite(t *testing.T) {
	src := `
use rusqlite::Connection;

fn migrate(conn: &mut Connection) -> rusqlite::Result<()> {
    let tx = conn.transaction()?;
    tx.execute("CREATE TABLE t (id INTEGER)", [])?;
    tx.commit()?;
    Ok(())
}
`
	ents := extract(t, "custom_rust_transactions", fi("migrate.rs", "rust", src))
	e, ok := txEntity(ents, "migrate.transaction")
	if !ok {
		t.Fatalf("expected migrate.transaction boundary; got %+v", ents)
	}
	if e.Props["framework"] != "rusqlite" {
		t.Errorf("framework = %q, want rusqlite", e.Props["framework"])
	}
	if e.Props["transaction_api"] != "rusqlite_transaction" {
		t.Errorf("transaction_api = %q, want rusqlite_transaction", e.Props["transaction_api"])
	}
	if e.Props["function"] != "migrate" {
		t.Errorf("function = %q, want migrate", e.Props["function"])
	}
}

// Honesty guard: a bare commit() with no sqlx/sea_orm import context must NOT
// produce a transaction boundary (avoids false positives on unrelated .commit()).
func TestRustTx_NoFalsePositiveOnBareCommit(t *testing.T) {
	src := `
fn finish(buf: &mut Builder) {
    buf.commit();
}
`
	ents := extract(t, "custom_rust_transactions", fi("buf.rs", "rust", src))
	if len(ents) != 0 {
		t.Fatalf("expected no tx boundaries for bare commit(); got %+v", ents)
	}
}

// Fallback shape: a transaction opener at module scope (no enclosing fn) stamps
// the receiver-based boundary name, mirroring the Crystal/Nim shape.
func TestRustTx_ReceiverFallback(t *testing.T) {
	src := `use diesel::prelude::*;
let _ = conn.transaction(|c| { Ok(()) });
`
	ents := extract(t, "custom_rust_transactions", fi("top.rs", "rust", src))
	if _, ok := txEntity(ents, "conn.transaction"); !ok {
		t.Fatalf("expected conn.transaction fallback boundary; got %+v", ents)
	}
}
