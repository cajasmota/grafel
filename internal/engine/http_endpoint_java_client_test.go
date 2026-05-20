package engine

import (
	"testing"
)

// TestJavaClient_StdlibHttpClient covers
// `HttpRequest.newBuilder().uri(URI.create("/api/users"))....build()`.
func TestJavaClient_StdlibHttpClient(t *testing.T) {
	src := `
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

public class UsersClient {
    private final HttpClient client = HttpClient.newHttpClient();

    public HttpResponse<String> fetchUsers() throws Exception {
        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create("/api/users"))
            .GET()
            .build();
        return client.send(request, HttpResponse.BodyHandlers.ofString());
    }

    public HttpResponse<String> createUser(String body) throws Exception {
        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create("/api/users"))
            .method("POST", HttpRequest.BodyPublishers.ofString(body))
            .build();
        return client.send(request, HttpResponse.BodyHandlers.ofString());
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "UsersClient.java", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "java-stdlib")
	requireFetches(t, rels, "http:GET:/api/users", "java-stdlib")
	requireFetches(t, rels, "http:POST:/api/users", "java-stdlib")
}

// TestJavaClient_RestTemplate covers Spring RestTemplate verbs.
func TestJavaClient_RestTemplate(t *testing.T) {
	src := `
import org.springframework.web.client.RestTemplate;

public class OrdersClient {
    private final RestTemplate restTemplate = new RestTemplate();

    public Order findOne(long id) {
        return restTemplate.getForObject("/api/orders/1", Order.class);
    }

    public Order create(Order o) {
        return restTemplate.postForObject("/api/orders", o, Order.class);
    }

    public void remove(long id) {
        restTemplate.delete("/api/orders/1");
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "OrdersClient.java", src)
	want := []string{
		"http:GET:/api/orders/1",
		"http:POST:/api/orders",
		"http:DELETE:/api/orders/1",
	}
	requireContains(t, ids, want, "rest-template")
	requireFetches(t, rels, "http:GET:/api/orders/1", "rest-template")
	requireFetches(t, rels, "http:POST:/api/orders", "rest-template")
}

// TestJavaClient_RestTemplateBaseURLComposition covers
// `restTemplate.setRootUri(...)` declarations folded onto subsequent calls.
func TestJavaClient_RestTemplateBaseURLComposition(t *testing.T) {
	src := `
import org.springframework.web.client.RestTemplate;
import org.springframework.boot.web.client.RestTemplateBuilder;

public class ApiClient {
    private final RestTemplate restTemplate;

    public ApiClient(RestTemplateBuilder builder) {
        this.restTemplate = builder.build();
        this.restTemplate.setRootUri("/api/v1");
    }

    public User getUser(long id) {
        return restTemplate.getForObject("/users/1", User.class);
    }
}
`
	ids, _ := runDetectWithRels(t, "java", "ApiClient.java", src)
	want := []string{"http:GET:/api/v1/users/1"}
	requireContains(t, ids, want, "rest-template-base-url")
}

// TestJavaClient_WebClient covers Spring WebClient
// `webClient.get().uri("/path").retrieve()...`.
func TestJavaClient_WebClient(t *testing.T) {
	src := `
import org.springframework.web.reactive.function.client.WebClient;

public class ReactiveClient {
    private final WebClient webClient = WebClient.builder()
        .baseUrl("/api/v2")
        .build();

    public Mono<User> getUser() {
        return webClient.get().uri("/users/1").retrieve().bodyToMono(User.class);
    }

    public Mono<Void> deleteUser() {
        return webClient.delete().uri("/users/1").retrieve().bodyToMono(Void.class);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "ReactiveClient.java", src)
	want := []string{
		"http:GET:/api/v2/users/1",
		"http:DELETE:/api/v2/users/1",
	}
	requireContains(t, ids, want, "web-client")
	requireFetches(t, rels, "http:GET:/api/v2/users/1", "web-client")
}

// TestJavaClient_OkHttp covers OkHttp Request.Builder().url(...).
func TestJavaClient_OkHttp(t *testing.T) {
	src := `
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;

public class OkHttpUsers {
    private final OkHttpClient client = new OkHttpClient();

    public Response fetchUsers() throws Exception {
        Request request = new Request.Builder()
            .url("/api/users")
            .get()
            .build();
        return client.newCall(request).execute();
    }

    public Response postUser(okhttp3.RequestBody body) throws Exception {
        Request request = new Request.Builder()
            .url("/api/users")
            .post(body)
            .build();
        return client.newCall(request).execute();
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "OkHttpUsers.java", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "okhttp")
	requireFetches(t, rels, "http:GET:/api/users", "okhttp")
}

// TestJavaClient_ApacheHttpClient covers the HttpGet/HttpPost/etc forms.
func TestJavaClient_ApacheHttpClient(t *testing.T) {
	src := `
import org.apache.http.client.HttpClient;
import org.apache.http.client.methods.HttpGet;
import org.apache.http.client.methods.HttpPost;

public class ApacheClient {
    private final HttpClient httpclient;

    public ApacheClient(HttpClient hc) { this.httpclient = hc; }

    public void fetchUsers() throws Exception {
        httpclient.execute(new HttpGet("/api/users"));
    }

    public void create() throws Exception {
        httpclient.execute(new HttpPost("/api/users"));
    }
}
`
	ids, _ := runDetectWithRels(t, "java", "ApacheClient.java", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "apache-httpclient")
}

// TestJavaClient_Retrofit covers Retrofit interface annotations including
// per-interface baseUrl composition.
func TestJavaClient_Retrofit(t *testing.T) {
	src := `
import retrofit2.Retrofit;
import retrofit2.Call;
import retrofit2.http.GET;
import retrofit2.http.POST;
import retrofit2.http.DELETE;
import retrofit2.http.Path;

public class RetrofitSetup {
    private final Retrofit retrofit = new Retrofit.Builder()
        .baseUrl("/api/v3")
        .build();

    public interface UsersApi {
        @GET("/users")
        Call<List<User>> listUsers();

        @POST("/users")
        Call<User> createUser(@retrofit2.http.Body User u);

        @DELETE("/users/{id}")
        Call<Void> deleteUser(@Path("id") long id);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "RetrofitSetup.java", src)
	want := []string{
		"http:GET:/api/v3/users",
		"http:POST:/api/v3/users",
		"http:DELETE:/api/v3/users/{id}",
	}
	requireContains(t, ids, want, "retrofit")
	requireFetches(t, rels, "http:GET:/api/v3/users", "retrofit")
}

// ---------------------------------------------------------------------------
// #796 — MicroProfile @RegisterRestClient (Quarkus)
// ---------------------------------------------------------------------------

// TestJavaClient_QuarkusRestClient_BasicGetPost verifies that a
// @RegisterRestClient interface with @GET and @POST method annotations
// produces FETCHES edges when the interface is injected and called.
func TestJavaClient_QuarkusRestClient_BasicGetPost(t *testing.T) {
	src := `
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;
import org.eclipse.microprofile.rest.client.inject.RestClient;
import javax.inject.Inject;
import javax.ws.rs.GET;
import javax.ws.rs.POST;
import javax.ws.rs.Path;
import javax.ws.rs.PathParam;

@RegisterRestClient
@Path("/customers")
public interface CustomerApiClient {
    @GET
    @Path("/{id}")
    Customer getCustomer(@PathParam("id") String id);

    @POST
    Customer create(Customer body);
}

@ApplicationScoped
public class TriageService {
    @Inject @RestClient CustomerApiClient customerApi;

    void process() {
        Customer c = customerApi.getCustomer("abc");
        customerApi.create(new Customer());
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "TriageService.java", src)
	want := []string{
		"http:GET:/customers/{id}",
		"http:POST:/customers",
	}
	requireContains(t, ids, want, "quarkus-rest-client-basic")
	requireFetches(t, rels, "http:GET:/customers/{id}", "quarkus-rest-client-basic")
	requireFetches(t, rels, "http:POST:/customers", "quarkus-rest-client-basic")
}

// TestJavaClient_QuarkusRestClient_OfferApi verifies extraction for a
// second client type (OfferApiClient) in the same file — simulates the
// fixture-f ai-triage service pattern.
func TestJavaClient_QuarkusRestClient_OfferApi(t *testing.T) {
	src := `
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;
import org.eclipse.microprofile.rest.client.inject.RestClient;
import javax.inject.Inject;
import javax.ws.rs.GET;
import javax.ws.rs.POST;
import javax.ws.rs.DELETE;
import javax.ws.rs.Path;

@RegisterRestClient
@Path("/offers")
public interface OfferApiClient {
    @GET
    @Path("/active")
    List<Offer> getActiveOffers();

    @POST
    Offer createOffer(Offer o);

    @DELETE
    @Path("/{id}")
    void deleteOffer(@PathParam("id") long id);
}

@ApplicationScoped
public class OfferService {
    @Inject @RestClient OfferApiClient offerApi;

    void processOffers() {
        List<Offer> active = offerApi.getActiveOffers();
        Offer created = offerApi.createOffer(new Offer());
        offerApi.deleteOffer(42L);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "OfferService.java", src)
	want := []string{
		"http:GET:/offers/active",
		"http:POST:/offers",
		"http:DELETE:/offers/{id}",
	}
	requireContains(t, ids, want, "quarkus-offer-api")
	requireFetches(t, rels, "http:GET:/offers/active", "quarkus-offer-api")
	requireFetches(t, rels, "http:POST:/offers", "quarkus-offer-api")
	requireFetches(t, rels, "http:DELETE:/offers/{id}", "quarkus-offer-api")
}

// TestJavaClient_QuarkusRestClient_RestClientAnnotationOrderReversed
// verifies that @RestClient @Inject (reversed order) is also detected.
func TestJavaClient_QuarkusRestClient_RestClientAnnotationOrderReversed(t *testing.T) {
	src := `
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;
import org.eclipse.microprofile.rest.client.inject.RestClient;
import javax.inject.Inject;
import javax.ws.rs.PUT;
import javax.ws.rs.Path;

@RegisterRestClient
@Path("/inventory")
public interface InventoryClient {
    @PUT
    @Path("/{sku}")
    void updateStock(@PathParam("sku") String sku, int qty);
}

@ApplicationScoped
public class WarehouseService {
    @RestClient @Inject InventoryClient inventoryClient;

    void restock(String sku, int qty) {
        inventoryClient.updateStock(sku, qty);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "WarehouseService.java", src)
	requireContains(t, ids, []string{"http:PUT:/inventory/{sku}"}, "quarkus-reversed-inject")
	requireFetches(t, rels, "http:PUT:/inventory/{sku}", "quarkus-reversed-inject")
}

// TestJavaClient_QuarkusRestClient_FullyQualifiedAnnotation verifies that
// the fully-qualified @RegisterRestClient annotation is detected.
func TestJavaClient_QuarkusRestClient_FullyQualifiedAnnotation(t *testing.T) {
	src := `
@org.eclipse.microprofile.rest.client.inject.RegisterRestClient(baseUri = "http://payment-service")
@javax.ws.rs.Path("/payments")
public interface PaymentClient {
    @javax.ws.rs.GET
    @javax.ws.rs.Path("/{txId}")
    Payment getPayment(@javax.ws.rs.PathParam("txId") String txId);

    @javax.ws.rs.POST
    Payment initiate(Payment p);
}

public class CheckoutService {
    @javax.inject.Inject
    @org.eclipse.microprofile.rest.client.inject.RestClient
    PaymentClient paymentClient;

    void checkout() {
        Payment p = paymentClient.getPayment("tx-123");
        paymentClient.initiate(p);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "CheckoutService.java", src)
	want := []string{
		"http:GET:/payments/{txId}",
		"http:POST:/payments",
	}
	requireContains(t, ids, want, "quarkus-fqn-annotation")
	requireFetches(t, rels, "http:GET:/payments/{txId}", "quarkus-fqn-annotation")
}

// TestJavaClient_QuarkusRestClient_MultipleClients verifies that two
// different @RegisterRestClient interfaces injected in the same consuming
// class both produce correct FETCHES edges (fixture-f multi-client scenario).
func TestJavaClient_QuarkusRestClient_MultipleClients(t *testing.T) {
	src := `
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;
import org.eclipse.microprofile.rest.client.inject.RestClient;
import javax.inject.Inject;
import javax.ws.rs.GET;
import javax.ws.rs.POST;
import javax.ws.rs.Path;

@RegisterRestClient
@Path("/users")
interface UserServiceClient {
    @GET
    @Path("/{id}")
    User getUser(@PathParam("id") String id);
}

@RegisterRestClient
@Path("/orders")
interface OrderServiceClient {
    @POST
    Order placeOrder(Order o);
}

@ApplicationScoped
public class AggregatorService {
    @Inject @RestClient UserServiceClient userClient;
    @Inject @RestClient OrderServiceClient orderClient;

    void handleRequest(String userId) {
        User u = userClient.getUser(userId);
        orderClient.placeOrder(new Order());
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "AggregatorService.java", src)
	want := []string{
		"http:GET:/users/{id}",
		"http:POST:/orders",
	}
	requireContains(t, ids, want, "quarkus-multi-client")
	requireFetches(t, rels, "http:GET:/users/{id}", "quarkus-multi-client")
	requireFetches(t, rels, "http:POST:/orders", "quarkus-multi-client")
}

// TestJavaClient_QuarkusRestClient_PatchHead verifies PATCH and HEAD verb
// detection on @RegisterRestClient interfaces.
func TestJavaClient_QuarkusRestClient_PatchHead(t *testing.T) {
	src := `
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;
import org.eclipse.microprofile.rest.client.inject.RestClient;
import javax.inject.Inject;
import javax.ws.rs.PATCH;
import javax.ws.rs.HEAD;
import javax.ws.rs.Path;

@RegisterRestClient
@Path("/profiles")
interface ProfileClient {
    @PATCH
    @Path("/{id}/bio")
    void updateBio(@PathParam("id") String id, String bio);

    @HEAD
    @Path("/{id}")
    void checkExists(@PathParam("id") String id);
}

@ApplicationScoped
class ProfileManager {
    @Inject @RestClient ProfileClient profileClient;

    void run() {
        profileClient.updateBio("u1", "hello");
        profileClient.checkExists("u2");
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "ProfileManager.java", src)
	want := []string{
		"http:PATCH:/profiles/{id}/bio",
		"http:HEAD:/profiles/{id}",
	}
	requireContains(t, ids, want, "quarkus-patch-head")
	requireFetches(t, rels, "http:PATCH:/profiles/{id}/bio", "quarkus-patch-head")
}

// TestJavaClient_QuarkusRestClient_NoInjectionNoEdge verifies that a
// @RegisterRestClient interface that is never @Inject @RestClient'd in the
// same file does not produce spurious FETCHES edges.
func TestJavaClient_QuarkusRestClient_NoInjectionNoEdge(t *testing.T) {
	src := `
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;
import javax.ws.rs.GET;
import javax.ws.rs.Path;

@RegisterRestClient
@Path("/things")
public interface ThingsClient {
    @GET
    List<Thing> list();
}
// No consumer class — no @Inject @RestClient field, no call sites.
`
	_, rels := runDetectWithRels(t, "java", "ThingsClient.java", src)
	for _, r := range rels {
		if r.Kind == "FETCHES" {
			t.Errorf("unexpected FETCHES edge %+v for interface-only file", r)
		}
	}
}

// ---------------------------------------------------------------------------
// #796 — Spring Cloud OpenFeign (@FeignClient) — beyond-minimum
// ---------------------------------------------------------------------------

// TestJavaClient_FeignClient_BasicGetMapping verifies that a @FeignClient
// interface with @GetMapping / @PostMapping produces FETCHES edges.
func TestJavaClient_FeignClient_BasicGetMapping(t *testing.T) {
	src := `
import org.springframework.cloud.openfeign.FeignClient;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.beans.factory.annotation.Autowired;

@FeignClient(name = "customer-service", url = "http://customer-svc")
public interface CustomerClient {
    @GetMapping("/customers/{id}")
    Customer getCustomer(@PathVariable String id);

    @PostMapping("/customers")
    Customer createCustomer(Customer c);
}

@Service
public class OrderHandler {
    @Autowired
    CustomerClient customerClient;

    void handle(String cid) {
        Customer c = customerClient.getCustomer(cid);
        customerClient.createCustomer(c);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "OrderHandler.java", src)
	want := []string{
		"http:GET:/customers/{id}",
		"http:POST:/customers",
	}
	requireContains(t, ids, want, "feign-basic")
	requireFetches(t, rels, "http:GET:/customers/{id}", "feign-basic")
	requireFetches(t, rels, "http:POST:/customers", "feign-basic")
}
