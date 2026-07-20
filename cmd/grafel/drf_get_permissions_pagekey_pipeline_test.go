package main

import (
	"testing"
)

// TestDRFGetPermissionsPageKey_PipelineStampsAuthPermissions is the deploy-9
// item-3 full-pipeline guard for the DRF get_permissions() per-action page-key.
//
// # The bug
//
// A DRF ViewSet whose get_permissions() branches on self.action and returns
// CustomPagePermissionCheck(PERMISSION_PAGES["<KEY>"]) guards PER ACTION
// resolves the page key correctly inside the engine pass
// (django_drf_get_permissions.go: parseDRFActionPermissions /
// mergeGetPermissionsBranches), and the engine-level ApplyDjangoDRFRoutes test
// (internal/engine/django_drf_get_permissions_test.go) already proves the
// resolved key reaches the endpoint posture's permissionPages. BUT through the
// FULL index pipeline (cmd/grafel Indexer.Run — the path the daemon runs on
// rebuild) the synthesized per-action endpoint did NOT carry `auth_permissions`:
// inspect(get_inspection_types, fields=[auth_permissions]) came back empty even
// though get_source resolved the page key. This is the #2670-class meta-bug
// (engine unit test green, real build drops the property somewhere downstream).
//
// # The fixture
//
// testdata/drf_get_permissions_pagekey_fixture mirrors the real
// acme_core/core/views/jurisdiction_viewset.py shape: a router-registered
// JurisdictionViewSet whose get_permissions() resolves
//   - get_inspection_types → PERMISSION_PAGES["JURISDICTIONS"]
//   - email                → PERMISSION_PAGES["EMAIL_TEMPLATES"]
//
// via the assign-then-return-comprehension idiom.
//
// # The assertions
//
// Endpoint side (the #3978 stamping — already green, kept as a regression guard):
//
//  1. The synthesized endpoint for GET /jurisdictions/inspection_types carries
//     auth_permissions=JURISDICTIONS (the resolved page key).
//  2. The synthesized endpoint for the `email` @action carries
//     auth_permissions=EMAIL_TEMPLATES — a DISTINCT key, proving no bleed.
//  3. A third action with no page-key guard
//     (at_least_one_jurisdiction_has_maintenance_evaluation → CustomActionPermissionCheck)
//     carries NO auth_permissions (honest-partial).
//
// Handler-Operation side (THE deploy-9 item-3 fix — fails before, passes after):
//
//  4. The action method Operation `JurisdictionViewSet.get_inspection_types`
//     (the symbol grafel_inspect / get_source resolve) carries
//     auth_permissions=JURISDICTIONS — propagated from its endpoint.
//  5. The `JurisdictionViewSet.email` Operation carries
//     auth_permissions=EMAIL_TEMPLATES — distinct, no bleed across handlers.
//  6. The no-page-key action's Operation carries NO auth_permissions.
func TestDRFGetPermissionsPageKey_PipelineStampsAuthPermissions(t *testing.T) {
	doc := runIndexerOn(t, "testdata/drf_get_permissions_pagekey_fixture", "drf_get_permissions_pagekey_fixture", nil)

	type endpoint struct {
		path       string
		verb       string
		authPerms  string
		sourceFile string
	}
	var got []endpoint
	for _, e := range doc.Entities {
		if e.Kind != "http_endpoint" && e.Kind != "http_endpoint_definition" {
			continue
		}
		got = append(got, endpoint{
			path:       e.PropGet("path"),
			verb:       e.PropGet("verb"),
			authPerms:  e.PropGet("auth_permissions"),
			sourceFile: e.SourceFile,
		})
	}
	if len(got) == 0 {
		t.Fatalf("no http_endpoint entities emitted by indexer")
	}

	find := func(verb, path string) *endpoint {
		for i := range got {
			if got[i].verb == verb && got[i].path == path {
				return &got[i]
			}
		}
		return nil
	}

	// ---- Endpoint side (regression guard for #3978) ----

	// (1) inspection_types → JURISDICTIONS.
	insp := find("GET", "/jurisdictions/inspection_types")
	if insp == nil {
		t.Fatalf("missing GET /jurisdictions/inspection_types — got endpoints: %+v", got)
	}
	if insp.authPerms != "JURISDICTIONS" {
		t.Errorf("GET /jurisdictions/inspection_types auth_permissions=%q; want %q",
			insp.authPerms, "JURISDICTIONS")
	}

	// (2) email → EMAIL_TEMPLATES (distinct key, no bleed). The `email` @action
	// has detail=True so the route carries the lookup placeholder; assert on the
	// page key regardless of verb (the action declares patch+put).
	var emailEP *endpoint
	for i := range got {
		if got[i].path == "/jurisdictions/{pk}/email" {
			emailEP = &got[i]
			break
		}
	}
	if emailEP == nil {
		t.Fatalf("missing /jurisdictions/{pk}/email — got endpoints: %+v", got)
	}
	if emailEP.authPerms != "EMAIL_TEMPLATES" {
		t.Errorf("/jurisdictions/{pk}/email auth_permissions=%q; want %q (distinct from inspection_types — no bleed)",
			emailEP.authPerms, "EMAIL_TEMPLATES")
	}

	// (3) the no-page-key action carries NO auth_permissions (honest-partial).
	noKey := find("GET", "/jurisdictions/at_least_one_jurisdiction_has_maintenance_evaluation")
	if noKey == nil {
		t.Fatalf("missing GET /jurisdictions/at_least_one_jurisdiction_has_maintenance_evaluation — got endpoints: %+v", got)
	}
	if noKey.authPerms != "" {
		t.Errorf("GET /jurisdictions/at_least_one_jurisdiction_has_maintenance_evaluation auth_permissions=%q; want empty (CustomActionPermissionCheck has no page key)",
			noKey.authPerms)
	}

	// ---- Handler-Operation side (THE deploy-9 item-3 fix) ----
	//
	// grafel_inspect / get_source resolve the @action by its method symbol
	// (a SCOPE.Operation named "<ViewSet>.<method>"), NOT the http_endpoint. The
	// page key MUST reach that Operation or auth_coverage starting from the
	// handler shows nothing. The views.py JurisdictionViewSet duplicates the same
	// method names; assert on the views.py fixture file to avoid cross-fixture
	// ambiguity (both files resolve identically — page key per action is stable).
	opAuthPerms := func(method string) (string, bool) {
		want := "JurisdictionViewSet." + method
		for _, e := range doc.Entities {
			if e.Kind != "SCOPE.Operation" || e.Name != want {
				continue
			}
			if e.SourceFile == "" || filepathSuffix(e.SourceFile) != "views.py" {
				continue
			}
			return e.PropGet("auth_permissions"), true
		}
		return "", false
	}

	// (4) get_inspection_types Operation → JURISDICTIONS (the core bug).
	if ap, ok := opAuthPerms("get_inspection_types"); !ok {
		t.Fatalf("missing SCOPE.Operation JurisdictionViewSet.get_inspection_types in views.py")
	} else if ap != "JURISDICTIONS" {
		t.Errorf("Operation get_inspection_types auth_permissions=%q; want %q "+
			"(page key resolved onto endpoint but NOT propagated to the handler symbol — deploy-9 item-3)",
			ap, "JURISDICTIONS")
	}

	// (5) email Operation → EMAIL_TEMPLATES (distinct — no bleed across handlers).
	if ap, ok := opAuthPerms("email"); !ok {
		t.Fatalf("missing SCOPE.Operation JurisdictionViewSet.email in views.py")
	} else if ap != "EMAIL_TEMPLATES" {
		t.Errorf("Operation email auth_permissions=%q; want %q (distinct from inspection_types — no bleed)",
			ap, "EMAIL_TEMPLATES")
	}

	// (6) no-page-key action Operation → empty (honest-partial preserved).
	if ap, ok := opAuthPerms("at_least_one_jurisdiction_has_maintenance_evaluation"); !ok {
		t.Fatalf("missing SCOPE.Operation JurisdictionViewSet.at_least_one_jurisdiction_has_maintenance_evaluation in views.py")
	} else if ap != "" {
		t.Errorf("Operation at_least_one_jurisdiction_has_maintenance_evaluation auth_permissions=%q; want empty",
			ap)
	}
}

// filepathSuffix returns the final path component of p (the basename), using
// both separators so the test is OS-agnostic without importing path/filepath
// for a single basename.
func filepathSuffix(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
