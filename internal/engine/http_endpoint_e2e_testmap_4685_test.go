package engine

import (
	"strings"
	"testing"

	// Register the C# integration-e2e extractor so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

// Issue #4685 LIVE-REPRO (resolve side, full in-pipeline) — ASP.NET Core.
//
// Proves end-to-end that an ASP.NET Core integration test calling a route by
// string through WebApplicationFactory + HttpClient links to the
// http_endpoint_definition it exercises — the C#/.NET slice of the all-language
// program (#4615), generalizing #4351 / #4369 / #4370 / #4371 / #4684.
//
// Pipeline (all REAL passes):
//  1. applyHTTPEndpointSynthesis over the controller source → http_endpoint_*.
//  2. The real custom_csharp_integration_e2e extractor over the test file →
//     the one-per-file test_suite carrying e2e_route_calls.
//  3. ResolveHTTPEndpointHandlers over the merged set → the shared
//     linkE2ERouteTestsToEndpoints pass emits the TESTS→endpoint edges.

const csControllerSrc4685 = `using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/v1/inspections")]
public class InspectionsController : ControllerBase
{
    [HttpGet("{id}")]
    public IActionResult GetOne(int id) => Ok();

    [HttpPost("{id}/items")]
    public IActionResult CreateItem(int id) => Created();
}
`

const csIntegrationTestSrc4685 = `using System.Net.Http;
using System.Net.Http.Json;
using Xunit;

public class InspectionsControllerTests : IClassFixture<WebApplicationFactory<Program>>
{
    private readonly WebApplicationFactory<Program> _factory;
    public InspectionsControllerTests(WebApplicationFactory<Program> f) { _factory = f; }

    [Fact]
    public async Task GetOne_Returns200()
    {
        var client = _factory.CreateClient();
        var resp = await client.GetAsync("/api/v1/inspections/123");
        resp.EnsureSuccessStatusCode();
    }

    [Fact]
    public async Task CreateItem_Returns201()
    {
        var client = _factory.CreateClient();
        await client.PostAsJsonAsync("/api/v1/inspections/123/items", new { name = "x" });
    }
}
`

func TestIssue4685_CSharpIntegrationE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := synthEndpoints(t, "csharp", "src/Controllers/InspectionsController.cs", csControllerSrc4685)
	suite := realSuite(t, "custom_csharp_integration_e2e",
		"tests/InspectionsControllerTests.cs", "csharp", csIntegrationTestSrc4685)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	targets := edgeTargets(afterOut)

	wantGet, wantPost := false, false
	for to := range targets {
		if strings.Contains(to, "GET:/api/v1/inspections/{id}") && !strings.Contains(to, "items") {
			wantGet = true
		}
		if strings.Contains(to, "POST:/api/v1/inspections/{id}/items") {
			wantPost = true
		}
	}
	if !wantGet {
		t.Errorf("no TESTS edge to GET /api/v1/inspections/{id} (targets=%v)", targets)
	}
	if !wantPost {
		t.Errorf("no TESTS edge to POST /api/v1/inspections/{id}/items (targets=%v)", targets)
	}
	t.Logf("#4685 ASP.NET Core integration endpoint-level TESTS edges: before=0 after=%d", edges)
}
