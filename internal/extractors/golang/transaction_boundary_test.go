// transaction_boundary_test.go — value-asserting tests for #3628 transaction
// boundary stamping on Go function entities.
//
// A function that opens a database/sql or GORM transaction is stamped with
// Properties["transactional"]="true" (+ tx_source / tx_isolation). A function
// that merely RECEIVES a *sql.Tx but never opens one is NOT stamped (the
// no-transitive-propagation honesty boundary).
package golang

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractGoEntities(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path: "store.go", Language: "go", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func findGoFn(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func TestGoTransactionBoundary_BeginCommit(t *testing.T) {
	src := `package store
import "database/sql"
type Store struct { db *sql.DB }
func (s *Store) save() error {
	tx, err := s.db.Begin()
	if err != nil { return err }
	defer tx.Rollback()
	return tx.Commit()
}
func (s *Store) read() error {
	return s.db.QueryRow("SELECT 1").Err()
}`
	ents := extractGoEntities(t, src)

	save := findGoFn(ents, "Store.save")
	if save == nil {
		t.Fatal("Store.save entity not found")
	}
	if save.Properties["transactional"] != "true" {
		t.Errorf("Store.save transactional = %q, want true", save.Properties["transactional"])
	}
	if save.Properties["tx_source"] != "go_sql_begin" {
		t.Errorf("Store.save tx_source = %q, want go_sql_begin", save.Properties["tx_source"])
	}

	// Negative: a non-transactional method must have the property absent.
	read := findGoFn(ents, "Store.read")
	if read == nil {
		t.Fatal("Store.read entity not found")
	}
	if _, ok := read.Properties["transactional"]; ok {
		t.Errorf("Store.read should not be stamped transactional")
	}
}

func TestGoTransactionBoundary_BeginTxIsolation(t *testing.T) {
	src := `package store
import (
	"context"
	"database/sql"
)
func transfer(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	_ = tx
	return err
}`
	ents := extractGoEntities(t, src)
	fn := findGoFn(ents, "transfer")
	if fn == nil {
		t.Fatal("transfer entity not found")
	}
	if fn.Properties["transactional"] != "true" {
		t.Errorf("transfer transactional = %q, want true", fn.Properties["transactional"])
	}
	if fn.Properties["tx_isolation"] != "Serializable" {
		t.Errorf("transfer tx_isolation = %q, want Serializable", fn.Properties["tx_isolation"])
	}
}

func TestGoTransactionBoundary_ReceivesTxNotStamped(t *testing.T) {
	// Honesty boundary: insertRow receives a *sql.Tx and uses it but does not
	// open a transaction — it must NOT be stamped transactional.
	src := `package store
import "database/sql"
func insertRow(tx *sql.Tx, id int) error {
	_, err := tx.Exec("INSERT INTO t(id) VALUES (?)", id)
	return err
}`
	ents := extractGoEntities(t, src)
	fn := findGoFn(ents, "insertRow")
	if fn == nil {
		t.Fatal("insertRow entity not found")
	}
	if _, ok := fn.Properties["transactional"]; ok {
		t.Errorf("insertRow only receives tx, must not be stamped: %v", fn.Properties)
	}
}
