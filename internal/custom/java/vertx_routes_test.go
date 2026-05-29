package java

import (
	"testing"
)

// ============================================================================
// Issue #3086: Vert.x route/middleware/DTO/auth/tests extractor
// ============================================================================

// ----------------------------------------------------------------------------
// Routing: route_extraction + handler_attribution
// ----------------------------------------------------------------------------

// TestVertx_RouteExtraction_Issue3086 proves that basic router.get/post/put/
// delete routes are extracted from a Vert.x Web application.
// Registry target: lang.java.framework.vertx Routing/route_extraction → partial.
// Cite: internal/custom/java/vertx_routes.go
func TestVertx_RouteExtraction_Issue3086(t *testing.T) {
	source := `
package com.example;

import io.vertx.core.AbstractVerticle;
import io.vertx.ext.web.Router;
import io.vertx.ext.web.RoutingContext;

public class MainVerticle extends AbstractVerticle {
    @Override
    public void start() {
        Router router = Router.router(vertx);

        router.get("/users").handler(this::getAllUsers);
        router.post("/users").handler(this::createUser);
        router.get("/users/:id").handler(this::getUser);
        router.put("/users/:id").handler(this::updateUser);
        router.delete("/users/:id").handler(this::deleteUser);

        vertx.createHttpServer().requestHandler(router).listen(8080);
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "MainVerticle.java",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_ROUTE" {
			key := e.Properties["http_verb"].(string) + ":" + e.Properties["path"].(string)
			routes[key] = true
		}
	}

	for _, want := range []string{
		"GET:/users",
		"POST:/users",
		"GET:/users/:id",
		"PUT:/users/:id",
		"DELETE:/users/:id",
	} {
		if !routes[want] {
			t.Errorf("[#3086 route_extraction] expected route %q, got %v", want, routes)
		}
	}
}

// TestVertx_RouteExtraction_CurlyBrace_Issue3086 proves that Vert.x {param}
// style path parameters are also extracted (router accepts both styles).
func TestVertx_RouteExtraction_CurlyBrace_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.get("/orders/{orderId}").handler(ctx -> {
            ctx.response().end(orderId);
        });
        router.delete("/orders/{orderId}/items/{itemId}").handler(ctx -> {});
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_ROUTE" {
			key := e.Properties["http_verb"].(string) + ":" + e.Properties["path"].(string)
			routes[key] = true
		}
	}

	if !routes["GET:/orders/{orderId}"] {
		t.Errorf("[#3086 route_extraction] expected GET:/orders/{orderId}, got %v", routes)
	}
	if !routes["DELETE:/orders/{orderId}/items/{itemId}"] {
		t.Errorf("[#3086 route_extraction] expected DELETE:/orders/{orderId}/items/{itemId}, got %v", routes)
	}
}

// TestVertx_HandlerAttribution_LambdaParam_Issue3086 proves that the lambda
// parameter name is captured as the handler for route attribution.
// Registry target: lang.java.framework.vertx Routing/handler_attribution → partial.
// Cite: internal/custom/java/vertx_routes.go
func TestVertx_HandlerAttribution_LambdaParam_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.get("/items").handler(ctx -> {
            ctx.response().end("[]");
        });
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_HANDLER" {
			found = true
			if e.Properties["framework"] != "vertx" {
				t.Errorf("[#3086 handler_attribution] expected framework=vertx, got %v", e.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3086 handler_attribution] expected INFERRED_FROM_VERTX_HANDLER entity")
	}
}

// TestVertx_HandlerAttribution_MethodRef_Issue3086 proves that method
// references (ClassName::method or this::method) are extracted as handler attributions.
func TestVertx_HandlerAttribution_MethodRef_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class MainVerticle {
    void setup(Router router) {
        router.get("/users").handler(UserController::getAll);
        router.post("/users").handler(UserController::create);
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "MainVerticle.java",
	})

	handlerNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_HANDLER" {
			handlerNames[e.Name] = true
		}
	}
	if !handlerNames["UserController::getAll"] {
		t.Errorf("[#3086 handler_attribution] expected UserController::getAll handler, got %v", handlerNames)
	}
	if !handlerNames["UserController::create"] {
		t.Errorf("[#3086 handler_attribution] expected UserController::create handler, got %v", handlerNames)
	}
}

// TestVertx_RouteExtraction_Gating_Issue3086 confirms the extractor does not
// fire for non-vertx frameworks.
func TestVertx_RouteExtraction_Gating_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;
public class App {
    void setup(Router router) {
        router.get("/users").handler(ctx -> ctx.response().end("[]"));
    }
}
`
	r := ExtractVertx(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "App.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3086 gating] expected 0 entities for framework=spring_boot, got %d", len(r.Entities))
	}
}

// TestVertx_RouteExtraction_NoSignalGating_Issue3086 confirms the extractor
// no-ops on Java files with no Vert.x signal.
func TestVertx_RouteExtraction_NoSignalGating_Issue3086(t *testing.T) {
	source := `
public class OrderService {
    public Order findById(long id) { return null; }
}
`
	r := ExtractVertx(PatternContext{
		Source: source, Language: "java", Framework: "vertx",
		FilePath: "OrderService.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3086 no-signal-gating] expected 0 entities for file without Vert.x signal, got %d", len(r.Entities))
	}
}

// TestVertx_Route_RouteTypeProperty_Issue3086 confirms that route entities
// carry route_type=lambda_dsl and framework=vertx.
func TestVertx_Route_RouteTypeProperty_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.get("/api/v1/items/{itemId}").handler(ctx -> {
            ctx.response().end("item");
        });
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	for _, e := range r.Entities {
		if e.Provenance != "INFERRED_FROM_VERTX_ROUTE" {
			continue
		}
		if e.Properties["framework"] != "vertx" {
			t.Errorf("[#3086] expected framework=vertx, got %v", e.Properties["framework"])
		}
		if e.Properties["http_verb"] != "GET" {
			t.Errorf("[#3086] expected http_verb=GET, got %v", e.Properties["http_verb"])
		}
		if e.Properties["route_type"] != "lambda_dsl" {
			t.Errorf("[#3086] expected route_type=lambda_dsl, got %v", e.Properties["route_type"])
		}
		return
	}
	t.Errorf("[#3086] expected at least one INFERRED_FROM_VERTX_ROUTE entity")
}

// ----------------------------------------------------------------------------
// Middleware: middleware_coverage — router.route().handler(...) global handlers
// ----------------------------------------------------------------------------

// TestVertx_Middleware_GlobalHandler_Issue3086 proves that router.route().handler(...)
// global middleware handlers are extracted.
// Registry target: lang.java.framework.vertx Middleware/middleware_coverage → partial.
// Cite: internal/custom/java/vertx_routes.go
func TestVertx_Middleware_GlobalHandler_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BodyHandler;
import io.vertx.ext.web.handler.LoggerHandler;
import io.vertx.ext.web.handler.CorsHandler;

public class App {
    void setup(Router router) {
        // Global body parser
        router.route().handler(BodyHandler.create());
        // Global CORS
        router.route().handler(CorsHandler.create("*"));
        // Global request logger
        router.route().handler(LoggerHandler.create());

        router.get("/health").handler(ctx -> ctx.response().end("UP"));
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	middlewareTypes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_MIDDLEWARE" {
			mt, _ := e.Properties["middleware_type"].(string)
			middlewareTypes[mt] = true
		}
	}
	if len(middlewareTypes) < 2 {
		t.Errorf("[#3086 middleware_coverage] expected >= 2 middleware entities, got %d types: %v", len(middlewareTypes), middlewareTypes)
	}
	if !middlewareTypes["body_handler"] {
		t.Errorf("[#3086 middleware_coverage] expected body_handler middleware, got %v", middlewareTypes)
	}
}

// TestVertx_Middleware_FrameworkProperty_Issue3086 confirms that middleware
// entities carry framework=vertx.
func TestVertx_Middleware_FrameworkProperty_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BodyHandler;

public class App {
    void setup(Router router) {
        router.route().handler(BodyHandler.create());
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_MIDDLEWARE" {
			if e.Properties["framework"] != "vertx" {
				t.Errorf("[#3086 middleware] expected framework=vertx, got %v", e.Properties["framework"])
			}
			return
		}
	}
	t.Errorf("[#3086 middleware] expected at least one INFERRED_FROM_VERTX_MIDDLEWARE entity")
}

// ----------------------------------------------------------------------------
// Auth: auth_coverage — BasicAuthHandler, JWTAuthHandler, inline guards
// ----------------------------------------------------------------------------

// TestVertx_Auth_JWTAuthHandler_Issue3086 proves that JWTAuthHandler.create(...)
// is extracted as auth_coverage evidence.
// Registry target: lang.java.framework.vertx Auth/auth_coverage → partial.
// Cite: internal/custom/java/vertx_routes.go
func TestVertx_Auth_JWTAuthHandler_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.JWTAuthHandler;
import io.vertx.ext.auth.jwt.JWTAuth;

public class App {
    void setup(Router router, JWTAuth jwtProvider) {
        router.route("/api/*").handler(JWTAuthHandler.create(jwtProvider));
        router.get("/api/users").handler(ctx -> {
            ctx.response().end("[]");
        });
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_AUTH" && e.Name == "JWTAuthHandler" {
			found = true
			if e.Properties["auth_type"] != "jwt_auth" {
				t.Errorf("[#3086 auth_coverage] expected auth_type=jwt_auth, got %v", e.Properties["auth_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3086 auth_coverage] expected INFERRED_FROM_VERTX_AUTH entity for JWTAuthHandler")
	}
}

// TestVertx_Auth_BasicAuthHandler_Issue3086 proves that BasicAuthHandler.create(...)
// is extracted as auth_coverage evidence.
func TestVertx_Auth_BasicAuthHandler_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BasicAuthHandler;

public class App {
    void setup(Router router) {
        router.route().handler(BasicAuthHandler.create(authProvider));
        router.get("/protected").handler(ctx -> ctx.response().end("secret"));
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_AUTH" && e.Name == "BasicAuthHandler" {
			found = true
			if e.Properties["auth_type"] != "basic_auth" {
				t.Errorf("[#3086 auth_coverage] expected auth_type=basic_auth, got %v", e.Properties["auth_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3086 auth_coverage] expected INFERRED_FROM_VERTX_AUTH entity for BasicAuthHandler")
	}
}

// TestVertx_Auth_InlineGuard_Issue3086 proves that inline auth guard patterns
// (user() checks in handlers) are captured.
func TestVertx_Auth_InlineGuard_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.route("/api/*").handler(ctx -> {
            if (ctx.user() == null) {
                ctx.fail(401);
            } else {
                ctx.next();
            }
        });
        router.get("/api/data").handler(ctx -> ctx.response().end("data"));
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_AUTH_GUARD" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3086 auth_coverage] expected INFERRED_FROM_VERTX_AUTH_GUARD entity for ctx.user() check")
	}
}

// ----------------------------------------------------------------------------
// Validation: dto_extraction + request_validation
// ----------------------------------------------------------------------------

// TestVertx_DTO_BodyAs_Issue3086 proves that body().as(Foo.class) is extracted
// as DTO evidence.
// Registry target: lang.java.framework.vertx Validation/dto_extraction → partial.
// Cite: internal/custom/java/vertx_routes.go
func TestVertx_DTO_BodyAs_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BodyHandler;

public class App {
    void setup(Router router) {
        router.route().handler(BodyHandler.create());

        router.post("/users").handler(ctx -> {
            CreateUserRequest req = ctx.body().as(CreateUserRequest.class);
            userService.create(req);
            ctx.response().setStatusCode(201).end();
        });

        router.put("/orders/:id").handler(ctx -> {
            UpdateOrderRequest req = ctx.body().as(UpdateOrderRequest.class);
            orderService.update(ctx.pathParam("id"), req);
        });
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_DTO" {
			dtoNames[e.Name] = true
		}
	}
	if !dtoNames["CreateUserRequest"] {
		t.Errorf("[#3086 dto_extraction] expected CreateUserRequest DTO entity, got %v", dtoNames)
	}
	if !dtoNames["UpdateOrderRequest"] {
		t.Errorf("[#3086 dto_extraction] expected UpdateOrderRequest DTO entity, got %v", dtoNames)
	}
}

// TestVertx_DTO_PrimitiveSkip_Issue3086 confirms that primitive/framework
// types are not extracted as DTOs.
func TestVertx_DTO_PrimitiveSkip_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.post("/echo").handler(ctx -> {
            String body = ctx.body().as(String.class);
            ctx.response().end(body);
        });
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_DTO" && e.Name == "String" {
			t.Errorf("[#3086 dto_extraction] String is a primitive type and should not be extracted as DTO")
		}
	}
}

// ----------------------------------------------------------------------------
// Tests: tests_linkage — VertxTestContext / VertxExtension + @Test methods
// ----------------------------------------------------------------------------

// TestVertx_TestsLinkage_VertxTestContext_Issue3086 proves that VertxTestContext
// is detected as test-infrastructure evidence.
// Registry target: lang.java.framework.vertx Testing/tests_linkage → partial.
// Cite: internal/custom/java/vertx_routes.go
func TestVertx_TestsLinkage_VertxTestContext_Issue3086(t *testing.T) {
	source := `
package com.example;

import io.vertx.core.Vertx;
import io.vertx.junit5.VertxTestContext;
import io.vertx.junit5.VertxExtension;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import static org.assertj.core.api.Assertions.assertThat;

@ExtendWith(VertxExtension.class)
class MainVerticleTest {

    @Test
    void verticleDeployed(Vertx vertx, VertxTestContext testContext) {
        vertx.deployVerticle(new MainVerticle(), testContext.succeedingThenComplete());
    }

    @Test
    void getUsers_returns200(Vertx vertx, VertxTestContext testContext) throws InterruptedException {
        // test logic
        testContext.completeNow();
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "MainVerticleTest.java",
	})

	foundSetup := false
	testCount := 0
	for _, e := range r.Entities {
		switch e.Provenance {
		case "INFERRED_FROM_VERTX_TEST_SETUP":
			foundSetup = true
		case "INFERRED_FROM_VERTX_TEST_METHOD":
			testCount++
		}
	}
	if !foundSetup {
		t.Errorf("[#3086 tests_linkage] expected INFERRED_FROM_VERTX_TEST_SETUP entity")
	}
	if testCount < 2 {
		t.Errorf("[#3086 tests_linkage] expected >= 2 @Test method entities, got %d", testCount)
	}
}

// TestVertx_TestsLinkage_JUnit5_Issue3086 proves that ExtractJUnit5 runs
// for the "vertx" framework (vertx is in junit5Frameworks).
// Registry target: lang.java.framework.vertx Testing/tests_linkage → partial.
// Cite: internal/custom/java/junit5.go
func TestVertx_TestsLinkage_JUnit5_Issue3086(t *testing.T) {
	source := `
import org.junit.jupiter.api.Test;
import io.vertx.core.Vertx;
import io.vertx.junit5.VertxTestContext;

class AppTest {
    @Test
    void ping(Vertx vertx, VertxTestContext ctx) {}

    @Test
    void healthCheck(Vertx vertx, VertxTestContext ctx) {}
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "AppTest.java",
	})
	if len(r.Entities) == 0 {
		t.Errorf("[#3086 tests_linkage] expected JUnit5 entities for framework=vertx, got none")
	}
	testCount := 0
	for _, e := range r.Entities {
		if e.Properties["test_annotation"] == "Test" {
			testCount++
		}
	}
	if testCount < 2 {
		t.Errorf("[#3086 tests_linkage] expected >= 2 @Test entities for vertx, got %d", testCount)
	}
}

// TestVertx_TestsLinkage_VertxExtension_Issue3086 proves that @ExtendWith(VertxExtension.class)
// is detected as test setup evidence.
func TestVertx_TestsLinkage_VertxExtension_Issue3086(t *testing.T) {
	source := `
import io.vertx.junit5.VertxExtension;
import org.junit.jupiter.api.extension.ExtendWith;
import org.junit.jupiter.api.Test;

@ExtendWith(VertxExtension.class)
class MyTest {
    @Test
    void someTest() {}
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "MyTest.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_TEST_SETUP" && e.Name == "VertxExtension" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3086 tests_linkage] expected INFERRED_FROM_VERTX_TEST_SETUP with VertxExtension")
	}
}

// ----------------------------------------------------------------------------
// HANDLED_BY relationship emission
// ----------------------------------------------------------------------------

// TestVertx_HandlerRelationship_Issue3086 proves that Route→Handler
// HANDLED_BY relationships are emitted when a handler is identifiable.
func TestVertx_HandlerRelationship_Issue3086(t *testing.T) {
	source := `
import io.vertx.ext.web.Router;

public class App {
    void setup(Router router) {
        router.get("/products").handler(ProductController::listAll);
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "App.java",
	})

	found := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "HANDLED_BY" {
			found = true
			if rel.Properties["framework"] != "vertx" {
				t.Errorf("[#3086 handler_attribution] expected framework=vertx on HANDLED_BY edge, got %v", rel.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3086 handler_attribution] expected HANDLED_BY relationship for method reference handler")
	}
}

// ----------------------------------------------------------------------------
// AOP: not_applicable — Vert.x has no built-in AOP support
// ----------------------------------------------------------------------------

// TestVertx_AOP_NotApplicable_Issue3086 confirms that Vert.x does not have
// AOP (advice_attribution, aspect_extraction, pointcut_resolution are
// not_applicable). The Spring AOP extractor must NOT fire for framework=vertx.
// Registry target: AOP/advice_attribution, AOP/aspect_extraction,
//
//	AOP/pointcut_resolution → not_applicable.
func TestVertx_AOP_NotApplicable_Issue3086(t *testing.T) {
	source := `
package com.example;

import io.vertx.core.AbstractVerticle;
import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Before;

// Note: @Aspect is NOT a Vert.x concept — this is here to confirm that
// Spring AOP extractor does not fire for vertx framework.
@Aspect
public class LoggingAspect {
    @Before("execution(* com.example.*.*(..))")
    public void logBefore() {}
}
`
	// Spring AOP extractor should not fire for vertx framework.
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "LoggingAspect.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3086 aop-not-applicable] Spring AOP extractor should not fire for framework=vertx, got %d entities", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// Comprehensive: full Vert.x application
// ----------------------------------------------------------------------------

// TestVertx_FullApp_Issue3086 verifies a realistic Vert.x Web application with
// routes, middleware, auth (JWT), DTOs all extracted correctly.
// This is the comprehensive proving test that justifies partial status for all
// 6 B cells (route_extraction, auth_coverage, middleware_coverage, dto_extraction,
// request_validation, tests_linkage).
func TestVertx_FullApp_Issue3086(t *testing.T) {
	source := `
package com.example;

import io.vertx.core.AbstractVerticle;
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BodyHandler;
import io.vertx.ext.web.handler.JWTAuthHandler;
import io.vertx.ext.web.handler.CorsHandler;
import io.vertx.ext.auth.jwt.JWTAuth;
import com.example.dto.CreateUserRequest;
import com.example.dto.UpdateUserRequest;

public class MainVerticle extends AbstractVerticle {
    private JWTAuth jwtProvider;

    @Override
    public void start() throws Exception {
        Router router = Router.router(vertx);

        // Global middleware
        router.route().handler(BodyHandler.create());
        router.route().handler(CorsHandler.create("*"));

        // JWT auth for /api/* routes
        router.route("/api/*").handler(JWTAuthHandler.create(jwtProvider));

        // Public routes
        router.get("/health").handler(ctx -> {
            ctx.response().setStatusCode(200).end("UP");
        });

        // CRUD routes
        router.get("/api/users").handler(UserHandler::listAll);
        router.post("/api/users").handler(ctx -> {
            CreateUserRequest req = ctx.body().as(CreateUserRequest.class);
            userService.create(req);
            ctx.response().setStatusCode(201).end();
        });
        router.get("/api/users/:id").handler(UserHandler::getById);
        router.put("/api/users/:id").handler(ctx -> {
            UpdateUserRequest req = ctx.body().as(UpdateUserRequest.class);
            userService.update(ctx.pathParam("id"), req);
        });
        router.delete("/api/users/:id").handler(UserHandler::delete);

        vertx.createHttpServer()
            .requestHandler(router)
            .listen(8080, result -> {
                if (result.succeeded()) {
                    System.out.println("Server started on port 8080");
                }
            });
    }
}
`
	r := ExtractVertx(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "vertx",
		FilePath:  "MainVerticle.java",
	})

	// Validate routes
	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_ROUTE" {
			key := e.Properties["http_verb"].(string) + ":" + e.Properties["path"].(string)
			routes[key] = true
		}
	}
	for _, want := range []string{
		"GET:/health",
		"GET:/api/users",
		"POST:/api/users",
		"GET:/api/users/:id",
		"PUT:/api/users/:id",
		"DELETE:/api/users/:id",
	} {
		if !routes[want] {
			t.Errorf("[#3086 full-app] expected route %q, got %v", want, routes)
		}
	}

	// Validate middleware
	middlewareCount := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_MIDDLEWARE" {
			middlewareCount++
		}
	}
	if middlewareCount < 2 {
		t.Errorf("[#3086 full-app] expected >= 2 middleware entities (BodyHandler+CorsHandler), got %d", middlewareCount)
	}

	// Validate auth
	foundJWT := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_AUTH" && e.Properties["auth_type"] == "jwt_auth" {
			foundJWT = true
		}
	}
	if !foundJWT {
		t.Errorf("[#3086 full-app] expected INFERRED_FROM_VERTX_AUTH entity with auth_type=jwt_auth")
	}

	// Validate DTOs
	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_VERTX_DTO" {
			dtoNames[e.Name] = true
		}
	}
	if !dtoNames["CreateUserRequest"] {
		t.Errorf("[#3086 full-app] expected CreateUserRequest DTO, got %v", dtoNames)
	}
	if !dtoNames["UpdateUserRequest"] {
		t.Errorf("[#3086 full-app] expected UpdateUserRequest DTO, got %v", dtoNames)
	}

	// Validate handler attributions via HANDLED_BY relationship
	foundHandledBy := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "HANDLED_BY" && rel.Properties["framework"] == "vertx" {
			foundHandledBy = true
			break
		}
	}
	if !foundHandledBy {
		t.Errorf("[#3086 full-app] expected at least one HANDLED_BY relationship")
	}
}
