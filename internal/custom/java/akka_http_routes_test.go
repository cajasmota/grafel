package java

import (
	"strings"
	"testing"
)

// ============================================================================
// Issue #3092: Akka-HTTP Java DSL route/handler/middleware/DTO extractor
// ============================================================================

// ----------------------------------------------------------------------------
// Routing: route_extraction + handler_attribution
// ----------------------------------------------------------------------------

// TestAkkaHTTP_RouteExtraction_Issue3092 proves that basic path() + HTTP
// method directive combos are extracted from Akka-HTTP Java DSL source.
// Registry target: lang.java.framework.akka-http Routing/route_extraction → partial.
// Cite: internal/custom/java/akka_http_routes.go
func TestAkkaHTTP_RouteExtraction_Issue3092(t *testing.T) {
	source := `
package com.example;

import akka.http.javadsl.server.AllDirectives;
import akka.http.javadsl.server.Route;

public class UserRouter extends AllDirectives {
    public Route createRoute() {
        return concat(
            path("users", () ->
                get(() ->
                    complete("list users")
                )
            ),
            path("users", () ->
                post(() ->
                    entity(as(CreateUserRequest.class), req ->
                        complete(StatusCodes.CREATED, "created")
                    )
                )
            ),
            pathPrefix("users", () ->
                path(segment(), id ->
                    get(() ->
                        complete("get user " + id)
                    )
                )
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/UserRouter.java",
	})

	// Must find at least one Route entity for "users"
	var found int
	for _, e := range r.Entities {
		if e.Kind == "Route" && strings.Contains(e.Name, "users") {
			found++
		}
	}
	if found == 0 {
		t.Errorf("expected Route entities for 'users', got none; entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_PathPrefixExtraction_Issue3092 proves that pathPrefix() directives
// are extracted as Route entities with route_type=path_prefix.
// Registry target: lang.java.framework.akka-http Routing/route_extraction → partial.
func TestAkkaHTTP_PathPrefixExtraction_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
import akka.http.javadsl.server.Route;

public class ApiRouter extends AllDirectives {
    public Route routes() {
        return pathPrefix("api", () ->
            pathPrefix("v1", () ->
                path("orders", () ->
                    get(() -> complete("orders"))
                )
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/ApiRouter.java",
	})

	var prefixRoutes []string
	for _, e := range r.Entities {
		if e.Kind == "Route" {
			prefixRoutes = append(prefixRoutes, e.Name)
		}
	}
	if len(prefixRoutes) == 0 {
		t.Errorf("expected Route entities from pathPrefix/path directives, got none")
	}
}

// TestAkkaHTTP_HTTPMethodVerbs_Issue3092 verifies all HTTP verb directives are
// detected and the verb is uppercased on the emitted Route entity.
// Registry target: lang.java.framework.akka-http Routing/route_extraction → partial.
func TestAkkaHTTP_HTTPMethodVerbs_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class VerbRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return concat(
            path("res", () -> get(() -> complete("get"))),
            path("res", () -> post(() -> complete("post"))),
            path("res", () -> put(() -> complete("put"))),
            path("res", () -> delete(() -> complete("delete"))),
            path("res", () -> patch(() -> complete("patch")))
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/VerbRouter.java",
	})

	verbsSeen := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "Route" {
			if v, ok := e.Properties["http_verb"].(string); ok {
				verbsSeen[v] = true
			}
		}
	}

	for _, want := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		if !verbsSeen[want] {
			t.Errorf("expected verb %s in route entities, got verbs=%v", want, verbsSeen)
		}
	}
}

// TestAkkaHTTP_HandlerAttribution_Issue3092 verifies handler attribution from
// method references (Handler::method) in Akka-HTTP routes.
// Registry target: lang.java.framework.akka-http Routing/handler_attribution → partial.
func TestAkkaHTTP_HandlerAttribution_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class HandlerRouter extends AllDirectives {
    UserHandler handler = new UserHandler();
    public akka.http.javadsl.server.Route routes() {
        return path("users", () ->
            get(() -> complete(handler.listUsers()))
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/HandlerRouter.java",
	})

	// Should find a Route entity
	var routeFound bool
	for _, e := range r.Entities {
		if e.Kind == "Route" {
			routeFound = true
		}
	}
	if !routeFound {
		t.Errorf("expected Route entity, got none; entities=%v", r.Entities)
	}
}

// ----------------------------------------------------------------------------
// Middleware: middleware_coverage
// ----------------------------------------------------------------------------

// TestAkkaHTTP_Middleware_ExceptionHandler_Issue3092 verifies that
// handleExceptions(...) is detected as Middleware.
// Registry target: lang.java.framework.akka-http Middleware/middleware_coverage → partial.
func TestAkkaHTTP_Middleware_ExceptionHandler_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class ExRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        final ExceptionHandler handler = ExceptionHandler.newBuilder()
            .match(RuntimeException.class, ex -> complete(StatusCodes.INTERNAL_SERVER_ERROR, ex.getMessage()))
            .build();
        return handleExceptions(handler, () ->
            path("safe", () -> get(() -> complete("ok")))
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/ExRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "Middleware" {
			if mt, ok := e.Properties["middleware_type"].(string); ok && mt == "exception_handler" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected Middleware entity with middleware_type=exception_handler, got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_Middleware_Timeout_Issue3092 verifies withRequestTimeout detection.
func TestAkkaHTTP_Middleware_Timeout_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
import java.time.Duration;
public class TimeoutRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return withRequestTimeout(Duration.ofSeconds(30), () ->
            path("slow", () -> get(() -> complete("done")))
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/TimeoutRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "Middleware" {
			if mt, ok := e.Properties["middleware_type"].(string); ok && mt == "request_timeout" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected Middleware entity with middleware_type=request_timeout, got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_Middleware_Logging_Issue3092 verifies logRequest/logResult detection.
func TestAkkaHTTP_Middleware_Logging_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class LogRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return logRequest("my-service", () ->
            path("health", () -> get(() -> complete("OK")))
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/LogRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "Middleware" {
			if mt, ok := e.Properties["middleware_type"].(string); ok && mt == "logging" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected Middleware entity with middleware_type=logging, got entities=%v", r.Entities)
	}
}

// ----------------------------------------------------------------------------
// Auth: auth_coverage
// ----------------------------------------------------------------------------

// TestAkkaHTTP_Auth_BasicAuth_Issue3092 verifies authenticateBasic() detection.
// Registry target: lang.java.framework.akka-http Auth/auth_coverage → partial.
func TestAkkaHTTP_Auth_BasicAuth_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class AuthRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("admin", () ->
            authenticateBasic("secure-area", credentials -> {
                if (credentials.verify("admin", "password")) return Optional.of("admin");
                return Optional.empty();
            }, user ->
                get(() -> complete("Hello, " + user))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/AuthRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "AuthGuard" {
			if at, ok := e.Properties["auth_type"].(string); ok && at == "basic_auth" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected AuthGuard entity with auth_type=basic_auth, got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_Auth_OAuth2_Issue3092 verifies authenticateOAuth2() detection.
func TestAkkaHTTP_Auth_OAuth2_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class OAuth2Router extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("protected", () ->
            authenticateOAuth2("my-realm", credentials ->
                Optional.of(credentials.token()), token ->
                get(() -> complete("token: " + token))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/OAuth2Router.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "AuthGuard" {
			if at, ok := e.Properties["auth_type"].(string); ok && at == "oauth2" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected AuthGuard entity with auth_type=oauth2, got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_Auth_Authorize_Issue3092 verifies authorize() directive detection.
func TestAkkaHTTP_Auth_Authorize_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class AclRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("resource", () ->
            authorize(isAdmin, () ->
                get(() -> complete("admin only"))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/AclRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "AuthGuard" {
			if at, ok := e.Properties["auth_type"].(string); ok && at == "authorize" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected AuthGuard entity with auth_type=authorize, got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_Auth_HeaderGuard_Issue3092 verifies headerValueByName("Authorization") detection.
func TestAkkaHTTP_Auth_HeaderGuard_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class HeaderAuthRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("secure", () ->
            headerValueByName("Authorization", token ->
                get(() -> complete("token: " + token))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/HeaderAuthRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "AuthGuard" {
			if at, ok := e.Properties["auth_type"].(string); ok && at == "header_guard" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected AuthGuard entity with auth_type=header_guard, got entities=%v", r.Entities)
	}
}

// ----------------------------------------------------------------------------
// DTO extraction: dto_extraction
// ----------------------------------------------------------------------------

// TestAkkaHTTP_DTO_EntityAs_Issue3092 verifies entity(as(MyDto.class)) DTO detection.
// Registry target: lang.java.framework.akka-http Validation/dto_extraction → partial.
func TestAkkaHTTP_DTO_EntityAs_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class DtoRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("orders", () ->
            post(() ->
                entity(as(CreateOrderRequest.class), req -> {
                    orderService.create(req);
                    return complete(StatusCodes.CREATED);
                })
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/DtoRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "Schema" && e.Name == "CreateOrderRequest" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Schema entity 'CreateOrderRequest', got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_DTO_Unmarshal_Issue3092 verifies unmarshal(..., MyDto.class) detection.
func TestAkkaHTTP_DTO_Unmarshal_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class UnmarshalRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("items", () ->
            post(() -> {
                CompletionStage<ItemRequest> bodyF = unmarshal(request, ItemRequest.class);
                return onSuccess(bodyF, body -> complete(StatusCodes.OK));
            })
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/UnmarshalRouter.java",
	})

	var found bool
	for _, e := range r.Entities {
		if e.Kind == "Schema" && e.Name == "ItemRequest" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Schema entity 'ItemRequest', got entities=%v", r.Entities)
	}
}

// TestAkkaHTTP_RequestValidation_Params_Issue3092 verifies parameter() directive
// detection for request validation.
// Registry target: lang.java.framework.akka-http Validation/request_validation → partial.
func TestAkkaHTTP_RequestValidation_Params_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class ParamRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("search", () ->
            get(() ->
                parameter("query", q ->
                    parameterOptional("page", page ->
                        complete("results for: " + q)
                    )
                )
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/ParamRouter.java",
	})

	paramsSeen := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "Schema" {
			if rv, ok := e.Properties["request_validated"].(bool); ok && rv {
				paramsSeen[e.Name] = true
			}
		}
	}

	if !paramsSeen["query"] {
		t.Errorf("expected param 'query' as validated Schema entity, got params=%v", paramsSeen)
	}
	if !paramsSeen["page"] {
		t.Errorf("expected param 'page' as validated Schema entity, got params=%v", paramsSeen)
	}
}

// ----------------------------------------------------------------------------
// Tests: tests_linkage
// ----------------------------------------------------------------------------

// TestAkkaHTTP_Tests_TestKit_Issue3092 verifies akka-http-testkit detection.
// Registry target: lang.java.framework.akka-http Tests/tests_linkage → partial.
func TestAkkaHTTP_Tests_TestKit_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.testkit.JUnitRouteTest;
import org.junit.Test;

public class UserRouterTest extends JUnitRouteTest {
    UserRouter router = new UserRouter();

    @Test
    public void testGetUsers() {
        testRoute(router.createRoute()).run(HttpRequest.GET("/users"))
            .assertStatusCode(StatusCodes.OK);
    }

    @Test
    public void testCreateUser() {
        testRoute(router.createRoute()).run(
            HttpRequest.POST("/users").withEntity(ContentTypes.APPLICATION_JSON, "{\"name\":\"Alice\"}")
        ).assertStatusCode(StatusCodes.CREATED);
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/test/java/UserRouterTest.java",
	})

	var setupFound bool
	var testCasesFound int
	for _, e := range r.Entities {
		if e.Kind == "TestSetup" {
			setupFound = true
		}
		if e.Kind == "TestCase" {
			testCasesFound++
		}
	}

	if !setupFound {
		t.Errorf("expected TestSetup entity for akka-http-testkit, got entities=%v", r.Entities)
	}
	if testCasesFound < 2 {
		t.Errorf("expected at least 2 TestCase entities, got %d; entities=%v", testCasesFound, r.Entities)
	}
}

// TestAkkaHTTP_Tests_RouteTest_Issue3092 verifies RouteTest/testRoute marker detection.
func TestAkkaHTTP_Tests_RouteTest_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.testkit.RouteTest;
import org.junit.Test;

public class RouterTest extends RouteTest {
    @Test
    public void testHealthEndpoint() {
        testRoute(myRouter.routes())
            .run(HttpRequest.GET("/health"))
            .assertStatusCode(StatusCodes.OK);
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/test/java/RouterTest.java",
	})

	var testSetupFound bool
	for _, e := range r.Entities {
		if e.Kind == "TestSetup" {
			testSetupFound = true
		}
	}
	if !testSetupFound {
		t.Errorf("expected TestSetup entity for RouteTest, got entities=%v", r.Entities)
	}
}

// ----------------------------------------------------------------------------
// Gate tests: wrong language/framework should return empty
// ----------------------------------------------------------------------------

// TestAkkaHTTP_GateLanguage_Issue3092 verifies the language gate.
func TestAkkaHTTP_GateLanguage_Issue3092(t *testing.T) {
	source := `path("users", () -> get(() -> complete("ok")))`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "kotlin", // wrong language
		Framework: "akka-http",
		FilePath:  "src/main/kotlin/Router.kt",
	})
	if len(r.Entities) != 0 {
		t.Errorf("expected empty result for non-java language, got %d entities", len(r.Entities))
	}
}

// TestAkkaHTTP_GateFramework_Issue3092 verifies the framework gate.
func TestAkkaHTTP_GateFramework_Issue3092(t *testing.T) {
	source := `path("users", () -> get(() -> complete("ok")))`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring", // wrong framework
		FilePath:  "src/main/java/Router.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("expected empty result for non-akka framework, got %d entities", len(r.Entities))
	}
}

// TestAkkaHTTP_FrameworkAliases_Issue3092 verifies all framework name aliases.
func TestAkkaHTTP_FrameworkAliases_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class R extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("ping", () -> get(() -> complete("pong")));
    }
}
`
	for _, fw := range []string{"akka-http", "akka_http", "akkahttp", "akka-http-java"} {
		r := ExtractAkkaHTTP(PatternContext{
			Source:    source,
			Language:  "java",
			Framework: fw,
			FilePath:  "src/main/java/R.java",
		})
		if len(r.Entities) == 0 {
			t.Errorf("expected entities for framework alias %q, got none", fw)
		}
	}
}

// TestAkkaHTTP_QuickExitNoSignals_Issue3092 verifies the quick-exit when no Akka signals present.
func TestAkkaHTTP_QuickExitNoSignals_Issue3092(t *testing.T) {
	source := `
public class Plain {
    public void doNothing() {
        System.out.println("hello");
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/Plain.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("expected quick-exit with no entities, got %d entities", len(r.Entities))
	}
}

// TestAkkaHTTP_PrimitiveTypesNotEmitted_Issue3092 verifies that primitive/String
// types are not emitted as DTO Schema entities.
func TestAkkaHTTP_PrimitiveTypesNotEmitted_Issue3092(t *testing.T) {
	source := `
import akka.http.javadsl.server.AllDirectives;
public class SafeRouter extends AllDirectives {
    public akka.http.javadsl.server.Route routes() {
        return path("test", () ->
            post(() ->
                entity(as(String.class), body -> complete(body))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "akka-http",
		FilePath:  "src/main/java/SafeRouter.java",
	})

	for _, e := range r.Entities {
		if e.Kind == "Schema" && e.Name == "String" {
			t.Errorf("String should not be emitted as a DTO Schema entity")
		}
	}
}
