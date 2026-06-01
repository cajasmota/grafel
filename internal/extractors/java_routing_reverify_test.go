package extractors

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// java_routing_reverify_test.go — live-path re-verification for #3588 (epic #3584).
//
// #3585/#3586 downgraded the Java routing/endpoint/DTO coverage cells to
// `missing` while the custom_java_patterns pattern-extractor layer was dead
// (zero non-test callers). #3599 wired that layer into the live indexer via the
// custom_java_patterns dispatcher. These tests drive the FULL live dispatch
// (RunCustomExtractors → CustomExtractorsFor("java") → custom_java_patterns
// .Extract → detectFrameworks → Extract* pattern funcs) on representative
// framework sources and assert the SPECIFIC route / handler / DTO / validation
// entity reaches the graph — proving each re-flipped coverage cell genuinely
// fires live, not merely in a unit test.
//
// Assertions are value-specific (exact Kind/Name/property), never len > 0.
// runJavaPatterns / findRecord / names are shared with the #3586 smoke test.

// findByKindProp returns the first record of the given kind whose property
// key==val, or nil. Used when the entity Name is data-dependent.
func findByKindProp(recs []types.EntityRecord, kind, key, val string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == kind && recs[i].Properties[key] == val {
			return &recs[i]
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Spring Boot — Validation.dto_extraction
// (cite: internal/custom/java/spring_request_response.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifySpringBootDTOExtraction(t *testing.T) {
	src := `
package com.example.api;

import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;

@RestController
@RequestMapping("/api/users")
public class UserController {

    @PostMapping
    public UserResponse createUser(@RequestBody CreateUserRequest body) {
        return null;
    }
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/api/UserController.java", src)

	// The @RequestBody DTO must emit as a spring DTO schema.
	in := findRecord(recs, "SCOPE.Schema", "CreateUserRequest")
	if in == nil {
		t.Fatalf("expected SCOPE.Schema CreateUserRequest (@RequestBody DTO) to emit live; got %v", names(recs))
	}
	if got := in.Properties["kind"]; got != "dto" {
		t.Errorf("CreateUserRequest kind = %q, want dto", got)
	}
	if got := in.Properties["framework"]; got != "spring" {
		t.Errorf("CreateUserRequest framework = %q, want spring", got)
	}
	// The return-type DTO must also emit.
	out := findRecord(recs, "SCOPE.Schema", "UserResponse")
	if out == nil {
		t.Fatalf("expected SCOPE.Schema UserResponse (return-type DTO) to emit live; got %v", names(recs))
	}
	if got := out.Properties["kind"]; got != "dto" {
		t.Errorf("UserResponse kind = %q, want dto", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Spring WebFlux — Validation.dto_extraction
// (same extractor; ResponseEntity / Mono unwrap path)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifySpringWebFluxDTOExtraction(t *testing.T) {
	src := `
package com.example.reactive;

import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import reactor.core.publisher.Mono;

@RestController
@RequestMapping("/api/accounts")
public class AccountController {

    @PostMapping
    public Mono<AccountView> open(@RequestBody OpenAccountCommand cmd) {
        return null;
    }
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/reactive/AccountController.java", src)

	in := findRecord(recs, "SCOPE.Schema", "OpenAccountCommand")
	if in == nil {
		t.Fatalf("expected SCOPE.Schema OpenAccountCommand (@RequestBody DTO) to emit live; got %v", names(recs))
	}
	if got := in.Properties["kind"]; got != "dto" {
		t.Errorf("OpenAccountCommand kind = %q, want dto", got)
	}
	// Mono<AccountView> must be unwrapped to the element DTO AccountView.
	out := findRecord(recs, "SCOPE.Schema", "AccountView")
	if out == nil {
		t.Fatalf("expected SCOPE.Schema AccountView (Mono<T> unwrapped return DTO) to emit live; got %v", names(recs))
	}
	if got := out.Properties["framework"]; got != "spring" {
		t.Errorf("AccountView framework = %q, want spring", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Javalin — Routing.handler_attribution + Validation.dto_extraction +
//           Validation.request_validation
// (cite: internal/custom/java/javalin_routes.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyJavalinHandlerAndDTO(t *testing.T) {
	src := `
package com.example.app;

import io.javalin.Javalin;

public class App {
    public static void main(String[] args) {
        Javalin app = Javalin.create();
        app.get("/users/{id}", UserHandler::getUser);
        app.post("/users", ctx -> {
            CreateUser body = ctx.bodyAsClass(CreateUser.class);
            ctx.bodyValidator(CreateUser.class).check(u -> u.name != null, "name required").get();
        });
    }
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/app/App.java", src)

	// handler_attribution: the GET /users/{id} route binds to UserHandler::getUser.
	route := findByKindProp(recs, "Route", "path", "/users/{id}")
	if route == nil {
		t.Fatalf("expected Javalin Route /users/{id} to emit live; got %v", names(recs))
	}
	if got := route.Properties["http_verb"]; got != "GET" {
		t.Errorf("route http_verb = %q, want GET", got)
	}
	h := findRecord(recs, "Handler", "UserHandler::getUser")
	if h == nil {
		t.Fatalf("expected Javalin Handler UserHandler::getUser to emit live; got %v", names(recs))
	}
	if !hasRel(route, "HANDLED_BY", h.Properties["ref"]) {
		t.Errorf("expected HANDLED_BY edge from /users/{id} route to UserHandler::getUser; got rels %v", route.Relationships)
	}

	// dto_extraction: ctx.bodyAsClass(CreateUser.class).
	dto := findByKindProp(recs, "Schema", "framework", "javalin")
	if dto == nil || dto.Name != "CreateUser" {
		t.Fatalf("expected Javalin Schema CreateUser (bodyAsClass DTO) to emit live; got %v", names(recs))
	}

	// request_validation: ctx.bodyValidator(CreateUser.class) — emitted as a
	// validation Schema (request_validated). Assert at least the validator DTO.
	if v := findByKindProp(recs, "Schema", "request_validated", "true"); v == nil {
		// Some javalin builds tag validation onto the same Schema; tolerate by
		// requiring an explicit validator-sourced schema.
		if vv := findByKindProp(recs, "Schema", "dto_source", "bodyValidator"); vv == nil {
			t.Errorf("expected a Javalin request-validation Schema (bodyValidator) to emit live; got %v", names(recs))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Vert.x — Validation.dto_extraction + Validation.request_validation
// (cite: internal/custom/java/vertx_routes.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyVertxDTOAndValidation(t *testing.T) {
	src := `
package com.example.vertx;

import io.vertx.ext.web.Router;

public class MainVerticle {
    public void start() {
        Router router = Router.router(vertx);
        router.post("/orders").handler(ctx -> {
            OrderDto dto = ctx.body().as(OrderDto.class);
        });
        BodyProcessorFactory.create(OrderValidation.class);
    }
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/vertx/MainVerticle.java", src)

	// dto_extraction: body().as(OrderDto.class).
	dto := findRecord(recs, "Schema", "OrderDto")
	if dto == nil {
		t.Fatalf("expected Vert.x Schema OrderDto (body().as DTO) to emit live; got %v", names(recs))
	}
	if got := dto.Properties["dto_source"]; got != "body().as" {
		t.Errorf("OrderDto dto_source = %q, want body().as", got)
	}

	// request_validation: BodyProcessorFactory.create(OrderValidation.class).
	val := findRecord(recs, "Schema", "OrderValidation")
	if val == nil {
		t.Fatalf("expected Vert.x Schema OrderValidation (BodyProcessorFactory validation) to emit live; got %v", names(recs))
	}
	if got := val.Properties["request_validated"]; got != "true" {
		t.Errorf("OrderValidation request_validated = %q, want true", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Struts — Routing.route_extraction + Validation.dto_extraction +
//          Validation.request_validation
// (cite: internal/custom/java/struts_routes.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyStrutsRouteDTOValidation(t *testing.T) {
	src := `
package com.example.struts;

import org.apache.struts2.convention.annotation.Action;
import com.opensymphony.xwork2.ActionSupport;

public class UserAction extends ActionSupport {

    private String username;

    public void setUsername(String username) { this.username = username; }

    @Action("/user/save")
    public String save() {
        return SUCCESS;
    }

    @Override
    public void validate() {
        if (username == null) addFieldError("username", "required");
    }
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/struts/UserAction.java", src)

	// route_extraction: @Action("/user/save").
	route := findByKindProp(recs, "Route", "path", "/user/save")
	if route == nil {
		t.Fatalf("expected Struts Route /user/save (@Action) to emit live; got %v", names(recs))
	}

	// dto_extraction: the ActionSupport subclass is a Struts DTO/form.
	dto := findRecord(recs, "SCOPE.Schema", "UserAction")
	if dto == nil {
		t.Fatalf("expected Struts SCOPE.Schema UserAction (ActionSupport DTO) to emit live; got %v", names(recs))
	}
	if got := dto.Properties["kind"]; got != "dto" {
		t.Errorf("UserAction kind = %q, want dto", got)
	}
	if got := dto.Properties["framework"]; got != "struts" {
		t.Errorf("UserAction framework = %q, want struts", got)
	}

	// request_validation: validate() override.
	val := findRecord(recs, "SCOPE.Operation", "UserAction.validate")
	if val == nil {
		t.Fatalf("expected Struts SCOPE.Operation UserAction.validate (validate() override) to emit live; got %v", names(recs))
	}
	if got := val.Properties["validation_kind"]; got != "validate_override" {
		t.Errorf("UserAction.validate validation_kind = %q, want validate_override", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Akka HTTP — Routing.handler_attribution + Validation.dto_extraction +
//             Validation.request_validation
// (cite: internal/custom/java/akka_http_routes.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyAkkaHandlerDTOValidation(t *testing.T) {
	src := `
package com.example.akka;

import akka.http.javadsl.server.AllDirectives;

public class UserRoutes extends AllDirectives {
    public Route createRoute() {
        return path("users", () ->
            parameter("page", page ->
                post(() ->
                    entity(as(CreateUserDto.class), dto ->
                        complete(userHandler.create(dto))
                    )
                )
            )
        );
    }
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/akka/UserRoutes.java", src)

	// handler_attribution: the post route binds to userHandler.create via the
	// complete(handler.method) directive, with a HANDLED_BY edge.
	route := findByKindProp(recs, "Route", "http_verb", "POST")
	if route == nil {
		t.Fatalf("expected an Akka HTTP POST Route to emit live; got %v", names(recs))
	}
	h := findRecord(recs, "Handler", "userHandler.create")
	if h == nil {
		t.Fatalf("expected Akka Handler userHandler.create (complete(handler.method)) to emit live; got %v", names(recs))
	}
	if got := h.Properties["handler_type"]; got != "directive_lambda" {
		t.Errorf("userHandler.create handler_type = %q, want directive_lambda", got)
	}
	if !hasRel(route, "HANDLED_BY", h.Properties["ref"]) {
		t.Errorf("expected HANDLED_BY edge from POST route to userHandler.create; got rels %v", route.Relationships)
	}

	// dto_extraction: entity(as(CreateUserDto.class)).
	dto := findRecord(recs, "Schema", "CreateUserDto")
	if dto == nil {
		t.Fatalf("expected Akka Schema CreateUserDto (entity(as(...)) DTO) to emit live; got %v", names(recs))
	}
	if got := dto.Properties["dto_source"]; got != "entity(as(...))" {
		t.Errorf("CreateUserDto dto_source = %q, want entity(as(...))", got)
	}

	// request_validation: parameter("page", ...) directive emits a validation Schema.
	val := findRecord(recs, "Schema", "page")
	if val == nil {
		t.Fatalf("expected Akka Schema page (parameter directive request-validation) to emit live; got %v", names(recs))
	}
	if got := val.Properties["request_validated"]; got != "true" {
		t.Errorf("page request_validated = %q, want true", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Play — Uncategorized.handler_attribution
// (cite: internal/custom/java/play_routes.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyPlayHandlerAttribution(t *testing.T) {
	src := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;

public class UserController extends Controller {

    public Result listUsers() {
        return ok();
    }
}
`
	recs := runJavaPatterns(t, "app/controllers/UserController.java", src)

	// handler_attribution: the Result-returning method is a Play handler.
	h := findRecord(recs, "Handler", "UserController.listUsers")
	if h == nil {
		t.Fatalf("expected Play Handler UserController.listUsers (Result method) to emit live; got %v", names(recs))
	}
	if got := h.Properties["handler_type"]; got != "result_method" {
		t.Errorf("listUsers handler_type = %q, want result_method", got)
	}
	if got := h.Properties["framework"]; got != "play" {
		t.Errorf("listUsers framework = %q, want play", got)
	}
}
