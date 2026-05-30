package kotlin_test

// Tests for the custom_kotlin_ktor_routes CST-based nested route extractor.
// Issue #3275 — Ktor route_extraction (nested prefix composition).

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func routeNames(ents []entitySummary) map[string]bool {
	out := make(map[string]bool, len(ents))
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			out[e.Name] = true
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Flat routes (no nesting)
// ---------------------------------------------------------------------------

func TestKtorRoutes_FlatGet(t *testing.T) {
	src := `
routing {
    get("/ping") { call.respond("pong") }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Routing.kt", "kotlin", src))
	names := routeNames(ents)
	if !names["GET /ping"] {
		t.Errorf("expected GET /ping, got %v", names)
	}
}

func TestKtorRoutes_FlatHTTPVerbs(t *testing.T) {
	src := `
routing {
    get("/users") { call.respond(users) }
    post("/users") { call.respond(HttpStatusCode.Created) }
    put("/users/{id}") { call.respond("updated") }
    delete("/users/{id}") { call.respond("deleted") }
    patch("/users/{id}") { call.respond("patched") }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Routes.kt", "kotlin", src))
	names := routeNames(ents)

	want := []string{
		"GET /users",
		"POST /users",
		"PUT /users/{id}",
		"DELETE /users/{id}",
		"PATCH /users/{id}",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("expected route %q; got %v", w, names)
		}
	}
}

// ---------------------------------------------------------------------------
// Nested routes — prefix composition
// ---------------------------------------------------------------------------

func TestKtorRoutes_NestedOneLevel(t *testing.T) {
	src := `
routing {
    route("/api") {
        get("/health") { call.respond("ok") }
        post("/users") { call.respond("created") }
    }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Api.kt", "kotlin", src))
	names := routeNames(ents)

	if !names["GET /api/health"] {
		t.Errorf("expected GET /api/health; got %v", names)
	}
	if !names["POST /api/users"] {
		t.Errorf("expected POST /api/users; got %v", names)
	}
}

func TestKtorRoutes_NestedTwoLevels(t *testing.T) {
	src := `
routing {
    route("/api") {
        route("/v1") {
            get("/users") { call.respond("ok") }
            post("/users") { call.respond("created") }
        }
        get("/health") { call.respond("ok") }
    }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("NestedApi.kt", "kotlin", src))
	names := routeNames(ents)

	want := []string{
		"GET /api/v1/users",
		"POST /api/v1/users",
		"GET /api/health",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("expected route %q; got %v", w, names)
		}
	}
}

func TestKtorRoutes_NestedThreeLevels(t *testing.T) {
	src := `
routing {
    route("/api") {
        route("/v2") {
            route("/admin") {
                get("/users") { call.respond("ok") }
                delete("/users/{id}") { call.respond("deleted") }
            }
        }
    }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Deep.kt", "kotlin", src))
	names := routeNames(ents)

	if !names["GET /api/v2/admin/users"] {
		t.Errorf("expected GET /api/v2/admin/users; got %v", names)
	}
	if !names["DELETE /api/v2/admin/users/{id}"] {
		t.Errorf("expected DELETE /api/v2/admin/users/{id}; got %v", names)
	}
}

func TestKtorRoutes_SiblingPrefixes(t *testing.T) {
	// Two separate route() blocks at the same level must not cross-contaminate.
	src := `
routing {
    route("/v1") {
        get("/items") { call.respond("v1 items") }
    }
    route("/v2") {
        get("/items") { call.respond("v2 items") }
    }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Versions.kt", "kotlin", src))
	names := routeNames(ents)

	if !names["GET /v1/items"] {
		t.Errorf("expected GET /v1/items; got %v", names)
	}
	if !names["GET /v2/items"] {
		t.Errorf("expected GET /v2/items; got %v", names)
	}
}

// ---------------------------------------------------------------------------
// Application module (routing inside extension function)
// ---------------------------------------------------------------------------

func TestKtorRoutes_AppModuleExtensionFunction(t *testing.T) {
	src := `
fun Application.configureRouting() {
    routing {
        route("/api") {
            get("/status") { call.respond("ok") }
        }
    }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("App.kt", "kotlin", src))
	names := routeNames(ents)

	if !names["GET /api/status"] {
		t.Errorf("expected GET /api/status; got %v", names)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestKtorRoutes_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktor_routes", fi("empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty content, got %d", len(ents))
	}
}

func TestKtorRoutes_WrongLanguage(t *testing.T) {
	src := `get("/ping") { println("pong") }`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("main.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-kotlin language, got %d", len(ents))
	}
}

func TestKtorRoutes_NoRoutes(t *testing.T) {
	src := `
data class User(val id: Int, val name: String)

fun main() {
    println("hello")
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Main.kt", "kotlin", src))
	names := routeNames(ents)
	if len(names) != 0 {
		t.Errorf("expected no route entities in plain Kotlin file, got %v", names)
	}
}

func TestKtorRoutes_DedupSameRoute(t *testing.T) {
	// Duplicate route entries (e.g., same handler defined twice) should only
	// emit once due to seen-set deduplication.
	src := `
routing {
    get("/ping") { call.respond("pong") }
    get("/ping") { call.respond("pong2") }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Dup.kt", "kotlin", src))
	count := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Name == "GET /ping" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 GET /ping entity (deduped), got %d", count)
	}
}

func TestKtorRoutes_PathParameterPreserved(t *testing.T) {
	src := `
routing {
    route("/users") {
        get("/{id}") { call.respond("user") }
        put("/{id}") { call.respond("updated") }
    }
}
`
	ents := extract(t, "custom_kotlin_ktor_routes", fi("Users.kt", "kotlin", src))
	names := routeNames(ents)

	if !names["GET /users/{id}"] {
		t.Errorf("expected GET /users/{id}; got %v", names)
	}
	if !names["PUT /users/{id}"] {
		t.Errorf("expected PUT /users/{id}; got %v", names)
	}
}
