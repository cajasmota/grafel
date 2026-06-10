package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// #4384 — Spring WebFlux / WebMvc.fn FUNCTIONAL routing
// (`RouterFunction` / `route().GET("/x", handler)`) must capture endpoints AND
// link them to a handler, generalising the anon-handler fix #4324 to Spring
// functional routes.
//
// Before this fix synthesizeSpringWebFlux emitted endpoints with refKind="Route"
// and an EMPTY handler — discarding the handler argument entirely — so every
// functional route was a handler-less graph ISLAND regardless of handler shape.
//
// Handler-shape-agnostic linkage (the root fix):
//   - lambda handler   (`req -> ServerResponse.ok()...`) → inline-handler synth
//     + merge-stable file-scoped IMPLEMENTS bridge (#4324 mechanism).
//   - method reference (`this::create`, `handler::getUser`) → resolve to the
//     NAMED method symbol via the #4319 synthesis-time structural bridge — NOT
//     an inline stand-in, because it is a real symbol.
//   - `.nest(path("/api"), ...)` composes its prefix onto inner routes.

// namedHandlerBridgedTo proves the endpoint (verb, path) has a resolved inbound
// IMPLEMENTS edge from the same-file named handler method `methodName` — i.e. it
// took the #4319 named-bridge path and is NOT an island, and did NOT synthesize
// an inline stand-in.
func assertNamedMethodBridged(t *testing.T, ents []types.EntityRecord, rels []types.RelationshipRecord, srcFile, verb, path, methodName string) {
	t.Helper()

	endpoint := endpointByVerbPath(ents, verb, path)
	if endpoint == nil {
		t.Fatalf("endpoint %s %s NOT emitted", verb, path)
	}
	// A method-reference handler is a real symbol: it must NOT get an inline
	// stand-in.
	if inlineHandlerEntity(ents, verb, path) != nil {
		t.Errorf("method-ref handler for %s %s must NOT synthesize an inline-handler stand-in", verb, path)
	}

	// In the LIVE pipeline the handler method symbol (`methodName`) is extracted
	// by the base Java symbol extractor and merged into the graph alongside the
	// engine-synthesised endpoint. detectInline only returns engine-emitted
	// entities, so we inject the same-file handler-method Operation the base
	// extractor would produce, exactly as it lands in buildDocument, before
	// running the resolve passes.
	ents = append(ents, types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       methodName,
		Subtype:    "method",
		SourceFile: srcFile,
		Language:   "java",
	})

	merged, _ := ResolveHTTPEndpointHandlers(ents)
	for i := range merged {
		merged[i].ID = merged[i].ComputeID()
	}
	ep := endpointByVerbPath(merged, verb, path)
	if ep == nil {
		t.Fatalf("endpoint %s %s lost after http-endpoint resolve pass", verb, path)
	}
	// Find the named handler Operation method in the merged set.
	var handlerID string
	for i := range merged {
		if merged[i].Name == methodName &&
			(merged[i].Kind == "SCOPE.Operation" || merged[i].Kind == "Operation") {
			handlerID = merged[i].ID
		}
	}
	if handlerID == "" {
		t.Fatalf("named handler method %q not found in merged entities for %s %s", methodName, verb, path)
	}

	idx := resolve.BuildIndex(merged)
	resolve.References(rels, idx)

	for _, r := range rels {
		if r.Kind == implementsEdgeKind && r.ToID == ep.ID && r.FromID == handlerID {
			return
		}
	}
	t.Fatalf("ISLAND: method-ref endpoint %s %s has no resolved IMPLEMENTS edge from named handler %q (#4384)", verb, path, methodName)
}

// TestSpringFn4384_LambdaHandlers covers the chained builder + predicate-builder
// forms with LAMBDA handlers — each must produce an inline-handler synth + bridge.
func TestSpringFn4384_LambdaHandlers(t *testing.T) {
	src := `package com.example;

import org.springframework.web.reactive.function.server.RouterFunction;
import org.springframework.web.reactive.function.server.RouterFunctions;
import org.springframework.web.reactive.function.server.ServerResponse;
import static org.springframework.web.reactive.function.server.RequestPredicates.GET;
import static org.springframework.web.reactive.function.server.RequestPredicates.POST;

@Configuration
public class LambdaRouter {
    @Bean
    public RouterFunction<ServerResponse> routes() {
        return RouterFunctions.route()
                .GET("/health", req -> ServerResponse.ok().build())
                .POST("/items", request -> ServerResponse.status(201).build())
                .build();
    }

    @Bean
    public RouterFunction<ServerResponse> predicateRoutes() {
        return RouterFunctions
                .route(GET("/ping"), req -> ServerResponse.ok().build())
                .andRoute(POST("/orders"), req -> ServerResponse.status(201).build());
    }
}
`
	ents, rels := detectInline(t, "java", "src/main/java/com/example/LambdaRouter.java", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/health", "spring_webflux")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/items", "spring_webflux")
	assertInlineEndpointBridged(t, ents, rels, "GET", "/ping", "spring_webflux")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/orders", "spring_webflux")
}

// TestSpringFn4384_MethodRefHandlers covers method-reference handlers — these
// resolve to the NAMED same-file method, NOT an inline stand-in.
func TestSpringFn4384_MethodRefHandlers(t *testing.T) {
	src := `package com.example;

import org.springframework.web.reactive.function.server.RouterFunction;
import org.springframework.web.reactive.function.server.RouterFunctions;
import org.springframework.web.reactive.function.server.ServerRequest;
import org.springframework.web.reactive.function.server.ServerResponse;
import reactor.core.publisher.Mono;

@Configuration
public class UserRouterConfig {
    @Bean
    public RouterFunction<ServerResponse> routes() {
        return RouterFunctions.route()
                .GET("/users", this::listUsers)
                .POST("/users", this::createUser)
                .GET("/users/{id}", this::getUser)
                .build();
    }

    public Mono<ServerResponse> listUsers(ServerRequest req) { return ServerResponse.ok().build(); }
    public Mono<ServerResponse> createUser(ServerRequest req) { return ServerResponse.status(201).build(); }
    public Mono<ServerResponse> getUser(ServerRequest req) { return ServerResponse.ok().build(); }
}
`
	const srcFile = "src/main/java/com/example/UserRouterConfig.java"
	ents, rels := detectInline(t, "java", srcFile, src)
	assertNamedMethodBridged(t, ents, rels, srcFile, "GET", "/users", "listUsers")
	assertNamedMethodBridged(t, ents, rels, srcFile, "POST", "/users", "createUser")
	assertNamedMethodBridged(t, ents, rels, srcFile, "GET", "/users/{id}", "getUser")
}

// TestSpringFn4384_NestedPrefix covers `.nest(path("/api"), ...)` prefix
// composition — inner routes must carry the nest prefix.
func TestSpringFn4384_NestedPrefix(t *testing.T) {
	src := `package com.example;

import org.springframework.web.reactive.function.server.RouterFunction;
import org.springframework.web.reactive.function.server.RouterFunctions;
import org.springframework.web.reactive.function.server.ServerResponse;
import static org.springframework.web.reactive.function.server.RequestPredicates.path;

@Configuration
public class NestedRouter {
    @Bean
    public RouterFunction<ServerResponse> routes() {
        return RouterFunctions.route()
                .nest(path("/api"), builder -> builder
                        .GET("/widgets", req -> ServerResponse.ok().build())
                        .POST("/widgets", req -> ServerResponse.status(201).build()))
                .build();
    }
}
`
	ents, rels := detectInline(t, "java", "src/main/java/com/example/NestedRouter.java", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/api/widgets", "spring_webflux")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/api/widgets", "spring_webflux")
	// The un-prefixed path must NOT exist — proves the nest prefix composed.
	if endpointByVerbPath(ents, "GET", "/widgets") != nil {
		t.Error("un-prefixed /widgets emitted; nest prefix did not compose")
	}
}
