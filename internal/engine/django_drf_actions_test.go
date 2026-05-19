package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
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
		"http:ANY:/api/v1/contracts/{pk}",
	}
	assertHasAllIDs(t, got, wantIDs)
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
// method name as the URL path.
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
	assertHasAllIDs(t, got, []string{"http:GET:/contracts/get_extras"})
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
	// The lookup_field=slug declaration is the canonical placeholder.
	// The pass ALSO emits {pk}/{id}/{param} alias variants as a
	// cross-repo match widener (#704 companion). The canonical slug
	// variant being present is the load-bearing assertion.
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
