// auth_public_flag_test.go — #4595 regression for the AUTHORITATIVE
// "public by design" signal on AuthEndpointFinding.
//
// Before the fix the auth-coverage wire shape collapsed every guard-less route
// into has_auth=false with no evidence, so a route that is unauthenticated BY
// DESIGN (@Public / AllowAny / permitAll / @AllowAnonymous) was indistinguishable
// from a forgotten guard. resolveEndpointPublic surfaces WHY: an explicit public
// mechanism vs no guard at all. These tests feed the EXACT property shapes the
// engine auth resolvers stamp (stampAuthPolicy / grpcJavaPolicyProps /
// java_annotation_routes / django_drf_actions) across frameworks.
package dashboard

import "testing"

func TestResolveEndpointPublic_ExplicitPublicVerdict(t *testing.T) {
	// JS/TS @Public() / Spring permitAll / gRPC public interceptor / HotChocolate
	// @AllowAnonymous: the reconciled posture is auth_required=false with a named
	// mechanism. MUST resolve public, with the mechanism echoed as the reason.
	cases := []struct {
		name       string
		props      map[string]string
		wantReason string
	}{
		{
			name:       "nest @Public() decorator",
			props:      map[string]string{"auth_required": "false", "auth_method": "decorator", "auth_confidence": "high"},
			wantReason: "auth_method=decorator",
		},
		{
			name:       "spring permitAll annotation",
			props:      map[string]string{"auth_required": "false", "auth_method": "annotation", "auth_confidence": "high"},
			wantReason: "auth_method=annotation",
		},
		{
			name:       "config-driven public route, method known",
			props:      map[string]string{"auth_required": "false", "auth_method": "config"},
			wantReason: "auth_method=config",
		},
		{
			name:       "explicit public, method unknown -> still public via verdict",
			props:      map[string]string{"auth_required": "false", "auth_method": "unknown"},
			wantReason: "auth_required=false",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub, reason := resolveEndpointPublic(tc.props)
			if !pub {
				t.Fatalf("%s: want public=true (props=%v)", tc.name, tc.props)
			}
			if reason != tc.wantReason {
				t.Errorf("%s: reason=%q, want %q", tc.name, reason, tc.wantReason)
			}
		})
	}
}

func TestResolveEndpointPublic_DRFAllowAny(t *testing.T) {
	// DRF permission_classes=[AllowAny]: by DRF semantics the resolver does NOT
	// stamp auth_required, but records AllowAny in the middleware chain. MUST
	// still resolve public.
	props := map[string]string{"middleware_names": "AllowAny"}
	pub, reason := resolveEndpointPublic(props)
	if !pub {
		t.Fatalf("DRF AllowAny route not recognised as public (props=%v)", props)
	}
	if reason != "permission_class=AllowAny" {
		t.Errorf("reason=%q, want permission_class=AllowAny", reason)
	}
}

func TestResolveEndpointPublic_ForgottenGuardIsNotPublic(t *testing.T) {
	// A genuinely-unguarded endpoint: no auth_required=false, no public permission
	// symbol — just no posture. MUST NOT be flagged public (this is the forgotten
	// guard the dashboard should alarm on).
	cases := []map[string]string{
		{},                                  // bare route, no posture at all
		{"verb": "POST", "path": "/orders"}, // route metadata but no auth signal
		{"middleware_names": "ThrottleClass"}, // non-auth middleware only
	}
	for _, props := range cases {
		if pub, reason := resolveEndpointPublic(props); pub {
			t.Errorf("forgotten-guard route flagged public (reason=%q, props=%v)", reason, props)
		}
	}
}

func TestResolveEndpointPublic_AuthenticatedRouteIsNotPublic(t *testing.T) {
	// An authenticated route (auth_required=true) is never public, even if some
	// AllowAny symbol leaks into a chain entry.
	props := map[string]string{"auth_required": "true", "middleware_names": "IsAuthenticated,AllowAny"}
	if pub, reason := resolveEndpointPublic(props); pub {
		t.Errorf("authenticated route flagged public (reason=%q)", reason)
	}
}
