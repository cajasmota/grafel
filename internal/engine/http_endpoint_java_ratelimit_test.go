package engine

import "testing"

// javaRLProps runs full synthesis over a Java source and returns the endpoint
// at "<VERB> <path>".
func javaRLProps(t *testing.T, content, key string) map[string]string {
	t.Helper()
	eps := authProps(t, "java", "src/main/java/com/x/App.java", content)
	e, ok := eps[key]
	if !ok {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keys)
	}
	return e.Properties
}

// TestJavaRateLimit_Resilience4jAnnotation — the canonical spec case:
// `@RateLimiter(name="api")` on a `@GetMapping` method → that endpoint is
// rate_limited=true (rate honest-partial: it lives in config); a sibling method
// with no throttle is NOT stamped (negative).
func TestJavaRateLimit_Resilience4jAnnotation(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import io.github.resilience4j.ratelimiter.annotation.RateLimiter;
@RestController
@RequestMapping("/api")
class ApiController {
  @RateLimiter(name="api")
  @GetMapping("/x")
  public Object getX() { return null; }

  @GetMapping("/free")
  public Object free() { return null; }
}
`
	p := javaRLProps(t, src, "GET /api/x")
	if p["rate_limited"] != "true" {
		t.Errorf("GET /api/x: rate_limited=%q, want true (props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit_scope"] != "route" {
		t.Errorf("GET /api/x: rate_limit_scope=%q, want route", p["rate_limit_scope"])
	}
	// The limiter name from `name="api"` is folded into the evidence so the
	// SPECIFIC limiter (which keys resilience4j.ratelimiter.api in config) is
	// asserted, not just the bare annotation.
	if p["rate_limit_source"] != "@RateLimiter(api)" {
		t.Errorf("GET /api/x: rate_limit_source=%q, want @RateLimiter(api)", p["rate_limit_source"])
	}
	// Honest-partial: a bare Resilience4j @RateLimiter's limit lives in
	// application.yml, so the rate MUST be omitted (never fabricated).
	if p["rate_limit"] != "" {
		t.Errorf("GET /api/x: rate_limit=%q, want omitted (config-driven honest-partial)", p["rate_limit"])
	}

	free := javaRLProps(t, src, "GET /api/free")
	if free["rate_limited"] == "true" {
		t.Errorf("GET /api/free: rate_limited=true, want unthrottled (props: %v)", free)
	}
}

// TestJavaRateLimit_Bucket4jLiteral — a bucket4j `@RateLimiting(capacity = 100)`
// with a literal capacity resolves the rate.
func TestJavaRateLimit_Bucket4jLiteral(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
@RestController
class BucketController {
  @RateLimiting(name = "ep", capacity = 100)
  @PostMapping("/orders")
  public Object create() { return null; }
}
`
	p := javaRLProps(t, src, "ANY /orders")
	if p["rate_limited"] != "true" {
		t.Errorf("/orders: rate_limited=%q, want true (props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit"] != "100/s" {
		t.Errorf("/orders: rate_limit=%q, want 100/s", p["rate_limit"])
	}
	// The bucket4j `name = "ep"` is folded into the evidence (specific limiter).
	if p["rate_limit_source"] != "@RateLimiting(ep)" {
		t.Errorf("/orders: rate_limit_source=%q, want @RateLimiting(ep)", p["rate_limit_source"])
	}
}

// TestJavaRateLimit_SpringCloudGateway — a Spring Cloud Gateway YAML route with
// a RequestRateLimiter filter (replenishRate=10) matched to its Path= predicate
// → endpoints under that path are rate_limited=true rate="10/s" scope=gateway.
func TestJavaRateLimit_SpringCloudGateway(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
@RestController
class GatewayBackedController {
  @GetMapping("/api/items")
  public Object items() { return null; }
}
/* Spring Cloud Gateway route config (application.yml-equivalent):
   - id: items_route
     uri: lb://items
     predicates:
       - Path=/api/**
     filters:
       - name: RequestRateLimiter
         args:
           replenishRate: 10
           burstCapacity: 20
*/
`
	p := javaRLProps(t, src, "ANY /api/items")
	if p["rate_limited"] != "true" {
		t.Errorf("/api/items: rate_limited=%q, want true (props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit"] != "10/s" {
		t.Errorf("/api/items: rate_limit=%q, want 10/s", p["rate_limit"])
	}
	if p["rate_limit_scope"] != "gateway" {
		t.Errorf("/api/items: rate_limit_scope=%q, want gateway", p["rate_limit_scope"])
	}
	if p["rate_limit_source"] != "RequestRateLimiter" {
		t.Errorf("/api/items: rate_limit_source=%q, want RequestRateLimiter", p["rate_limit_source"])
	}
}

// TestJavaRateLimit_NonThrottleUnaffected — a non-throttle annotation
// (@Validated) on a mapped method must NOT stamp a rate limit.
func TestJavaRateLimit_NonThrottleUnaffected(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import org.springframework.validation.annotation.Validated;
@RestController
class PlainController {
  @Validated
  @GetMapping("/plain")
  public Object plain() { return null; }
}
`
	p := javaRLProps(t, src, "ANY /plain")
	if p["rate_limited"] == "true" {
		t.Errorf("/plain: rate_limited=true, want unthrottled (@Validated is not a limiter; props: %v)", p)
	}
}

// TestJavaRateLimit_MVCResilience4jLimiterName — deepen the MVC Resilience4j
// surface: `@RateLimiter(name="orders")` on an MVC `@GetMapping("/orders")`
// handler stamps rate_limited=true scope=route and folds the SPECIFIC limiter
// name into the evidence (`@RateLimiter(orders)` — the resilience4j.ratelimiter
// config key). Rate stays honest-partial: limitForPeriod lives in YAML.
func TestJavaRateLimit_MVCResilience4jLimiterName(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import io.github.resilience4j.ratelimiter.annotation.RateLimiter;
@RestController
class OrderController {
  @RateLimiter(name="orders")
  @GetMapping("/orders")
  public Object orders() { return null; }
}
`
	p := javaRLProps(t, src, "ANY /orders")
	if p["rate_limited"] != "true" {
		t.Errorf("/orders: rate_limited=%q, want true (props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit_scope"] != "route" {
		t.Errorf("/orders: rate_limit_scope=%q, want route", p["rate_limit_scope"])
	}
	if p["rate_limit_source"] != "@RateLimiter(orders)" {
		t.Errorf("/orders: rate_limit_source=%q, want @RateLimiter(orders) (limiter name asserted)", p["rate_limit_source"])
	}
	if p["rate_limit"] != "" {
		t.Errorf("/orders: rate_limit=%q, want omitted (config-driven honest-partial)", p["rate_limit"])
	}
}

// TestJavaRateLimit_Bucket4jTryConsumeGuard — the imperative bucket4j surface
// (NOT an annotation): an `if (!bucket.tryConsume(1)) … 429` guard inside an
// MVC handler body stamps rate_limited=true scope=route source=bucket.tryConsume.
// Rate is honest-partial (the Bandwidth/bucket is built elsewhere). A sibling
// handler with no guard is NOT stamped (negative).
func TestJavaRateLimit_Bucket4jTryConsumeGuard(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
@RestController
class BuyController {
  @GetMapping("/buy")
  public Object buy() {
    if (!bucket.tryConsume(1)) {
      return ResponseEntity.status(429).build();
    }
    return null;
  }

  @GetMapping("/browse")
  public Object browse() { return null; }
}
`
	p := javaRLProps(t, src, "ANY /buy")
	if p["rate_limited"] != "true" {
		t.Errorf("/buy: rate_limited=%q, want true (props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit_scope"] != "route" {
		t.Errorf("/buy: rate_limit_scope=%q, want route", p["rate_limit_scope"])
	}
	if p["rate_limit_source"] != "bucket.tryConsume" {
		t.Errorf("/buy: rate_limit_source=%q, want bucket.tryConsume", p["rate_limit_source"])
	}
	if p["rate_limit"] != "" {
		t.Errorf("/buy: rate_limit=%q, want omitted (bucket bandwidth built elsewhere = honest-partial)", p["rate_limit"])
	}

	browse := javaRLProps(t, src, "ANY /browse")
	if browse["rate_limited"] == "true" {
		t.Errorf("/browse: rate_limited=true, want unthrottled (no tryConsume guard; props: %v)", browse)
	}
}

// TestJavaRateLimit_Bucket4jRateLimitAnnotation — the bucket4j
// `@Bucket4jRateLimit(name="api")` method annotation (whose `RateLimit` suffix
// previously mis-split the annotation regex) is now recognised: rate_limited=true
// with the limiter name folded into the evidence.
func TestJavaRateLimit_Bucket4jRateLimitAnnotation(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
@RestController
class B4jController {
  @Bucket4jRateLimit(name="api")
  @GetMapping("/b4j")
  public Object b() { return null; }
}
`
	p := javaRLProps(t, src, "ANY /b4j")
	if p["rate_limited"] != "true" {
		t.Errorf("/b4j: rate_limited=%q, want true (props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit_source"] != "@Bucket4jRateLimit(api)" {
		t.Errorf("/b4j: rate_limit_source=%q, want @Bucket4jRateLimit(api)", p["rate_limit_source"])
	}
}

// TestJavaRateLimit_WebFluxResilience4jUnregressed — guards the #4023 WebFlux
// parity: a Resilience4j `@RateLimiter` on a WebFlux `@GetMapping` handler
// returning Mono is still stamped (the MVC/WebFlux distinction is handler-shape
// only; the rate-limit pass is shape-agnostic).
func TestJavaRateLimit_WebFluxResilience4jUnregressed(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
import io.github.resilience4j.ratelimiter.annotation.RateLimiter;
import reactor.core.publisher.Mono;
@RestController
class FluxController {
  @RateLimiter(name="flux")
  @GetMapping("/reactive")
  public Mono<Object> reactive() { return Mono.empty(); }
}
`
	p := javaRLProps(t, src, "ANY /reactive")
	if p["rate_limited"] != "true" {
		t.Errorf("/reactive: rate_limited=%q, want true (WebFlux #4023 parity; props: %v)", p["rate_limited"], p)
	}
	if p["rate_limit_source"] != "@RateLimiter(flux)" {
		t.Errorf("/reactive: rate_limit_source=%q, want @RateLimiter(flux)", p["rate_limit_source"])
	}
}

// TestJavaRateLimit_PlainMethodUnaffected — a plain (non-handler, non-mapped)
// method that happens to call tryConsume must NOT stamp any endpoint, and a
// non-rate-limit @GetMapping is untouched (defense against tryConsume binding to
// an unrelated route).
func TestJavaRateLimit_PlainMethodUnaffected(t *testing.T) {
	src := `package com.x;
import org.springframework.web.bind.annotation.*;
@RestController
class MixedController {
  @GetMapping("/open")
  public Object open() { return null; }

  private boolean helper() {
    return someBucket.tryConsume(1);
  }
}
`
	// /open precedes the helper's `someBucket.tryConsume(1)`, but the helper has
	// NO 429 / TOO_MANY_REQUESTS response signal, so it is not recognised as an
	// HTTP rate-limit guard and must NOT stamp the preceding route.
	open := javaRLProps(t, src, "ANY /open")
	if open["rate_limited"] == "true" {
		t.Errorf("/open: rate_limited=true, want unthrottled (tryConsume is in an unmapped helper; props: %v)", open)
	}
}
