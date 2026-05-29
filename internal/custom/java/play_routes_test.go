package java

import (
	"testing"
)

// ============================================================================
// Issue #3090: Play Framework Java route/middleware/auth extractor
// ============================================================================

// ----------------------------------------------------------------------------
// Routing: route_extraction — conf/routes DSL parsing
// ----------------------------------------------------------------------------

// TestPlay_RouteExtraction_ConfRoutes_Issue3090 proves that a Play conf/routes
// file is parsed and all HTTP method + path pairs are emitted as Route entities.
// Registry target: lang.java.framework.play Routing/route_extraction → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_RouteExtraction_ConfRoutes_Issue3090(t *testing.T) {
	source := `# Play routes
GET     /users                    controllers.UserController.list()
POST    /users                    controllers.UserController.create()
GET     /users/:id                controllers.UserController.show(id: Long)
PUT     /users/:id                controllers.UserController.update(id: Long)
DELETE  /users/:id                controllers.UserController.delete(id: Long)
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	routes := make(map[string]string) // path → verb
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" {
			routes[e.Properties["path"].(string)] = e.Properties["http_verb"].(string)
		}
	}

	cases := []struct{ path, verb string }{
		{"/users", "GET"},
		{"/users", "POST"},
		{"/users/{id}", "GET"},
		{"/users/{id}", "PUT"},
		{"/users/{id}", "DELETE"},
	}
	for _, c := range cases {
		found := false
		for _, e := range r.Entities {
			if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" &&
				e.Properties["path"].(string) == c.path &&
				e.Properties["http_verb"].(string) == c.verb {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("[#3090 route_extraction] expected route %s %s, got entities: %v", c.verb, c.path, routes)
		}
	}
}

// TestPlay_RouteExtraction_ColonParam_Issue3090 proves that colon-style path
// parameters (:id) are normalised to {id}.
func TestPlay_RouteExtraction_ColonParam_Issue3090(t *testing.T) {
	source := `GET  /orders/:orderId/items  controllers.OrderController.items(orderId: Long)
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" && e.Properties["path"].(string) == "/orders/{orderId}/items" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3090 route_extraction] expected colon param :orderId → {orderId}")
	}
}

// TestPlay_RouteExtraction_DollarParam_Issue3090 proves that regex-constrained
// path parameters ($id<[0-9]+>) are normalised to {id}.
func TestPlay_RouteExtraction_DollarParam_Issue3090(t *testing.T) {
	source := `GET  /orders/$orderId<[0-9]+>  controllers.OrderController.show(orderId: Long)
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" && e.Properties["path"].(string) == "/orders/{orderId}" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3090 route_extraction] expected dollar param $orderId<...> → {orderId}")
	}
}

// TestPlay_RouteExtraction_ControllerAction_Issue3090 proves that the
// controller action name is stored in the route entity properties.
func TestPlay_RouteExtraction_ControllerAction_Issue3090(t *testing.T) {
	source := `GET  /home  controllers.HomeController.index()
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" {
			action, _ := e.Properties["controller_action"].(string)
			if action == "controllers.HomeController.index" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("[#3090 route_extraction] expected controller_action=controllers.HomeController.index")
	}
}

// ----------------------------------------------------------------------------
// Routing: handler_attribution — HANDLED_BY relationship
// ----------------------------------------------------------------------------

// TestPlay_HandlerAttribution_Issue3090 proves that each Route in conf/routes
// emits a corresponding Handler entity and a HANDLED_BY relationship.
// Registry target: lang.java.framework.play Routing/handler_attribution → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_HandlerAttribution_Issue3090(t *testing.T) {
	source := `POST  /products  controllers.ProductController.create()
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	foundRoute := false
	foundHandler := false
	foundRel := false

	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" {
			foundRoute = true
		}
		if e.Provenance == "INFERRED_FROM_PLAY_HANDLER" {
			foundHandler = true
			if e.Name != "controllers.ProductController.create" {
				t.Errorf("[#3090 handler_attribution] expected handler=controllers.ProductController.create, got %q", e.Name)
			}
		}
	}
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "HANDLED_BY" && rel.Properties["framework"] == "play" {
			foundRel = true
		}
	}
	if !foundRoute {
		t.Errorf("[#3090 handler_attribution] expected route entity")
	}
	if !foundHandler {
		t.Errorf("[#3090 handler_attribution] expected handler entity")
	}
	if !foundRel {
		t.Errorf("[#3090 handler_attribution] expected HANDLED_BY relationship")
	}
}

// ----------------------------------------------------------------------------
// Routing: endpoint_synthesis — Result-returning controller methods
// ----------------------------------------------------------------------------

// TestPlay_EndpointSynthesis_ResultMethod_Issue3090 proves that controller
// methods returning play.mvc.Result are detected as Handler entities.
// Registry target: lang.java.framework.play Routing/endpoint_synthesis → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_EndpointSynthesis_ResultMethod_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;

public class HomeController extends Controller {
    public Result index() {
        return ok("home");
    }

    public Result about() {
        return ok("about");
    }

    public Result notFound() {
        return notFound("404");
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/HomeController.java",
	})

	methods := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_RESULT_METHOD" {
			methods[e.Properties["method"].(string)] = true
		}
	}

	for _, want := range []string{"index", "about", "notFound"} {
		if !methods[want] {
			t.Errorf("[#3090 endpoint_synthesis] expected handler method %q, got %v", want, methods)
		}
	}
}

// TestPlay_EndpointSynthesis_AsyncResult_Issue3090 proves that async
// CompletionStage<Result> methods are also detected.
func TestPlay_EndpointSynthesis_AsyncResult_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;
import java.util.concurrent.CompletionStage;

public class AsyncController extends Controller {
    public CompletionStage<Result> fetchData() {
        return null;
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/AsyncController.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_RESULT_METHOD" && e.Properties["method"].(string) == "fetchData" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3090 endpoint_synthesis] expected CompletionStage<Result> method fetchData")
	}
}

// ----------------------------------------------------------------------------
// Middleware: middleware_coverage — @With + Action subclasses + filters
// ----------------------------------------------------------------------------

// TestPlay_Middleware_WithAnnotation_Issue3090 proves that @With(MyAction.class)
// on a controller is detected as Middleware.
// Registry target: lang.java.framework.play Middleware/middleware_coverage → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_Middleware_WithAnnotation_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;
import play.mvc.With;
import actions.AuthAction;

@With(AuthAction.class)
public class SecureController extends Controller {
    public Result secret() {
        return ok("secret");
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/SecureController.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_PLAY_WITH_ANNOTATION" {
			found = true
			if e.Properties["middleware_type"] != "action_composition" {
				t.Errorf("[#3090 middleware_coverage] expected middleware_type=action_composition, got %v", e.Properties["middleware_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3090 middleware_coverage] expected Middleware entity from @With annotation")
	}
}

// TestPlay_Middleware_ActionClass_Issue3090 proves that classes extending
// play.mvc.Action are detected as Middleware entities.
func TestPlay_Middleware_ActionClass_Issue3090(t *testing.T) {
	source := `
package actions;

import play.mvc.Action;
import play.mvc.Http;
import play.mvc.Result;
import java.util.concurrent.CompletionStage;

public class AuthAction extends Action<Void> {
    @Override
    public CompletionStage<Result> call(Http.Request request) {
        if (request.session().data().containsKey("userId")) {
            return delegate.call(request);
        }
        return null;
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/actions/AuthAction.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_PLAY_ACTION_CLASS" {
			found = true
			if e.Name != "AuthAction" {
				t.Errorf("[#3090 middleware_coverage] expected name=AuthAction, got %q", e.Name)
			}
		}
	}
	if !found {
		t.Errorf("[#3090 middleware_coverage] expected Middleware entity for Action subclass")
	}
}

// TestPlay_Middleware_HttpFilters_Issue3090 proves that classes implementing
// HttpFilters are detected as Middleware.
func TestPlay_Middleware_HttpFilters_Issue3090(t *testing.T) {
	source := `
package filters;

import play.http.DefaultHttpFilters;
import play.filters.cors.CORSFilter;
import javax.inject.Inject;

public class Filters extends DefaultHttpFilters {
    @Inject
    public Filters(CORSFilter cors) {
        super(cors);
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/filters/Filters.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_PLAY_HTTP_FILTERS" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3090 middleware_coverage] expected Middleware entity for HttpFilters subclass")
	}
}

// ----------------------------------------------------------------------------
// Auth: auth_coverage — Deadbolt-2 + manual session
// ----------------------------------------------------------------------------

// TestPlay_Auth_Deadbolt_Issue3090 proves that Deadbolt-2 annotations are
// detected as AuthGuard entities.
// Registry target: lang.java.framework.play Auth/auth_coverage → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_Auth_Deadbolt_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;
import be.objectify.deadbolt.java.actions.SubjectPresent;
import be.objectify.deadbolt.java.actions.Restrict;
import be.objectify.deadbolt.java.actions.Group;

public class AdminController extends Controller {
    @SubjectPresent
    public Result dashboard() {
        return ok("admin dashboard");
    }

    @Restrict(@Group("admin"))
    public Result adminOnly() {
        return ok("admin only");
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/AdminController.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_DEADBOLT" {
			found = true
			if e.Properties["auth_type"] != "deadbolt2" {
				t.Errorf("[#3090 auth_coverage] expected auth_type=deadbolt2, got %v", e.Properties["auth_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3090 auth_coverage] expected AuthGuard entity for Deadbolt-2 annotations")
	}
}

// TestPlay_Auth_ManualSession_Issue3090 proves that manual session-based
// auth patterns are detected.
func TestPlay_Auth_ManualSession_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;

public class ProfileController extends Controller {
    public Result profile() {
        String userId = session("userId");
        if (userId == null) {
            return redirect(routes.HomeController.login());
        }
        return ok("profile");
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/ProfileController.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_SESSION_AUTH" {
			found = true
			if e.Properties["auth_type"] != "session" {
				t.Errorf("[#3090 auth_coverage] expected auth_type=session, got %v", e.Properties["auth_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3090 auth_coverage] expected AuthGuard entity for manual session auth")
	}
}

// ----------------------------------------------------------------------------
// Validation: request_validation — request().body() and form binding
// ----------------------------------------------------------------------------

// TestPlay_RequestValidation_FormBinding_Issue3090 proves that
// Form.form(Dto.class).bindFromRequest() is detected as a Schema entity
// (dto_extraction + request_validation signal).
// Registry target: lang.java.framework.play Validation/request_validation → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_RequestValidation_FormBinding_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;
import play.data.Form;
import play.data.FormFactory;
import javax.inject.Inject;
import models.UserForm;

public class UserController extends Controller {
    @Inject
    private FormFactory formFactory;

    public Result create() {
        Form<UserForm> form = formFactory.form(UserForm.class).bindFromRequest();
        if (form.hasErrors()) {
            return badRequest(form.errorsAsJson());
        }
        return created("ok");
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/UserController.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_FORM_BINDING" && e.Name == "UserForm" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3090 request_validation] expected Schema entity for UserForm form binding")
	}
}

// TestPlay_RequestValidation_RequestBody_Issue3090 proves that request().body()
// usage is detected as a request validation signal.
func TestPlay_RequestValidation_RequestBody_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;

public class OrderController extends Controller {
    public Result create() {
        String body = request().body().asText().orElse("");
        return created(body);
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/OrderController.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_REQUEST_BODY" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3090 request_validation] expected Schema entity for request().body() usage")
	}
}

// ----------------------------------------------------------------------------
// Tests: tests_linkage
// ----------------------------------------------------------------------------

// TestPlay_TestsLinkage_WithApplication_Issue3090 proves that Play test setup
// (WithApplication/fakeApplication) is detected as a TestSetup entity.
// Registry target: lang.java.framework.play Testing/tests_linkage → partial.
// Cite: internal/custom/java/play_routes.go
func TestPlay_TestsLinkage_WithApplication_Issue3090(t *testing.T) {
	source := `
package test;

import org.junit.Test;
import play.test.WithApplication;
import play.mvc.Result;
import static play.test.Helpers.*;
import static org.junit.Assert.*;

public class UserControllerTest extends WithApplication {

    @Test
    public void testListUsers() {
        Result result = route(app, fakeRequest("GET", "/users"));
        assertEquals(200, result.status());
    }

    @Test
    public void testCreateUser() {
        Result result = route(app, fakeRequest("POST", "/users"));
        assertEquals(201, result.status());
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "test/UserControllerTest.java",
	})

	foundSetup := false
	testMethods := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_TEST_SETUP" {
			foundSetup = true
		}
		if e.Provenance == "INFERRED_FROM_PLAY_TEST_METHOD" {
			testMethods[e.Name] = true
		}
	}
	if !foundSetup {
		t.Errorf("[#3090 tests_linkage] expected TestSetup entity for WithApplication")
	}
	if !testMethods["testListUsers"] {
		t.Errorf("[#3090 tests_linkage] expected TestCase entity for testListUsers")
	}
	if !testMethods["testCreateUser"] {
		t.Errorf("[#3090 tests_linkage] expected TestCase entity for testCreateUser")
	}
}

// ----------------------------------------------------------------------------
// Gating: extractor must not fire for wrong framework or absent signal
// ----------------------------------------------------------------------------

// TestPlay_Gating_WrongFramework_Issue3090 confirms the extractor does not
// fire for non-play frameworks.
func TestPlay_Gating_WrongFramework_Issue3090(t *testing.T) {
	source := `GET  /users  controllers.UserController.list()
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "conf/routes",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3090 gating] expected 0 entities for framework=spring_boot, got %d", len(r.Entities))
	}
}

// TestPlay_Gating_WrongLanguage_Issue3090 confirms the extractor does not
// fire for non-java languages.
func TestPlay_Gating_WrongLanguage_Issue3090(t *testing.T) {
	source := `GET  /users  controllers.UserController.list()
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "play",
		FilePath:  "conf/routes",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3090 gating] expected 0 entities for language=kotlin, got %d", len(r.Entities))
	}
}

// TestPlay_Gating_NoSignal_Issue3090 confirms the extractor no-ops on Java
// files with no Play signals.
func TestPlay_Gating_NoSignal_Issue3090(t *testing.T) {
	source := `
public class OrderService {
    public Order findById(long id) { return null; }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "OrderService.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3090 gating] expected 0 entities for file without Play signals, got %d", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// Comprehensive: full Play app (routes file + controller)
// ----------------------------------------------------------------------------

// TestPlay_FullApp_Routes_Issue3090 verifies a realistic Play routes file
// with multiple HTTP verbs, colon params, and dollar params.
// This is the comprehensive proving test for route_extraction → partial.
func TestPlay_FullApp_Routes_Issue3090(t *testing.T) {
	source := `# Play Framework routes fixture
GET     /                               controllers.HomeController.index()
GET     /about                          controllers.HomeController.about()
GET     /users                          controllers.UserController.list()
POST    /users                          controllers.UserController.create()
GET     /users/:id                      controllers.UserController.show(id: Long)
PUT     /users/:id                      controllers.UserController.update(id: Long)
DELETE  /users/:id                      controllers.UserController.delete(id: Long)
GET     /orders/$orderId<[0-9]+>        controllers.OrderController.show(orderId: Long)
POST    /orders                         controllers.OrderController.create()
GET     /orders/:orderId/items          controllers.OrderController.listItems(orderId: Long)
PATCH   /orders/:orderId/status         controllers.OrderController.updateStatus(orderId: Long)
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	routes := make(map[string][]string)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" {
			path := e.Properties["path"].(string)
			verb := e.Properties["http_verb"].(string)
			routes[path] = append(routes[path], verb)
		}
	}

	expectedRoutes := []struct{ path, verb string }{
		{"/", "GET"},
		{"/about", "GET"},
		{"/users", "GET"},
		{"/users", "POST"},
		{"/users/{id}", "GET"},
		{"/users/{id}", "PUT"},
		{"/users/{id}", "DELETE"},
		{"/orders/{orderId}", "GET"},
		{"/orders", "POST"},
		{"/orders/{orderId}/items", "GET"},
		{"/orders/{orderId}/status", "PATCH"},
	}

	for _, want := range expectedRoutes {
		found := false
		for _, e := range r.Entities {
			if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" &&
				e.Properties["path"].(string) == want.path &&
				e.Properties["http_verb"].(string) == want.verb {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("[#3090 full-app] expected route %s %s, all routes: %v", want.verb, want.path, routes)
		}
	}

	// Verify handler entities are emitted
	handlers := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_HANDLER" {
			handlers++
		}
	}
	if handlers == 0 {
		t.Errorf("[#3090 full-app] expected Handler entities, got 0")
	}

	// Verify HANDLED_BY relationships
	rels := 0
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "HANDLED_BY" {
			rels++
		}
	}
	if rels == 0 {
		t.Errorf("[#3090 full-app] expected HANDLED_BY relationships, got 0")
	}
}

// TestPlay_FullApp_Controller_Issue3090 verifies a realistic Play controller
// with DI, middleware, auth, form binding, and request body access.
func TestPlay_FullApp_Controller_Issue3090(t *testing.T) {
	source := `
package controllers;

import play.mvc.Controller;
import play.mvc.Result;
import play.mvc.With;
import play.data.Form;
import play.data.FormFactory;
import javax.inject.Inject;
import actions.AuthAction;
import models.UserForm;
import be.objectify.deadbolt.java.actions.SubjectPresent;

@With(AuthAction.class)
public class UserController extends Controller {

    @Inject
    private FormFactory formFactory;

    @SubjectPresent
    public Result list() {
        return ok("users");
    }

    public Result create() {
        Form<UserForm> form = formFactory.form(UserForm.class).bindFromRequest();
        if (form.hasErrors()) {
            return badRequest(form.errorsAsJson());
        }
        return created("ok");
    }

    public Result update(Long id) {
        String body = request().body().asText().orElse("");
        return ok(body);
    }
}
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "app/controllers/UserController.java",
	})

	// Validate Result-returning methods
	methods := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_RESULT_METHOD" {
			methods[e.Properties["method"].(string)] = true
		}
	}
	for _, want := range []string{"list", "create", "update"} {
		if !methods[want] {
			t.Errorf("[#3090 full-app-ctrl] expected Result method %q, got %v", want, methods)
		}
	}

	// Validate @With middleware
	foundWith := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_WITH_ANNOTATION" {
			foundWith = true
		}
	}
	if !foundWith {
		t.Errorf("[#3090 full-app-ctrl] expected @With middleware entity")
	}

	// Validate Deadbolt auth
	foundAuth := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_DEADBOLT" {
			foundAuth = true
		}
	}
	if !foundAuth {
		t.Errorf("[#3090 full-app-ctrl] expected Deadbolt auth entity")
	}

	// Validate DTO extraction
	foundDTO := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_FORM_BINDING" && e.Name == "UserForm" {
			foundDTO = true
		}
	}
	if !foundDTO {
		t.Errorf("[#3090 full-app-ctrl] expected DTO entity for UserForm")
	}

	// Validate request body access
	foundBody := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_REQUEST_BODY" {
			foundBody = true
		}
	}
	if !foundBody {
		t.Errorf("[#3090 full-app-ctrl] expected request body entity")
	}
}

// TestPlay_RouteProperties_Issue3090 confirms that route entities carry
// the expected properties (framework, route_type, http_verb, path).
func TestPlay_RouteProperties_Issue3090(t *testing.T) {
	source := `GET  /items  controllers.ItemController.list()
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	for _, e := range r.Entities {
		if e.Provenance != "INFERRED_FROM_PLAY_ROUTES" {
			continue
		}
		if e.Properties["framework"] != "play" {
			t.Errorf("[#3090] expected framework=play, got %v", e.Properties["framework"])
		}
		if e.Properties["route_type"] != "conf_routes" {
			t.Errorf("[#3090] expected route_type=conf_routes, got %v", e.Properties["route_type"])
		}
		if e.Properties["http_verb"] != "GET" {
			t.Errorf("[#3090] expected http_verb=GET, got %v", e.Properties["http_verb"])
		}
		if e.Properties["path"] != "/items" {
			t.Errorf("[#3090] expected path=/items, got %v", e.Properties["path"])
		}
		return
	}
	t.Errorf("[#3090] expected at least one INFERRED_FROM_PLAY_ROUTES entity")
}

// TestPlay_RouteExtraction_AllVerbs_Issue3090 verifies all HTTP verbs are
// recognised by the routes parser.
func TestPlay_RouteExtraction_AllVerbs_Issue3090(t *testing.T) {
	source := `GET     /res    controllers.ResController.get()
POST    /res    controllers.ResController.post()
PUT     /res    controllers.ResController.put()
DELETE  /res    controllers.ResController.delete()
PATCH   /res    controllers.ResController.patch()
HEAD    /res    controllers.ResController.head()
OPTIONS /res    controllers.ResController.options()
`
	r := ExtractPlay(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "play",
		FilePath:  "conf/routes",
	})

	verbs := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_PLAY_ROUTES" {
			verbs[e.Properties["http_verb"].(string)] = true
		}
	}

	for _, want := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		if !verbs[want] {
			t.Errorf("[#3090 all-verbs] expected verb %q, got %v", want, verbs)
		}
	}
}
