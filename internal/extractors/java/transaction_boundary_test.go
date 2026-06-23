// transaction_boundary_test.go — value-asserting tests for #3628 transaction
// boundary stamping on Java method entities (@Transactional, Spring + JTA).
package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractJavaTx(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "Service.java", Content: []byte(src), Language: "java", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

func findJavaOpTx(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Operation" {
			return &ents[i]
		}
	}
	return nil
}

func TestJavaTx_MethodTransactionalWithPropagation(t *testing.T) {
	src := `package com.example;
public class AccountService {
    @Transactional(propagation = Propagation.REQUIRES_NEW, isolation = Isolation.SERIALIZABLE, readOnly = false)
    public void transfer(Long from, Long to, BigDecimal amt) {
        debit(from, amt);
        credit(to, amt);
    }
    public BigDecimal balance(Long id) {
        return repo.balanceOf(id);
    }
}`
	ents := extractJavaTx(t, src)
	tr := findJavaOpTx(ents, "AccountService.transfer")
	if tr == nil {
		t.Fatal("AccountService.transfer not found")
	}
	if tr.Properties["transactional"] != "true" {
		t.Errorf("transfer transactional = %q, want true", tr.Properties["transactional"])
	}
	if tr.Properties["tx_propagation"] != "REQUIRES_NEW" {
		t.Errorf("transfer tx_propagation = %q, want REQUIRES_NEW", tr.Properties["tx_propagation"])
	}
	if tr.Properties["tx_isolation"] != "SERIALIZABLE" {
		t.Errorf("transfer tx_isolation = %q, want SERIALIZABLE", tr.Properties["tx_isolation"])
	}
	if tr.Properties["tx_read_only"] != "false" {
		t.Errorf("transfer tx_read_only = %q, want false", tr.Properties["tx_read_only"])
	}

	// Negative: a plain method must not be stamped.
	bal := findJavaOpTx(ents, "AccountService.balance")
	if bal == nil {
		t.Fatal("AccountService.balance not found")
	}
	if _, ok := bal.Properties["transactional"]; ok {
		t.Errorf("balance should not be stamped transactional")
	}
}

func TestJavaTx_ClassLevelPropagatesToMethods(t *testing.T) {
	src := `package com.example;
@Transactional
public class OrderService {
    public void place(Order o) { repo.save(o); }
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void audit(Order o) { auditRepo.save(o); }
}`
	ents := extractJavaTx(t, src)

	// Class-level @Transactional → place() inherits transactional=true.
	place := findJavaOpTx(ents, "OrderService.place")
	if place == nil {
		t.Fatal("OrderService.place not found")
	}
	if place.Properties["transactional"] != "true" {
		t.Errorf("place should inherit class-level transactional, got %v", place.Properties)
	}

	// Method-level annotation wins on specificity (own propagation kept).
	audit := findJavaOpTx(ents, "OrderService.audit")
	if audit == nil {
		t.Fatal("OrderService.audit not found")
	}
	if audit.Properties["tx_propagation"] != "REQUIRES_NEW" {
		t.Errorf("audit should keep its own REQUIRES_NEW, got %q", audit.Properties["tx_propagation"])
	}
}

func TestJavaTx_JtaTxType(t *testing.T) {
	src := `package com.example;
public class Repo {
    @Transactional(Transactional.TxType.MANDATORY)
    public void persist(Entity e) { em.persist(e); }
}`
	ents := extractJavaTx(t, src)
	fn := findJavaOpTx(ents, "Repo.persist")
	if fn == nil {
		t.Fatal("Repo.persist not found")
	}
	if fn.Properties["transactional"] != "true" || fn.Properties["tx_propagation"] != "MANDATORY" {
		t.Errorf("persist JTA TxType not captured: %v", fn.Properties)
	}
}
