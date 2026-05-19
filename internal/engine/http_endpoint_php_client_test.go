package engine

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Guzzle instance methods
// ---------------------------------------------------------------------------

// TestPHPClient_GuzzleVerbMethods covers $client->get($url), $client->post($url)
// with an absolute URL.
func TestPHPClient_GuzzleVerbMethods(t *testing.T) {
	src := `<?php

use GuzzleHttp\Client;

function fetchUsers() {
    $client = new Client();
    $response = $client->get('https://api.example.com/api/users');
    return $response->getBody();
}

function createUser($data) {
    $client = new Client();
    $response = $client->post('https://api.example.com/api/users');
    return $response;
}
`
	ids, rels := runDetectWithRels(t, "php", "users_client.php", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "php-guzzle-verb-methods")
	requireFetches(t, rels, "http:GET:/api/users", "php-guzzle-verb-methods")
	requireFetches(t, rels, "http:POST:/api/users", "php-guzzle-verb-methods")
}

// TestPHPClient_GuzzleRequestMethod covers $client->request('POST', $url, ['json' => $body]).
func TestPHPClient_GuzzleRequestMethod(t *testing.T) {
	src := `<?php

use GuzzleHttp\Client;

function submitOrder($data) {
    $client = new Client();
    $response = $client->request('POST', 'https://api.example.com/api/orders', [
        'json' => $data,
    ]);
    return $response;
}

function fetchOrder($id) {
    $client = new Client();
    $response = $client->request('GET', '/api/orders/' . $id);
    return $response->getBody();
}
`
	ids, rels := runDetectWithRels(t, "php", "orders_client.php", src)
	want := []string{
		"http:POST:/api/orders",
	}
	requireContains(t, ids, want, "php-guzzle-request-method")
	requireFetches(t, rels, "http:POST:/api/orders", "php-guzzle-request-method")
}

// ---------------------------------------------------------------------------
// Symfony HttpClient
// ---------------------------------------------------------------------------

// TestPHPClient_SymfonyHttpClient covers HttpClient::create()->request('GET', $url)
// and the stored-client form.
func TestPHPClient_SymfonyHttpClient(t *testing.T) {
	src := `<?php

use Symfony\Component\HttpClient\HttpClient;

function fetchProducts() {
    $response = HttpClient::create()->request('GET', 'https://api.example.com/api/products');
    return $response->toArray();
}

function createProduct($data) {
    $client = HttpClient::create();
    $response = $client->request('POST', 'https://api.example.com/api/products', [
        'json' => $data,
    ]);
    return $response->getStatusCode();
}
`
	ids, rels := runDetectWithRels(t, "php", "symfony_client.php", src)
	want := []string{
		"http:GET:/api/products",
		"http:POST:/api/products",
	}
	requireContains(t, ids, want, "php-symfony-httpclient")
	requireFetches(t, rels, "http:GET:/api/products", "php-symfony-httpclient")
	requireFetches(t, rels, "http:POST:/api/products", "php-symfony-httpclient")
}

// ---------------------------------------------------------------------------
// cURL
// ---------------------------------------------------------------------------

// TestPHPClient_CurlGet covers curl_init($url) without CURLOPT_POST → GET.
func TestPHPClient_CurlGet(t *testing.T) {
	src := `<?php

function fetchData($url) {
    $ch = curl_init("https://api.example.com/api/payments");
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
    $result = curl_exec($ch);
    curl_close($ch);
    return $result;
}
`
	ids, rels := runDetectWithRels(t, "php", "curl_get.php", src)
	requireContains(t, ids, []string{"http:GET:/api/payments"}, "php-curl-get")
	requireFetches(t, rels, "http:GET:/api/payments", "php-curl-get")
}

// TestPHPClient_CurlPost covers curl_init + CURLOPT_POST=true → POST.
func TestPHPClient_CurlPost(t *testing.T) {
	src := `<?php

function postData($data) {
    $ch = curl_init("https://api.example.com/api/events");
    curl_setopt($ch, CURLOPT_POST, true);
    curl_setopt($ch, CURLOPT_POSTFIELDS, $data);
    $result = curl_exec($ch);
    curl_close($ch);
    return $result;
}
`
	ids, rels := runDetectWithRels(t, "php", "curl_post.php", src)
	requireContains(t, ids, []string{"http:POST:/api/events"}, "php-curl-post")
	requireFetches(t, rels, "http:POST:/api/events", "php-curl-post")
}

// ---------------------------------------------------------------------------
// file_get_contents
// ---------------------------------------------------------------------------

// TestPHPClient_FileGetContents covers file_get_contents("https://...") → GET.
func TestPHPClient_FileGetContents(t *testing.T) {
	src := `<?php

function fetchStatus() {
    $data = file_get_contents("https://api.example.com/api/status");
    return json_decode($data, true);
}
`
	ids, rels := runDetectWithRels(t, "php", "file_get_contents.php", src)
	requireContains(t, ids, []string{"http:GET:/api/status"}, "php-file-get-contents")
	requireFetches(t, rels, "http:GET:/api/status", "php-file-get-contents")
}

// ---------------------------------------------------------------------------
// WordPress HTTP API
// ---------------------------------------------------------------------------

// TestPHPClient_WPRemote covers wp_remote_get($url) and wp_remote_post($url, $args).
func TestPHPClient_WPRemote(t *testing.T) {
	src := `<?php

function get_remote_data($url) {
    $response = wp_remote_get('https://api.example.com/api/posts');
    return wp_remote_retrieve_body($response);
}

function send_remote_data($data) {
    $response = wp_remote_post('https://api.example.com/api/posts', [
        'body' => json_encode($data),
    ]);
    return $response;
}
`
	ids, rels := runDetectWithRels(t, "php", "wp_remote.php", src)
	want := []string{
		"http:GET:/api/posts",
		"http:POST:/api/posts",
	}
	requireContains(t, ids, want, "php-wp-remote")
	requireFetches(t, rels, "http:GET:/api/posts", "php-wp-remote")
	requireFetches(t, rels, "http:POST:/api/posts", "php-wp-remote")
}

// ---------------------------------------------------------------------------
// Laravel HTTP facade
// ---------------------------------------------------------------------------

// TestPHPClient_LaravelHttp covers Http::get($url), Http::post($url, $body).
func TestPHPClient_LaravelHttp(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Http;

function getNotifications() {
    $response = Http::get('https://api.example.com/api/notifications');
    return $response->json();
}

function sendNotification($data) {
    $response = Http::post('https://api.example.com/api/notifications', $data);
    return $response->status();
}
`
	ids, rels := runDetectWithRels(t, "php", "laravel_http.php", src)
	want := []string{
		"http:GET:/api/notifications",
		"http:POST:/api/notifications",
	}
	requireContains(t, ids, want, "php-laravel-http")
	requireFetches(t, rels, "http:GET:/api/notifications", "php-laravel-http")
	requireFetches(t, rels, "http:POST:/api/notifications", "php-laravel-http")
}

// TestPHPClient_LaravelHttpChained covers the chained form:
// Http::withHeaders([...])->get("/path").
func TestPHPClient_LaravelHttpChained(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Http;

function getProtectedResource($token) {
    $response = Http::withToken($token)->get('https://api.example.com/api/protected');
    return $response->json();
}

function postWithHeaders($data) {
    $response = Http::withHeaders(['X-Custom' => 'value'])->post('https://api.example.com/api/secure');
    return $response->json();
}
`
	ids, rels := runDetectWithRels(t, "php", "laravel_http_chained.php", src)
	want := []string{
		"http:GET:/api/protected",
		"http:POST:/api/secure",
	}
	requireContains(t, ids, want, "php-laravel-http-chained")
	requireFetches(t, rels, "http:GET:/api/protected", "php-laravel-http-chained")
	requireFetches(t, rels, "http:POST:/api/secure", "php-laravel-http-chained")
}

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// TestPHPClient_EnvVarConcat covers $client->get(getenv('API_URL') . '/users')
// → runtime_dynamic=true.
func TestPHPClient_EnvVarConcat(t *testing.T) {
	src := `<?php

use GuzzleHttp\Client;

function callRemote() {
    $client = new Client();
    $response = $client->get(getenv('API_URL') . '/users');
    return $response->getBody();
}
`
	ids, rels := runDetectWithRels(t, "php", "env_client.php", src)
	requireContains(t, ids, []string{"http:GET:/users"}, "php-env-var-concat")
	requireFetches(t, rels, "http:GET:/users", "php-env-var-concat")

	// Verify runtime_dynamic=true is stamped on the entity.
	_, res := runDetect(t, "php", "env_client.php", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:GET:/users" && e.Properties["runtime_dynamic"] == "true" {
			found = true
		}
	}
	if !found {
		t.Errorf("php-env-var-concat: expected runtime_dynamic=true on http:GET:/users")
	}
}

// ---------------------------------------------------------------------------
// Verb coverage
// ---------------------------------------------------------------------------

// TestPHPClient_VerbCoverage covers all HTTP verbs via Guzzle request().
func TestPHPClient_VerbCoverage(t *testing.T) {
	src := `<?php

use GuzzleHttp\Client;

function allVerbs() {
    $client = new Client();
    $client->get('https://api.example.com/api/items');
    $client->post('https://api.example.com/api/items');
    $client->put('https://api.example.com/api/items');
    $client->patch('https://api.example.com/api/items');
    $client->delete('https://api.example.com/api/items');
    $client->head('https://api.example.com/api/items');
}
`
	ids, _ := runDetectWithRels(t, "php", "all_verbs.php", src)
	want := []string{
		"http:GET:/api/items",
		"http:POST:/api/items",
		"http:PUT:/api/items",
		"http:PATCH:/api/items",
		"http:DELETE:/api/items",
		"http:HEAD:/api/items",
	}
	requireContains(t, ids, want, "php-verb-coverage")
}

// ---------------------------------------------------------------------------
// Negative case
// ---------------------------------------------------------------------------

// TestPHPClient_Negative verifies that a non-HTTP PHP file does not emit
// any http_endpoint synthetics.
func TestPHPClient_Negative(t *testing.T) {
	src := `<?php

class DataProcessor {
    public function process(array $items): array {
        return array_map(function($item) {
            return strtoupper($item);
        }, $items);
    }

    public function filter(array $items, callable $callback): array {
        return array_filter($items, $callback);
    }
}
`
	ids, _ := runDetectWithRels(t, "php", "data_processor.php", src)
	for _, id := range ids {
		if strings.HasPrefix(id, "http:") {
			t.Errorf("php-negative: unexpected http_endpoint %q from non-HTTP file", id)
		}
	}
}
