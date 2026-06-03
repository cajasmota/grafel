package engine

import "testing"

// ---------------------------------------------------------------------------
// C# / ASP.NET Core deprecation + api_version port (epic #3628).
//
// Mirrors the flagship property contract exactly: deprecated / deprecated_since
// / deprecated_replacement / deprecation_source / api_version. Uses the shared
// deprecProps/mustEndpoint harness from http_endpoint_deprecation_test.go.
// ---------------------------------------------------------------------------

// [Obsolete("use /api/v2/users")] on an attribute-routed action under
// /api/v1/users → deprecated=true + replacement + source + api_version=1 (path).
func TestDeprecation_CSharpObsoleteAttribute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/v1/[controller]")]
public class UsersController : ControllerBase
{
    [Obsolete("use /api/v2/users")]
    [HttpGet("/api/v1/users")]
    public IActionResult GetUsers() { return Ok(); }

    [HttpGet("/api/v1/health")]
    public IActionResult Health() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/UsersController.cs", src)

	dep := mustEndpoint(t, eps, "GET /api/v1/users")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /api/v1/users deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "[Obsolete]" {
		t.Errorf("deprecation_source=%q, want [Obsolete]", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/api/v2/users" {
		t.Errorf("deprecated_replacement=%q, want /api/v2/users", got)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1 (path-derived)", got)
	}

	// Negative: the non-obsolete sibling action carries no deprecation.
	live := mustEndpoint(t, eps, "GET /api/v1/health")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /api/v1/health deprecated fabricated, want absent (props: %v)", live.Properties)
	}
}

// [Obsolete("Deprecated since 2.0, use /reports/v2 instead")] resolves both the
// since-version and the replacement out of the message via the shared parser.
func TestDeprecation_CSharpObsoleteSinceAndReplacement(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
public class ReportsController : ControllerBase
{
    [Obsolete("since 2.0 use /reports/v2 instead")]
    [HttpGet("/reports")]
    public IActionResult GetReports() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/ReportsController.cs", src)
	dep := mustEndpoint(t, eps, "GET /reports")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /reports deprecated=%q, want true", dep.Properties["deprecated"])
	}
	if got := dep.Properties["deprecated_since"]; got != "2.0" {
		t.Errorf("deprecated_since=%q, want 2.0", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/reports/v2" {
		t.Errorf("deprecated_replacement=%q, want /reports/v2", got)
	}
}

// ApiExplorer [Deprecated] attribute marks the action deprecated.
func TestDeprecation_CSharpDeprecatedAttribute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
public class LegacyController : ControllerBase
{
    [Deprecated]
    [HttpGet("/legacy")]
    public IActionResult GetLegacy() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/LegacyController.cs", src)
	dep := mustEndpoint(t, eps, "GET /legacy")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /legacy deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "[Deprecated]" {
		t.Errorf("deprecation_source=%q, want [Deprecated]", got)
	}
}

// An action-level [ApiVersion("1.0", Deprecated = true)] sunset flag in the
// handler's attribute region marks that action deprecated AND pins its version.
func TestDeprecation_CSharpApiVersionDeprecatedFlag(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using Asp.Versioning;

[ApiController]
[Route("api/[controller]")]
public class BillingController : ControllerBase
{
    [ApiVersion("1.0", Deprecated = true)]
    [HttpGet("/billing")]
    public IActionResult GetBilling() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/BillingController.cs", src)
	dep := mustEndpoint(t, eps, "GET /billing")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /billing deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "[ApiVersion(Deprecated=true)]" {
		t.Errorf("deprecation_source=%q, want [ApiVersion(Deprecated=true)]", got)
	}
	// The version itself is pinned from the same attribute (no /vN in route).
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1 (from [ApiVersion])", got)
	}
}

// Honest-partial: a CONTROLLER-level [ApiVersion(Deprecated = true)] is not
// attributed to the action's deprecation state (the handler-region model keeps
// a class-wide flag from leaking across controllers in a multi-class file), but
// the version is still pinned via the sole-file [ApiVersion] fallback.
func TestDeprecation_CSharpControllerApiVersionDeprecatedIsPartial(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using Asp.Versioning;

[ApiController]
[ApiVersion("1.0", Deprecated = true)]
[Route("api/[controller]")]
public class BillingController : ControllerBase
{
    [HttpGet("/billing")]
    public IActionResult GetBilling() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/BillingController.cs", src)
	dep := mustEndpoint(t, eps, "GET /billing")
	if _, ok := dep.Properties["deprecated"]; ok {
		t.Fatalf("controller-level [ApiVersion(Deprecated)] leaked deprecation (props: %v)", dep.Properties)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1 (sole-file [ApiVersion])", got)
	}
}

// A Sunset response header written in the action body is a runtime deprecation
// signal (cross-language flagship path), proven to fire for C#.
func TestDeprecation_CSharpSunsetResponseHeader(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
public class PaymentsController : ControllerBase
{
    [HttpGet("/payments")]
    public IActionResult GetPayments() {
        Response.Headers.Append("Sunset", "Sat, 31 Dec 2025 23:59:59 GMT");
        return Ok();
    }
}
`
	eps := deprecProps(t, "csharp", "src/PaymentsController.cs", src)
	dep := mustEndpoint(t, eps, "GET /payments")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /payments deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "Sunset response header" {
		t.Errorf("deprecation_source=%q, want 'Sunset response header'", got)
	}
}

// ---------------------------------------------------------------------------
// api_version — [ApiVersion] attribute fallback (no /vN route segment)
// ---------------------------------------------------------------------------

// [ApiVersion("2.0")] on the controller pins api_version=2 (major only) on a
// conventional `api/[controller]` route that carries no version segment.
func TestAPIVersion_CSharpApiVersionAttribute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using Asp.Versioning;

[ApiController]
[ApiVersion("2.0")]
[Route("api/[controller]")]
public class OrdersController : ControllerBase
{
    [HttpGet("/orders")]
    public IActionResult GetOrders() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/OrdersController.cs", src)
	e := mustEndpoint(t, eps, "GET /orders")
	if got := e.Properties["api_version"]; got != "2" {
		t.Fatalf("api_version=%q, want 2 (from [ApiVersion(\"2.0\")])", got)
	}
}

// Negative: a versionless route with NO [ApiVersion] attribute carries no
// api_version (honest-partial — never fabricated).
func TestAPIVersion_CSharpNoVersionNoAttribute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
public class StatusController : ControllerBase
{
    [HttpGet("/status")]
    public IActionResult GetStatus() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/StatusController.cs", src)
	e := mustEndpoint(t, eps, "GET /status")
	if got, ok := e.Properties["api_version"]; ok {
		t.Fatalf("api_version=%q fabricated on versionless route, want absent", got)
	}
}

// The path-derived version wins over (and never conflicts with) the attribute
// path: an explicit /api/v3 segment yields api_version=3 even if an [ApiVersion]
// attribute names something else — path is the authoritative routable version.
func TestAPIVersion_CSharpPathWinsOverAttribute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;
using Asp.Versioning;

[ApiController]
[ApiVersion("2.0")]
public class CatalogController : ControllerBase
{
    [HttpGet("/api/v3/catalog")]
    public IActionResult GetCatalog() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/CatalogController.cs", src)
	e := mustEndpoint(t, eps, "GET /api/v3/catalog")
	if got := e.Properties["api_version"]; got != "3" {
		t.Fatalf("api_version=%q, want 3 (path-derived wins)", got)
	}
}

// Negative: a non-route [Obsolete] helper method must not leak deprecation onto
// an unrelated sibling endpoint.
func TestDeprecation_CSharpNonRouteObsoleteDoesNotLeak(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/v1/[controller]")]
public class AccountsController : ControllerBase
{
    [Obsolete("internal helper")]
    private string LegacyHelper() { return "x"; }

    [HttpGet("/api/v1/accounts")]
    public IActionResult GetAccounts() { return Ok(); }
}
`
	eps := deprecProps(t, "csharp", "src/AccountsController.cs", src)
	e := mustEndpoint(t, eps, "GET /api/v1/accounts")
	if _, ok := e.Properties["deprecated"]; ok {
		t.Fatalf("non-route [Obsolete] leaked onto GET /api/v1/accounts (props: %v)", e.Properties)
	}
}

// ---------------------------------------------------------------------------
// unit-level: attribute parsers
// ---------------------------------------------------------------------------

func TestCsharpDeprecationVerdict(t *testing.T) {
	cases := []struct {
		name       string
		region     string
		wantDep    bool
		wantSource string
		wantRepl   string
	}{
		{"obsolete bare", `[Obsolete]`, true, "[Obsolete]", ""},
		{"obsolete msg", `[Obsolete("use /api/v2/x")]`, true, "[Obsolete]", "/api/v2/x"},
		{"obsolete two-arg", `[Obsolete("gone", true)]`, true, "[Obsolete]", ""},
		{"fq obsolete", `[System.Obsolete("use /v2")]`, true, "[Obsolete]", "/v2"},
		{"deprecated attr", `[Deprecated]`, true, "[Deprecated]", ""},
		{"apiversion deprecated", `[ApiVersion("1.0", Deprecated = true)]`, true, "[ApiVersion(Deprecated=true)]", ""},
		{"none", `[HttpGet("/x")]`, false, "", ""},
		{"obsoletesomething no match", `[ObsoleteHelper]`, false, "", ""},
	}
	for _, c := range cases {
		v, ok := csharpDeprecationVerdict(c.region)
		if ok != c.wantDep {
			t.Errorf("%s: ok=%v, want %v", c.name, ok, c.wantDep)
			continue
		}
		if !c.wantDep {
			continue
		}
		if v.source != c.wantSource {
			t.Errorf("%s: source=%q, want %q", c.name, v.source, c.wantSource)
		}
		if v.replacement != c.wantRepl {
			t.Errorf("%s: replacement=%q, want %q", c.name, v.replacement, c.wantRepl)
		}
	}
}

func TestCsParseAPIVersion(t *testing.T) {
	cases := []struct {
		s    string
		want int
		ok   bool
	}{
		{`[ApiVersion("2.0")]`, 2, true},
		{`[ApiVersion("1")]`, 1, true},
		{`[ApiVersion( "3.1" )]`, 3, true},
		{`[ApiVersion("v2")]`, 2, true},
		{`[ApiVersion("100.0")]`, 0, false}, // out of range
		{`[Route("api/x")]`, 0, false},
	}
	for _, c := range cases {
		got, ok := csParseAPIVersion(c.s)
		if ok != c.ok || got != c.want {
			t.Errorf("csParseAPIVersion(%q)=(%d,%v), want (%d,%v)", c.s, got, ok, c.want, c.ok)
		}
	}
}

// A file declaring two DIFFERENT [ApiVersion] majors is ambiguous at the
// controller level → csSoleFileAPIVersion returns nothing.
func TestCsSoleFileAPIVersion_AmbiguousIsPartial(t *testing.T) {
	content := `[ApiVersion("1.0")] class A {} [ApiVersion("2.0")] class B {}`
	if v, ok := csSoleFileAPIVersion(content); ok {
		t.Fatalf("ambiguous file yielded api_version=%d, want none", v)
	}
	// Two identical declarations are unambiguous.
	content2 := `[ApiVersion("2.0")] class A {} [ApiVersion("2.0")] class B {}`
	if v, ok := csSoleFileAPIVersion(content2); !ok || v != 2 {
		t.Fatalf("identical-version file yielded (%d,%v), want (2,true)", v, ok)
	}
}
