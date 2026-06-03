package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// C#/.NET rate-limit stamping (#4089)
// ---------------------------------------------------------------------------

// rlFind returns the first rate_limit SCOPE.Pattern whose rate_limit_name
// matches `policy`, or nil. For AspNetCoreRateLimit (no policy name) pass "".
func rlFind(ents []types.EntityRecord, policy string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "rate_limit" &&
			e.Properties["rate_limit_name"] == policy {
			return e
		}
	}
	return nil
}

// TestRateLimitRequireFixedWindowResolvesRate is the headline value-asserting
// case: .RequireRateLimiting("fixed") + an in-file AddFixedWindowLimiter("fixed",
// PermitLimit=100, Window=FromMinutes(1)) resolves rate="100/60s",
// source=fixed_window, name=fixed, scope=route.
func TestRateLimitRequireFixedWindowResolvesRate(t *testing.T) {
	src := `
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddRateLimiter(o =>
{
    o.AddFixedWindowLimiter("fixed", opt =>
    {
        opt.PermitLimit = 100;
        opt.Window = TimeSpan.FromMinutes(1);
    });
});
var app = builder.Build();
app.MapGet("/api/x", () => "ok").RequireRateLimiting("fixed");
app.Run();
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Program.cs", "csharp", src))
	e := rlFind(ents, "fixed")
	if e == nil {
		t.Fatal("expected rate_limit marker for policy 'fixed'")
	}
	if got := e.Properties["rate_limited"]; got != "true" {
		t.Errorf("rate_limited = %q, want true", got)
	}
	if got := e.Properties["rate_limit"]; got != "100/60s" {
		t.Errorf("rate_limit = %q, want 100/60s", got)
	}
	if got := e.Properties["rate_limit_source"]; got != "fixed_window" {
		t.Errorf("rate_limit_source = %q, want fixed_window", got)
	}
	if got := e.Properties["rate_limit_scope"]; got != "route" {
		t.Errorf("rate_limit_scope = %q, want route", got)
	}
}

// TestRateLimitSlidingWindowSeconds asserts seconds-window resolution and the
// sliding-window source.
func TestRateLimitSlidingWindowSeconds(t *testing.T) {
	src := `
builder.Services.AddRateLimiter(o =>
{
    o.AddSlidingWindowLimiter("sw", opt =>
    {
        opt.PermitLimit = 30;
        opt.Window = TimeSpan.FromSeconds(30);
    });
});
app.MapPost("/api/y", () => "ok").RequireRateLimiting("sw");
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Program.cs", "csharp", src))
	e := rlFind(ents, "sw")
	if e == nil {
		t.Fatal("expected rate_limit marker for policy 'sw'")
	}
	if got := e.Properties["rate_limit"]; got != "30/30s" {
		t.Errorf("rate_limit = %q, want 30/30s", got)
	}
	if got := e.Properties["rate_limit_source"]; got != "sliding_window" {
		t.Errorf("rate_limit_source = %q, want sliding_window", got)
	}
}

// TestRateLimitTokenBucketLimit asserts TokenLimit + Hours window resolution and
// the token-bucket source.
func TestRateLimitTokenBucketLimit(t *testing.T) {
	src := `
builder.Services.AddRateLimiter(o =>
{
    o.AddTokenBucketLimiter("tb", opt =>
    {
        opt.TokenLimit = 10;
        opt.Window = TimeSpan.FromHours(1);
    });
});
app.MapGet("/z", () => "ok").RequireRateLimiting("tb");
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Program.cs", "csharp", src))
	e := rlFind(ents, "tb")
	if e == nil {
		t.Fatal("expected rate_limit marker for policy 'tb'")
	}
	if got := e.Properties["rate_limit"]; got != "10/3600s" {
		t.Errorf("rate_limit = %q, want 10/3600s", got)
	}
	if got := e.Properties["rate_limit_source"]; got != "token_bucket" {
		t.Errorf("rate_limit_source = %q, want token_bucket", got)
	}
}

// TestRateLimitEnableAttributeNamesPolicy: [EnableRateLimiting("api")] on a
// controller action stamps rate_limited naming the policy, scope=route.
func TestRateLimitEnableAttributeNamesPolicy(t *testing.T) {
	src := `
[ApiController]
[Route("api/[controller]")]
public class OrdersController : ControllerBase
{
    [HttpGet]
    [EnableRateLimiting("api")]
    public IActionResult Get() => Ok();
}
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("OrdersController.cs", "csharp", src))
	e := rlFind(ents, "api")
	if e == nil {
		t.Fatal("expected rate_limit marker for policy 'api'")
	}
	if got := e.Properties["rate_limited"]; got != "true" {
		t.Errorf("rate_limited = %q, want true", got)
	}
	if got := e.Properties["rate_limit_scope"]; got != "route" {
		t.Errorf("rate_limit_scope = %q, want route", got)
	}
	if got := e.Properties["rate_limit_binding"]; got != "enable_rate_limiting" {
		t.Errorf("rate_limit_binding = %q, want enable_rate_limiting", got)
	}
	// No in-file policy 'api' defined → honest-partial (rate omitted).
	if got := e.Properties["rate_limit"]; got != "" {
		t.Errorf("rate_limit = %q, want empty (cross-file policy honest-partial)", got)
	}
}

// TestRateLimitCrossFilePolicyHonestPartial: a .RequireRateLimiting("remote")
// whose policy is defined elsewhere stamps rate_limited but omits the rate and
// falls back to the binding idiom as the source.
func TestRateLimitCrossFilePolicyHonestPartial(t *testing.T) {
	src := `app.MapGet("/p", () => "ok").RequireRateLimiting("remote");`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Endpoints.cs", "csharp", src))
	e := rlFind(ents, "remote")
	if e == nil {
		t.Fatal("expected rate_limit marker for policy 'remote'")
	}
	if got := e.Properties["rate_limited"]; got != "true" {
		t.Errorf("rate_limited = %q, want true", got)
	}
	if _, has := e.Properties["rate_limit"]; has {
		t.Errorf("rate_limit should be omitted for cross-file policy, got %q", e.Properties["rate_limit"])
	}
	if got := e.Properties["rate_limit_source"]; got != "require_rate_limiting" {
		t.Errorf("rate_limit_source = %q, want require_rate_limiting", got)
	}
}

// TestRateLimitAspNetCoreRateLimitEngine: app.UseIpRateLimiting() stamps an
// engine-scope, config-driven (rate-omitted) marker.
func TestRateLimitAspNetCoreRateLimitEngine(t *testing.T) {
	src := `
var app = builder.Build();
app.UseIpRateLimiting();
app.Run();
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Program.cs", "csharp", src))
	var e *types.EntityRecord
	for i := range ents {
		if ents[i].Properties["rate_limit_source"] == "aspnetcoreratelimit" {
			e = &ents[i]
			break
		}
	}
	if e == nil {
		t.Fatal("expected aspnetcoreratelimit engine marker")
	}
	if got := e.Properties["rate_limit_scope"]; got != "engine" {
		t.Errorf("rate_limit_scope = %q, want engine", got)
	}
	if got := e.Properties["rate_limit_variant"]; got != "ip" {
		t.Errorf("rate_limit_variant = %q, want ip", got)
	}
	if _, has := e.Properties["rate_limit"]; has {
		t.Errorf("rate_limit should be omitted (config-driven), got %q", e.Properties["rate_limit"])
	}
}

// TestRateLimitDisableIsNegative: [DisableRateLimiting] on an action is NOT
// stamped as rate-limited.
func TestRateLimitDisableIsNegative(t *testing.T) {
	src := `
public class HealthController : ControllerBase
{
    [HttpGet]
    [DisableRateLimiting]
    [EnableRateLimiting("api")]
    public IActionResult Get() => Ok();
}
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("HealthController.cs", "csharp", src))
	if e := rlFind(ents, "api"); e != nil {
		t.Errorf("policy 'api' should be suppressed by adjacent [DisableRateLimiting], got marker %q", e.Name)
	}
}

// TestRateLimitPlainEndpointNone: a plain endpoint with no rate-limit idiom
// produces no rate_limit markers.
func TestRateLimitPlainEndpointNone(t *testing.T) {
	src := `
app.MapGet("/open", () => "ok").AllowAnonymous();
app.MapGet("/plain", () => "ok");
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Program.cs", "csharp", src))
	for _, e := range ents {
		if e.Subtype == "rate_limit" {
			t.Errorf("plain/anonymous endpoint should yield no rate_limit marker, got %q", e.Name)
		}
	}
}

// TestRateLimitConcurrencyKindNoRate: a concurrency limiter resolves its kind as
// the source but has no window, so the rate is honest-partial (omitted).
func TestRateLimitConcurrencyKindNoRate(t *testing.T) {
	src := `
builder.Services.AddRateLimiter(o =>
{
    o.AddConcurrencyLimiter("conc", opt =>
    {
        opt.PermitLimit = 5;
    });
});
app.MapGet("/c", () => "ok").RequireRateLimiting("conc");
`
	ents := extractFull(t, "custom_csharp_rate_limit", fi("Program.cs", "csharp", src))
	e := rlFind(ents, "conc")
	if e == nil {
		t.Fatal("expected rate_limit marker for policy 'conc'")
	}
	if got := e.Properties["rate_limit_source"]; got != "concurrency" {
		t.Errorf("rate_limit_source = %q, want concurrency", got)
	}
	if _, has := e.Properties["rate_limit"]; has {
		t.Errorf("concurrency limiter has no window; rate should be omitted, got %q", e.Properties["rate_limit"])
	}
}
