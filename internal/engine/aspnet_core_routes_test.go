package engine

import (
	"testing"
)

// TestASPNetCore_Basic covers class-level [Route] + per-method verb
// attributes — the dominant ASP.NET Core pattern.
func TestASPNetCore_Basic(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("/api/widgets")]
public class WidgetsController : ControllerBase
{
    [HttpGet]
    public IActionResult List() { return Ok(); }

    [HttpGet("{id}")]
    public IActionResult Get(int id) { return Ok(); }

    [HttpPost]
    public IActionResult Create() { return Ok(); }
}
`
	ids, _ := runDetect(t, "csharp", "WidgetsController.cs", src)
	want := []string{
		"http:GET:/api/widgets",
		"http:GET:/api/widgets/{id}",
		"http:POST:/api/widgets",
	}
	requireContains(t, ids, want, "aspnet-basic")
}

// TestASPNetCore_ControllerTokenSubstitution verifies the `[controller]`
// token expands to the lower-cased controller class name (minus the
// "Controller" suffix).
func TestASPNetCore_ControllerTokenSubstitution(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("/api/[controller]")]
public class WidgetsController : ControllerBase
{
    [HttpGet]
    public IActionResult List() { return Ok(); }

    [HttpGet("{id}")]
    public IActionResult Get(int id) { return Ok(); }
}
`
	ids, _ := runDetect(t, "csharp", "WidgetsController.cs", src)
	want := []string{
		"http:GET:/api/widgets",
		"http:GET:/api/widgets/{id}",
	}
	requireContains(t, ids, want, "aspnet-controller-token")
}

// TestASPNetCore_AllVerbs covers get/post/put/patch/delete/head/options.
func TestASPNetCore_AllVerbs(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;

[Route("/res")]
public class ResController : ControllerBase
{
    [HttpGet] public IActionResult G() { return Ok(); }
    [HttpPost] public IActionResult P() { return Ok(); }
    [HttpPut("{id}")] public IActionResult U(int id) { return Ok(); }
    [HttpPatch("{id}")] public IActionResult Pa(int id) { return Ok(); }
    [HttpDelete("{id}")] public IActionResult D(int id) { return Ok(); }
    [HttpHead] public IActionResult H() { return Ok(); }
    [HttpOptions] public IActionResult O() { return Ok(); }
}
`
	ids, _ := runDetect(t, "csharp", "ResController.cs", src)
	want := []string{
		"http:GET:/res",
		"http:POST:/res",
		"http:PUT:/res/{id}",
		"http:PATCH:/res/{id}",
		"http:DELETE:/res/{id}",
		"http:HEAD:/res",
		"http:OPTIONS:/res",
	}
	requireContains(t, ids, want, "aspnet-all-verbs")
}

// TestASPNetCore_MethodLevelAbsolutePath covers a method-level attribute
// whose path starts with `/` and therefore OVERRIDES the class prefix
// (matching ASP.NET Core routing semantics).
func TestASPNetCore_MethodLevelAbsolutePath(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;

[Route("/api/[controller]")]
public class HealthController : ControllerBase
{
    [HttpGet("/healthz")]
    public IActionResult Healthz() { return Ok(); }
}
`
	ids, _ := runDetect(t, "csharp", "HealthController.cs", src)
	want := []string{"http:GET:/healthz"}
	requireContains(t, ids, want, "aspnet-method-absolute")
}

// TestASPNetCore_RouteConstraintStripped verifies that the `:int`
// route-constraint suffix on `{id:int}` is dropped during canonicalisation
// so producer and consumer sides agree on the same synthetic ID.
func TestASPNetCore_RouteConstraintStripped(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;

[Route("/api/widgets")]
public class WidgetsController : ControllerBase
{
    [HttpGet("{id:int}")]
    public IActionResult Get(int id) { return Ok(); }
}
`
	ids, _ := runDetect(t, "csharp", "WidgetsController.cs", src)
	want := []string{"http:GET:/api/widgets/{id}"}
	requireContains(t, ids, want, "aspnet-route-constraint")
}
