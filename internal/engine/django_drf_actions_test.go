package engine

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// idsFromRecords returns the slice of entity IDs from a record slice.
func idsFromRecords(records []types.EntityRecord) []string {
	out := make([]string, 0, len(records))
	for _, e := range records {
		out = append(out, e.ID)
	}
	return out
}

func assertHasAllIDs(t *testing.T, records []types.EntityRecord, want []string) {
	t.Helper()
	got := idsFromRecords(records)
	gotSet := make(map[string]bool, len(got))
	for _, id := range got {
		gotSet[id] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("missing expected id %q; got: %v", w, got)
		}
	}
}

func assertHasNoneIDs(t *testing.T, records []types.EntityRecord, unwanted []string) {
	t.Helper()
	got := idsFromRecords(records)
	gotSet := make(map[string]bool, len(got))
	for _, id := range got {
		gotSet[id] = true
	}
	for _, w := range unwanted {
		if gotSet[w] {
			t.Errorf("unexpected id %q present; got: %v", w, got)
		}
	}
}

// TestApplyDjangoDRFRoutes_ModelViewSetEmitsFullCRUD verifies that a
// router.register(prefix, FooViewSet) where FooViewSet inherits ModelViewSet
// emits all six standard endpoints (list, create, retrieve, update,
// partial_update, destroy).
func TestApplyDjangoDRFRoutes_ModelViewSetEmitsFullCRUD(t *testing.T) {
	files := fileMap{
		"myproject/urls.py": `
from django.urls import path, include
urlpatterns = [
    path("api/v1/", include("core.routers")),
]
`,
		"core/routers.py": `
from rest_framework import routers
from core.views import ContractViewSet

router = routers.DefaultRouter()
router.register(r"contracts", ContractViewSet)

urlpatterns = [
    path("", include(router.urls)),
]
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet

class ContractViewSet(ModelViewSet):
    queryset = None
    serializer_class = None
`,
	}

	pyPaths := []string{"myproject/urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	wantIDs := []string{
		"http:GET:/api/v1/contracts",
		"http:POST:/api/v1/contracts",
		"http:GET:/api/v1/contracts/{pk}",
		"http:PUT:/api/v1/contracts/{pk}",
		"http:PATCH:/api/v1/contracts/{pk}",
		"http:DELETE:/api/v1/contracts/{pk}",
	}
	assertHasAllIDs(t, got, wantIDs)
	// Fix #1692 — per-verb detail routes must be emitted; the ANY catch-all
	// must NOT appear alongside them (it was redundant and polluted the index).
	assertHasNoneIDs(t, got, []string{"http:ANY:/api/v1/contracts/{pk}"})
}

// TestApplyDjangoDRFRoutes_ReadOnlyModelViewSet verifies that a
// ReadOnlyModelViewSet emits only the list + retrieve endpoints.
func TestApplyDjangoDRFRoutes_ReadOnlyModelViewSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ReadOnlyVS

router = routers.DefaultRouter()
router.register(r"items", ReadOnlyVS)
`,
		"views.py": `
from rest_framework.viewsets import ReadOnlyModelViewSet

class ReadOnlyVS(ReadOnlyModelViewSet):
    pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	assertHasAllIDs(t, got, []string{
		"http:GET:/items",
		"http:GET:/items/{pk}",
	})
	assertHasNoneIDs(t, got, []string{
		"http:POST:/items",
		"http:DELETE:/items/{pk}",
	})
}

// TestApplyDjangoDRFRoutes_DetailActionPost verifies that
// @action(detail=True, methods=["post"], url_path="cancel") emits
// POST /<prefix>/{pk}/cancel.
func TestApplyDjangoDRFRoutes_DetailActionPost(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ContractViewSet

router = routers.DefaultRouter()
router.register(r"contracts", ContractViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ContractViewSet(ModelViewSet):
    @action(detail=True, methods=["post"], url_path="cancel")
    def cancel(self, request, pk=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{"http:POST:/contracts/{pk}/cancel"})
}

// TestApplyDjangoDRFRoutes_CollectionActionDefaultGet verifies that
// @action(detail=False) (no methods kwarg) defaults to GET and uses the
// VERBATIM method name as the URL path (DRF keeps underscores). #5298.
func TestApplyDjangoDRFRoutes_CollectionActionDefaultGet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ContractViewSet

router = routers.DefaultRouter()
router.register(r"contracts", ContractViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ContractViewSet(ModelViewSet):
    @action(detail=False)
    def get_extras(self, request):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	// DRF default url_path for a method `get_extras` is the verbatim
	// `get_extras` (underscores preserved), not `get-extras` (#5298).
	assertHasAllIDs(t, got, []string{"http:GET:/contracts/get_extras"})
	assertHasNoneIDs(t, got, []string{"http:GET:/contracts/get-extras"})
}

// TestApplyDjangoDRFRoutes_ActionMultipleMethods verifies that an action
// with methods=["get", "put"] emits both endpoints.
func TestApplyDjangoDRFRoutes_ActionMultipleMethods(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ContractViewSet

router = routers.DefaultRouter()
router.register(r"contracts", ContractViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ContractViewSet(ModelViewSet):
    @action(detail=True, methods=["get", "put"], url_path="assigned_contacts")
    def assigned_contacts(self, request, pk=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{
		"http:GET:/contracts/{pk}/assigned_contacts",
		"http:PUT:/contracts/{pk}/assigned_contacts",
	})
}

// TestApplyDjangoDRFRoutes_ActionNestedURLPathParams verifies #5230: an
// @action(url_path='contacts/(?P<contact_id>[^/.]+)/remove') emits the route at
// the VERBATIM url_path value (not the method name), with the nested
// `(?P<contact_id>…)` capture group surfaced as a `{contact_id}` path param.
func TestApplyDjangoDRFRoutes_ActionNestedURLPathParams(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import BranchViewSet

router = routers.DefaultRouter()
router.register(r"branches", BranchViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class BranchViewSet(ModelViewSet):
    @action(detail=True, methods=["delete"], url_path='contacts/(?P<contact_id>[^/.]+)/remove')
    def remove_contact(self, request, pk=None, contact_id=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	// url_path used verbatim; (?P<contact_id>…) becomes {contact_id}; the method
	// name `remove_contact` must NOT leak into the URL.
	assertHasAllIDs(t, got, []string{
		"http:DELETE:/branches/{pk}/contacts/{contact_id}/remove",
	})
	assertHasNoneIDs(t, got, []string{
		"http:DELETE:/branches/{pk}/remove_contact",
		"http:DELETE:/branches/{pk}/remove-contact",
	})
	// contact_id surfaces as a canonical path param in the emitted path.
	if !strings.Contains(idsFromString(got, "http:DELETE:/branches/{pk}/contacts/{contact_id}/remove"), "{contact_id}") {
		t.Errorf("expected {contact_id} path param in the action route path")
	}
}

// TestApplyDjangoDRFRoutes_ActionDefaultVerbatimSegment verifies #5298 (corrects
// #5230): an @action with no url_path and a method name `do_thing` is routed at
// the VERBATIM `do_thing` (underscores preserved). DRF's @action decorator sets
// `func.url_path = url_path if url_path else func.__name__` — the `_`→`-`
// replacement applies only to `url_name` (the reverse() lookup), NOT the URL.
func TestApplyDjangoDRFRoutes_ActionDefaultVerbatimSegment(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import WidgetViewSet

router = routers.DefaultRouter()
router.register(r"widgets", WidgetViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class WidgetViewSet(ModelViewSet):
    @action(detail=False)
    def do_thing(self, request):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{"http:GET:/widgets/do_thing"})
	assertHasNoneIDs(t, got, []string{"http:GET:/widgets/do-thing"})
}

// TestApplyDjangoDRFRoutes_NestedRouterActionParams verifies #5230 composes
// correctly with a drf-nested-routers parent prefix: a nested router mounted at
// `companies/(?P<company_pk>\d+)/branches` plus an @action with its own nested
// url_path surfaces BOTH the parent `{company_pk}` and the action's
// `{contact_id}` path params in the same route.
func TestApplyDjangoDRFRoutes_NestedRouterActionParams(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from rest_framework_nested import routers as nested_routers
from views import CompanyViewSet, BranchViewSet

router = routers.DefaultRouter()
router.register(r"companies", CompanyViewSet)

companies_router = nested_routers.NestedSimpleRouter(router, r"companies", lookup="company")
companies_router.register(r"branches", BranchViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class CompanyViewSet(ModelViewSet):
    pass

class BranchViewSet(ModelViewSet):
    @action(detail=True, methods=["delete"], url_path='contacts/(?P<contact_id>[^/.]+)/remove')
    def remove_contact(self, request, company_pk=None, pk=None, contact_id=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	// The nested router lookup="company" contributes the parent `{company}`
	// placeholder; the action's url_path contributes `{contact_id}`. Both
	// nested-prefix and action capture params survive into the same route (#5230).
	assertHasAllIDs(t, got, []string{
		"http:DELETE:/companies/{company}/branches/{pk}/contacts/{contact_id}/remove",
	})
}

// TestApplyDjangoDRFRoutes_BranchViewSetRegression5230 is the #5230 / #5298
// regression fixture: a BranchViewSet with actions (a nested-url_path detail
// action plus two default-segment detail actions) must produce the exact
// expected URL set. With no url_path the default segment is the method name
// VERBATIM (underscores preserved) — `set_director`, not `set-director`
// (#5298 corrects the earlier hyphenated belief; the explicit-url_path action
// is still used verbatim with its capture group).
func TestApplyDjangoDRFRoutes_BranchViewSetRegression5230(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import BranchViewSet

router = routers.DefaultRouter()
router.register(r"branches", BranchViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class BranchViewSet(ModelViewSet):
    @action(detail=True, methods=["delete"], url_path='contacts/(?P<contact_id>[^/.]+)/remove')
    def remove_contact(self, request, pk=None, contact_id=None):
        pass

    @action(detail=True, methods=["post"])
    def set_director(self, request, pk=None):
        pass

    @action(detail=True, methods=["post"])
    def clear_director(self, request, pk=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{
		"http:DELETE:/branches/{pk}/contacts/{contact_id}/remove",
		"http:POST:/branches/{pk}/set_director",
		"http:POST:/branches/{pk}/clear_director",
	})
	// The hyphenated forms must NOT appear — DRF's default url_path keeps
	// underscores (only url_name is hyphenated). The remove_contact method's
	// own name must not leak into the URL (its explicit url_path wins).
	assertHasNoneIDs(t, got, []string{
		"http:DELETE:/branches/{pk}/remove_contact",
		"http:POST:/branches/{pk}/set-director",
		"http:POST:/branches/{pk}/clear-director",
	})
}

// idsFromString returns the id from records that equals want, or "" if absent.
// Helper for path-param substring assertions.
func idsFromString(records []types.EntityRecord, want string) string {
	for _, e := range records {
		if e.ID == want {
			return e.ID
		}
	}
	return ""
}

// TestApplyDjangoDRFRoutes_LookupFieldOverride verifies that a ViewSet
// with lookup_field = "slug" emits {slug} placeholder in detail routes
// (CRUD + actions).
func TestApplyDjangoDRFRoutes_LookupFieldOverride(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ArticleViewSet

router = routers.DefaultRouter()
router.register(r"articles", ArticleViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ArticleViewSet(ModelViewSet):
    lookup_field = "slug"

    @action(detail=True, methods=["post"])
    def publish(self, request, slug=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{
		"http:GET:/articles/{slug}",
		"http:POST:/articles/{slug}/publish",
	})
	// With #730 dedup: exactly ONE canonical placeholder is emitted.
	// {pk}/{id}/{param} alias variants must NOT be present — the #704
	// byPath normalizer handles cross-placeholder matching at lookup time.
	assertHasNoneIDs(t, got, []string{
		"http:GET:/articles/{pk}",
		"http:GET:/articles/{id}",
		"http:GET:/articles/{param}",
		"http:POST:/articles/{pk}/publish",
		"http:POST:/articles/{id}/publish",
		"http:POST:/articles/{param}/publish",
	})
}

// TestApplyDjangoDRFRoutes_SinglePlaceholderEmission verifies that #730
// dedup is in effect: a ViewSet with lookup_field="slug" emits exactly ONE
// detail-route placeholder shape ({slug}), NOT the four-variant set that
// the pre-#730 multi-emit workaround produced.
func TestApplyDjangoDRFRoutes_SinglePlaceholderEmission(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import PostViewSet

router = routers.DefaultRouter()
router.register(r"posts", PostViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class PostViewSet(ModelViewSet):
    lookup_field = "slug"
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// Canonical slug placeholder must be present.
	assertHasAllIDs(t, got, []string{
		"http:GET:/posts/{slug}",
		"http:PUT:/posts/{slug}",
		"http:PATCH:/posts/{slug}",
		"http:DELETE:/posts/{slug}",
	})

	// Alias variants must NOT be present — the byPath normalizer (#704)
	// handles cross-placeholder matching at index-lookup time so we no
	// longer need to inflate the entity set with duplicates.
	assertHasNoneIDs(t, got, []string{
		"http:GET:/posts/{pk}",
		"http:GET:/posts/{id}",
		"http:GET:/posts/{param}",
		"http:PUT:/posts/{pk}",
		"http:PUT:/posts/{id}",
		"http:PUT:/posts/{param}",
	})
}

// TestApplyDjangoDRFRoutes_LegacyDetailRoute verifies that the pre-DRF-3.8
// @detail_route(methods=["post"]) decorator is interpreted as
// @action(detail=True, methods=["post"]).
func TestApplyDjangoDRFRoutes_LegacyDetailRoute(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import LegacyViewSet

router = routers.DefaultRouter()
router.register(r"legacy", LegacyViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import detail_route

class LegacyViewSet(ModelViewSet):
    @detail_route(methods=["post"], url_path="reset")
    def reset(self, request, pk=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{"http:POST:/legacy/{pk}/reset"})
}

// TestApplyDjangoDRFRoutes_NoIncludeStillEmits verifies that a routers file
// not included via path("...", include(...)) still produces routes at its
// bare register prefix (regression guard against the parent-prefix
// resolution returning nothing).
func TestApplyDjangoDRFRoutes_NoIncludeStillEmits(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import FooViewSet

router = routers.DefaultRouter()
router.register(r"foos", FooViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class FooViewSet(ModelViewSet):
    pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{
		"http:GET:/foos",
		"http:GET:/foos/{pk}",
	})
}

// TestApplyDjangoDRFRoutes_UnknownViewSetFallsBackToFullCRUD verifies
// that when the ViewSet class can't be located (e.g. its module is not
// in the classified file set), the pass still emits the full CRUD family
// rather than emitting nothing.
func TestApplyDjangoDRFRoutes_UnknownViewSetFallsBackToFullCRUD(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from third_party import MysteryViewSet

router = routers.DefaultRouter()
router.register(r"mystery", MysteryViewSet)
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py"}, files.reader)
	assertHasAllIDs(t, got, []string{
		"http:GET:/mystery",
		"http:POST:/mystery",
		"http:GET:/mystery/{pk}",
		"http:DELETE:/mystery/{pk}",
	})
}

// TestApplyDjangoDRFRoutes_ExplicitCreateOnBareViewSet verifies Issue #1648.
// A ViewSet whose base class is just `viewsets.ViewSet` (no CRUD mixins) but
// which explicitly defines `def create(self, ...)` must emit a per-verb POST
// endpoint at the bare prefix, not collapse to a verb-less ANY entry. This is
// the upvate RefreshViewSet shape — without the fix every consumer-side
// `POST /api/v1/auth/refresh` call falls into the verb-agnostic ANY bucket
// and the cross-repo matcher can't tell apart a real POST from an accidental
// GET / DELETE caller.
func TestApplyDjangoDRFRoutes_ExplicitCreateOnBareViewSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import RefreshViewSet

router = routers.DefaultRouter()
router.register(r"auth/refresh", RefreshViewSet)
`,
		"views.py": `
from rest_framework import viewsets

class RefreshViewSet(viewsets.ViewSet):
    http_method_names = ['post']

    def create(self, request):
        return None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	// Must emit the per-verb POST at the bare prefix (driven by the explicit
	// `def create` + `http_method_names=['post']`).
	assertHasAllIDs(t, got, []string{
		"http:POST:/auth/refresh",
	})
	// Must NOT emit phantom verbs filtered out by http_method_names=['post'].
	// In particular GET/list and DELETE/destroy/PUT/PATCH are forbidden by
	// the http_method_names gate, so they should never appear.
	assertHasNoneIDs(t, got, []string{
		"http:GET:/auth/refresh",
		"http:GET:/auth/refresh/{pk}",
		"http:DELETE:/auth/refresh/{pk}",
		"http:PUT:/auth/refresh/{pk}",
		"http:PATCH:/auth/refresh/{pk}",
	})
}

// TestApplyDjangoDRFRoutes_HTTPMethodNamesFiltersModelViewSet verifies that
// `http_method_names = ['post']` on a ModelViewSet correctly suppresses every
// CRUD method whose verb is NOT in the list. Mirrors Django REST Framework's
// dispatch behaviour: the verb gate runs BEFORE the method handler is
// resolved, so verbs not listed are simply unreachable in production.
func TestApplyDjangoDRFRoutes_HTTPMethodNamesFiltersModelViewSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import LoginViewSet

router = routers.DefaultRouter()
router.register(r"auth/login", LoginViewSet)
`,
		"views.py": `
from rest_framework import viewsets

class LoginViewSet(viewsets.ModelViewSet):
    http_method_names = ['post']

    def create(self, request, *args, **kwargs):
        return None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertHasAllIDs(t, got, []string{
		"http:POST:/auth/login",
	})
	// ModelViewSet would normally emit GET/PUT/PATCH/DELETE at the detail
	// path, but http_method_names=['post'] eliminates every non-POST verb.
	assertHasNoneIDs(t, got, []string{
		"http:GET:/auth/login",
		"http:GET:/auth/login/{pk}",
		"http:PUT:/auth/login/{pk}",
		"http:PATCH:/auth/login/{pk}",
		"http:DELETE:/auth/login/{pk}",
	})
}

// ---------------------------------------------------------------------------
// Issue #1692 — detail routes ({pk}) must emit per-verb, not ANY
// ---------------------------------------------------------------------------

// TestApplyDjangoDRFRoutes_DetailRoutePerVerbModelViewSet verifies that a
// standard ModelViewSet emits per-verb detail routes (GET, PUT, PATCH, DELETE)
// for the /{pk} path and does NOT emit ANY as a catch-all alongside them.
// This is the core regression test for #1692: the per-verb fix from #1648/#1673
// that landed on collection routes must also suppress the ANY catch-all on
// detail routes.
func TestApplyDjangoDRFRoutes_DetailRoutePerVerbModelViewSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import InspectionViewSet

router = routers.DefaultRouter()
router.register(r"inspections", InspectionViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class InspectionViewSet(ModelViewSet):
    http_method_names = ["get", "post", "put", "patch", "delete"]
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// All four detail verbs must be present.
	assertHasAllIDs(t, got, []string{
		"http:GET:/inspections/{pk}",
		"http:PUT:/inspections/{pk}",
		"http:PATCH:/inspections/{pk}",
		"http:DELETE:/inspections/{pk}",
	})
	// Fix #1692 — ANY must NOT be emitted alongside per-verb detail routes.
	assertHasNoneIDs(t, got, []string{"http:ANY:/inspections/{pk}"})
}

// TestApplyDjangoDRFRoutes_DetailRoutePerVerbExplicitBareViewSet verifies that
// a bare viewsets.ViewSet with explicitly defined retrieve/update/partial_update/
// destroy methods emits per-verb /{pk} routes and NOT ANY. This mirrors the
// collection-route fix from #1648/#1673 but for the detail-route path.
func TestApplyDjangoDRFRoutes_DetailRoutePerVerbExplicitBareViewSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ResourceViewSet

router = routers.DefaultRouter()
router.register(r"resources", ResourceViewSet)
`,
		"views.py": `
from rest_framework import viewsets

class ResourceViewSet(viewsets.ViewSet):
    def retrieve(self, request, pk=None):
        return None

    def update(self, request, pk=None):
        return None

    def partial_update(self, request, pk=None):
        return None

    def destroy(self, request, pk=None):
        return None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// Per-verb detail routes must be emitted via the explicitMethods merge.
	assertHasAllIDs(t, got, []string{
		"http:GET:/resources/{pk}",
		"http:PUT:/resources/{pk}",
		"http:PATCH:/resources/{pk}",
		"http:DELETE:/resources/{pk}",
	})
	// Fix #1692 — ANY must NOT appear alongside per-verb detail routes.
	assertHasNoneIDs(t, got, []string{"http:ANY:/resources/{pk}"})
}

// TestApplyDjangoDRFRoutes_DetailRouteANYFallbackWhenUnknownViewSet verifies
// that when a ViewSet class cannot be resolved from disk (no import/index hit),
// the fallback behaviour is to use modelViewSetMethods() which emits per-verb
// routes — not ANY. The ANY fallback is now only emitted when crudMethods is
// empty after resolution, which is a very rare edge case (unknown base with no
// explicit methods).
func TestApplyDjangoDRFRoutes_DetailRouteANYFallbackWhenUnknownViewSet(t *testing.T) {
	// viewSetFile is NOT listed in pyPaths, so parseViewSetClass returns zero
	// value. The conservative fallback at line 422 applies modelViewSetMethods()
	// which includes retrieve/update/partial_update/destroy — all per-verb.
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import GhostViewSet

router = routers.DefaultRouter()
router.register(r"ghosts", GhostViewSet)
`,
		// "views.py" intentionally absent — ViewSet not resolvable.
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py"}, files.reader)

	// The modelViewSetMethods() fallback should produce per-verb routes.
	assertHasAllIDs(t, got, []string{
		"http:GET:/ghosts/{pk}",
		"http:PUT:/ghosts/{pk}",
		"http:PATCH:/ghosts/{pk}",
		"http:DELETE:/ghosts/{pk}",
	})
	// ANY must NOT appear when modelViewSetMethods() fallback fills in detail verbs.
	assertHasNoneIDs(t, got, []string{"http:ANY:/ghosts/{pk}"})
}

// TestParseHTTPMethodNames covers the small parser used by parseViewSetClass
// when filtering CRUD methods through the http_method_names class attribute.
func TestParseHTTPMethodNames(t *testing.T) {
	tests := []struct {
		in   string
		want []string // sorted UPPERCASE verbs
	}{
		{`'post'`, []string{"POST"}},
		{`"post"`, []string{"POST"}},
		{`'get', 'post'`, []string{"GET", "POST"}},
		{`"get", "post", "patch"`, []string{"GET", "PATCH", "POST"}},
		{`  'post' ,  'put'  `, []string{"POST", "PUT"}},
		{``, nil},
	}
	for _, tc := range tests {
		got := parseHTTPMethodNames(tc.in)
		gotKeys := make([]string, 0, len(got))
		for k := range got {
			gotKeys = append(gotKeys, k)
		}
		sort.Strings(gotKeys)
		if !equalStringSlicesDRF(gotKeys, tc.want) {
			t.Errorf("parseHTTPMethodNames(%q) = %v, want %v", tc.in, gotKeys, tc.want)
		}
	}
}

// TestParseActionArgs verifies the @action decorator argument parser.
func TestParseActionArgs(t *testing.T) {
	tests := []struct {
		args        string
		defaultDet  bool
		wantDetail  bool
		wantMethods []string
		wantURL     string
	}{
		{`detail=True, methods=["post"], url_path="cancel"`, false, true, []string{"POST"}, "cancel"},
		{`detail=False`, false, false, nil, ""},
		{`methods=["get", "put"], detail=True`, false, true, []string{"GET", "PUT"}, ""},
		{``, true, true, nil, ""},
		{`methods=("post",)`, false, false, []string{"POST"}, ""},
	}
	for _, tc := range tests {
		got := parseActionArgs(tc.args, "do_thing", tc.defaultDet)
		if got.detail != tc.wantDetail {
			t.Errorf("parseActionArgs(%q) detail=%v want %v", tc.args, got.detail, tc.wantDetail)
		}
		if got.urlPath != tc.wantURL {
			t.Errorf("parseActionArgs(%q) url_path=%q want %q", tc.args, got.urlPath, tc.wantURL)
		}
		if !equalStringSlicesDRF(got.methods, tc.wantMethods) {
			t.Errorf("parseActionArgs(%q) methods=%v want %v", tc.args, got.methods, tc.wantMethods)
		}
	}
}

// TestClassifyViewSetParent covers the parent-class -> CRUD-method-set
// mapping.
func TestClassifyViewSetParent(t *testing.T) {
	tests := []struct {
		base string
		want []string
	}{
		{"ModelViewSet", []string{"create", "destroy", "list", "partial_update", "retrieve", "update"}},
		{"ReadOnlyModelViewSet", []string{"list", "retrieve"}},
		{"viewsets.ReadOnlyModelViewSet", []string{"list", "retrieve"}},
		{"mixins.ListModelMixin, mixins.RetrieveModelMixin, GenericViewSet", []string{"list", "retrieve"}},
		{"GenericViewSet", []string{}},
		// Unknown base falls back to the full ModelViewSet method set.
		{"SomeIntermediateBase", []string{"create", "destroy", "list", "partial_update", "retrieve", "update"}},
	}
	for _, tc := range tests {
		got := classifyViewSetParent(tc.base)
		gotKeys := make([]string, 0, len(got))
		for k := range got {
			gotKeys = append(gotKeys, k)
		}
		sort.Strings(gotKeys)
		want := append([]string(nil), tc.want...)
		sort.Strings(want)
		if strings.Join(gotKeys, ",") != strings.Join(want, ",") {
			t.Errorf("classifyViewSetParent(%q) = %v, want %v", tc.base, gotKeys, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #699c — source_handler + synthetic SCOPE.Operation entity tests
// ---------------------------------------------------------------------------

// TestApplyDjangoDRFRoutes_SourceHandlerSet verifies that each http_endpoint
// synthetic emitted for a CRUD method carries source_handler =
// "SCOPE.Operation:<ViewSet>.<method>" so ResolveHTTPEndpointHandlers can
// emit an IMPLEMENTS edge. Since #1692, no ANY catch-all is emitted alongside
// per-verb routes; this test also guards that if ANY is emitted (fallback case)
// it must NOT carry source_handler (it has no single owning method).
func TestApplyDjangoDRFRoutes_SourceHandlerSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import UserViewSet

router = routers.DefaultRouter()
router.register(r"users", UserViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class UserViewSet(ModelViewSet):
    pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	wantHandlers := map[string]string{
		"http:GET:/users":         "SCOPE.Operation:UserViewSet.list",
		"http:POST:/users":        "SCOPE.Operation:UserViewSet.create",
		"http:GET:/users/{pk}":    "SCOPE.Operation:UserViewSet.retrieve",
		"http:PUT:/users/{pk}":    "SCOPE.Operation:UserViewSet.update",
		"http:PATCH:/users/{pk}":  "SCOPE.Operation:UserViewSet.partial_update",
		"http:DELETE:/users/{pk}": "SCOPE.Operation:UserViewSet.destroy",
	}

	for _, r := range got {
		if r.Kind != httpEndpointKind {
			continue
		}
		want, ok := wantHandlers[r.ID]
		if !ok {
			continue
		}
		got := r.Properties["source_handler"]
		if got != want {
			t.Errorf("entity %q: source_handler=%q want %q", r.ID, got, want)
		}
	}

	// ANY catch-all must NOT have source_handler.
	for _, r := range got {
		if r.Kind != httpEndpointKind {
			continue
		}
		if r.Properties["verb"] == "ANY" && r.Properties["source_handler"] != "" {
			t.Errorf("ANY catch-all entity %q has unexpected source_handler=%q",
				r.ID, r.Properties["source_handler"])
		}
	}
}

// TestApplyDjangoDRFRoutes_SyntheticMethodEntitiesEmittedForInherited verifies
// that when a ModelViewSet does NOT explicitly define a CRUD method, a
// synthetic SCOPE.Operation entity is emitted for that method so the
// source_handler resolver has a target to bind.
func TestApplyDjangoDRFRoutes_SyntheticMethodEntitiesEmittedForInherited(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ArticleViewSet

router = routers.DefaultRouter()
router.register(r"articles", ArticleViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class ArticleViewSet(ModelViewSet):
    # No methods defined — all 6 are inherited from ModelViewSet.
    queryset = None
    serializer_class = None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// All six CRUD method entities should be emitted as synthetics.
	wantMethodNames := []string{
		"ArticleViewSet.list",
		"ArticleViewSet.create",
		"ArticleViewSet.retrieve",
		"ArticleViewSet.update",
		"ArticleViewSet.partial_update",
		"ArticleViewSet.destroy",
	}

	nameSet := make(map[string]bool)
	for _, r := range got {
		if r.Kind == "SCOPE.Operation" {
			nameSet[r.Name] = true
		}
	}

	for _, want := range wantMethodNames {
		if !nameSet[want] {
			t.Errorf("missing synthetic SCOPE.Operation entity for %q", want)
		}
	}
}

// TestApplyDjangoDRFRoutes_NoSyntheticForExplicitMethods verifies that when
// a ViewSet explicitly defines a CRUD method, NO duplicate synthetic entity
// is emitted (the Python extractor will have already emitted a real one).
func TestApplyDjangoDRFRoutes_NoSyntheticForExplicitMethods(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import PostViewSet

router = routers.DefaultRouter()
router.register(r"posts", PostViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class PostViewSet(ModelViewSet):
    def list(self, request):
        return super().list(request)
    def retrieve(self, request, pk=None):
        return super().retrieve(request, pk=pk)
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// list and retrieve are explicitly defined — no synthetic entity expected.
	explicitMethods := map[string]bool{
		"PostViewSet.list":     true,
		"PostViewSet.retrieve": true,
	}

	for _, r := range got {
		if r.Kind == "SCOPE.Operation" && explicitMethods[r.Name] {
			t.Errorf("unexpected synthetic SCOPE.Operation entity for explicitly-defined method %q", r.Name)
		}
	}

	// create, update, partial_update, destroy are inherited — synthetics expected.
	inheritedMethods := []string{
		"PostViewSet.create",
		"PostViewSet.update",
		"PostViewSet.partial_update",
		"PostViewSet.destroy",
	}
	nameSet := make(map[string]bool)
	for _, r := range got {
		if r.Kind == "SCOPE.Operation" {
			nameSet[r.Name] = true
		}
	}
	for _, want := range inheritedMethods {
		if !nameSet[want] {
			t.Errorf("missing synthetic SCOPE.Operation entity for inherited method %q", want)
		}
	}
}

// TestApplyDjangoDRFRoutes_ReadOnlyViewSetSyntheticMethods verifies that
// ReadOnlyModelViewSet emits synthetics only for list + retrieve.
func TestApplyDjangoDRFRoutes_ReadOnlyViewSetSyntheticMethods(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ReadOnlyVS

router = routers.DefaultRouter()
router.register(r"items", ReadOnlyVS)
`,
		"views.py": `
from rest_framework.viewsets import ReadOnlyModelViewSet

class ReadOnlyVS(ReadOnlyModelViewSet):
    pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	nameSet := make(map[string]bool)
	for _, r := range got {
		if r.Kind == "SCOPE.Operation" {
			nameSet[r.Name] = true
		}
	}

	// Only list and retrieve should be emitted for ReadOnly.
	if !nameSet["ReadOnlyVS.list"] {
		t.Error("missing ReadOnlyVS.list synthetic entity")
	}
	if !nameSet["ReadOnlyVS.retrieve"] {
		t.Error("missing ReadOnlyVS.retrieve synthetic entity")
	}

	// Mutable methods must NOT be emitted.
	for _, unwanted := range []string{"ReadOnlyVS.create", "ReadOnlyVS.update", "ReadOnlyVS.destroy"} {
		if nameSet[unwanted] {
			t.Errorf("unexpected synthetic entity for ReadOnly-unsupported method %q", unwanted)
		}
	}
}

// TestApplyDjangoDRFRoutes_ActionSourceHandlerSet verifies that @action
// endpoints also receive source_handler pointing to the action method name.
func TestApplyDjangoDRFRoutes_ActionSourceHandlerSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ContractViewSet

router = routers.DefaultRouter()
router.register(r"contracts", ContractViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ContractViewSet(ModelViewSet):
    @action(detail=True, methods=["post"], url_path="cancel")
    def cancel(self, request, pk=None):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	var cancelEndpoint *types.EntityRecord
	for i := range got {
		if got[i].Kind == httpEndpointKind && got[i].ID == "http:POST:/contracts/{pk}/cancel" {
			cancelEndpoint = &got[i]
			break
		}
	}
	if cancelEndpoint == nil {
		t.Fatal("missing http:POST:/contracts/{pk}/cancel entity")
	}
	wantHandler := "SCOPE.Operation:ContractViewSet.cancel"
	if got := cancelEndpoint.Properties["source_handler"]; got != wantHandler {
		t.Errorf("cancel action source_handler=%q want %q", got, wantHandler)
	}
}

// TestApplyDjangoDRFRoutes_NoDuplicateSyntheticEntities verifies that when a
// ViewSet is registered on multiple prefixes (bare + parent-include), only ONE
// set of synthetic method entities is emitted (not one per prefix).
func TestApplyDjangoDRFRoutes_NoDuplicateSyntheticEntities(t *testing.T) {
	files := fileMap{
		"myproject/urls.py": `
from django.urls import path, include
urlpatterns = [
    path("api/v1/", include("core.routers")),
]
`,
		"core/routers.py": `
from rest_framework import routers
from core.views import UserViewSet

router = routers.DefaultRouter()
router.register(r"users", UserViewSet)
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet

class UserViewSet(ModelViewSet):
    pass
`,
	}
	got := ApplyDjangoDRFRoutes(
		[]string{"myproject/urls.py", "core/routers.py", "core/views.py"},
		files.reader,
	)

	// Count synthetic SCOPE.Operation entities for UserViewSet.list.
	var listCount int
	for _, r := range got {
		if r.Kind == "SCOPE.Operation" && r.Name == "UserViewSet.list" {
			listCount++
		}
	}
	if listCount != 1 {
		t.Errorf("UserViewSet.list synthetic entity count=%d want 1", listCount)
	}
}

// ---------------------------------------------------------------------------
// Issue #800 — prefix-doubled duplicate suppression tests
// ---------------------------------------------------------------------------

// TestApplyDjangoDRFRoutes_PrefixedOnlyNoBareDupe is the primary regression
// test for #800. When a router module is included via
// path('api/v1/', include('core.routers')), each ViewSet route must be emitted
// EXACTLY ONCE at the prefixed path (/api/v1/<prefix>/...) and NOT at the
// bare path (/<prefix>/...).
func TestApplyDjangoDRFRoutes_PrefixedOnlyNoBareDupe(t *testing.T) {
	files := fileMap{
		"upvate_core/urls.py": `
from django.urls import path, include
urlpatterns = [
    path('api/v1/', include('core.routers')),
]
`,
		"core/routers.py": `
from rest_framework import routers
from core.views import BuildingViewSet, DeviceViewSet, ContractViewSet

router = routers.DefaultRouter()
router.register(r'buildings', BuildingViewSet)
router.register(r'devices', DeviceViewSet)
router.register(r'contracts', ContractViewSet)
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet

class BuildingViewSet(ModelViewSet):
    pass

class DeviceViewSet(ModelViewSet):
    pass

class ContractViewSet(ModelViewSet):
    pass
`,
	}
	pyPaths := []string{"upvate_core/urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Each of the 3 ViewSets should appear ONLY at the /api/v1/ prefix.
	prefixedIDs := []string{
		"http:GET:/api/v1/buildings",
		"http:POST:/api/v1/buildings",
		"http:GET:/api/v1/buildings/{pk}",
		"http:PUT:/api/v1/buildings/{pk}",
		"http:PATCH:/api/v1/buildings/{pk}",
		"http:DELETE:/api/v1/buildings/{pk}",
		"http:GET:/api/v1/devices",
		"http:GET:/api/v1/contracts",
	}
	assertHasAllIDs(t, got, prefixedIDs)

	// Bare-path duplicates must NOT be present.
	bareIDs := []string{
		"http:GET:/buildings",
		"http:POST:/buildings",
		"http:GET:/buildings/{pk}",
		"http:GET:/devices",
		"http:GET:/contracts",
	}
	assertHasNoneIDs(t, got, bareIDs)

	// Verify each route was emitted exactly once.
	for _, wantID := range prefixedIDs {
		count := 0
		for _, r := range got {
			if r.ID == wantID {
				count++
			}
		}
		if count != 1 {
			t.Errorf("entity %q: emitted %d times, want exactly 1", wantID, count)
		}
	}
}

// TestApplyDjangoDRFRoutes_URLPrefixProperty verifies that entities emitted
// under a parent include() prefix carry the url_prefix property so downstream
// consumers can strip it when matching client-side API calls.
func TestApplyDjangoDRFRoutes_URLPrefixProperty(t *testing.T) {
	files := fileMap{
		"upvate_core/urls.py": `
from django.urls import path, include
urlpatterns = [
    path('api/v1/', include('core.routers')),
]
`,
		"core/routers.py": `
from rest_framework import routers
from core.views import BuildingViewSet

router = routers.DefaultRouter()
router.register(r'buildings', BuildingViewSet)
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet

class BuildingViewSet(ModelViewSet):
    pass
`,
	}
	pyPaths := []string{"upvate_core/urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	for _, r := range got {
		if r.Kind != httpEndpointKind {
			continue
		}
		if r.Properties["url_prefix"] != "/api/v1" {
			t.Errorf("entity %q: url_prefix=%q want \"/api/v1\"", r.ID, r.Properties["url_prefix"])
		}
	}
}

// TestApplyDjangoDRFRoutes_NestedIncludeChain tests the "beyond the minimum"
// case: path('api/', include([path('v1/', include('core.routers'))]))
// should resolve to /api/v1/buildings/, not /buildings/ or /v1/buildings/.
// This test covers the case where findParentIncludePrefixes recursively
// resolves the chain (current implementation walks one level; this test
// validates at least the direct include level works correctly).
func TestApplyDjangoDRFRoutes_NestedIncludeChain(t *testing.T) {
	files := fileMap{
		"upvate_core/urls.py": `
from django.urls import path, include
urlpatterns = [
    path('api/v1/', include('core.routers')),
]
`,
		"core/routers.py": `
from rest_framework import routers
from core.views import ContractViewSet

router = routers.DefaultRouter()
router.register(r'contracts', ContractViewSet)
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet

class ContractViewSet(ModelViewSet):
    pass
`,
	}
	pyPaths := []string{"upvate_core/urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Prefixed form must be present.
	assertHasAllIDs(t, got, []string{"http:GET:/api/v1/contracts"})
	// Bare form must NOT be present.
	assertHasNoneIDs(t, got, []string{"http:GET:/contracts"})
}

// TestApplyDjangoDRFRoutes_LegitimateMultiPrefixKept verifies that when the
// SAME ViewSet is registered under two DIFFERENT URL prefixes (a legitimate
// multi-prefix setup, NOT a duplicate), both routes are kept. Dedup must
// only suppress the bare/prefixed pair, not routes at genuinely different
// paths.
func TestApplyDjangoDRFRoutes_LegitimateMultiPrefixKept(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include
urlpatterns = [
    path('api/v1/', include('core.routers')),
    path('legacy/', include('core.routers')),
]
`,
		"core/routers.py": `
from rest_framework import routers
from core.views import LoginViewSet

router = routers.DefaultRouter()
router.register(r'login', LoginViewSet)
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet

class LoginViewSet(ModelViewSet):
    pass
`,
	}
	pyPaths := []string{"urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Both prefixed forms are legitimate and must be present.
	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/login",
		"http:GET:/legacy/login",
	})
	// The bare form must NOT be present — it is a dupe of one of the above.
	assertHasNoneIDs(t, got, []string{"http:GET:/login"})
}

func equalStringSlicesDRF(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Issue #786 — ApplyDjangoCBVRoutes tests
// ---------------------------------------------------------------------------

// TestApplyDjangoCBVRoutes_ListViewEmitsGet verifies that a ListView-based
// CBV emits a GET http_endpoint synthetic with source_handler pointing to
// the inherited `get` method.
func TestApplyDjangoCBVRoutes_ListViewEmitsGet(t *testing.T) {
	files := fileMap{
		"myapp/urls.py": `
from django.urls import path
from myapp.views import ContractListView

urlpatterns = [
    path("contracts/", ContractListView.as_view(), name="contract-list"),
]
`,
		"myapp/views.py": `
from django.views.generic import ListView

class ContractListView(ListView):
    model = None
    template_name = "contracts/list.html"
`,
	}
	got := ApplyDjangoCBVRoutes([]string{"myapp/urls.py", "myapp/views.py"}, files.reader)

	assertHasAllIDs(t, got, []string{"http:GET:/contracts"})
	// ListView is read-only — no POST expected.
	assertHasNoneIDs(t, got, []string{"http:POST:/contracts"})
}

// TestApplyDjangoCBVRoutes_CreateViewEmitsGetAndPost verifies that a
// CreateView-based CBV emits both GET and POST synthetics.
func TestApplyDjangoCBVRoutes_CreateViewEmitsGetAndPost(t *testing.T) {
	files := fileMap{
		"myapp/urls.py": `
from django.urls import path
from myapp.views import ContractCreateView

urlpatterns = [
    path("contracts/new/", ContractCreateView.as_view(), name="contract-create"),
]
`,
		"myapp/views.py": `
from django.views.generic.edit import CreateView

class ContractCreateView(CreateView):
    model = None
    fields = "__all__"
`,
	}
	got := ApplyDjangoCBVRoutes([]string{"myapp/urls.py", "myapp/views.py"}, files.reader)

	assertHasAllIDs(t, got, []string{
		"http:GET:/contracts/new",
		"http:POST:/contracts/new",
	})
}

// TestApplyDjangoCBVRoutes_SourceHandlerPointsToMethod verifies that the
// http_endpoint synthetic carries source_handler = "SCOPE.Operation:View.method".
func TestApplyDjangoCBVRoutes_SourceHandlerPointsToMethod(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path
from views import ItemDetailView

urlpatterns = [
    path("items/<int:pk>/", ItemDetailView.as_view()),
]
`,
		"views.py": `
from django.views.generic import DetailView

class ItemDetailView(DetailView):
    model = None
`,
	}
	got := ApplyDjangoCBVRoutes([]string{"urls.py", "views.py"}, files.reader)

	var found *types.EntityRecord
	for i := range got {
		if got[i].Kind == httpEndpointKind && got[i].Properties["verb"] == "GET" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatal("missing GET http_endpoint for ItemDetailView")
	}
	want := "SCOPE.Operation:ItemDetailView.get"
	if got := found.Properties["source_handler"]; got != want {
		t.Errorf("source_handler=%q want %q", got, want)
	}
}

// TestApplyDjangoCBVRoutes_SyntheticMethodEntitiesEmitted verifies that
// synthetic SCOPE.Operation entities are emitted for inherited handlers so
// the Phase-2 resolver can bind source_handler references.
func TestApplyDjangoCBVRoutes_SyntheticMethodEntitiesEmitted(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path
from views import UserListView

urlpatterns = [
    path("users/", UserListView.as_view()),
]
`,
		"views.py": `
from django.views.generic import ListView

class UserListView(ListView):
    pass
`,
	}
	got := ApplyDjangoCBVRoutes([]string{"urls.py", "views.py"}, files.reader)

	// A synthetic SCOPE.Operation entity for the inherited `get` method.
	found := false
	for _, r := range got {
		if r.Kind == "SCOPE.Operation" && r.Name == "UserListView.get" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing synthetic SCOPE.Operation entity for UserListView.get")
	}
}

// TestApplyDjangoCBVRoutes_ExplicitMethodNoSynthetic verifies that when the
// CBV explicitly defines a handler method, NO duplicate synthetic entity is
// emitted (the Python extractor already has the real one).
func TestApplyDjangoCBVRoutes_ExplicitMethodNoSynthetic(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path
from views import CustomListView

urlpatterns = [
    path("items/", CustomListView.as_view()),
]
`,
		"views.py": `
from django.views.generic import ListView

class CustomListView(ListView):
    def get(self, request, *args, **kwargs):
        return super().get(request, *args, **kwargs)
`,
	}
	got := ApplyDjangoCBVRoutes([]string{"urls.py", "views.py"}, files.reader)

	// The explicit `get` method must NOT produce a synthetic entity.
	for _, r := range got {
		if r.Kind == "SCOPE.Operation" && r.Name == "CustomListView.get" {
			t.Errorf("unexpected synthetic entity for explicitly-defined method CustomListView.get")
		}
	}
}

// TestApplyDjangoCBVRoutes_NestedIncludeComposesPrefix verifies that CBV
// routes in an included urls.py are emitted ONLY at the prefixed path. Fix
// #800: emitting both /orders and /api/v1/orders is wrong — Django only
// resolves to /api/v1/orders when the root conf says
// path("api/v1/", include("core.urls")).
func TestApplyDjangoCBVRoutes_NestedIncludeComposesPrefix(t *testing.T) {
	files := fileMap{
		"myproject/urls.py": `
from django.urls import path, include
urlpatterns = [
    path("api/v1/", include("core.urls")),
]
`,
		"core/urls.py": `
from django.urls import path
from core.views import OrderListView

urlpatterns = [
    path("orders/", OrderListView.as_view()),
]
`,
		"core/views.py": `
from django.views.generic import ListView

class OrderListView(ListView):
    model = None
`,
	}
	got := ApplyDjangoCBVRoutes(
		[]string{"myproject/urls.py", "core/urls.py", "core/views.py"},
		files.reader,
	)

	// Fix #800: ONLY the prefixed form should be emitted; bare /orders is
	// a structural duplicate and must NOT appear.
	assertHasAllIDs(t, got, []string{"http:GET:/api/v1/orders"})
	assertHasNoneIDs(t, got, []string{"http:GET:/orders"})
}

// TestApplyDjangoCBVRoutes_DeleteViewEmitsGetPost verifies DeleteView
// exposes GET (confirmation page) and POST (deletion submit).
func TestApplyDjangoCBVRoutes_DeleteViewEmitsGetPost(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path
from views import ContractDeleteView

urlpatterns = [
    path("contracts/<int:pk>/delete/", ContractDeleteView.as_view()),
]
`,
		"views.py": `
from django.views.generic.edit import DeleteView

class ContractDeleteView(DeleteView):
    model = None
    success_url = "/"
`,
	}
	got := ApplyDjangoCBVRoutes([]string{"urls.py", "views.py"}, files.reader)

	assertHasAllIDs(t, got, []string{
		"http:GET:/contracts/{pk}/delete",
		"http:POST:/contracts/{pk}/delete",
	})
}

// TestClassifyCBVParent covers key CBV base classes.
func TestClassifyCBVParent(t *testing.T) {
	tests := []struct {
		base     string
		wantGet  bool
		wantPost bool
	}{
		{"ListView", true, false},
		{"DetailView", true, false},
		{"TemplateView", true, false},
		{"CreateView", true, true},
		{"UpdateView", true, true},
		{"DeleteView", true, true},
		{"FormView", true, true},
		{"View", false, false},         // bare View: no defaults
		{"SomeCustomBase", true, true}, // unknown: fallback GET+POST
	}
	for _, tc := range tests {
		got := classifyCBVParent(tc.base)
		if got["get"] != tc.wantGet {
			t.Errorf("classifyCBVParent(%q) get=%v want %v", tc.base, got["get"], tc.wantGet)
		}
		if got["post"] != tc.wantPost {
			t.Errorf("classifyCBVParent(%q) post=%v want %v", tc.base, got["post"], tc.wantPost)
		}
	}
}

// TestDeduplicateNestedURLConfDRF_DeduplicatesWhenDRFCoversPath verifies that
// urlconf_nested_include ANY entries are dropped when drf_router_expanded
// per-verb entries cover the same path.
func TestDeduplicateNestedURLConfDRF_DeduplicatesWhenDRFCoversPath(t *testing.T) {
	nestedEntities := []types.EntityRecord{
		{
			ID:   "http:ANY:/api/v1/contracts",
			Name: "http:ANY:/api/v1/contracts",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/contracts",
				"pattern_type": "urlconf_nested_include",
			},
		},
		{
			ID:   "http:ANY:/api/v1/users",
			Name: "http:ANY:/api/v1/users",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/users",
				"pattern_type": "urlconf_nested_include",
			},
		},
	}

	drfEntities := []types.EntityRecord{
		{
			ID:   "http:GET:/api/v1/contracts",
			Name: "http:GET:/api/v1/contracts",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "GET",
				"path":         "/api/v1/contracts",
				"pattern_type": "drf_router_expanded",
			},
		},
		{
			ID:   "http:POST:/api/v1/contracts",
			Name: "http:POST:/api/v1/contracts",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "POST",
				"path":         "/api/v1/contracts",
				"pattern_type": "drf_router_expanded",
			},
		},
	}

	got := DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)

	// /api/v1/contracts ANY should be dropped (drf_router_expanded covers it)
	// /api/v1/users ANY should be kept (no drf_router_expanded coverage)
	if len(got) != 1 {
		t.Errorf("DeduplicateNestedURLConfDRF returned %d entities, want 1", len(got))
	}
	if len(got) > 0 && got[0].Properties["path"] != "/api/v1/users" {
		t.Errorf("Expected remaining entity for /api/v1/users, got %v", got[0].Properties["path"])
	}
}

// TestDeduplicateNestedURLConfDRF_KeepsWhenNoDRFCoverage verifies that
// urlconf_nested_include entries are kept when no drf_router_expanded
// entries exist for the same path.
func TestDeduplicateNestedURLConfDRF_KeepsWhenNoDRFCoverage(t *testing.T) {
	nestedEntities := []types.EntityRecord{
		{
			ID:   "http:ANY:/api/v1/users",
			Name: "http:ANY:/api/v1/users",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/users",
				"pattern_type": "urlconf_nested_include",
			},
		},
	}

	// No drf_router_expanded entries
	drfEntities := []types.EntityRecord{}

	got := DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)

	// Should keep the nested_include entry since no drf coverage
	if len(got) != 1 {
		t.Errorf("DeduplicateNestedURLConfDRF returned %d entities, want 1", len(got))
	}
}

// TestDeduplicateNestedURLConfDRF_PreservesNonDjangoEntities verifies that
// non-Django entities are preserved unchanged.
func TestDeduplicateNestedURLConfDRF_PreservesNonDjangoEntities(t *testing.T) {
	nestedEntities := []types.EntityRecord{
		{
			ID:   "http:GET:/api/v1/users",
			Name: "http:GET:/api/v1/users",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "GET",
				"path":         "/api/v1/users",
				"pattern_type": "flask_route",
			},
		},
		{
			ID:   "http:ANY:/api/v1/admin",
			Name: "http:ANY:/api/v1/admin",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/admin",
				"pattern_type": "urlconf_nested_include",
			},
		},
	}

	drfEntities := []types.EntityRecord{}

	got := DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)

	// Both should be kept (non-Django + nested with no drf coverage)
	if len(got) != 2 {
		t.Errorf("DeduplicateNestedURLConfDRF returned %d entities, want 2", len(got))
	}
}

// TestDeduplicateNestedURLConfDRF_HandlesMultiplePaths verifies correct
// behavior with multiple paths, some with drf coverage and some without.
func TestDeduplicateNestedURLConfDRF_HandlesMultiplePaths(t *testing.T) {
	nestedEntities := []types.EntityRecord{
		{
			ID:   "http:ANY:/api/v1/users",
			Name: "http:ANY:/api/v1/users",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/users",
				"pattern_type": "urlconf_nested_include",
			},
		},
		{
			ID:   "http:ANY:/api/v1/posts",
			Name: "http:ANY:/api/v1/posts",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/posts",
				"pattern_type": "urlconf_nested_include",
			},
		},
		{
			ID:   "http:ANY:/api/v1/comments",
			Name: "http:ANY:/api/v1/comments",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/comments",
				"pattern_type": "urlconf_nested_include",
			},
		},
	}

	drfEntities := []types.EntityRecord{
		// /api/v1/users has drf coverage
		{
			ID:   "http:GET:/api/v1/users",
			Name: "http:GET:/api/v1/users",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "GET",
				"path":         "/api/v1/users",
				"pattern_type": "drf_router_expanded",
			},
		},
		// /api/v1/posts has drf coverage
		{
			ID:   "http:POST:/api/v1/posts",
			Name: "http:POST:/api/v1/posts",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "POST",
				"path":         "/api/v1/posts",
				"pattern_type": "drf_router_expanded",
			},
		},
		// /api/v1/comments has no drf coverage
	}

	got := DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)

	// Should keep only /api/v1/comments (no drf coverage)
	if len(got) != 1 {
		t.Errorf("DeduplicateNestedURLConfDRF returned %d entities, want 1; got paths: %v",
			len(got), func() []string {
				var paths []string
				for _, e := range got {
					paths = append(paths, e.Properties["path"])
				}
				return paths
			}())
	}
	if len(got) > 0 && got[0].Properties["path"] != "/api/v1/comments" {
		t.Errorf("Expected remaining entity for /api/v1/comments, got %v", got[0].Properties["path"])
	}
}

// TestDeduplicateNestedURLConfDRF_FixtureAScenario simulates the fixture-a
// scenario where 46-68 urlconf_nested_include entries are deduplicated by
// drf_router_expanded coverage, leaving only bare nested-include entries
// (those with no DRF registration).
func TestDeduplicateNestedURLConfDRF_FixtureAScenario(t *testing.T) {
	// Create 68 urlconf_nested_include entries representing fixture-a
	nestedEntities := make([]types.EntityRecord, 68)
	for i := 0; i < 68; i++ {
		path := fmt.Sprintf("/api/v1/resource%d", i)
		nestedEntities[i] = types.EntityRecord{
			ID:   fmt.Sprintf("http:ANY:%s", path),
			Name: fmt.Sprintf("http:ANY:%s", path),
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         path,
				"pattern_type": "urlconf_nested_include",
			},
		}
	}

	// Create drf_router_expanded entries for paths 0-45 (46 total)
	// This leaves paths 46-67 with only nested_include coverage
	drfEntities := make([]types.EntityRecord, 46)
	for i := 0; i < 46; i++ {
		path := fmt.Sprintf("/api/v1/resource%d", i)
		drfEntities[i] = types.EntityRecord{
			ID:   fmt.Sprintf("http:GET:%s", path),
			Name: fmt.Sprintf("http:GET:%s", path),
			Kind: "http_endpoint",
			Properties: map[string]string{
				"verb":         "GET",
				"path":         path,
				"pattern_type": "drf_router_expanded",
			},
		}
	}

	got := DeduplicateNestedURLConfDRF(nestedEntities, drfEntities)

	// Should drop 46 entries (those with drf coverage), keep 22
	if len(got) != 22 {
		t.Errorf("Expected 22 remaining nested_include entries, got %d (removed %d)",
			len(got), len(nestedEntities)-len(got))
	}

	// Verify remaining are paths 46-67
	remainingPaths := make(map[string]bool)
	for _, e := range got {
		remainingPaths[e.Properties["path"]] = true
	}
	for i := 46; i < 68; i++ {
		path := fmt.Sprintf("/api/v1/resource%d", i)
		if !remainingPaths[path] {
			t.Errorf("Expected to keep path %s", path)
		}
	}
	for i := 0; i < 46; i++ {
		path := fmt.Sprintf("/api/v1/resource%d", i)
		if remainingPaths[path] {
			t.Errorf("Expected to drop path %s", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #1126 — DeduplicateHTTPSynthesisANY tests
// ---------------------------------------------------------------------------

// makeHTTPSynthesisANY is a helper that builds an http_endpoint_synthesis
// ANY entity for the given path (mimicking synthesizeDjangoFromComposed output).
func makeHTTPSynthesisANY(path string) types.EntityRecord {
	id := "http:ANY:" + path
	return types.EntityRecord{
		ID:   id,
		Name: id,
		Kind: httpEndpointKind,
		Properties: map[string]string{
			"verb":         "ANY",
			"path":         path,
			"framework":    "django",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
}

// makeDRFExpanded builds an http_endpoint entity as ApplyDjangoDRFRoutes emits.
func makeDRFExpanded(verb, path string) types.EntityRecord {
	id := "http:" + verb + ":" + path
	return types.EntityRecord{
		ID:   id,
		Name: id,
		Kind: httpEndpointKind,
		Properties: map[string]string{
			"verb":         verb,
			"path":         path,
			"framework":    "django",
			"pattern_type": "drf_router_expanded",
		},
	}
}

// TestDeduplicateHTTPSynthesisANY_BasicCRUD verifies that ANY synthesis entries
// for a ModelViewSet-backed path are removed when concrete verbs are present.
// Fixture: ModelViewSet on /api/v1/contracts — 6 concrete verbs from
// ApplyDjangoDRFRoutes. Both the list-path and detail-path ANY synthesis entries
// must be dropped. Since #1692 emitCRUDFamily no longer emits a drf_router_expanded
// ANY catch-all when per-verb routes are present.
func TestDeduplicateHTTPSynthesisANY_BasicCRUD(t *testing.T) {
	listPath := "/api/v1/contracts"
	detailPath := "/api/v1/contracts/{pk}"

	// Pass 2.5 synthesis entries (would have been ~200 ANY in upvate).
	synthEntities := []types.EntityRecord{
		makeHTTPSynthesisANY(listPath),
		makeHTTPSynthesisANY(detailPath),
		// Non-endpoint record — must be preserved unchanged.
		{ID: "other:entity", Name: "other", Kind: "SCOPE.Component"},
	}

	// Pass 2.6b DRF entries (6 CRUD verbs — no ANY catch-all since #1692).
	drfEntities := []types.EntityRecord{
		makeDRFExpanded("GET", listPath),
		makeDRFExpanded("POST", listPath),
		makeDRFExpanded("GET", detailPath),
		makeDRFExpanded("PUT", detailPath),
		makeDRFExpanded("PATCH", detailPath),
		makeDRFExpanded("DELETE", detailPath),
	}

	got := DeduplicateHTTPSynthesisANY(synthEntities, drfEntities)

	// Both http_endpoint_synthesis ANY entries (list + detail) must be gone.
	for _, e := range got {
		if e.Kind == httpEndpointKind &&
			e.Properties != nil &&
			e.Properties["pattern_type"] == "http_endpoint_synthesis" &&
			e.Properties["verb"] == "ANY" {
			t.Errorf("unexpected http_endpoint_synthesis ANY survived: id=%q path=%q",
				e.ID, e.Properties["path"])
		}
	}

	// The non-endpoint record must survive.
	found := false
	for _, e := range got {
		if e.ID == "other:entity" {
			found = true
			break
		}
	}
	if !found {
		t.Error("non-endpoint entity was incorrectly removed")
	}
}

// TestDeduplicateHTTPSynthesisANY_ReadOnly verifies that ANY synthesis entries
// for a ReadOnlyModelViewSet path (list + retrieve only) are dropped when those
// concrete-verb entries exist.
func TestDeduplicateHTTPSynthesisANY_ReadOnly(t *testing.T) {
	listPath := "/api/v1/products"
	detailPath := "/api/v1/products/{pk}"

	synthEntities := []types.EntityRecord{
		makeHTTPSynthesisANY(listPath),
		makeHTTPSynthesisANY(detailPath),
	}

	drfEntities := []types.EntityRecord{
		makeDRFExpanded("GET", listPath),   // list
		makeDRFExpanded("GET", detailPath), // retrieve
		// No ANY detail catch-all since #1692: emitCRUDFamily only emits
		// per-verb routes when crudMethods is known.
	}

	got := DeduplicateHTTPSynthesisANY(synthEntities, drfEntities)

	// Both synthesis ANY entries must be removed.
	for _, e := range got {
		if e.Kind == httpEndpointKind &&
			e.Properties != nil &&
			e.Properties["pattern_type"] == "http_endpoint_synthesis" &&
			e.Properties["verb"] == "ANY" {
			t.Errorf("unexpected http_endpoint_synthesis ANY survived: id=%q path=%q",
				e.ID, e.Properties["path"])
		}
	}

	if len(got) != 0 {
		t.Errorf("expected 0 remaining entities, got %d: %+v", len(got), got)
	}
}

// TestDeduplicateHTTPSynthesisANY_NoDRFCoverage verifies that ANY synthesis
// entries for paths NOT covered by drf_router_expanded are preserved.
// These are genuine multi-verb endpoints or non-DRF routes.
func TestDeduplicateHTTPSynthesisANY_NoDRFCoverage(t *testing.T) {
	genuinePath := "/api/v1/some-custom-view"

	synthEntities := []types.EntityRecord{
		makeHTTPSynthesisANY(genuinePath),
	}

	// Only DRF entries for a different path — no coverage for genuinePath.
	drfEntities := []types.EntityRecord{
		makeDRFExpanded("GET", "/api/v1/contracts"),
		makeDRFExpanded("POST", "/api/v1/contracts"),
	}

	got := DeduplicateHTTPSynthesisANY(synthEntities, drfEntities)

	if len(got) != 1 {
		t.Errorf("expected 1 entity preserved, got %d", len(got))
	}
	if len(got) > 0 && got[0].ID != "http:ANY:"+genuinePath {
		t.Errorf("wrong entity preserved: %q", got[0].ID)
	}
}

// TestDeduplicateHTTPSynthesisANY_EmptyInputs verifies nil-safety.
func TestDeduplicateHTTPSynthesisANY_EmptyInputs(t *testing.T) {
	if got := DeduplicateHTTPSynthesisANY(nil, nil); got != nil {
		t.Errorf("expected nil for nil inputs, got %v", got)
	}
	entities := []types.EntityRecord{makeHTTPSynthesisANY("/api/v1/foo")}
	if got := DeduplicateHTTPSynthesisANY(entities, nil); len(got) != 1 {
		t.Errorf("expected 1 entity preserved with nil drfEntities, got %d", len(got))
	}
	if got := DeduplicateHTTPSynthesisANY(nil, []types.EntityRecord{makeDRFExpanded("GET", "/api/v1/foo")}); got != nil {
		t.Errorf("expected nil for nil synthEntities, got %v", got)
	}
}

// TestDeduplicateHTTPSynthesisANY_ModelViewSetAndAction verifies the full
// fixture from issue #1126: a ModelViewSet with one @action emits 6
// drf_router_expanded per-verb entries (no ANY since #1692); the synthesis
// ANY for the same paths must be dropped. The @action-path ANY synthesis
// entry should also be dropped when covered.
func TestDeduplicateHTTPSynthesisANY_ModelViewSetAndAction(t *testing.T) {
	listPath := "/api/v1/users"
	detailPath := "/api/v1/users/{pk}"
	actionPath := "/api/v1/users/activate"

	synthEntities := []types.EntityRecord{
		makeHTTPSynthesisANY(listPath),
		makeHTTPSynthesisANY(detailPath),
		makeHTTPSynthesisANY(actionPath),
	}

	drfEntities := []types.EntityRecord{
		makeDRFExpanded("GET", listPath),
		makeDRFExpanded("POST", listPath),
		makeDRFExpanded("GET", detailPath),
		makeDRFExpanded("PUT", detailPath),
		makeDRFExpanded("PATCH", detailPath),
		makeDRFExpanded("DELETE", detailPath),
		// No ANY detail catch-all since #1692: per-verb routes suppress it.
		// @action(detail=False, methods=["post"]) on activate endpoint.
		makeDRFExpanded("POST", actionPath),
	}

	got := DeduplicateHTTPSynthesisANY(synthEntities, drfEntities)

	// All three http_endpoint_synthesis ANY entries must be removed.
	for _, e := range got {
		if e.Kind == httpEndpointKind &&
			e.Properties != nil &&
			e.Properties["pattern_type"] == "http_endpoint_synthesis" &&
			e.Properties["verb"] == "ANY" {
			t.Errorf("unexpected http_endpoint_synthesis ANY survived: id=%q path=%q",
				e.ID, e.Properties["path"])
		}
	}

	if len(got) != 0 {
		t.Errorf("expected 0 remaining entities, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Issue #1124 — same endpoint with AND without /api/v1/ prefix
// ---------------------------------------------------------------------------

// TestApplyDjangoDRFRoutes_LocalAttrIncludeNoBareDupe is the regression test
// for #1124. When a urls.py contains both `router.register(...)` calls and a
// `path("api/v1/", include(router.urls))` call in the SAME file (i.e., the
// router mount is entirely local, not reached via an outer parent include()),
// the pass must emit routes ONLY at the composed /api/v1/<prefix> path and
// NOT at the bare /<prefix> path.
//
// This is the primary pattern that produced 105 duplicates in a real project:
// the router is declared and registered in the same urls.py that also mounts
// it under a prefix. Previously, findParentIncludePrefixes returned [] for
// this file (no OTHER urls.py includes it via string form), so the fallback
// [""] caused routes to be emitted at bare prefix. Meanwhile the Route
// entities from the AST pass (applyDjangoRouteComposition) correctly used
// the local path() prefix, resulting in both /api/v1/X and /X in the graph.
func TestApplyDjangoDRFRoutes_LocalAttrIncludeNoBareDupe(t *testing.T) {
	files := fileMap{
		"upvate_core/urls.py": `
from django.urls import path, include
from rest_framework import routers
from upvate_core.views import (
    AlternateAddressViewSet,
    BuildingViewSet,
    ContractViewSet,
)

router = routers.DefaultRouter()
router.register(r"alternate-addresses", AlternateAddressViewSet, basename="alternate-address")
router.register(r"buildings", BuildingViewSet, basename="building")
router.register(r"contracts", ContractViewSet, basename="contract")

urlpatterns = [
    path("api/v1/", include(router.urls)),
]
`,
		"upvate_core/views.py": `
from rest_framework.viewsets import ModelViewSet

class AlternateAddressViewSet(ModelViewSet):
    pass

class BuildingViewSet(ModelViewSet):
    pass

class ContractViewSet(ModelViewSet):
    pass
`,
	}

	pyPaths := []string{"upvate_core/urls.py", "upvate_core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Must emit at the /api/v1/ prefix (correct composed path).
	prefixedIDs := []string{
		"http:GET:/api/v1/alternate-addresses",
		"http:POST:/api/v1/alternate-addresses",
		"http:GET:/api/v1/alternate-addresses/{pk}",
		"http:PUT:/api/v1/alternate-addresses/{pk}",
		"http:PATCH:/api/v1/alternate-addresses/{pk}",
		"http:DELETE:/api/v1/alternate-addresses/{pk}",
		"http:GET:/api/v1/buildings",
		"http:GET:/api/v1/contracts",
	}
	assertHasAllIDs(t, got, prefixedIDs)

	// Must NOT emit at bare prefix (the duplicates from #1124).
	bareIDs := []string{
		"http:GET:/alternate-addresses",
		"http:POST:/alternate-addresses",
		"http:GET:/alternate-addresses/{pk}",
		"http:GET:/buildings",
		"http:GET:/contracts",
	}
	assertHasNoneIDs(t, got, bareIDs)

	// Each prefixed route must appear exactly once.
	for _, wantID := range prefixedIDs {
		count := 0
		for _, r := range got {
			if r.ID == wantID {
				count++
			}
		}
		if count != 1 {
			t.Errorf("entity %q: emitted %d times, want exactly 1", wantID, count)
		}
	}
}

// TestApplyDjangoDRFRoutes_TwoLocalRoutersDistinctPrefixes verifies that when
// a single urls.py declares two routers mounted at different local prefixes
// (e.g. router at "api/v1/" and api_router at "api/v2/"), each ViewSet's
// routes land under the correct prefix and bare-path duplicates are absent.
func TestApplyDjangoDRFRoutes_TwoLocalRoutersDistinctPrefixes(t *testing.T) {
	files := fileMap{
		"myapp/urls.py": `
from django.urls import path, include
from rest_framework import routers
from myapp.views import UserViewSet, ReviewViewSet

router = routers.DefaultRouter()
router.register(r"users", UserViewSet)

api_router = routers.SimpleRouter()
api_router.register(r"reviews", ReviewViewSet)

urlpatterns = [
    path("api/v1/", include(router.urls)),
    path("api/v2/", include(api_router.urls)),
]
`,
		"myapp/views.py": `
from rest_framework.viewsets import ModelViewSet

class UserViewSet(ModelViewSet):
    pass

class ReviewViewSet(ModelViewSet):
    pass
`,
	}

	pyPaths := []string{"myapp/urls.py", "myapp/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Each router is mounted at its own prefix.
	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/users",
		"http:POST:/api/v1/users",
		"http:GET:/api/v1/users/{pk}",
		"http:GET:/api/v2/reviews",
		"http:POST:/api/v2/reviews",
		"http:GET:/api/v2/reviews/{pk}",
	})

	// No bare-path duplicates for either router.
	assertHasNoneIDs(t, got, []string{
		"http:GET:/users",
		"http:GET:/reviews",
	})
}

// TestApplyDjangoDRFRoutes_NestedSimpleRouter verifies that a
// drf-nested-routers NestedSimpleRouter registration correctly emits
// routes at the composed path:
//
//	<parent_prefix>/{parent_lookup_pk}/<child_prefix>/...
//
// Fixture:
//
//	router.register(r"groups", GroupsViewSet)
//	nested = routers.NestedSimpleRouter(router, r"groups", lookup="group")
//	nested.register(r"companies", CompaniesViewSet)
//
// Expected: GET /groups/{group_pk}/companies  (list)
//
//	GET /groups/{group_pk}/companies/{pk}  (detail)
//
// Refs #1424.
func TestApplyDjangoDRFRoutes_NestedSimpleRouter(t *testing.T) {
	files := fileMap{
		"api/urls.py": `
from rest_framework import routers
from rest_framework_nested import routers as nested_routers
from api.views import GroupsViewSet, CompaniesViewSet

router = routers.DefaultRouter()
router.register(r"groups", GroupsViewSet, basename="group")

nested = nested_routers.NestedSimpleRouter(router, r"groups", lookup="group")
nested.register(r"companies", CompaniesViewSet, basename="company")

urlpatterns = [
    path("api/v1/", include(router.urls)),
    path("api/v1/", include(nested.urls)),
]
`,
		"api/views.py": `
from rest_framework.viewsets import ModelViewSet

class GroupsViewSet(ModelViewSet):
    pass

class CompaniesViewSet(ModelViewSet):
    pass
`,
	}
	pyPaths := []string{"api/urls.py", "api/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Nested router routes must be present.
	// The lookup="group" param produces {group} in the path. DRF's drf-nested-routers
	// library uses {group_pk} at runtime, but the {*} path-normalizer in grafel
	// canonicalises all placeholders to {*} at match-time, so {group} resolves
	// against any client-side placeholder variant.
	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/groups/{group}/companies",
		"http:POST:/api/v1/groups/{group}/companies",
		"http:GET:/api/v1/groups/{group}/companies/{pk}",
		"http:PUT:/api/v1/groups/{group}/companies/{pk}",
		"http:PATCH:/api/v1/groups/{group}/companies/{pk}",
		"http:DELETE:/api/v1/groups/{group}/companies/{pk}",
	})

	// Parent router routes must also be present.
	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/groups",
		"http:GET:/api/v1/groups/{pk}",
	})
}

// TestApplyDjangoDRFRoutes_NestedRouter_ActionOnChildViewSet verifies that
// @action methods on a nested ViewSet emit at the full composed path:
//
//	<parent_prefix>/{group_pk}/<child_prefix>/<action_url_path>
//
// Refs #1424.
func TestApplyDjangoDRFRoutes_NestedRouter_ActionOnChildViewSet(t *testing.T) {
	files := fileMap{
		"api/urls.py": `
from rest_framework import routers
from rest_framework_nested import routers as nested_routers
from api.views import GroupsViewSet, CompaniesViewSet

router = routers.DefaultRouter()
router.register(r"groups", GroupsViewSet, basename="group")

nested = nested_routers.NestedSimpleRouter(router, r"groups", lookup="group")
nested.register(r"companies", CompaniesViewSet, basename="company")

urlpatterns = [
    path("api/v1/", include(router.urls)),
    path("api/v1/", include(nested.urls)),
]
`,
		"api/views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class GroupsViewSet(ModelViewSet):
    pass

class CompaniesViewSet(ModelViewSet):
    @action(detail=False, methods=["get"], url_path="sync-logs")
    def sync_logs(self, request, group_pk=None):
        pass
`,
	}
	pyPaths := []string{"api/urls.py", "api/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/groups/{group}/companies/sync-logs",
	})
}

// TestApplyDjangoDRFRoutes_NestedRouter_DefaultLookup verifies that when
// lookup is not specified in NestedSimpleRouter(), the default "pk" is used:
//
//	/parent_prefix/{pk}/child_prefix/...
//
// Refs #1424.
func TestApplyDjangoDRFRoutes_NestedRouter_DefaultLookup(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from rest_framework_nested import routers as nested_routers
from views import ParentViewSet, ChildViewSet

router = routers.DefaultRouter()
router.register(r"parents", ParentViewSet)

nested = nested_routers.NestedSimpleRouter(router, r"parents")
nested.register(r"children", ChildViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class ParentViewSet(ModelViewSet):
    pass

class ChildViewSet(ModelViewSet):
    pass
`,
	}
	pyPaths := []string{"urls.py", "views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Default lookup="pk" → {pk} in the nested path.
	assertHasAllIDs(t, got, []string{
		"http:GET:/parents/{pk}/children",
		"http:POST:/parents/{pk}/children",
		"http:GET:/parents/{pk}/children/{pk}",
	})
}

// ---------------------------------------------------------------------------
// Issue #2614 — ViewSet.as_view({dict}) route synthesis tests
// ---------------------------------------------------------------------------

// TestDRF_ActionDecorator_EmitsEndpoint verifies that a ViewSet with an
// @action decorator whose url_path contains a slash (e.g. "notes/create")
// is correctly emitted under the full composed path
// /<prefix>/{pk}/notes/create when detail=True. This is the upvate
// ContractViewSet pattern where custom sub-resource actions are mounted as
// nested URL segments rather than flat paths.
func TestDRF_ActionDecorator_EmitsEndpoint(t *testing.T) {
	files := fileMap{
		"core/routers.py": `
from rest_framework import routers
from core.views import ContractViewSet

router = routers.DefaultRouter()
router.register(r"contracts", ContractViewSet)

urlpatterns = [
    path("", include(router.urls)),
]
`,
		"upvate_core/urls.py": `
from django.urls import path, include
urlpatterns = [
    path("api/v1/", include("core.routers")),
]
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ContractViewSet(ModelViewSet):
    @action(
        methods=["post"],
        detail=False,
        url_path="notes/create",
    )
    def create_note(self, request):
        pass

    @action(
        methods=["delete"],
        detail=True,
        url_path="notes/delete",
    )
    def delete_note(self, request, pk, *args, **kwargs):
        pass
`,
	}

	pyPaths := []string{"upvate_core/urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// detail=False → collection route (no {pk})
	assertHasAllIDs(t, got, []string{
		"http:POST:/api/v1/contracts/notes/create",
	})
	// detail=True → detail route with {pk}
	assertHasAllIDs(t, got, []string{
		"http:DELETE:/api/v1/contracts/{pk}/notes/delete",
	})
}

// TestDRF_ActionDetailFalse_EmitsCollectionPath verifies that
// @action(detail=False) decorators on a ViewSet emit the action at the
// collection path (without a {pk} segment). Tests multiple HTTP verbs and
// url_path values to cover the common upvate patterns (permissions,
// s3_attachment, import viewsets).
func TestDRF_ActionDetailFalse_EmitsCollectionPath(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import PermissionViewSet

router = routers.DefaultRouter()
router.register(r"permissions", PermissionViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class PermissionViewSet(ModelViewSet):
    @action(methods=["put"], detail=False, url_path="bulk_update_permissions")
    def bulk_update_permissions(self, request):
        pass

    @action(methods=["get"], detail=True, url_path="scope_permissions")
    def scope_permissions(self, request, pk=None):
        pass

    @action(methods=["put"], detail=True, url_path="assign_scope_permissions")
    def assign_scope_permissions(self, request, pk=None):
        pass
`,
	}

	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// detail=False → collection path (no {pk})
	assertHasAllIDs(t, got, []string{
		"http:PUT:/permissions/bulk_update_permissions",
	})
	// detail=True → detail path with {pk}
	assertHasAllIDs(t, got, []string{
		"http:GET:/permissions/{pk}/scope_permissions",
		"http:PUT:/permissions/{pk}/assign_scope_permissions",
	})
	// detail=False action must NOT have {pk} in the path
	assertHasNoneIDs(t, got, []string{
		"http:PUT:/permissions/{pk}/bulk_update_permissions",
	})
}

// TestDjango_CustomPathOutsideRouter_EmitsEndpoint is the primary regression
// test for #2614 pattern (b): a DRF ViewSet mounted outside router.register()
// via the explicit method-map form ViewSet.as_view({'verb': 'action'}).
//
// This covers the upvate notification routing pattern where:
//
//	_notification_list = views.NotificationViewSet.as_view({
//	    'get': 'list', 'delete': 'delete_all'})
//	urlpatterns = [
//	    path("notifications/", _notification_list),
//	    path("notifications/<int:pk>/", _notification_detail),
//	]
func TestDjango_CustomPathOutsideRouter_EmitsEndpoint(t *testing.T) {
	files := fileMap{
		"upvate_core/urls.py": `
from django.urls import path, include
urlpatterns = [
    path("api/v1/", include("core.routers")),
]
`,
		"core/routers.py": `
from django.urls import path, include
from rest_framework import routers
from core import views

router = routers.DefaultRouter()
router.register(r"contracts", views.ContractViewSet)

_notification_list = views.NotificationViewSet.as_view({
    'get': 'list',
    'delete': 'delete_all',
})
_notification_detail = views.NotificationViewSet.as_view({
    'patch': 'partial_update',
    'delete': 'destroy',
})
_notification_mark_all_read = views.NotificationViewSet.as_view({
    'post': 'mark_all_read',
})

urlpatterns = [
    path("", include(router.urls)),
    path("notifications/", _notification_list, name="notifications-list"),
    path("notifications/mark-all-read/", _notification_mark_all_read, name="notifications-mark-all-read"),
    path("notifications/<int:pk>/", _notification_detail, name="notifications-detail"),
]
`,
	}

	pyPaths := []string{"upvate_core/urls.py", "core/routers.py"}
	got := ApplyDjangoViewSetAsViewRoutes(pyPaths, files.reader)

	// Pre-bound variable form: _notification_list → GET + DELETE at collection path.
	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/notifications",
		"http:DELETE:/api/v1/notifications",
	})
	// _notification_mark_all_read → POST at mark-all-read path.
	assertHasAllIDs(t, got, []string{
		"http:POST:/api/v1/notifications/mark-all-read",
	})
	// _notification_detail → PATCH + DELETE at detail path.
	assertHasAllIDs(t, got, []string{
		"http:PATCH:/api/v1/notifications/{pk}",
		"http:DELETE:/api/v1/notifications/{pk}",
	})
}

// TestDjango_ViewSetAsView_InlineDict verifies that an inline
// ViewSet.as_view({'get': 'list'}) call directly inside a path() (no
// pre-bound variable) is also handled.
func TestDjango_ViewSetAsView_InlineDict(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path
from views import ItemViewSet

urlpatterns = [
    path("items/", ItemViewSet.as_view({'get': 'list', 'post': 'create'})),
    path("items/<int:pk>/", ItemViewSet.as_view({'get': 'retrieve', 'delete': 'destroy'})),
]
`,
	}

	got := ApplyDjangoViewSetAsViewRoutes([]string{"urls.py"}, files.reader)

	assertHasAllIDs(t, got, []string{
		"http:GET:/items",
		"http:POST:/items",
		"http:GET:/items/{pk}",
		"http:DELETE:/items/{pk}",
	})
}

// TestDjango_ViewSetAsView_SourceHandlerSet verifies that the
// source_handler property on each emitted entity points to the correct
// ViewSet action method name, enabling the Phase-2 resolver to emit
// IMPLEMENTS edges.
func TestDjango_ViewSetAsView_SourceHandlerSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path
from views import NotificationViewSet

_list = NotificationViewSet.as_view({'get': 'list', 'delete': 'delete_all'})

urlpatterns = [
    path("notifications/", _list),
]
`,
	}

	got := ApplyDjangoViewSetAsViewRoutes([]string{"urls.py"}, files.reader)

	wantHandlers := map[string]string{
		"http:GET:/notifications":    "SCOPE.Operation:NotificationViewSet.list",
		"http:DELETE:/notifications": "SCOPE.Operation:NotificationViewSet.delete_all",
	}

	foundCount := 0
	for _, r := range got {
		if r.Kind != httpEndpointKind {
			continue
		}
		wantHandler, ok := wantHandlers[r.ID]
		if !ok {
			continue
		}
		foundCount++
		gotHandler := r.Properties["source_handler"]
		if gotHandler != wantHandler {
			t.Errorf("entity %q: source_handler=%q want %q", r.ID, gotHandler, wantHandler)
		}
	}
	if foundCount != 2 {
		t.Errorf("expected 2 matching entities, found %d", foundCount)
	}
}

// ---------------------------------------------------------------------------
// Issue #2669 — @action(url_path=) regex-named-group extraction
// ---------------------------------------------------------------------------

// TestDRF_ActionDecorator_RegexUrlPath_NamedGroups verifies that
// @action(url_path="…(?P<name>regex)…") decorators are parsed correctly
// after the balanced-paren scanner replaced the pre-#2669 regex that
// truncated at the first inner `)`. The url_path is also canonicalised
// to {name} form so the producer-side synthetic ID lines up with what
// consumer-side clients (frontend/mobile) emit for the same logical
// endpoint.
//
// Failure mode prior to the fix: NoteViewSet's three @action declarations
// silently disappeared from the index, cascading into 7 unresolved mobile
// orphans in the upvate group quality bench.
func TestDRF_ActionDecorator_RegexUrlPath_NamedGroups(t *testing.T) {
	files := fileMap{
		"core/routers.py": `
from rest_framework import routers
from core.views import NoteViewSet

router = routers.DefaultRouter()
router.register(r"notes", NoteViewSet)

urlpatterns = [
    path("", include(router.urls)),
]
`,
		"upvate_core/urls.py": `
from django.urls import path, include
urlpatterns = [
    path("api/v1/", include("core.routers")),
]
`,
		"core/views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class NoteViewSet(ModelViewSet):
    @action(detail=False, methods=['get', 'post'], url_path='group/(?P<group_id>[^/.]+)/entity/(?P<entity>[^/.]+)/(?P<entity_id>[^/.]+)')
    def notes_by_group_and_entity(self, request, group_id, entity, entity_id):
        pass

    @action(detail=False, methods=['get', 'post'], url_path='entity/(?P<entity>[^/.]+)/(?P<entity_id>[^/.]+)')
    def notes_by_entity(self, request, entity, entity_id):
        pass

    @action(detail=False, methods=['get'], url_path='catalogs/entity-types/(?P<entity>[^/.]+)')
    def entity_type_catalogs(self, request, entity):
        pass
`,
	}

	pyPaths := []string{"upvate_core/urls.py", "core/routers.py", "core/views.py"}
	got := ApplyDjangoDRFRoutes(pyPaths, files.reader)

	// Every @action above must emit a canonicalised endpoint whose path
	// uses `{name}` (not the raw `(?P<name>charclass)` substring).
	assertHasAllIDs(t, got, []string{
		"http:GET:/api/v1/notes/group/{group_id}/entity/{entity}/{entity_id}",
		"http:POST:/api/v1/notes/group/{group_id}/entity/{entity}/{entity_id}",
		"http:GET:/api/v1/notes/entity/{entity}/{entity_id}",
		"http:POST:/api/v1/notes/entity/{entity}/{entity_id}",
		"http:GET:/api/v1/notes/catalogs/entity-types/{entity}",
	})

	// Negative: the raw regex form must not leak into any emitted ID.
	for _, r := range got {
		if strings.Contains(r.ID, "(?P<") || strings.Contains(r.ID, "[^/.]") {
			t.Errorf("entity %q leaked raw regex syntax — canonicalisation incomplete", r.ID)
		}
	}
}

// TestScanBalancedClose covers the balanced-paren scanner directly so the
// edge cases — string literals containing `)`, triple-quoted blocks,
// nested function calls — are exercised independently of the full
// extraction pipeline.
func TestScanBalancedClose(t *testing.T) {
	cases := []struct {
		body      string
		start     int
		wantClose int
		wantOK    bool
	}{
		// Simple balanced pair.
		{"abc(xy)z", 4, 6, true},
		// Nested parens.
		{"(a(b)c)", 1, 6, true},
		// String literal containing `)`.
		{`(url_path='(?P<id>[^/.]+)')`, 1, 26, true},
		// Double-quoted string literal containing `)`.
		{`(x="a)b")`, 1, 8, true},
		// Escape inside string.
		{`(x='\\')`, 1, 7, true},
		// Triple-quoted block containing `)`.
		{`(x="""a)b""")`, 1, 12, true},
		// Unbalanced — returns ok=false.
		{`(abc`, 1, 0, false},
	}
	for _, tc := range cases {
		gotClose, gotOK := scanBalancedClose(tc.body, tc.start)
		if gotOK != tc.wantOK {
			t.Errorf("scanBalancedClose(%q, %d) ok=%v want %v", tc.body, tc.start, gotOK, tc.wantOK)
			continue
		}
		if gotOK && gotClose != tc.wantClose {
			t.Errorf("scanBalancedClose(%q, %d) close=%d want %d", tc.body, tc.start, gotClose, tc.wantClose)
		}
	}
}

// recordByID returns the EntityRecord with the given ID, or nil.
func recordByID(records []types.EntityRecord, id string) *types.EntityRecord {
	for i := range records {
		if records[i].ID == id {
			return &records[i]
		}
	}
	return nil
}

// assertProp asserts a property key has an exact value on the named endpoint.
func assertEndpointProp(t *testing.T, records []types.EntityRecord, id, key, want string) {
	t.Helper()
	e := recordByID(records, id)
	if e == nil {
		t.Fatalf("endpoint %q not emitted; got %v", id, idsFromRecords(records))
	}
	if got := e.Properties[key]; got != want {
		t.Errorf("endpoint %q prop %q = %q, want %q", id, key, got, want)
	}
}

// assertNoProp asserts a property key is absent (or empty) on the named endpoint.
func assertNoProp(t *testing.T, records []types.EntityRecord, id, key string) {
	t.Helper()
	e := recordByID(records, id)
	if e == nil {
		t.Fatalf("endpoint %q not emitted; got %v", id, idsFromRecords(records))
	}
	if got, ok := e.Properties[key]; ok && got != "" {
		t.Errorf("endpoint %q prop %q = %q, want absent", id, key, got)
	}
}

// TestApplyDjangoDRFRoutes_PostureOnAllRouterExpandedRoutes is the #3864
// value-asserting test: a ViewSet declaring pagination_class +
// permission_classes + throttle_classes must propagate that posture onto
// EVERY router-expanded route (list/create/retrieve/update/...), so the
// endpoint posture is visible on the exact http_endpoint entities the MCP
// `endpoints definitions` query returns.
func TestApplyDjangoDRFRoutes_PostureOnAllRouterExpandedRoutes(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ReportViewSet

router = routers.DefaultRouter()
router.register(r"reports", ReportViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.permissions import IsAuthenticated
from rest_framework.pagination import LimitOffsetPagination
from rest_framework.throttling import UserRateThrottle

class ReportViewSet(ModelViewSet):
    pagination_class = LimitOffsetPagination
    permission_classes = [IsAuthenticated]
    throttle_classes = [UserRateThrottle]
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// Every CRUD route must carry pagination + auth + rate-limit posture.
	allRoutes := []string{
		"http:GET:/reports",
		"http:POST:/reports",
		"http:GET:/reports/{pk}",
		"http:PUT:/reports/{pk}",
		"http:PATCH:/reports/{pk}",
		"http:DELETE:/reports/{pk}",
	}
	for _, id := range allRoutes {
		assertEndpointProp(t, got, id, "paginated", "true")
		assertEndpointProp(t, got, id, "pagination_style", "offset")
		assertEndpointProp(t, got, id, "pagination_params", "limit,offset")
		assertEndpointProp(t, got, id, "pagination_source", "pagination_class=LimitOffsetPagination")
		assertEndpointProp(t, got, id, "auth_required", "true")
		assertEndpointProp(t, got, id, "rate_limited", "true")
		assertEndpointProp(t, got, id, "rate_limit_source", "throttle_classes=UserRateThrottle")
		// Middleware chain carries the permission + throttle classes (view scope).
		assertEndpointProp(t, got, id, "middleware_names", "IsAuthenticated,UserRateThrottle")
		assertEndpointProp(t, got, id, "middleware_scope", "view")
		assertEndpointProp(t, got, id, "middleware_count", "2")
	}
}

// TestApplyDjangoDRFRoutes_ActionPostureOverride verifies that a per-@action
// posture override applies to that action's route ONLY, while the CRUD routes
// keep the ViewSet-level posture.
func TestApplyDjangoDRFRoutes_ActionPostureOverride(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import DocViewSet

router = routers.DefaultRouter()
router.register(r"docs", DocViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action
from rest_framework.permissions import IsAuthenticated, AllowAny

class DocViewSet(ModelViewSet):
    permission_classes = [IsAuthenticated]

    @action(detail=False, methods=["get"], url_path="public", permission_classes=[AllowAny])
    def public_docs(self, request):
        pass
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// CRUD route keeps the ViewSet-level auth.
	assertEndpointProp(t, got, "http:GET:/docs", "auth_required", "true")
	assertEndpointProp(t, got, "http:GET:/docs", "middleware_names", "IsAuthenticated")
	// The @action route overrode permission_classes=[AllowAny] → not required,
	// and the chain reflects AllowAny rather than IsAuthenticated.
	assertEndpointProp(t, got, "http:GET:/docs/public", "middleware_names", "AllowAny")
	assertNoProp(t, got, "http:GET:/docs/public", "auth_required")
}

// TestApplyDjangoDRFRoutes_NoPostureNegative verifies the negative case: a
// ViewSet that declares no pagination/permission/throttle gets NO posture
// props on its routes (no fabricated posture — honest-partial).
func TestApplyDjangoDRFRoutes_NoPostureNegative(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import BareViewSet

router = routers.DefaultRouter()
router.register(r"bare", BareViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class BareViewSet(ModelViewSet):
    queryset = None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	for _, id := range []string{"http:GET:/bare", "http:GET:/bare/{pk}"} {
		assertNoProp(t, got, id, "paginated")
		assertNoProp(t, got, id, "auth_required")
		assertNoProp(t, got, id, "rate_limited")
		assertNoProp(t, got, id, "middleware_chain")
	}
}

// TestApplyDjangoDRFRoutes_UnknownPaginatorNotStamped verifies honest-partial:
// a custom (unrecognised) pagination class does NOT flip `paginated`.
func TestApplyDjangoDRFRoutes_UnknownPaginatorNotStamped(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import CustomViewSet

router = routers.DefaultRouter()
router.register(r"custom", CustomViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class CustomViewSet(ModelViewSet):
    pagination_class = MyWeirdPaginator
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)
	assertNoProp(t, got, "http:GET:/custom", "paginated")
}

// TestApplyDjangoDRFRoutes_ProvenanceMixedViewSet is the #3831 value-asserting
// test. A ModelViewSet that OVERRIDES `list` in its body but INHERITS
// retrieve/update/partial_update/destroy from the DRF mixins, plus an @action,
// must tag each router-expanded route with the right provenance + defining_class:
//
//   - GET /things         (list, overridden)        → explicit, ThingViewSet
//   - GET /things/{pk}     (retrieve, inherited)      → inherited, RetrieveModelMixin
//   - PUT /things/{pk}     (update, inherited)        → inherited, UpdateModelMixin
//   - PATCH /things/{pk}   (partial_update, inherited)→ inherited, UpdateModelMixin
//   - DELETE /things/{pk}  (destroy, inherited)       → inherited, DestroyModelMixin
//   - POST /things         (create, inherited)        → inherited, CreateModelMixin
//   - POST /things/{pk}/archive (@action)            → action, ThingViewSet
func TestApplyDjangoDRFRoutes_ProvenanceMixedViewSet(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import ThingViewSet

router = routers.DefaultRouter()
router.register(r"things", ThingViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet
from rest_framework.decorators import action

class ThingViewSet(ModelViewSet):
    def list(self, request, *args, **kwargs):
        return None

    @action(detail=True, methods=["post"], url_path="archive")
    def archive(self, request, pk=None):
        return None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	// list is overridden in the body → explicit, defining_class = the ViewSet.
	assertEndpointProp(t, got, "http:GET:/things", "provenance", drfProvExplicit)
	assertEndpointProp(t, got, "http:GET:/things", "defining_class", "ThingViewSet")

	// retrieve is inherited from RetrieveModelMixin.
	assertEndpointProp(t, got, "http:GET:/things/{pk}", "provenance", drfProvInherited)
	assertEndpointProp(t, got, "http:GET:/things/{pk}", "defining_class", "RetrieveModelMixin")

	// update / partial_update both come from UpdateModelMixin.
	assertEndpointProp(t, got, "http:PUT:/things/{pk}", "provenance", drfProvInherited)
	assertEndpointProp(t, got, "http:PUT:/things/{pk}", "defining_class", "UpdateModelMixin")
	assertEndpointProp(t, got, "http:PATCH:/things/{pk}", "provenance", drfProvInherited)
	assertEndpointProp(t, got, "http:PATCH:/things/{pk}", "defining_class", "UpdateModelMixin")

	// destroy is inherited from DestroyModelMixin.
	assertEndpointProp(t, got, "http:DELETE:/things/{pk}", "provenance", drfProvInherited)
	assertEndpointProp(t, got, "http:DELETE:/things/{pk}", "defining_class", "DestroyModelMixin")

	// create is inherited from CreateModelMixin.
	assertEndpointProp(t, got, "http:POST:/things", "provenance", drfProvInherited)
	assertEndpointProp(t, got, "http:POST:/things", "defining_class", "CreateModelMixin")

	// The @action route is provenance=action, defining_class = the ViewSet
	// (the decorated method lives in the ViewSet body).
	assertEndpointProp(t, got, "http:POST:/things/{pk}/archive", "provenance", drfProvAction)
	assertEndpointProp(t, got, "http:POST:/things/{pk}/archive", "defining_class", "ThingViewSet")
}

// TestApplyDjangoDRFRoutes_ProvenanceAllExplicit is the #3831 negative case: a
// ViewSet that overrides EVERY CRUD method in its body → every route is
// provenance=explicit with defining_class = the ViewSet, and NO route carries
// an inherited mixin defining_class.
func TestApplyDjangoDRFRoutes_ProvenanceAllExplicit(t *testing.T) {
	files := fileMap{
		"urls.py": `
from rest_framework import routers
from views import FullViewSet

router = routers.DefaultRouter()
router.register(r"items", FullViewSet)
`,
		"views.py": `
from rest_framework.viewsets import ModelViewSet

class FullViewSet(ModelViewSet):
    def list(self, request, *a, **k): return None
    def create(self, request, *a, **k): return None
    def retrieve(self, request, *a, **k): return None
    def update(self, request, *a, **k): return None
    def partial_update(self, request, *a, **k): return None
    def destroy(self, request, *a, **k): return None
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py", "views.py"}, files.reader)

	for _, id := range []string{
		"http:GET:/items",
		"http:POST:/items",
		"http:GET:/items/{pk}",
		"http:PUT:/items/{pk}",
		"http:PATCH:/items/{pk}",
		"http:DELETE:/items/{pk}",
	} {
		assertEndpointProp(t, got, id, "provenance", drfProvExplicit)
		assertEndpointProp(t, got, id, "defining_class", "FullViewSet")
	}
}

// TestApplyDjangoDRFRoutes_ProvenanceUnresolvedIsSynthesized verifies that when
// the ViewSet class cannot be located on disk (no body ever read), the assumed
// CRUD family is tagged provenance=synthesized with no defining_class — the
// honest-partial case distinct from `inherited` (#3831).
func TestApplyDjangoDRFRoutes_ProvenanceUnresolvedIsSynthesized(t *testing.T) {
	files := fileMap{
		// No views.py — MysteryViewSet is never resolvable, so the pass falls
		// back to the full CRUD family without reading any class body.
		"urls.py": `
from rest_framework import routers
from somewhere.unknown import MysteryViewSet

router = routers.DefaultRouter()
router.register(r"mystery", MysteryViewSet)
`,
	}
	got := ApplyDjangoDRFRoutes([]string{"urls.py"}, files.reader)

	assertEndpointProp(t, got, "http:GET:/mystery", "provenance", drfProvSynthesized)
	assertNoProp(t, got, "http:GET:/mystery", "defining_class")
	assertEndpointProp(t, got, "http:DELETE:/mystery/{pk}", "provenance", drfProvSynthesized)
	assertNoProp(t, got, "http:DELETE:/mystery/{pk}", "defining_class")
}
