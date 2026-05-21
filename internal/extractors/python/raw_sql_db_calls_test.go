package python_test

// Issue #1414: Python raw SQL DB call edge extraction.
//
// Verifies that the raw SQL scanner emits:
//   - cursor.execute("CALL proc_name(...)") → enclosing function CALLS proc_name
//   - cursor.execute("SELECT ... FROM view_name") → enclosing function READS_FROM view_name
//
// These edges bridge the Python application layer (app/db.py, consumer.py)
// to the SQL schema layer (procedures, views defined in migration files).

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractPyContent1414(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return entities
}

func findPyEntityByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

func hasEdge(rels []types.RelationshipRecord, kind, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

// Test1414_PyCallsProcedure verifies that cursor.execute("CALL mark_order_paid(...)")
// produces a CALLS edge from the enclosing Python function to mark_order_paid.
func Test1414_PyCallsProcedure(t *testing.T) {
	src := `
import psycopg2

def mark_order_paid(conn, order_id, amount):
    cursor = conn.cursor()
    cursor.execute("CALL mark_order_paid(%s, %s)", (order_id, amount))
    conn.commit()
`
	entities := extractPyContent1414(t, src, "app/db.py")
	fn := findPyEntityByName(entities, "mark_order_paid")
	if fn == nil {
		t.Fatal("[CALLS] expected function mark_order_paid to be extracted")
	}
	if !hasEdge(fn.Relationships, "CALLS", "mark_order_paid") {
		t.Errorf("[CALLS] expected mark_order_paid CALLS mark_order_paid (procedure), got %v", fn.Relationships)
	}
}

// Test1414_PyReadsFromView verifies that cursor.execute("SELECT ... FROM order_summary")
// produces a READS_FROM edge from the enclosing function to order_summary.
func Test1414_PyReadsFromView(t *testing.T) {
	src := `
import psycopg2

def get_user_summary(conn, user_id):
    cursor = conn.cursor()
    cursor.execute("SELECT id, user_id, status FROM order_summary WHERE user_id = %s", (user_id,))
    return cursor.fetchall()
`
	entities := extractPyContent1414(t, src, "app/db.py")
	fn := findPyEntityByName(entities, "get_user_summary")
	if fn == nil {
		t.Fatal("[READS_FROM] expected function get_user_summary to be extracted")
	}
	if !hasEdge(fn.Relationships, "READS_FROM", "order_summary") {
		t.Errorf("[READS_FROM] expected get_user_summary READS_FROM order_summary, got %v", fn.Relationships)
	}
}

// Test1414_PyNoFalsePositivesOnPlainExecute ensures that cursor.execute() calls
// without SQL CALL or SELECT ... FROM do NOT emit spurious edges.
func Test1414_PyNoFalsePositivesOnPlainExecute(t *testing.T) {
	src := `
def create_table(conn):
    cursor = conn.cursor()
    cursor.execute("CREATE TABLE IF NOT EXISTS foo (id INT)")
    conn.commit()
`
	entities := extractPyContent1414(t, src, "app/schema.py")
	fn := findPyEntityByName(entities, "create_table")
	if fn == nil {
		return // function not emitted — test moot
	}
	for _, r := range fn.Relationships {
		if (r.Kind == "CALLS" || r.Kind == "READS_FROM") && r.Properties["raw_sql"] == "true" {
			t.Errorf("unexpected raw SQL edge from create_table: %+v", r)
		}
	}
}

// Test1414_PyConsumerCallsProc verifies that a consumer.py pattern calling
// the procedure on payments.settled event also emits the CALLS edge.
func Test1414_PyConsumerCallsProc(t *testing.T) {
	src := `
import psycopg2

def on_payment_settled(conn, event):
    order_id = event["order_id"]
    amount = event["amount"]
    conn.execute("CALL mark_order_paid(%s, %s)" % (order_id, amount))
`
	entities := extractPyContent1414(t, src, "consumer.py")
	fn := findPyEntityByName(entities, "on_payment_settled")
	if fn == nil {
		t.Fatal("expected function on_payment_settled to be extracted")
	}
	if !hasEdge(fn.Relationships, "CALLS", "mark_order_paid") {
		t.Errorf("expected on_payment_settled CALLS mark_order_paid (procedure), got %v", fn.Relationships)
	}
}
