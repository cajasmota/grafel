package java

import "testing"

// transactional_3863_test.go — value-asserting tests for #3863 (epic #3854):
// non-Spring JVM transaction crediting + net-new programmatic boundary/rollback
// and the Jakarta/JTA rollbackOn / dontRollbackOn spelling.

// TestTransactional_Dropwizard_3863 proves the Dropwizard framework (now in
// txFrameworks) fires the @Transactional annotation surface. Dropwizard-Hibernate
// services carry Jakarta/JTA @Transactional.
// Registry target: lang.java.framework.dropwizard Transactions/* missing -> full.
func TestTransactional_Dropwizard_3863(t *testing.T) {
	src := `package com.example.dw;
import jakarta.transaction.Transactional;
public class AccountResource {
    @Transactional(rollbackOn = PersistenceException.class)
    public void transfer(Long from, Long to) { accountDao.save(from); accountDao.save(to); }

    public Account lookup(Long id) { return accountDao.findById(id); }
}`
	r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: "dropwizard", FilePath: "AccountResource.java"})

	tr, ok := txEntityByName(r, "AccountResource.transfer")
	if !ok {
		t.Fatalf("[#3863 dropwizard] expected AccountResource.transfer boundary; got %v", entityNames(r.Entities))
	}
	if tr.Properties["transaction_boundary"] != "method" {
		t.Errorf("[#3863 dropwizard] transfer transaction_boundary = %v, want method", tr.Properties["transaction_boundary"])
	}
	if tr.Properties["framework"] != "dropwizard" {
		t.Errorf("[#3863 dropwizard] framework = %v, want dropwizard", tr.Properties["framework"])
	}
	// rollbackOn (JTA spelling) folds into rollback_for.
	if tr.Properties["rollback_for"] != "PersistenceException" {
		t.Errorf("[#3863 dropwizard rollback] transfer rollback_for = %v, want PersistenceException", tr.Properties["rollback_for"])
	}
	// Negative: the non-transactional read must not be stamped a boundary.
	if _, ok := txEntityByName(r, "AccountResource.lookup"); ok {
		t.Errorf("[#3863 dropwizard] lookup (non-@Transactional read) must NOT be a boundary")
	}
}

// TestTransactional_DGS_3863 proves Netflix DGS resolver methods carrying Spring
// @Transactional are credited (dgs added to txFrameworks).
// Registry target: lang.java.framework.dgs Transactions/* missing -> full.
func TestTransactional_DGS_3863(t *testing.T) {
	src := `package com.example.dgs;
import com.netflix.graphql.dgs.DgsComponent;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.annotation.Propagation;
public class OrderFetcher {
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public Order placeOrder(String id) { return orderRepo.save(id); }
}`
	r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: "dgs", FilePath: "OrderFetcher.java"})
	pl, ok := txEntityByName(r, "OrderFetcher.placeOrder")
	if !ok {
		t.Fatalf("[#3863 dgs] expected OrderFetcher.placeOrder boundary; got %v", entityNames(r.Entities))
	}
	if pl.Properties["framework"] != "dgs" {
		t.Errorf("[#3863 dgs] framework = %v, want dgs", pl.Properties["framework"])
	}
	if pl.Properties["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[#3863 dgs] placeOrder propagation = %v, want REQUIRES_NEW", pl.Properties["propagation"])
	}
}

// TestTransactional_SpringGraphQL_3863 proves Spring-for-GraphQL resolver
// methods carrying Spring @Transactional are credited (spring_graphql added to
// txFrameworks). Registry target: lang.java.framework.spring-graphql.
func TestTransactional_SpringGraphQL_3863(t *testing.T) {
	src := `package com.example.gql;
import org.springframework.graphql.data.method.annotation.MutationMapping;
import org.springframework.transaction.annotation.Transactional;
public class OrderController {
    @MutationMapping
    @Transactional(rollbackFor = OrderException.class)
    public Order createOrder(String input) { return orderRepo.save(input); }
}`
	r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: "spring_graphql", FilePath: "OrderController.java"})
	co, ok := txEntityByName(r, "OrderController.createOrder")
	if !ok {
		t.Fatalf("[#3863 spring-graphql] expected OrderController.createOrder boundary; got %v", entityNames(r.Entities))
	}
	if co.Properties["framework"] != "spring_graphql" {
		t.Errorf("[#3863 spring-graphql] framework = %v, want spring_graphql", co.Properties["framework"])
	}
	if co.Properties["rollback_for"] != "OrderException" {
		t.Errorf("[#3863 spring-graphql] createOrder rollback_for = %v, want OrderException", co.Properties["rollback_for"])
	}
}

// TestTransactional_JTARollbackOn_3863 proves the Jakarta/JTA rollbackOn /
// dontRollbackOn attribute spelling is captured (folded into rollback_for /
// no_rollback_for, same as Spring rollbackFor / noRollbackFor).
func TestTransactional_JTARollbackOn_3863(t *testing.T) {
	src := `package com.example.q;
import jakarta.transaction.Transactional;
public class InventoryService {
    @Transactional(rollbackOn = {IOException.class, SQLException.class}, dontRollbackOn = ValidationException.class)
    public void restock(Long sku, int qty) { inventory.persist(sku); }
}`
	r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: "quarkus", FilePath: "InventoryService.java"})
	rs, ok := txEntityByName(r, "InventoryService.restock")
	if !ok {
		t.Fatalf("[#3863 jta-rollbackOn] expected InventoryService.restock; got %v", entityNames(r.Entities))
	}
	if rs.Properties["rollback_for"] != "IOException, SQLException" {
		t.Errorf("[#3863 jta-rollbackOn] restock rollback_for = %v, want 'IOException, SQLException'", rs.Properties["rollback_for"])
	}
	if rs.Properties["no_rollback_for"] != "ValidationException" {
		t.Errorf("[#3863 jta-dontRollbackOn] restock no_rollback_for = %v, want ValidationException", rs.Properties["no_rollback_for"])
	}
}

// TestTransactional_Programmatic_3863 proves the NET-NEW programmatic transaction
// boundary + rollback detection: UserTransaction.begin()/commit()/rollback(),
// Hibernate session.beginTransaction(), JPA em.getTransaction().begin(), and
// setRollbackOnly(). Runs for the wider JVM-backend framework set (here: vertx,
// which does NOT use @Transactional).
func TestTransactional_Programmatic_3863(t *testing.T) {
	src := `package com.example.vx;
public class TransferHandler {
    public void runJta() {
        userTransaction.begin();
        try {
            em.persist(a);
            em.persist(b);
            userTransaction.commit();
        } catch (Exception e) {
            userTransaction.rollback();
        }
    }
    public void runHibernate() {
        Transaction tx = session.beginTransaction();
        session.save(o);
        tx.commit();
    }
    public void markOnly() {
        context.setRollbackOnly();
    }
    public Account read(Long id) {
        return repository.findById(id);
    }
}`
	r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: "vertx", FilePath: "TransferHandler.java"})

	// JTA UserTransaction: boundary + programmatic rollback.
	jta, ok := txEntityByName(r, "TransferHandler.runJta")
	if !ok {
		t.Fatalf("[#3863 prog-jta] expected TransferHandler.runJta boundary; got %v", entityNames(r.Entities))
	}
	if jta.Properties["transaction_boundary"] != "programmatic" {
		t.Errorf("[#3863 prog-jta] runJta transaction_boundary = %v, want programmatic", jta.Properties["transaction_boundary"])
	}
	if jta.Properties["tx_api"] != "jta_user_transaction" {
		t.Errorf("[#3863 prog-jta] runJta tx_api = %v, want jta_user_transaction", jta.Properties["tx_api"])
	}
	if jta.Properties["rollback"] != "programmatic" {
		t.Errorf("[#3863 prog-jta] runJta should carry programmatic rollback (userTransaction.rollback)")
	}
	if jta.Properties["framework"] != "vertx" {
		t.Errorf("[#3863 prog-jta] framework = %v, want vertx", jta.Properties["framework"])
	}

	// Hibernate session.beginTransaction(): boundary, no rollback marker.
	hib, ok := txEntityByName(r, "TransferHandler.runHibernate")
	if !ok {
		t.Fatalf("[#3863 prog-hib] expected TransferHandler.runHibernate boundary")
	}
	if hib.Properties["tx_api"] != "hibernate_session" {
		t.Errorf("[#3863 prog-hib] runHibernate tx_api = %v, want hibernate_session", hib.Properties["tx_api"])
	}
	if _, has := hib.Properties["rollback"]; has {
		t.Errorf("[#3863 prog-hib] runHibernate has no rollback() call; must NOT carry rollback marker")
	}

	// setRollbackOnly() alone: rollback marker, NOT a boundary (opens elsewhere).
	mark, ok := txEntityByName(r, "TransferHandler.markOnly")
	if !ok {
		t.Fatalf("[#3863 prog-mark] expected TransferHandler.markOnly entity")
	}
	if _, isBoundary := mark.Properties["transaction_boundary"]; isBoundary {
		t.Errorf("[#3863 prog-mark] markOnly only calls setRollbackOnly(); must NOT be a boundary, got %v", mark.Properties)
	}
	if mark.Properties["rollback"] != "programmatic" {
		t.Errorf("[#3863 prog-mark] markOnly should carry programmatic rollback")
	}

	// Negative: a plain read with no tx API must produce no entity at all.
	if _, ok := txEntityByName(r, "TransferHandler.read"); ok {
		t.Errorf("[#3863 prog-neg] read() (no tx API) must NOT produce a boundary/rollback entity")
	}
}

// TestTransactional_ProgrammaticGating_3863 proves programmatic detection fires
// for the wider set (akka_http, struts, javalin, guice) and that a method with no
// programmatic tx API in an annotation-only framework stays empty.
func TestTransactional_ProgrammaticGating_3863(t *testing.T) {
	src := `package x;
public class Svc {
    public void open() { userTransaction.begin(); userTransaction.commit(); }
}`
	for _, fw := range []string{"akka_http", "struts", "javalin", "guice", "vertx"} {
		r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: fw, FilePath: "Svc.java"})
		e, ok := txEntityByName(r, "Svc.open")
		if !ok {
			t.Errorf("[#3863 prog-gating] framework %q expected Svc.open programmatic boundary, got %v", fw, entityNames(r.Entities))
			continue
		}
		if e.Properties["transaction_boundary"] != "programmatic" {
			t.Errorf("[#3863 prog-gating] %q Svc.open transaction_boundary = %v, want programmatic", fw, e.Properties["transaction_boundary"])
		}
	}
	// A framework outside BOTH sets: no-op.
	if r := ExtractTransactional(PatternContext{Source: src, Language: "java", Framework: "django", FilePath: "Svc.java"}); len(r.Entities) != 0 {
		t.Errorf("[#3863 prog-gating] django should no-op, got %d entities", len(r.Entities))
	}
}
