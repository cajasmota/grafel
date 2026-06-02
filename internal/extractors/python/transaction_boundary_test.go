// transaction_boundary_test.go — value-asserting tests for #3628 transaction
// boundary stamping on Python function/method entities.
package python

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractPyTx(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "svc.py", Language: "python", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return out
}

func findPyOp(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Operation" {
			return &ents[i]
		}
	}
	return nil
}

func TestPyTx_AtomicDecorator(t *testing.T) {
	src := `from django.db import transaction

@transaction.atomic
def checkout(request):
    order = Order.objects.create()
    return order
`
	ents := extractPyTx(t, src)
	fn := findPyOp(ents, "checkout")
	if fn == nil {
		t.Fatal("checkout entity not found")
	}
	if fn.Properties["transactional"] != "true" {
		t.Errorf("checkout transactional = %q, want true", fn.Properties["transactional"])
	}
	if fn.Properties["tx_source"] != "django_atomic" {
		t.Errorf("checkout tx_source = %q, want django_atomic", fn.Properties["tx_source"])
	}
}

func TestPyTx_AtomicBlock(t *testing.T) {
	src := `from django.db import transaction

def process(items):
    with transaction.atomic():
        for i in items:
            i.save()
`
	ents := extractPyTx(t, src)
	fn := findPyOp(ents, "process")
	if fn == nil {
		t.Fatal("process entity not found")
	}
	if fn.Properties["transactional"] != "true" {
		t.Errorf("process (with transaction.atomic()) transactional = %q, want true", fn.Properties["transactional"])
	}
}

func TestPyTx_SQLAlchemyBegin(t *testing.T) {
	src := `def commit_all(session):
    with session.begin():
        session.add(obj)
`
	ents := extractPyTx(t, src)
	fn := findPyOp(ents, "commit_all")
	if fn == nil {
		t.Fatal("commit_all entity not found")
	}
	if fn.Properties["transactional"] != "true" || fn.Properties["tx_source"] != "sqlalchemy_begin" {
		t.Errorf("commit_all not stamped sqlalchemy_begin: %v", fn.Properties)
	}
}

func TestPyTx_ReceivesSessionNotStamped(t *testing.T) {
	// Honesty boundary: a fn that only receives a session and writes through it
	// (no begin) must NOT be stamped.
	src := `def add_row(session, row):
    session.add(row)
    session.flush()
`
	ents := extractPyTx(t, src)
	fn := findPyOp(ents, "add_row")
	if fn == nil {
		t.Fatal("add_row entity not found")
	}
	if _, ok := fn.Properties["transactional"]; ok {
		t.Errorf("add_row only receives session, must not be stamped: %v", fn.Properties)
	}
}

func TestPyTx_PlainMethodNotStamped(t *testing.T) {
	src := `class Repo:
    def find(self, pk):
        return self.objects.get(pk=pk)
`
	ents := extractPyTx(t, src)
	fn := findPyOp(ents, "Repo.find")
	if fn == nil {
		t.Fatal("Repo.find entity not found")
	}
	if _, ok := fn.Properties["transactional"]; ok {
		t.Errorf("Repo.find should not be stamped: %v", fn.Properties)
	}
}
