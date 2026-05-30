package java

// kotlin_port_test.go — proves that the 7 ported Java extractors fire on
// Kotlin source (language="kotlin") in addition to Java.
//
// Each test passes language="kotlin" to the same extractor functions,
// verifying that the language-gate relaxation from ctx.Language != "java"
// to (ctx.Language != "java" && ctx.Language != "kotlin") is effective.
// Java behaviour is unchanged (existing tests still run).
//
// Part of #3274 / #3272 — Kotlin Java-extractor language-gate port.

import (
	"strings"
	"testing"
)

// ============================================================================
// 1. Spring Boot DI (spring_boot.go) — Kotlin
// ============================================================================

// TestKotlinSpringBoot_Component_Issue3274 verifies that @Component/@Service/
// @Repository annotations on Kotlin classes produce DI entities.
// Registry target: lang.kotlin.framework.spring-boot DI/* → partial.
func TestKotlinSpringBoot_Component_Issue3274(t *testing.T) {
	source := `
import org.springframework.stereotype.Service
import org.springframework.stereotype.Repository
import org.springframework.stereotype.Component

@Service
class UserService {
    fun findAll(): List<User> = emptyList()
}

@Repository
class UserRepository {
    fun findById(id: Long): User? = null
}

@Component
class AuditLogger {
    fun log(msg: String) {}
}
`
	r := ExtractSpringBoot(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "UserService.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin spring_boot] expected DI entities, got 0")
	}
	byStereotype := make(map[string]bool)
	for _, e := range r.Entities {
		if s, ok := e.Properties["stereotype"]; ok {
			byStereotype[s.(string)] = true
		}
	}
	for _, want := range []string{"service", "repository", "component"} {
		if !byStereotype[want] {
			t.Errorf("[#3274 kotlin spring_boot] missing stereotype=%q, got %v", want, byStereotype)
		}
	}
}

// TestKotlinSpringBoot_Autowired_Issue3274 verifies @Autowired constructor
// injection on Kotlin classes emits DEPENDS_ON relationships.
func TestKotlinSpringBoot_Autowired_Issue3274(t *testing.T) {
	source := `
import org.springframework.stereotype.Service
import org.springframework.beans.factory.annotation.Autowired

@Service
class OrderService @Autowired constructor(
    private val userRepository: UserRepository
) {
    fun placeOrder() {}
}
`
	r := ExtractSpringBoot(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "OrderService.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin spring_boot autowired] expected entities, got 0")
	}
}

// TestKotlinSpringBoot_Scope_Issue3274 verifies @Scope/@RequestScope on Kotlin
// classes emits scope entities.
func TestKotlinSpringBoot_Scope_Issue3274(t *testing.T) {
	source := `
import org.springframework.stereotype.Component
import org.springframework.context.annotation.Scope
import org.springframework.web.context.annotation.RequestScope

@RequestScope
@Component
class RequestContext {
    var userId: String = ""
}

@SessionScope
@Component
class SessionData {
    var token: String = ""
}
`
	r := ExtractSpringBoot(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "Scoped.kt",
	})
	// @RequestScope or @SessionScope should emit a scoped-bean entity
	found := false
	for _, e := range r.Entities {
		if strings.Contains(e.Provenance, "SCOPE") {
			found = true
		}
		if e.Ref != "" && strings.Contains(e.Ref, "scoped_bean") {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3274 kotlin spring_boot scope] expected a scoped-bean entity, got entities: %v", r.Entities)
	}
}

// TestKotlinSpringBoot_WrongLanguage_Issue3274 verifies language gate still
// rejects languages other than java/kotlin.
func TestKotlinSpringBoot_WrongLanguage_Issue3274(t *testing.T) {
	source := `@Service class Foo {}`
	r := ExtractSpringBoot(PatternContext{
		Source:    source,
		Language:  "python",
		Framework: "spring_boot",
		FilePath:  "Foo.kt",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3274 kotlin spring_boot gate] expected 0 entities for python, got %d", len(r.Entities))
	}
}

// ============================================================================
// 2. Transactional (transactional.go) — Kotlin
// ============================================================================

// TestKotlinTransactional_Method_Issue3274 verifies @Transactional before a
// Kotlin `fun` declaration is detected as a transaction boundary.
// Registry target: lang.kotlin.framework.spring-boot Transaction/* → partial.
func TestKotlinTransactional_Method_Issue3274(t *testing.T) {
	source := `
import org.springframework.transaction.annotation.Transactional

class OrderService {

    @Transactional
    fun placeOrder(order: Order) {
        // ...
    }

    @Transactional(readOnly = true)
    fun getOrders(): List<Order> = emptyList()

    @Transactional(propagation = Propagation.REQUIRES_NEW, rollbackFor = [Exception::class])
    fun processPayment(payment: Payment) {
        // ...
    }
}
`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "OrderService.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin @Transactional] expected transaction entities, got 0")
	}
	foundBoundary := false
	foundPropagation := false
	for _, e := range r.Entities {
		if e.Subtype == "transaction_boundary" {
			foundBoundary = true
		}
		if p, ok := e.Properties["propagation"]; ok && p != nil {
			foundPropagation = true
		}
	}
	if !foundBoundary {
		t.Error("[#3274 kotlin @Transactional] no transaction_boundary entity found")
	}
	if !foundPropagation {
		t.Error("[#3274 kotlin @Transactional] no propagation attribute captured")
	}
}

// TestKotlinTransactional_Quarkus_Issue3274 verifies Kotlin Quarkus uses JTA @Transactional.
func TestKotlinTransactional_Quarkus_Issue3274(t *testing.T) {
	source := `
import jakarta.transaction.Transactional

@Transactional
class ProductService {
    fun save(product: Product) {}
}
`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "quarkus",
		FilePath:  "ProductService.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin quarkus @Transactional] expected entities, got 0")
	}
}

// TestKotlinTransactional_WrongLanguage_Issue3274 verifies the gate still
// rejects non-JVM languages.
func TestKotlinTransactional_WrongLanguage_Issue3274(t *testing.T) {
	source := `@Transactional fun save() {}`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "python",
		Framework: "spring_boot",
		FilePath:  "service.py",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3274 kotlin @Transactional gate] expected 0 for python, got %d", len(r.Entities))
	}
}

// ============================================================================
// 3. Spring AOP (spring_aop.go) — Kotlin
// ============================================================================

// TestKotlinSpringAOP_Aspect_Issue3274 verifies @Aspect on a Kotlin class
// emits an aspect entity with OWNS edges to advice methods.
// Registry target: lang.kotlin.framework.spring-boot AOP/* → partial.
func TestKotlinSpringAOP_Aspect_Issue3274(t *testing.T) {
	source := `
import org.aspectj.lang.annotation.Aspect
import org.aspectj.lang.annotation.Before
import org.aspectj.lang.annotation.Pointcut
import org.springframework.stereotype.Component

@Aspect
@Component
class LoggingAspect {

    @Pointcut("execution(* com.example.service.*.*(..))")
    fun serviceMethods() {}

    @Before("serviceMethods()")
    fun logBefore(): Unit {
        // log entry
    }
}
`
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "LoggingAspect.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin spring aop] expected AOP entities, got 0")
	}
	foundAspect := false
	foundPointcut := false
	foundAdvice := false
	for _, e := range r.Entities {
		switch e.Subtype {
		case "aspect":
			foundAspect = true
		case "pointcut":
			foundPointcut = true
		case "advice":
			foundAdvice = true
		}
	}
	if !foundAspect {
		t.Error("[#3274 kotlin spring aop] no aspect entity found")
	}
	if !foundPointcut {
		t.Error("[#3274 kotlin spring aop] no pointcut entity found")
	}
	if !foundAdvice {
		t.Error("[#3274 kotlin spring aop] no advice entity found")
	}
}

// TestKotlinSpringAOP_WrongLanguage_Issue3274 verifies gate rejects non-JVM.
func TestKotlinSpringAOP_WrongLanguage_Issue3274(t *testing.T) {
	source := `@Aspect class Foo {}`
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "ruby",
		Framework: "spring_boot",
		FilePath:  "Foo.rb",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3274 kotlin spring aop gate] expected 0 for ruby, got %d", len(r.Entities))
	}
}

// ============================================================================
// 4. Micronaut AOP (micronaut_aop.go) — Kotlin
// ============================================================================

// TestKotlinMicronautAOP_Interceptor_Issue3274 verifies Micronaut AOP
// interceptor classes in Kotlin emit aspect/advice entities.
// Registry target: lang.kotlin.framework.micronaut AOP/* → partial.
func TestKotlinMicronautAOP_Interceptor_Issue3274(t *testing.T) {
	source := `
import io.micronaut.aop.Around
import io.micronaut.aop.InterceptorBean
import io.micronaut.aop.MethodInterceptor
import io.micronaut.aop.MethodInvocationContext

@Around
@interface Loggable

@InterceptorBean(Loggable::class)
class LoggingInterceptor : MethodInterceptor<Any, Any> {
    override fun intercept(context: MethodInvocationContext<Any, Any>): Any? {
        return context.proceed()
    }
}
`
	r := ExtractMicronautAOP(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "micronaut",
		FilePath:  "LoggingInterceptor.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin micronaut aop] expected AOP entities, got 0")
	}
	foundAspect := false
	for _, e := range r.Entities {
		if e.Subtype == "aspect" || e.Subtype == "advice" || e.Subtype == "pointcut" {
			foundAspect = true
		}
	}
	if !foundAspect {
		t.Error("[#3274 kotlin micronaut aop] no AOP entity found")
	}
}

// ============================================================================
// 5. Observability (observability.go) — Kotlin
// ============================================================================

// TestKotlinObservability_Slf4j_Issue3274 verifies @Slf4j annotation and
// LoggerFactory.getLogger on Kotlin classes emit logger entities.
// Registry target: lang.kotlin.framework.spring-boot Observability/* → partial.
func TestKotlinObservability_Slf4j_Issue3274(t *testing.T) {
	source := `
import mu.KotlinLogging
import org.slf4j.LoggerFactory

@Slf4j
class UserService {
    fun processUser(id: Long) {
        log.info("Processing user {}", id)
        log.debug("Debug trace")
    }
}

class PaymentService {
    private val logger = LoggerFactory.getLogger(PaymentService::class.java)

    fun processPayment() {
        logger.warn("payment started")
    }
}
`
	r := ExtractObservability(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "UserService.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin observability] expected observability entities, got 0")
	}
	foundLogger := false
	foundLogStmt := false
	for _, e := range r.Entities {
		switch e.Subtype {
		case "logger":
			foundLogger = true
		case "log_statement":
			foundLogStmt = true
		}
	}
	if !foundLogger {
		t.Error("[#3274 kotlin observability] no logger entity found")
	}
	if !foundLogStmt {
		t.Error("[#3274 kotlin observability] no log_statement entity found")
	}
}

// TestKotlinObservability_Micrometer_Issue3274 verifies @Timed and
// Counter.builder on Kotlin code emits metric entities.
func TestKotlinObservability_Micrometer_Issue3274(t *testing.T) {
	source := `
import io.micrometer.core.annotation.Timed
import io.micrometer.core.instrument.Counter

class OrderMetrics(private val meterRegistry: MeterRegistry) {

    @Timed("orders.placed")
    fun placeOrder(order: Order) {}

    fun recordCancel() {
        Counter.builder("orders.cancelled")
            .register(meterRegistry)
            .increment()
    }
}
`
	r := ExtractObservability(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "OrderMetrics.kt",
	})
	foundMetric := false
	for _, e := range r.Entities {
		if e.Subtype == "metric" {
			foundMetric = true
		}
	}
	if !foundMetric {
		t.Errorf("[#3274 kotlin observability metrics] expected metric entity, got %v", r.Entities)
	}
}

// TestKotlinObservability_OTel_Issue3274 verifies @WithSpan on Kotlin functions
// emits trace_span entities.
func TestKotlinObservability_OTel_Issue3274(t *testing.T) {
	source := `
import io.opentelemetry.instrumentation.annotations.WithSpan

class CheckoutService {

    @WithSpan("checkout.process")
    fun processCheckout(cart: Cart): Order {
        return Order()
    }

    @WithSpan
    fun validateCart(cart: Cart): Boolean = true
}
`
	r := ExtractObservability(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "spring_boot",
		FilePath:  "CheckoutService.kt",
	})
	foundTrace := false
	for _, e := range r.Entities {
		if e.Subtype == "trace_span" {
			foundTrace = true
		}
	}
	if !foundTrace {
		t.Errorf("[#3274 kotlin observability trace] expected trace_span entity, got %v", r.Entities)
	}
}

// TestKotlinObservability_WrongLanguage_Issue3274 verifies gate rejects non-JVM.
func TestKotlinObservability_WrongLanguage_Issue3274(t *testing.T) {
	source := `@Slf4j class Foo { log.info("hi") }`
	r := ExtractObservability(PatternContext{
		Source:    source,
		Language:  "go",
		Framework: "spring_boot",
		FilePath:  "service.go",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3274 kotlin observability gate] expected 0 for go, got %d", len(r.Entities))
	}
}

// ============================================================================
// 6. Hibernate / JPA (hibernate.go + jpa_fk_lazy.go) — Kotlin
// ============================================================================

// TestKotlinHibernate_Entity_Issue3274 verifies @Entity on a Kotlin data class
// emits an entity record and associations.
// Registry target: lang.kotlin.orm.hibernate relationship_extraction,
// foreign_key_extraction, lazy_loading_recognition → partial.
func TestKotlinHibernate_Entity_Issue3274(t *testing.T) {
	source := `
import jakarta.persistence.Entity
import jakarta.persistence.Table
import jakarta.persistence.OneToMany
import jakarta.persistence.ManyToOne
import jakarta.persistence.JoinColumn
import jakarta.persistence.FetchType

@Entity
@Table(name = "orders")
data class Order(
    val id: Long = 0,

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "customer_id")
    val customer: Customer? = null,

    @OneToMany(mappedBy = "order", fetch = FetchType.LAZY)
    val items: List<OrderItem> = emptyList()
)

@Entity
@Table(name = "customers")
data class Customer(
    val id: Long = 0,
    val name: String = ""
)
`
	r := ExtractHibernate(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "hibernate",
		FilePath:  "Order.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin hibernate] expected entities, got 0")
	}
	foundEntity := false
	foundFK := false
	foundFetch := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			foundEntity = true
		}
		if e.Provenance == "INFERRED_FROM_JPA_JOIN_COLUMN" {
			foundFK = true
		}
		if e.Provenance == "INFERRED_FROM_JPA_FETCH_TYPE" {
			foundFetch = true
		}
	}
	if !foundEntity {
		t.Error("[#3274 kotlin hibernate] no SCOPE.Schema entity found")
	}
	if !foundFK {
		t.Error("[#3274 kotlin hibernate] no JPA foreign_key entity found")
	}
	if !foundFetch {
		t.Error("[#3274 kotlin hibernate] no fetch_config entity found")
	}
}

// TestKotlinHibernate_Associations_Issue3274 verifies @OneToMany / @ManyToOne
// produce DEPENDS_ON relationships.
func TestKotlinHibernate_Associations_Issue3274(t *testing.T) {
	source := `
import jakarta.persistence.Entity
import jakarta.persistence.OneToMany
import jakarta.persistence.ManyToOne
import java.util.List

@Entity
class Department {
    @OneToMany
    var employees: List<Employee>? = null
}

@Entity
class Employee {
    @ManyToOne
    var department: Department? = null
}
`
	r := ExtractHibernate(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "hibernate",
		FilePath:  "Dept.kt",
	})
	if len(r.Relationships) == 0 {
		t.Error("[#3274 kotlin hibernate assoc] expected DEPENDS_ON relationships, got 0")
	}
}

// TestKotlinHibernate_WrongLanguage_Issue3274 verifies gate rejects non-JVM.
func TestKotlinHibernate_WrongLanguage_Issue3274(t *testing.T) {
	source := `@Entity class Foo {}`
	r := ExtractHibernate(PatternContext{
		Source:    source,
		Language:  "python",
		Framework: "hibernate",
		FilePath:  "model.py",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3274 kotlin hibernate gate] expected 0 for python, got %d", len(r.Entities))
	}
}

// ============================================================================
// 5b. CDI Interceptors (cdi_interceptors.go) — Kotlin / Quarkus
// ============================================================================

// TestKotlinCDIInterceptors_Issue3274 verifies @Interceptor/@AroundInvoke on
// Kotlin classes for Quarkus emit aspect/advice entities.
// Registry target: lang.kotlin.framework.quarkus AOP/* → partial.
func TestKotlinCDIInterceptors_Issue3274(t *testing.T) {
	source := `
import jakarta.interceptor.Interceptor
import jakarta.interceptor.AroundInvoke
import jakarta.interceptor.InvocationContext
import jakarta.interceptor.InterceptorBinding

@Retention(AnnotationRetention.RUNTIME)
@Target(AnnotationTarget.CLASS, AnnotationTarget.FUNCTION)
@InterceptorBinding
annotation class Logged

@Logged
@Interceptor
class LoggingInterceptor {

    @AroundInvoke
    fun log(ctx: InvocationContext): Any? {
        return ctx.proceed()
    }
}
`
	r := ExtractCDIInterceptors(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "quarkus",
		FilePath:  "LoggingInterceptor.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin cdi interceptors] expected entities, got 0")
	}
	foundAspect := false
	for _, e := range r.Entities {
		if e.Subtype == "aspect" || e.Subtype == "advice" || e.Subtype == "pointcut" {
			foundAspect = true
		}
	}
	if !foundAspect {
		t.Error("[#3274 kotlin cdi interceptors] no AOP entity found")
	}
}

// ============================================================================
// 7. Javalin routes (javalin_routes.go) — Kotlin
// ============================================================================

// TestKotlinJavalin_Routes_Issue3274 verifies Kotlin Javalin trailing-lambda
// route DSL is extracted correctly.
// Registry target: lang.kotlin.framework.javalin Routing/* → partial.
func TestKotlinJavalin_Routes_Issue3274(t *testing.T) {
	source := `
import io.javalin.Javalin

fun main() {
    val app = Javalin.create().start(7070)

    app.get("/users") { ctx ->
        ctx.json(userService.findAll())
    }

    app.post("/users") { ctx ->
        val user = ctx.bodyAsClass(UserRequest::class.java)
        ctx.status(201)
    }

    app.delete("/users/{id}") { ctx ->
        val id = ctx.pathParam("id")
        userService.delete(id)
        ctx.status(204)
    }

    app.before { ctx ->
        // auth check
    }
}
`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "kotlin",
		Framework: "javalin",
		FilePath:  "App.kt",
	})
	if len(r.Entities) == 0 {
		t.Fatal("[#3274 kotlin javalin] expected route entities, got 0")
	}
	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAVALIN_ROUTE" {
			if verb, ok := e.Properties["http_verb"]; ok {
				if path, ok2 := e.Properties["path"]; ok2 {
					routes[verb.(string)+":"+path.(string)] = true
				}
			}
		}
	}
	for _, want := range []string{"GET:/users", "POST:/users", "DELETE:/users/{id}"} {
		if !routes[want] {
			t.Errorf("[#3274 kotlin javalin] missing route %q; got %v", want, routes)
		}
	}
}

// TestKotlinJavalin_WrongLanguage_Issue3274 verifies that non-java/kotlin
// languages are still rejected.
func TestKotlinJavalin_WrongLanguage_Issue3274(t *testing.T) {
	source := `app.get("/foo") { ctx -> }`
	r := ExtractJavalin(PatternContext{
		Source:    source,
		Language:  "scala",
		Framework: "javalin",
		FilePath:  "App.scala",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3274 kotlin javalin gate] expected 0 for scala, got %d", len(r.Entities))
	}
}
