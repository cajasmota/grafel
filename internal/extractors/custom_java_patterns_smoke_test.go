package extractors

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// custom_java_patterns_smoke_test.go — end-to-end proof for #3586.
//
// These tests drive the FULL live custom-extractor dispatch
// (RunCustomExtractors → CustomExtractorsFor("java") → prefix selection →
// custom_java_patterns.Extract) on representative Java sources and assert that
// SPECIFIC entities and relationships now reach the graph through the live path.
//
// Before this PR the 35 Extract*(ctx PatternContext) PatternResult functions had
// zero non-test callers; PatternContext was only built in unit tests, so none of
// these entities/relationships were ever emitted by the indexer. A passing
// assertion here therefore proves the dead layer is genuinely wired, not merely
// unit-callable. Assertions are value-specific (exact Kind/Name and an exact
// relationship Kind+ToID), never len > 0.
//
// #6105 — THE EXPECTED REFS GAINED A `java:` SEGMENT. Every ref asserted below
// is now `scope:<kind>:<subtype>:java:<file>:<name>` rather than the five-segment
// `scope:<kind>:<subtype>:<file>:<name>` these tests previously pinned. The old
// shape was the defect: the resolver's Format A takes six segments
// (internal/resolve/refs.go:69) and rejects anything else outright, so no ref
// this package minted could ever bind. patternResultToRecords canonicalises them
// at the output boundary — see canonicalStructuralRef. The Extract* functions
// still build the five-segment form internally, so their own unit tests are
// unaffected; only what LEAVES the package changed.

// runJavaPatterns dispatches the live java custom-extractor pass and returns the
// emitted records. It asserts the custom_java_patterns extractor is actually
// selected for "java" so a regression in dispatch wiring fails loudly.
func runJavaPatterns(t *testing.T, path, content string) []types.EntityRecord {
	t.Helper()

	selected := false
	for _, e := range CustomExtractorsFor("java") {
		if e.Language() == "custom_java_patterns" {
			selected = true
			break
		}
	}
	if !selected {
		t.Fatalf("custom_java_patterns is NOT selected by CustomExtractorsFor(\"java\") — " +
			"the pattern dispatch extractor would never run live")
	}

	ents, errs := RunCustomExtractors(context.Background(), FileInput{
		Path:     path,
		Language: "java",
		Content:  []byte(content),
	})
	for _, err := range errs {
		t.Fatalf("custom dispatch returned error for %s: %v", path, err)
	}
	return ents
}

// findRecord returns the first record matching kind+name, or nil.
func findRecord(recs []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == kind && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

// hasRel reports whether rec carries an embedded relationship of the given kind
// to the given ToID (structural ref).
func hasRel(rec *types.EntityRecord, kind, toID string) bool {
	if rec == nil {
		return false
	}
	for _, r := range rec.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestJavaPatternsSpringControllerLive proves Spring Boot DI + request-mapping
// extraction reaches the graph through the live dispatch path.
func TestJavaPatternsSpringControllerLive(t *testing.T) {
	src := `
package com.example.api;

import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.stereotype.Service;
import org.springframework.beans.factory.annotation.Autowired;

@RestController
@RequestMapping("/api/users")
public class UserController {

    @GetMapping("/{id}")
    public User getUser(Long id) {
        return null;
    }
}

@Service
public class UserService {

    @Autowired
    private UserRepository userRepository;
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/api/UserController.java", src)

	// 1. The HTTP endpoint operation must emit with method + resolved path.
	ep := findRecord(recs, "SCOPE.Operation", "UserController.getUser")
	if ep == nil {
		t.Fatalf("expected SCOPE.Operation UserController.getUser endpoint to emit live; got %v", names(recs))
	}
	if got := ep.Properties["http_method"]; got != "GET" {
		t.Errorf("endpoint http_method = %q, want GET", got)
	}
	if got := ep.Properties["path"]; got != "/api/users/{id}" {
		t.Errorf("endpoint path = %q, want /api/users/{id}", got)
	}

	// 2. The @Service stereotype component must emit.
	svc := findRecord(recs, "SCOPE.Component", "UserService")
	if svc == nil {
		t.Fatalf("expected SCOPE.Component UserService stereotype to emit; got %v", names(recs))
	}
	if got := svc.Properties["stereotype"]; got != "service" {
		t.Errorf("UserService stereotype = %q, want service", got)
	}

	// 3. The @Autowired DI edge UserService -> UserRepository must emit as an
	//    embedded DEPENDS_ON relationship on the service's stereotype entity.
	wantTarget := "scope:dependency:spring_boot:java:" +
		"src/main/java/com/example/api/UserController.java:UserRepository"
	if !hasRel(svc, "DEPENDS_ON", wantTarget) {
		t.Errorf("expected DEPENDS_ON edge from UserService to UserRepository (%s); got rels %v",
			wantTarget, svc.Relationships)
	}
}

// TestJavaPatternsJpaEntityLive proves JPA/Hibernate entity + association
// extraction reaches the graph through the live dispatch path.
func TestJavaPatternsJpaEntityLive(t *testing.T) {
	src := `
package com.example.model;

import jakarta.persistence.Entity;
import jakarta.persistence.Table;
import jakarta.persistence.OneToMany;

@Entity
@Table(name = "orders")
public class Order {

    @OneToMany
    private List<LineItem> items;
}
`
	recs := runJavaPatterns(t, "src/main/java/com/example/model/Order.java", src)

	order := findRecord(recs, "SCOPE.Schema", "Order")
	if order == nil {
		t.Fatalf("expected SCOPE.Schema Order entity to emit live; got %v", names(recs))
	}
	if got := order.Properties["table_name"]; got != "orders" {
		t.Errorf("Order table_name = %q, want orders", got)
	}

	// The @OneToMany association Order -> LineItem must emit as a DEPENDS_ON edge.
	wantTarget := "scope:schema:hibernate_entity:java:" +
		"src/main/java/com/example/model/Order.java:LineItem"
	if !hasRel(order, "DEPENDS_ON", wantTarget) {
		t.Errorf("expected DEPENDS_ON association edge Order -> LineItem (%s); got rels %v",
			wantTarget, order.Relationships)
	}
}

// TestJavaPatternsAndroidActivityLive proves Android component extraction reaches
// the graph through the live dispatch path.
func TestJavaPatternsAndroidActivityLive(t *testing.T) {
	src := `
package com.example.app;

import android.app.Activity;
import android.os.Bundle;
import android.content.Intent;

public class MainActivity extends Activity {

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        Intent intent = new Intent(this, DetailActivity.class);
        startActivity(intent);
    }
}
`
	recs := runJavaPatterns(t, "app/src/main/java/com/example/app/MainActivity.java", src)

	// The Activity must emit as a SCOPE.UIComponent (subtype=component) screen.
	act := findRecord(recs, "SCOPE.UIComponent", "MainActivity")
	if act == nil {
		t.Fatalf("expected SCOPE.UIComponent MainActivity (Android Activity) to emit live; got %v", names(recs))
	}
	if prov := act.Properties["provenance"]; prov != "INFERRED_FROM_ANDROID_ACTIVITY" {
		t.Errorf("MainActivity provenance = %q, want INFERRED_FROM_ANDROID_ACTIVITY", prov)
	}

	// The explicit Intent(this, DetailActivity.class) navigation must emit as a
	// SCOPE.Operation navigation edge MainActivity->DetailActivity.
	nav := findRecord(recs, "SCOPE.Operation", "MainActivity->DetailActivity")
	if nav == nil {
		t.Fatalf("expected SCOPE.Operation MainActivity->DetailActivity intent navigation to emit live; got %v", names(recs))
	}
}

// names is a compact dump of (Kind,Name) pairs for failure messages.
func names(recs []types.EntityRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Kind+"/"+r.Name)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// DI / AOP / Transactions / Validation re-verification (issue #3589, epic #3584)
//
// These value-asserting tests drive the LIVE custom dispatch (via runJavaPatterns)
// and prove the now-wired DI/AOP/Tx/validation pattern extractors emit specific
// entities/edges. They are the evidence backing the coverage re-flips in
// docs/coverage/registry.json for lang.java.framework.{spring-boot,spring-webflux,
// micronaut,jaxrs} and lang.java.validation.bean-validation.
// ─────────────────────────────────────────────────────────────────────────────

// findByRef returns the first record whose Properties["ref"] equals ref.
func findByRef(recs []types.EntityRecord, ref string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Properties["ref"] == ref {
			return &recs[i]
		}
	}
	return nil
}

// TestJavaPatternsSpringAOPLive proves Spring AOP aspect_extraction,
// advice_attribution, and pointcut_resolution reach the graph live.
func TestJavaPatternsSpringAOPLive(t *testing.T) {
	src := `
package x;
import org.springframework.stereotype.Component;
import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Pointcut;
import org.aspectj.lang.annotation.Around;
@Aspect
@Component
public class LoggingAspect {
    @Pointcut("execution(* com.example..*(..))")
    public void anyOp() {}
    @Around("anyOp()")
    public Object logAround(ProceedingJoinPoint pjp) throws Throwable { return pjp.proceed(); }
}
`
	recs := runJavaPatterns(t, "src/main/java/x/LoggingAspect.java", src)

	// aspect_extraction
	aspect := findByRef(recs, "scope:pattern:aspect:java:src/main/java/x/LoggingAspect.java:LoggingAspect")
	if aspect == nil {
		t.Fatalf("expected @Aspect LoggingAspect to emit SCOPE.Pattern(aspect); got %v", names(recs))
	}
	if got := aspect.Properties["kind"]; got != "aspect" {
		t.Errorf("aspect kind = %q, want aspect", got)
	}

	// advice_attribution — advice_type must be the resolved around designator.
	advice := findByRef(recs, "scope:pattern:advice:java:src/main/java/x/LoggingAspect.java:LoggingAspect.logAround")
	if advice == nil {
		t.Fatalf("expected @Around advice LoggingAspect.logAround to emit; got %v", names(recs))
	}
	if got := advice.Properties["advice_type"]; got != "around" {
		t.Errorf("advice_type = %q, want around", got)
	}
	// aspect OWNS advice
	if !hasRel(aspect, "OWNS", advice.Properties["ref"]) {
		t.Errorf("expected aspect OWNS advice edge to %s; got %v", advice.Properties["ref"], aspect.Relationships)
	}

	// pointcut_resolution — pointcut entity + advice REFERENCES pointcut.
	pc := findByRef(recs, "scope:pattern:pointcut:java:src/main/java/x/LoggingAspect.java:LoggingAspect.anyOp")
	if pc == nil {
		t.Fatalf("expected @Pointcut LoggingAspect.anyOp to emit; got %v", names(recs))
	}
	if got := pc.Properties["pointcut_expression"]; got != "execution(* com.example..*(..))" {
		t.Errorf("pointcut_expression = %q", got)
	}
	if !hasRel(advice, "REFERENCES", pc.Properties["ref"]) {
		t.Errorf("expected advice REFERENCES pointcut edge to %s; got %v", pc.Properties["ref"], advice.Relationships)
	}
}

// TestJavaPatternsTransactionalLive proves @Transactional boundary, propagation,
// and rollback-rule extraction reach the graph live.
func TestJavaPatternsTransactionalLive(t *testing.T) {
	src := `
package x;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.annotation.Propagation;
@Service
public class OrderService {
    @Transactional(propagation = Propagation.REQUIRES_NEW, rollbackFor = IllegalStateException.class)
    public void placeOrder() {}
}
`
	recs := runJavaPatterns(t, "src/main/java/x/OrderService.java", src)

	tx := findByRef(recs, "scope:pattern:transaction_boundary:java:src/main/java/x/OrderService.java:OrderService.placeOrder")
	if tx == nil {
		t.Fatalf("expected @Transactional method to emit a transaction_boundary entity; got %v", names(recs))
	}
	if got := tx.Properties["transaction_boundary"]; got != "method" {
		t.Errorf("transaction_boundary = %q, want method", got)
	}
	// transaction_propagation
	if got := tx.Properties["propagation"]; got != "REQUIRES_NEW" {
		t.Errorf("propagation = %q, want REQUIRES_NEW", got)
	}
	// transaction_rollback_rules
	if got := tx.Properties["rollback_for"]; got != "IllegalStateException" {
		t.Errorf("rollback_for = %q, want IllegalStateException", got)
	}
}

// TestJavaPatternsMicronautDIAOPLive proves Micronaut DI binding/injection/scope,
// AOP aspect/advice, and HttpServerFilter middleware reach the graph live.
func TestJavaPatternsMicronautDIAOPLive(t *testing.T) {
	bean := `
package x;
import io.micronaut.context.annotation.Bean;
import jakarta.inject.Singleton;
@Singleton
public class OrderService {
    private final OrderRepository repo;
    public OrderService(OrderRepository repo) { this.repo = repo; }
}
`
	recs := runJavaPatterns(t, "src/main/java/x/OrderService.java", bean)
	// di_binding_extraction + di_scope_resolution: @Singleton bean.
	svc := findByRef(recs, "scope:service:micronaut_bean:java:src/main/java/x/OrderService.java:OrderService")
	if svc == nil {
		t.Fatalf("expected @Singleton OrderService micronaut bean; got %v", names(recs))
	}
	if got := svc.Properties["scope"]; got != "Singleton" {
		t.Errorf("micronaut bean scope = %q, want Singleton", got)
	}
	// di_injection_point: constructor DEPENDS_ON OrderRepository.
	if !hasRel(svc, "DEPENDS_ON", "scope:dependency:micronaut:java:src/main/java/x/OrderService.java:OrderRepository") {
		t.Errorf("expected ctor DEPENDS_ON OrderRepository edge; got %v", svc.Relationships)
	}

	interceptor := `
package x;
import io.micronaut.aop.MethodInterceptor;
import io.micronaut.aop.MethodInvocationContext;
import jakarta.inject.Singleton;
@Singleton
public class LoggingInterceptor implements MethodInterceptor<Object, Object> {
    public Object intercept(MethodInvocationContext<Object, Object> context) { return context.proceed(); }
}
`
	recs2 := runJavaPatterns(t, "src/main/java/x/LoggingInterceptor.java", interceptor)
	// AOP aspect_extraction: MethodInterceptor class is an aspect.
	asp := findByRef(recs2, "scope:pattern:aspect:java:src/main/java/x/LoggingInterceptor.java:LoggingInterceptor")
	if asp == nil {
		t.Fatalf("expected MethodInterceptor to emit aspect; got %v", names(recs2))
	}
	// AOP advice_attribution: intercept() method advice_type=around + OWNS.
	adv := findByRef(recs2, "scope:pattern:advice:java:src/main/java/x/LoggingInterceptor.java:LoggingInterceptor.intercept")
	if adv == nil {
		t.Fatalf("expected intercept() advice; got %v", names(recs2))
	}
	if got := adv.Properties["advice_type"]; got != "around" {
		t.Errorf("micronaut advice_type = %q, want around", got)
	}
	if !hasRel(asp, "OWNS", adv.Properties["ref"]) {
		t.Errorf("expected interceptor OWNS advice edge; got %v", asp.Relationships)
	}

	filter := `
package x;
import io.micronaut.http.annotation.Filter;
import io.micronaut.http.filter.HttpServerFilter;
@Filter("/**")
public class AuthFilter implements HttpServerFilter {
}
`
	recs3 := runJavaPatterns(t, "src/main/java/x/AuthFilter.java", filter)
	// middleware_coverage: HttpServerFilter.
	mw := findByRef(recs3, "scope:component:micronaut_filter:java:src/main/java/x/AuthFilter.java:AuthFilter")
	if mw == nil {
		t.Fatalf("expected @Filter HttpServerFilter middleware; got %v", names(recs3))
	}
	if got := mw.Properties["middleware"]; got != "http_server_filter" {
		t.Errorf("micronaut middleware = %q, want http_server_filter", got)
	}
}

// TestJavaPatternsJaxrsCDILive proves JAX-RS / CDI AOP (aspect/advice) and the
// @Provider filter middleware reach the graph live.
func TestJavaPatternsJaxrsCDILive(t *testing.T) {
	cdi := `
package x;
import jakarta.interceptor.Interceptor;
import jakarta.interceptor.AroundInvoke;
import jakarta.interceptor.InvocationContext;
import jakarta.ws.rs.Path;
@Interceptor
public class LoggingInterceptor {
    @AroundInvoke
    public Object logInvocation(InvocationContext ctx) throws Exception { return ctx.proceed(); }
}
`
	recs := runJavaPatterns(t, "src/main/java/x/LoggingInterceptor.java", cdi)
	asp := findByRef(recs, "scope:pattern:cdi_interceptor:java:src/main/java/x/LoggingInterceptor.java:LoggingInterceptor")
	if asp == nil {
		t.Fatalf("expected @Interceptor CDI aspect; got %v", names(recs))
	}
	if got := asp.Properties["kind"]; got != "cdi_interceptor" {
		t.Errorf("CDI aspect kind = %q, want cdi_interceptor", got)
	}
	adv := findByRef(recs, "scope:pattern:cdi_advice:java:src/main/java/x/LoggingInterceptor.java:LoggingInterceptor.logInvocation")
	if adv == nil {
		t.Fatalf("expected @AroundInvoke advice; got %v", names(recs))
	}
	if got := adv.Properties["advice_type"]; got != "around_invoke" {
		t.Errorf("CDI advice_type = %q, want around_invoke", got)
	}
	if !hasRel(asp, "OWNS", adv.Properties["ref"]) {
		t.Errorf("expected interceptor OWNS advice edge; got %v", asp.Relationships)
	}

	filter := `
package x;
import jakarta.ws.rs.ext.Provider;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.container.ContainerRequestContext;
@Provider
public class AuthFilter implements ContainerRequestFilter {
    public void filter(ContainerRequestContext ctx) {}
}
`
	recs2 := runJavaPatterns(t, "src/main/java/x/AuthFilter.java", filter)
	mw := findByRef(recs2, "scope:component:jaxrs_filter:java:src/main/java/x/AuthFilter.java:AuthFilter")
	if mw == nil {
		t.Fatalf("expected @Provider ContainerRequestFilter middleware; got %v", names(recs2))
	}
	if got := mw.Properties["filter_type"]; got != "container_request_filter" {
		t.Errorf("jaxrs filter_type = %q, want container_request_filter", got)
	}
}

// TestJavaPatternsJaxrsDIScopeLive proves JAX-RS/CDI di_scope_resolution
// (@RequestScoped) AND di_binding (the bean entity) reach the graph live.
// NOTE: the @Inject/@EJB injection-point DEPENDS_ON edge is intentionally NOT
// asserted here — it is dropped by the no-carrier policy in
// patternResultToRecords (see di_injection_point left missing for jaxrs).
func TestJavaPatternsJaxrsDIScopeLive(t *testing.T) {
	src := `
package x;
import jakarta.ws.rs.Path;
import jakarta.enterprise.context.RequestScoped;
@Path("/users")
@RequestScoped
public class UserResource {
}
`
	recs := runJavaPatterns(t, "src/main/java/x/UserResource.java", src)
	scoped := findByRef(recs, "scope:component:cdi_scoped_bean:java:src/main/java/x/UserResource.java:UserResource")
	if scoped == nil {
		t.Fatalf("expected @RequestScoped CDI scope component; got %v", names(recs))
	}
	if got := scoped.Properties["cdi_scope"]; got != "RequestScoped" {
		t.Errorf("cdi_scope = %q, want RequestScoped", got)
	}
}

// TestJavaPatternsSpringWebfluxMiddlewareLive proves spring-webflux WebFilter
// middleware reaches the graph live (the spring_webflux token activates via the
// reactor/Mono marker present in the source).
func TestJavaPatternsSpringWebfluxMiddlewareLive(t *testing.T) {
	src := `
package x;
import org.springframework.web.server.WebFilter;
import org.springframework.web.server.ServerWebExchange;
import org.springframework.web.server.WebFilterChain;
import reactor.core.publisher.Mono;
public class AuthWebFilter implements WebFilter {
    public Mono<Void> filter(ServerWebExchange exchange, WebFilterChain chain) { return chain.filter(exchange); }
}
`
	recs := runJavaPatterns(t, "src/main/java/x/AuthWebFilter.java", src)
	mw := findRecord(recs, "Middleware", "AuthWebFilter")
	if mw == nil {
		t.Fatalf("expected WebFilter Middleware AuthWebFilter; got %v", names(recs))
	}
	if got := mw.Properties["middleware_type"]; got != "web_filter" {
		t.Errorf("webflux middleware_type = %q, want web_filter", got)
	}
}

// TestJavaPatternsBeanValidationLive proves bean-validation schema_extraction
// (field constraints) and custom_validator_extraction reach the graph live.
// The nested-model VALIDATES edge is NOT asserted (dropped by no-carrier policy;
// nested_model_extraction is recorded partial, not full).
func TestJavaPatternsBeanValidationLive(t *testing.T) {
	dto := `
package x;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Email;
import jakarta.validation.Valid;
public class CreateUserRequest {
    @NotNull
    @Email
    private String email;
    @Valid
    private Address address;
}
`
	recs := runJavaPatterns(t, "src/main/java/x/CreateUserRequest.java", dto)
	// schema_extraction: field-level constraints.
	field := findByRef(recs, "scope:schema:bean_validation_field:java:src/main/java/x/CreateUserRequest.java:CreateUserRequest.email")
	if field == nil {
		t.Fatalf("expected @NotNull @Email field schema entity; got %v", names(recs))
	}
	if got := field.Properties["constraints"]; got != "@NotNull,@Email" {
		t.Errorf("field constraints = %q, want @NotNull,@Email", got)
	}
	// nested_model_extraction: the @Valid nested field entity still emits.
	nested := findByRef(recs, "scope:schema:bean_validation_field:java:src/main/java/x/CreateUserRequest.java:CreateUserRequest.address")
	if nested == nil {
		t.Fatalf("expected @Valid nested field schema entity; got %v", names(recs))
	}

	cv := `
package x;
import jakarta.validation.ConstraintValidator;
import jakarta.validation.ConstraintValidatorContext;
public class PhoneValidator implements ConstraintValidator<ValidPhone, String> {
    public boolean isValid(String value, ConstraintValidatorContext ctx) { return true; }
}
`
	recs2 := runJavaPatterns(t, "src/main/java/x/PhoneValidator.java", cv)
	// custom_validator_extraction: ConstraintValidator implementor.
	cve := findByRef(recs2, "scope:custom_validator:bean_validation:java:src/main/java/x/PhoneValidator.java:PhoneValidator")
	if cve == nil {
		t.Fatalf("expected ConstraintValidator SCOPE.CustomValidator; got %v", names(recs2))
	}
	if got := cve.Properties["annotation_type"]; got != "ValidPhone" {
		t.Errorf("custom validator annotation_type = %q, want ValidPhone", got)
	}
	if got := cve.Properties["validated_type"]; got != "String" {
		t.Errorf("custom validator validated_type = %q, want String", got)
	}
}

// TestJavaPatternsSpringDataJpaAssociationLive proves that Spring Data JPA
// entities — not just plain JPA/Hibernate ones — reach the graph through the
// live dispatch path with their @OneToMany/@ManyToOne associations intact.
//
// Spring Data JPA entities are ordinary jakarta.persistence @Entity classes, so
// the same hibernate.go extractor handles them. The framework gate
// hibernateFrameworks already admits the "spring_data_jpa" token, and
// frameworkMarkers maps "org.springframework.data.jpa" -> "spring_data_jpa", so
// a file importing the Spring Data JPA package is dispatched into ExtractHibernate.
// This test asserts the association emerges as a directed DEPENDS_ON edge with
// association_kind=OneToMany, which is exactly what association_extraction and
// relationship_extraction credit for the sibling jpa/hibernate ORM records.
func TestJavaPatternsSpringDataJpaAssociationLive(t *testing.T) {
	src := `
package com.example.model;

import jakarta.persistence.Entity;
import jakarta.persistence.Table;
import jakarta.persistence.OneToMany;
import org.springframework.data.jpa.repository.JpaRepository;

@Entity
@Table(name = "orders")
public class Order {

    @OneToMany
    private List<LineItem> items;
}
`
	path := "src/main/java/com/example/model/Order.java"
	recs := runJavaPatterns(t, path, src)

	order := findRecord(recs, "SCOPE.Schema", "Order")
	if order == nil {
		t.Fatalf("expected SCOPE.Schema Order entity to emit live for spring_data_jpa; got %v", names(recs))
	}
	if got := order.Properties["table_name"]; got != "orders" {
		t.Errorf("Order table_name = %q, want orders", got)
	}

	// relationship_extraction: the @OneToMany association Order -> LineItem must
	// emit as a directed DEPENDS_ON edge, identical to the plain-JPA path.
	wantTarget := "scope:schema:hibernate_entity:java:" + path + ":LineItem"
	if !hasRel(order, "DEPENDS_ON", wantTarget) {
		t.Errorf("expected DEPENDS_ON association edge Order -> LineItem (%s) for spring_data_jpa; got rels %v",
			wantTarget, order.Relationships)
	}

	// association_extraction: the edge must carry association_kind=OneToMany so
	// the association classifier (not merely "some dependency") is satisfied.
	foundKind := false
	for _, r := range order.Relationships {
		if r.Kind == "DEPENDS_ON" && r.ToID == wantTarget {
			if r.Properties.Get("association_kind") != "OneToMany" {
				t.Errorf("association_kind = %q, want OneToMany", r.Properties.Get("association_kind"))
			}
			foundKind = true
		}
	}
	if !foundKind {
		t.Errorf("expected the Order -> LineItem DEPENDS_ON edge to carry association_kind=OneToMany; got rels %v",
			order.Relationships)
	}
}
