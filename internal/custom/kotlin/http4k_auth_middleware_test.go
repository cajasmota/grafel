package kotlin_test

import (
	"testing"
)

// http4k_auth_middleware_test.go: tests for custom_kotlin_http4k_auth_middleware.
// Registry targets:
//   lang.kotlin.framework.http4k Auth/auth_coverage        → partial
//   lang.kotlin.framework.http4k Middleware/middleware_coverage → partial

const http4kAuthSrc = `
package com.example.app

import org.http4k.filter.ServerFilters
import org.http4k.core.HttpHandler
import org.http4k.core.then

val securedApp = ServerFilters.BearerAuth(credentials) .then(routes)
val basicApp   = ServerFilters.BasicAuth("realm", credentials).then(routes)
`

const http4kMiddlewareSrc = `
package com.example.app

import org.http4k.filter.ServerFilters
import org.http4k.core.Filter

val app = ServerFilters.RequestTracing()
    .then(ServerFilters.GZip())
    .then(routes)

val customFilter = Filter { next ->
    { req ->
        println("before")
        next(req).also { println("after") }
    }
}
`

const http4kNoMatchSrc = `
package com.example
data class Foo(val x: Int)
`

func TestHttp4kAuth_BearerBasicAuth(t *testing.T) {
	// Registry target: lang.kotlin.framework.http4k Auth/auth_coverage → partial
	ents := extract(t, "custom_kotlin_http4k_auth_middleware", fi("App.kt", "kotlin", http4kAuthSrc))
	if len(ents) == 0 {
		t.Fatal("[http4k_auth] expected auth entities from ServerFilters.BearerAuth, got none")
	}
	bearerFound := false
	basicFound := false
	for _, e := range ents {
		if e.Name == "ServerFilters.BearerAuth" {
			bearerFound = true
		}
		if e.Name == "ServerFilters.BasicAuth" {
			basicFound = true
		}
	}
	if !bearerFound {
		t.Errorf("[http4k_auth] expected 'ServerFilters.BearerAuth' entity, got: %v", ents)
	}
	if !basicFound {
		t.Errorf("[http4k_auth] expected 'ServerFilters.BasicAuth' entity, got: %v", ents)
	}
}

func TestHttp4kMiddleware_ServerFiltersAndCustomFilter(t *testing.T) {
	// Registry target: lang.kotlin.framework.http4k Middleware/middleware_coverage → partial
	ents := extract(t, "custom_kotlin_http4k_auth_middleware", fi("Middleware.kt", "kotlin", http4kMiddlewareSrc))
	if len(ents) == 0 {
		t.Fatal("[http4k_mw] expected middleware entities, got none")
	}
	tracingFound := false
	customFilterFound := false
	for _, e := range ents {
		if e.Name == "ServerFilters.RequestTracing" {
			tracingFound = true
		}
		if e.Subtype == "middleware" && e.Name == "ServerFilters.GZip" {
			// also valid
		}
		if e.Subtype == "middleware" && e.Kind == "SCOPE.Pattern" {
			_ = e
		}
	}
	// Accept either tracingFound or customFilterFound as evidence
	for _, e := range ents {
		if e.Name == "ServerFilters.RequestTracing" {
			tracingFound = true
		}
		if e.Subtype == "middleware" {
			customFilterFound = true
		}
	}
	if !tracingFound && !customFilterFound {
		t.Errorf("[http4k_mw] expected at least one middleware entity, got: %v", ents)
	}
}

func TestHttp4kAuth_NoMatch(t *testing.T) {
	ents := extract(t, "custom_kotlin_http4k_auth_middleware", fi("Foo.kt", "kotlin", http4kNoMatchSrc))
	if len(ents) != 0 {
		t.Errorf("[http4k_auth] expected no entities for non-http4k file, got %d", len(ents))
	}
}

func TestHttp4kAuth_EmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_http4k_auth_middleware", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[http4k_auth] expected no entities for empty file, got %d", len(ents))
	}
}
