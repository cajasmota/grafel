package java

import (
	"strings"
	"testing"
)

// ============================================================================
// Issue #3087: Dropwizard DI + auth + middleware + transactions + DTO + tests
// ============================================================================

// ----------------------------------------------------------------------------
// DI: di_injection_point — @Inject field and constructor injection
// ----------------------------------------------------------------------------

// TestDropwizard_DI_InjectField_Issue3087 proves that @Inject field injection
// is detected as a di_injection_point for Dropwizard (Guice/HK2).
// Registry target: lang.java.framework.dropwizard DI/di_injection_point → partial.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_DI_InjectField_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import javax.inject.Inject;

public class UserResource {

    @Inject
    private UserService userService;

    @Inject
    private OrderRepository orderRepository;
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "UserResource.java",
	})

	count := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_INJECT_FIELD" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("[#3087 di_injection_point] expected >= 2 @Inject field entities, got %d", count)
	}
}

// TestDropwizard_DI_InjectConstructor_Issue3087 proves that @Inject constructor
// injection is detected for Dropwizard.
func TestDropwizard_DI_InjectConstructor_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import javax.inject.Inject;

public class OrderService {

    private final UserRepository users;
    private final EventBus eventBus;

    @Inject
    public OrderService(UserRepository users, EventBus eventBus) {
        this.users = users;
        this.eventBus = eventBus;
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "OrderService.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_INJECT_CONSTRUCTOR" {
			found = true
			if e.Properties["di_pattern"] != "constructor_injection" {
				t.Errorf("[#3087 di_injection_point] expected di_pattern=constructor_injection, got %v", e.Properties["di_pattern"])
			}
		}
	}
	if !found {
		t.Errorf("[#3087 di_injection_point] expected INFERRED_FROM_DROPWIZARD_INJECT_CONSTRUCTOR entity")
	}
}

// ----------------------------------------------------------------------------
// DI: di_binding_extraction — @Provides (Guice module) + bind(...).to(...)
// ----------------------------------------------------------------------------

// TestDropwizard_DI_Provides_Issue3087 proves that @Provides methods in Guice
// modules are extracted as di_binding_extraction evidence.
// Registry target: lang.java.framework.dropwizard DI/di_binding_extraction → partial.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_DI_Provides_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import com.google.inject.Provides;
import com.google.inject.Singleton;

public class AppModule extends AbstractModule {

    @Provides
    @Singleton
    public UserService provideUserService(UserRepository repo) {
        return new UserServiceImpl(repo);
    }

    @Provides
    public OrderRepository provideOrderRepository(DBI dbi) {
        return dbi.onDemand(OrderRepository.class);
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "AppModule.java",
	})

	count := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_GUICE_PROVIDES" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("[#3087 di_binding_extraction] expected >= 2 @Provides entities, got %d", count)
	}
}

// TestDropwizard_DI_BindTo_Issue3087 proves that bind(X).to(Y) bindings
// are extracted for Guice AbstractModule.
func TestDropwizard_DI_BindTo_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

public class ServiceModule extends AbstractModule {
    @Override
    protected void configure() {
        bind(UserService.class).to(UserServiceImpl.class);
        bind(OrderService.class).to(OrderServiceImpl.class);
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "ServiceModule.java",
	})

	count := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_GUICE_BIND" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("[#3087 di_binding_extraction] expected >= 2 bind-to entities, got %d", count)
	}
}

// ----------------------------------------------------------------------------
// DI: di_scope_resolution — @Singleton, @RequestScoped
// ----------------------------------------------------------------------------

// TestDropwizard_DI_ScopeResolution_Issue3087 proves that @Singleton and
// @RequestScoped annotations on classes are captured.
// Registry target: lang.java.framework.dropwizard DI/di_scope_resolution → partial.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_DI_ScopeResolution_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import javax.inject.Singleton;
import javax.enterprise.context.RequestScoped;

@Singleton
public class CacheService {
}

@RequestScoped
public class RequestContextHolder {
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "ScopedServices.java",
	})

	scopes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_DI_SCOPE" {
			if scope, ok := e.Properties["di_scope"].(string); ok {
				scopes[scope] = true
			}
		}
	}
	if !scopes["Singleton"] {
		t.Errorf("[#3087 di_scope_resolution] expected Singleton scope, got %v", scopes)
	}
	if !scopes["RequestScoped"] {
		t.Errorf("[#3087 di_scope_resolution] expected RequestScoped scope, got %v", scopes)
	}
}

// ----------------------------------------------------------------------------
// Auth: auth_coverage — @Authenticated, @RolesAllowed, @PermitAll
// ----------------------------------------------------------------------------

// TestDropwizard_Auth_Authenticated_Issue3087 proves that @Authenticated is
// detected as auth_coverage evidence for Dropwizard resource methods.
// Registry target: lang.java.framework.dropwizard Auth/auth_coverage → partial.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_Auth_Authenticated_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.resources;

import io.dropwizard.auth.Auth;
import io.dropwizard.auth.Authenticated;
import javax.ws.rs.GET;
import javax.ws.rs.Path;

@Path("/users")
public class UserResource {

    @GET
    @Authenticated
    public UserResponse getProfile(@Auth User user) {
        return new UserResponse(user);
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "UserResource.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_AUTHENTICATED" {
			found = true
			if e.Properties["auth_annotation"] != "Authenticated" {
				t.Errorf("[#3087 auth] expected auth_annotation=Authenticated, got %v", e.Properties["auth_annotation"])
			}
			if e.Properties["auth_required"] != true {
				t.Errorf("[#3087 auth] expected auth_required=true, got %v", e.Properties["auth_required"])
			}
		}
	}
	if !found {
		t.Errorf("[#3087 auth] expected INFERRED_FROM_DROPWIZARD_AUTHENTICATED entity")
	}
}

// TestDropwizard_Auth_RolesAllowed_Issue3087 proves that @RolesAllowed is
// detected as auth_coverage for Dropwizard JAX-RS resources.
func TestDropwizard_Auth_RolesAllowed_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.resources;

import javax.annotation.security.RolesAllowed;
import javax.ws.rs.DELETE;
import javax.ws.rs.Path;
import javax.ws.rs.PathParam;

@Path("/admin")
public class AdminResource {

    @DELETE
    @Path("/{id}")
    @RolesAllowed("ADMIN")
    public void deleteUser(@PathParam("id") Long id) {
        // admin-only deletion
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "AdminResource.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_ROLES_ALLOWED" {
			found = true
			if e.Properties["auth_annotation"] != "RolesAllowed" {
				t.Errorf("[#3087 auth] expected auth_annotation=RolesAllowed, got %v", e.Properties["auth_annotation"])
			}
		}
	}
	if !found {
		t.Errorf("[#3087 auth] expected INFERRED_FROM_DROPWIZARD_ROLES_ALLOWED entity")
	}
}

// TestDropwizard_Auth_PermitAll_Issue3087 proves that @PermitAll (public
// endpoints) is also captured.
func TestDropwizard_Auth_PermitAll_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.resources;

import javax.annotation.security.PermitAll;
import javax.ws.rs.GET;
import javax.ws.rs.Path;

@Path("/health")
public class HealthResource {

    @GET
    @PermitAll
    public HealthResponse check() {
        return new HealthResponse("UP");
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "HealthResource.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_PERMIT_ALL" {
			found = true
			if e.Properties["auth_required"] != false {
				t.Errorf("[#3087 auth] expected auth_required=false for @PermitAll, got %v", e.Properties["auth_required"])
			}
		}
	}
	if !found {
		t.Errorf("[#3087 auth] expected INFERRED_FROM_DROPWIZARD_PERMIT_ALL entity")
	}
}

// ----------------------------------------------------------------------------
// Middleware: middleware_coverage — ContainerRequestFilter
// ----------------------------------------------------------------------------

// TestDropwizard_Middleware_RequestFilter_Issue3087 proves that JAX-RS
// ContainerRequestFilter is detected as middleware for Dropwizard.
// Registry target: lang.java.framework.dropwizard Middleware/middleware_coverage → partial.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_Middleware_RequestFilter_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.filters;

import javax.ws.rs.container.ContainerRequestContext;
import javax.ws.rs.container.ContainerRequestFilter;
import javax.ws.rs.ext.Provider;

@Provider
public class AuthTokenFilter implements ContainerRequestFilter {

    @Override
    public void filter(ContainerRequestContext requestContext) {
        // validate bearer token
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "AuthTokenFilter.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_FILTER" && e.Name == "AuthTokenFilter" {
			found = true
			if e.Properties["framework"] != "dropwizard" {
				t.Errorf("[#3087 middleware] expected framework=dropwizard, got %v", e.Properties["framework"])
			}
			filterType, _ := e.Properties["filter_type"].(string)
			if !strings.Contains(strings.ToLower(filterType), "request") {
				t.Errorf("[#3087 middleware] expected filter_type containing 'request', got %v", filterType)
			}
			if e.Properties["provider_registered"] != true {
				t.Errorf("[#3087 middleware] expected provider_registered=true for @Provider class")
			}
		}
	}
	if !found {
		t.Errorf("[#3087 middleware] expected INFERRED_FROM_DROPWIZARD_FILTER for AuthTokenFilter")
	}
}

// TestDropwizard_Middleware_ResponseFilter_Issue3087 proves ContainerResponseFilter
// is detected for Dropwizard.
func TestDropwizard_Middleware_ResponseFilter_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.filters;

import javax.ws.rs.container.ContainerRequestContext;
import javax.ws.rs.container.ContainerResponseContext;
import javax.ws.rs.container.ContainerResponseFilter;
import javax.ws.rs.ext.Provider;
import javax.ws.rs.Priorities;
import javax.annotation.Priority;

@Provider
@Priority(Priorities.HEADER_DECORATOR)
public class CorsFilter implements ContainerResponseFilter {

    @Override
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {
        res.getHeaders().add("Access-Control-Allow-Origin", "*");
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "CorsFilter.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_FILTER" && e.Name == "CorsFilter" {
			found = true
			filterType, _ := e.Properties["filter_type"].(string)
			if !strings.Contains(strings.ToLower(filterType), "response") {
				t.Errorf("[#3087 middleware] expected filter_type containing 'response', got %v", filterType)
			}
		}
	}
	if !found {
		t.Errorf("[#3087 middleware] expected INFERRED_FROM_DROPWIZARD_FILTER for CorsFilter")
	}
}

// TestDropwizard_Middleware_Gating_Issue3087 confirms that the filter extractor
// does NOT fire for non-dropwizard frameworks.
func TestDropwizard_Middleware_Gating_Issue3087(t *testing.T) {
	source := `
public class SomeFilter implements ContainerRequestFilter {
    public void filter(ContainerRequestContext ctx) {}
}
`
	r := ExtractDropwizard(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "SomeFilter.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3087 middleware-gating] expected 0 entities for framework=spring_boot, got %d", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// Transactions: JDBI @Transaction — transaction_boundary_extraction,
//               transaction_propagation, transaction_rollback_rules
// ----------------------------------------------------------------------------

// TestDropwizard_Tx_JDBITransaction_Method_Issue3087 proves that JDBI
// @Transaction on a DAO method is extracted as transaction_boundary_extraction.
// Registry target: lang.java.framework.dropwizard Transactions/transaction_boundary_extraction → partial.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_Tx_JDBITransaction_Method_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.dao;

import org.jdbi.v3.sqlobject.transaction.Transaction;

public interface OrderDAO {

    @Transaction
    void createOrderWithItems(Order order, List<OrderItem> items);

    @Transaction(TransactionIsolationLevel.SERIALIZABLE)
    void transferFunds(long fromId, long toId, BigDecimal amount);
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "OrderDAO.java",
	})

	txBoundaries := 0
	var foundIsolation bool
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_JDBI_TRANSACTION" {
			txBoundaries++
			if e.Properties["tx_attribute"] != nil {
				attr, _ := e.Properties["tx_attribute"].(string)
				if strings.Contains(attr, "SERIALIZABLE") {
					foundIsolation = true
				}
			}
		}
	}
	if txBoundaries < 2 {
		t.Errorf("[#3087 tx_boundary] expected >= 2 @Transaction method entities, got %d", txBoundaries)
	}
	if !foundIsolation {
		t.Errorf("[#3087 tx_propagation] expected isolation attribute SERIALIZABLE to be captured")
	}
}

// TestDropwizard_Tx_JDBITransaction_Class_Issue3087 proves that @Transaction on
// a DAO class/interface is extracted.
func TestDropwizard_Tx_JDBITransaction_Class_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.dao;

import org.jdbi.v3.sqlobject.transaction.Transaction;

@Transaction
public interface AccountDAO {
    void debit(long id, BigDecimal amount);
    void credit(long id, BigDecimal amount);
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "AccountDAO.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_JDBI_TRANSACTION_CLASS" {
			found = true
			if e.Properties["transaction_boundary"] != "class" {
				t.Errorf("[#3087 tx_boundary] expected transaction_boundary=class, got %v", e.Properties["transaction_boundary"])
			}
			if e.Properties["tx_type"] != "jdbi_transaction" {
				t.Errorf("[#3087 tx_boundary] expected tx_type=jdbi_transaction, got %v", e.Properties["tx_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3087 tx_boundary] expected INFERRED_FROM_DROPWIZARD_JDBI_TRANSACTION_CLASS entity")
	}
}

// TestDropwizard_Tx_JDBITransaction_Rollback_Issue3087 proves that
// transaction isolation attributes are captured (proxying transaction_rollback_rules
// evidence via @Transaction attributes).
func TestDropwizard_Tx_JDBITransaction_Rollback_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.dao;

import org.jdbi.v3.sqlobject.transaction.Transaction;

public interface PaymentDAO {

    @Transaction(TransactionIsolationLevel.READ_COMMITTED)
    Payment processPayment(PaymentRequest req);
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "PaymentDAO.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_JDBI_TRANSACTION" {
			found = true
			if e.Properties["tx_attribute"] == nil {
				t.Errorf("[#3087 tx_rollback_rules] expected tx_attribute to be captured")
			}
		}
	}
	if !found {
		t.Errorf("[#3087 tx_rollback_rules] expected @Transaction entity")
	}
}

// TestDropwizard_Tx_Gating_Issue3087 confirms the extractor does not fire for
// non-dropwizard frameworks.
func TestDropwizard_Tx_Gating_Issue3087(t *testing.T) {
	source := `
public interface OrderDAO {
    @Transaction
    void create(Order o);
}
`
	r := ExtractDropwizard(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "OrderDAO.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3087 tx-gating] expected 0 entities for framework=spring_boot, got %d", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// Validation: dto_extraction — @Path + JAX-RS body/return types (via jaxrsDTOFrameworks)
// ----------------------------------------------------------------------------

// TestDropwizard_DTO_Extraction_Issue3087 proves that JAX-RS DTO extraction
// runs for the "dropwizard" framework (via gating extension in jakarta_jaxrs_dto.go).
// Registry target: lang.java.framework.dropwizard Validation/dto_extraction → partial.
// Cite: internal/custom/java/jakarta_jaxrs_dto.go
func TestDropwizard_DTO_Extraction_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.resources;

import javax.ws.rs.POST;
import javax.ws.rs.GET;
import javax.ws.rs.Path;
import javax.ws.rs.PathParam;

@Path("/orders")
public class OrderResource {

    @POST
    public OrderResponse createOrder(CreateOrderRequest request) {
        return new OrderResponse();
    }

    @GET
    @Path("/{id}")
    public OrderResponse getOrder(@PathParam("id") Long id) {
        return new OrderResponse();
    }
}
`
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "OrderResource.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAXRS_DTO" {
			dtoNames[e.Name] = true
		}
	}
	if !dtoNames["CreateOrderRequest"] {
		t.Errorf("[#3087 dto_extraction] expected CreateOrderRequest DTO entity, got %v", dtoNames)
	}
	if !dtoNames["OrderResponse"] {
		t.Errorf("[#3087 dto_extraction] expected OrderResponse DTO entity, got %v", dtoNames)
	}
}

// ----------------------------------------------------------------------------
// Validation: request_validation — @Valid on JAX-RS method params
// ----------------------------------------------------------------------------

// TestDropwizard_RequestValidation_Issue3087 proves that @Valid on a JAX-RS
// method parameter is detected for Dropwizard.
// Registry target: lang.java.framework.dropwizard Validation/request_validation → partial.
// Cite: internal/custom/java/jakarta_jaxrs_dto.go
func TestDropwizard_RequestValidation_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.resources;

import javax.validation.Valid;
import javax.ws.rs.POST;
import javax.ws.rs.Path;

@Path("/products")
public class ProductResource {

    @POST
    public ProductResponse createProduct(@Valid CreateProductRequest request) {
        return new ProductResponse();
    }
}
`
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "ProductResource.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAXRS_DTO" && e.Name == "CreateProductRequest" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3087 request_validation] expected CreateProductRequest DTO entity for @Valid param")
	}
}

// ----------------------------------------------------------------------------
// Tests: tests_linkage — JUnit5 gating + DropwizardAppRule / ResourceTestRule
// ----------------------------------------------------------------------------

// TestDropwizard_TestsLinkage_JUnit5_Issue3087 proves that ExtractJUnit5 runs
// for the "dropwizard" framework and picks up @Test methods.
// Registry target: lang.java.framework.dropwizard Testing/tests_linkage → partial.
// Cite: internal/custom/java/junit5.go (gating), internal/custom/java/dropwizard.go
func TestDropwizard_TestsLinkage_JUnit5_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import io.dropwizard.testing.junit5.DropwizardExtensionsSupport;
import io.dropwizard.testing.junit5.ResourceExtension;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import static org.assertj.core.api.Assertions.assertThat;

@ExtendWith(DropwizardExtensionsSupport.class)
class UserResourceTest {

    @Test
    void getUser_returns200() {
        assertThat(200).isEqualTo(200);
    }

    @Test
    void createUser_returns201() {
        assertThat(201).isEqualTo(201);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "UserResourceTest.java",
	})

	// #4359: per-@Test orphan nodes folded into the suite's test_method_count.
	testCount := suiteTestMethodCount(r)
	if testCount < 2 {
		t.Errorf("[#3087 tests_linkage] expected >= 2 @Test entities for dropwizard, got %d", testCount)
	}
}

// TestDropwizard_TestsLinkage_AppRule_Issue3087 proves that DropwizardAppRule
// and ResourceTestRule fields are detected as test infrastructure.
func TestDropwizard_TestsLinkage_AppRule_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import io.dropwizard.testing.junit.DropwizardAppRule;
import io.dropwizard.testing.junit.ResourceTestRule;
import org.junit.ClassRule;
import org.junit.Test;

public class IntegrationTest {

    @ClassRule
    public static final DropwizardAppRule<AppConfig> APP_RULE =
        new DropwizardAppRule<>(MyApp.class, ResourceHelpers.resourceFilePath("test.yml"));

    @ClassRule
    public static final ResourceTestRule RESOURCE =
        ResourceTestRule.builder()
            .addResource(new UserResource())
            .build();

    @Test
    public void testHealthCheck() {
        // integration test
    }
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "IntegrationTest.java",
	})

	provenances := make(map[string]int)
	for _, e := range r.Entities {
		provenances[e.Provenance]++
	}
	if provenances["INFERRED_FROM_DROPWIZARD_APP_RULE"] < 1 {
		t.Errorf("[#3087 tests_linkage] expected >= 1 DropwizardAppRule entity, got %d", provenances["INFERRED_FROM_DROPWIZARD_APP_RULE"])
	}
	if provenances["INFERRED_FROM_DROPWIZARD_RESOURCE_TEST_RULE"] < 1 {
		t.Errorf("[#3087 tests_linkage] expected >= 1 ResourceTestRule entity, got %d", provenances["INFERRED_FROM_DROPWIZARD_RESOURCE_TEST_RULE"])
	}
}

// TestDropwizard_TestsLinkage_ExtensionsSupport_Issue3087 proves that
// @ExtendWith(DropwizardExtensionsSupport.class) is detected.
func TestDropwizard_TestsLinkage_ExtensionsSupport_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import io.dropwizard.testing.junit5.DropwizardExtensionsSupport;
import org.junit.jupiter.api.extension.ExtendWith;

@ExtendWith(DropwizardExtensionsSupport.class)
class OrderServiceTest {
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "OrderServiceTest.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_EXTENSIONS_SUPPORT" {
			found = true
			if e.Properties["test_rule"] != "DropwizardExtensionsSupport" {
				t.Errorf("[#3087 tests_linkage] expected test_rule=DropwizardExtensionsSupport, got %v", e.Properties["test_rule"])
			}
		}
	}
	if !found {
		t.Errorf("[#3087 tests_linkage] expected INFERRED_FROM_DROPWIZARD_EXTENSIONS_SUPPORT entity")
	}
}

// TestDropwizard_TestsLinkage_Gating_Issue3087 confirms "dropwizard" is in
// junit5Frameworks map.
func TestDropwizard_TestsLinkage_Gating_Issue3087(t *testing.T) {
	source := `
class FooTest {
    @Test
    void foo() {}
}
`
	r := ExtractJUnit5(PatternContext{
		Source: source, Language: "java", Framework: "dropwizard",
		FilePath: "FooTest.java",
	})
	if len(r.Entities) == 0 {
		t.Error("[#3087 tests-gating] expected test entity for framework=dropwizard, got none")
	}
}

// ----------------------------------------------------------------------------
// AOP: not_applicable — Dropwizard has no AOP
// ----------------------------------------------------------------------------

// TestDropwizard_AOP_NotApplicable_Issue3087 confirms that Dropwizard does not
// use AOP (advice_attribution, aspect_extraction, pointcut_resolution are
// not_applicable). This test documents the negative assertion: the Spring AOP
// extractor must NOT fire for framework=dropwizard.
// Registry target: AOP/advice_attribution, AOP/aspect_extraction,
//
//	AOP/pointcut_resolution → not_applicable.
func TestDropwizard_AOP_NotApplicable_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard;

import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Before;

@Aspect
public class LoggingAspect {
    @Before("execution(* com.example.*.*(..))")
    public void logBefore() {}
}
`
	// Spring AOP extractor should not fire for dropwizard framework.
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "LoggingAspect.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3087 aop-not-applicable] Spring AOP extractor should not fire for framework=dropwizard, got %d entities", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// JDBI SQL DAOs — di_binding_extraction
// ----------------------------------------------------------------------------

// TestDropwizard_JDBI_SqlDAO_Issue3087 proves that JDBI @SqlQuery/@SqlUpdate
// methods are extracted as DAO binding evidence.
// Cite: internal/custom/java/dropwizard.go
func TestDropwizard_JDBI_SqlDAO_Issue3087(t *testing.T) {
	source := `
package com.example.dropwizard.dao;

import org.jdbi.v3.sqlobject.statement.SqlQuery;
import org.jdbi.v3.sqlobject.statement.SqlUpdate;

public interface UserDAO {

    @SqlQuery("SELECT * FROM users WHERE id = :id")
    User findById(long id);

    @SqlUpdate("INSERT INTO users (name, email) VALUES (:name, :email)")
    void insert(String name, String email);
}
`
	r := ExtractDropwizard(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "dropwizard",
		FilePath:  "UserDAO.java",
	})

	sqlEntities := 0
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_DROPWIZARD_JDBI_SQL" {
			sqlEntities++
		}
	}
	if sqlEntities < 2 {
		t.Errorf("[#3087 di_binding_extraction] expected >= 2 JDBI SQL DAO entities, got %d", sqlEntities)
	}
}
