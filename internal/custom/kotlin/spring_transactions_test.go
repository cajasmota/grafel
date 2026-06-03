package kotlin_test

import (
	"testing"
)

// spring_transactions_test.go: value-asserting tests for the native Kotlin
// Spring @Transactional extractor (custom_kotlin_spring_transactions, #4014).
//
// These assert the SPECIFIC boundary names, propagation modes, rollback class
// lists, readOnly markers, and the db_write honesty boundary — the bar to flip
// the four lang.kotlin.framework.spring-boot Transactions cells missing→partial.

const ktSpringTxSrc = `
package com.example.service

import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.transaction.annotation.Propagation
import org.springframework.transaction.annotation.Isolation
import java.io.IOException

@Service
class AccountService(private val repo: AccountRepository) {

    @Transactional
    fun transfer(from: Account, to: Account) {
        repo.save(from)
        repo.save(to)
    }

    @Transactional(readOnly = true)
    fun lookup(id: Long): Account? = repo.findById(id).orElse(null)

    @Transactional(propagation = Propagation.REQUIRES_NEW, rollbackFor = [IOException::class])
    fun audit(e: Event) {
        repo.save(e)
    }

    @Transactional(isolation = Isolation.SERIALIZABLE, noRollbackFor = [WarnException::class])
    fun reconcile() {
        repo.deleteAll()
    }

    fun plain() {
        println("no transaction here")
    }
}
`

// txByName indexes transaction_boundary entities by their entity Name.
func txByName(ents []entitySummary) map[string]*entitySummary {
	out := map[string]*entitySummary{}
	for i := range ents {
		if ents[i].Subtype == "transaction_boundary" {
			out[ents[i].Name] = &ents[i]
		}
	}
	return out
}

// TestKotlinSpringTx_Boundaries_4014 asserts each annotated fun produces a
// boundary stamped framework=spring-boot, transactional=true, and that the
// non-annotated fun produces NONE.
func TestKotlinSpringTx_Boundaries_4014(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("AccountService.kt", "kotlin", ktSpringTxSrc))
	if len(ents) == 0 {
		t.Fatal("[4014 tx] expected transaction boundaries, got none")
	}
	by := txByName(ents)

	for _, want := range []string{"AccountService.transfer", "AccountService.lookup", "AccountService.audit", "AccountService.reconcile"} {
		e, ok := by[want]
		if !ok {
			t.Fatalf("[4014 tx] missing boundary %q; got %v", want, by)
		}
		if e.Props["framework"] != "spring-boot" {
			t.Errorf("[4014 tx] %s framework = %q, want spring-boot", want, e.Props["framework"])
		}
		if e.Props["transactional"] != "true" {
			t.Errorf("[4014 tx] %s transactional = %q, want true", want, e.Props["transactional"])
		}
		if e.Props["transaction_boundary"] != "method" {
			t.Errorf("[4014 tx] %s boundary = %q, want method", want, e.Props["transaction_boundary"])
		}
		if e.Props["declaring_class"] != "AccountService" {
			t.Errorf("[4014 tx] %s declaring_class = %q, want AccountService", want, e.Props["declaring_class"])
		}
	}

	// Negative: the un-annotated plain() fun is NOT a boundary.
	for name := range by {
		if name == "AccountService.plain" || name == "plain" {
			t.Errorf("[4014 tx negative] plain() must not be a transaction boundary, got %q", name)
		}
	}
}

// TestKotlinSpringTx_Propagation_4014 asserts propagation=REQUIRES_NEW is
// captured on audit (not the hardcoded REQUIRED the ktor extractor emitted).
func TestKotlinSpringTx_Propagation_4014(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("AccountService.kt", "kotlin", ktSpringTxSrc))
	by := txByName(ents)

	if e := by["AccountService.audit"]; e == nil || e.Props["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[4014 propagation] audit propagation = %v, want REQUIRES_NEW", e)
	}
	// transfer has no explicit propagation → property absent (no phantom REQUIRED).
	if e := by["AccountService.transfer"]; e == nil || e.Props["propagation"] != "" {
		t.Errorf("[4014 propagation] transfer propagation = %q, want empty (no default fabrication)", e.Props["propagation"])
	}
}

// TestKotlinSpringTx_RollbackRules_4014 asserts rollbackFor / noRollbackFor /
// isolation are captured from the Kotlin [X::class] list form.
func TestKotlinSpringTx_RollbackRules_4014(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("AccountService.kt", "kotlin", ktSpringTxSrc))
	by := txByName(ents)

	if e := by["AccountService.audit"]; e == nil || e.Props["rollback_for"] != "IOException" {
		t.Errorf("[4014 rollback] audit rollback_for = %v, want IOException", e)
	}
	if e := by["AccountService.reconcile"]; e == nil || e.Props["no_rollback_for"] != "WarnException" {
		t.Errorf("[4014 rollback] reconcile no_rollback_for = %v, want WarnException", e)
	}
	if e := by["AccountService.reconcile"]; e == nil || e.Props["isolation"] != "SERIALIZABLE" {
		t.Errorf("[4014 rollback] reconcile isolation = %v, want SERIALIZABLE", e)
	}
}

// TestKotlinSpringTx_ReadOnlyAndDbWrite_4014 asserts the honesty boundary:
// a write-shaped method gets db_write=true; a readOnly method NEVER does, even
// though it issues a repo call.
func TestKotlinSpringTx_ReadOnlyAndDbWrite_4014(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_transactions", fi("AccountService.kt", "kotlin", ktSpringTxSrc))
	by := txByName(ents)

	// transfer writes (repo.save) → db_write true.
	if e := by["AccountService.transfer"]; e == nil || e.Props["db_write"] != "true" {
		t.Errorf("[4014 db_write] transfer db_write = %v, want true", e)
	}
	// audit writes (repo.save) → db_write true.
	if e := by["AccountService.audit"]; e == nil || e.Props["db_write"] != "true" {
		t.Errorf("[4014 db_write] audit db_write = %v, want true", e)
	}
	// lookup is readOnly and only reads (findById) → read_only true, NO db_write.
	e := by["AccountService.lookup"]
	if e == nil {
		t.Fatal("[4014 db_write] missing lookup boundary")
	}
	if e.Props["read_only"] != "true" {
		t.Errorf("[4014 db_write] lookup read_only = %q, want true", e.Props["read_only"])
	}
	if e.Props["db_write"] != "" {
		t.Errorf("[4014 db_write] lookup db_write = %q, want empty (readOnly read must not write)", e.Props["db_write"])
	}
}

// TestKotlinSpringTx_ClassLevel_4014 asserts a class-level @Transactional emits
// a class boundary.
func TestKotlinSpringTx_ClassLevel_4014(t *testing.T) {
	src := `
package com.example
import org.springframework.transaction.annotation.Transactional

@Transactional
class BillingService {
    fun charge() {}
}
`
	ents := extract(t, "custom_kotlin_spring_transactions", fi("BillingService.kt", "kotlin", src))
	by := txByName(ents)
	e := by["BillingService"]
	if e == nil {
		t.Fatalf("[4014 class] expected class-level boundary BillingService, got %v", by)
	}
	if e.Props["transaction_boundary"] != "class" {
		t.Errorf("[4014 class] boundary = %q, want class", e.Props["transaction_boundary"])
	}
}

// TestKotlinSpringTx_NoSpringImport_4014 asserts a same-named user @Transactional
// without a Spring/Jakarta import is NOT claimed (framework-attribution honesty).
func TestKotlinSpringTx_NoSpringImport_4014(t *testing.T) {
	src := `
package com.example
annotation class Transactional

@Transactional
fun homegrown() {}
`
	ents := extract(t, "custom_kotlin_spring_transactions", fi("Home.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[4014 honesty] non-Spring @Transactional must not be claimed, got %d entities", len(ents))
	}
}

// TestKotlinSpringTx_EmptyAndWrongLanguage_4014 covers the bail-out paths.
func TestKotlinSpringTx_EmptyAndWrongLanguage_4014(t *testing.T) {
	if ents := extract(t, "custom_kotlin_spring_transactions", fi("Empty.kt", "kotlin", "")); len(ents) != 0 {
		t.Errorf("[4014] empty file → 0 entities, got %d", len(ents))
	}
	if ents := extract(t, "custom_kotlin_spring_transactions", fi("X.java", "java", ktSpringTxSrc)); len(ents) != 0 {
		t.Errorf("[4014] non-kotlin language → 0 entities, got %d", len(ents))
	}
}
