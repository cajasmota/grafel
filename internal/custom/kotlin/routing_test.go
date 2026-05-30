package kotlin_test

// routing_test.go — tests for Spring Boot, Micronaut, Quarkus, and http4k
// route extractors.
//
// Issue #3275 — Part of Kotlin routing + ORM-depth builds.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Spring Boot
// ---------------------------------------------------------------------------

func TestSpringRoutes_BasicComposition(t *testing.T) {
	src := `
package com.example

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api")
class OrderController {
    @GetMapping("/orders")
    fun listOrders() = emptyList<Any>()

    @PostMapping("/orders")
    fun createOrder(): String = "ok"

    @PutMapping("/orders/{id}")
    fun updateOrder(): String = "ok"

    @DeleteMapping("/orders/{id}")
    fun deleteOrder() {}

    @PatchMapping("/orders/{id}")
    fun patchOrder() {}
}
`
	ents := extract(t, "custom_kotlin_spring_routes", fi("OrderController.kt", "kotlin", src))
	want := []string{
		"GET /api/orders",
		"POST /api/orders",
		"PUT /api/orders/{id}",
		"DELETE /api/orders/{id}",
		"PATCH /api/orders/{id}",
	}
	names := routeNames(ents)
	for _, w := range want {
		if !names[w] {
			t.Errorf("spring-boot: expected route %q; got %v", w, names)
		}
	}
}

func TestSpringRoutes_NoController(t *testing.T) {
	src := `data class User(val name: String)`
	ents := extract(t, "custom_kotlin_spring_routes", fi("User.kt", "kotlin", src))
	if len(routeNames(ents)) != 0 {
		t.Errorf("spring-boot: expected no routes in plain data class, got %v", routeNames(ents))
	}
}

func TestSpringRoutes_WrongLanguage(t *testing.T) {
	src := `@RestController @RequestMapping("/api") class Foo { @GetMapping("/x") fun x() {} }`
	ents := extract(t, "custom_kotlin_spring_routes", fi("Foo.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("spring-boot: expected no entities for java language, got %d", len(ents))
	}
}

func TestSpringRoutes_NoClassPrefix(t *testing.T) {
	// Without @RequestMapping on the class the extractor still emits method paths.
	src := `
@RestController
class HealthController {
    @GetMapping("/health")
    fun health(): String = "ok"
}
`
	ents := extract(t, "custom_kotlin_spring_routes", fi("Health.kt", "kotlin", src))
	names := routeNames(ents)
	if !names["GET /health"] {
		t.Errorf("spring-boot: expected GET /health (no class prefix); got %v", names)
	}
}

// ---------------------------------------------------------------------------
// Micronaut
// ---------------------------------------------------------------------------

func TestMicronautRoutes_BasicComposition(t *testing.T) {
	src := `
import io.micronaut.http.annotation.*

@Controller("/products")
class ProductController {
    @Get("/")
    fun list(): List<String> = emptyList()

    @Post("/")
    fun create(): String = "created"

    @Get("/{id}")
    fun get(): String = "item"

    @Delete("/{id}")
    fun delete() {}
}
`
	ents := extract(t, "custom_kotlin_micronaut_routes", fi("ProductController.kt", "kotlin", src))
	names := routeNames(ents)
	want := []string{
		"GET /products/",
		"POST /products/",
		"GET /products/{id}",
		"DELETE /products/{id}",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("micronaut: expected %q; got %v", w, names)
		}
	}
}

func TestMicronautRoutes_NoController(t *testing.T) {
	src := `class NotAController { fun hello() = "hi" }`
	ents := extract(t, "custom_kotlin_micronaut_routes", fi("Plain.kt", "kotlin", src))
	if len(routeNames(ents)) != 0 {
		t.Errorf("micronaut: expected no routes, got %v", routeNames(ents))
	}
}

func TestMicronautRoutes_WrongLanguage(t *testing.T) {
	src := `@Controller("/x") class Foo { @Get("/y") fun y() {} }`
	ents := extract(t, "custom_kotlin_micronaut_routes", fi("Foo.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("micronaut: expected no entities for java language, got %d", len(ents))
	}
}

func TestMicronautRoutes_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_micronaut_routes", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("micronaut: expected no entities for empty content, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Quarkus
// ---------------------------------------------------------------------------

func TestQuarkusRoutes_BasicJAXRS(t *testing.T) {
	src := `
import javax.ws.rs.*

@Path("/items")
class ItemResource {
    @GET
    fun list(): List<String> = emptyList()

    @POST
    fun create(): String = "created"
}
`
	ents := extract(t, "custom_kotlin_quarkus_routes", fi("ItemResource.kt", "kotlin", src))
	names := routeNames(ents)
	if !names["GET /items"] {
		t.Errorf("quarkus: expected GET /items; got %v", names)
	}
	if !names["POST /items"] {
		t.Errorf("quarkus: expected POST /items; got %v", names)
	}
}

func TestQuarkusRoutes_SubPath(t *testing.T) {
	src := `
@Path("/orders")
class OrderResource {
    @Path("/{id}")
    @GET
    fun get(): String = "ok"

    @GET
    @Path("/active")
    fun listActive(): List<String> = emptyList()
}
`
	ents := extract(t, "custom_kotlin_quarkus_routes", fi("OrderResource.kt", "kotlin", src))
	names := routeNames(ents)
	if !names["GET /orders/{id}"] {
		t.Errorf("quarkus: expected GET /orders/{id}; got %v", names)
	}
	if !names["GET /orders/active"] {
		t.Errorf("quarkus: expected GET /orders/active; got %v", names)
	}
}

func TestQuarkusRoutes_NoPath(t *testing.T) {
	src := `class NotAResource { fun hello() = "hi" }`
	ents := extract(t, "custom_kotlin_quarkus_routes", fi("Plain.kt", "kotlin", src))
	if len(routeNames(ents)) != 0 {
		t.Errorf("quarkus: expected no routes, got %v", routeNames(ents))
	}
}

func TestQuarkusRoutes_WrongLanguage(t *testing.T) {
	src := `@Path("/x") class Foo { @GET fun g() {} }`
	ents := extract(t, "custom_kotlin_quarkus_routes", fi("Foo.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("quarkus: expected no entities for java language, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// http4k
// ---------------------------------------------------------------------------

func TestHttp4kRoutes_FlatBind(t *testing.T) {
	src := `
val app = routes(
    "/ping" bind GET to ::pingHandler,
    "/users" bind POST to ::createUser,
    "/users/{id}" bind DELETE to ::deleteUser,
)
`
	ents := extract(t, "custom_kotlin_http4k_routes", fi("App.kt", "kotlin", src))
	names := routeNames(ents)
	if !names["GET /ping"] {
		t.Errorf("http4k: expected GET /ping; got %v", names)
	}
	if !names["POST /users"] {
		t.Errorf("http4k: expected POST /users; got %v", names)
	}
	if !names["DELETE /users/{id}"] {
		t.Errorf("http4k: expected DELETE /users/{id}; got %v", names)
	}
}

func TestHttp4kRoutes_NestedBind(t *testing.T) {
	src := `
val app = routes(
    "/api" bind routes(
        "/users" bind GET to ::listUsers,
        "/users" bind POST to ::createUser,
    ),
)
`
	ents := extract(t, "custom_kotlin_http4k_routes", fi("Nested.kt", "kotlin", src))
	// Nested "/api" bind routes( … ) must compose onto the leaf paths.
	names := routeNames(ents)
	if !names["GET /api/users"] {
		t.Errorf("http4k: expected composed GET /api/users in nested; got %v", names)
	}
	if !names["POST /api/users"] {
		t.Errorf("http4k: expected composed POST /api/users in nested; got %v", names)
	}
}

func TestHttp4kRoutes_NestedTwoLevels(t *testing.T) {
	src := `
val app = routes(
    "/api" bind routes(
        "/v1" bind routes(
            "/users" bind GET to ::listUsers,
            "/users/{id}" bind DELETE to ::deleteUser,
        ),
    ),
    "/ping" bind GET to ::ping,
)
`
	ents := extract(t, "custom_kotlin_http4k_routes", fi("DeepNested.kt", "kotlin", src))
	names := routeNames(ents)
	want := []string{
		"GET /api/v1/users",
		"DELETE /api/v1/users/{id}",
		"GET /ping",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("http4k: expected composed route %q; got %v", w, names)
		}
	}
}

func TestHttp4kRoutes_NoRoutes(t *testing.T) {
	src := `data class User(val name: String)`
	ents := extract(t, "custom_kotlin_http4k_routes", fi("User.kt", "kotlin", src))
	if len(routeNames(ents)) != 0 {
		t.Errorf("http4k: expected no routes, got %v", routeNames(ents))
	}
}

func TestHttp4kRoutes_WrongLanguage(t *testing.T) {
	src := `val app = routes("/ping" bind GET to ::ping)`
	ents := extract(t, "custom_kotlin_http4k_routes", fi("App.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("http4k: expected no entities for java language, got %d", len(ents))
	}
}

func TestHttp4kRoutes_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_http4k_routes", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("http4k: expected no entities for empty content, got %d", len(ents))
	}
}
