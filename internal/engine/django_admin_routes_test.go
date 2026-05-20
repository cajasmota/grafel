// Tests for the Django admin URL synthesis pass — Issue #801.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// adminFileMapReader returns a NestedURLConfFileReader backed by the given map.
func adminFileMapReader(files map[string]string) NestedURLConfFileReader {
	return func(relPath string) []byte {
		if s, ok := files[relPath]; ok {
			return []byte(s)
		}
		return nil
	}
}

// adminIDSet returns a set of IDs from records for O(1) lookup.
func adminIDSet(records []types.EntityRecord) map[string]bool {
	out := make(map[string]bool, len(records))
	for _, r := range records {
		out[r.ID] = true
	}
	return out
}

// adminIDList returns all IDs from records for error messages.
func adminIDList(records []types.EntityRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.ID)
	}
	return out
}

// assertAdminHasID fails the test if the given ID is missing from records.
func assertAdminHasID(t *testing.T, records []types.EntityRecord, id string) {
	t.Helper()
	for _, r := range records {
		if r.ID == id {
			return
		}
	}
	t.Errorf("missing expected id %q; got: %v", id, adminIDList(records))
}

// assertAdminLacksID fails the test if the given ID is present in records.
func assertAdminLacksID(t *testing.T, records []types.EntityRecord, id string) {
	t.Helper()
	for _, r := range records {
		if r.ID == id {
			t.Errorf("unexpected id %q present in records", id)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Test 1: bare admin.site.register(Model)
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_BareRegister(t *testing.T) {
	files := map[string]string{
		"users/admin.py": `
from django.contrib import admin
from .models import User

admin.site.register(User)
`,
	}
	pyPaths := []string{"users/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	if len(records) == 0 {
		t.Fatal("expected admin route synthetics, got none")
	}

	// Must include changelist.
	want := "http:GET:/admin/users/user"
	found := false
	for _, r := range records {
		if r.ID == want {
			found = true
			break
		}
	}
	if !found {
		ids := make([]string, 0, len(records))
		for _, r := range records {
			ids = append(ids, r.ID)
		}
		t.Errorf("missing %q; got: %v", want, ids)
	}

	// Must include add (GET + POST).
	for _, id := range []string{
		"http:GET:/admin/users/user/add",
		"http:POST:/admin/users/user/add",
	} {
		found = false
		for _, r := range records {
			if r.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q", id)
		}
	}

	// All records must carry framework=django_admin.
	for _, r := range records {
		if r.Properties["framework"] != "django_admin" {
			t.Errorf("record %q missing framework=django_admin; props=%v", r.ID, r.Properties)
		}
		if r.Properties["pattern_type"] != "django_admin_synthetic" {
			t.Errorf("record %q missing pattern_type=django_admin_synthetic", r.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: @admin.register(Model) decorator
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_RegisterDecorator(t *testing.T) {
	files := map[string]string{
		"articles/admin.py": `
from django.contrib import admin
from .models import Article

@admin.register(Article)
class ArticleAdmin(admin.ModelAdmin):
    list_display = ["title"]
`,
	}
	pyPaths := []string{"articles/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	wantIDs := []string{
		"http:GET:/admin/articles/article",
		"http:GET:/admin/articles/article/add",
		"http:POST:/admin/articles/article/add",
		"http:GET:/admin/articles/article/{id}/change",
		"http:POST:/admin/articles/article/{id}/change",
		"http:GET:/admin/articles/article/{id}/delete",
		"http:POST:/admin/articles/article/{id}/delete",
		"http:GET:/admin/articles/article/{id}/history",
	}
	idSet := map[string]bool{}
	for _, r := range records {
		idSet[r.ID] = true
	}
	for _, id := range wantIDs {
		if !idSet[id] {
			ids := make([]string, 0, len(records))
			for _, r := range records {
				ids = append(ids, r.ID)
			}
			t.Errorf("missing %q; got: %v", id, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 3: class-based ModelAdmin with no explicit register() call
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_ClassBasedNoRegister(t *testing.T) {
	files := map[string]string{
		"products/admin.py": `
from django.contrib import admin
from .models import Product

class ProductAdmin(admin.ModelAdmin):
    list_display = ["name", "price"]
`,
	}
	pyPaths := []string{"products/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	// Should synthesize for "product" (strip "Admin" suffix, lowercase).
	want := "http:GET:/admin/products/product"
	found := false
	for _, r := range records {
		if r.ID == want {
			found = true
			break
		}
	}
	if !found {
		ids := make([]string, 0, len(records))
		for _, r := range records {
			ids = append(ids, r.ID)
		}
		t.Errorf("missing %q; got: %v", want, ids)
	}
}

// ---------------------------------------------------------------------------
// Test 4: search_fields triggers autocomplete endpoint
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_SearchFieldsAutocomplete(t *testing.T) {
	files := map[string]string{
		"catalog/admin.py": `
from django.contrib import admin
from .models import Item

@admin.register(Item)
class ItemAdmin(admin.ModelAdmin):
    search_fields = ["name", "sku"]
`,
	}
	pyPaths := []string{"catalog/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	want := "http:GET:/admin/catalog/item/autocomplete"
	found := false
	for _, r := range records {
		if r.ID == want {
			found = true
			break
		}
	}
	if !found {
		ids := make([]string, 0, len(records))
		for _, r := range records {
			ids = append(ids, r.ID)
		}
		t.Errorf("missing autocomplete %q; got: %v", want, ids)
	}
}

// ---------------------------------------------------------------------------
// Test 5: no search_fields — no autocomplete
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_NoSearchFieldsNoAutocomplete(t *testing.T) {
	files := map[string]string{
		"catalog/admin.py": `
from django.contrib import admin
from .models import Item

@admin.register(Item)
class ItemAdmin(admin.ModelAdmin):
    list_display = ["name"]
`,
	}
	pyPaths := []string{"catalog/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	unwanted := "http:GET:/admin/catalog/item/autocomplete"
	for _, r := range records {
		if r.ID == unwanted {
			t.Errorf("unexpected autocomplete route emitted without search_fields")
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: custom actions on ModelAdmin
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_CustomActions(t *testing.T) {
	files := map[string]string{
		"orders/admin.py": `
from django.contrib import admin
from .models import Order

def mark_shipped(modeladmin, request, queryset):
    queryset.update(status="shipped")

@admin.register(Order)
class OrderAdmin(admin.ModelAdmin):
    actions = [mark_shipped, "export_csv"]
`,
	}
	pyPaths := []string{"orders/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	for _, want := range []string{
		"http:POST:/admin/orders/order/mark_shipped",
		"http:POST:/admin/orders/order/export_csv",
	} {
		found := false
		for _, r := range records {
			if r.ID == want {
				found = true
				break
			}
		}
		if !found {
			ids := make([]string, 0, len(records))
			for _, r := range records {
				ids = append(ids, r.ID)
			}
			t.Errorf("missing custom action route %q; got: %v", want, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 7: site-level routes emitted once per project
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_SiteLevelRoutes(t *testing.T) {
	files := map[string]string{
		"users/admin.py": `
from django.contrib import admin
from .models import User
admin.site.register(User)
`,
	}
	pyPaths := []string{"users/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	siteRoutes := []string{
		"http:GET:/admin",
		"http:GET:/admin/login",
		"http:POST:/admin/login",
		"http:GET:/admin/logout",
		"http:GET:/admin/password_change",
		"http:POST:/admin/password_change",
		"http:GET:/admin/jsi18n",
	}
	idSet := map[string]bool{}
	for _, r := range records {
		idSet[r.ID] = true
	}
	for _, id := range siteRoutes {
		if !idSet[id] {
			ids := make([]string, 0, len(records))
			for _, r := range records {
				ids = append(ids, r.ID)
			}
			t.Errorf("missing site-level route %q; got: %v", id, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 8: get_urls() override — custom URL patterns synthesized
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_GetURLsOverride(t *testing.T) {
	files := map[string]string{
		"reports/admin.py": `
from django.contrib import admin
from django.urls import path
from .models import Report

@admin.register(Report)
class ReportAdmin(admin.ModelAdmin):
    def get_urls(self):
        urls = super().get_urls()
        custom_urls = [
            path("generate/", self.admin_site.admin_view(self.generate_view), name="report-generate"),
            path("export/", self.admin_site.admin_view(self.export_view), name="report-export"),
        ]
        return custom_urls + urls
`,
	}
	pyPaths := []string{"reports/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	for _, want := range []string{
		"http:GET:/admin/reports/report/generate",
		"http:GET:/admin/reports/report/export",
	} {
		found := false
		for _, r := range records {
			if r.ID == want {
				found = true
				break
			}
		}
		if !found {
			ids := make([]string, 0, len(records))
			for _, r := range records {
				ids = append(ids, r.ID)
			}
			t.Errorf("missing get_urls custom route %q; got: %v", want, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 9: admin.site.register with explicit admin class
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_RegisterWithExplicitClass(t *testing.T) {
	files := map[string]string{
		"inventory/admin.py": `
from django.contrib import admin
from .models import Product

class ProductAdmin(admin.ModelAdmin):
    search_fields = ["name"]

admin.site.register(Product, ProductAdmin)
`,
	}
	pyPaths := []string{"inventory/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	// Autocomplete must be emitted because ProductAdmin has search_fields.
	want := "http:GET:/admin/inventory/product/autocomplete"
	found := false
	for _, r := range records {
		if r.ID == want {
			found = true
			break
		}
	}
	if !found {
		ids := make([]string, 0, len(records))
		for _, r := range records {
			ids = append(ids, r.ID)
		}
		t.Errorf("missing autocomplete from explicit class %q; got: %v", want, ids)
	}
}

// ---------------------------------------------------------------------------
// Test 10: TabularInline does NOT emit direct routes
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_InlineNoDirectRoutes(t *testing.T) {
	files := map[string]string{
		"shop/admin.py": `
from django.contrib import admin
from .models import Order, OrderItem

class OrderItemInline(admin.TabularInline):
    model = OrderItem

@admin.register(Order)
class OrderAdmin(admin.ModelAdmin):
    inlines = [OrderItemInline]
`,
	}
	pyPaths := []string{"shop/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	// OrderItemInline must NOT generate /admin/shop/orderitem/* routes.
	for _, r := range records {
		if strings.Contains(r.ID, "orderitem") {
			t.Errorf("unexpected inline route %q — TabularInline should not emit direct routes", r.ID)
		}
	}

	// OrderAdmin SHOULD generate routes.
	want := "http:GET:/admin/shop/order"
	found := false
	for _, r := range records {
		if r.ID == want {
			found = true
			break
		}
	}
	if !found {
		ids := make([]string, 0, len(records))
		for _, r := range records {
			ids = append(ids, r.ID)
		}
		t.Errorf("missing order changelist %q; got: %v", want, ids)
	}
}

// ---------------------------------------------------------------------------
// Test 11: model_class property set on per-model routes
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_ModelClassProperty(t *testing.T) {
	files := map[string]string{
		"blog/admin.py": `
from django.contrib import admin
from .models import Post

@admin.register(Post)
class PostAdmin(admin.ModelAdmin):
    pass
`,
	}
	pyPaths := []string{"blog/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	for _, r := range records {
		if strings.Contains(r.ID, "/post") && r.Properties["model_class"] == "" {
			t.Errorf("record %q missing model_class property", r.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 12: admin.site.register with bracketed list of models
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_BracketedModelList(t *testing.T) {
	files := map[string]string{
		"catalog/admin.py": `
from django.contrib import admin
from .models import Category, Tag

admin.site.register([Category, Tag])
`,
	}
	pyPaths := []string{"catalog/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	for _, want := range []string{
		"http:GET:/admin/catalog/category",
		"http:GET:/admin/catalog/tag",
	} {
		found := false
		for _, r := range records {
			if r.ID == want {
				found = true
				break
			}
		}
		if !found {
			ids := make([]string, 0, len(records))
			for _, r := range records {
				ids = append(ids, r.ID)
			}
			t.Errorf("missing %q from bracketed list; got: %v", want, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 13: per-app route emitted once per app
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_PerAppRoute(t *testing.T) {
	files := map[string]string{
		"blog/admin.py": `
from django.contrib import admin
from .models import Post, Comment
admin.site.register(Post)
admin.site.register(Comment)
`,
	}
	pyPaths := []string{"blog/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	appRoute := "http:GET:/admin/blog"
	count := 0
	for _, r := range records {
		if r.ID == appRoute {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 app-level route %q; got %d", appRoute, count)
	}
}

// ---------------------------------------------------------------------------
// Test 14: non-admin.py files are skipped (no false positives)
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_SkipsNonAdminFiles(t *testing.T) {
	files := map[string]string{
		"users/views.py": `
from django.contrib import admin
from .models import User
admin.site.register(User)
`,
	}
	pyPaths := []string{"users/views.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	if len(records) > 0 {
		ids := make([]string, 0, len(records))
		for _, r := range records {
			ids = append(ids, r.ID)
		}
		t.Errorf("expected no routes from non-admin file; got: %v", ids)
	}
}

// ---------------------------------------------------------------------------
// Test 15: deduplication — same model registered twice yields no duplicates
// ---------------------------------------------------------------------------

func TestApplyDjangoAdminRoutes_Deduplication(t *testing.T) {
	files := map[string]string{
		"users/admin.py": `
from django.contrib import admin
from .models import User
admin.site.register(User)
admin.site.register(User)
`,
	}
	pyPaths := []string{"users/admin.py"}
	reader := adminFileMapReader(files)
	records := ApplyDjangoAdminRoutes(pyPaths, reader)

	seen := map[string]int{}
	for _, r := range records {
		seen[r.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("duplicate entity %q emitted %d times", id, n)
		}
	}
}
