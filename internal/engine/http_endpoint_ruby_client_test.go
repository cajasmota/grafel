package engine

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Net::HTTP class-level verbs
// ---------------------------------------------------------------------------

// TestRubyClient_NetHTTPClassLiteral covers Net::HTTP.get(URI(url)) and
// Net::HTTP.post(URI(url), data) with literal string URLs.
func TestRubyClient_NetHTTPClassLiteral(t *testing.T) {
	src := `
require 'net/http'
require 'uri'

def fetch_users
  Net::HTTP.get(URI("https://api.example.com/api/users"))
end

def create_user(data)
  Net::HTTP.post(URI("https://api.example.com/api/users"), data)
end
`
	ids, rels := runDetectWithRels(t, "ruby", "users_client.rb", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "ruby-net-http-class-literal")
	requireFetches(t, rels, "http:GET:/api/users", "ruby-net-http-class-literal")
	requireFetches(t, rels, "http:POST:/api/users", "ruby-net-http-class-literal")
}

// TestRubyClient_NetHTTPInstanceVerbs covers the instance form:
// http = Net::HTTP.new(host, port); http.get(path), http.post(path)
func TestRubyClient_NetHTTPInstanceVerbs(t *testing.T) {
	src := `
require 'net/http'

def fetch_orders
  http = Net::HTTP.new("api.example.com", 443)
  http.get("/api/orders")
end

def place_order(body)
  conn = Net::HTTP.new("api.example.com", 443)
  conn.post("/api/orders", body)
end
`
	ids, rels := runDetectWithRels(t, "ruby", "orders_client.rb", src)
	want := []string{
		"http:GET:/api/orders",
		"http:POST:/api/orders",
	}
	requireContains(t, ids, want, "ruby-net-http-instance")
	requireFetches(t, rels, "http:GET:/api/orders", "ruby-net-http-instance")
	requireFetches(t, rels, "http:POST:/api/orders", "ruby-net-http-instance")
}

// TestRubyClient_NetHTTPStartBlock covers the block form:
// Net::HTTP.start(host, port) { |http| http.get(path) }
func TestRubyClient_NetHTTPStartBlock(t *testing.T) {
	src := `
require 'net/http'

def fetch_profile
  Net::HTTP.start("api.example.com", 443) { |http| http.get("/api/profile") }
end
`
	ids, rels := runDetectWithRels(t, "ruby", "profile_client.rb", src)
	requireContains(t, ids, []string{"http:GET:/api/profile"}, "ruby-net-http-start-block")
	requireFetches(t, rels, "http:GET:/api/profile", "ruby-net-http-start-block")
}

// ---------------------------------------------------------------------------
// Faraday
// ---------------------------------------------------------------------------

// TestRubyClient_FaradayClass covers Faraday.get(url), Faraday.post(url).
func TestRubyClient_FaradayClass(t *testing.T) {
	src := `
require 'faraday'

def list_products
  Faraday.get("https://api.example.com/api/products")
end

def create_product(body)
  Faraday.post("https://api.example.com/api/products")
end
`
	ids, rels := runDetectWithRels(t, "ruby", "faraday_client.rb", src)
	want := []string{
		"http:GET:/api/products",
		"http:POST:/api/products",
	}
	requireContains(t, ids, want, "ruby-faraday-class")
	requireFetches(t, rels, "http:GET:/api/products", "ruby-faraday-class")
	requireFetches(t, rels, "http:POST:/api/products", "ruby-faraday-class")
}

// TestRubyClient_FaradayInstance covers conn.get(path), conn.post(path) { |req| ... }
func TestRubyClient_FaradayInstance(t *testing.T) {
	src := `
require 'faraday'

def search_items(q)
  conn = Faraday.new(url: 'https://api.example.com')
  conn.get('/api/search')
end

def submit_form(data)
  conn = Faraday.new(url: 'https://api.example.com')
  conn.post('/api/submissions') do |req|
    req.body = data
  end
end
`
	ids, rels := runDetectWithRels(t, "ruby", "faraday_instance.rb", src)
	want := []string{
		"http:GET:/api/search",
		"http:POST:/api/submissions",
	}
	requireContains(t, ids, want, "ruby-faraday-instance")
	requireFetches(t, rels, "http:GET:/api/search", "ruby-faraday-instance")
	requireFetches(t, rels, "http:POST:/api/submissions", "ruby-faraday-instance")
}

// ---------------------------------------------------------------------------
// HTTParty
// ---------------------------------------------------------------------------

// TestRubyClient_HTTPartyClass covers HTTParty.get(url), HTTParty.post(url, body: ...).
func TestRubyClient_HTTPartyClass(t *testing.T) {
	src := `
require 'httparty'

def get_status
  HTTParty.get("https://api.example.com/api/status")
end

def login(creds)
  HTTParty.post("https://api.example.com/api/auth/login", body: creds)
end
`
	ids, rels := runDetectWithRels(t, "ruby", "httparty_client.rb", src)
	want := []string{
		"http:GET:/api/status",
		"http:POST:/api/auth/login",
	}
	requireContains(t, ids, want, "ruby-httparty-class")
	requireFetches(t, rels, "http:GET:/api/status", "ruby-httparty-class")
	requireFetches(t, rels, "http:POST:/api/auth/login", "ruby-httparty-class")
}

// TestRubyClient_HTTPartyIncluded covers the include HTTParty form where
// HTTP methods are called without the HTTParty. prefix.
func TestRubyClient_HTTPartyIncluded(t *testing.T) {
	src := `
require 'httparty'

class UserService
  include HTTParty
  base_uri 'https://api.example.com'

  def users
    get('/api/users')
  end

  def create(user)
    post('/api/users')
  end
end
`
	ids, rels := runDetectWithRels(t, "ruby", "user_service.rb", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "ruby-httparty-included")
	requireFetches(t, rels, "http:GET:/api/users", "ruby-httparty-included")
	requireFetches(t, rels, "http:POST:/api/users", "ruby-httparty-included")
}

// ---------------------------------------------------------------------------
// RestClient
// ---------------------------------------------------------------------------

// TestRubyClient_RestClientClass covers RestClient.get(url), RestClient.post(url, payload).
func TestRubyClient_RestClientClass(t *testing.T) {
	src := `
require 'rest-client'

def fetch_payments
  RestClient.get("https://api.example.com/api/payments")
end

def create_payment(payload)
  RestClient.post("https://api.example.com/api/payments", payload)
end
`
	ids, rels := runDetectWithRels(t, "ruby", "rest_client_class.rb", src)
	want := []string{
		"http:GET:/api/payments",
		"http:POST:/api/payments",
	}
	requireContains(t, ids, want, "ruby-rest-client-class")
	requireFetches(t, rels, "http:GET:/api/payments", "ruby-rest-client-class")
	requireFetches(t, rels, "http:POST:/api/payments", "ruby-rest-client-class")
}

// TestRubyClient_RestClientResource covers RestClient::Resource.new(url).get and .post.
func TestRubyClient_RestClientResource(t *testing.T) {
	src := `
require 'rest-client'

def fetch_inventory
  RestClient::Resource.new("https://api.example.com/api/inventory").get
end

def add_item(payload)
  RestClient::Resource.new("https://api.example.com/api/inventory").post(payload)
end
`
	ids, rels := runDetectWithRels(t, "ruby", "rest_resource.rb", src)
	want := []string{
		"http:GET:/api/inventory",
		"http:POST:/api/inventory",
	}
	requireContains(t, ids, want, "ruby-rest-client-resource")
	requireFetches(t, rels, "http:GET:/api/inventory", "ruby-rest-client-resource")
	requireFetches(t, rels, "http:POST:/api/inventory", "ruby-rest-client-resource")
}

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// TestRubyClient_EnvVarConcat covers Net::HTTP.get(URI(ENV['API_URL'] + '/users'))
// → runtime_dynamic=true.
func TestRubyClient_EnvVarConcat(t *testing.T) {
	src := `
require 'net/http'
require 'uri'

def call_remote
  Net::HTTP.get(URI(ENV['API_URL'] + '/users'))
end
`
	ids, rels := runDetectWithRels(t, "ruby", "env_client.rb", src)
	requireContains(t, ids, []string{"http:GET:/users"}, "ruby-env-var-concat")
	requireFetches(t, rels, "http:GET:/users", "ruby-env-var-concat")

	// Verify runtime_dynamic=true is stamped on the entity.
	_, res := runDetect(t, "ruby", "env_client.rb", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:GET:/users" && e.Properties["runtime_dynamic"] == "true" {
			found = true
		}
	}
	if !found {
		t.Errorf("ruby-env-var-concat: expected runtime_dynamic=true on http:GET:/users")
	}
}

// ---------------------------------------------------------------------------
// Verb coverage
// ---------------------------------------------------------------------------

// TestRubyClient_VerbCoverage covers all HTTP verbs on Net::HTTP and HTTParty.
func TestRubyClient_VerbCoverage(t *testing.T) {
	src := `
require 'httparty'

def all_verbs
  HTTParty.get("https://api.example.com/api/items")
  HTTParty.post("https://api.example.com/api/items")
  HTTParty.put("https://api.example.com/api/items/1")
  HTTParty.patch("https://api.example.com/api/items/1")
  HTTParty.delete("https://api.example.com/api/items/1")
  HTTParty.head("https://api.example.com/api/items")
  HTTParty.options("https://api.example.com/api/items")
end
`
	ids, _ := runDetectWithRels(t, "ruby", "all_verbs.rb", src)
	want := []string{
		"http:GET:/api/items",
		"http:POST:/api/items",
		"http:PUT:/api/items/1",
		"http:PATCH:/api/items/1",
		"http:DELETE:/api/items/1",
		"http:HEAD:/api/items",
		"http:OPTIONS:/api/items",
	}
	requireContains(t, ids, want, "ruby-verb-coverage")
}

// ---------------------------------------------------------------------------
// Negative case
// ---------------------------------------------------------------------------

// TestRubyClient_Negative verifies that a non-HTTP Ruby file does not emit
// any http_endpoint synthetics.
func TestRubyClient_Negative(t *testing.T) {
	src := `
class DataProcessor
  def process(items)
    items.map { |item| item.upcase }
  end

  def filter(items, &block)
    items.select(&block)
  end
end
`
	ids, _ := runDetectWithRels(t, "ruby", "data_processor.rb", src)
	for _, id := range ids {
		if strings.HasPrefix(id, "http:") {
			t.Errorf("ruby-negative: unexpected http_endpoint %q from non-HTTP file", id)
		}
	}
}
