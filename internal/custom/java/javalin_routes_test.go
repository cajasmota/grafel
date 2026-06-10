package java

import (
	"strings"
	"testing"
)

// ============================================================================
// Issue #3085: Javalin route/handler/middleware/DTO extractor
// ============================================================================

// ----------------------------------------------------------------------------
// Routing: route_extraction + endpoint_synthesis + handler_attribution
// ----------------------------------------------------------------------------

// TestJavalin_RouteExtraction_Issue3085 proves that basic app.get/post/put/
// delete routes are extracted from a Javalin lambda DSL app.
// Registry target: lang.java.framework.javalin Routing/route_extraction → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_RouteExtraction_Issue3085(t *testing.T) {
	source := `
package com.example;

import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);

        app.get("/users", ctx -> {
            ctx.json(userService.findAll());
        });

        app.post("/users", ctx -> {
            var user = ctx.bodyAsClass(UserRequest.class);
            userService.create(user);
            ctx.status(201);
        });

        app.get("/users/{id}", ctx -> {
            var id = ctx.pathParam("id");
            ctx.json(userService.findById(id));
        });

        app.put("/users/{id}", ctx -> {
            var user = ctx.bodyAsClass(UserRequest.class);
            userService.update(ctx.pathParam("id"), user);
        });

        app.delete("/users/{id}", ctx -> {
            userService.delete(ctx.pathParam("id"));
            ctx.status(204);
        });
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_ROUTE" {
			key := e.Properties["http_verb"].(string) + ":" + e.Properties["path"].(string)
			routes[key] = true
		}
	}

	for _, want := range []string{
		"GET:/users",
		"POST:/users",
		"GET:/users/{id}",
		"PUT:/users/{id}",
		"DELETE:/users/{id}",
	} {
		if !routes[want] {
			t.Errorf("[#3085 route_extraction] expected route %q, got %v", want, routes)
		}
	}
}

// TestJavalin_HandlerAttribution_LambdaParam_Issue3085 proves that the lambda
// parameter name is captured as the handler for route attribution.
// Registry target: lang.java.framework.javalin Routing/handler_attribution → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_HandlerAttribution_LambdaParam_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.get("/orders", ctx -> ctx.json(orders));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_HANDLER" {
			found = true
			if e.Properties["framework"] != "javalin" {
				t.Errorf("[#3085 handler_attribution] expected framework=javalin, got %v", e.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3085 handler_attribution] expected INFERRED_FROM_JAVALIN_HANDLER entity")
	}
}

// TestJavalin_HandlerAttribution_MethodRef_Issue3085 proves that method
// references (ClassName::method) are extracted as handler attributions.
func TestJavalin_HandlerAttribution_MethodRef_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.get("/users", UserController::getAll);
        app.post("/users", UserController::create);
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	handlerNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_HANDLER" {
			handlerNames[e.Name] = true
		}
	}
	if !handlerNames["UserController::getAll"] {
		t.Errorf("[#3085 handler_attribution] expected UserController::getAll handler, got %v", handlerNames)
	}
	if !handlerNames["UserController::create"] {
		t.Errorf("[#3085 handler_attribution] expected UserController::create handler, got %v", handlerNames)
	}
}

// TestJavalin_RouteExtraction_Gating_Issue3085 confirms the extractor does not
// fire for non-javalin frameworks.
func TestJavalin_RouteExtraction_Gating_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;
public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.get("/users", ctx -> ctx.json(users));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "App.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3085 gating] expected 0 entities for framework=spring_boot, got %d", len(r.Entities))
	}
}

// TestJavalin_RouteExtraction_NoSignalGating_Issue3085 confirms the extractor
// no-ops on Java files with no Javalin signal.
func TestJavalin_RouteExtraction_NoSignalGating_Issue3085(t *testing.T) {
	source := `
public class OrderService {
    public Order findById(long id) { return null; }
}
`
	r := ExtractJavalin(PatternContext{
		Source: source, Language: "java", Framework: "javalin",
		FilePath: "OrderService.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3085 no-signal-gating] expected 0 entities for file without Javalin signal, got %d", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// Middleware: middleware_coverage — before/after handlers
// ----------------------------------------------------------------------------

// TestJavalin_Middleware_Before_Issue3085 proves that app.before() global
// middleware handlers are extracted.
// Registry target: lang.java.framework.javalin Middleware/middleware_coverage → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_Middleware_Before_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);

        // Global before handler — runs before every request
        app.before(ctx -> {
            ctx.header("X-Request-Id", java.util.UUID.randomUUID().toString());
        });

        // Path-scoped before handler
        app.before("/api/*", ctx -> {
            String token = ctx.header("Authorization");
            if (token == null) ctx.status(401).result("Unauthorized");
        });

        app.get("/users", ctx -> ctx.json(users));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	beforeCount := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_MIDDLEWARE" {
			middlewareType, _ := e.Properties["middleware_type"].(string)
			if middlewareType == "before" {
				beforeCount++
			}
		}
	}
	if beforeCount < 2 {
		t.Errorf("[#3085 middleware_coverage] expected >= 2 'before' middleware entities, got %d", beforeCount)
	}
}

// TestJavalin_Middleware_After_Issue3085 proves that app.after() middleware
// handlers are extracted.
func TestJavalin_Middleware_After_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.after(ctx -> {
            // log response
        });
        app.get("/ping", ctx -> ctx.result("pong"));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_MIDDLEWARE" {
			if t2, _ := e.Properties["middleware_type"].(string); t2 == "after" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("[#3085 middleware_coverage] expected INFERRED_FROM_JAVALIN_MIDDLEWARE entity with middleware_type=after")
	}
}

// TestJavalin_Middleware_FrameworkProperty_Issue3085 confirms that middleware
// entities carry framework=javalin.
func TestJavalin_Middleware_FrameworkProperty_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;
public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.before(ctx -> { /* noop */ });
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_MIDDLEWARE" {
			if e.Properties["framework"] != "javalin" {
				t.Errorf("[#3085 middleware] expected framework=javalin, got %v", e.Properties["framework"])
			}
			return
		}
	}
	t.Errorf("[#3085 middleware] expected at least one INFERRED_FROM_JAVALIN_MIDDLEWARE entity")
}

// ----------------------------------------------------------------------------
// Auth: auth_coverage — AccessManager + inline auth guard
// ----------------------------------------------------------------------------

// TestJavalin_Auth_AccessManager_Issue3085 proves that app.accessManager(...)
// is extracted as auth_coverage evidence.
// Registry target: lang.java.framework.javalin Auth/auth_coverage → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_Auth_AccessManager_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;
import io.javalin.security.AccessManager;
import io.javalin.security.RouteRole;

public class App {

    enum Role implements RouteRole { ADMIN, USER, ANYONE }

    public static void main(String[] args) {
        var app = Javalin.create(config -> {
            config.accessManager((handler, ctx, permittedRoles) -> {
                var userRole = getUserRole(ctx);
                if (permittedRoles.contains(userRole)) {
                    handler.handle(ctx);
                } else {
                    ctx.status(401).result("Unauthorized");
                }
            });
        }).start(7070);

        app.get("/admin", ctx -> ctx.result("admin"), Role.ADMIN);
        app.get("/profile", ctx -> ctx.result("profile"), Role.USER);
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_ACCESS_MANAGER" {
			found = true
			if e.Properties["auth_type"] != "access_manager" {
				t.Errorf("[#3085 auth_coverage] expected auth_type=access_manager, got %v", e.Properties["auth_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3085 auth_coverage] expected INFERRED_FROM_JAVALIN_ACCESS_MANAGER entity")
	}
}

// TestJavalin_Auth_InlineGuard_Issue3085 proves that inline auth guard patterns
// (Authorization header checks in before handlers) are captured.
func TestJavalin_Auth_InlineGuard_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.before("/api/*", ctx -> {
            String token = ctx.header("Authorization");
            if (token == null || !isValid(token)) {
                ctx.status(401).result("Unauthorized");
            }
        });
        app.get("/api/users", ctx -> ctx.json(users));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_AUTH_GUARD" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3085 auth_coverage] expected INFERRED_FROM_JAVALIN_AUTH_GUARD entity for Authorization header check")
	}
}

// ----------------------------------------------------------------------------
// Validation: dto_extraction + request_validation
// ----------------------------------------------------------------------------

// TestJavalin_DTO_BodyAsClass_Issue3085 proves that ctx.bodyAsClass(MyDto.class)
// is extracted as DTO evidence.
// Registry target: lang.java.framework.javalin Validation/dto_extraction → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_DTO_BodyAsClass_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;
import com.example.dto.CreateUserRequest;
import com.example.dto.UpdateOrderRequest;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);

        app.post("/users", ctx -> {
            var body = ctx.bodyAsClass(CreateUserRequest.class);
            userService.create(body);
            ctx.status(201);
        });

        app.put("/orders/{id}", ctx -> {
            var req = ctx.bodyAsClass(UpdateOrderRequest.class);
            orderService.update(ctx.pathParam("id"), req);
        });
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_DTO" {
			dtoNames[e.Name] = true
		}
	}
	if !dtoNames["CreateUserRequest"] {
		t.Errorf("[#3085 dto_extraction] expected CreateUserRequest DTO entity, got %v", dtoNames)
	}
	if !dtoNames["UpdateOrderRequest"] {
		t.Errorf("[#3085 dto_extraction] expected UpdateOrderRequest DTO entity, got %v", dtoNames)
	}
}

// TestJavalin_RequestValidation_BodyValidator_Issue3085 proves that
// ctx.bodyValidator(MyDto.class) is extracted as request_validation evidence.
// Registry target: lang.java.framework.javalin Validation/request_validation → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_RequestValidation_BodyValidator_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;
import com.example.dto.CreateProductRequest;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);

        app.post("/products", ctx -> {
            CreateProductRequest req = ctx.bodyValidator(CreateProductRequest.class)
                .check(it -> it.getName() != null, "Name is required")
                .check(it -> it.getPrice() > 0, "Price must be positive")
                .get();
            productService.create(req);
            ctx.status(201);
        });
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_VALIDATION" && e.Name == "CreateProductRequest" {
			found = true
			if e.Properties["request_validated"] != true {
				t.Errorf("[#3085 request_validation] expected request_validated=true, got %v", e.Properties["request_validated"])
			}
		}
	}
	if !found {
		t.Errorf("[#3085 request_validation] expected INFERRED_FROM_JAVALIN_VALIDATION entity for CreateProductRequest")
	}
}

// TestJavalin_DTO_PrimitiveSkip_Issue3085 confirms that primitive/framework
// types are not extracted as DTOs.
func TestJavalin_DTO_PrimitiveSkip_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.post("/echo", ctx -> {
            String body = ctx.bodyAsClass(String.class);
            ctx.result(body);
        });
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_DTO" && e.Name == "String" {
			t.Errorf("[#3085 dto_extraction] String is a primitive type and should not be extracted as DTO")
		}
	}
}

// ----------------------------------------------------------------------------
// Tests: tests_linkage — JavalinTest.create + @Test methods
// ----------------------------------------------------------------------------

// TestJavalin_TestsLinkage_JavalinTest_Issue3085 proves that JavalinTest.create
// is detected as test-infrastructure evidence.
// Registry target: lang.java.framework.javalin Testing/tests_linkage → partial.
// Cite: internal/custom/java/javalin_routes.go
func TestJavalin_TestsLinkage_JavalinTest_Issue3085(t *testing.T) {
	source := `
package com.example;

import io.javalin.Javalin;
import io.javalin.testtools.JavalinTest;
import org.junit.jupiter.api.Test;
import static org.assertj.core.api.Assertions.assertThat;

class UserControllerTest {

    Javalin app = App.createApp();

    @Test
    void getUsers_returns200() {
        JavalinTest.test(app, (server, client) -> {
            assertThat(client.get("/users").code()).isEqualTo(200);
        });
    }

    @Test
    void createUser_returns201() {
        JavalinTest.test(app, (server, client) -> {
            assertThat(client.post("/users").code()).isEqualTo(201);
        });
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "UserControllerTest.java",
	})

	foundSetup := false
	testCount := 0
	for _, e := range r.Entities {
		switch e.Provenance {
		case "INFERRED_FROM_JAVALIN_TEST_SETUP":
			foundSetup = true
		case "INFERRED_FROM_JAVALIN_TEST_METHOD":
			testCount++
		}
	}
	if !foundSetup {
		t.Errorf("[#3085 tests_linkage] expected INFERRED_FROM_JAVALIN_TEST_SETUP entity")
	}
	if testCount < 2 {
		t.Errorf("[#3085 tests_linkage] expected >= 2 @Test method entities, got %d", testCount)
	}
}

// TestJavalin_TestsLinkage_JUnit5_Issue3085 proves that ExtractJUnit5 runs
// for the "javalin" framework (javalin is in junit5Frameworks).
// Registry target: lang.java.framework.javalin Testing/tests_linkage → partial.
// Cite: internal/custom/java/junit5.go
func TestJavalin_TestsLinkage_JUnit5_Issue3085(t *testing.T) {
	source := `
import org.junit.jupiter.api.Test;
import io.javalin.Javalin;

class AppTest {
    @Test
    void ping() {}

    @Test
    void healthCheck() {}
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "AppTest.java",
	})
	if len(r.Entities) == 0 {
		t.Errorf("[#3085 tests_linkage] expected JUnit5 entities for framework=javalin, got none")
	}
	// #4359: per-@Test orphan nodes folded into the suite's test_method_count.
	testCount := suiteTestMethodCount(r)
	if testCount < 2 {
		t.Errorf("[#3085 tests_linkage] expected >= 2 @Test entities for javalin, got %d", testCount)
	}
}

// ----------------------------------------------------------------------------
// Comprehensive: full Javalin app
// ----------------------------------------------------------------------------

// TestJavalin_FullApp_Issue3085 verifies a realistic Javalin app with routes,
// middleware, auth, and DTOs all extracted correctly.
// This is the comprehensive proving test that justifies partial status for all
// 8 B cells.
func TestJavalin_FullApp_Issue3085(t *testing.T) {
	source := `
package com.example;

import io.javalin.Javalin;
import io.javalin.security.AccessManager;
import io.javalin.security.RouteRole;
import com.example.dto.CreateUserRequest;
import com.example.dto.UserResponse;

public class App {

    enum Role implements RouteRole { ADMIN, USER, ANYONE }

    public static Javalin createApp() {
        return Javalin.create(config -> {
            config.accessManager((handler, ctx, permittedRoles) -> {
                if (permittedRoles.contains(Role.ANYONE)) {
                    handler.handle(ctx);
                } else {
                    var user = ctx.attribute("user");
                    if (user == null) ctx.status(401).result("Unauthorized");
                    else handler.handle(ctx);
                }
            });
        });
    }

    public static void main(String[] args) {
        var app = createApp().start(7070);

        // Global request logger
        app.before(ctx -> {
            System.out.println(ctx.method() + " " + ctx.path());
        });

        // Response time header
        app.after(ctx -> {
            ctx.header("X-Response-Time", "...");
        });

        // Public routes
        app.get("/health", ctx -> ctx.result("UP"), Role.ANYONE);

        // Auth-required routes
        app.get("/users", ctx -> {
            ctx.json(userService.findAll());
        }, Role.USER);

        app.post("/users", ctx -> {
            var req = ctx.bodyAsClass(CreateUserRequest.class);
            var created = userService.create(req);
            ctx.status(201).json(created);
        }, Role.USER);

        app.get("/users/{id}", ctx -> {
            ctx.json(userService.findById(ctx.pathParam("id")));
        }, Role.USER);

        app.put("/users/{id}", ctx -> {
            var req = ctx.bodyValidator(CreateUserRequest.class)
                .check(it -> it.getName() != null, "Name required")
                .get();
            userService.update(ctx.pathParam("id"), req);
        }, Role.USER);

        app.delete("/users/{id}", ctx -> {
            userService.delete(ctx.pathParam("id"));
            ctx.status(204);
        }, Role.ADMIN);

        // 404 handler
        app.error(404, ctx -> ctx.result("Not found"));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	// Validate routes
	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_ROUTE" {
			key := e.Properties["http_verb"].(string) + ":" + e.Properties["path"].(string)
			routes[key] = true
		}
	}
	for _, want := range []string{
		"GET:/health",
		"GET:/users",
		"POST:/users",
		"GET:/users/{id}",
		"PUT:/users/{id}",
		"DELETE:/users/{id}",
	} {
		if !routes[want] {
			t.Errorf("[#3085 full-app] expected route %q, got %v", want, routes)
		}
	}

	// Validate middleware
	middlewareCount := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_MIDDLEWARE" {
			middlewareCount++
		}
	}
	if middlewareCount < 2 {
		t.Errorf("[#3085 full-app] expected >= 2 middleware entities (before+after), got %d", middlewareCount)
	}

	// Validate auth
	foundAuth := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_ACCESS_MANAGER" {
			foundAuth = true
		}
	}
	if !foundAuth {
		t.Errorf("[#3085 full-app] expected INFERRED_FROM_JAVALIN_ACCESS_MANAGER entity")
	}

	// Validate DTOs
	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_DTO" || e.Provenance == "INFERRED_FROM_JAVALIN_VALIDATION" {
			dtoNames[e.Name] = true
		}
	}
	if !dtoNames["CreateUserRequest"] {
		t.Errorf("[#3085 full-app] expected CreateUserRequest DTO, got %v", dtoNames)
	}
}

// ----------------------------------------------------------------------------
// AOP: not_applicable — Javalin has no AOP support
// ----------------------------------------------------------------------------

// TestJavalin_AOP_NotApplicable_Issue3085 confirms that Javalin does not have
// AOP (advice_attribution, aspect_extraction, pointcut_resolution are
// not_applicable). This test documents the negative assertion: the Spring AOP
// extractor must NOT fire for framework=javalin.
// Registry target: AOP/advice_attribution, AOP/aspect_extraction,
//
//	AOP/pointcut_resolution → not_applicable.
func TestJavalin_AOP_NotApplicable_Issue3085(t *testing.T) {
	source := `
package com.example;

import io.javalin.Javalin;
import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Before;

// Note: @Aspect is NOT a Javalin concept — this is here to test that
// Spring AOP extractor does not fire for javalin framework.
@Aspect
public class LoggingAspect {
    @Before("execution(* com.example.*.*(..))")
    public void logBefore() {}
}
`
	// Spring AOP extractor should not fire for javalin framework.
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "LoggingAspect.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3085 aop-not-applicable] Spring AOP extractor should not fire for framework=javalin, got %d entities", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// HANDLED_BY relationship emission
// ----------------------------------------------------------------------------

// TestJavalin_HandlerRelationship_Issue3085 proves that Route→Handler
// HANDLED_BY relationships are emitted when a handler is identifiable.
func TestJavalin_HandlerRelationship_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.get("/products", ProductController::listAll);
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	found := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "HANDLED_BY" {
			found = true
			if rel.Properties["framework"] != "javalin" {
				t.Errorf("[#3085 handler_attribution] expected framework=javalin on HANDLED_BY edge, got %v", rel.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3085 handler_attribution] expected HANDLED_BY relationship for method reference handler")
	}
}

// ----------------------------------------------------------------------------
// Synthesis integration: synthesizeJavalin in http_endpoint_synthesis.go
// ----------------------------------------------------------------------------

// TestJavalin_RouteProperties_Issue3085 confirms that route entities carry the
// expected properties that the synthesis pass consumes.
func TestJavalin_RouteProperties_Issue3085(t *testing.T) {
	source := `
import io.javalin.Javalin;
public class App {
    public static void main(String[] args) {
        var app = Javalin.create().start(7070);
        app.get("/api/v1/items/{itemId}", ctx -> ctx.json(items));
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "javalin",
		FilePath:  "App.java",
	})

	for _, e := range r.Entities {
		if e.Provenance != "INFERRED_FROM_JAVALIN_ROUTE" {
			continue
		}
		if e.Properties["framework"] != "javalin" {
			t.Errorf("[#3085] expected framework=javalin, got %v", e.Properties["framework"])
		}
		if e.Properties["http_verb"] != "GET" {
			t.Errorf("[#3085] expected http_verb=GET, got %v", e.Properties["http_verb"])
		}
		if e.Properties["path"] != "/api/v1/items/{itemId}" {
			t.Errorf("[#3085] expected path=/api/v1/items/{itemId}, got %v", e.Properties["path"])
		}
		if e.Properties["route_type"] != "lambda_dsl" {
			t.Errorf("[#3085] expected route_type=lambda_dsl, got %v", e.Properties["route_type"])
		}
		return
	}
	t.Errorf("[#3085] expected at least one INFERRED_FROM_JAVALIN_ROUTE entity")
}

// ----------------------------------------------------------------------------
// Helper to check for strings in entity properties (used in verbose checks)
// ----------------------------------------------------------------------------

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
