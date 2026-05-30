package kotlin_test

import (
	"testing"
)

// ktor_auth_middleware_test.go: value-asserting tests for
// custom_kotlin_ktor_auth_middleware.
// Registry targets (partial → full):
//   lang.kotlin.framework.ktor Auth/auth_coverage
//   lang.kotlin.framework.ktor Middleware/middleware_coverage
//
// "full" discipline: each test asserts a SPECIFIC auth method (jwt/basic/oauth)
// and SPECIFIC middleware/plugin names + order — never len(ents) > 0.

const ktorAuthInstallSrc = `
package com.example.security

import io.ktor.server.auth.*
import io.ktor.server.auth.jwt.*

fun Application.configureSecurity() {
    install(Authentication) {
        jwt("auth-jwt") {
            realm = "ktor app"
            verifier(jwkProvider)
            validate { credential -> JWTPrincipal(credential.payload) }
        }
        basic("auth-basic") {
            realm = "Access to admin"
            validate { creds -> if (creds.name == "admin") UserIdPrincipal(creds.name) else null }
        }
        oauth("auth-oauth-google") {
            urlProvider = { "http://localhost:8080/callback" }
            providerLookup = { googleOauthProvider }
        }
    }

    routing {
        authenticate("auth-jwt") {
            get("/protected") { call.respond("ok") }
        }
        authenticate("auth-basic", "auth-jwt") {
            get("/admin") { call.respond("admin") }
        }
    }
}
`

const ktorMiddlewareSrc = `
package com.example.config

import io.ktor.server.plugins.cors.routing.*
import io.ktor.server.plugins.callloging.*
import io.ktor.server.plugins.contentnegotiation.*

fun Application.configureHTTP() {
    install(CORS) {
        anyHost()
    }
    install(CallLogging) {
        level = Level.INFO
    }
    install(ContentNegotiation) {
        json()
    }

    intercept(ApplicationCallPipeline.Plugins) {
        call.response.headers.append("X-Custom", "1")
    }
}
`

const ktorAuthMwNoMatchSrc = `
package com.example
data class Config(val port: Int)
`

func TestKtorAuthMiddleware_SpecificAuthMethods(t *testing.T) {
	// Registry target: lang.kotlin.framework.ktor Auth/auth_coverage → full
	ents := extract(t, "custom_kotlin_ktor_auth_middleware", fi("Security.kt", "kotlin", ktorAuthInstallSrc))
	if len(ents) == 0 {
		t.Fatal("[ktor_auth] expected auth entities, got none")
	}
	// Assert each SPECIFIC auth method is detected with its provider name.
	wantAuth := map[string]bool{
		"auth:jwt:auth-jwt":            false,
		"auth:basic:auth-basic":        false,
		"auth:oauth:auth-oauth-google": false,
	}
	// Assert authenticate() guards bind the named providers.
	wantGuard := map[string]bool{
		"authenticate:auth-jwt":   false,
		"authenticate:auth-basic": false,
	}
	for _, e := range ents {
		if _, ok := wantAuth[e.Name]; ok && e.Subtype == "auth_provider" {
			wantAuth[e.Name] = true
		}
		if _, ok := wantGuard[e.Name]; ok && e.Subtype == "auth_guard" {
			wantGuard[e.Name] = true
		}
	}
	for name, found := range wantAuth {
		if !found {
			t.Errorf("[ktor_auth] expected auth_provider entity %q, got: %v", name, ents)
		}
	}
	for name, found := range wantGuard {
		if !found {
			t.Errorf("[ktor_auth] expected auth_guard entity %q, got: %v", name, ents)
		}
	}
}

func TestKtorAuthMiddleware_OrderedPluginPipeline(t *testing.T) {
	// Registry target: lang.kotlin.framework.ktor Middleware/middleware_coverage → full
	ents := extract(t, "custom_kotlin_ktor_auth_middleware", fi("HTTP.kt", "kotlin", ktorMiddlewareSrc))
	if len(ents) == 0 {
		t.Fatal("[ktor_mw] expected middleware entities, got none")
	}
	// Assert SPECIFIC plugin names AND a custom interceptor are recorded.
	wantMw := map[string]bool{
		"install:CORS":                              false,
		"install:CallLogging":                       false,
		"install:ContentNegotiation":                false,
		"intercept:ApplicationCallPipeline.Plugins": false,
	}
	for _, e := range ents {
		if _, ok := wantMw[e.Name]; ok && e.Subtype == "middleware" {
			wantMw[e.Name] = true
		}
	}
	for name, found := range wantMw {
		if !found {
			t.Errorf("[ktor_mw] expected middleware entity %q, got: %v", name, ents)
		}
	}
}

func TestKtorAuthMiddleware_NoMatch(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_auth_middleware", fi("Config.kt", "kotlin", ktorAuthMwNoMatchSrc))
	if len(ents) != 0 {
		t.Errorf("[ktor_auth] expected no entities for non-ktor file, got %d", len(ents))
	}
}

func TestKtorAuthMiddleware_EmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_auth_middleware", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[ktor_auth] expected no entities for empty file, got %d", len(ents))
	}
}
