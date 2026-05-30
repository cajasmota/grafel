package kotlin_test

import (
	"testing"
)

// micronaut_quarkus_test.go: tests for Micronaut and Quarkus Kotlin extractors.
// Registry targets:
//   lang.kotlin.framework.micronaut Auth/auth_coverage, Middleware/middleware_coverage,
//                                   DI/di_binding_extraction, DI/di_injection_point,
//                                   DI/di_scope_resolution
//   lang.kotlin.framework.quarkus   Auth/auth_coverage, Middleware/middleware_coverage,
//                                   DI/di_binding_extraction, DI/di_injection_point,
//                                   DI/di_scope_resolution

const mnAuthSrc = `
package com.example.security

import io.micronaut.security.annotation.Secured
import javax.annotation.security.RolesAllowed
import javax.annotation.security.PermitAll

@Secured("isAuthenticated()")
class AdminController {
    @RolesAllowed("ADMIN", "SUPER_ADMIN")
    fun adminAction() = "ok"

    @PermitAll
    fun publicAction() = "public"
}
`

const mnDISrc = `
package com.example.service

import jakarta.inject.Inject
import jakarta.inject.Singleton
import io.micronaut.context.annotation.Bean

@Singleton
class UserService {
    @Inject
    val userRepository: UserRepository? = null
}

@Singleton
class OrderService {
    @Inject lateinit var paymentService: PaymentService
}
`

const mnMiddlewareSrc = `
package com.example.filter

import io.micronaut.http.filter.HttpServerFilter
import io.micronaut.http.annotation.Filter

@Filter("/**")
class LoggingFilter : HttpServerFilter {
    override fun doFilter(
        request: io.micronaut.http.HttpRequest<*>,
        chain: io.micronaut.http.filter.ServerFilterChain
    ) = chain.proceed(request)
}
`

const qkAuthSrc = `
package com.example.resource

import javax.annotation.security.RolesAllowed
import javax.annotation.security.PermitAll
import javax.annotation.security.DenyAll
import io.quarkus.security.Authenticated

@Authenticated
class OrderResource {
    @RolesAllowed("USER")
    fun listOrders() = listOf()

    @PermitAll
    fun health() = "ok"

    @DenyAll
    fun admin() = "denied"
}
`

const qkDISrc = `
package com.example.service

import jakarta.enterprise.context.ApplicationScoped
import jakarta.inject.Inject
import jakarta.enterprise.inject.Produces

@ApplicationScoped
class UserService {
    @Inject
    lateinit var userRepository: UserRepository

    @Produces
    fun createCache(): Cache = Cache()
}

@jakarta.enterprise.context.RequestScoped
class RequestScopedService
`

const qkMiddlewareSrc = `
package com.example.filter

import javax.ws.rs.ext.Provider
import javax.ws.rs.container.ContainerRequestFilter
import javax.ws.rs.container.ContainerRequestContext

@Provider
class AuthFilter : ContainerRequestFilter {
    override fun filter(requestContext: ContainerRequestContext) {
        // check auth
    }
}
`

// --- Micronaut tests ---

func TestKotlinMicronaut_Auth(t *testing.T) {
	// Registry target: lang.kotlin.framework.micronaut Auth/auth_coverage → partial
	ents := extract(t, "custom_kotlin_micronaut", fi("AdminController.kt", "kotlin", mnAuthSrc))
	if len(ents) == 0 {
		t.Fatal("[micronaut_auth] expected auth entities, got none")
	}
	authCount := 0
	for _, e := range ents {
		if e.Subtype == "auth_policy" {
			authCount++
		}
	}
	if authCount < 2 {
		t.Errorf("[micronaut_auth] expected >= 2 auth_policy entities (@Secured + @RolesAllowed), got %d", authCount)
	}
}

func TestKotlinMicronaut_DI(t *testing.T) {
	// Registry target: lang.kotlin.framework.micronaut DI/* → partial
	ents := extract(t, "custom_kotlin_micronaut", fi("Service.kt", "kotlin", mnDISrc))
	if len(ents) == 0 {
		t.Fatal("[micronaut_di] expected DI entities, got none")
	}
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
		t.Errorf("[micronaut_di] expected di_binding entities (@Singleton), got 0")
	}
	if injectCount == 0 {
		t.Errorf("[micronaut_di] expected di_injection_point entities (@Inject), got 0")
	}
}

func TestKotlinMicronaut_Middleware(t *testing.T) {
	// Registry target: lang.kotlin.framework.micronaut Middleware/middleware_coverage → partial
	ents := extract(t, "custom_kotlin_micronaut", fi("LoggingFilter.kt", "kotlin", mnMiddlewareSrc))
	if len(ents) == 0 {
		t.Fatal("[micronaut_mw] expected middleware entities, got none")
	}
	mwCount := 0
	for _, e := range ents {
		if e.Subtype == "middleware" {
			mwCount++
		}
	}
	if mwCount == 0 {
		t.Errorf("[micronaut_mw] expected middleware entity (HttpServerFilter), got 0; all: %v", ents)
	}
}

func TestKotlinMicronaut_EmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_micronaut", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[micronaut] expected no entities for empty file, got %d", len(ents))
	}
}

// --- Quarkus tests ---

func TestKotlinQuarkus_Auth(t *testing.T) {
	// Registry target: lang.kotlin.framework.quarkus Auth/auth_coverage → partial
	ents := extract(t, "custom_kotlin_quarkus", fi("OrderResource.kt", "kotlin", qkAuthSrc))
	if len(ents) == 0 {
		t.Fatal("[quarkus_auth] expected auth entities, got none")
	}
	authCount := 0
	for _, e := range ents {
		if e.Subtype == "auth_policy" {
			authCount++
		}
	}
	if authCount < 3 {
		t.Errorf("[quarkus_auth] expected >= 3 auth_policy entities (@Authenticated + @RolesAllowed + @PermitAll + @DenyAll), got %d", authCount)
	}
}

func TestKotlinQuarkus_DI(t *testing.T) {
	// Registry target: lang.kotlin.framework.quarkus DI/* → partial
	ents := extract(t, "custom_kotlin_quarkus", fi("UserService.kt", "kotlin", qkDISrc))
	if len(ents) == 0 {
		t.Fatal("[quarkus_di] expected DI entities, got none")
	}
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
		t.Errorf("[quarkus_di] expected di_binding entities (@ApplicationScoped), got 0")
	}
	if injectCount == 0 {
		t.Errorf("[quarkus_di] expected di_injection_point entities (@Inject), got 0")
	}
}

func TestKotlinQuarkus_Middleware(t *testing.T) {
	// Registry target: lang.kotlin.framework.quarkus Middleware/middleware_coverage → partial
	ents := extract(t, "custom_kotlin_quarkus", fi("AuthFilter.kt", "kotlin", qkMiddlewareSrc))
	if len(ents) == 0 {
		t.Fatal("[quarkus_mw] expected middleware entities, got none")
	}
	mwCount := 0
	for _, e := range ents {
		if e.Subtype == "middleware" {
			mwCount++
		}
	}
	if mwCount == 0 {
		t.Errorf("[quarkus_mw] expected middleware entity (@Provider ContainerRequestFilter), got 0; all: %v", ents)
	}
}

func TestKotlinQuarkus_EmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_quarkus", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[quarkus] expected no entities for empty file, got %d", len(ents))
	}
}
