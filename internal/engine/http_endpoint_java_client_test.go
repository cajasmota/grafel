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
