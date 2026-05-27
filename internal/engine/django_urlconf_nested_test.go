package engine

import (
	"testing"
)

// fileMap is a small in-memory file reader for unit tests.
type fileMap map[string]string

func (m fileMap) reader(relPath string) []byte {
	s, ok := m[relPath]
	if !ok {
		return nil
	}
	return []byte(s)
}

// TestApplyDjangoNestedURLConf_BasicInclude verifies that a simple
// path("prefix/", include("module.urls")) composition produces a fully
// resolved http_endpoint entity.
func TestApplyDjangoNestedURLConf_BasicInclude(t *testing.T) {
	files := fileMap{
		"myproject/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/v1/", include("api.urls")),
]
`,
		"api/urls.py": `
from django.urls import path
from api import views

urlpatterns = [
    path("users/", views.UserListView.as_view(), name="user-list"),
    path("users/<int:id>/", views.UserDetailView.as_view(), name="user-detail"),
]
`,
	}

	pyPaths := []string{"myproject/urls.py", "api/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	wantIDs := map[string]bool{
		"http:ANY:/api/v1/users":      false,
		"http:ANY:/api/v1/users/{id}": false,
	}
	for _, e := range got {
		if e.Kind != httpEndpointKind {
			continue
		}
		if _, ok := wantIDs[e.ID]; ok {
			wantIDs[e.ID] = true
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing expected http_endpoint %q", id)
		}
	}
}

// TestApplyDjangoNestedURLConf_PathParameters verifies that Django-style
// angle-bracket path parameters are canonicalized to {name} form.
func TestApplyDjangoNestedURLConf_PathParameters(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/v1/", include("api.urls")),
]
`,
		"api/urls.py": `
from django.urls import path
from api import views

urlpatterns = [
    path("users/<int:id>/checklists/", views.ChecklistView.as_view()),
    path("items/<str:slug>/", views.ItemView.as_view()),
]
`,
	}

	pyPaths := []string{"urls.py", "api/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	wantIDs := map[string]bool{
		"http:ANY:/api/v1/users/{id}/checklists": false,
		"http:ANY:/api/v1/items/{slug}":          false,
	}
	for _, e := range got {
		if e.Kind != httpEndpointKind {
			continue
		}
		if _, ok := wantIDs[e.ID]; ok {
			wantIDs[e.ID] = true
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing expected http_endpoint %q", id)
		}
	}
}

// TestApplyDjangoNestedURLConf_TwoLevelNesting verifies recursive include()
// composition (parent → child → grandchild). Max depth is 2 levels; this test
// exercises the first recursive step.
func TestApplyDjangoNestedURLConf_TwoLevelNesting(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("api.urls")),
]
`,
		"api/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("v1/", include("api.v1.urls")),
]
`,
		"api/v1/urls.py": `
from django.urls import path
from api.v1 import views

urlpatterns = [
    path("users/", views.UserListView.as_view()),
]
`,
	}

	pyPaths := []string{"urls.py", "api/urls.py", "api/v1/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	// Two-level compose: api/ + v1/ + users/ → /api/v1/users
	wantID := "http:ANY:/api/v1/users"
	found := false
	for _, e := range got {
		if e.ID == wantID {
			found = true
			break
		}
	}
	if !found {
		ids := make([]string, 0, len(got))
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		t.Errorf("missing %q; got: %v", wantID, ids)
	}
}

// TestApplyDjangoNestedURLConf_NonURLFileSkipped verifies that Python files
// whose base name does not end in "urls.py" are not scanned.
func TestApplyDjangoNestedURLConf_NonURLFileSkipped(t *testing.T) {
	files := fileMap{
		// This has the right syntax but the file is not named urls.py
		"views.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("api.urls")),
]
`,
		"api/urls.py": `
from django.urls import path

urlpatterns = [
    path("users/", None),
]
`,
	}

	pyPaths := []string{"views.py", "api/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	for _, e := range got {
		if e.ID == "http:ANY:/api/users" {
			t.Errorf("should NOT have scanned views.py for urlpatterns: got %q", e.ID)
		}
	}
}

// TestApplyDjangoNestedURLConf_MissingChildFile verifies that a missing
// included file is silently skipped (no panic, no spurious child route
// entities). Since #2677 the pass also emits a single `url_mount_point`
// entity per include() call so the mount site stays discoverable even
// when the child file is unreachable. Both the no-panic and the
// no-child-route assertions remain.
func TestApplyDjangoNestedURLConf_MissingChildFile(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("nonexistent.urls")),
]
`,
	}

	pyPaths := []string{"urls.py"}
	// Should not panic.
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)
	// Expect exactly one entity — the url_mount_point — and zero
	// urlconf_nested_include child-route entries (the child file is missing).
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 entity (url_mount_point); got %d", len(got))
	}
	if got[0].Properties["pattern_type"] != "url_mount_point" {
		t.Errorf("expected pattern_type=url_mount_point; got %q",
			got[0].Properties["pattern_type"])
	}
	if got[0].Properties["url_prefix"] != "/api" {
		t.Errorf("expected url_prefix=/api; got %q", got[0].Properties["url_prefix"])
	}
}

// TestApplyDjangoNestedURLConf_Dedup verifies that the same path is not
// emitted twice when multiple parents include the same child module.
func TestApplyDjangoNestedURLConf_Dedup(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("api.urls")),
    path("api/", include("api.urls")),
]
`,
		"api/urls.py": `
from django.urls import path

urlpatterns = [
    path("users/", None),
]
`,
	}

	pyPaths := []string{"urls.py", "api/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	count := 0
	for _, e := range got {
		if e.ID == "http:ANY:/api/users" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entity for /api/users, got %d", count)
	}
}

// TestApplyDjangoNestedURLConf_DjangoMiniFixture verifies the golden fixture
// from internal/quality/golden/python-django-mini: path("users/", include("users.urls"))
// in myproject/urls.py should produce /users, /users/{pk}, /users/health.
func TestApplyDjangoNestedURLConf_DjangoMiniFixture(t *testing.T) {
	files := fileMap{
		"myproject/urls.py": `
from django.contrib import admin
from django.urls import include, path

urlpatterns = [
    path("admin/", admin.site.urls),
    path("users/", include("users.urls")),
]
`,
		"users/urls.py": `
from django.urls import path
from users import views

urlpatterns = [
    path("", views.UserListView.as_view(), name="user-list"),
    path("<int:pk>/", views.UserDetailView.as_view(), name="user-detail"),
    path("health/", views.health_check, name="user-health"),
]
`,
	}

	pyPaths := []string{"myproject/urls.py", "users/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	wantIDs := map[string]bool{
		"http:ANY:/users/{pk}":   false,
		"http:ANY:/users/health": false,
		// path("", ...) with empty pattern composes to /users
		"http:ANY:/users": false,
	}
	for _, e := range got {
		if e.Kind != httpEndpointKind {
			continue
		}
		if _, ok := wantIDs[e.ID]; ok {
			wantIDs[e.ID] = true
		}
	}
	for id, found := range wantIDs {
		if !found {
			ids := make([]string, 0, len(got))
			for _, e := range got {
				ids = append(ids, e.ID)
			}
			t.Errorf("missing expected http_endpoint %q; got: %v", id, ids)
		}
	}
}

// TestApplyDjangoNestedURLConf_DRFRouterChild verifies that a child file using
// DRF router.register() (e.g. core/routers.py) is handled correctly when
// the parent urls.py uses include("core.routers").
func TestApplyDjangoNestedURLConf_DRFRouterChild(t *testing.T) {
	files := fileMap{
		"myproject/urls.py": `
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
router.register(r"health", views.HealthCheckViewSet, basename="health")
router.register(r"users", views.UserViewSet, basename="users")
router.register(r"checklists", views.ChecklistViewSet, basename="checklists")

urlpatterns = [
    path("", include(router.urls)),
]
`,
	}

	pyPaths := []string{"myproject/urls.py", "core/routers.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	wantIDs := map[string]bool{
		"http:ANY:/api/v1/health":     false,
		"http:ANY:/api/v1/users":      false,
		"http:ANY:/api/v1/checklists": false,
	}
	for _, e := range got {
		if e.Kind != httpEndpointKind {
			continue
		}
		if _, ok := wantIDs[e.ID]; ok {
			wantIDs[e.ID] = true
		}
	}
	for id, found := range wantIDs {
		if !found {
			ids := make([]string, 0, len(got))
			for _, e := range got {
				ids = append(ids, e.ID)
			}
			t.Errorf("missing expected http_endpoint %q; got: %v", id, ids)
		}
	}
}

// TestApplyDjangoNestedURLConf_FBVSourceHandler verifies that direct FBV
// view references in path() calls produce http_endpoint entities with a
// source_handler property pointing to the view function (issue #527).
func TestApplyDjangoNestedURLConf_FBVSourceHandler(t *testing.T) {
	files := fileMap{
		"conduit/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("api.urls")),
]
`,
		"api/urls.py": `
from django.urls import path
from api import views

urlpatterns = [
    path("users/", views.user_list, name="user-list"),
    path("users/<int:pk>/", views.user_detail, name="user-detail"),
    path("articles/", views.ArticleView.as_view(), name="article-list"),
    path("health/", views.health_check, name="health"),
]
`,
	}

	pyPaths := []string{"conduit/urls.py", "api/urls.py"}
	got := ApplyDjangoNestedURLConf(pyPaths, files.reader)

	byID := map[string]string{} // id → source_handler
	for _, e := range got {
		if e.Kind != httpEndpointKind {
			continue
		}
		byID[e.ID] = e.Properties["source_handler"]
	}

	// FBV: module-qualified → bare name as Controller:<name>
	if h := byID["http:ANY:/api/users"]; h != "Controller:user_list" {
		t.Errorf("http:ANY:/api/users source_handler = %q, want %q", h, "Controller:user_list")
	}
	if h := byID["http:ANY:/api/users/{pk}"]; h != "Controller:user_detail" {
		t.Errorf("http:ANY:/api/users/{pk} source_handler = %q, want %q", h, "Controller:user_detail")
	}
	// FBV bare name (no module prefix)
	if h := byID["http:ANY:/api/health"]; h != "Controller:health_check" {
		t.Errorf("http:ANY:/api/health source_handler = %q, want %q", h, "Controller:health_check")
	}
	// CBV as_view() — source_handler must be absent (not set by this pass)
	if h := byID["http:ANY:/api/articles"]; h != "" {
		t.Errorf("http:ANY:/api/articles source_handler = %q, want empty (CBV)", h)
	}

	// Issue #2278 — every synthetic emitted under a parent include() prefix
	// must carry the url_prefix property so the cross-repo HTTP resolver in
	// internal/links/http_pass.go can strip it when matching client paths.
	for _, e := range got {
		if e.Kind != httpEndpointKind {
			continue
		}
		if e.Properties["pattern_type"] != "urlconf_nested_include" {
			continue
		}
		if got := e.Properties["url_prefix"]; got != "/api" {
			t.Errorf("entity %q: url_prefix=%q, want %q", e.ID, got, "/api")
		}
	}
}

// TestResolveFBVHandler verifies the handler name extraction logic.
func TestResolveFBVHandler(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"views.user_list", "user_list"},
		{"user_list", "user_list"},
		{"views.UserView.as_view()", ""},
		{"UserView.as_view()", ""},
		{"", ""},
		{"app.views.fn", "fn"},
		{"ALLOWED_HOSTS.append", "append"}, // bare word after last dot — valid identifier
		{"include(router.urls)", ""},       // include() call — no handler
	}
	for _, tt := range tests {
		got := resolveFBVHandler(tt.input)
		if got != tt.want {
			t.Errorf("resolveFBVHandler(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestModulePathToFilePath verifies the module-path to file-path conversion.
// The output must always use forward slashes regardless of the host OS so that
// fileReader lookups (which use the same forward-slash convention) succeed on
// Windows (P6 of #2196).
func TestModulePathToFilePath(t *testing.T) {
	tests := []struct {
		modulePath string
		want       string
	}{
		{"api.urls", "api/urls.py"},
		{"apps.users.urls", "apps/users/urls.py"},
		{"urls", "urls.py"},
	}
	for _, tt := range tests {
		got := modulePathToFilePath(tt.modulePath)
		if got != tt.want {
			t.Errorf("modulePathToFilePath(%q) = %q, want %q", tt.modulePath, got, tt.want)
		}
	}
}

// TestModulePathToFilePath_relToParent verifies the relative-to-parent module
// resolution. The output must always use forward slashes (P6 of #2196).
func TestModulePathToFilePath_relToParent(t *testing.T) {
	tests := []struct {
		modulePath string
		parentPath string
		want       string
	}{
		// Standard case: child module inside same app directory.
		{"urls", "myapp/urls.py", "myapp/urls.py"},
		// Two-level parent: core/routers.py included from api/v1/urls.py.
		{"routers", "api/v1/urls.py", "api/v1/routers.py"},
		// Empty module path → empty result.
		{"", "api/urls.py", ""},
		// Parent with no directory component → empty result (no relative base).
		{"urls", "urls.py", ""},
	}
	for _, tt := range tests {
		got := modulePathToFilePath_relToParent(tt.modulePath, tt.parentPath)
		if got != tt.want {
			t.Errorf("modulePathToFilePath_relToParent(%q, %q) = %q, want %q",
				tt.modulePath, tt.parentPath, got, tt.want)
		}
	}
}
