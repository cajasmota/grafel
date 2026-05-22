// Unit tests for #1483 — NestJS HttpService (RxJS) + Apollo Client URI +
// Elixir Finch/HTTPoison consumer-side extraction.
package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Idiom 1 — NestJS HttpService (RxJS)
// ---------------------------------------------------------------------------

// TestSynth_NestHttpService_StaticURL covers `this.httpService.get("url")` and
// `this.httpService.post("url", body)` with static string literals.
func TestSynth_NestHttpService_StaticURL(t *testing.T) {
	src := `
import { Injectable } from '@nestjs/common';
import { HttpService } from '@nestjs/axios';
import { map } from 'rxjs/operators';

@Injectable()
export class GatewayService {
  constructor(private httpService: HttpService) {}

  getOrders() {
    return this.httpService
      .get('http://orders:3000/orders')
      .pipe(map(r => r.data));
  }

  createOrder(body: any) {
    return this.httpService
      .post('http://orders:3000/orders', body)
      .pipe(map(r => r.data));
  }

  deleteOrder(id: string) {
    return this.httpService
      .delete('http://orders:3000/orders/' + id)
      .pipe(map(r => r.data));
  }
}
`
	got, _ := runDetect(t, "typescript", "gateway.service.ts", src)
	want := []string{
		"http:GET:/orders",
		"http:POST:/orders",
	}
	requireContains(t, got, want, "nestjs-httpservice-static")
}

// TestSynth_NestHttpService_AbsoluteURLStripHost verifies that absolute URLs
// (http://service:port/path) are host-stripped to produce canonical paths
// for cross-repo matching.
func TestSynth_NestHttpService_AbsoluteURLStripHost(t *testing.T) {
	src := `
@Injectable()
export class GatewayService {
  constructor(private httpService: HttpService) {}

  getProducts() {
    return this.httpService
      .get('http://catalog:3001/products')
      .pipe(map(r => r.data));
  }

  getProduct(id: string) {
    return this.httpService.get('http://catalog:3001/products/' + id);
  }
}
`
	got, _ := runDetect(t, "typescript", "gateway2.service.ts", src)
	want := []string{"http:GET:/products"}
	requireContains(t, got, want, "nestjs-httpservice-host-strip")
}

// TestSynth_NestHttpService_TemplateLiteral verifies template-literal URLs
// in HttpService calls produce named placeholders.
func TestSynth_NestHttpService_TemplateLiteral(t *testing.T) {
	src := "import { HttpService } from '@nestjs/axios';\n" +
		"\n" +
		"@Injectable()\n" +
		"export class GatewayService {\n" +
		"  constructor(private httpService: HttpService) {}\n" +
		"\n" +
		"  getOrderById(orderId: string) {\n" +
		"    return this.httpService\n" +
		"      .get(`http://orders:3000/orders/${orderId}`)\n" +
		"      .pipe(map(r => r.data));\n" +
		"  }\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "gateway3.service.ts", src)
	want := []string{"http:GET:/orders/{orderId}"}
	requireContains(t, got, want, "nestjs-httpservice-template-literal")
}

// TestSynth_NestHttpService_AllVerbs checks that all standard HTTP verbs are
// recognised on this.httpService.
func TestSynth_NestHttpService_AllVerbs(t *testing.T) {
	src := `
@Injectable()
export class ApiGatewayService {
  constructor(private httpService: HttpService) {}

  all() {
    this.httpService.get('http://svc:8000/items').subscribe();
    this.httpService.post('http://svc:8000/items', {}).subscribe();
    this.httpService.put('http://svc:8000/items/1', {}).subscribe();
    this.httpService.patch('http://svc:8000/items/1', {}).subscribe();
    this.httpService.delete('http://svc:8000/items/1').subscribe();
  }
}
`
	got, _ := runDetect(t, "typescript", "gateway4.service.ts", src)
	want := []string{
		"http:GET:/items",
		"http:POST:/items",
		"http:PUT:/items/1",
		"http:PATCH:/items/1",
		"http:DELETE:/items/1",
	}
	requireContains(t, got, want, "nestjs-httpservice-all-verbs")
}

// ---------------------------------------------------------------------------
// Idiom 2 — Apollo Client URI
// ---------------------------------------------------------------------------

// TestSynth_ApolloClientURI_Basic covers the basic `new ApolloClient({ uri: "..." })` pattern.
func TestSynth_ApolloClientURI_Basic(t *testing.T) {
	src := `
import { ApolloClient, InMemoryCache } from '@apollo/client';

const client = new ApolloClient({
  uri: "http://search-graphql:4000/graphql",
  cache: new InMemoryCache(),
});

export default client;
`
	got, _ := runDetect(t, "typescript", "apollo-client.ts", src)
	want := []string{"http:GRAPHQL:/graphql"}
	requireContains(t, got, want, "apollo-client-uri-basic")
}

// TestSynth_ApolloClientURI_PathOnly covers a URI with path but no port.
func TestSynth_ApolloClientURI_PathOnly(t *testing.T) {
	src := `
const adminClient = new ApolloClient({
  uri: "http://api.example.com/graphql/v2",
  cache: new InMemoryCache(),
});
`
	got, _ := runDetect(t, "typescript", "admin-apollo.ts", src)
	want := []string{"http:GRAPHQL:/graphql/v2"}
	requireContains(t, got, want, "apollo-client-uri-path")
}

// TestSynth_ApolloClientURI_EnvFallback covers the env-var fallback pattern:
// `uri: process.env.GQL_URL || "http://service:4000/graphql"`.
func TestSynth_ApolloClientURI_EnvFallback(t *testing.T) {
	src := `
import { ApolloClient, InMemoryCache } from '@apollo/client';

const client = new ApolloClient({
  uri: process.env.GRAPHQL_URL || "http://search-graphql:4000/graphql",
  cache: new InMemoryCache(),
});
`
	got, _ := runDetect(t, "javascript", "apollo-env.js", src)
	want := []string{"http:GRAPHQL:/graphql"}
	requireContains(t, got, want, "apollo-client-uri-env-fallback")
}

// TestSynth_ApolloClientURI_NoFalsePositive verifies that a plain
// ApolloClient({ cache }) usage (no uri) does NOT emit an endpoint.
func TestSynth_ApolloClientURI_NoFalsePositive(t *testing.T) {
	src := `
const client = new ApolloClient({
  cache: new InMemoryCache(),
  link: authLink.concat(httpLink),
});
`
	got, _ := runDetect(t, "typescript", "no-uri.ts", src)
	for _, id := range got {
		if id == "http:GRAPHQL:/graphql" {
			t.Errorf("false positive: emitted GRAPHQL synthetic for ApolloClient with no uri prop")
		}
	}
}

// ---------------------------------------------------------------------------
// Idiom 3 — Elixir Finch / HTTPoison
// ---------------------------------------------------------------------------

// TestSynth_ElixirFinch_StaticURL covers `Finch.build(:get, "url")`.
func TestSynth_ElixirFinch_StaticURL(t *testing.T) {
	src := `
defmodule RealtimeDashboard.GatewayClient do
  def get_status do
    Finch.build(:get, "http://gateway:4000/status")
    |> Finch.request(RealtimeDashboard.Finch)
  end

  def create_event(body) do
    Finch.build(:post, "http://gateway:4000/events")
    |> Finch.request(RealtimeDashboard.Finch)
  end
end
`
	got, _ := runDetect(t, "elixir", "gateway_client.ex", src)
	want := []string{
		"http:GET:/status",
		"http:POST:/events",
	}
	requireContains(t, got, want, "elixir-finch-static")
}

// TestSynth_ElixirFinch_InterpolatedVariable covers the pattern where the
// URL is assembled via string interpolation and passed as a variable:
//
//	url = "#{@base_url}/orders/#{id}"
//	Finch.build(:get, url)
func TestSynth_ElixirFinch_InterpolatedVariable(t *testing.T) {
	src := `
defmodule RealtimeDashboard.OrdersClient do
  @base_url "http://gateway:4000"

  def get_order(id) do
    url = "#{@base_url}/orders/#{id}"
    Finch.build(:get, url)
    |> Finch.request(RealtimeDashboard.Finch)
  end

  def list_orders do
    url = "#{@base_url}/orders"
    Finch.build(:get, url)
    |> Finch.request(RealtimeDashboard.Finch)
  end
end
`
	got, _ := runDetect(t, "elixir", "orders_client.ex", src)
	want := []string{
		"http:GET:/orders/{id}",
		"http:GET:/orders",
	}
	requireContains(t, got, want, "elixir-finch-interpolated")
}

// TestSynth_ElixirHTTPoison_Static covers `HTTPoison.get("url")` and
// `HTTPoison.post("url", body)`.
func TestSynth_ElixirHTTPoison_Static(t *testing.T) {
	src := `
defmodule RealtimeDashboard.CatalogClient do
  def list_products do
    HTTPoison.get("http://catalog:3001/products")
  end

  def create_product(body) do
    HTTPoison.post("http://catalog:3001/products", body)
  end
end
`
	got, _ := runDetect(t, "elixir", "catalog_client.ex", src)
	want := []string{
		"http:GET:/products",
		"http:POST:/products",
	}
	requireContains(t, got, want, "elixir-httpoison-static")
}

// TestSynth_ElixirFinch_NoFalsePositive ensures that non-HTTP Finch usage
// (e.g. `Finch.start_link`) does NOT emit endpoint synthetics.
func TestSynth_ElixirFinch_NoFalsePositive(t *testing.T) {
	src := `
defmodule MyApp.Application do
  def start(_type, _args) do
    children = [
      {Finch, name: MyApp.Finch}
    ]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
`
	got, _ := runDetect(t, "elixir", "application.ex", src)
	for _, id := range got {
		if id == "http:GET:/" || id == "http:POST:/" {
			t.Errorf("false positive Elixir Finch: emitted endpoint for non-HTTP Finch usage: %q", id)
		}
	}
}
