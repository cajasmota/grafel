package engine

import (
	"fmt"
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
// emit an IMPLEMENTS edge. The ANY catch-all must NOT carry source_handler
// (it has no single owning method).
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
