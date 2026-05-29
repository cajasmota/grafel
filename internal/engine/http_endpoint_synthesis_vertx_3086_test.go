package engine

// Vert.x endpoint synthesis tests for issue #3086.
// These tests verify that synthesizeVertx in http_endpoint_synthesis.go
// correctly emits http_endpoint_definition entities for Vert.x Web router routes.

import (
	"testing"
)

// TestSynth_Vertx_BasicRoutes_Issue3086 verifies that Vert.x Web router
// route registrations are synthesised into http_endpoint_definition entities
// with correct (verb, canonical-path) IDs.
// Registry target: lang.java.framework.vertx Routing/endpoint_synthesis → partial.
// Cite: internal/engine/http_endpoint_synthesis.go (synthesizeVertx)
func TestSynth_Vertx_BasicRoutes_Issue3086(t *testing.T) {
	src := `package com.example;

import io.vertx.core.AbstractVerticle;
import io.vertx.ext.web.Router;

public class MainVerticle extends AbstractVerticle {
    @Override
    public void start() {
        Router router = Router.router(vertx);

        router.get("/users").handler(ctx -> ctx.response().end("[]"));
        router.post("/users").handler(ctx -> {
            ctx.response().setStatusCode(201).end();
        });
        router.get("/users/:id").handler(ctx -> ctx.response().end("user"));
        router.put("/users/:id").handler(ctx -> ctx.response().end());
        router.delete("/users/:id").handler(ctx -> ctx.response().setStatusCode(204).end());

        vertx.createHttpServer().requestHandler(router).listen(8080);
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/MainVerticle.java", src)
	want := []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:GET:/users/:id",
		"http:PUT:/users/:id",
		"http:DELETE:/users/:id",
	}
	requireContains(t, got, want, "Vert.x basic routes")
}

// TestSynth_Vertx_PathParams_Issue3086 verifies that Vert.x {param}
// curly-brace path parameters are canonicalised correctly.
func TestSynth_Vertx_PathParams_Issue3086(t *testing.T) {
	src := `package com.example;

import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.get("/orgs/{orgId}/projects/{projectId}").handler(ctx -> ctx.response().end("ok"));
        router.patch("/items/{itemId}/status").handler(ctx -> ctx.response().setStatusCode(200).end());
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/App.java", src)
	want := []string{
		"http:GET:/orgs/{orgId}/projects/{projectId}",
		"http:PATCH:/items/{itemId}/status",
	}
	requireContains(t, got, want, "Vert.x path params")
}

// TestSynth_Vertx_NoSignalNoOp_Issue3086 verifies that the synthesizer
// no-ops on Java files without any Vert.x signal.
func TestSynth_Vertx_NoSignalNoOp_Issue3086(t *testing.T) {
	src := `package com.example;

public class OrderService {
    public Order findById(long id) { return null; }
    public void save(Order o) {}
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/OrderService.java", src)
	for _, id := range got {
		// We expect zero Vert.x synthetics. Any http: ID here would come from
		// JAX-RS or Spring, but this file has neither.
		if len(id) > 5 && id[:5] == "http:" {
			t.Errorf("[#3086 no-signal-noop] unexpected http entity %q in non-Vert.x file", id)
		}
	}
}

// TestSynth_Vertx_CoexistsWithJavalin_Issue3086 verifies that Vert.x and
// Javalin synthetics can coexist in the same Java case without contaminating
// each other (dedup isolation).
func TestSynth_Vertx_CoexistsWithJavalin_Issue3086(t *testing.T) {
	src := `package com.example;

import io.vertx.ext.web.Router;
import io.javalin.Javalin;

// Hypothetical file with both Vert.x router and Javalin routes.
// In practice these would not coexist, but tests dedup isolation.
class VertxApp {
    void setup(Router router) {
        router.get("/vertx-items").handler(ctx -> ctx.response().end("[]"));
    }
}

class JavalinApp {
    static void register(Javalin app) {
        app.get("/javalin-items", ctx -> ctx.result("[]"));
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/App.java", src)
	want := []string{
		"http:GET:/vertx-items",
		"http:GET:/javalin-items",
	}
	requireContains(t, got, want, "Vert.x+Javalin coexistence")
}
