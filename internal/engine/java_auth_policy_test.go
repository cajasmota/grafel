// Tests for the Java auth_policy resolver (#1942 Phase 1).
//
// Coverage:
//   - Handler-level @PermitAll wins over Quarkus framework default.
//   - Class-level @Secured inherits when method has no auth annotation.
//   - Method-level @PermitAll overrides class-level @Secured.
//   - Quarkus quarkus-security extension → framework_default required, low.
//   - Quarkus application.properties permission policy → config-driven medium.
//   - Multi-role @RolesAllowed({"ADMIN","USER"}) preserves both roles.
//   - @PreAuthorize SpEL parsing.
//   - End-to-end ApplyJavaAnnotationRoutesWithContext emits auth_policy props.
//
// Fixture names use the client-fixture-X convention (no client-name leaks).
package engine

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func TestResolveJavaAuthPolicy_MethodPermitAll(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		"@POST\n@PermitAll\n@Path(\"/login\")",
		42,
		"@Path(\"/auth\")", "AuthController", 10,
		"client-fixture-x/AuthController.java",
		"/auth/login",
		JavaAuthContext{QuarkusSecurityEnabled: true, QuarkusSecurityFile: "pom.xml"},
	)
	if policy.Required {
		t.Errorf("expected required=false for @PermitAll, got true")
	}
	if policy.Method != "annotation" {
		t.Errorf("expected method=annotation, got %q", policy.Method)
	}
	if policy.Confidence != "high" {
		t.Errorf("expected confidence=high, got %q", policy.Confidence)
	}
	if len(policy.SourceChain) != 1 || policy.SourceChain[0].Text != "@PermitAll" {
		t.Errorf("expected @PermitAll signal, got %#v", policy.SourceChain)
	}
	if policy.SourceChain[0].Line != 42 {
		t.Errorf("expected method line 42 in source chain, got %d", policy.SourceChain[0].Line)
	}
}

func TestResolveJavaAuthPolicy_ClassLevelSecuredInherits(t *testing.T) {
	// Method has no auth annotation; class has @Secured("ROLE_ADMIN"). The
	// resolver must inherit from the class and report confidence=high.
	policy := ResolveJavaAuthPolicy(
		"@GET\n@Path(\"/admin/users\")",
		55,
		"@Secured(\"ROLE_ADMIN\")\n@Path(\"/admin\")", "AdminController", 12,
		"client-fixture-x/AdminController.java",
		"/admin/users",
		JavaAuthContext{},
	)
	if !policy.Required {
		t.Fatalf("expected required=true via class-level @Secured")
	}
	if len(policy.Roles) != 1 || policy.Roles[0] != "ADMIN" {
		t.Errorf("expected roles=[ADMIN] (ROLE_ prefix stripped), got %v", policy.Roles)
	}
	if policy.Confidence != "high" {
		t.Errorf("expected confidence=high, got %q", policy.Confidence)
	}
	if len(policy.SourceChain) == 0 || policy.SourceChain[0].Line != 12 {
		t.Errorf("expected class-line 12 in source chain, got %#v", policy.SourceChain)
	}
	if policy.SourceChain[0].EntityID != "AdminController" {
		t.Errorf("expected class-level signal entity_id=AdminController, got %q", policy.SourceChain[0].EntityID)
	}
}

func TestResolveJavaAuthPolicy_MethodOverridesClass(t *testing.T) {
	// Class has @Secured; method has @PermitAll. The method wins.
	policy := ResolveJavaAuthPolicy(
		"@GET\n@PermitAll\n@Path(\"/health\")",
		28,
		"@Secured(\"ROLE_ADMIN\")", "AdminController", 12,
		"client-fixture-x/AdminController.java",
		"/admin/health",
		JavaAuthContext{},
	)
	if policy.Required {
		t.Errorf("expected required=false (method @PermitAll wins)")
	}
	if policy.SourceChain[0].Text != "@PermitAll" {
		t.Errorf("expected source chain text=@PermitAll, got %q", policy.SourceChain[0].Text)
	}
	if policy.SourceChain[0].Line != 28 {
		t.Errorf("expected method line 28, got %d", policy.SourceChain[0].Line)
	}
}

func TestResolveJavaAuthPolicy_QuarkusFrameworkDefault(t *testing.T) {
	// No annotations at all + quarkus-security on the classpath → framework
	// default required at LOW confidence.
	policy := ResolveJavaAuthPolicy(
		"@GET\n@Path(\"/ping\")",
		7,
		"", "PingResource", 4,
		"client-fixture-x/PingResource.java",
		"/ping",
		JavaAuthContext{QuarkusSecurityEnabled: true, QuarkusSecurityFile: "pom.xml"},
	)
	if !policy.Required {
		t.Fatalf("expected required=true via Quarkus framework default")
	}
	if policy.Method != "framework_default" {
		t.Errorf("expected method=framework_default, got %q", policy.Method)
	}
	if policy.Confidence != "low" {
		t.Errorf("expected confidence=low, got %q", policy.Confidence)
	}
	if len(policy.SourceChain) != 1 || policy.SourceChain[0].Kind != "framework_default" {
		t.Errorf("expected framework_default signal, got %#v", policy.SourceChain)
	}
	if policy.SourceChain[0].File != "pom.xml" {
		t.Errorf("expected file=pom.xml in source chain, got %q", policy.SourceChain[0].File)
	}
}

func TestResolveJavaAuthPolicy_QuarkusConfigDriven(t *testing.T) {
	// Quarkus permission block declares /api/admin/* requires authenticated.
	// Endpoint /api/admin/users has no annotation but matches the pattern.
	policy := ResolveJavaAuthPolicy(
		"@GET",
		20,
		"@Path(\"/api/admin\")", "AdminResource", 8,
		"client-fixture-x/AdminResource.java",
		"/api/admin/users",
		JavaAuthContext{
			QuarkusSecurityEnabled: true,
			QuarkusSecurityFile:    "pom.xml",
			QuarkusPermissions: []QuarkusPermission{{
				Name:   "admin",
				Paths:  []string{"/api/admin/*"},
				Policy: "authenticated",
				File:   "src/main/resources/application.properties",
				Line:   3,
			}},
		},
	)
	if !policy.Required {
		t.Fatalf("expected required=true via config-driven permission")
	}
	if policy.Method != "config" {
		t.Errorf("expected method=config, got %q", policy.Method)
	}
	if policy.Confidence != "medium" {
		t.Errorf("expected confidence=medium, got %q", policy.Confidence)
	}
	if len(policy.SourceChain) != 1 || policy.SourceChain[0].Kind != "config" {
		t.Errorf("expected config signal, got %#v", policy.SourceChain)
	}
	if policy.SourceChain[0].Line != 3 {
		t.Errorf("expected config signal line 3, got %d", policy.SourceChain[0].Line)
	}
}

func TestResolveJavaAuthPolicy_MultiRoleRolesAllowed(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		`@GET
@RolesAllowed({"ADMIN", "USER"})
@Path("/multi")`,
		30,
		"", "MultiResource", 5,
		"client-fixture-x/MultiResource.java",
		"/multi",
		JavaAuthContext{},
	)
	if !policy.Required {
		t.Fatal("expected required=true for @RolesAllowed")
	}
	sort.Strings(policy.Roles)
	if strings.Join(policy.Roles, ",") != "ADMIN,USER" {
		t.Errorf("expected roles=[ADMIN,USER], got %v", policy.Roles)
	}
}

func TestResolveJavaAuthPolicy_PreAuthorize(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		`@GetMapping("/audit")
@PreAuthorize("hasAnyRole('ADMIN','AUDITOR')")`,
		45,
		"", "AuditController", 9,
		"client-fixture-x/AuditController.java",
		"/audit",
		JavaAuthContext{},
	)
	if !policy.Required {
		t.Fatal("expected required=true for @PreAuthorize")
	}
	sort.Strings(policy.Roles)
	if strings.Join(policy.Roles, ",") != "ADMIN,AUDITOR" {
		t.Errorf("expected roles=[ADMIN,AUDITOR], got %v", policy.Roles)
	}
}

// @PreAuthorize("hasAuthority('user:delete')") must capture the fine-grained
// permission on auth_permissions, NOT conflate it into auth_roles.
func TestResolveJavaAuthPolicy_PreAuthorizeAuthorityPermission(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		`@DeleteMapping("/users/{id}")
@PreAuthorize("hasAuthority('user:delete')")`,
		51,
		"", "UserController", 9,
		"client-fixture-x/UserController.java",
		"/users/{id}",
		JavaAuthContext{},
	)
	if !policy.Required {
		t.Fatal("expected required=true for @PreAuthorize hasAuthority")
	}
	if strings.Join(policy.Permissions, ",") != "user:delete" {
		t.Errorf("expected permissions=[user:delete], got %v", policy.Permissions)
	}
	if len(policy.Roles) != 0 {
		t.Errorf("expected no roles for a hasAuthority permission, got %v", policy.Roles)
	}
}

// @PreAuthorize("hasAuthority('SCOPE_read')") is an OAuth scope, not a role or
// a bare permission — the SCOPE_ prefix must route it to auth_scopes.
func TestResolveJavaAuthPolicy_PreAuthorizeScope(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		`@GetMapping("/reports")
@PreAuthorize("hasAuthority('SCOPE_reports:read')")`,
		60,
		"", "ReportController", 9,
		"client-fixture-x/ReportController.java",
		"/reports",
		JavaAuthContext{},
	)
	if strings.Join(policy.Scopes, ",") != "reports:read" {
		t.Errorf("expected scopes=[reports:read], got %v", policy.Scopes)
	}
	if len(policy.Roles) != 0 || len(policy.Permissions) != 0 {
		t.Errorf("expected no roles/permissions for a SCOPE_ authority, got roles=%v perms=%v", policy.Roles, policy.Permissions)
	}
}

// @PreAuthorize("hasPermission(#id, 'Order', 'delete')") — the trailing literal
// is the permission name; the dynamic target #id must not become a permission.
func TestResolveJavaAuthPolicy_PreAuthorizeHasPermission(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		`@DeleteMapping("/orders/{id}")
@PreAuthorize("hasPermission(#id, 'Order', 'delete')")`,
		70,
		"", "OrderController", 9,
		"client-fixture-x/OrderController.java",
		"/orders/{id}",
		JavaAuthContext{},
	)
	if strings.Join(policy.Permissions, ",") != "delete" {
		t.Errorf("expected permissions=[delete], got %v", policy.Permissions)
	}
}

// Negative: @PreAuthorize("hasRole(roleVar)") with a non-literal role argument
// must NOT fabricate a role/permission.
func TestResolveJavaAuthPolicy_PreAuthorizeDynamicRoleNoFabrication(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		`@GetMapping("/x")
@PreAuthorize("hasRole(roleVar)")`,
		80,
		"", "DynController", 9,
		"client-fixture-x/DynController.java",
		"/x",
		JavaAuthContext{},
	)
	if !policy.Required {
		t.Fatal("expected required=true (the annotation is present)")
	}
	if len(policy.Roles) != 0 || len(policy.Permissions) != 0 || len(policy.Scopes) != 0 {
		t.Errorf("expected no fabricated tokens for a dynamic role, got roles=%v perms=%v scopes=%v",
			policy.Roles, policy.Permissions, policy.Scopes)
	}
}

func TestResolveJavaAuthPolicy_DenyAll(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		"@GET\n@DenyAll",
		18,
		"", "Locked", 4, "client-fixture-x/Locked.java", "/locked",
		JavaAuthContext{},
	)
	if !policy.Required {
		t.Errorf("expected required=true for @DenyAll")
	}
	if policy.SourceChain[0].Text != "@DenyAll" {
		t.Errorf("expected source chain @DenyAll, got %q", policy.SourceChain[0].Text)
	}
}

func TestResolveJavaAuthPolicy_UnknownWhenNoSignals(t *testing.T) {
	policy := ResolveJavaAuthPolicy(
		"@GET", 5, "", "Bare", 2, "client-fixture-x/Bare.java", "/bare",
		JavaAuthContext{},
	)
	if policy.Method != "unknown" {
		t.Errorf("expected method=unknown, got %q", policy.Method)
	}
	if policy.Required {
		t.Errorf("expected required=false for unknown policy")
	}
}

func TestParseQuarkusPermissions(t *testing.T) {
	content := `# Quarkus permissions
quarkus.http.auth.permission.admin.paths=/api/admin/*
quarkus.http.auth.permission.admin.policy=authenticated
quarkus.http.auth.permission.admin.roles-allowed=ADMIN,SUPER

quarkus.http.auth.permission.public.paths=/health,/metrics
quarkus.http.auth.permission.public.policy=permit
`
	perms := ParseQuarkusPermissions(content, "application.properties")
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(perms))
	}
	byName := map[string]QuarkusPermission{}
	for _, p := range perms {
		byName[p.Name] = p
	}
	admin := byName["admin"]
	if strings.Join(admin.Paths, "|") != "/api/admin/*" {
		t.Errorf("admin paths = %v", admin.Paths)
	}
	if admin.Policy != "authenticated" {
		t.Errorf("admin policy = %q", admin.Policy)
	}
	if strings.Join(admin.RolesAllowed, ",") != "ADMIN,SUPER" {
		t.Errorf("admin roles = %v", admin.RolesAllowed)
	}
	pub := byName["public"]
	if pub.Policy != "permit" {
		t.Errorf("public policy = %q", pub.Policy)
	}
	if strings.Join(pub.Paths, "|") != "/health|/metrics" {
		t.Errorf("public paths = %v", pub.Paths)
	}
}

func TestDetectQuarkusSecurityExtension(t *testing.T) {
	bd := map[string]string{
		"client-fixture-x/pom.xml": `<project>
  <dependencies>
    <dependency>
      <groupId>io.quarkus</groupId>
      <artifactId>quarkus-smallrye-jwt</artifactId>
    </dependency>
  </dependencies>
</project>`,
	}
	ok, file := DetectQuarkusSecurityExtension(bd)
	if !ok {
		t.Fatal("expected quarkus-smallrye-jwt to be detected")
	}
	if file != "client-fixture-x/pom.xml" {
		t.Errorf("expected file=client-fixture-x/pom.xml, got %q", file)
	}

	none := map[string]string{
		"client-fixture-x/pom.xml": "<project><dependencies></dependencies></project>",
	}
	if ok, _ := DetectQuarkusSecurityExtension(none); ok {
		t.Error("expected no detection on empty pom.xml")
	}
}

func TestEncodeDecodeAuthPolicy(t *testing.T) {
	in := AuthPolicy{
		Required:   true,
		Method:     "annotation",
		Roles:      []string{"USER", "ADMIN"}, // unsorted to verify EncodeAuthPolicy sorts
		Confidence: "high",
		SourceChain: []AuthSignal{{
			Kind: "annotation", Text: "@RolesAllowed({\"ADMIN\",\"USER\"})", File: "X.java", Line: 42,
		}},
	}
	enc := EncodeAuthPolicy(in)
	if enc == "" {
		t.Fatal("EncodeAuthPolicy returned empty string")
	}
	dec := DecodeAuthPolicy(enc)
	if !dec.Required || dec.Method != "annotation" || dec.Confidence != "high" {
		t.Errorf("roundtrip mismatch: %#v", dec)
	}
	if strings.Join(dec.Roles, ",") != "ADMIN,USER" {
		t.Errorf("expected sorted roles, got %v", dec.Roles)
	}
	// Source chain preserved.
	if len(dec.SourceChain) != 1 || dec.SourceChain[0].File != "X.java" {
		t.Errorf("source chain lost in roundtrip: %#v", dec.SourceChain)
	}
}

func TestDecodeAuthPolicy_EmptyReturnsUnknown(t *testing.T) {
	p := DecodeAuthPolicy("")
	if p.Method != "unknown" {
		t.Errorf("expected unknown, got %q", p.Method)
	}
}

// ApplyJavaAnnotationRoutesWithContext end-to-end: emits a synthetic
// http_endpoint that carries the resolved auth_policy in its properties.
func TestApplyJavaAnnotationRoutesWithContext_AuthPolicyEmitted(t *testing.T) {
	src := `package com.example;
import jakarta.ws.rs.*;
import jakarta.annotation.security.*;

@Path("/auth")
public class AuthController {
    @POST
    @PermitAll
    @Path("/login")
    public Object login() { return null; }

    @GET
    @Path("/me")
    public Object me() { return null; }
}
`
	files := map[string]string{"client-fixture-x/AuthController.java": src}
	authCtx := JavaAuthContext{
		QuarkusSecurityEnabled: true,
		QuarkusSecurityFile:    "client-fixture-x/pom.xml",
	}
	got := ApplyJavaAnnotationRoutesWithContext(
		[]string{"client-fixture-x/AuthController.java"},
		mapReader(files), authCtx,
	)
	if len(got) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(got))
	}
	byID := map[string]map[string]string{}
	for _, e := range got {
		byID[e.ID] = e.Properties
	}
	login := byID["http:POST:/auth/login"]
	if login == nil {
		ids := make([]string, 0, len(byID))
		for k := range byID {
			ids = append(ids, k)
		}
		sort.Strings(ids)
		t.Fatalf("missing POST /auth/login; got IDs: %v", ids)
	}
	if login["auth_method"] != "annotation" || login["auth_required"] != "false" || login["auth_confidence"] != "high" {
		t.Errorf("login auth props = %v", login)
	}
	// auth_policy JSON should round-trip.
	var p AuthPolicy
	if err := json.Unmarshal([]byte(login["auth_policy"]), &p); err != nil {
		t.Fatalf("auth_policy JSON invalid: %v", err)
	}
	if len(p.SourceChain) != 1 || p.SourceChain[0].Text != "@PermitAll" {
		t.Errorf("login source chain = %#v", p.SourceChain)
	}

	me := byID["http:GET:/auth/me"]
	if me == nil {
		t.Fatalf("missing GET /auth/me")
	}
	if me["auth_method"] != "framework_default" || me["auth_required"] != "true" || me["auth_confidence"] != "low" {
		t.Errorf("me auth props = %v", me)
	}
}
