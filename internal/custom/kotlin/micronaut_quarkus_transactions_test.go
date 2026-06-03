package kotlin_test

import (
	"testing"
)

// micronaut_quarkus_transactions_test.go: value-asserting tests for the
// Micronaut + Quarkus framework attribution of the native Kotlin @Transactional
// extractor (custom_kotlin_spring_transactions, #4016, epic #3872, audit #3886).
//
// #4016 extends the #4014 Spring extractor: the shared jakarta/javax JTA
// @Transactional used by Micronaut and Quarkus was previously credited to
// framework=spring-boot, so the micronaut/quarkus Transactions cells got zero
// credit. These tests assert the SPECIFIC framework attribution, propagation,
// rollback rules, and the Quarkus Panache.withTransaction boundary — the bar to
// flip those cells missing→partial.

// reuses txByName from spring_transactions_test.go (same package).

// ktMicronautTxSrc: Micronaut Kotlin service using the JTA @Transactional with
// JTA TxType propagation and JTA rollbackOn rollback rules.
const ktMicronautTxSrc = `
package com.example.service

import jakarta.inject.Singleton
import jakarta.transaction.Transactional
import jakarta.transaction.Transactional.TxType
import io.micronaut.transaction.annotation.ReadOnly
import java.io.IOException

@Singleton
open class OrderService(private val repo: OrderRepository) {

    @Transactional
    open fun place(o: Order) {
        repo.save(o)
    }

    @Transactional(TxType.REQUIRES_NEW)
    open fun audit(e: Event) {
        repo.persist(e)
    }

    @Transactional(rollbackOn = [IOException::class])
    open fun export(id: Long) {
        repo.update(id)
    }

    open fun untracked() {
        println("no boundary here")
    }
}
`

// TestKotlinMnTx_FrameworkAttribution_4016 asserts a Micronaut jakarta
// @Transactional fun produces a boundary stamped framework=micronaut (NOT
// spring-boot) and transactional=true, and that the un-annotated fun is none.
func TestKotlinMnTx_FrameworkAttribution_4016(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("OrderService.kt", "kotlin", ktMicronautTxSrc))
	if len(ents) == 0 {
		t.Fatal("[4016 micronaut] expected transaction boundaries, got none")
	}
	by := txByName(ents)

	for _, want := range []string{"OrderService.place", "OrderService.audit", "OrderService.export"} {
		e, ok := by[want]
		if !ok {
			t.Fatalf("[4016 micronaut] missing boundary %q; got %v", want, by)
		}
		if e.Props["framework"] != "micronaut" {
			t.Errorf("[4016 micronaut] %s framework = %q, want micronaut", want, e.Props["framework"])
		}
		if e.Props["transactional"] != "true" {
			t.Errorf("[4016 micronaut] %s transactional = %q, want true", want, e.Props["transactional"])
		}
		if e.Props["transaction_boundary"] != "method" {
			t.Errorf("[4016 micronaut] %s boundary = %q, want method", want, e.Props["transaction_boundary"])
		}
	}

	// Negative: the un-annotated untracked() fun is NOT a boundary.
	for name := range by {
		if name == "OrderService.untracked" || name == "untracked" {
			t.Errorf("[4016 micronaut negative] untracked() must not be a boundary, got %q", name)
		}
	}
}

// TestKotlinMnTx_PropagationAndRollback_4016 asserts JTA TxType propagation and
// JTA rollbackOn rollback rules are captured (the Java extractor never fired on
// .kt; these forms differ from Spring's Propagation/rollbackFor).
func TestKotlinMnTx_PropagationAndRollback_4016(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("OrderService.kt", "kotlin", ktMicronautTxSrc))
	by := txByName(ents)

	// JTA TxType.REQUIRES_NEW → propagation=REQUIRES_NEW.
	if e := by["OrderService.audit"]; e == nil || e.Props["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[4016 micronaut propagation] audit propagation = %v, want REQUIRES_NEW", e)
	}
	// JTA rollbackOn = [IOException::class] → rollback_for=IOException.
	if e := by["OrderService.export"]; e == nil || e.Props["rollback_for"] != "IOException" {
		t.Errorf("[4016 micronaut rollback] export rollback_for = %v, want IOException", e)
	}
	// place writes (repo.save) → db_write true.
	if e := by["OrderService.place"]; e == nil || e.Props["db_write"] != "true" {
		t.Errorf("[4016 micronaut db_write] place db_write = %v, want true", e)
	}
}

// ktQuarkusTxSrc: Quarkus Kotlin service using the JTA @Transactional plus a
// Panache.withTransaction reactive boundary and a read-only withSession.
const ktQuarkusTxSrc = `
package com.example.service

import jakarta.enterprise.context.ApplicationScoped
import jakarta.transaction.Transactional
import io.quarkus.hibernate.reactive.panache.Panache
import java.io.IOException

@ApplicationScoped
class OrderService(private val repo: OrderRepository) {

    @Transactional
    fun create(o: Order) {
        repo.persist(o)
    }

    @Transactional(rollbackOn = [IOException::class])
    fun risky(o: Order) {
        repo.save(o)
    }

    fun reactiveCreate(o: Order) = Panache.withTransaction {
        repo.persist(o)
    }

    fun reactiveLookup(id: Long) = Panache.withSession {
        repo.findById(id)
    }

    fun untracked() {
        println("no boundary here")
    }
}
`

// TestKotlinQkTx_FrameworkAttribution_4016 asserts a Quarkus jakarta
// @Transactional fun produces a boundary stamped framework=quarkus.
func TestKotlinQkTx_FrameworkAttribution_4016(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("OrderService.kt", "kotlin", ktQuarkusTxSrc))
	by := txByName(ents)

	for _, want := range []string{"OrderService.create", "OrderService.risky"} {
		e, ok := by[want]
		if !ok {
			t.Fatalf("[4016 quarkus] missing boundary %q; got %v", want, by)
		}
		if e.Props["framework"] != "quarkus" {
			t.Errorf("[4016 quarkus] %s framework = %q, want quarkus", want, e.Props["framework"])
		}
		if e.Props["transaction_boundary"] != "method" {
			t.Errorf("[4016 quarkus] %s boundary = %q, want method", want, e.Props["transaction_boundary"])
		}
	}

	// JTA rollbackOn rollback rule captured.
	if e := by["OrderService.risky"]; e == nil || e.Props["rollback_for"] != "IOException" {
		t.Errorf("[4016 quarkus rollback] risky rollback_for = %v, want IOException", e)
	}

	// Negative.
	for name := range by {
		if name == "OrderService.untracked" || name == "untracked" {
			t.Errorf("[4016 quarkus negative] untracked() must not be a boundary, got %q", name)
		}
	}
}

// TestKotlinQkTx_PanacheBoundary_4016 asserts the Panache.withTransaction
// reactive boundary is credited (framework=quarkus, transaction_boundary=code,
// db_write from the lambda body), and that the read-only withSession is a
// boundary WITHOUT db_write.
func TestKotlinQkTx_PanacheBoundary_4016(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("OrderService.kt", "kotlin", ktQuarkusTxSrc))
	by := txByName(ents)

	wt := by["OrderService.reactiveCreate:Panache.withTransaction"]
	if wt == nil {
		t.Fatalf("[4016 panache] missing Panache.withTransaction boundary; got %v", by)
	}
	if wt.Props["framework"] != "quarkus" {
		t.Errorf("[4016 panache] withTransaction framework = %q, want quarkus", wt.Props["framework"])
	}
	if wt.Props["transaction_boundary"] != "code" {
		t.Errorf("[4016 panache] withTransaction boundary = %q, want code", wt.Props["transaction_boundary"])
	}
	if wt.Props["tx_api"] != "panache_withTransaction" {
		t.Errorf("[4016 panache] withTransaction tx_api = %q, want panache_withTransaction", wt.Props["tx_api"])
	}
	// The lambda body writes (repo.persist) → db_write true.
	if wt.Props["db_write"] != "true" {
		t.Errorf("[4016 panache] withTransaction db_write = %q, want true", wt.Props["db_write"])
	}

	// withSession is a read scope → boundary present but NO db_write.
	ws := by["OrderService.reactiveLookup:Panache.withSession"]
	if ws == nil {
		t.Fatalf("[4016 panache] missing Panache.withSession boundary; got %v", by)
	}
	if ws.Props["db_write"] != "" {
		t.Errorf("[4016 panache] withSession db_write = %q, want empty (read scope)", ws.Props["db_write"])
	}
}

// TestKotlinQkTx_PanacheGatedOnQuarkus_4016 asserts a Panache.withTransaction
// call is NOT claimed when there is no Quarkus/Panache import (honesty: a
// user-defined Panache symbol or a Spring file must not yield a quarkus tx).
func TestKotlinQkTx_PanacheGatedOnQuarkus_4016(t *testing.T) {
	src := `
package com.example
object Panache { fun withTransaction(b: () -> Unit) = b() }

fun doIt() = Panache.withTransaction {
    println("not quarkus")
}
`
	ents := extract(t, "custom_kotlin_spring_transactions", fi("Home.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[4016 panache honesty] non-Quarkus Panache must not be claimed, got %d entities", len(ents))
	}
}

// TestKotlinMnQkTx_FrameworkAttribution_4016 documents the disambiguation: a
// jakarta @Transactional file with an io.micronaut import is micronaut, with an
// io.quarkus import is quarkus, and with neither is spring-boot (the default).
func TestKotlinMnQkTx_FrameworkAttribution_4016(t *testing.T) {
	cases := []struct {
		name     string
		extraImp string
		wantFW   string
	}{
		{"micronaut", "import io.micronaut.context.annotation.Context", "micronaut"},
		{"quarkus", "import io.quarkus.runtime.Startup", "quarkus"},
		{"spring-default", "", "spring-boot"},
	}
	for _, tc := range cases {
		src := `
package com.example
import jakarta.transaction.Transactional
` + tc.extraImp + `

class Svc {
    @Transactional
    fun act() { repo.save(x) }
}
`
		ents := extract(t, "custom_kotlin_spring_transactions", fi("Svc.kt", "kotlin", src))
		by := txByName(ents)
		e := by["Svc.act"]
		if e == nil {
			t.Fatalf("[4016 %s] missing boundary Svc.act", tc.name)
		}
		if e.Props["framework"] != tc.wantFW {
			t.Errorf("[4016 %s] framework = %q, want %q", tc.name, e.Props["framework"], tc.wantFW)
		}
	}
}
