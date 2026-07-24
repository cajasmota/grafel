package python_test

// django_admin_test.go — fixture tests for issue #1990.
//
// Verifies:
//   - admin.site.register(M, A) emits REFERENCES edges from the admin
//     module entity to both M and A.
//   - @admin.register(M) decorator emits a REFERENCES edge to M.
//   - ModelAdmin classes get list_display / search_fields / etc captured
//     as flat properties.
//   - @admin.action methods get admin_action=true + description stamped
//     as Operation properties.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// countAdminRefsFrom returns REFERENCES edges with pattern_type=admin_register
// emitted from the file entity (admin module) in entities.
func countAdminRefsFrom(entities []types.EntityRecord, filePath string) []types.RelationshipRecord {
	for i := range entities {
		e := &entities[i]
		if e.SourceFile != filePath {
			continue
		}
		if e.Kind != "SCOPE.Component" || e.Subtype != "file" {
			continue
		}
		var out []types.RelationshipRecord
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if r.Properties.Get("pattern_type") != "admin_register" {
				continue
			}
			out = append(out, r)
		}
		return out
	}
	return nil
}

// TestAdminSiteRegister verifies admin.site.register(M, A) emits two
// REFERENCES edges from the admin module — one to M, one to A.
//
// Issue #1990.
func TestAdminSiteRegister(t *testing.T) {
	src := `from django.contrib import admin
from .models import Permit

class PermitAdmin(admin.ModelAdmin):
    pass

admin.site.register(Permit, PermitAdmin)
`
	out := extractPy(t, src, "core/admin.py")
	edges := countAdminRefsFrom(out, "core/admin.py")
	if len(edges) != 2 {
		t.Fatalf("expected 2 admin_register REFERENCES edges, got %d", len(edges))
	}
	targets := []string{edges[0].ToID, edges[1].ToID}
	wantContain := []string{"Permit", "PermitAdmin"}
	for _, w := range wantContain {
		found := false
		for _, target := range targets {
			if strings.Contains(target, ":"+w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected REFERENCES edge target containing %q, got %v", w, targets)
		}
	}
}

// TestAdminSiteRegisterBareForm verifies admin.site.register(M) (no admin
// class supplied) still emits a REFERENCES edge to the model.
func TestAdminSiteRegisterBareForm(t *testing.T) {
	src := `from django.contrib import admin
from .models import Tag

admin.site.register(Tag)
`
	out := extractPy(t, src, "core/admin.py")
	edges := countAdminRefsFrom(out, "core/admin.py")
	if len(edges) != 1 {
		t.Fatalf("expected 1 admin_register REFERENCES edge, got %d", len(edges))
	}
	if !strings.Contains(edges[0].ToID, ":Tag") {
		t.Errorf("expected edge target containing :Tag, got %q", edges[0].ToID)
	}
}

// TestAdminRegisterDecorator verifies @admin.register(M) on a ModelAdmin
// class emits a REFERENCES edge to M plus to the admin class.
func TestAdminRegisterDecorator(t *testing.T) {
	src := `from django.contrib import admin
from .models import Contract

@admin.register(Contract)
class ContractAdmin(admin.ModelAdmin):
    pass
`
	out := extractPy(t, src, "core/admin.py")
	edges := countAdminRefsFrom(out, "core/admin.py")
	if len(edges) < 2 {
		t.Fatalf("expected ≥ 2 admin_register REFERENCES edges, got %d", len(edges))
	}
	var sawContract, sawAdmin bool
	for _, e := range edges {
		if strings.Contains(e.ToID, ":Contract") && !strings.Contains(e.ToID, ":ContractAdmin") {
			sawContract = true
		}
		if strings.Contains(e.ToID, ":ContractAdmin") {
			sawAdmin = true
		}
	}
	if !sawContract {
		t.Errorf("expected REFERENCES to Contract, edges=%v", edges)
	}
	if !sawAdmin {
		t.Errorf("expected REFERENCES to ContractAdmin, edges=%v", edges)
	}
}

// TestModelAdminProperties verifies a ModelAdmin class gets every canonical
// attribute (list_display, list_filter, search_fields, readonly_fields,
// ordering, …) captured as flat properties.
//
// W4R4 evidence: search_fields was missing while list_display / list_filter
// were captured — this test enforces consistency.
func TestModelAdminProperties(t *testing.T) {
	src := `from django.contrib import admin
from .models import Permit

class PermitAdmin(admin.ModelAdmin):
    list_display = ("id", "title", "status")
    list_filter = ("status",)
    search_fields = ("title", "applicant__name")
    readonly_fields = ("created_at",)
    ordering = ("-created_at",)
    date_hierarchy = "created_at"
    actions = ["approve_bulk"]
`
	out := extractPy(t, src, "core/admin.py")
	cls := findDRFClass(out, "core/admin.py", "PermitAdmin")
	if cls == nil {
		t.Fatalf("PermitAdmin class not found")
	}
	for _, want := range []string{
		"list_display", "list_filter", "search_fields",
		"readonly_fields", "ordering", "date_hierarchy", "actions",
	} {
		if v := cls.Properties[want]; v == "" {
			t.Errorf("ModelAdmin property %q missing", want)
		}
	}
	if cls.Properties["component_kind"] != "model_admin" {
		t.Errorf("component_kind = %q, want \"model_admin\"", cls.Properties["component_kind"])
	}
}

// TestAdminActionDecorator verifies @admin.action(description="…") on a
// ModelAdmin method stamps admin_action=true + description on the
// Operation entity.
func TestAdminActionDecorator(t *testing.T) {
	src := `from django.contrib import admin

class PermitAdmin(admin.ModelAdmin):
    @admin.action(description="Approve selected permits")
    def approve_bulk(self, request, queryset):
        queryset.update(status="approved")
`
	out := extractPy(t, src, "core/admin.py")
	op := findDRFOp(out, "core/admin.py", "PermitAdmin.approve_bulk")
	if op == nil {
		t.Fatalf("PermitAdmin.approve_bulk operation not found")
	}
	if op.Properties["admin_action"] != "true" {
		t.Errorf("admin_action = %q, want \"true\"", op.Properties["admin_action"])
	}
	if op.Properties["description"] != "Approve selected permits" {
		t.Errorf("description = %q, want \"Approve selected permits\"", op.Properties["description"])
	}
}

// TestAdminNonAdminFileSkipped verifies the pass is a no-op for files
// that aren't admin.py / admin/.
func TestAdminNonAdminFileSkipped(t *testing.T) {
	src := `from django.contrib import admin
from .models import Permit

admin.site.register(Permit)
`
	out := extractPy(t, src, "core/views.py")
	edges := countAdminRefsFrom(out, "core/views.py")
	if len(edges) != 0 {
		t.Errorf("expected 0 admin_register edges from non-admin file, got %d", len(edges))
	}
}
