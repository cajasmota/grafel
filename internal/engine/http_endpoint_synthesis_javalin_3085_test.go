package engine

// Javalin endpoint synthesis tests for issue #3085.
// These tests verify that synthesizeJavalin in http_endpoint_synthesis.go
// correctly emits http_endpoint_definition entities for Javalin lambda-DSL routes.

import (
	"testing"
)

// TestSynth_Javalin_BasicRoutes_Issue3085 verifies that Javalin lambda-DSL
// route registrations are synthesised into http_endpoint_definition entities
// with correct (verb, canonical-path) IDs.
// Registry target: lang.java.framework.javalin Routing/endpoint_synthesis → partial.
// Cite: internal/engine/http_endpoint_synthesis.go (synthesizeJavalin)
func TestSynth_Javalin_BasicRoutes_Issue3085(t *testing.T) {
	src := `package com.example;

import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);

        app.get("/users", ctx -> ctx.json(userService.findAll()));
        app.post("/users", ctx -> {
            ctx.status(201);
        });
        app.get("/users/{id}", ctx -> ctx.json(userService.findById(ctx.pathParam("id"))));
        app.put("/users/{id}", ctx -> userService.update(ctx.pathParam("id"), ctx.body()));
        app.delete("/users/{id}", ctx -> {
            userService.delete(ctx.pathParam("id"));
            ctx.status(204);
        });
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/App.java", src)
	want := []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:GET:/users/{id}",
		"http:PUT:/users/{id}",
		"http:DELETE:/users/{id}",
	}
	requireContains(t, got, want, "Javalin basic routes")
}

// TestSynth_Javalin_PathParams_Issue3085 verifies that Javalin {param}
// curly-brace path parameters are canonicalised correctly.
func TestSynth_Javalin_PathParams_Issue3085(t *testing.T) {
	src := `package com.example;

import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.get("/orgs/{orgId}/projects/{projectId}", ctx -> ctx.json("ok"));
        app.patch("/items/{itemId}/status", ctx -> ctx.status(200));
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/App.java", src)
	want := []string{
		"http:GET:/orgs/{orgId}/projects/{projectId}",
		"http:PATCH:/items/{itemId}/status",
	}
	requireContains(t, got, want, "Javalin path params")
}

// TestSynth_Javalin_NoSignalNoOp_Issue3085 verifies that the synthesizer
// no-ops on Java files without any Javalin signal.
func TestSynth_Javalin_NoSignalNoOp_Issue3085(t *testing.T) {
	src := `package com.example;

public class OrderService {
    public Order findById(long id) { return null; }
    public void save(Order o) {}
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/OrderService.java", src)
	for _, id := range got {
		// We expect zero Javalin synthetics
		if len(id) > 5 && id[:5] == "http:" {
			// Any http: ID here would have to come from JAX-RS or Spring, not Javalin.
			// This file has neither, so any http: entity is a failure.
			t.Errorf("[#3085 no-signal-noop] unexpected http entity %q in non-Javalin file", id)
		}
	}
}

// TestSynth_Javalin_CoexistsWithJAXRS_Issue3085 verifies that Javalin and
// JAX-RS synthetics can coexist in the same file without cross-contamination.
// (Unusual in practice, but tests dedup isolation.)
func TestSynth_Javalin_CoexistsWithJAXRS_Issue3085(t *testing.T) {
	src := `package com.example;

import io.javalin.Javalin;
import jakarta.ws.rs.GET;
import jakarta.ws.rs.Path;

// Hypothetical file that has both Javalin routes and JAX-RS annotations.
// In practice these would not coexist, but we verify the synthesisers
// produce distinct IDs without interfering.

@Path("/jaxrs-users")
public class HybridResource {
    @GET
    public String getUsers() { return "[]"; }
}

class JavalinApp {
    static void register(Javalin app) {
        app.get("/javalin-items", ctx -> ctx.result("[]"));
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/HybridResource.java", src)
	want := []string{
		"http:GET:/jaxrs-users",
		"http:GET:/javalin-items",
	}
	requireContains(t, got, want, "Javalin+JAX-RS coexistence")
}
