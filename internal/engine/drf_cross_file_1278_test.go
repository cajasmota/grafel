// Tests for issue #1278: cross-file DRF router suppression.
//
// When router.register() calls live in one file and path("api/v1/",
// include(router.urls)) lives in another file, the per-file
// claimedRegisterNames set is empty during the register-file pass and bare
// YAML Route entities (and their ROUTES_TO edges) survive suppression,
// producing ghost http:ANY:/X endpoints with no /api/ prefix.
//
// The fix: a global cross-file registry (drfGlobalRegisterNames) is populated
// by ScanDRFRegisterNames before per-file extraction begins. The suppression
// gate in applyDjangoRouteComposition now also consults this global set.
//
// Additionally, findParentIncludePrefixes is extended to detect attribute-form
// include(routerVar.urls) in parent files so ApplyDjangoDRFRoutes correctly
// emits prefixed routes when the parent uses attribute-form include.
package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// ---------------------------------------------------------------------------
// Fixture A: register in file A, include in file B (cross-file)
// ---------------------------------------------------------------------------
//
// routers.py has only router.register() calls.
// urls.py has path("api/v1/", include(router.urls)).
// Without the fix: bare Route:users and Route:orders survive in routers.py and
// ApplyDjangoDRFRoutes emits ghost http:ANY:/users endpoints.
// With the fix: bare Route entities are suppressed; only /api/v1/* paths land.

const crossFileRoutersFile = `from rest_framework.routers import DefaultRouter
from .views import UserViewSet, OrderViewSet

router = DefaultRouter()
router.register(r'users', UserViewSet, basename='user')
router.register(r'orders', OrderViewSet, basename='order')
`

const crossFileURLsFile = `from django.urls import path, include
from .routers import router

urlpatterns = [
    path('api/v1/', include(router.urls)),
]
`

// TestDRFCrossFile_SuppressesOrphanRouteEntities verifies that when
// router.register() is in routers.py and include(router.urls) is in urls.py,
// bare Route entities ("users", "orders") are suppressed by the global
// register-name registry and do not appear in the Detect output.
//
// Regression test for #1278.
func TestDRFCrossFile_SuppressesOrphanRouteEntities(t *testing.T) {
	// Set up the global register-name registry as the pre-pass would.
	ClearDRFRegisterNames()
	ScanDRFRegisterNames([]byte(crossFileRoutersFile))
	ScanDRFRegisterNames([]byte(crossFileURLsFile))
	t.Cleanup(ClearDRFRegisterNames)

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	ctx := context.Background()

	// Detect routers.py — should suppress bare Route:users and Route:orders.
	routersResult, err := det.Detect(ctx, extractor.FileInput{
		Path:     "myapp/routers.py",
		Content:  []byte(crossFileRoutersFile),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect(routers.py): %v", err)
	}

	// Bare Route entities must be absent from routers.py output.
	orphanNames := map[string]bool{"users": true, "orders": true}
	for _, e := range routersResult.Entities {
		if e.Kind == "Route" && orphanNames[e.Name] {
			t.Errorf("orphan Route %q survived cross-file suppression in routers.py output", e.Name)
		}
	}
	// Bare ROUTES_TO edges must also be absent.
	for _, r := range routersResult.Relationships {
		if r.Kind == "ROUTES_TO" {
			bare := ""
			if len(r.FromID) > len("Route:") && r.FromID[:6] == "Route:" {
				bare = r.FromID[6:]
			}
			if orphanNames[bare] {
				t.Errorf("orphan ROUTES_TO from Route:%s survived cross-file suppression in routers.py", bare)
			}
		}
	}
}

// TestDRFCrossFile_SameFileCompositionUnaffected verifies that the existing
// same-file composition path (#64) still works correctly when the global
// register-name registry is populated (i.e., we don't over-suppress composed
// Route entities that have a real prefix).
//
// This is the regression guard for the #64 fix against the #1278 fix.
func TestDRFCrossFile_SameFileCompositionUnaffected(t *testing.T) {
	// Same-file content: both register() and include(router.urls) in one file.
	sameFile := `from rest_framework.routers import DefaultRouter
from django.urls import path, include
from myapp.views import UserViewSet, OrderViewSet

router = DefaultRouter()
router.register(r'users', UserViewSet)
router.register(r'orders', OrderViewSet)

urlpatterns = [
    path('api/v1/', include(router.urls)),
]
`
	// Populate global registry as the pre-pass would.
	ClearDRFRegisterNames()
	ScanDRFRegisterNames([]byte(sameFile))
	t.Cleanup(ClearDRFRegisterNames)

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	ctx := context.Background()

	result, err := det.Detect(ctx, extractor.FileInput{
		Path:     "myapp/urls.py",
		Content:  []byte(sameFile),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// Composed Route entities must be present.
	got := map[string]bool{}
	for _, e := range result.Entities {
		if e.Kind == "Route" {
			got[e.Name] = true
		}
	}
	wantComposed := []string{"/api/v1/users", "/api/v1/orders"}
	for _, p := range wantComposed {
		if !got[p] {
			t.Errorf("missing composed Route %q after global registry suppression (got %v)", p, got)
		}
	}
	// Bare names must NOT be present.
	for _, bare := range []string{"users", "orders", "api/v1/"} {
		if got[bare] {
			t.Errorf("orphan bare Route %q must not be present after same-file composition", bare)
		}
	}

	// Verify ROUTES_TO edges from composed routes exist.
	type rel struct{ from, to string }
	wantRels := map[rel]bool{
		{"Route:/api/v1/users", "View:UserViewSet"}:   false,
		{"Route:/api/v1/orders", "View:OrderViewSet"}: false,
	}
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		k := rel{r.FromID, r.ToID}
		if _, ok := wantRels[k]; ok {
			wantRels[k] = true
		}
	}
	for k, seen := range wantRels {
		if !seen {
			t.Errorf("missing composed ROUTES_TO %s -> %s", k.from, k.to)
		}
	}
}

// TestDRFCrossFile_BarePlainPathPreserved verifies that a bare path() in one
// file, with no router.register() anywhere, produces a Route entity that is
// NOT suppressed by the global registry (which would be empty).
//
// Fixture: a plain Django urls.py with non-DRF paths only.
func TestDRFCrossFile_BarePlainPathPreserved(t *testing.T) {
	plainURLs := `from django.urls import path
from myapp import views

urlpatterns = [
    path('about/', views.about),
    path('contact/', views.contact),
]
`
	// Reset global registry — no register() calls in the repo.
	ClearDRFRegisterNames()
	ScanDRFRegisterNames([]byte(plainURLs))
	t.Cleanup(ClearDRFRegisterNames)

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	ctx := context.Background()

	result, err := det.Detect(ctx, extractor.FileInput{
		Path:     "myapp/urls.py",
		Content:  []byte(plainURLs),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// Plain path-based routes must still be present (not suppressed).
	got := map[string]bool{}
	for _, e := range result.Entities {
		if e.Kind == "Route" {
			got[e.Name] = true
		}
	}
	for _, want := range []string{"about/", "contact/"} {
		if !got[want] {
			t.Errorf("plain Route %q was incorrectly suppressed (got %v)", want, got)
		}
	}
}

// TestDRFCrossFile_ApplyDRFRoutes_AttributeFormInclude verifies that
// ApplyDjangoDRFRoutes correctly emits prefixed routes when the parent file
// uses attribute-form include(router.urls) to mount a routers file.
//
// Fixture: client-fixture-X layout where routers.py is included via
// path("api/v1/", include(router.urls)) in urls.py (attribute form, not
// string form).
func TestDRFCrossFile_ApplyDRFRoutes_AttributeFormInclude(t *testing.T) {
	routersPy := `from rest_framework.routers import DefaultRouter
from .views import UserViewSet, OrderViewSet

router = DefaultRouter()
router.register(r'users', UserViewSet, basename='user')
router.register(r'orders', OrderViewSet, basename='order')
`
	urlsPy := `from django.urls import path, include
from .routers import router

urlpatterns = [
    path('api/v1/', include(router.urls)),
]
`
	files := []string{"myapp/routers.py", "myapp/urls.py"}
	contentMap := map[string][]byte{
		"myapp/routers.py": []byte(routersPy),
		"myapp/urls.py":    []byte(urlsPy),
	}
	reader := func(p string) []byte { return contentMap[p] }

	out := ApplyDjangoDRFRoutes(files, reader)

	// Collect all http_endpoint IDs.
	ids := map[string]bool{}
	for _, e := range out {
		ids[e.ID] = true
	}

	// Expect prefixed routes (from the cross-file attribute-form include heuristic).
	wantPrefixed := []string{
		"http:GET:/api/v1/users",
		"http:POST:/api/v1/users",
		"http:GET:/api/v1/users/{pk}",
	}
	for _, want := range wantPrefixed {
		if !ids[want] {
			t.Errorf("expected prefixed route %q to be emitted; got %v", want, ids)
		}
	}

	// Ghost bare-prefix routes must NOT be emitted.
	ghostBare := []string{"http:GET:/users", "http:GET:/users/{pk}", "http:POST:/users"}
	for _, ghost := range ghostBare {
		if ids[ghost] {
			t.Errorf("ghost bare-prefix route %q must not be emitted when parent uses attribute-form include", ghost)
		}
	}
}
