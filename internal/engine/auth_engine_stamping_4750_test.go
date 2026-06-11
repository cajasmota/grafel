package engine

// auth_engine_stamping_4750_test.go — LIVE-PATH tests for #4750/#4751/#4752: the
// engine now stamps the STRUCTURED auth-posture props the authposture resolvers
// read, so a guarded endpoint resolves to the correct structured posture in the
// live diff (not just source-scan/unknown). Each test feeds source as the engine
// receives it, asserts the props the extractor stamps, then resolves them through
// the authposture registry exactly as the MCP auth_posture_diff tool does.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/authposture"
)

// resolveEndpointProps runs an endpoint's stamped props through the authposture
// registry the same way the MCP diff tool harvests them (the prop keys are the
// shared contract; this mirrors mcp.buildAuthSignal without the import cycle).
func resolveEndpointProps(props map[string]string) authposture.Posture {
	sig := authposture.Signal{Props: map[string]string{}}
	for k, v := range props {
		sig.Props[k] = v
	}
	sig.Framework = props["framework"]
	for _, k := range []string{
		"action_source", "handler_source", "controller_source", "view_source",
		"route_source", "router_source",
	} {
		if v := props[k]; v != "" {
			sig.Source = v
			break
		}
	}
	sig.Action = props["action"]
	p, _ := authposture.NewRegistry().Resolve(sig)
	return p
}

// endpointWithProps finds the synthesized http_endpoint_definition entity for
// (verb, path) and returns its properties.
func endpointWithProps(t *testing.T, res *DetectResult, verb, path string) map[string]string {
	t.Helper()
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind || e.Properties == nil {
			continue
		}
		if e.Properties["verb"] == verb && e.Properties["path"] == path {
			return e.Properties
		}
	}
	t.Fatalf("no endpoint %s %s found in result", verb, path)
	return nil
}

// --- #4750 ASP.NET Core: method ▸ class ▸ global [Authorize] precedence ---

func TestStamp_Aspnet_MethodRoles(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;
using Microsoft.AspNetCore.Authorization;

[ApiController]
[Route("/api/widgets")]
public class WidgetsController : ControllerBase
{
    [HttpGet]
    [Authorize(Roles = "Admin")]
    public IActionResult List() { return Ok(); }
}
`
	_, res := runDetect(t, "csharp", "WidgetsController.cs", src)
	props := endpointWithProps(t, res, "GET", "/api/widgets")
	if props["auth_roles"] != "Admin" {
		t.Fatalf("expected auth_roles=Admin, got props=%+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindRole || p.Literal != "Admin" {
		t.Fatalf("aspnet method roles: want role/Admin, got %+v", p)
	}
}

func TestStamp_Aspnet_ClassAuthorize(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;
using Microsoft.AspNetCore.Authorization;

[ApiController]
[Authorize]
[Route("/api/orders")]
public class OrdersController : ControllerBase
{
    [HttpGet]
    public IActionResult List() { return Ok(); }
}
`
	_, res := runDetect(t, "csharp", "OrdersController.cs", src)
	props := endpointWithProps(t, res, "GET", "/api/orders")
	if props["aspnet_class_authorize"] != "true" {
		t.Fatalf("expected aspnet_class_authorize=true, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindAuthenticated {
		t.Fatalf("aspnet class authorize: want authenticated, got %+v", p)
	}
}

func TestStamp_Aspnet_MethodAllowAnonymousOverride(t *testing.T) {
	src := `
using Microsoft.AspNetCore.Mvc;
using Microsoft.AspNetCore.Authorization;

[ApiController]
[Authorize]
[Route("/api/public")]
public class PublicController : ControllerBase
{
    [HttpGet]
    [AllowAnonymous]
    public IActionResult Ping() { return Ok(); }
}
`
	_, res := runDetect(t, "csharp", "PublicController.cs", src)
	props := endpointWithProps(t, res, "GET", "/api/public")
	if props["allow_anonymous"] != "true" {
		t.Fatalf("expected allow_anonymous=true, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindPublic {
		t.Fatalf("aspnet allow-anonymous override: want public, got %+v", p)
	}
}

// --- #4750 Spring: class @PreAuthorize + method @PreAuthorize + global ---

func TestStamp_Spring_ClassPreAuthorize(t *testing.T) {
	src := `
package demo;
import org.springframework.web.bind.annotation.*;
import org.springframework.security.access.prepost.PreAuthorize;

@RestController
@RequestMapping("/api/admin")
@PreAuthorize("hasRole('ADMIN')")
public class AdminController {
    @GetMapping("/users")
    public String users() { return ""; }
}
`
	_, res := runDetect(t, "java", "AdminController.java", src)
	props := endpointWithProps(t, res, "GET", "/api/admin/users")
	if props["spring_class_pre_authorize"] != "hasRole('ADMIN')" {
		t.Fatalf("expected spring_class_pre_authorize, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindRole || p.Literal != "ADMIN" {
		t.Fatalf("spring class @PreAuthorize: want role/ADMIN, got %+v", p)
	}
}

func TestStamp_Spring_MethodPreAuthorize(t *testing.T) {
	src := `
package demo;
import org.springframework.web.bind.annotation.*;
import org.springframework.security.access.prepost.PreAuthorize;

@RestController
@RequestMapping("/api/reports")
public class ReportController {
    @GetMapping("/export")
    @PreAuthorize("hasAuthority('reports:export')")
    public String export() { return ""; }
}
`
	_, res := runDetect(t, "java", "ReportController.java", src)
	props := endpointWithProps(t, res, "GET", "/api/reports/export")
	if props["auth_expression"] != "hasAuthority('reports:export')" {
		t.Fatalf("expected auth_expression, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindAction || p.Literal != "reports:export" {
		t.Fatalf("spring method @PreAuthorize: want action/reports:export, got %+v", p)
	}
}

func TestStamp_Spring_GlobalFilterChain(t *testing.T) {
	// Controller + a same-file SecurityFilterChain whose requestMatchers covers
	// the controller's /api/secure prefix → spring_global_authorization stamped.
	src := `
package demo;
import org.springframework.web.bind.annotation.*;
import org.springframework.context.annotation.Bean;
import org.springframework.security.web.SecurityFilterChain;

@RestController
@RequestMapping("/api/secure")
public class SecureController {
    @GetMapping("/data")
    public String data() { return ""; }

    @Bean
    public SecurityFilterChain chain(org.springframework.security.config.annotation.web.builders.HttpSecurity http) throws Exception {
        http.authorizeHttpRequests(a -> a.requestMatchers("/api/secure/**").authenticated());
        return http.build();
    }
}
`
	_, res := runDetect(t, "java", "SecureController.java", src)
	props := endpointWithProps(t, res, "GET", "/api/secure/data")
	if props["spring_global_authorization"] == "" {
		t.Fatalf("expected spring_global_authorization, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindAuthenticated {
		t.Fatalf("spring global filter-chain: want authenticated, got %+v", p)
	}
}

// --- #4752 Laravel: reconciled route + group auth middleware ---

func TestStamp_Laravel_RouteAuthMiddleware(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;
Route::get('/profile', 'ProfileController@show')->middleware('auth');
`
	_, res := runDetect(t, "php", "routes/web.php", src)
	props := endpointWithProps(t, res, "GET", "/profile")
	if props["auth_required"] != "true" {
		t.Fatalf("expected auth_required=true, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindAuthenticated {
		t.Fatalf("laravel route auth middleware: want authenticated, got %+v", p)
	}
}

func TestStamp_Laravel_GroupRoleMiddleware(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;
Route::group(['middleware' => ['auth', 'role:admin']], function () {
    Route::get('/admin/dashboard', 'AdminController@index');
});
`
	_, res := runDetect(t, "php", "routes/web.php", src)
	props := endpointWithProps(t, res, "GET", "/admin/dashboard")
	if props["auth_roles"] != "admin" {
		t.Fatalf("expected auth_roles=admin, got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindRole || p.Literal != "admin" {
		t.Fatalf("laravel group role middleware: want role/admin, got %+v", p)
	}
}

func TestStamp_Laravel_WithoutMiddlewareOverride(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;
Route::group(['middleware' => ['auth']], function () {
    Route::get('/health', 'HealthController@check')->withoutMiddleware('auth');
});
`
	_, res := runDetect(t, "php", "routes/web.php", src)
	props := endpointWithProps(t, res, "GET", "/health")
	if props["auth_required"] != "false" {
		t.Fatalf("expected auth_required=false (withoutMiddleware), got %+v", props)
	}
	if p := resolveEndpointProps(props); p.Kind != authposture.KindPublic {
		t.Fatalf("laravel withoutMiddleware override: want public, got %+v", p)
	}
}
