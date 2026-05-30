package kotlin_test

import (
	"testing"
)

// ktor_di_transactions_test.go: tests for Ktor DI (Koin) and Exposed transaction extractors.

const ktorKoinModuleSrc = `
package com.example.di

import org.koin.dsl.module
import org.koin.core.module.dsl.singleOf

val appModule = module {
    single<UserService> { UserServiceImpl(get()) }
    factory<UserRepository> { UserRepositoryImpl(get()) }
    scoped<CacheService> { CacheServiceImpl() }
}

class UserController(private val userService: UserService) {
    val repo: UserRepository by inject()
}
`

const ktorExposedTransactionSrc = `
package com.example.db

import org.jetbrains.exposed.sql.transactions.transaction
import org.jetbrains.exposed.sql.transactions.experimental.newSuspendedTransaction

suspend fun findUser(id: Long) = newSuspendedTransaction {
    Users.select { Users.id eq id }.firstOrNull()
}

fun createUser(name: String) = transaction {
    Users.insert { it[this.name] = name }
}

fun createUserWithIsolation() = transaction(
    transactionIsolation = Connection.TRANSACTION_SERIALIZABLE
) {
    Users.insert { it[this.name] = "isolated" }
}
`

func TestKtorDI_KoinSingleFactory(t *testing.T) {
	// Registry target: lang.kotlin.framework.ktor DI/di_binding_extraction → partial
	ents := extract(t, "custom_kotlin_ktor_di", fi("AppModule.kt", "kotlin", ktorKoinModuleSrc))
	if len(ents) == 0 {
		t.Fatal("[ktor_di] expected Koin DI entities, got none")
	}
	// Should find: UserService (single), UserRepository (factory), CacheService (scoped),
	// plus at least one injection point (val repo by inject())
	bindingCount := 0
	injectCount := 0
	for _, e := range ents {
		if e.Subtype == "di_binding" {
			bindingCount++
		}
		if e.Subtype == "di_injection_point" {
			injectCount++
		}
	}
	if bindingCount == 0 {
		t.Errorf("[ktor_di] expected di_binding entities, got 0 (all: %v)", ents)
	}
	if injectCount == 0 {
		t.Errorf("[ktor_di] expected di_injection_point entity from 'by inject()', got 0")
	}
}

func TestKtorDI_EmptySource(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_di", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[ktor_di] expected no entities for empty file, got %d", len(ents))
	}
}

func TestKtorDI_NonKoinSource(t *testing.T) {
	src := `
package com.example
fun hello() = "world"
`
	ents := extract(t, "custom_kotlin_ktor_di", fi("Hello.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[ktor_di] expected no entities for non-Koin file, got %d", len(ents))
	}
}

func TestKtorTransactions_ExposedTransaction(t *testing.T) {
	// Registry target: lang.kotlin.framework.ktor Transactions/transaction_boundary_extraction → partial
	ents := extract(t, "custom_kotlin_ktor_transactions", fi("UserDao.kt", "kotlin", ktorExposedTransactionSrc))
	if len(ents) == 0 {
		t.Fatal("[ktor_tx] expected transaction boundary entities, got none")
	}
	txCount := 0
	for _, e := range ents {
		if e.Subtype == "transaction_boundary" {
			txCount++
		}
	}
	if txCount < 2 {
		t.Errorf("[ktor_tx] expected >= 2 transaction_boundary entities (transaction + newSuspendedTransaction), got %d", txCount)
	}
}

func TestKtorTransactions_EmptySource(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_transactions", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[ktor_tx] expected no entities for empty file, got %d", len(ents))
	}
}

// ----------------------------------------------------------------------------
// DEEP-GRIND (#3435): value-asserting tests for Koin DI binding types /
// injection points and Exposed transaction boundaries (incl. isolation level).
// These assert SPECIFIC bound type names — the bar to flip the ktor
// di_binding_extraction / di_injection_point / transaction_boundary_extraction
// cells from partial→full.
// ----------------------------------------------------------------------------

// TestKtorDI_BindingTypeNames_3435 asserts that each Koin single/factory/scoped
// declaration produces a di_binding entity named after its bound TYPE.
func TestKtorDI_BindingTypeNames_3435(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_di", fi("AppModule.kt", "kotlin", ktorKoinModuleSrc))

	bindings := map[string]bool{}
	var injectionPoint string
	for _, e := range ents {
		switch e.Subtype {
		case "di_binding":
			bindings[e.Name] = true
		case "di_injection_point":
			injectionPoint = e.Name
		}
	}
	for _, want := range []string{"UserService", "UserRepository", "CacheService"} {
		if !bindings[want] {
			t.Errorf("[3435 ktor di_binding] missing bound type %q; got %v", want, bindings)
		}
	}
	// `val repo: UserRepository by inject()` → injection point keyed field:type.
	if injectionPoint != "repo:UserRepository" {
		t.Errorf("[3435 ktor di_injection] injection point = %q, want repo:UserRepository", injectionPoint)
	}
}

// TestKtorTransactions_BoundaryAndIsolation_3435 asserts that Exposed
// transaction { } / newSuspendedTransaction { } produce boundary entities and
// that the SERIALIZABLE isolation level is captured by name.
func TestKtorTransactions_BoundaryAndIsolation_3435(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_transactions", fi("UserDao.kt", "kotlin", ktorExposedTransactionSrc))

	boundaries := 0
	foundIsolation := false
	for _, e := range ents {
		if e.Subtype != "transaction_boundary" {
			continue
		}
		boundaries++
		if e.Name == "transaction#isolation:SERIALIZABLE" {
			foundIsolation = true
		}
	}
	if boundaries < 3 {
		t.Errorf("[3435 ktor tx] expected >=3 transaction boundaries (transaction + suspended + isolation), got %d", boundaries)
	}
	if !foundIsolation {
		t.Error("[3435 ktor tx] expected SERIALIZABLE isolation boundary entity")
	}
}
