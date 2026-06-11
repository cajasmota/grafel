package mcp

// auth_posture_diff_harvest_test.go — LIVE-PATH harvest tests for #4734 + #4742..
// #4747. These assert that the framework auth-posture resolvers
// (Spring/Rails/Flask/Laravel/ASP.NET/Go/Phoenix) surface a NON-unknown posture
// when fed an endpoint shaped as the engine ACTUALLY produces it (the structured
// props the extractor stamps and/or a handler/controller/router SOURCE body) —
// i.e. the props flow through authPostureSignalProps / authPostureSourceProps in
// buildAuthSignal into the resolver, NOT only through the unit tests in
// internal/authposture that construct a Signal directly.
//
// The contract under test is the SIGNAL PLUMBING: before this change the harvest
// list dropped every framework-specific prop and hard-wired Signal.Source to the
// Django get_permissions body, so a clearly-guarded Spring/Rails/Flask/Laravel/
// ASP.NET/Go/Phoenix endpoint resolved to `unknown` in the live diff. The #4550
// JOIN keying is unchanged.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/authposture"
	"github.com/cajasmota/archigraph/internal/graph"
)

// resolvedV3Posture runs the full harvest → resolver path on a single v3
// endpoint entity (the same buildAuthSignal the live tool uses) and returns the
// resolved posture. It mirrors collectAuthEndpoints' per-endpoint harvest.
func resolvedV3Posture(t *testing.T, props map[string]string) authposture.Posture {
	t.Helper()
	e := endpointEntity("v1", "GET", "/x", props)
	sig := buildAuthSignal(&e)
	p, _ := authposture.NewRegistry().Resolve(sig)
	return p
}

// assertGuardedSurfaces asserts a clearly-guarded endpoint resolves to a known,
// non-public posture (the resolver SAW the auth signal through the harvest).
func assertGuardedSurfaces(t *testing.T, framework string, props map[string]string, wantKind authposture.Kind) {
	t.Helper()
	p := resolvedV3Posture(t, props)
	if p.Kind == authposture.KindUnknown {
		t.Fatalf("%s: guarded endpoint resolved to UNKNOWN through the harvest — "+
			"props did not reach the resolver: posture=%+v props=%+v", framework, p, props)
	}
	if wantKind != "" && p.Kind != wantKind {
		t.Fatalf("%s: posture kind=%s, want %s; posture=%+v", framework, p.Kind, wantKind, p)
	}
}

// --- Spring (#4734): method/class @PreAuthorize + global SecurityFilterChain ---

func TestHarvest_Spring_ClassPreAuthorize_Surfaces(t *testing.T) {
	// A controller with a class-level @PreAuthorize("hasRole('ADMIN')") and no
	// method override — the engine stamps spring_class_pre_authorize on the
	// endpoint. Before the harvest fix this key was dropped → unknown.
	assertGuardedSurfaces(t, "spring", map[string]string{
		"framework":                  "spring",
		"spring_class_pre_authorize": "hasRole('ADMIN')",
	}, authposture.KindRole)
}

func TestHarvest_Spring_GlobalFilterChain_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "spring", map[string]string{
		"framework":                   "spring",
		"spring_global_authorization": `requestMatchers("/api/**").authenticated()`,
	}, authposture.KindAuthenticated)
}

// --- Rails (#4742): reconciled before_action + Pundit literal ---

func TestHarvest_Rails_BeforeAction_Surfaces(t *testing.T) {
	// ruby/auth.go stamps auth_required/auth_guard for a before_action chain.
	assertGuardedSurfaces(t, "rails", map[string]string{
		"framework":     "rails",
		"auth_required": "true",
		"auth_guard":    "require_admin",
	}, authposture.KindRole)
}

func TestHarvest_Rails_PunditPolicy_Surfaces(t *testing.T) {
	// pundit_policy/pundit_action literals (harvested for the first time here).
	assertGuardedSurfaces(t, "rails", map[string]string{
		"framework":      "rails",
		"pundit_policy":  "PostPolicy",
		"pundit_action":  "update",
	}, authposture.KindAction)
}

func TestHarvest_Rails_ControllerSource_Surfaces(t *testing.T) {
	// No structured props — only a controller_source body. The per-framework
	// source fallback in buildAuthSignal must populate Signal.Source so the
	// resolver source-scans it.
	assertGuardedSurfaces(t, "rails", map[string]string{
		"framework": "rails",
		"controller_source": "class PostsController < ApplicationController\n" +
			"  before_action :authenticate_user!\n  def index; end\nend",
	}, authposture.KindAuthenticated)
}

// --- Flask (#4743): reconciled login_required + decorator/page ---

func TestHarvest_Flask_LoginRequired_Surfaces(t *testing.T) {
	// python/flask.go stamps framework=flask + auth_required=true for a
	// login_required view.
	assertGuardedSurfaces(t, "flask", map[string]string{
		"framework":     "flask",
		"auth_required": "true",
	}, authposture.KindAuthenticated)
}

func TestHarvest_Flask_RolesDecorator_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "flask", map[string]string{
		"framework":      "flask",
		"auth_decorator": "roles_required",
		"auth_roles":     "admin",
	}, authposture.KindRole)
}

// --- Laravel (#4744): route/group middleware ---

func TestHarvest_Laravel_AuthMiddleware_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "laravel", map[string]string{
		"framework":       "laravel",
		"auth_required":   "true",
		"auth_middleware": "auth",
	}, authposture.KindAuthenticated)
}

func TestHarvest_Laravel_RoleMiddleware_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "laravel", map[string]string{
		"framework":  "laravel",
		"middleware": "role:admin",
		"auth_roles": "admin",
	}, authposture.KindRole)
}

// --- ASP.NET Core (#4745): [Authorize]/policy/[AllowAnonymous] ---

func TestHarvest_Aspnet_Authorize_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "aspnet", map[string]string{
		"framework":     "aspnet",
		"auth_required": "true",
	}, authposture.KindAuthenticated)
}

func TestHarvest_Aspnet_ClassAuthorizeRoles_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "aspnet", map[string]string{
		"framework":               "aspnet",
		"aspnet_class_authorize":  "true",
		"aspnet_class_roles":      "Admin",
	}, authposture.KindRole)
}

func TestHarvest_Aspnet_AllowAnonymous_Surfaces(t *testing.T) {
	// AllowAnonymous is an explicit public marker — must resolve to public, not
	// unknown. Verifies allow_anonymous flows through the harvest.
	p := resolvedV3Posture(t, map[string]string{
		"framework":       "aspnet",
		"allow_anonymous": "true",
	})
	if p.Kind != authposture.KindPublic {
		t.Fatalf("aspnet AllowAnonymous: kind=%s, want public; posture=%+v", p.Kind, p)
	}
}

// --- Go HTTP middleware (#4746): reconciled middleware chain ---

func TestHarvest_Go_AuthMiddleware_Surfaces(t *testing.T) {
	// route_auth.go now stamps auth_middleware (the guard symbol) in addition to
	// auth_required; an admin guard must resolve to superuser, not bare
	// authenticated.
	assertGuardedSurfaces(t, "go", map[string]string{
		"framework":       "go",
		"auth_required":   "true",
		"auth_middleware": "RequireAdmin",
	}, authposture.KindSuperuser)
}

func TestHarvest_Go_AuthRequiredOnly_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "go", map[string]string{
		"framework":     "gin",
		"auth_required": "true",
	}, authposture.KindAuthenticated)
}

// --- Phoenix (#4747): pipeline/plug + router source fallback ---

func TestHarvest_Phoenix_AuthPipelines_Surfaces(t *testing.T) {
	assertGuardedSurfaces(t, "phoenix", map[string]string{
		"framework":      "phoenix",
		"auth_pipelines": "browser -> auth",
		"auth_plugs":     "plug Guardian.Plug.EnsureAuthenticated",
	}, authposture.KindAuthenticated)
}

func TestHarvest_Phoenix_RouterSource_Surfaces(t *testing.T) {
	// Only a router_source body — the per-framework source fallback must feed it
	// to the Phoenix resolver's router-source scan.
	assertGuardedSurfaces(t, "phoenix", map[string]string{
		"framework": "phoenix",
		"router_source": "pipeline :auth do\n  plug Guardian.Plug.EnsureAuthenticated\nend\n" +
			"scope \"/\" do\n  pipe_through [:browser, :auth]\n  get \"/x\", PageController, :index\nend",
	}, "")
}

// TestHarvest_AllFrameworkPropsReachResolver is a guard: every framework-specific
// prop key a resolver reads must be present in authPostureSignalProps, else it is
// silently dropped before the resolver sees it (the #4734/#4742-#4747 bug class).
func TestHarvest_AllFrameworkPropsReachResolver(t *testing.T) {
	must := []string{
		// Spring.
		"spring_class_pre_authorize", "spring_class_secured", "spring_class_roles_allowed",
		"spring_global_authorization", "auth_expression",
		// Rails.
		"pundit_policy", "pundit_action", "cancancan_ability",
		// Flask.
		"auth_decorator", "auth_page",
		// Laravel.
		"auth_middleware", "middleware", "auth_permissions",
		// ASP.NET.
		"auth_policy", "allow_anonymous", "aspnet_class_authorize", "aspnet_class_roles",
		"aspnet_class_policy", "aspnet_class_allow_anonymous", "aspnet_fallback_policy",
		// Phoenix.
		"auth_pipelines", "auth_plugs", "pipe_through", "plugs",
	}
	set := map[string]bool{}
	for _, k := range authPostureSignalProps {
		set[k] = true
	}
	for _, k := range must {
		if !set[k] {
			t.Errorf("authPostureSignalProps is missing framework prop %q — it will be "+
				"dropped before the resolver sees it", k)
		}
	}
}

// TestHarvest_SourceFallbackPopulatesSignal verifies buildAuthSignal fills
// Signal.Source from a per-framework source-body prop, not just the Django
// get_permissions key.
func TestHarvest_SourceFallbackPopulatesSignal(t *testing.T) {
	for _, k := range authPostureSourceProps {
		e := graph.Entity{Properties: map[string]string{"verb": "GET", "path": "/x", k: "BODY"}}
		sig := buildAuthSignal(&e)
		if sig.Source != "BODY" {
			t.Errorf("source prop %q did not populate Signal.Source (got %q)", k, sig.Source)
		}
	}
}
