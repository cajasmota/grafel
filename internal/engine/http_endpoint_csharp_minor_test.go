package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// csMinorEndpoint looks up the synthesized http_endpoint_definition with the
// given canonical ID in a DetectResult and returns it. nil when absent.
func csMinorEndpoint(res *DetectResult, id string) *types.EntityRecord {
	for i := range res.Entities {
		e := &res.Entities[i]
		if e.ID == id && (e.Kind == httpEndpointDefinitionKind || e.Kind == httpEndpointKind) {
			return e
		}
	}
	return nil
}

// requireCSMinorEndpoint asserts a canonical endpoint exists AND carries the
// expected framework + source_handler attribution — the specific #3962
// capability artifact (canonical synthesis + handler_attribution), not len>0.
func requireCSMinorEndpoint(t *testing.T, res *DetectResult, id, wantFramework, wantHandler string) {
	t.Helper()
	e := csMinorEndpoint(res, id)
	if e == nil {
		t.Fatalf("missing canonical endpoint %q", id)
	}
	if got := e.Properties["framework"]; got != wantFramework {
		t.Errorf("%s: framework=%q want %q", id, got, wantFramework)
	}
	if wantHandler != "" {
		if got := e.Properties["source_handler"]; got != wantHandler {
			t.Errorf("%s: source_handler=%q want %q", id, got, wantHandler)
		}
	}
}

// TestSynth_Carter covers app.MapVerb routes inside an ICarterModule.
func TestSynth_Carter(t *testing.T) {
	src := `using Carter;
public class WidgetsModule : ICarterModule
{
    public void AddRoutes(IEndpointRouteBuilder app)
    {
        app.MapGet("/widgets", () => Results.Ok());
        app.MapGet("/widgets/{id:int}", (int id) => Results.Ok());
        app.MapPost("/widgets", (CreateWidget req) => Results.Ok());
        app.MapDelete("/widgets/{id}", (int id) => Results.NoContent());
    }
}`
	ids, res := runDetect(t, "csharp", "WidgetsModule.cs", src)
	requireContains(t, ids, []string{
		"http:GET:/widgets",
		"http:GET:/widgets/{id}",
		"http:POST:/widgets",
		"http:DELETE:/widgets/{id}",
	}, "carter")
	requireCSMinorEndpoint(t, res, "http:POST:/widgets", "carter", "SCOPE.Operation:WidgetsModule.AddRoutes")
}

// TestSynth_FastEndpoints covers Endpoint<TReq> + bare verb routes in Configure().
func TestSynth_FastEndpoints(t *testing.T) {
	src := `using FastEndpoints;
public class CreateWidgetEndpoint : Endpoint<CreateWidgetReq, WidgetRes>
{
    public override void Configure()
    {
        Post("/widgets");
        AllowAnonymous();
    }
    public override async Task HandleAsync(CreateWidgetReq req, CancellationToken ct) { }
}`
	ids, res := runDetect(t, "csharp", "CreateWidgetEndpoint.cs", src)
	requireContains(t, ids, []string{"http:POST:/widgets"}, "fastendpoints")
	requireCSMinorEndpoint(t, res, "http:POST:/widgets", "fastendpoints",
		"SCOPE.Operation:CreateWidgetEndpoint.HandleAsync")
}

// TestSynth_FastEndpoints_NoHandleAsync attributes to the class when no
// HandleAsync method is present (handler-name heuristic fallback).
func TestSynth_FastEndpoints_NoHandleAsync(t *testing.T) {
	src := `using FastEndpoints;
public class ListWidgetsEndpoint : EndpointWithoutRequest<List<WidgetRes>>
{
    public override void Configure()
    {
        Get("/widgets");
    }
}`
	// EndpointWithoutRequest still trips the signal via the using import; but the
	// class regex requires Endpoint< — provide the generic base to exercise the path.
	src = `using FastEndpoints;
public class ListWidgetsEndpoint : Endpoint<EmptyRequest, List<WidgetRes>>
{
    public override void Configure()
    {
        Get("/widgets");
    }
}`
	_, res := runDetect(t, "csharp", "ListWidgetsEndpoint.cs", src)
	requireCSMinorEndpoint(t, res, "http:GET:/widgets", "fastendpoints",
		"SCOPE.Operation:ListWidgetsEndpoint")
}

// TestSynth_Nancy covers both index `Get["/x"]` and call `Get("/x")` syntax.
func TestSynth_Nancy(t *testing.T) {
	src := `using Nancy;
public class WidgetsModule : NancyModule
{
    public WidgetsModule()
    {
        Get["/widgets"] = _ => 200;
        Post("/widgets", args => CreateWidget(args));
        Delete["/widgets/{id}"] = args => 204;
    }
}`
	ids, res := runDetect(t, "csharp", "WidgetsModule.cs", src)
	requireContains(t, ids, []string{
		"http:GET:/widgets",
		"http:POST:/widgets",
		"http:DELETE:/widgets/{id}",
	}, "nancy")
	requireCSMinorEndpoint(t, res, "http:GET:/widgets", "nancyfx", "SCOPE.Operation:WidgetsModule")
}

// TestSynth_ServiceStack covers [Route("/path","VERBS")] + : Service handlers.
func TestSynth_ServiceStack(t *testing.T) {
	src := `using ServiceStack;

[Route("/widgets", "GET POST")]
public class WidgetRequest { public int Id { get; set; } }

[Route("/widgets/{Id}", "GET")]
public class WidgetByIdRequest { public int Id { get; set; } }

public class WidgetService : Service
{
    public object Get(WidgetRequest req) => null;
    public object Post(WidgetRequest req) => null;
}`
	ids, res := runDetect(t, "csharp", "WidgetService.cs", src)
	requireContains(t, ids, []string{
		"http:GET:/widgets",
		"http:POST:/widgets",
		"http:GET:/widgets/{Id}",
	}, "servicestack")
	requireCSMinorEndpoint(t, res, "http:POST:/widgets", "servicestack",
		"SCOPE.Operation:WidgetService.Post")
	// GET maps to the Get handler.
	requireCSMinorEndpoint(t, res, "http:GET:/widgets", "servicestack",
		"SCOPE.Operation:WidgetService.Get")
}

// TestSynth_ServiceStack_AnyHandler — a [Route] with no explicit verb string
// and only an `Any` handler defaults to GET attributed to `.Any`.
func TestSynth_ServiceStack_AnyHandler(t *testing.T) {
	src := `using ServiceStack;

[Route("/ping")]
public class PingRequest {}

public class PingService : Service
{
    public object Any(PingRequest req) => "pong";
}`
	_, res := runDetect(t, "csharp", "PingService.cs", src)
	requireCSMinorEndpoint(t, res, "http:GET:/ping", "servicestack", "SCOPE.Operation:PingService.Any")
}

// ---------------------------------------------------------------------------
// Negatives — the synthesizers must NOT fire on the wrong frameworks.
// ---------------------------------------------------------------------------

// TestSynth_CSMinor_NoSignal_NoOp — a plain ASP.NET Core controller must NOT
// be claimed by any of the four minor synthesizers (only aspnet_core owns it).
func TestSynth_CSMinor_NoSignal_NoOp(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("/api/widgets")]
public class WidgetsController : ControllerBase
{
    [HttpGet] public IActionResult List() => Ok();
}`
	_, res := runDetect(t, "csharp", "WidgetsController.cs", src)
	// The single endpoint must be attributed to aspnet_core, never carter/etc.
	e := csMinorEndpoint(res, "http:GET:/api/widgets")
	if e == nil {
		t.Fatalf("aspnet endpoint missing")
	}
	if fw := e.Properties["framework"]; fw != "aspnet_core" {
		t.Errorf("aspnet endpoint mis-attributed to framework=%q", fw)
	}
}

// TestSynth_Carter_NoModule_NoOp — bare app.MapGet outside an ICarterModule
// (plain minimal-API) must NOT be claimed by the Carter synthesizer.
func TestSynth_Carter_NoModule_NoOp(t *testing.T) {
	src := `using Carter;
var app = builder.Build();
app.MapGet("/health", () => "ok");`
	_, res := runDetect(t, "csharp", "Program.cs", src)
	if e := csMinorEndpoint(res, "http:GET:/health"); e != nil && e.Properties["framework"] == "carter" {
		t.Errorf("minimal-API route mis-claimed by Carter synthesizer: %v", e.Properties)
	}
}

// TestSynth_FastEndpoints_NoConsumerCall — a `client.Get("/x")` HttpClient
// consumer call must NOT be mistaken for a FastEndpoints producer route.
func TestSynth_FastEndpoints_NoConsumerCall(t *testing.T) {
	src := `using FastEndpoints;
public class CreateWidgetEndpoint : Endpoint<CreateWidgetReq>
{
    public override void Configure() { Post("/widgets"); }
    public override async Task HandleAsync(CreateWidgetReq req, CancellationToken ct)
    {
        var other = await httpClient.Get("/downstream");
    }
}`
	_, res := runDetect(t, "csharp", "CreateWidgetEndpoint.cs", src)
	// The producer route fires.
	requireCSMinorEndpoint(t, res, "http:POST:/widgets", "fastendpoints",
		"SCOPE.Operation:CreateWidgetEndpoint.HandleAsync")
	// The `.Get("/downstream")` receiver-call must NOT be a fastendpoints producer.
	if e := csMinorEndpoint(res, "http:GET:/downstream"); e != nil && e.Properties["framework"] == "fastendpoints" {
		t.Errorf("consumer .Get call mis-claimed as FastEndpoints producer route: %v", e.Properties)
	}
}
