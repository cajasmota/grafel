package csharp_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Auth — auth_coverage
// ---------------------------------------------------------------------------

func TestAuthAuthorizeAttribute(t *testing.T) {
	src := `
[ApiController]
[Authorize]
public class SecureController : ControllerBase
{
    [HttpGet]
    public IActionResult Get() => Ok();
}
`
	ents := extract(t, "custom_csharp_auth", fi("SecureController.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth_coverage SCOPE.Pattern from [Authorize]")
	}
}

func TestAuthAuthorizeRoles(t *testing.T) {
	src := `[Authorize(Roles = "Admin,Manager")]`
	ents := extract(t, "custom_csharp_auth", fi("AdminController.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" && e.Name == "auth:Authorize:roles:Admin,Manager" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth:Authorize:roles:Admin,Manager entity")
	}
}

func TestAuthAuthorizePolicy(t *testing.T) {
	src := `[Authorize(Policy = "RequireAdminRole")]`
	ents := extract(t, "custom_csharp_auth", fi("Controller.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" && e.Name == "auth:Authorize:policy:RequireAdminRole" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth:Authorize:policy:RequireAdminRole entity")
	}
}

func TestAuthAuthorizePositional(t *testing.T) {
	src := `[Authorize("AdminOnly")]`
	ents := extract(t, "custom_csharp_auth", fi("Controller.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" && e.Name == "auth:Authorize:policy:AdminOnly" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth:Authorize:policy:AdminOnly from positional [Authorize(\"AdminOnly\")]")
	}
}

func TestAuthAllowAnonymous(t *testing.T) {
	src := `
[AllowAnonymous]
public IActionResult Login() => Ok();
`
	ents := extract(t, "custom_csharp_auth", fi("AuthController.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth_coverage entity from [AllowAnonymous]")
	}
}

func TestAuthRequireAuthorization(t *testing.T) {
	src := `
app.MapGet("/secure", () => "secret").RequireAuthorization();
app.MapPost("/admin", Handler).RequireAuthorization("AdminPolicy");
`
	ents := extract(t, "custom_csharp_auth", fi("Program.cs", "csharp", src))
	count := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 auth_coverage entities from RequireAuthorization, got %d", count)
	}
}

func TestAuthAddAuthorization(t *testing.T) {
	src := `
builder.Services.AddAuthorization(options =>
{
    options.AddPolicy("AdminOnly", policy => policy.RequireRole("Admin"));
    options.AddPolicy("PremiumUser", policy => policy.RequireClaim("subscription", "premium"));
});
`
	ents := extract(t, "custom_csharp_auth", fi("Program.cs", "csharp", src))
	addAuthFound := false
	policyFound := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "auth_coverage" {
			if e.Name == "auth:AddAuthorization:Program.cs" {
				addAuthFound = true
			}
			if e.Name == "auth:AddPolicy:AdminOnly" || e.Name == "auth:AddPolicy:PremiumUser" {
				policyFound++
			}
		}
	}
	if !addAuthFound {
		t.Error("expected auth:AddAuthorization:Program.cs entity")
	}
	if policyFound < 2 {
		t.Errorf("expected 2 AddPolicy entities, got %d", policyFound)
	}
}

func TestAuthNoMatch(t *testing.T) {
	src := `namespace App { class Helper { public void DoWork() {} } }`
	ents := extract(t, "custom_csharp_auth", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 auth entities, got %d", len(ents))
	}
}

func TestAuthWrongLanguageSkipped(t *testing.T) {
	src := `[Authorize]`
	ents := extract(t, "custom_csharp_auth", fi("file.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-csharp language, got %d", len(ents))
	}
}
