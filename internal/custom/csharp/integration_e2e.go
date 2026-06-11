package csharp

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// integration_e2e.go — link ASP.NET Core integration-test route-by-string calls
// to the http_endpoint_definition they exercise (issue #4685, the C#/.NET slice
// of #4615).
//
// Background
// ----------
// ASP.NET Core integration tests drive the app in-process through HTTP using
// WebApplicationFactory<Program> + HttpClient:
//
//	var client = _factory.CreateClient();
//	var resp = await client.GetAsync("/api/v1/inspections/123/items");
//	await client.PostAsJsonAsync("/api/v1/inspections", dto);
//	await client.PutAsync("/api/v1/inspections/1", content);
//
// but no edge ever connected that route string to the http_endpoint_definition
// it exercises — so those endpoints looked untested. This generalizes the
// NestJS/supertest fix #4351, the Python/pytest fix #4369, the Java/Spring fix
// #4370, the Go/httptest fix #4371, and the Ruby/RSpec fix #4684 to ASP.NET
// Core's WebApplicationFactory + HttpClient.
//
// Extractor side: this extractor emits exactly ONE test_suite entity per C#
// test file that issues at least one HttpClient route-by-string call, and
// stamps the `VERB route` pairs onto its `e2e_route_calls` property — the same
// property every other language's e2e extractor stamps. It is decoupled from
// any unit-test SUT-affinity suite; the shared resolve pass fires on whatever
// carries `e2e_route_calls`.
//
// Resolve side: the shared pass (engine.linkE2ERouteTestsToEndpoints, #4351) is
// REUSED UNCHANGED. It already gates on Kind||Subtype=="test_suite", wildcards
// {id}/:id definition segments, and tolerates the /api/vN mount prefix. No
// matcher fork, no new producer Kind.
//
// Conservatism: only literal "/..."-shaped routes are captured; a route built
// from a variable / interpolated string ($"/x/{id}") / non-literal expression
// (no static leading-slash path) is dropped, and the shared resolver emits an
// edge only on a UNIQUE verb+route match. NB: this is distinct from the
// consumer-side HttpClient synthesis in engine/http_endpoint_csharp_client.go,
// which models OUTBOUND calls to OTHER services (FETCHES edge to a synthetic
// consumer endpoint); here the same HttpClient call in a TEST file is treated
// as an in-process route hit against the app's OWN endpoints (TESTS edge).

func init() {
	extractor.Register("custom_csharp_integration_e2e", &csharpIntegrationE2EExtractor{})
}

type csharpIntegrationE2EExtractor struct{}

func (e *csharpIntegrationE2EExtractor) Language() string {
	return "custom_csharp_integration_e2e"
}

var (
	// HttpClient verb methods whose verb is encoded in the METHOD NAME and whose
	// FIRST argument is the route. Covers the plain async verbs
	// (GetAsync/PostAsync/PutAsync/DeleteAsync/PatchAsync), the typed-content
	// JSON helpers (PostAsJsonAsync/PutAsJsonAsync/PatchAsJsonAsync), and the
	// string/stream read helpers (GetStringAsync/GetByteArrayAsync/GetStreamAsync).
	// The route is a plain "..." or verbatim @"..." string literal. Group 1 =
	// method, group 2 = double-quoted route, group 3 = verbatim route.
	csE2EHttpClientVerbRe = regexp.MustCompile(
		`\.\s*(GetAsync|PostAsync|PutAsync|DeleteAsync|PatchAsync|HeadAsync|OptionsAsync|` +
			`PostAsJsonAsync|PutAsJsonAsync|PatchAsJsonAsync|DeleteFromJsonAsync|` +
			`GetFromJsonAsync|GetStringAsync|GetByteArrayAsync|GetStreamAsync)\s*` +
			`(?:<[^>(]*>)?\s*\(\s*(?:"(/[^"\r\n]*)"|@"(/[^"\r\n]*)")`)

	// new HttpRequestMessage(HttpMethod.Get, "/route") — verb is the HttpMethod
	// member, route the second arg. Group 1 = verb member, group 2/3 = route.
	csE2EHttpRequestMsgRe = regexp.MustCompile(
		`new\s+HttpRequestMessage\s*\(\s*HttpMethod\s*\.\s*(Get|Post|Put|Delete|Patch|Head|Options)\s*,\s*` +
			`(?:"(/[^"\r\n]*)"|@"(/[^"\r\n]*)")`)

	// csE2EVerbFromMethod maps an HttpClient method name to its HTTP verb.
	csE2EVerbFromMethod = map[string]string{
		"GetAsync": "GET", "GetStringAsync": "GET", "GetByteArrayAsync": "GET",
		"GetStreamAsync": "GET", "GetFromJsonAsync": "GET",
		"PostAsync": "POST", "PostAsJsonAsync": "POST",
		"PutAsync": "PUT", "PutAsJsonAsync": "PUT",
		"PatchAsync": "PATCH", "PatchAsJsonAsync": "PATCH",
		"DeleteAsync": "DELETE", "DeleteFromJsonAsync": "DELETE",
		"HeadAsync": "HEAD", "OptionsAsync": "OPTIONS",
	}

	// csE2EVerbFromHttpMethod maps an HttpMethod.X member to its HTTP verb.
	csE2EVerbFromHttpMethod = map[string]string{
		"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
		"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
	}

	// Test-file signals — at least one must be present for the extractor to
	// fire. xUnit ([Fact]/[Theory]), NUnit ([Test]/[TestFixture]), MSTest
	// ([TestMethod]/[TestClass]), or the WebApplicationFactory / HttpClient
	// integration-test harness itself.
	csE2ETestSignals = []string{
		"[Fact]", "[Theory]", "[Test]", "[TestFixture]", "[TestMethod]",
		"[TestClass]", "WebApplicationFactory", "IClassFixture", "HttpClient",
		"CreateClient",
	}
)

func (e *csharpIntegrationE2EExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_integration_e2e_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "aspnetcore_integration"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "csharp" {
		return nil, nil
	}
	src := string(file.Content)

	// Only fire on test files (by name or by a test-framework signal) that
	// actually issue an HttpClient route-by-string call — a strict no-op
	// everywhere else.
	if !csLooksLikeTestFile(file.Path) && !csHasAnyTestSignal(src) {
		return nil, nil
	}
	if !strings.Contains(src, "Async") && !strings.Contains(src, "HttpRequestMessage") {
		return nil, nil
	}

	routeCalls := collectCSharpE2ERouteCalls(src)
	if len(routeCalls) == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	ent := makeEntity("aspnetcore_e2e_suite:"+csTestBaseName(file.Path), "SCOPE.Pattern", "test_suite",
		file.Path, file.Language, 1)
	setProps(&ent, "framework", "aspnetcore_integration",
		"provenance", "INFERRED_FROM_ASPNETCORE_INTEGRATION_ROUTE",
		"test_framework", "aspnetcore_integration",
		"e2e_route_calls", strings.Join(routeCalls, "\n"),
		"e2e_route_count", itoa(len(routeCalls)),
	)

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
}

// collectCSharpE2ERouteCalls extracts every ASP.NET Core HttpClient
// route-by-string call in a C# test file and returns de-duplicated
// `VERB route` lines — the exact shape the shared resolve pass consumes
// (engine.linkE2ERouteTestsToEndpoints). Two shapes are covered:
//
//	client.GetAsync("/x/123/items")          (verb in method name)
//	client.PostAsJsonAsync("/x", dto)        (verb in method name)
//	new HttpRequestMessage(HttpMethod.Get, "/x/1")  (verb in HttpMethod member)
//
// A call whose route is not a literal leading-slash path (interpolated /
// concatenated / variable route) is dropped — conservative; the resolver
// wildcards {id}/:id definition segments so concrete ids are fine verbatim.
func collectCSharpE2ERouteCalls(src string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, route string) {
		verb = strings.ToUpper(strings.TrimSpace(verb))
		route = strings.TrimSpace(route)
		if verb == "" || !strings.HasPrefix(route, "/") {
			return
		}
		// Drop a query string / fragment; the resolver matches on path only.
		if q := strings.IndexAny(route, "?#"); q >= 0 {
			route = route[:q]
		}
		if route == "" || !strings.HasPrefix(route, "/") {
			return
		}
		line := verb + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}

	for _, m := range csE2EHttpClientVerbRe.FindAllStringSubmatch(src, -1) {
		verb := csE2EVerbFromMethod[m[1]]
		route := m[2]
		if route == "" {
			route = m[3] // verbatim @"..."
		}
		add(verb, route)
	}
	for _, m := range csE2EHttpRequestMsgRe.FindAllStringSubmatch(src, -1) {
		verb := csE2EVerbFromHttpMethod[m[1]]
		route := m[2]
		if route == "" {
			route = m[3]
		}
		add(verb, route)
	}
	return out
}

// csHasAnyTestSignal reports whether the source carries any xUnit/NUnit/MSTest
// or WebApplicationFactory/HttpClient integration-test marker.
func csHasAnyTestSignal(src string) bool {
	for _, sig := range csE2ETestSignals {
		if strings.Contains(src, sig) {
			return true
		}
	}
	return false
}

// csLooksLikeTestFile reports whether a path matches the conventional .NET test
// file naming (Tests.cs / Test.cs / Spec.cs, or under a tests/ directory).
func csLooksLikeTestFile(path string) bool {
	lower := strings.ToLower(path)
	base := lower
	if i := strings.LastIndexAny(lower, "/\\"); i >= 0 {
		base = lower[i+1:]
	}
	if strings.HasSuffix(base, "tests.cs") || strings.HasSuffix(base, "test.cs") ||
		strings.HasSuffix(base, "spec.cs") || strings.HasSuffix(base, ".tests.cs") {
		return true
	}
	return strings.Contains(lower, "/tests/") || strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "\\tests\\") || strings.Contains(lower, "\\test\\")
}

// csTestBaseName returns the test file's base name without directory or the
// `.cs` extension — used to namespace the suite entity so it never collides
// with a production symbol in the resolver's by-name index.
func csTestBaseName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".cs")
}
