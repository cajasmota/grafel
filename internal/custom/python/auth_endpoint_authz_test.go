// Internal tests for the fine-grained authz (permission / scope) capture added
// to the Python endpoint-protection resolvers. These assert the SPECIFIC
// permission / scope value lands on the right bucket, not merely len>0.
package python

import (
	"strings"
	"testing"
)

// DRF custom permission class carrying an explicit permission literal —
// `HasPermission('orders.delete')` — must surface that string on Permissions.
func TestDRFAuth_CustomPermissionArg(t *testing.T) {
	block := "@permission_classes([IsAuthenticated, HasPermission('orders.delete')])"
	a := resolveDRFDecoratorAuth(block)
	if !a.found {
		t.Fatal("expected found=true for a non-public permission class")
	}
	if strings.Join(a.Permissions, ",") != "orders.delete" {
		t.Errorf("expected permissions=[orders.delete], got %v", a.Permissions)
	}
}

// django-oauth-toolkit `TokenHasScope` + `required_scopes = ['read']` must
// surface the scope on Scopes.
func TestDRFAuth_TokenHasScope(t *testing.T) {
	block := "required_scopes = ['read', 'write']\n@permission_classes([TokenHasScope])"
	a := resolveDRFDecoratorAuth(block)
	if !a.found {
		t.Fatal("expected found=true for TokenHasScope")
	}
	got := append([]string(nil), a.Scopes...)
	if strings.Join(sortedForTest(got), ",") != "read,write" {
		t.Errorf("expected scopes=[read,write], got %v", a.Scopes)
	}
}

// `HasScope('read')` inline arg must be captured as a scope, not a permission.
func TestDRFAuth_InlineScopeArg(t *testing.T) {
	block := "@permission_classes([HasScope('reports:read')])"
	a := resolveDRFDecoratorAuth(block)
	if strings.Join(a.Scopes, ",") != "reports:read" {
		t.Errorf("expected scopes=[reports:read], got %v", a.Scopes)
	}
	if len(a.Permissions) != 0 {
		t.Errorf("a scope class must not become a permission, got %v", a.Permissions)
	}
}

// Negative: a generic IsAuthenticated (authn, not authz) must add no
// permission/scope/role — it is auth_required only.
func TestDRFAuth_IsAuthenticatedNoPermission(t *testing.T) {
	a := resolveDRFDecoratorAuth("@permission_classes([IsAuthenticated])")
	if !a.found {
		t.Fatal("expected found=true (protected)")
	}
	if len(a.Permissions) != 0 || len(a.Scopes) != 0 || len(a.Roles) != 0 {
		t.Errorf("IsAuthenticated must not yield authz tokens, got perms=%v scopes=%v roles=%v",
			a.Permissions, a.Scopes, a.Roles)
	}
}

// Flask `@permission_required('app.delete_order')` routes the literal to
// Permissions, while `@roles_required('admin')` routes to Roles.
func TestFlaskAuth_PermissionRequiredArg(t *testing.T) {
	a := resolveFlaskDecoratorAuth("@permission_required('app.delete_order')")
	if strings.Join(a.Permissions, ",") != "app.delete_order" {
		t.Errorf("expected permissions=[app.delete_order], got %v", a.Permissions)
	}
	if len(a.Roles) != 0 {
		t.Errorf("permission_required must not yield roles, got %v", a.Roles)
	}

	r := resolveFlaskDecoratorAuth("@roles_required('admin')")
	if strings.Join(r.Roles, ",") != "admin" {
		t.Errorf("expected roles=[admin], got %v", r.Roles)
	}
	if len(r.Permissions) != 0 {
		t.Errorf("roles_required must not yield permissions, got %v", r.Permissions)
	}
}

func sortedForTest(s []string) []string {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s
}
