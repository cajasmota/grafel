package engine

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// HttpClient async verbs
// ---------------------------------------------------------------------------

// TestCsClient_HttpClientGetAsync covers `await client.GetAsync(url)` and
// `await client.GetStringAsync(url)`.
func TestCsClient_HttpClientGetAsync(t *testing.T) {
	src := `
using System.Net.Http;

public class UserService
{
    private readonly HttpClient _client;

    public async Task<string> GetUsersAsync()
    {
        var response = await _client.GetAsync("https://api.example.com/api/users");
        return await response.Content.ReadAsStringAsync();
    }

    public async Task<string> GetUserStringAsync()
    {
        return await _client.GetStringAsync("/api/users/profile");
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "UserService.cs", src)
	want := []string{
		"http:GET:/api/users",
		"http:GET:/api/users/profile",
	}
	requireContains(t, ids, want, "cs-httpclient-get-async")
	requireFetches(t, rels, "http:GET:/api/users", "cs-httpclient-get-async")
	requireFetches(t, rels, "http:GET:/api/users/profile", "cs-httpclient-get-async")
}

// TestCsClient_HttpClientPostAsync covers `await client.PostAsync(url, content)`.
func TestCsClient_HttpClientPostAsync(t *testing.T) {
	src := `
using System.Net.Http;

public class OrderService
{
    private readonly HttpClient _client;

    public async Task<HttpResponseMessage> CreateOrderAsync(StringContent body)
    {
        return await _client.PostAsync("/api/orders", body);
    }

    public async Task UpdateOrderAsync(StringContent body)
    {
        await _client.PutAsync("/api/orders/1", body);
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "OrderService.cs", src)
	want := []string{
		"http:POST:/api/orders",
		"http:PUT:/api/orders/1",
	}
	requireContains(t, ids, want, "cs-httpclient-post-async")
	requireFetches(t, rels, "http:POST:/api/orders", "cs-httpclient-post-async")
	requireFetches(t, rels, "http:PUT:/api/orders/1", "cs-httpclient-post-async")
}

// TestCsClient_HttpClientDeletePatchAsync covers DELETE and PATCH async verbs.
func TestCsClient_HttpClientDeletePatchAsync(t *testing.T) {
	src := `
using System.Net.Http;

public class ProductService
{
    private readonly HttpClient _client;

    public async Task DeleteProductAsync(int id)
    {
        await _client.DeleteAsync("/api/products/42");
    }

    public async Task PatchProductAsync(StringContent patch)
    {
        await _client.PatchAsync("/api/products/42", patch);
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "ProductService.cs", src)
	want := []string{
		"http:DELETE:/api/products/42",
		"http:PATCH:/api/products/42",
	}
	requireContains(t, ids, want, "cs-httpclient-delete-patch-async")
	requireFetches(t, rels, "http:DELETE:/api/products/42", "cs-httpclient-delete-patch-async")
	requireFetches(t, rels, "http:PATCH:/api/products/42", "cs-httpclient-delete-patch-async")
}

// TestCsClient_HttpRequestMessage covers `new HttpRequestMessage(HttpMethod.Post, url)`.
func TestCsClient_HttpRequestMessage(t *testing.T) {
	src := `
using System.Net.Http;

public class PaymentService
{
    private readonly HttpClient _client;

    public async Task<HttpResponseMessage> ChargeAsync(StringContent body)
    {
        var request = new HttpRequestMessage(HttpMethod.Post, "/api/payments/charge");
        request.Content = body;
        return await _client.SendAsync(request);
    }

    public async Task<HttpResponseMessage> VoidAsync()
    {
        var request = new HttpRequestMessage(HttpMethod.Delete, "/api/payments/void");
        return await _client.SendAsync(request);
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "PaymentService.cs", src)
	want := []string{
		"http:POST:/api/payments/charge",
		"http:DELETE:/api/payments/void",
	}
	requireContains(t, ids, want, "cs-httprequestmessage")
	requireFetches(t, rels, "http:POST:/api/payments/charge", "cs-httprequestmessage")
	requireFetches(t, rels, "http:DELETE:/api/payments/void", "cs-httprequestmessage")
}

// ---------------------------------------------------------------------------
// RestSharp
// ---------------------------------------------------------------------------

// TestCsClient_RestSharp covers `new RestRequest(path, Method.Get)` + ExecuteAsync.
func TestCsClient_RestSharp(t *testing.T) {
	src := `
using RestSharp;

public class InventoryService
{
    public async Task<RestResponse> GetInventoryAsync()
    {
        var client = new RestClient("https://api.example.com");
        var request = new RestRequest("/api/inventory", Method.Get);
        return await client.ExecuteAsync(request);
    }

    public async Task<RestResponse> AddItemAsync()
    {
        var client = new RestClient("https://api.example.com");
        var request = new RestRequest("/api/inventory", Method.Post);
        return await client.ExecuteAsync(request);
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "InventoryService.cs", src)
	want := []string{
		"http:GET:/api/inventory",
		"http:POST:/api/inventory",
	}
	requireContains(t, ids, want, "cs-restsharp")
	requireFetches(t, rels, "http:GET:/api/inventory", "cs-restsharp")
	requireFetches(t, rels, "http:POST:/api/inventory", "cs-restsharp")
}

// ---------------------------------------------------------------------------
// Refit
// ---------------------------------------------------------------------------

// TestCsClient_RefitAnnotations covers Refit verb annotations on interface methods.
func TestCsClient_RefitAnnotations(t *testing.T) {
	src := `
using Refit;
using System.Collections.Generic;
using System.Threading.Tasks;

public interface IUserApi
{
    [Get("/api/users")]
    Task<List<User>> GetUsersAsync();

    [Post("/api/users")]
    Task<User> CreateUserAsync([Body] User user);

    [Put("/api/users/{id}")]
    Task UpdateUserAsync(int id, [Body] User user);

    [Delete("/api/users/{id}")]
    Task DeleteUserAsync(int id);

    [Patch("/api/users/{id}")]
    Task PatchUserAsync(int id, [Body] UserPatch patch);
}
`
	ids, rels := runDetectWithRels(t, "csharp", "IUserApi.cs", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
		"http:PUT:/api/users/{id}",
		"http:DELETE:/api/users/{id}",
		"http:PATCH:/api/users/{id}",
	}
	requireContains(t, ids, want, "cs-refit-annotations")
	requireFetches(t, rels, "http:GET:/api/users", "cs-refit-annotations")
	requireFetches(t, rels, "http:POST:/api/users", "cs-refit-annotations")
	requireFetches(t, rels, "http:DELETE:/api/users/{id}", "cs-refit-annotations")
}

// TestCsClient_RefitFullAnnotationSuite covers all Refit annotation verbs.
func TestCsClient_RefitFullAnnotationSuite(t *testing.T) {
	src := `
using Refit;
using System.Threading.Tasks;

public interface IFullApi
{
    [Get("/api/resources")]
    Task<string> GetAsync();

    [Post("/api/resources")]
    Task<string> PostAsync([Body] string body);

    [Put("/api/resources/{id}")]
    Task PutAsync(int id, [Body] string body);

    [Delete("/api/resources/{id}")]
    Task DeleteAsync(int id);

    [Patch("/api/resources/{id}")]
    Task PatchAsync(int id, [Body] string patch);

    [Head("/api/resources")]
    Task HeadAsync();
}
`
	ids, _ := runDetectWithRels(t, "csharp", "IFullApi.cs", src)
	want := []string{
		"http:GET:/api/resources",
		"http:POST:/api/resources",
		"http:PUT:/api/resources/{id}",
		"http:DELETE:/api/resources/{id}",
		"http:PATCH:/api/resources/{id}",
		"http:HEAD:/api/resources",
	}
	requireContains(t, ids, want, "cs-refit-full-suite")
}

// ---------------------------------------------------------------------------
// WebClient (legacy)
// ---------------------------------------------------------------------------

// TestCsClient_WebClientDownload covers wc.DownloadString(url) → GET.
func TestCsClient_WebClientDownload(t *testing.T) {
	src := `
using System.Net;

public class LegacyService
{
    public string FetchData()
    {
        var wc = new WebClient();
        return wc.DownloadString("https://api.example.com/api/legacy");
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "LegacyService.cs", src)
	requireContains(t, ids, []string{"http:GET:/api/legacy"}, "cs-webclient-download")
	requireFetches(t, rels, "http:GET:/api/legacy", "cs-webclient-download")
}

// TestCsClient_WebClientUpload covers wc.UploadString(url, data) → POST.
func TestCsClient_WebClientUpload(t *testing.T) {
	src := `
using System.Net;

public class LegacyUploadService
{
    public string SendData(string data)
    {
        var wc = new WebClient();
        return wc.UploadString("https://api.example.com/api/legacy/upload", data);
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "LegacyUploadService.cs", src)
	requireContains(t, ids, []string{"http:POST:/api/legacy/upload"}, "cs-webclient-upload")
	requireFetches(t, rels, "http:POST:/api/legacy/upload", "cs-webclient-upload")
}

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// TestCsClient_EnvVarConcat covers
// `await client.GetAsync(Environment.GetEnvironmentVariable("API_URL") + "/users")`
// → runtime_dynamic=true.
func TestCsClient_EnvVarConcat(t *testing.T) {
	src := `
using System;
using System.Net.Http;

public class RemoteService
{
    private readonly HttpClient _client;

    public async Task<string> CallRemoteAsync()
    {
        var response = await _client.GetAsync(Environment.GetEnvironmentVariable("API_URL") + "/api/data");
        return await response.Content.ReadAsStringAsync();
    }
}
`
	ids, rels := runDetectWithRels(t, "csharp", "RemoteService.cs", src)
	requireContains(t, ids, []string{"http:GET:/api/data"}, "cs-env-var-concat")
	requireFetches(t, rels, "http:GET:/api/data", "cs-env-var-concat")

	// Verify runtime_dynamic=true.
	_, res := runDetect(t, "csharp", "RemoteService.cs", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:GET:/api/data" && e.Properties["runtime_dynamic"] == "true" {
			found = true
		}
	}
	if !found {
		t.Errorf("cs-env-var-concat: expected runtime_dynamic=true on http:GET:/api/data")
	}
}

// ---------------------------------------------------------------------------
// Negative case
// ---------------------------------------------------------------------------

// TestCsClient_Negative verifies that a non-HTTP C# file does not emit
// any http_endpoint synthetics.
func TestCsClient_Negative(t *testing.T) {
	src := `
using System;
using System.Collections.Generic;

public class DataProcessor
{
    public List<string> Process(List<string> items)
    {
        var result = new List<string>();
        foreach (var item in items)
        {
            result.Add(item.ToUpper());
        }
        return result;
    }

    public string FormatMessage(string name, int count)
    {
        return $"Hello {name}, you have {count} items.";
    }
}
`
	ids, _ := runDetectWithRels(t, "csharp", "DataProcessor.cs", src)
	for _, id := range ids {
		if strings.HasPrefix(id, "http:") {
			t.Errorf("cs-negative: unexpected http_endpoint %q from non-HTTP file", id)
		}
	}
}
