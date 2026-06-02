package java

import "testing"

// framework_auth_test.go — value-asserting auth_coverage tests for the JVM
// frameworks that trailed Spring on the Auth cell (#3862, epic #3854):
// Javalin, Vert.x, Akka-HTTP, Struts, Dropwizard, Netflix DGS.
//
// Each test asserts the FLAT Spring-compatible auth contract on the SPECIFIC
// endpoint/route entity for the named framework — not len>0 — plus a negative
// case proving an unprotected route carries no auth_required.

// routeWithPath returns the first Route/endpoint entity whose path property
// equals path, or nil.
func routeWithPath(r PatternResult, path string) *SecondaryEntity {
	for i := range r.Entities {
		e := &r.Entities[i]
		if p, _ := e.Properties["path"].(string); p == path {
			return e
		}
	}
	return nil
}

func propStr(e *SecondaryEntity, key string) string {
	if e == nil || e.Properties == nil {
		return ""
	}
	s, _ := e.Properties[key].(string)
	return s
}

// ── Javalin: roles(Role.ADMIN) on /admin → auth_required=true roles=[ADMIN] ──

func TestJavalinAuth_RouteRoles_Issue3862(t *testing.T) {
	source := `
package com.example;
import io.javalin.Javalin;
import static io.javalin.security.RouteRole.roles;
public class App {
    public static void main(String[] args) {
        var app = Javalin.create(config -> config.accessManager(new MyAccessManager()));
        app.get("/public", ctx -> ctx.result("ok"));
        app.get("/admin", AdminHandler::handle, roles(Role.ADMIN));
        app.post("/staff", ctx -> ctx.result("x"), roles(Role.ADMIN, Role.STAFF));
    }
}
`
	r := ExtractJavalin(PatternContext{Source: source, Language: "java", Framework: "javalin"})

	admin := routeWithPath(r, "/admin")
	if admin == nil {
		t.Fatal("expected /admin route entity")
	}
	if got := propStr(admin, "auth_required"); got != "true" {
		t.Errorf("/admin auth_required = %q, want \"true\"", got)
	}
	if got := propStr(admin, "auth_roles"); got != "ADMIN" {
		t.Errorf("/admin auth_roles = %q, want \"ADMIN\"", got)
	}

	staff := routeWithPath(r, "/staff")
	if got := propStr(staff, "auth_roles"); got != "ADMIN,STAFF" {
		t.Errorf("/staff auth_roles = %q, want \"ADMIN,STAFF\" (sorted)", got)
	}
	if got := propStr(staff, "auth_required"); got != "true" {
		t.Errorf("/staff auth_required = %q, want \"true\"", got)
	}

	// /public carries no inline roles, but the app wires an accessManager → it
	// inherits a low-confidence auth_required (the manager gates every route).
	pub := routeWithPath(r, "/public")
	if got := propStr(pub, "auth_required"); got != "true" {
		t.Errorf("/public auth_required = %q, want \"true\" (accessManager)", got)
	}
	if got := propStr(pub, "auth_confidence"); got != "low" {
		t.Errorf("/public auth_confidence = %q, want \"low\"", got)
	}
}

// Negative: no accessManager, no roles() → auth_required absent.
func TestJavalinAuth_Unprotected_Issue3862(t *testing.T) {
	source := `
package com.example;
import io.javalin.Javalin;
public class App {
    public static void main(String[] args) {
        var app = Javalin.create();
        app.get("/health", ctx -> ctx.result("ok"));
    }
}
`
	r := ExtractJavalin(PatternContext{Source: source, Language: "java", Framework: "javalin"})
	h := routeWithPath(r, "/health")
	if h == nil {
		t.Fatal("expected /health route")
	}
	if _, ok := h.Properties["auth_required"]; ok {
		t.Errorf("/health should have no auth_required, got %v", h.Properties["auth_required"])
	}
}

// ── Vert.x: JWTAuthHandler on a route → auth_required mechanism=jwt ──────────

func TestVertxAuth_JWTHandler_Issue3862(t *testing.T) {
	source := `
package com.example;
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.JWTAuthHandler;
public class ApiVerticle {
    void routes(Router router) {
        router.route("/api/*").handler(JWTAuthHandler.create(jwtAuth));
        router.get("/api/orders").handler(ctx -> ctx.json(orders));
    }
}
`
	r := ExtractVertx(PatternContext{Source: source, Language: "java", Framework: "vertx"})
	orders := routeWithPath(r, "/api/orders")
	if orders == nil {
		t.Fatal("expected /api/orders route")
	}
	if got := propStr(orders, "auth_required"); got != "true" {
		t.Errorf("/api/orders auth_required = %q, want \"true\"", got)
	}
	if got := propStr(orders, "auth_mechanism"); got != "jwt" {
		t.Errorf("/api/orders auth_mechanism = %q, want \"jwt\"", got)
	}
}

// Vert.x @RolesAllowed → roles surfaced; Basic mechanism precedence sanity.
func TestVertxAuth_BasicAndRoles_Issue3862(t *testing.T) {
	source := `
package com.example;
import io.vertx.ext.web.Router;
import io.vertx.ext.web.handler.BasicAuthHandler;
public class AdminVerticle {
    void routes(Router router) {
        router.route().handler(BasicAuthHandler.create(provider));
        @RolesAllowed({"ADMIN"})
        public void admin(RoutingContext ctx) {}
        router.get("/admin").handler(this::admin);
    }
}
`
	r := ExtractVertx(PatternContext{Source: source, Language: "java", Framework: "vertx"})
	admin := routeWithPath(r, "/admin")
	if got := propStr(admin, "auth_mechanism"); got != "basic" {
		t.Errorf("/admin auth_mechanism = %q, want \"basic\"", got)
	}
	if got := propStr(admin, "auth_roles"); got != "ADMIN" {
		t.Errorf("/admin auth_roles = %q, want \"ADMIN\"", got)
	}
}

// Negative: a Vert.x router with no auth handler → auth_required absent.
func TestVertxAuth_Unprotected_Issue3862(t *testing.T) {
	source := `
package com.example;
import io.vertx.ext.web.Router;
public class PublicVerticle {
    void routes(Router router) {
        router.get("/ping").handler(ctx -> ctx.end("pong"));
    }
}
`
	r := ExtractVertx(PatternContext{Source: source, Language: "java", Framework: "vertx"})
	ping := routeWithPath(r, "/ping")
	if ping == nil {
		t.Fatal("expected /ping route")
	}
	if _, ok := ping.Properties["auth_required"]; ok {
		t.Errorf("/ping should have no auth_required")
	}
}

// ── Akka-HTTP: authenticateOAuth2 directive → auth_required mechanism=oauth2 ─

func TestAkkaHTTPAuth_OAuth2Directive_Issue3862(t *testing.T) {
	source := `
package com.example;
import static akka.http.javadsl.server.Directives.*;
public class Routes {
    Route createRoute() {
        return authenticateOAuth2("realm", authenticator, token ->
            path("orders", () ->
                get(() -> complete("orders"))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{Source: source, Language: "java", Framework: "akka-http"})
	orders := routeWithPath(r, "orders")
	if orders == nil {
		t.Fatal("expected orders route")
	}
	if got := propStr(orders, "auth_required"); got != "true" {
		t.Errorf("orders auth_required = %q, want \"true\"", got)
	}
	if got := propStr(orders, "auth_mechanism"); got != "oauth2" {
		t.Errorf("orders auth_mechanism = %q, want \"oauth2\"", got)
	}
}

// Akka authorize(hasRole("ADMIN")) → roles surfaced.
func TestAkkaHTTPAuth_AuthorizeRole_Issue3862(t *testing.T) {
	source := `
package com.example;
import static akka.http.javadsl.server.Directives.*;
public class Routes {
    Route createRoute() {
        return authenticateBasic("realm", auth, user ->
            authorize(() -> user.hasRole("ADMIN"), () ->
                path("admin", () -> get(() -> complete("ok")))
            )
        );
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{Source: source, Language: "java", Framework: "akka-http"})
	admin := routeWithPath(r, "admin")
	if admin == nil {
		t.Fatal("expected admin route")
	}
	if got := propStr(admin, "auth_required"); got != "true" {
		t.Errorf("admin auth_required = %q, want \"true\"", got)
	}
	if got := propStr(admin, "auth_mechanism"); got != "basic" {
		t.Errorf("admin auth_mechanism = %q, want \"basic\"", got)
	}
	if got := propStr(admin, "auth_roles"); got != "ADMIN" {
		t.Errorf("admin auth_roles = %q, want \"ADMIN\"", got)
	}
}

// Negative: a plain Akka route with no auth directive → auth_required absent.
func TestAkkaHTTPAuth_Unprotected_Issue3862(t *testing.T) {
	source := `
package com.example;
import static akka.http.javadsl.server.Directives.*;
public class Routes {
    Route createRoute() {
        return path("health", () -> get(() -> complete("ok")));
    }
}
`
	r := ExtractAkkaHTTP(PatternContext{Source: source, Language: "java", Framework: "akka-http"})
	h := routeWithPath(r, "health")
	if h == nil {
		t.Fatal("expected health route")
	}
	if _, ok := h.Properties["auth_required"]; ok {
		t.Errorf("health should have no auth_required")
	}
}

// ── Struts: roles interceptor in struts.xml with allowedRoles → roles ───────

func TestStrutsAuth_RolesInterceptorXML_Issue3862(t *testing.T) {
	source := `<?xml version="1.0"?>
<struts>
  <package name="secure" namespace="/admin" extends="struts-default">
    <interceptor-ref name="roles">
      <param name="allowedRoles">ADMIN,MANAGER</param>
    </interceptor-ref>
    <action name="dashboard" class="com.example.DashboardAction" method="show">
      <result>/dashboard.jsp</result>
    </action>
  </package>
</struts>`
	r := ExtractStruts(PatternContext{Source: source, Language: "java", Framework: "struts", FilePath: "struts.xml"})
	dash := routeWithPath(r, "/admin/dashboard")
	if dash == nil {
		t.Fatal("expected /admin/dashboard route")
	}
	if got := propStr(dash, "auth_required"); got != "true" {
		t.Errorf("dashboard auth_required = %q, want \"true\"", got)
	}
	if got := propStr(dash, "auth_roles"); got != "ADMIN,MANAGER" {
		t.Errorf("dashboard auth_roles = %q, want \"ADMIN,MANAGER\"", got)
	}
}

// Negative: struts.xml with no roles interceptor → auth_required absent.
func TestStrutsAuth_UnprotectedXML_Issue3862(t *testing.T) {
	source := `<?xml version="1.0"?>
<struts>
  <package name="public" namespace="/" extends="struts-default">
    <action name="home" class="com.example.HomeAction" method="index">
      <result>/home.jsp</result>
    </action>
  </package>
</struts>`
	r := ExtractStruts(PatternContext{Source: source, Language: "java", Framework: "struts", FilePath: "struts.xml"})
	home := routeWithPath(r, "/home")
	if home == nil {
		t.Fatal("expected /home route")
	}
	if _, ok := home.Properties["auth_required"]; ok {
		t.Errorf("/home should have no auth_required")
	}
}

// ── Netflix DGS: @PreAuthorize / @Secured on @DgsQuery → auth on the endpoint ─

func TestDGSAuth_SecuredQuery_Issue3862(t *testing.T) {
	source := `
package com.example;
import com.netflix.graphql.dgs.DgsComponent;
import com.netflix.graphql.dgs.DgsQuery;
import com.netflix.graphql.dgs.DgsMutation;
import org.springframework.security.access.annotation.Secured;
import org.springframework.security.access.prepost.PreAuthorize;

@DgsComponent
public class UserFetcher {
    @DgsQuery
    public List<User> users() { return repo.findAll(); }

    @DgsQuery
    @Secured("ROLE_ADMIN")
    public List<User> allUsers() { return repo.findAll(); }

    @PreAuthorize("hasRole('MANAGER') and hasAuthority('SCOPE_write')")
    @DgsMutation
    public User addUser(String name) { return repo.add(name); }
}
`
	r := ExtractSpringGraphQL(PatternContext{Source: source, Language: "java", Framework: "dgs", FilePath: "UserFetcher.java"})

	// @Secured("ROLE_ADMIN") on allUsers → Query.allUsers endpoint protected.
	all := routeWithPath(r, "/graphql/Query/allUsers")
	if all == nil {
		t.Fatal("expected /graphql/Query/allUsers endpoint")
	}
	if got := propStr(all, "auth_required"); got != "true" {
		t.Errorf("allUsers auth_required = %q, want \"true\"", got)
	}
	if got := propStr(all, "auth_roles"); got != "ADMIN" {
		t.Errorf("allUsers auth_roles = %q, want \"ADMIN\" (ROLE_ stripped)", got)
	}

	// @PreAuthorize on addUser → role MANAGER + scope write.
	add := routeWithPath(r, "/graphql/Mutation/addUser")
	if add == nil {
		t.Fatal("expected /graphql/Mutation/addUser endpoint")
	}
	if got := propStr(add, "auth_roles"); got != "MANAGER" {
		t.Errorf("addUser auth_roles = %q, want \"MANAGER\"", got)
	}
	if got := propStr(add, "auth_scopes"); got != "write" {
		t.Errorf("addUser auth_scopes = %q, want \"write\" (SCOPE_ stripped)", got)
	}

	// users() has no security annotation → unprotected resolver endpoint.
	users := routeWithPath(r, "/graphql/Query/users")
	if users == nil {
		t.Fatal("expected /graphql/Query/users endpoint")
	}
	if _, ok := users.Properties["auth_required"]; ok {
		t.Errorf("users should have no auth_required")
	}
}

// ── Dropwizard: @RolesAllowed on a resource method → auth_required + roles ───

func TestDropwizardAuth_RolesAllowed_Issue3862(t *testing.T) {
	source := `
package com.example;
import javax.annotation.security.RolesAllowed;
import javax.ws.rs.*;

@Path("/admin")
public class AdminResource {
    @GET
    @RolesAllowed("ADMIN")
    public Response stats() { return Response.ok().build(); }

    @GET
    @Path("/me")
    public Response me(@Auth User user) { return Response.ok(user).build(); }
}
`
	r := ExtractDropwizard(PatternContext{Source: source, Language: "java", Framework: "dropwizard", FilePath: "AdminResource.java"})

	// @RolesAllowed("ADMIN") auth entity carries auth_required + auth_roles +
	// auth_guard (the signal archigraph_auth_coverage reads).
	var rolesEnt, authEnt *SecondaryEntity
	for i := range r.Entities {
		switch propStr(&r.Entities[i], "auth_annotation") {
		case "RolesAllowed":
			rolesEnt = &r.Entities[i]
		case "Auth":
			authEnt = &r.Entities[i]
		}
	}
	if rolesEnt == nil {
		t.Fatal("expected a RolesAllowed auth entity")
	}
	if rolesEnt.Properties["auth_required"] != true {
		t.Errorf("RolesAllowed auth_required = %v, want true", rolesEnt.Properties["auth_required"])
	}
	if got := propStr(rolesEnt, "auth_roles"); got != "ADMIN" {
		t.Errorf("RolesAllowed auth_roles = %q, want \"ADMIN\"", got)
	}
	if got := propStr(rolesEnt, "auth_guard"); got != "RolesAllowed" {
		t.Errorf("RolesAllowed auth_guard = %q, want \"RolesAllowed\"", got)
	}

	// @Auth principal injection → auth entity proving the method is authenticated.
	if authEnt == nil {
		t.Fatal("expected an @Auth principal auth entity")
	}
	if authEnt.Properties["auth_required"] != true {
		t.Errorf("@Auth auth_required = %v, want true", authEnt.Properties["auth_required"])
	}
	if got := propStr(authEnt, "auth_guard"); got != "Auth" {
		t.Errorf("@Auth auth_guard = %q, want \"Auth\"", got)
	}
}
