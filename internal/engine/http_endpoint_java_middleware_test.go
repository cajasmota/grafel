package engine

import (
	"testing"
)

// springEndpointMW runs the full synthesis pass over a Java source and returns
// the decoded chain + props for the endpoint at "<VERB> <path>".
func springEndpointMW(t *testing.T, path, content, key string) (chain []middlewareEntry, count, names, scope string) {
	t.Helper()
	eps := authProps(t, "java", path, content)
	e, ok := eps[key]
	if !ok {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keys)
	}
	return mwChain(t, e.Properties["middleware_chain"]),
		e.Properties["middleware_count"],
		e.Properties["middleware_names"],
		e.Properties["middleware_scope"]
}

// TestMiddleware_SpringInterceptorPathMatch asserts a HandlerInterceptor whose
// addPathPatterns("/api/**") matches an /api/x route is bound to that route,
// scope=interceptor, with auth_kind on an auth interceptor.
func TestMiddleware_SpringInterceptorPathMatch(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.servlet.config.annotation.*;
import org.springframework.context.annotation.Configuration;

@RestController
@RequestMapping("/api")
class UserController {
  @GetMapping("/users")
  public Object list() { return null; }
}

@RestController
class PublicController {
  @GetMapping("/health")
  public Object health() { return null; }
}

@Configuration
class WebConfig implements WebMvcConfigurer {
  @Override
  public void addInterceptors(InterceptorRegistry registry) {
    registry.addInterceptor(new AuthInterceptor()).addPathPatterns("/api/**");
  }
}
`
	// The /api/users route IS under /api/** → bound.
	chain, count, names, scope := springEndpointMW(t, "src/main/java/com/x/App.java", src, "GET /api/users")
	if len(chain) != 1 {
		t.Fatalf("/api/users chain len=%d, want 1 (names=%q count=%q)", len(chain), names, count)
	}
	if chain[0].Name != "AuthInterceptor" {
		t.Errorf("chain[0].Name=%q, want AuthInterceptor", chain[0].Name)
	}
	if chain[0].Scope != javaMWScopeInterceptor {
		t.Errorf("chain[0].Scope=%q, want interceptor", chain[0].Scope)
	}
	if chain[0].Order != 0 {
		t.Errorf("chain[0].Order=%d, want 0", chain[0].Order)
	}
	if chain[0].AuthKind != "auth" {
		t.Errorf("AuthInterceptor auth_kind=%q, want auth", chain[0].AuthKind)
	}
	if scope != "interceptor" {
		t.Errorf("scope=%q, want interceptor", scope)
	}

	// The /health route is NOT under /api/** → NOT bound (negative case).
	healthChain, healthCount, _, _ := springEndpointMW(t, "src/main/java/com/x/App.java", src, "ANY /health")
	if len(healthChain) != 0 || (healthCount != "" && healthCount != "0") {
		t.Errorf("/health bound an interceptor (len=%d count=%q), want none — path pattern /api/** does not match",
			len(healthChain), healthCount)
	}
}

// TestMiddleware_SpringFilterAndInterceptorOrder asserts a Servlet filter
// (FilterRegistrationBean urlPatterns) is the OUTERMOST entry and an interceptor
// is inner, with scope "filter+interceptor".
func TestMiddleware_SpringFilterAndInterceptorOrder(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.servlet.config.annotation.*;
import org.springframework.boot.web.servlet.FilterRegistrationBean;
import org.springframework.context.annotation.*;

@RestController
@RequestMapping("/api")
class OrderController {
  @GetMapping("/orders")
  public Object list() { return null; }
}

@Configuration
class Cfg implements WebMvcConfigurer {
  @Bean
  public FilterRegistrationBean<RequestLogFilter> logFilter() {
    FilterRegistrationBean<RequestLogFilter> registration = new FilterRegistrationBean<>();
    registration.setFilter(new RequestLogFilter());
    registration.addUrlPatterns("/api/*");
    registration.setOrder(1);
    return registration;
  }

  @Override
  public void addInterceptors(InterceptorRegistry registry) {
    registry.addInterceptor(new MetricsInterceptor()).addPathPatterns("/api/**");
  }
}
`
	chain, count, names, scope := springEndpointMW(t, "src/main/java/com/x/App.java", src, "GET /api/orders")
	if len(chain) != 2 {
		t.Fatalf("/api/orders chain len=%d, want 2 (names=%q count=%q)", len(chain), names, count)
	}
	// Outermost = filter, inner = interceptor.
	if chain[0].Name != "RequestLogFilter" || chain[0].Scope != javaMWScopeFilter {
		t.Errorf("chain[0]=%+v, want RequestLogFilter/filter", chain[0])
	}
	if chain[1].Name != "MetricsInterceptor" || chain[1].Scope != javaMWScopeInterceptor {
		t.Errorf("chain[1]=%+v, want MetricsInterceptor/interceptor", chain[1])
	}
	if scope != "filter+interceptor" {
		t.Errorf("scope=%q, want filter+interceptor", scope)
	}
}

// TestMiddleware_SpringExcludePathPatterns asserts an excludePathPatterns that
// matches a route un-binds the interceptor for that route (honest negative).
func TestMiddleware_SpringExcludePathPatterns(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.servlet.config.annotation.*;
import org.springframework.context.annotation.Configuration;

@RestController
class ApiController {
  @GetMapping("/api/secure")
  public Object secure() { return null; }
  @GetMapping("/api/public/info")
  public Object info() { return null; }
}

@Configuration
class WebConfig implements WebMvcConfigurer {
  @Override
  public void addInterceptors(InterceptorRegistry registry) {
    registry.addInterceptor(new AuthInterceptor())
            .addPathPatterns("/api/**")
            .excludePathPatterns("/api/public/**");
  }
}
`
	// /api/secure is bound.
	secure, _, _, _ := springEndpointMW(t, "src/main/java/com/x/App.java", src, "ANY /api/secure")
	if len(secure) != 1 || secure[0].Name != "AuthInterceptor" {
		t.Errorf("/api/secure chain=%v, want [AuthInterceptor]", secure)
	}
	// /api/public/info is excluded → NOT bound.
	info, infoCount, _, _ := springEndpointMW(t, "src/main/java/com/x/App.java", src, "ANY /api/public/info")
	if len(info) != 0 || (infoCount != "" && infoCount != "0") {
		t.Errorf("/api/public/info bound (len=%d count=%q), want none — excludePathPatterns(/api/public/**)",
			len(info), infoCount)
	}
}

// TestMiddleware_SpringWildcardUnresolvableSkipped asserts a filter whose
// urlPatterns cannot be statically resolved (no setUrlPatterns at all) is not
// bound — honest-partial.
func TestMiddleware_SpringFilterNoUrlPatternsSkipped(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import org.springframework.boot.web.servlet.FilterRegistrationBean;
import org.springframework.context.annotation.*;

@RestController
class C {
  @GetMapping("/api/thing")
  public Object thing() { return null; }
}

@Configuration
class Cfg {
  @Bean
  public FilterRegistrationBean<MyFilter> f() {
    FilterRegistrationBean<MyFilter> registration = new FilterRegistrationBean<>();
    registration.setFilter(new MyFilter());
    return registration;
  }
}
`
	chain, count, _, _ := springEndpointMW(t, "src/main/java/com/x/App.java", src, "ANY /api/thing")
	if len(chain) != 0 || (count != "" && count != "0") {
		t.Errorf("filter with no urlPatterns bound a chain (len=%d count=%q), want none", len(chain), count)
	}
}

// ---------------------------------------------------------------------------
// #3859 — cross-framework JVM middleware (WebFlux WebFilter / JAX-RS provider
// filter / Javalin before-after)
// ---------------------------------------------------------------------------

// TestMiddleware_WebFluxWebFilter asserts a Spring-WebFlux `implements WebFilter`
// class is bound as a global filter to every reactive route in the file, scope
// "filter". Negative: a class that does NOT implement WebFilter is not bound.
func TestMiddleware_WebFluxWebFilter(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.WebFilter;
import org.springframework.stereotype.Component;
import reactor.core.publisher.Mono;

@RestController
class ApiController {
  @GetMapping("/items")
  public Mono<Object> items() { return null; }
}

@Component
class LoggingWebFilter implements WebFilter {
  public Mono<Void> filter(ServerWebExchange ex, WebFilterChain chain) { return chain.filter(ex); }
}

// A plain helper class that is NOT a WebFilter — must not be bound.
class NotAFilter {
  public void doThing() {}
}
`
	chain, count, names, scope := springEndpointMW(t, "src/main/java/com/x/ApiController.java", src, "ANY /items")
	if len(chain) != 1 {
		t.Fatalf("/items chain len=%d, want 1 (names=%q count=%q)", len(chain), names, count)
	}
	if chain[0].Name != "LoggingWebFilter" {
		t.Errorf("chain[0].Name=%q, want LoggingWebFilter", chain[0].Name)
	}
	if chain[0].Scope != javaMWScopeFilter {
		t.Errorf("chain[0].Scope=%q, want filter", chain[0].Scope)
	}
	if scope != "filter" {
		t.Errorf("scope=%q, want filter", scope)
	}
	if names != "LoggingWebFilter" {
		t.Errorf("names=%q, want LoggingWebFilter (NotAFilter must not appear)", names)
	}
}

// TestMiddleware_JaxRsProviderFilter asserts a JAX-RS `@Provider class …
// implements ContainerRequestFilter` is bound as a global filter to every
// resource method in the file, with auth_kind tagged on an auth filter.
// Negative: a `@NameBinding`-restricted filter is NOT globally bound
// (honest-partial — selective activation).
func TestMiddleware_JaxRsProviderFilter(t *testing.T) {
	src := `package com.x;
import javax.ws.rs.*;
import javax.ws.rs.container.ContainerRequestFilter;
import javax.ws.rs.ext.Provider;

@Path("/things")
class ThingResource {
  @GET
  @Path("/{id}")
  public Object get(@PathParam("id") String id) { return null; }
}

@Provider
class AuthRequestFilter implements ContainerRequestFilter {
  public void filter(ContainerRequestContext ctx) {}
}
`
	chain, count, names, scope := springEndpointMW(t, "src/main/java/com/x/ThingResource.java", src, "GET /things/{id}")
	if len(chain) != 1 {
		t.Fatalf("/things/{id} chain len=%d, want 1 (names=%q count=%q)", len(chain), names, count)
	}
	if chain[0].Name != "AuthRequestFilter" {
		t.Errorf("chain[0].Name=%q, want AuthRequestFilter", chain[0].Name)
	}
	if chain[0].Scope != javaMWScopeFilter {
		t.Errorf("chain[0].Scope=%q, want filter", chain[0].Scope)
	}
	if chain[0].AuthKind != "auth" {
		t.Errorf("AuthRequestFilter auth_kind=%q, want auth", chain[0].AuthKind)
	}
	if scope != "filter" {
		t.Errorf("scope=%q, want filter", scope)
	}
}

// TestMiddleware_JaxRsNameBindingSkipped asserts a JAX-RS filter restricted by a
// @NameBinding meta-annotation is NOT globally bound (honest-partial).
func TestMiddleware_JaxRsNameBindingSkipped(t *testing.T) {
	src := `package com.x;
import javax.ws.rs.*;
import javax.ws.rs.container.ContainerRequestFilter;
import javax.ws.rs.ext.Provider;

@Path("/things")
class ThingResource {
  @GET
  public Object list() { return null; }
}

@Provider
@Secured
@NameBinding
class SecuredFilter implements ContainerRequestFilter {
  public void filter(ContainerRequestContext ctx) {}
}
`
	chain, count, _, _ := springEndpointMW(t, "src/main/java/com/x/ThingResource.java", src, "GET /things")
	if len(chain) != 0 || (count != "" && count != "0") {
		t.Errorf("@NameBinding filter bound globally (len=%d count=%q), want none — selective activation honest-partial",
			len(chain), count)
	}
}

// TestMiddleware_JavalinBeforeAfter asserts Javalin `before("/api/*", …)` binds
// to the matching route and a bare `after(…)` binds to every route. Negative: a
// `before("/admin/*", …)` does NOT bind a non-/admin route.
func TestMiddleware_JavalinBeforeAfter(t *testing.T) {
	src := `package com.x;
import io.javalin.Javalin;

class App {
  public static void main(String[] args) {
    Javalin app = Javalin.create();
    app.before("/api/*", ctx -> { });
    app.before("/admin/*", ctx -> { });
    app.get("/api/ping", ctx -> ctx.result("pong"));
    app.after(ctx -> { });
  }
}
`
	chain, count, names, scope := springEndpointMW(t, "src/main/java/com/x/App.java", src, "GET /api/ping")
	if len(chain) != 2 {
		t.Fatalf("/api/ping chain len=%d, want 2 (names=%q count=%q)", len(chain), names, count)
	}
	// before("/api/*") matched (glob hits /api/ping); after (no path) is global.
	// before("/admin/*") must NOT appear (negative).
	gotNames := map[string]bool{}
	for _, e := range chain {
		gotNames[e.Name] = true
		if e.Scope != javaMWScopeFilter {
			t.Errorf("entry %q scope=%q, want filter", e.Name, e.Scope)
		}
	}
	if !gotNames["javalin:before(/api/*)"] {
		t.Errorf("missing javalin:before(/api/*) (names=%q)", names)
	}
	if !gotNames["javalin:after"] {
		t.Errorf("missing global javalin:after (names=%q)", names)
	}
	if gotNames["javalin:before(/admin/*)"] {
		t.Errorf("javalin:before(/admin/*) wrongly bound to /api/ping (names=%q)", names)
	}
	if scope != "filter" {
		t.Errorf("scope=%q, want filter", scope)
	}
}

// TestMiddleware_WebFluxRateLimitParity asserts a Spring-WebFlux annotated
// handler (`@GetMapping Mono<User>`) carrying `@RateLimiter(name="api")` gets the
// same rate-limit posture as a Spring-MVC handler — WebFlux/MVC parity.
func TestMiddleware_WebFluxRateLimitParity(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import io.github.resilience4j.ratelimiter.annotation.RateLimiter;
import reactor.core.publisher.Mono;

@RestController
@RequestMapping("/api")
class ReactiveController {
  @RateLimiter(name="api")
  @GetMapping("/users/{id}")
  public Mono<User> get(@PathVariable String id) { return null; }
}
`
	eps := authProps(t, "java", "src/main/java/com/x/ReactiveController.java", src)
	e, ok := eps["GET /api/users/{id}"]
	if !ok {
		t.Fatalf("reactive endpoint not synthesised (got: %v)", keysOf(eps))
	}
	if e.Properties["rate_limited"] != "true" {
		t.Errorf("reactive @RateLimiter: rate_limited=%q, want true (props: %v)", e.Properties["rate_limited"], e.Properties)
	}
	// The limiter name from `name="api"` is folded into the evidence (enriched
	// over the original #4023 bare `@RateLimiter`; WebFlux parity preserved).
	if e.Properties["rate_limit_source"] != "@RateLimiter(api)" {
		t.Errorf("rate_limit_source=%q, want @RateLimiter(api)", e.Properties["rate_limit_source"])
	}
	// Honest-partial: config-driven limit → rate omitted.
	if e.Properties["rate_limit"] != "" {
		t.Errorf("rate_limit=%q, want omitted (config-driven)", e.Properties["rate_limit"])
	}
}
