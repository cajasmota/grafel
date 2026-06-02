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
