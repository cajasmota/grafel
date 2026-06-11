package csharp_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// e2eSuite runs the C# integration-e2e extractor and returns the single
// test_suite entity (or nil).
func e2eSuite(t *testing.T, path, src string) *types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_csharp_integration_e2e")
	if !ok {
		t.Fatal("custom_csharp_integration_e2e not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: "csharp", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for i := range ents {
		if ents[i].Subtype == "test_suite" {
			return &ents[i]
		}
	}
	return nil
}

func routeLines(s *types.EntityRecord) []string {
	if s == nil || s.Properties == nil {
		return nil
	}
	raw := s.Properties["e2e_route_calls"]
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func hasRouteLine(s *types.EntityRecord, want string) bool {
	for _, l := range routeLines(s) {
		if l == want {
			return true
		}
	}
	return false
}

// Validation B: WebApplicationFactory + HttpClient.GetAsync route hit → suite
// with the VERB route pair stamped (TESTS edge linked downstream by the shared
// resolve pass).
func TestCSharpE2E_WebAppFactoryGetAsync(t *testing.T) {
	src := `
public class XControllerTests : IClassFixture<WebApplicationFactory<Program>>
{
    private readonly WebApplicationFactory<Program> _factory;
    public XControllerTests(WebApplicationFactory<Program> f) { _factory = f; }

    [Fact]
    public async Task GetCounts_Returns200()
    {
        var client = _factory.CreateClient();
        var resp = await client.GetAsync("/api/v1/x/get_counts");
        resp.EnsureSuccessStatusCode();
    }
}
`
	s := e2eSuite(t, "tests/XControllerTests.cs", src)
	if s == nil {
		t.Fatal("expected a test_suite entity")
	}
	if !hasRouteLine(s, "GET /api/v1/x/get_counts") {
		t.Errorf("expected 'GET /api/v1/x/get_counts'; got %v", routeLines(s))
	}
	if s.Properties["framework"] != "aspnetcore_integration" {
		t.Errorf("framework=%q", s.Properties["framework"])
	}
}

// PostAsJsonAsync (typed-content JSON helper) → POST verb.
func TestCSharpE2E_PostAsJsonAsync(t *testing.T) {
	src := `
public class OrdersTests
{
    [Fact]
    public async Task Create()
    {
        var client = _factory.CreateClient();
        await client.PostAsJsonAsync("/api/v1/orders", dto);
    }
}
`
	s := e2eSuite(t, "OrdersTests.cs", src)
	if !hasRouteLine(s, "POST /api/v1/orders") {
		t.Errorf("expected 'POST /api/v1/orders'; got %v", routeLines(s))
	}
}

// new HttpRequestMessage(HttpMethod.Put, "/x/1") → PUT verb from member.
func TestCSharpE2E_HttpRequestMessage(t *testing.T) {
	src := `
public class XTests
{
    [Fact]
    public async Task Update()
    {
        var req = new HttpRequestMessage(HttpMethod.Put, "/api/v1/x/1");
        await client.SendAsync(req);
    }
}
`
	s := e2eSuite(t, "XTests.cs", src)
	if !hasRouteLine(s, "PUT /api/v1/x/1") {
		t.Errorf("expected 'PUT /api/v1/x/1'; got %v", routeLines(s))
	}
}

// Query string is stripped from the captured route.
func TestCSharpE2E_QueryStringStripped(t *testing.T) {
	src := `
public class XTests
{
    [Fact]
    public async Task List()
    {
        await client.GetAsync("/api/v1/x?page=2&size=10");
    }
}
`
	s := e2eSuite(t, "XTests.cs", src)
	if !hasRouteLine(s, "GET /api/v1/x") {
		t.Errorf("expected 'GET /api/v1/x' (query stripped); got %v", routeLines(s))
	}
}

// Negative: an interpolated / non-literal route is NOT captured (conservative).
func TestCSharpE2E_InterpolatedRouteDropped(t *testing.T) {
	src := `
public class XTests
{
    [Fact]
    public async Task Get()
    {
        var id = 5;
        await client.GetAsync($"/api/v1/x/{id}");
        await client.GetAsync(routeVar);
    }
}
`
	s := e2eSuite(t, "XTests.cs", src)
	if s != nil && len(routeLines(s)) != 0 {
		t.Errorf("expected no captured routes for non-literal calls; got %v", routeLines(s))
	}
}

// Negative: a non-test file with no test signals and no HttpClient call is a
// strict no-op.
func TestCSharpE2E_NonTestNoOp(t *testing.T) {
	src := `
public class OrderService
{
    public void Process() { }
}
`
	if s := e2eSuite(t, "src/OrderService.cs", src); s != nil {
		t.Errorf("expected no suite for production file; got %+v", s)
	}
}
